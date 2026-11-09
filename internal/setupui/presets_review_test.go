package setupui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/setup"
)

func reviewText(m *PresetModel) string {
	body := screenBody{focusStart: -1}
	m.reviewContent(&body, 76)
	text := strings.ReplaceAll(ansi.Strip(strings.Join(body.blocks, "\n")), "│", " ")
	return strings.Join(strings.Fields(text), " ")
}

func TestCompactReviewKeepsSeparateDiskBudgetsAndRefusalReasons(t *testing.T) {
	m := populatedPresetEditor(t)
	m.plan.CanPrepare = false
	m.plan.Refusals = []string{"The model cache is full."}
	m.plan.RemainingDownloadBytes = 3 << 30
	m.plan.RemainingDiskBytes, m.plan.FreeDiskBytes = 1<<30, 9<<30
	m.plan.HFCache = &setup.CachePlan{Root: "/private/cache", RemainingDiskBytes: 3 << 30, FreeDiskBytes: 2 << 30}
	m.plan.Root = "/private/configuration"
	m.plan.Presets = []setup.PresetPlan{{Lock: m.input.Presets[0].Lock, MemoryBytes: 4 << 30, RuntimeDiskEstimateBytes: 6 << 30}}
	text := reviewText(m)
	for _, want := range []string{"The model cache is full.", "Download 3.00 GiB", "Disk 1.00 GiB needed · 9.00 GiB free", "Model cache 3.00 GiB needed · 2.00 GiB free", "Splash builds a cache of about 6.00 GiB", "2 GiB free"} {
		if !strings.Contains(text, want) {
			t.Errorf("review omitted %q: %s", want, text)
		}
	}
	for _, absent := range []string{"/private/", "Context / out", "Idle unload", "Files shared by several presets", "Models:", "Layouts:", "Memory estimate"} {
		if strings.Contains(text, absent) {
			t.Errorf("review retained settings or implementation details %q: %s", absent, text)
		}
	}
	if m.actionAvailable(0) {
		t.Fatal("compact review bypassed the installation refusal")
	}
}

// Review answers what will be installed. Measurement records (counter,
// workload, limits) mean nothing without their study, so they stay on the
// preset screens; memory problems still reach Review as warnings.
func TestReviewLeavesMemoryEvidenceToThePresetScreens(t *testing.T) {
	m := populatedPresetEditor(t)
	m.plan.Presets = []setup.PresetPlan{{Lock: m.input.Presets[0].Lock, MemoryBytes: 4 << 30, Memory: setup.MemoryEstimate{
		Basis: "measured", Measurement: &catalog.MemoryMeasurement{
			PeakBytes: 4 << 30, Counter: "phys_footprint",
			Workload: "8192-token prompt", Limitations: "Excludes the router process.",
		},
	}}}
	text := reviewText(m)
	for _, absent := range []string{"peak", "phys_footprint", "8192-token prompt", "Excludes the router process", "Largest memory allowance"} {
		if strings.Contains(text, absent) {
			t.Errorf("review shows measurement detail %q: %s", absent, text)
		}
	}
}
