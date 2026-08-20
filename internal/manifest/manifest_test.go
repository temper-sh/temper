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
