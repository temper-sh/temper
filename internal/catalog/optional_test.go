package catalog_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/render/engine"
	"github.com/temper-sh/temper/internal/testfixture"
)

func TestOptionalForkClearSurvivesCompilationAndLockRoundTrip(t *testing.T) {
	d := testfixture.MixedEngineCatalog(t)
	p := d.Presets[mtp]
	p.RequestDefaults.ReasoningBudgetTokens = new(512)
	p.Profiles[0].EngineConfig.Controls.CheckpointMinStep = new(1024)
	d.Presets[mtp] = p
	changes := `{"request_defaults":{"reasoning_budget_tokens":null,"sampling":{"temperature":null,"top_p":null,"top_k":0,"min_p":null,"repeat_penalty":null,"presence_penalty":null}},"engine_config":{"context_checkpoints":null,"prompt_cache_ram_mib":null,"controls":{"reasoning_effort":null,"checkpoint_min_step":null}}}`
	u := userPreset(t, "mine", fmt.Sprintf(`{"schema":"temper-preset/v1","origin":%q,"changes":%s}`, mtp, changes))
	lock, _ := compileUser(t, d, "mine", u)
	raw, err := catalog.MarshalLock(lock)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := catalog.ParseLock(raw)
	if err != nil {
		t.Fatal(err)
	}
	got := restored.Records.Presets["mine"]
	c := got.Config()
	if got.RequestDefaults.Sampling.Temperature != nil || got.RequestDefaults.Sampling.TopP != nil || got.RequestDefaults.ReasoningBudgetTokens != nil || c.ContextCheckpoints != nil || c.PromptCacheRAMMiB != nil || c.Controls.ReasoningEffort != "" || c.Controls.CheckpointMinStep != nil {
		t.Fatal("cleared fields were restored", got)
	}
	if got.RequestDefaults.Sampling.TopK == nil || *got.RequestDefaults.Sampling.TopK != 0 {
		t.Fatal("lost explicit zero")
	}
	projection, err := restored.Projections()
	if err != nil {
		t.Fatal(err)
	}
	if projection.Manifest.Layouts["mine"].Llama.ContextCheckpoints != nil {
		t.Fatal("projection invented checkpoint zero")
	}
	inherited, _ := compileUser(t, d, "mine", userPreset(t, "mine", fmt.Sprintf(`{"schema":"temper-preset/v1","origin":%q}`, mtp)))
	if inherited.Records.Presets["mine"].Config().Controls.ReasoningEffort != p.Config().Controls.ReasoningEffort {
		t.Fatal("omitted change did not inherit")
	}
}

func TestReasoningCapabilityRejectsContradictoryDefaults(t *testing.T) {
	for _, tc := range []struct{ capability, reasoning string }{{"always", "off"}, {"none", "on"}, {"invented", "on"}} {
		d := testfixture.MixedEngineCatalog(t)
		p := d.Presets[mtp]
		p.ReasoningCapability, p.RequestDefaults.Reasoning = tc.capability, tc.reasoning
		d.Presets[mtp] = p
		if _, err := catalog.CompilePreset(d, mtp, 0, 0, target); err == nil || !strings.Contains(err.Error(), "reasoning") && !strings.Contains(err.Error(), "thinking") {
			t.Fatalf("accepted %s/%s: %v", tc.capability, tc.reasoning, err)
		}
	}
}

func TestLegacyRerankerZeroSamplingRemainsReadable(t *testing.T) {
	d := testfixture.MixedEngineCatalog(t)
	id := "qwen3-reranker-0.6b-q8-llama"
	p := d.Presets[id]
	p.RequestDefaults.Sampling = engine.SamplingDefaults{Temperature: new(0.0), TopK: new(0), TopP: new(0.0), MinP: new(0.0), RepeatPenalty: new(0.0), PresencePenalty: new(0.0), Seed: new(0)}
	d.Presets[id] = p
	lock, err := catalog.CompilePreset(d, id, 0, 0, target)
	if err != nil {
		t.Fatal("legacy zero generation block rejected", err)
	}
	raw, err := catalog.MarshalLock(lock)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ParseLock(raw); err != nil {
		t.Fatal(err)
	}
	p.RequestDefaults.Sampling.Temperature = new(0.2)
	d.Presets[id] = p
	if _, err := catalog.CompilePreset(d, id, 0, 0, target); err == nil {
		t.Fatal("accepted generation sampling on reranker")
	}
}
