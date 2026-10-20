package harness_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/harness"
	"gopkg.in/yaml.v3"
)

func TestHermesDiscoveryResolvesProfilesWithoutWriting(t *testing.T) {
	home := tempDir(t)
	native, custom := filepath.Join(home, ".hermes"), filepath.Join(home, "custom")
	for _, root := range []string{native, custom} {
		profile := filepath.Join(root, "profiles", "work")
		if err := os.MkdirAll(profile, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(profile, ".env"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "active_profile"), []byte("\ufeffwork\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name, env, want string
	}{
		{"sticky default root", "", native + "/profiles/work"},
		{"sticky explicit root", "~/.hermes", native + "/profiles/work"},
		{"custom root", "${HOME}/custom", custom + "/profiles/work"},
		{"pinned profile", "$HOME/custom/profiles/pinned", custom + "/profiles/pinned"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options, err := harness.Discover(home, func(key string) string {
				if key == "HERMES_HOME" {
					return tc.env
				}
				return ""
			}, func(string) (string, error) { return "/bin/client", nil })
			if err != nil {
				t.Fatal(err)
			}
			o := options[2]
			if o.ID != "hermes" || !o.Detected || o.Problem != "" || o.ModelsFile != filepath.Join(tc.want, "config.yaml") {
				t.Fatalf("wrong profile: %+v", o)
			}
			if _, err := os.Stat(o.ModelsFile); !os.IsNotExist(err) {
				t.Fatal("discovery created configuration")
			}
		})
	}
	// Without a sticky selection Hermes uses the default profile.
	if err := os.Remove(filepath.Join(native, "active_profile")); err != nil {
		t.Fatal(err)
	}
	options, err := harness.Discover(home, func(string) string { return "" }, func(string) (string, error) { return "", os.ErrNotExist })
	if err != nil || options[2].Detected || options[2].ModelsFile != filepath.Join(native, "config.yaml") || options[2].Problem != "" {
		t.Fatalf("default profile: %+v, %v", options, err)
	}
}

func TestHermesDiscoveryRefusesAmbiguousOrStaleProfile(t *testing.T) {
	home := tempDir(t)
	dir := filepath.Join(home, ".hermes")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, env, active string }{
		{"missing", "", "missing"},
		{"traversal", "", "../../other"},
		{"undefined env", "$UNKNOWN/home", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(dir, "active_profile"), []byte(tc.active), 0600); err != nil {
				t.Fatal(err)
			}
			options, err := harness.Discover(home, func(key string) string {
				if key == "HERMES_HOME" {
					return tc.env
				}
				return ""
			}, func(string) (string, error) { return "/bin/client", nil })
			if err != nil || options[2].Problem == "" || options[0].Problem != "" || options[1].Problem != "" {
				t.Fatalf("invalid Hermes target disabled other integrations: %+v, %v", options, err)
			}
		})
	}
}

func TestHermesPreservesConfigurationAndReconcilesOwnedMetadata(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "config.yaml")
	root := filepath.Join(dir, "temper")
	original := `# My Hermes settings
model:
  provider: cloud
  default: cloud-model
  context_length: 4096 # explicit user limit
auxiliary:
  vision: {provider: cloud, model: vision}
shared: &headers
  X-Owner: mine
custom_providers:
  - name: legacy
    base_url: https://legacy.example/v1
    api_key: ${LEGACY_KEY}
providers:
  cloud: # keep this provider
    api: https://cloud.example/v1
    api_key: ${CLOUD_KEY}
    extra_headers: *headers
unfamiliar:
  number: 9007199254740993
  literal: "on"
  prompt: |
    Do useful work.
`
	if err := os.WriteFile(path, []byte(original), 0640); err != nil {
		t.Fatal(err)
	}
	choices := []harness.Selection{{ID: "hermes", ModelsFile: path}}
	vision := model("vision")
	vision.Input = []string{"text", "image"}
	sync := func(models []harness.Model) []harness.Result {
		t.Helper()
		plan, err := harness.Prepare(root, choices, "127.0.0.1:9444", models)
		if err != nil {
			t.Fatal(err)
		}
		results, err := plan.Publish(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return results
	}
	results := sync([]harness.Model{vision, model("stale")})
	if len(results) != 1 || results[0].Provider != "temper" || results[0].Status != "updated" {
		t.Fatal(results)
	}
	first, _ := os.ReadFile(path)
	info, _ := os.Stat(path)
	doc := yamlObject(t, first)
	providers := doc["providers"].(map[string]any)
	owned := providers["temper"].(map[string]any)
	if owned["api"] != "http://127.0.0.1:9444/v1" || owned["transport"] != "chat_completions" || owned["discover_models"] != true || owned["name"] != "temper" {
		t.Fatalf("wrong provider: %+v", owned)
	}
	metadata := owned["models"].(map[string]any)["vision"].(map[string]any)
	if metadata["context_length"] != 8192 || metadata["supports_vision"] != true {
		t.Fatal("wrong metadata", metadata)
	}
	delete(providers, "temper")
	if !reflect.DeepEqual(yamlObject(t, []byte(original)), doc) {
		t.Fatal("unrelated configuration changed", string(first))
	}
	for _, comment := range []string{"# My Hermes settings", "# explicit user limit", "# keep this provider", "&headers", "*headers", "# temper-root: " + strconv.Quote(root)} {
		if !bytes.Contains(first, []byte(comment)) {
			t.Fatalf("lost %q:\n%s", comment, first)
		}
	}
	if info.Mode().Perm() != 0640 {
		t.Fatal("lost file permissions")
	}
	if results := sync([]harness.Model{vision, model("stale")}); results[0].Status != "unchanged" {
		t.Fatal("identical activation rewrote config", results)
	}
	second, _ := os.ReadFile(path)
	after, _ := os.Stat(path)
	if !bytes.Equal(first, second) || !info.ModTime().Equal(after.ModTime()) {
		t.Fatal("clean activation changed bytes or timestamp")
	}

	// User-owned fields survive while endpoint aliases and stale metadata do
	// not keep routing to a previous activation.
	edited := strings.Replace(string(first), "name: temper", `name: temper
    api_key: ${TEMPER_KEY}
    extra_headers: {X-Custom: value}
    default_model: vision
    context_length: 2048
    base_url: http://obsolete/v1
    apiMode: responses`, 1)
	edited = strings.Replace(edited, "supports_vision: true", "supports_vision: true\n        custom_metadata: keep", 1)
	if err := os.WriteFile(path, []byte(edited), 0640); err != nil {
		t.Fatal(err)
	}
	vision.ContextWindow, vision.Input = 16384, []string{"text"}
	sync([]harness.Model{vision, model("coding")})
	next, _ := os.ReadFile(path)
	owned = yamlObject(t, next)["providers"].(map[string]any)["temper"].(map[string]any)
	metadata = owned["models"].(map[string]any)["vision"].(map[string]any)
	if metadata["context_length"] != 16384 || metadata["supports_vision"] != false || metadata["custom_metadata"] != "keep" {
		t.Fatal("wrong metadata reconciliation", metadata)
	}
	if owned["api_key"] != "${TEMPER_KEY}" || owned["default_model"] != "vision" || owned["context_length"] != 2048 || owned["extra_headers"].(map[string]any)["X-Custom"] != "value" || owned["base_url"] != nil || owned["apiMode"] != nil {
		t.Fatal("user settings or managed transport not reconciled", owned)
	}
	if strings.Contains(string(next), "stale:") || !strings.Contains(string(next), "coding:") {
		t.Fatal("wrong model list", string(next))
	}
}

func TestHermesRefusesMalformedAmbiguousAndUnownedConfiguration(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "config.yaml")
	owner := "# temper-root: " + strconv.Quote(dir)
	for name, raw := range map[string]string{
		"not mapping":     "[]",
		"null":            "null",
		"null providers":  "providers:",
		"duplicate":       "providers: {}\nproviders: {}",
		"duplicate model": "model: {default: one, default: two}",
		"multidoc":        "providers: {}\n---\nother: yes",
		"unowned":         "providers: {temper: {name: temper}}",
		"case collision":  "providers: {Temper: {api: 'http://localhost/v1'}}",
		"other root":      "providers:\n  temper: # temper-root: \"/another/root\"\n    name: temper",
		"legacy conflict": "custom_providers:\n  - {name: TEMPER, base_url: 'http://localhost/v1'}",
		"shared provider": "providers: &shared {}\nother: *shared",
		"merged root":     "base: &shared {providers: {}}\n<<: *shared",
		"merged provider": "base: &shared {api: old}\nproviders:\n  temper: " + owner + "\n    <<: *shared",
		"owned anchor":    "providers:\n  temper: " + owner + "\n    models: &shared {}\nother: *shared",
		"owned alias":     "shared: &shared {}\nproviders:\n  temper: " + owner + "\n    models: *shared",
		"model list":      "providers:\n  temper: " + owner + "\n    models: [one]",
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := harness.Prepare(dir, []harness.Selection{{ID: "hermes", ModelsFile: path}}, "127.0.0.1:8080", nil); err == nil {
				t.Fatalf("accepted ambiguous configuration:\n%s", raw)
			}
			current, _ := os.ReadFile(path)
			if string(current) != raw {
				t.Fatal("preflight changed the file")
			}
		})
	}
}

func TestHermesAndPiCannotShareAConfigurationFile(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "config")
	if _, err := harness.Prepare(dir, []harness.Selection{{ID: "pi", ModelsFile: path}, {ID: "hermes", ModelsFile: path}}, "127.0.0.1:8080", nil); err == nil {
		t.Fatal("accepted incompatible formats")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("refusal wrote configuration")
	}
}

func TestHermesEmptyAndFlowDocumentsRemainReadableAndClean(t *testing.T) {
	for _, raw := range []string{"", "# Keep my notes\n", "{}", "{providers: {}}"} {
		t.Run(raw, func(t *testing.T) {
			dir := tempDir(t)
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			selection := []harness.Selection{{ID: "hermes", ModelsFile: path}}
			plan, err := harness.Prepare(dir, selection, "127.0.0.1:8080", nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = plan.Publish(context.Background()); err != nil {
				t.Fatal(err)
			}
			saved, _ := os.ReadFile(path)
			if strings.Contains(raw, "# Keep my notes") && !bytes.Contains(saved, []byte("# Keep my notes")) {
				t.Fatal("lost comments from otherwise empty document")
			}
			plan, err = harness.Prepare(dir, selection, "127.0.0.1:8080", nil)
			if err != nil || plan.Results()[0].Status != "unchanged" {
				t.Fatalf("second activation: %v\n%s", err, saved)
			}
		})
	}
}

func yamlObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err, string(raw))
	}
	return doc
}
