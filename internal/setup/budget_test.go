package setup_test

import (
	"testing"

	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/setup"
)

const budgetManifest = `schema: temper-manifest/v1
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
    llama: {kv: q8, parallel: 1, flash_attention: on, batch: 512, ubatch: 512}
modes:
  local:
    foreground: coder
    members:
      resident: [{layout: coder}]
`

func TestPredictBudgetReportsAResidentEnvelopeAboveTheWall(t *testing.T) {
	document, err := manifest.Parse([]byte(budgetManifest))
	if err != nil {
		t.Fatal(err)
	}
	prediction, err := setup.PredictBudget(document, document.Modes["local"], budget.Machine{
		PhysicalMiB: 2048, DeviceMiB: 1658, WiredLimitMiB: 1500, WiredSource: budget.WiredSourceMetal,
	}, map[string]int64{"coder": 7})
	if err != nil {
		t.Fatal(err)
	}
	if prediction.Status != budget.StatusExceeded || prediction.RequiredMiB != 2433 || prediction.Holder != "coder" || !prediction.HasSuggestion {
		t.Fatalf("prediction = %#v", prediction)
	}
}

func TestPredictBudgetMarksACPUOnlyForegroundNotApplicable(t *testing.T) {
	document, err := manifest.Parse([]byte(budgetManifest))
	if err != nil {
		t.Fatal(err)
	}
	mode := document.Modes["local"]
	ngl := 0
	mode.Members.Resident[0].NGL = &ngl
	prediction, err := setup.PredictBudget(document, mode, budget.Machine{
		PhysicalMiB: 32768, DeviceMiB: 26542, WiredLimitMiB: 24576, WiredSource: budget.WiredSourceMetal,
	}, map[string]int64{"coder": 7})
	if err != nil || prediction.Status != budget.StatusNotApplicable {
		t.Fatalf("prediction = %#v, %v", prediction, err)
	}
}
