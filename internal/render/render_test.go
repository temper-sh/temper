package render_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/render"
	"gopkg.in/yaml.v3"
)

func TestBuildMapsManifestFactsToConcreteConfig(t *testing.T) {
	bundle := buildFixture(t, "local")

	tests := []struct {
		name       string
		artifact   string
		contains   []string
		notContain []string
	}{
		{
			name:     "lock selects an exact offline model artifact",
			artifact: "llama-swap/config.yaml",
			contains: []string{
				"-m '/temper/artifacts/layouts/coder/dc0bf13d38e973845242c06b8a09ac72b014da701f66e925d2f716ad8a417166/model/coder.gguf'",
				"--offline",
				"--no-mmproj",
			},
			notContain: []string{"-hf org/Coder"},
		},
		{
			name:     "coder layout owns engine and prompt tuning",
			artifact: "llama-swap/config.yaml",
			contains: []string{
				"--parallel 1\n      -c 24576\n      -fa on",
				"-ctk q8_0 -ctv q8_0",
				"-b 512\n      -ub 512",
				"--spec-type draft-mtp\n      --spec-draft-n-max 3",
				"--chat-template-file '/temper/artifacts/layouts/coder/dc0bf13d38e973845242c06b8a09ac72b014da701f66e925d2f716ad8a417166/patches/sharp/template.jinja'",
				`--chat-template-kwargs '{"enable_thinking":false}'`,
			},
		},
		{
			name:     "member placement owns ttl offload and routing",
			artifact: "llama-swap/config.yaml",
			contains: []string{
				"ttl: 7200",
				"--reranking",
				"-ngl 0",
				`members: ["coder"]`,
				`members: ["rerank-0.6b"]`,
			},
		},
		{
			name:     "Pi sees coders but not specialists",
			artifact: "pi/models.json",
			contains: []string{
				`"id": "coder"`,
				`"contextWindow": 24576`,
				`"remote"`,
				`"ownerKey": "preserved"`,
			},
			notContain: []string{`"id": "rerank-0.6b"`, `"id": "stale"`},
		},
		{
			name:     "Pi compaction derives from the resident coder",
			artifact: "pi/settings.json",
			contains: []string{
				`"defaultModel": "coder"`,
				`"keepRecentTokens": 7168`,
				`"reserveTokens": 3072`,
				`"theme": "dark"`,
				`"ownerKey": "preserved"`,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := string(artifact(t, bundle, test.artifact))
			for _, wanted := range test.contains {
				if !strings.Contains(actual, wanted) {
					t.Errorf("%s does not contain %q\n%s", test.artifact, wanted, actual)
				}
			}
			for _, unwanted := range test.notContain {
				if strings.Contains(actual, unwanted) {
					t.Errorf("%s unexpectedly contains %q\n%s", test.artifact, unwanted, actual)
				}
			}
		})
	}
}

func TestBuildIsDeterministic(t *testing.T) {
	first := buildFixture(t, "local")
	second := buildFixture(t, "local")
	if first.Digest() != second.Digest() {
		t.Fatalf("same inputs produced digests %s and %s", first.Digest(), second.Digest())
	}
	for index := range first.Artifacts {
		if first.Artifacts[index].Path != second.Artifacts[index].Path || string(first.Artifacts[index].Data) != string(second.Artifacts[index].Data) {
			t.Fatalf("artifact %d differs between identical renders", index)
		}
	}
}

func TestLlamaSwapConfigGolden(t *testing.T) {
	bundle := buildFixture(t, "local")
	want, err := os.ReadFile("testdata/local-config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	got := artifact(t, bundle, "llama-swap/config.yaml")
	if string(got) != string(want) {
		t.Fatalf("llama-swap config changed\n--- want\n%s--- got\n%s", want, got)
	}
}

func TestArtifactsAreSyntacticallyValid(t *testing.T) {
	bundle := buildFixture(t, "local")
	for _, item := range bundle.Artifacts {
		switch {
		case strings.HasSuffix(item.Path, ".json"):
			if !json.Valid(item.Data) {
				t.Errorf("%s is not valid JSON", item.Path)
			}
		case strings.HasSuffix(item.Path, ".yaml"):
			var value any
			if err := yaml.Unmarshal(item.Data, &value); err != nil {
				t.Errorf("%s is not valid YAML: %v", item.Path, err)
			}
		}
	}
}

func TestOffModeRendersAnEmptyWorld(t *testing.T) {
	bundle := buildFixture(t, "off")
	if len(bundle.Artifacts) != 1 {
		t.Fatalf("off rendered %d artifacts, want only llama-swap config", len(bundle.Artifacts))
	}
	config := string(artifact(t, bundle, "llama-swap/config.yaml"))
	if !strings.Contains(config, "models: {}") {
		t.Fatalf("off config is not empty:\n%s", config)
	}
	if strings.Contains(config, "routing:") || strings.Contains(config, "hooks:") {
		t.Fatalf("off config contains live-world sections:\n%s", config)
	}
}

func TestPreferredDoesNotImplyPreload(t *testing.T) {
	document := parseManifest(t)
	mode := document.Modes["local"]
	mode.Members.Resident[0].Preload = false
	document.Modes["local"] = mode
	bundle, err := render.Build(render.Inputs{
		Manifest: document,
		Lock:     parseLock(t),
		Mode:     "local",
		Root:     "/temper",
	})
	if err != nil {
		t.Fatal(err)
	}
	config := string(artifact(t, bundle, "llama-swap/config.yaml"))
	if strings.Contains(config, "hooks:") {
		t.Fatalf("preferred member was implicitly preloaded:\n%s", config)
	}
	settings := string(artifact(t, bundle, "pi/settings.json"))
	if !strings.Contains(settings, `"defaultModel": "coder"`) {
		t.Fatalf("preferred member was not selected by Pi:\n%s", settings)
	}
}

func TestPiCompactionDisabledIsPreserved(t *testing.T) {
	document := parseManifest(t)
	locked := parseLock(t)
	bundle, err := render.Build(render.Inputs{
		Manifest:       document,
		Lock:           locked,
		Mode:           "local",
		Root:           "/temper",
		PiSettingsBase: []byte(`{"compaction":{"enabled":false,"reserveTokens":99},"theme":"dark"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(artifact(t, bundle, "pi/settings.json"), &settings); err != nil {
		t.Fatal(err)
	}
	compaction := settings["compaction"].(map[string]any)
	if compaction["reserveTokens"] != float64(99) {
		t.Fatalf("disabled compaction was overwritten: %#v", compaction)
	}
}

func TestBuildRefusesManifestLockDrift(t *testing.T) {
	document := parseManifest(t)
	locked := parseLock(t)
	entry := locked.Entries["coder"]
	entry.Repo = "org/Moved"
	locked.Entries["coder"] = entry
	_, err := render.Build(render.Inputs{Manifest: document, Lock: locked, Mode: "local", Root: "/temper"})
	if err == nil || !strings.Contains(err.Error(), "repo drift") {
		t.Fatalf("Build error = %v, want repo drift refusal", err)
	}
}

func buildFixture(t *testing.T, mode string) render.Bundle {
	t.Helper()
	bundle, err := render.Build(render.Inputs{
		Manifest:       parseManifest(t),
		Lock:           parseLock(t),
		Mode:           mode,
		Root:           "/temper",
		PiModelsBase:   []byte(`{"providers":{"local":{"models":[{"id":"stale"}]},"remote":{"baseUrl":"https://example.invalid"}},"ownerKey":"preserved"}`),
		PiSettingsBase: []byte(`{"theme":"dark","compaction":{"enabled":true,"ownerKey":"preserved"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func artifact(t *testing.T, bundle render.Bundle, path string) []byte {
	t.Helper()
	for _, artifact := range bundle.Artifacts {
		if artifact.Path == path {
			return artifact.Data
		}
	}
	t.Fatalf("artifact %q not found", path)
	return nil
}

func parseManifest(t *testing.T) manifest.Document {
	t.Helper()
	document, err := manifest.Parse([]byte(manifestFixture))
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func parseLock(t *testing.T) lockfile.Document {
	t.Helper()
	document, err := lockfile.Parse([]byte(lockFixture))
	if err != nil {
		t.Fatal(err)
	}
	return document
}

const manifestFixture = `schema: temper-manifest/v1
defaults:
  ttl: 1800
  gpu_memory_utilization: 0.85
patches:
  sharp:
    source: hf://org/sharp@ffffffffffffffffffffffffffffffffffffffff/template.jinja
    file: template.jinja
layouts:
  coder:
    display_name: Coder
    model: {repo: org/Coder, file: coder.gguf}
    engine: llama-server
    role: coder
    window: 24576
    max_tokens: 4096
    kv: q8
    thinking: off
    chat_template: sharp
    llama: {parallel: 1, flash_attention: on, batch: 512, ubatch: 512, spec_type: draft-mtp, spec_draft_n_max: 3}
  rerank-0.6b:
    display_name: Reranker
    model: {repo: org/Reranker, file: reranker.gguf}
    engine: llama-server
    role: rerank
    window: 4096
    llama: {parallel: 1, flash_attention: auto, batch: 256, ubatch: 256}
tools:
  search:
    source: github://org/search
    needs: [rerank]
modes:
  local:
    foreground: local
    tools: [search]
    harnesses: [pi]
    members:
      resident:
        - {layout: coder, ttl: 7200, preferred: true, preload: true}
      on_demand:
        - {layout: rerank-0.6b, ttl: 120, ngl: 0}
  off:
    foreground: none
`

const lockFixture = `schema: temper-lock/v1
entries:
  coder:
    repo: org/Coder
    revision: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    files:
      - {name: coder.gguf, sha256: cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc}
    patches:
      - {name: sharp, sha256: eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee}
    resolved: 2026-08-19
  rerank-0.6b:
    repo: org/Reranker
    revision: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
    files:
      - {name: reranker.gguf, sha256: dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd}
    resolved: 2026-08-19
`
