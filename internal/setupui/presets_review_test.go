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
	for _, want := range []string{"The model cache is full.", "Downloads · 3.00 GiB", "Disk: 1.00 GiB needed · 9.00 GiB free", "Model cache: 3.00 GiB needed · 2.00 GiB free", "about 6.00 GiB extra disk", "2 GiB free", "Memory estimate: 4.00 GiB accounted", "Total runtime fit unknown"} {
		if !strings.Contains(text, want) {
			t.Errorf("review omitted %q: %s", want, text)
		}
	}
	for _, absent := range []string{"/private/", "Context / out", "Idle unload", "Files shared by several presets"} {
		if strings.Contains(text, absent) {
			t.Errorf("review retained settings or implementation details %q: %s", absent, text)
		}
	}
	if m.actionAvailable(0) {
		t.Fatal("compact review bypassed the installation refusal")
	}
}

func TestCompactReviewRetainsMeasuredCounterWorkloadAndLimits(t *testing.T) {
	for _, loaded := range []*int64{nil, new(int64(3 << 30))} {
		m := populatedPresetEditor(t)
		m.plan.Presets = []setup.PresetPlan{{Lock: m.input.Presets[0].Lock, MemoryBytes: 4 << 30, Memory: setup.MemoryEstimate{
			Basis: "measured", Measurement: &catalog.MemoryMeasurement{
				LoadedBytes: loaded, PeakBytes: 4 << 30, Counter: "phys_footprint",
				Workload: "8192-token prompt", Limitations: "Excludes the router process.",
			},
		}}}
		text := reviewText(m)
		wantLoaded := "loaded not sampled"
		if loaded != nil {
			wantLoaded = "loaded 3.00 GiB"
		}
		for _, want := range []string{"4.00 GiB peak", wantLoaded, "phys_footprint", "8192-token prompt", "Excludes the router process"} {
			if !strings.Contains(text, want) {
				t.Errorf("review lost measurement meaning %q: %s", want, text)
			}
		}
	}
}
