package manifest_test

import (
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/manifest"
)

func TestParseRejectsUnknownFields(t *testing.T) {
	_, err := manifest.Parse([]byte(`schema: temper-manifest/v1
defaults: {ttl: 1800, gpu_memory_utilization: 0.85}
layouts:
  coder:
    display_name: Coder
    model: {repo: org/Coder, file: coder.gguf}
    engine: llama-server
    role: coder
    window: 8192
    max_tokens: 2048
    kv: q8
    thinking: off
    mystery: silently-ignore-me
    llama: {parallel: 1, flash_attention: on, batch: 512, ubatch: 512}
modes:
  local:
    foreground: local
    members:
      resident: [{layout: coder, preferred: true}]
`))
	if err == nil || !strings.Contains(err.Error(), "field mystery not found") {
		t.Fatalf("Parse error = %v, want strict unknown-field refusal", err)
	}
}

func TestParseReportsRelatedModeProblemsTogether(t *testing.T) {
	_, err := manifest.Parse([]byte(`schema: temper-manifest/v1
defaults: {ttl: 1800, gpu_memory_utilization: 0.85}
layouts:
  coder:
    display_name: Coder
    model: {repo: org/Coder, file: coder.gguf}
    engine: llama-server
    role: coder
    window: 8192
    max_tokens: 2048
    kv: q8
    thinking: off
    llama: {parallel: 1, flash_attention: on, batch: 512, ubatch: 512}
modes:
  local:
    foreground: local
    members:
      on_demand: [{layout: coder, preferred: true}]
`))
	if err == nil {
		t.Fatal("Parse succeeded, want invalid mode")
	}
	for _, wanted := range []string{"preferred is allowed only on a resident coder", "needs a resident coder", "exactly one preferred resident coder"} {
		if !strings.Contains(err.Error(), wanted) {
			t.Errorf("error does not contain %q: %v", wanted, err)
		}
	}
}

func TestParseRejectsInvalidFlashAttentionAndOnDemandPreload(t *testing.T) {
	_, err := manifest.Parse([]byte(`schema: temper-manifest/v1
defaults: {ttl: 1800, gpu_memory_utilization: 0.85}
layouts:
  coder:
    display_name: Coder
    model: {repo: org/Coder, file: coder.gguf}
    engine: llama-server
    role: coder
    window: 8192
    max_tokens: 2048
    kv: q8
    thinking: off
    llama: {parallel: 1, flash_attention: sometimes, batch: 512, ubatch: 512}
modes:
  local:
    foreground: local
    members:
      resident: [{layout: coder, preferred: true}]
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
	_, err := manifest.Parse([]byte(`schema: temper-manifest/v1
defaults: {ttl: 1800, gpu_memory_utilization: 0.85}
patches:
  template: {source: hf://org/template, file: template.jinja}
layouts:
  coder:
    display_name: Coder
    model: {repo: org/Coder, file: coder.gguf}
    engine: llama-server
    role: coder
    window: 8192
    max_tokens: 2048
    kv: q8
    thinking: off
    chat_template: template
    llama: {parallel: 1, flash_attention: on, batch: 512, ubatch: 512}
modes:
  local:
    foreground: local
    members:
      resident: [{layout: coder, preferred: true}]
`))
	if err == nil || !strings.Contains(err.Error(), "must include repository and file path") {
		t.Fatalf("Parse error = %v, want pinned patch-source refusal", err)
	}
}

func TestParseValidatesEmbeddedMTPAsAPairedCoderSetting(t *testing.T) {
	tests := []struct {
		name string
		role string
		spec string
		want string
	}{
		{name: "maximum without type", role: "coder", spec: "spec_draft_n_max: 3", want: "requires spec_type"},
		{name: "unknown type", role: "coder", spec: "spec_type: magic\n      spec_draft_n_max: 3", want: "is unsupported"},
		{name: "zero maximum", role: "coder", spec: "spec_type: draft-mtp", want: "must be between 1 and 16"},
		{name: "reranker", role: "rerank", spec: "spec_type: draft-mtp\n      spec_draft_n_max: 3", want: "supported only for coder"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			coderOnly := "    max_tokens: 2048\n    kv: q8\n    thinking: off\n"
			if test.role == "rerank" {
				coderOnly = ""
			}
			input := `schema: temper-manifest/v1
defaults: {ttl: 1800, gpu_memory_utilization: 0.85}
layouts:
  layout:
    display_name: Model
    model: {repo: org/Model, file: model.gguf}
    engine: llama-server
    role: ` + test.role + `
    window: 8192
` + coderOnly + `    llama:
      parallel: 1
      flash_attention: on
      batch: 512
      ubatch: 512
      ` + test.spec + `
modes:
  off: {foreground: none}
`
			_, err := manifest.Parse([]byte(input))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Parse error = %v, want %q", err, test.want)
			}
		})
	}
}
