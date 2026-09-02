package render_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/render"
	"github.com/temper-sh/temper/internal/runtimeconfig"
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
				"--parallel 1\n      -c 24576\n      --ctx-checkpoints 16\n      --cache-ram 0\n      -fa on",
				"-ctk q8_0 -ctv q8_0",
				"-b 512\n      -ub 512",
				"--spec-type draft-mtp\n      --spec-draft-n-max 3",
				"--chat-template-file '/temper/artifacts/layouts/coder/dc0bf13d38e973845242c06b8a09ac72b014da701f66e925d2f716ad8a417166/patches/sharp/template.jinja'",
				"--reasoning off",
			},
			notContain: []string{"--chat-template-kwargs"},
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

func TestBuildV2RendersEverySelectedEngineThroughItsAdapter(t *testing.T) {
	document, err := manifest.Parse([]byte(renderV2Manifest))
	if err != nil {
		t.Fatal(err)
	}
	files := []lockfile.File{
		{Name: "config.json", SHA256: strings.Repeat("a", 64)},
		{Name: "model.safetensors", SHA256: strings.Repeat("b", 64)},
		{Name: "tokenizer.json", SHA256: strings.Repeat("c", 64)},
	}
	locked := lockfile.Document{Schema: lockfile.SchemaV1, Entries: map[string]lockfile.Entry{}}
	for _, item := range []struct{ id, revision string }{{"mlx", "d"}, {"rapid", "e"}, {"vllm", "f"}} {
		locked.Entries[item.id] = lockfile.Entry{
			Repo: document.Layouts[item.id].Model.Repo, Revision: strings.Repeat(item.revision, 40),
			Files: append([]lockfile.File(nil), files...), Resolved: "2026-09-02",
		}
	}
	bundle, err := render.Build(render.Inputs{
		Manifest: document, Lock: locked, Mode: "large", Root: "/temper",
	})
	if err != nil {
		t.Fatal(err)
	}
	config := string(artifact(t, bundle, "llama-swap/config.yaml"))
	for _, wanted := range []string{
		"rapid-mlx --no-telemetry serve",
		"checkEndpoint: \"/health/ready\"", "HF_HUB_OFFLINE=1",
		"mlx_vlm.server --host 127.0.0.1 --port ${PORT}", "useModelName:",
		"vllm serve", "VLLM_METAL_USE_PAGED_ATTENTION=1",
	} {
		if !strings.Contains(config, wanted) {
			t.Errorf("config does not contain %q:\n%s", wanted, config)
		}
	}
	if strings.Contains(config, "mlx_vlm.server --max-model-len") {
		t.Fatalf("MLX-VLM received a false context flag:\n%s", config)
	}

	requirements, err := runtimeconfig.Parse(artifact(t, bundle, "runtime/requirements.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantPackages := []string{"llama-swap", "mlx-vlm", "rapid-mlx", "vllm-metal"}
	for _, packageID := range wantPackages {
		found := false
		for _, requirement := range requirements.Requirements {
			found = found || requirement.Package == packageID
		}
		if !found {
			t.Errorf("runtime requirements omit %q: %#v", packageID, requirements.Requirements)
		}
	}
	models := string(artifact(t, bundle, "pi/models.json"))
	if !strings.Contains(models, `"input": [`) || !strings.Contains(models, `"image"`) {
		t.Fatalf("Pi did not receive selected image modality:\n%s", models)
	}
	settings := string(artifact(t, bundle, "pi/settings.json"))
	if !strings.Contains(settings, `"defaultModel": "rapid"`) || !strings.Contains(settings, `"reserveTokens": 16384`) {
		t.Fatalf("Pi did not derive settings from explicit v2 foreground:\n%s", settings)
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
	if len(bundle.Artifacts) != 2 {
		t.Fatalf("off rendered %d artifacts, want config and runtime requirements", len(bundle.Artifacts))
	}
	config := string(artifact(t, bundle, "llama-swap/config.yaml"))
	if !strings.Contains(config, "models: {}") {
		t.Fatalf("off config is not empty:\n%s", config)
	}
	if strings.Contains(config, "routing:") || strings.Contains(config, "hooks:") {
		t.Fatalf("off config contains live-world sections:\n%s", config)
	}
	requirements := string(artifact(t, bundle, "runtime/requirements.json"))
	if !strings.Contains(requirements, `"package": "llama-swap"`) || strings.Contains(requirements, `"package": "llama-cpp"`) {
		t.Fatalf("off runtime requirements are not router-only:\n%s", requirements)
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

const renderV2Manifest = `schema: temper-manifest/v2
defaults: {ttl: 1800, gpu_memory_utilization: 0.9}
layouts:
  rapid:
    display_name: Rapid large
    model:
      repo: org/rapid
      format: mlx-safetensors
      files: [config.json, model.safetensors, tokenizer.json]
    engine: rapid-mlx
    interface: chat-completions
    modalities: [text]
    window: 131072
    max_tokens: 8192
    thinking: off
    speculation: {method: none}
    rapid_mlx:
      max_num_seqs: 2
      max_concurrent_requests: 2
      prefill_batch_size: 1
      completion_batch_size: 2
      gpu_memory_utilization: 0.95
      prefix_cache: on
      cache_memory_mib: 0
      kv_cache_dtype: bf16
      pflash: off
  mlx:
    display_name: MLX vision
    model:
      repo: org/mlx
      format: mlx-safetensors
      files: [config.json, model.safetensors, tokenizer.json]
    engine: mlx-vlm
    interface: chat-completions
    modalities: [text, image]
    window: 65536
    max_tokens: 4096
    thinking: on
    speculation: {method: none}
    mlx_vlm:
      max_num_seqs: 1
      prefill_step_size: 2048
      vision_cache_size: 8
  vllm:
    display_name: vLLM large
    model:
      repo: org/vllm
      format: safetensors
      files: [config.json, model.safetensors, tokenizer.json]
    engine: vllm-metal
    interface: chat-completions
    modalities: [text]
    window: 65536
    max_tokens: 4096
    thinking: off
    speculation: {method: none}
    vllm_metal:
      max_num_seqs: 1
      max_num_batched_tokens: 4096
      gpu_memory_utilization: 0.9
      kv_cache_dtype: bfloat16
      prefix_cache: off
tools: {}
modes:
  large:
    foreground: rapid
    harnesses: [pi]
    members:
      resident:
        - {layout: rapid}
      on_demand:
        - {layout: mlx}
        - {layout: vllm}
`

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
    llama: {parallel: 1, flash_attention: on, batch: 512, ubatch: 512, spec_type: draft-mtp, spec_draft_n_max: 3, context_checkpoints: 16, prompt_cache_ram_mib: 0}
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
