package engine_test

import (
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/render/engine"
)

func TestExternalDraftUsesExplicitPlacementAndIndependentCache(t *testing.T) {
	request := validLlamaServerRequest()
	request.Speculation, request.SpeculativeTokens = "dflash2", 15
	request.DraftModelPath = "/models/assistant's draft.gguf"
	cpu := 0
	request.NGL = &cpu
	command, err := engine.Build(request)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(command.Lines(), "\n")
	for _, want := range []string{"--spec-type draft-dflash", "--spec-draft-n-max 15", `--model-draft '/models/assistant'\''s draft.gguf'`, "--gpu-layers-draft 0", "--cache-type-k-draft f16", "--cache-type-v-draft f16", "-ctk q8_0 -ctv q8_0"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}

func TestExternalDraftRefusesSilentFallback(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*engine.Request)
	}{
		{"missing material", func(r *engine.Request) { r.DraftModelPath = "" }},
		{"missing placement", func(r *engine.Request) { r.NGL = nil }},
		{"no speculation", func(r *engine.Request) { r.Speculation, r.SpeculativeTokens = "none", 0 }},
		{"engine-owned block on llama", func(r *engine.Request) { r.SpeculativeTokens = 0 }},
		{"oversized block", func(r *engine.Request) { r.SpeculativeTokens = 16 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := validLlamaServerRequest()
			r.Speculation, r.SpeculativeTokens, r.DraftModelPath = "dflash2", 7, "/draft.gguf"
			gpu := 99
			r.NGL = &gpu
			tc.change(&r)
			if _, err := engine.Build(r); err == nil {
				t.Fatal("incomplete draft request accepted")
			}
		})
	}
}
