package harness_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/harness"
)

func TestDiscoveryHonorsDirectoriesWithoutSelectingOrWriting(t *testing.T) {
	home := tempDir(t)
	env := map[string]string{"PIG_HOME": "~/pig-home", "PIG_CODING_AGENT_DIR": "~/pig-agent", "PI_CODING_AGENT_DIR": "~/pi-agent"}
	lookup := func(string) (string, error) { return "/bin/client", nil }
	options, err := harness.Discover(home, func(k string) string { return env[k] }, lookup)
	if err != nil || len(options) != 3 || options[0].ModelsFile != filepath.Join(home, "pi-agent/models.json") || options[1].ModelsFile != filepath.Join(home, "pig-agent/models.json") {
		t.Fatalf("wrong destinations: %+v, %v", options, err)
	}
	env["PIG_USE_PI_DIRS"] = "1"
	options, err = harness.Discover(home, func(k string) string { return env[k] }, lookup)
	if err != nil || options[0].ModelsFile != options[1].ModelsFile {
		t.Fatalf("shared mode: %+v, %v", options, err)
	}
	entries, _ := os.ReadDir(home)
	if len(entries) != 0 {
		t.Fatal("discovery wrote into the harness home")
	}
}

func model(id string) harness.Model {
	return harness.Model{ID: id, Name: id, ContextWindow: 8192, MaxTokens: 8192, Input: []string{"text"}, SamplingParams: map[string]any{"max_tokens": nil, "max_completion_tokens": nil}}
}

func TestDiscoveryKeepsUnusableOptionalDestinationsOutOfTheWay(t *testing.T) {
	dir := tempDir(t)
	broken := filepath.Join(dir, "broken")
	if err := os.Symlink(filepath.Join(dir, "absent"), broken); err != nil {
		t.Fatal(err)
	}
	options, err := harness.Discover(dir, func(k string) string {
		if k == "PI_CODING_AGENT_DIR" {
			return broken
		}
		return ""
	}, func(string) (string, error) { return "/bin/client", nil })
	if err != nil || len(options) != 3 || options[0].Problem == "" || options[1].Problem != "" || options[2].Problem != "" {
		t.Fatal("one optional destination prevented setup", options, err)
	}
}

func TestSharedTargetPreservesOtherConfigurationAndCleanReruns(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "models.json")
	original := `{"custom":{"exact":9007199254740993},"providers":{"local":{"models":[{"id":"mine"}],"extra":"keep"}}}`
	if err := os.WriteFile(path, []byte(original), 0640); err != nil {
		t.Fatal(err)
	}
	selections := []harness.Selection{{ID: "pi", ModelsFile: path}, {ID: "pig", ModelsFile: path}}
	plan, err := harness.Prepare(dir+"/temper", selections, "127.0.0.1:9444", []harness.Model{model("writing")})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Results()) != 1 || len(plan.Results()[0].Harnesses) != 2 {
		t.Fatal("shared destination was not grouped")
	}
	results, err := plan.Publish(context.Background())
	if err != nil || results[0].Status != "updated" || results[0].Provider != "temper" {
		t.Fatalf("%+v, %v", results, err)
	}
	first, _ := os.ReadFile(path)
	var doc struct {
		Providers map[string]json.RawMessage
	}
	if err := json.Unmarshal(first, &doc); err != nil || len(doc.Providers) != 2 || doc.Providers["temper"] == nil || doc.Providers["local"] == nil {
		t.Fatalf("expected temper alongside the existing provider: %s, %v", first, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0640 || !strings.Contains(string(first), "9007199254740993") || !strings.Contains(string(first), `"extra": "keep"`) || !strings.Contains(string(first), "http://127.0.0.1:9444/v1") {
		t.Fatalf("lost data, permissions or listener: %s", first)
	}
	again, err := harness.Prepare(dir+"/temper", selections, "127.0.0.1:9444", []harness.Model{model("writing")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = again.Publish(context.Background()); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)
	after, _ := os.Stat(path)
	if string(first) != string(second) || !info.ModTime().Equal(after.ModTime()) {
		t.Fatal("identical sync rewrote the file")
	}
	// A layout change replaces only the owned list; removed models disappear.
	next, err := harness.Prepare(dir+"/temper", selections, "127.0.0.1:9444", []harness.Model{model("coding")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = next.Publish(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "writing") || !strings.Contains(string(raw), "coding") || !strings.Contains(string(raw), "mine") {
		t.Fatalf("wrong reconciliation: %s", raw)
	}
}

func TestPreflightRefusesMalformedAndUnownedProviders(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "models.json")
	root := filepath.Join(dir, "temper")
	for _, raw := range []string{`[]`, `null`, `{"providers":null}`, `{"providers":{},"providers":{}}`, `{"providers":{"temper":{"name":"mine"}}}`, `{"providers":{"temper":{}}}`} {
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := harness.Prepare(root, []harness.Selection{{ID: "pi", ModelsFile: path}}, "127.0.0.1:8080", nil); err == nil {
			t.Fatalf("accepted %s", raw)
		}
		current, _ := os.ReadFile(path)
		if string(current) != raw {
			t.Fatal("preflight changed the file")
		}
	}
}

func TestConcurrentEditAndPartialFailureRecoverWithoutLosingUserData(t *testing.T) {
	dir := tempDir(t)
	a, b := filepath.Join(dir, "a", "models.json"), filepath.Join(dir, "b", "models.json")
	if err := os.Mkdir(filepath.Dir(b), 0700); err != nil {
		t.Fatal(err)
	}
	choices := []harness.Selection{{ID: "pi", ModelsFile: a}, {ID: "pig", ModelsFile: b}}
	plan, err := harness.Prepare(dir, choices, "127.0.0.1:8080", []harness.Model{model("one")})
	if err != nil {
		t.Fatal(err)
	}
	// The first file can commit; the second has changed since preflight.
	if err := os.WriteFile(b, []byte(`{"providers":{},"user":"new"}`), 0600); err != nil {
		t.Fatal(err)
	}
	results, err := plan.Publish(context.Background())
	if err == nil || results[0].Status != "updated" || results[1].Status != "failed" {
		t.Fatalf("%+v, %v", results, err)
	}
	again, err := harness.Prepare(dir, choices, "127.0.0.1:8080", []harness.Model{model("one")})
	if err != nil {
		t.Fatal(err)
	}
	results, err = again.Publish(context.Background())
	if err != nil || results[0].Status != "unchanged" || results[1].Status != "updated" {
		t.Fatalf("%+v, %v", results, err)
	}
	raw, _ := os.ReadFile(b)
	var saved map[string]any
	if err = json.Unmarshal(raw, &saved); err != nil || saved["user"] != "new" {
		t.Fatal("lost concurrent data")
	}
}

func TestPreviewAndCancellationCreateNothing(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "missing", "agent", "models.json")
	plan, err := harness.Prepare(dir, []harness.Selection{{ID: "pi", ModelsFile: path}}, "127.0.0.1:8080", nil)
	if err != nil || plan.Results()[0].Status != "pending" {
		t.Fatalf("%+v, %v", plan, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = plan.Publish(ctx); err == nil {
		t.Fatal("ignored cancellation")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("preview/cancel created directories")
	}
}

func tempDir(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestProjectionUsesRoutedIDsAndSavedCapabilities(t *testing.T) {
	raw, err := os.ReadFile("../testfixture/testdata/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{"nuextract3-q4km-llama", "qwen3.8-27b-q4xl-mtp", "qwen3-reranker-0.6b-q8-llama"}
	var presets []harness.Preset
	for i, id := range ids {
		l, err := catalog.CompilePreset(d, id, 0, 0, d.Runtime.Router.Target)
		if err != nil {
			t.Fatal(err)
		}
		if i == 1 {
			p := l.Records.Presets[id]
			p.RequestDefaults.MaxOutputTokens = 0
			p.Profiles[0].ContextWindowTokens = 32768
			p.Profiles[0].EngineConfig.Parallel = 2
			l.Records.Presets[id] = p
		}
		presets = append(presets, harness.Preset{ID: []string{"extract", "think", "rerank"}[i], Name: "Saved display name", Lock: l})
	}
	models, err := harness.Models(presets)
	if err != nil || len(models) != 2 {
		t.Fatal(models, err)
	}
	if models[0].ID != "extract" || models[0].Name != "Saved display name" || len(models[0].Input) != 2 || models[0].Input[1] != "image" || models[0].MaxTokens != 1536 {
		t.Fatal("lost routed identity, explicit output limit or image modality", models[0])
	}
	if models[1].ID != "think" || !models[1].Reasoning || models[1].ContextWindow != 16384 || models[1].MaxTokens != 16384 || len(models[1].SamplingParams) != 2 || models[1].Compat["supportsReasoningEffort"] != false {
		t.Fatal("lost slot context or server-owned reasoning/output behavior", models[1])
	}
}

func TestSurvivingModelsKeepUserSettingsWhileOwnedFieldsRefresh(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "models.json")
	id := "temper"
	initial := map[string]any{"providers": map[string]any{id: map[string]any{
		"name": "Temper (" + dir + ")", "apiKey": "MY_LOCAL_TOKEN", "headers": map[string]string{"User": "header"},
		"models": []any{map[string]any{"id": "one", "baseUrl": "http://old/v1", "api": "old",
			"custom": true, "compat": map[string]any{"custom": true, "supportsStore": true},
			"samplingParams": map[string]any{"temperature": 0.4, "max_tokens": 999}}}}}}
	raw, _ := json.Marshal(initial)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	desired := model("one")
	desired.SamplingParams = nil // An explicit cap replaces the uncapped policy.
	desired.Compat = map[string]any{"supportsStore": false}
	plan, err := harness.Prepare(dir, []harness.Selection{{ID: "pi", ModelsFile: path}}, "127.0.0.1:9090", []harness.Model{desired})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = plan.Publish(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(path)
	var doc struct {
		Providers map[string]struct {
			APIKey string `json:"apiKey"`
			Models []map[string]any
		}
	}
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	p := doc.Providers[id]
	m := p.Models[0]
	if p.APIKey != "MY_LOCAL_TOKEN" || m["custom"] != true || m["baseUrl"] != nil || m["api"] != nil || m["compat"].(map[string]any)["custom"] != true || m["compat"].(map[string]any)["supportsStore"] != false || m["samplingParams"].(map[string]any)["temperature"] != 0.4 || m["samplingParams"].(map[string]any)["max_tokens"] != nil {
		t.Fatalf("user settings lost or owned overrides stale: %s", raw)
	}
}

func TestConcurrentRootsCannotTakeOverProvider(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "models.json")
	choices := []harness.Selection{{ID: "pi", ModelsFile: path}}
	a, err := harness.Prepare(dir+"/a", choices, "127.0.0.1:8080", []harness.Model{model("a")})
	if err != nil {
		t.Fatal(err)
	}
	b, err := harness.Prepare(dir+"/b", choices, "127.0.0.1:8081", []harness.Model{model("b")})
	if err != nil {
		t.Fatal(err)
	}
	completed := make(chan error, 2)
	start := make(chan struct{})
	for _, plan := range []harness.Plan{a, b} {
		go func() {
			<-start
			_, err := plan.Publish(context.Background())
			completed <- err
		}()
	}
	close(start)
	first, second := <-completed, <-completed
	if (first == nil) == (second == nil) {
		t.Fatal("concurrent writers must serialize or detect changed preflight", first, second)
	}
	raw, _ := os.ReadFile(path)
	var doc struct {
		Providers map[string]struct {
			Name string
		}
	}
	if err := json.Unmarshal(raw, &doc); err != nil || len(doc.Providers) != 1 {
		t.Fatalf("expected one provider: %s, %v", raw, err)
	}
	owner, other := dir+"/a", dir+"/b"
	if doc.Providers["temper"].Name == "Temper ("+other+")" {
		owner, other = other, owner
	}
	if doc.Providers["temper"].Name != "Temper ("+owner+")" {
		t.Fatalf("missing root ownership: %s", raw)
	}
	if _, err := harness.Prepare(other, choices, "127.0.0.1:9090", []harness.Model{model("replacement")}); err == nil || !strings.Contains(err.Error(), "not owned by this Temper root") {
		t.Fatalf("another root must be refused on retry: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(raw) {
		t.Fatal("refused takeover changed the file")
	}
	plan, err := harness.Prepare(owner, choices, "127.0.0.1:9090", []harness.Model{model("replacement")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = plan.Publish(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, _ = os.ReadFile(path)
	if !strings.Contains(string(after), "replacement") || !strings.Contains(string(after), "http://127.0.0.1:9090/v1") {
		t.Fatalf("owning root could not update its provider: %s", after)
	}
}

func TestRefuseRedirectedTargets(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "models.json")
	choices := []harness.Selection{{ID: "pi", ModelsFile: path}}
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	var err error
	if err = os.Link(path, filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err = harness.Prepare(dir+"/a", choices, "127.0.0.1:8080", nil); err == nil {
		t.Fatal("hard-linked destination accepted")
	}
	if err = os.Remove(filepath.Join(dir, "linked")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "alias")
	if err = os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	options, err := harness.Discover(dir, func(key string) string {
		if key == "PI_CODING_AGENT_DIR" {
			return link
		}
		return ""
	}, func(string) (string, error) { return "/bin/client", nil })
	if err != nil || options[0].ModelsFile != path {
		t.Fatal("discovery did not resolve target", options, err)
	}
	if err = os.Rename(path, filepath.Join(dir, "old")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(filepath.Join(dir, "old"), path); err != nil {
		t.Fatal(err)
	}
	if _, err = harness.Prepare(dir, choices, "127.0.0.1:8080", nil); err == nil {
		t.Fatal("retargeted saved path accepted")
	}
}
