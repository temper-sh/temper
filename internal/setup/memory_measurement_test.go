package setup_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/testfixture"
)

func TestKapitsaMeasurementUsesWorkloadFootprintWithoutTreatingItAsWiredAllocation(t *testing.T) {
	d := testfixture.KapitsaCatalog(t)
	l := selectedLock(t, d, "qwen3.8-27b-mlx4-kapitsa")
	f := facts(32)
	f.Chip = "Apple M5"
	f.MetalDeviceMemoryMiB = 24576
	f.WiredLimitMiB = 24576
	p, err := setup.Assess(l, f)
	if err != nil || len(p.Refusals) != 0 || p.Memory.Basis != "measured" || p.MemoryBytes != 25761248456 || p.WiredMemory != nil {
		t.Fatalf("measured memory misrepresented: %+v %v", p.Memory, err)
	}
	c := setup.EmptyConfiguration()
	c.Presets[l.Preset] = setup.Preset{Name: "Kapitsa", Lock: l}
	c.Layouts["default"] = setup.Layout{Name: "Default", Presets: []string{l.Preset}, Startup: []string{l.Preset}, IdleSeconds: 300}
	composition, err := setup.AssessLayout(c, "default", f)
	if err != nil || len(composition.Refusals) != 0 || !composition.MemoryIncomplete || composition.AllFit || composition.StartupBytes != p.MemoryBytes {
		t.Fatal("layout inferred a wired requirement or fit from process footprint", composition, err)
	}
	f.Chip = "Apple M1"
	f.Target.DistributionVersion = "14.0"
	p, err = setup.Assess(l, f)
	if err != nil || p.Memory.Basis != "estimated" || p.Memory.EngineLimitBytes != 24<<30 || !strings.Contains(strings.Join(p.Memory.Unknown, " "), "ANE service") {
		t.Fatal(p.Memory, err)
	}
}

func TestMoEPlacementDoesNotInventPhysicalSavingsOrWiredAdvice(t *testing.T) {
	l := attentionLock(t, 4096, catalog.ComputeGPU)
	observed := facts(16)
	before, err := setup.Assess(l, observed)
	if err != nil {
		t.Fatal(err)
	}
	p := l.Records.Presets[l.Preset]
	p.Profiles[0].EngineConfig.Controls.CPUMoELayers = 4
	l.Records.Presets[l.Preset] = p
	after, err := setup.Assess(l, observed)
	if err != nil {
		t.Fatal(err)
	}
	if after.MemoryBytes != before.MemoryBytes || after.WiredMemory != nil || !strings.Contains(strings.Join(after.Memory.Unknown, " "), "CPU/GPU") {
		t.Fatalf("mixed placement invented memory savings or GPU fit: before=%+v after=%+v", before, after)
	}
	observed.WiredLimitMiB = 1
	observed.MetalDeviceMemoryMiB = 1
	c := setup.EmptyConfiguration()
	c.Presets[l.Preset] = setup.Preset{Name: "Mixed placement", Lock: l}
	c.Layouts["default"] = setup.Layout{Name: "Default", Presets: []string{l.Preset}, Startup: []string{l.Preset}, IdleSeconds: 300}
	composition, err := setup.AssessLayout(c, "default", observed)
	if err != nil || len(composition.Refusals) != 0 || !composition.MemoryIncomplete || composition.AllFit || composition.StartupBytes != before.MemoryBytes {
		t.Fatal("layout invented physical savings or a wired requirement", composition, err)
	}
}

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
	for _, scenario := range []string{"context", "engine", "engine version", "weights", "compute", "cache", "MoE placement", "machine", "RAM", "Metal budget"} {
		t.Run(scenario, func(t *testing.T) {
			observed := facts(16)
			l := withMemoryMeasurement(attentionLock(t, 4096, catalog.ComputeGPU), observed, 4<<30)
			p := l.Records.Presets[l.Preset]
			switch scenario {
			case "MoE placement":
				p.Profiles[0].EngineConfig.Controls.CPUMoELayers = 4
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

// A measurement taken under a lowered Metal budget (here the 24 GiB
// iogpu.wired_limit_mb override) still describes the footprint at the larger
// macOS default (25,559 MiB on an M5 with 32 GiB) when the engine cannot
// allocate beyond the recorded budget. Engines that size from the budget, or
// whose behavior is not characterized, still need the exact recorded budget.
func TestMeasurementFromASmallerMetalBudgetAppliesOnlyWhenTheEngineCannotGrow(t *testing.T) {
	const recordedMiB, liveMiB = 24576, 25559
	splashWithCeiling := func(t *testing.T, ceiling int64) catalog.Lock {
		l := selectedLock(t, catalogDocument(t), "qwen3.8-27b-q4xl-splash")
		p := l.Records.Presets[l.Preset]
		splash := *p.Profiles[0].EngineConfig.Splash
		splash.MaxMemoryBytes = ceiling
		p.Profiles[0].EngineConfig.Splash = &splash
		l.Records.Presets[l.Preset] = p
		return l
	}
	scenarios := []struct {
		name    string
		lock    func(*testing.T) catalog.Lock
		applies bool
	}{
		{"llama-server without fit", func(t *testing.T) catalog.Lock { return attentionLock(t, 4096, catalog.ComputeGPU) }, true},
		{"llama-server with fit", func(t *testing.T) catalog.Lock {
			l := attentionLock(t, 4096, catalog.ComputeGPU)
			p := l.Records.Presets[l.Preset]
			p.Profiles[0].EngineConfig.Controls.Fit = "on"
			l.Records.Presets[l.Preset] = p
			return l
		}, false},
		{"Splash ceiling at the recorded budget", func(t *testing.T) catalog.Lock { return splashWithCeiling(t, recordedMiB<<20) }, true},
		{"Splash ceiling above the recorded budget", func(t *testing.T) catalog.Lock { return splashWithCeiling(t, (recordedMiB+1)<<20) }, false},
		{"Kapitsa ceiling within the recorded budget", func(t *testing.T) catalog.Lock {
			return selectedLock(t, testfixture.KapitsaCatalog(t), "qwen3.8-27b-mlx4-kapitsa")
		}, true},
		{"Rapid MLX GPU fraction", func(t *testing.T) catalog.Lock { return selectedLock(t, studyCatalog(t), "rapid-mlx") }, false},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			recorded := facts(32)
			recorded.Chip = "Apple M5"
			recorded.MetalDeviceMemoryMiB, recorded.WiredLimitMiB = recordedMiB, recordedMiB
			l := withMemoryMeasurement(scenario.lock(t), recorded, 8<<30)
			live := recorded
			live.MetalDeviceMemoryMiB, live.WiredLimitMiB = liveMiB, liveMiB
			got, err := setup.Assess(l, live)
			if err != nil {
				t.Fatal(err)
			}
			if scenario.applies {
				if got.Memory.Basis != "measured" || got.MemoryBytes != 8<<30 {
					t.Fatal("bounded footprint lost its measurement at a larger Metal budget", got.Memory)
				}
				return
			}
			if got.Memory.Basis != "estimated" || !strings.Contains(got.Memory.FallbackReason, "Metal memory budget") {
				t.Fatal("measurement applied to an engine that can grow with the Metal budget", got.Memory)
			}
		})
	}
}

func TestMeasurementFromALargerMetalBudgetDoesNotApply(t *testing.T) {
	recorded := facts(32)
	l := withMemoryMeasurement(attentionLock(t, 4096, catalog.ComputeGPU), recorded, 8<<30)
	live := recorded
	live.MetalDeviceMemoryMiB--
	live.WiredLimitMiB = live.MetalDeviceMemoryMiB
	got, err := setup.Assess(l, live)
	if err != nil || got.Memory.Basis != "estimated" || !strings.Contains(got.Memory.FallbackReason, "Metal memory budget") {
		t.Fatal("measurement applied below its recorded Metal budget", got.Memory, err)
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
