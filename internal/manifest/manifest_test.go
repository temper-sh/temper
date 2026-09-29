package manifest_test

import (
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/manifest"
)

func TestParseRejectsUnknownFields(t *testing.T) {
	_, err := manifest.Parse([]byte(`schema: temper-manifest/v2
defaults: {ttl: 1800, gpu_memory_utilization: 0.85}
layouts:
  coder:
    display_name: Coder
    model: {repo: org/Coder, format: gguf, files: [coder.gguf]}
    engine: llama-server
    interface: chat-completions
    modalities: [text]
    speculation: {method: none}
    window: 8192
    max_tokens: 2048
    thinking: off
    mystery: silently-ignore-me
    llama: {kv: q8, parallel: 1, flash_attention: on, batch: 512, ubatch: 512}
modes:
  local:
    foreground: coder
    members:
      resident: [{layout: coder}]
`))
	if err == nil || !strings.Contains(err.Error(), "field mystery not found") {
		t.Fatalf("Parse error = %v, want strict unknown-field refusal", err)
	}
}

func TestParseRejectsInvalidFlashAttentionAndOnDemandPreload(t *testing.T) {
	_, err := manifest.Parse([]byte(`schema: temper-manifest/v2
defaults: {ttl: 1800, gpu_memory_utilization: 0.85}
layouts:
  coder:
    display_name: Coder
    model: {repo: org/Coder, format: gguf, files: [coder.gguf]}
    engine: llama-server
    interface: chat-completions
    modalities: [text]
    speculation: {method: none}
    window: 8192
    max_tokens: 2048
    thinking: off
    llama: {kv: q8, parallel: 1, flash_attention: sometimes, batch: 512, ubatch: 512}
modes:
  local:
    foreground: coder
    members:
      resident: [{layout: coder}]
      on_demand: [{layout: coder, preload: true}]
`))
	if err == nil {
		t.Fatal("Parse succeeded, want invalid flash-attention and preload")
	}
	for _, wanted := range []string{"must be on, off or auto", "preload is allowed only on a resident member"} {
		if !strings.Contains(err.Error(), wanted) {
			t.Errorf("error does not contain %q: %v", wanted, err)
		}
	}
}

func TestParseRejectsUnpinnedPatchSource(t *testing.T) {
	_, err := manifest.Parse([]byte(`schema: temper-manifest/v2
defaults: {ttl: 1800, gpu_memory_utilization: 0.85}
patches:
  template: {source: hf://org/template, file: template.jinja}
layouts:
  coder:
    display_name: Coder
    model: {repo: org/Coder, format: gguf, files: [coder.gguf]}
    engine: llama-server
    interface: chat-completions
    modalities: [text]
    speculation: {method: none}
    window: 8192
    max_tokens: 2048
    thinking: off
    chat_template: template
    llama: {kv: q8, parallel: 1, flash_attention: on, batch: 512, ubatch: 512}
modes:
  local:
    foreground: coder
    members:
      resident: [{layout: coder}]
`))
	if err == nil || !strings.Contains(err.Error(), "must include repository and file path") {
		t.Fatalf("Parse error = %v, want pinned patch-source refusal", err)
	}
}

func TestParsePreservesExplicitZeroPromptCacheRAM(t *testing.T) {
	document, err := manifest.Parse([]byte(`schema: temper-manifest/v2
defaults: {ttl: 1800, gpu_memory_utilization: 0.85}
layouts:
  coder:
    display_name: Coder
    model: {repo: org/Coder, format: gguf, files: [coder.gguf]}
    engine: llama-server
    interface: chat-completions
    modalities: [text]
    speculation: {method: none}
    window: 8192
    max_tokens: 2048
    thinking: off
    llama:
      kv: q8
      parallel: 1
      flash_attention: on
      batch: 512
      ubatch: 512
      context_checkpoints: 16
      prompt_cache_ram_mib: 0
modes:
  off: {foreground: none}
`))
	if err != nil {
		t.Fatal(err)
	}
	tuning := document.Layouts["coder"].Llama
	if tuning.ContextCheckpoints == nil || *tuning.ContextCheckpoints != 16 {
		t.Fatalf("context_checkpoints = %v, want 16", tuning.ContextCheckpoints)
	}
	if tuning.PromptCacheRAMMiB == nil || *tuning.PromptCacheRAMMiB != 0 {
		t.Fatalf("prompt_cache_ram_mib = %v, want explicit zero", tuning.PromptCacheRAMMiB)
	}
}

func TestParseRejectsNegativeLlamaCacheTuning(t *testing.T) {
	_, err := manifest.Parse([]byte(`schema: temper-manifest/v2
defaults: {ttl: 1800, gpu_memory_utilization: 0.85}
layouts:
  coder:
    display_name: Coder
    model: {repo: org/Coder, format: gguf, files: [coder.gguf]}
    engine: llama-server
    interface: chat-completions
    modalities: [text]
    speculation: {method: none}
    window: 8192
    max_tokens: 2048
    thinking: off
    llama:
      kv: q8
      parallel: 1
      flash_attention: on
      batch: 512
      ubatch: 512
      context_checkpoints: -1
      prompt_cache_ram_mib: -1
modes:
  off: {foreground: none}
`))
	if err == nil {
		t.Fatal("Parse succeeded, want negative llama cache tuning refusal")
	}
	for _, wanted := range []string{"llama.context_checkpoints must be zero or greater", "llama.prompt_cache_ram_mib must be zero or greater"} {
		if !strings.Contains(err.Error(), wanted) {
			t.Errorf("error does not contain %q: %v", wanted, err)
		}
	}
}

func TestParseManifestV2AdmitsOnlyMatchedTypedEngineVariants(t *testing.T) {
	document, err := manifest.Parse([]byte(validV2Manifest))
	if err != nil {
		t.Fatal(err)
	}
	if document.Schema != manifest.SchemaV2 || document.Layouts["rapid"].RapidMLX == nil || document.Layouts["mlx"].MLXVLM == nil || document.Layouts["vllm"].VLLMMetal == nil {
		t.Fatalf("parsed v2 manifest lost engine variants: %#v", document.Layouts)
	}
	mode := document.Modes["large"]
	if got := document.ForegroundLayout(mode); got != "rapid" {
		t.Fatalf("ForegroundLayout() = %q, want rapid", got)
	}
	if got := document.Layouts["rapid"].ModelFiles(); len(got) != 3 || got[0] != "config.json" {
		t.Fatalf("ModelFiles() = %#v", got)
	}
}

func TestManifestV2ExternalForegroundRequiresAnUnambiguousHelperMode(t *testing.T) {
	document, err := manifest.Parse([]byte(validV2Manifest))
	if err != nil {
		t.Fatal(err)
	}
	mode := document.Modes["large"]
	mode.Foreground = ""
	mode.ExternalForeground = true
	document.Modes["large"] = mode
	if err := document.Validate(); err != nil {
		t.Fatalf("external helper mode refused: %v", err)
	}
	if got := document.ForegroundLayout(mode); got != "" {
		t.Fatalf("external mode foreground layout = %q", got)
	}
	mode.Foreground = "rapid"
	document.Modes["large"] = mode
	if err := document.Validate(); err == nil || !strings.Contains(err.Error(), "cannot name a local foreground") {
		t.Fatalf("external mode with local route error = %v", err)
	}
	mode.Foreground = ""
	mode.Members = manifest.Members{}
	document.Modes["large"] = mode
	if err := document.Validate(); err == nil || !strings.Contains(err.Error(), "needs a helper member") {
		t.Fatalf("external mode without helper error = %v", err)
	}
}

func TestManifestV2RefusesAmbiguousOrLegacyEngineState(t *testing.T) {
	tests := []struct {
		name   string
		old    string
		new    string
		wanted string
	}{
		{name: "legacy file", old: "      files: [config.json, model.safetensors, tokenizer.json]", new: "      file: model.safetensors", wanted: "field file not found"},
		{name: "unsorted snapshot", old: "      files: [config.json, model.safetensors, tokenizer.json]", new: "      files: [tokenizer.json, model.safetensors, config.json]", wanted: "model.files must be sorted"},
		{name: "legacy role", old: "    engine: rapid-mlx", new: "    engine: rapid-mlx\n    role: coder", wanted: "field role not found"},
		{name: "mismatched tuning", old: "    engine: rapid-mlx", new: "    engine: mlx-vlm", wanted: "does not match its tuning block"},
		{name: "preferred foreground alias", old: "        - {layout: rapid}", new: "        - {layout: rapid, preferred: true}", wanted: "field preferred not found"},
		{name: "on-demand foreground", old: "      resident:\n        - {layout: rapid}\n      on_demand:", new: "      resident: []\n      on_demand:\n        - {layout: rapid}", wanted: "must be resident"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := strings.Replace(validV2Manifest, test.old, test.new, 1)
			_, err := manifest.Parse([]byte(data))
			if err == nil || !strings.Contains(err.Error(), test.wanted) {
				t.Fatalf("Parse() error = %v, want %q", err, test.wanted)
			}
		})
	}
}

func TestManifestV2RefusesVLLMMetalMTPWithPrefixCaching(t *testing.T) {
	data := strings.Replace(validV2Manifest,
		"    speculation: {method: none}\n    vllm_metal:",
		"    speculation: {method: mtp, max_tokens: 2}\n    vllm_metal:", 1)
	data = strings.Replace(data, "      prefix_cache: off", "      prefix_cache: on", 1)

	_, err := manifest.Parse([]byte(data))
	if err == nil || !strings.Contains(err.Error(), "MTP with prefix caching is refused") {
		t.Fatalf("Parse() error = %v, want an MTP/prefix-cache refusal", err)
	}
}

const validV2Manifest = `schema: temper-manifest/v2
defaults:
  ttl: 1800
  gpu_memory_utilization: 0.9
patches: {}
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
  off:
    foreground: none
    members:
      resident: []
      on_demand: []
`

func TestManifestRejectsRetiredSchema(t *testing.T) {
	raw := strings.Replace(validV2Manifest, "temper-manifest/v2", "temper-manifest/v1", 1)
	if _, err := manifest.Parse([]byte(raw)); err == nil {
		t.Fatal("retired manifest accepted")
	}
}
