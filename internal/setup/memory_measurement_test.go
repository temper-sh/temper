package setup_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/setup"
)

func withMemoryMeasurement(l catalog.Lock, facts machine.Facts, peak int64) catalog.Lock {
	p := l.Records.Presets[l.Preset]
	loaded := peak - (1 << 20)
	p.Profiles[0].Memory = &catalog.MemoryMeasurement{
		ObservedOn: "2026-10-20", Source: "https://example.com/assessment",
		Workload: "Fixture workload", Counter: "engine-physical-footprint",
		LoadedBytes: &loaded, PeakBytes: peak, Limitations: "Fixture observation; engine only.",
		Machine: catalog.MemoryMachine{Chip: facts.Chip, PhysicalMemoryBytes: facts.PhysicalMemoryBytes,
			MetalMemoryMiB: facts.MetalDeviceMemoryMiB, OS: "macOS 26.6"},
		Execution: l.MemoryExecution(),
	}
	l.Records.Presets[l.Preset] = p
	return l
}

func TestApplicableMeasurementReplacesEstimateThroughoutThePlan(t *testing.T) {
	// The synthetic geometry would exceed this machine's budget. The matching
	// measured peak must own the decision, not max(measurement, estimate).
	observed := facts(8)
	l := withMemoryMeasurement(attentionLock(t, 65536, catalog.ComputeGPU), observed, 4<<30)
	raw, err := catalog.MarshalLock(l)
	if err != nil {
		t.Fatal(err)
	}
	l, err = catalog.ParseLock(raw)
	if err != nil {
		t.Fatal(err)
	}
	got, err := setup.Assess(l, observed)
	if err != nil || len(got.Refusals) != 0 || got.Memory.Basis != "measured" || got.MemoryBytes != 4<<30 ||
		got.Memory.KVCacheBytes != nil || got.Memory.Measurement == nil {
		t.Fatal("measurement did not replace estimate", got.Memory, got.Refusals, err)
	}
	c := setup.EmptyConfiguration()
	c.Presets["coding"] = setup.Preset{Name: "Coding", Lock: l}
	c.Layouts["work"] = setup.Layout{Name: "Work", Presets: []string{"coding"}, Startup: []string{"coding"}, IdleSeconds: 60}
	plan, err := setup.BuildConfiguration(t.TempDir(), observed, 100<<30, c, setup.Material{})
	if err != nil || !plan.CanPrepare || !plan.Layouts[0].AllFit || plan.Layouts[0].StartupBytes != 4<<30 {
		t.Fatal("layout did not use measured admission", plan.Refusals, err)
	}
	text := strings.Join(plan.Lines(), "\n")
	wire, _ := json.Marshal(plan)
	if !strings.Contains(text, "Measured memory: 4.00 GiB peak") || !strings.Contains(text, "Fixture observation") ||
		!strings.Contains(string(wire), `"basis":"measured"`) {
		t.Fatal("measurement lost from review or JSON", text)
	}
}

func TestMeasurementMismatchFallsBackWithTheReason(t *testing.T) {
	for _, scenario := range []string{"context", "engine", "engine version", "weights", "compute", "cache", "machine", "RAM", "Metal budget"} {
		t.Run(scenario, func(t *testing.T) {
			observed := facts(16)
			l := withMemoryMeasurement(attentionLock(t, 4096, catalog.ComputeGPU), observed, 4<<30)
			p := l.Records.Presets[l.Preset]
			switch scenario {
			case "context":
				p.Profiles[0].ContextWindowTokens = 8192
			case "engine":
				e := l.Records.Engines[p.Engine]
				e.Supply.Release.Artifact.SHA256 = strings.Repeat("a", 64)
				l.Records.Engines[p.Engine] = e
			case "engine version":
				e := l.Records.Engines[p.Engine]
				oldVersion := e.Supply.Release.Version
				e.Supply.Release.Version = "b99999"
				e.Supply.Release.Artifact.Locator = strings.ReplaceAll(e.Supply.Release.Artifact.Locator, oldVersion, "b99999")
				l.Records.Engines[p.Engine] = e
			case "weights":
				a := l.Records.Artifacts[p.Artifact]
				a.Files[0].SHA256 = strings.Repeat("b", 64)
				l.Records.Artifacts[p.Artifact] = a
			case "compute":
				p.Compute = catalog.ComputeCPU
			case "cache":
				p.Profiles[0].EngineConfig.KVCache = "q8"
			case "machine":
				observed.Chip = "Apple other chip"
			case "RAM":
				observed.PhysicalMemoryBytes *= 2
			case "Metal budget":
				observed.MetalDeviceMemoryMiB /= 2
				observed.WiredLimitMiB = observed.MetalDeviceMemoryMiB
			}
			l.Records.Presets[l.Preset] = p
			got, err := setup.Assess(l, observed)
			if err != nil || got.Memory.Basis != "estimated" || got.Memory.Measurement != nil ||
				!strings.Contains(got.Memory.FallbackReason, "different") {
				t.Fatal("stale measurement used or fallback unexplained", got.Memory, err)
			}
		})
	}
}

func TestMeasurementPeakStillRefusesAnOverBudgetSelection(t *testing.T) {
	observed := facts(8)
	l := withMemoryMeasurement(attentionLock(t, 4096, catalog.ComputeCPU), observed, 8<<30)
	got, err := setup.Assess(l, observed)
	if err != nil || got.Memory.Status != "exceeds-budget" || got.Memory.Basis != "measured" || len(got.Refusals) == 0 {
		t.Fatal("measured peak bypassed the memory allowance", got.Memory, got.Refusals, err)
	}
}
