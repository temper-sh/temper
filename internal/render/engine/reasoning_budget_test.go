package engine_test

import (
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/render/engine"
)

func TestReasoningBudgetCannotSilentlyReachOtherEnginesOrReranking(t *testing.T) {
	for _, family := range []string{engine.RapidMLX, engine.VLLMMetal, engine.Splash, engine.CrispEmbed} {
		t.Run(family, func(t *testing.T) {
			r := validLlamaServerRequest()
			r.Engine = family
			r.ReasoningBudgetTokens = new(1024)
			if _, err := engine.Build(r); err == nil || !strings.Contains(err.Error(), "reasoning_budget_tokens") {
				t.Fatalf("unsupported default was not refused: %v", err)
			}
		})
	}
	r := validLlamaServerRequest()
	r.Interface = engine.InterfaceReranking
	r.ReasoningBudgetTokens = new(0)
	if _, err := engine.Build(r); err == nil || !strings.Contains(err.Error(), "reasoning_budget_tokens") {
		t.Fatalf("reranker budget was not refused: %v", err)
	}
}
