package setup_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/setup"
)

func TestLiveMetalBudgetDoesNotUseTheOldPercentageEnvelope(t *testing.T) {
	observed := facts(32)
	observed.MetalDeviceMemoryMiB, observed.WiredLimitMiB = 24576, 24576
	observed.MetalDeviceMemorySource, observed.WiredLimitSource = machine.MetalDeviceSourceLive, budget.WiredSourceMetal
	assessment, err := setup.Assess(selectedLock(t, catalogDocument(t), largeLocal), observed)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Budget.DeviceMiB != 24576 || assessment.Budget.WiredSource != budget.WiredSourceMetal || assessment.WiredMemory != nil {
		t.Fatalf("actual Metal budget still produced the old 26 GiB recommendation: %+v", assessment)
	}
}

func TestLiveMetalRecommendationAllowsForGrowingFractionEnvelope(t *testing.T) {
	for _, tc := range []struct {
		name        string
		utilization float64
		canIncrease bool
	}{
		{"adequate bounded increase", 0.85, true},
		{"fraction consumes the margin", 0.95, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := catalogDocument(t)
			p := d.Profiles[largeLocal]
			p.GPUMemoryUtilization = tc.utilization
			d.Profiles[largeLocal] = p
			locked := selectedLock(t, d, largeLocal)
			observed := facts(32)
			observed.MetalDeviceMemoryMiB, observed.WiredLimitMiB = 18432, 18432
			observed.MetalDeviceMemorySource, observed.WiredLimitSource = machine.MetalDeviceSourceLive, budget.WiredSourceMetal
			before, err := setup.Assess(locked, observed)
			if err != nil || before.WiredMemory == nil {
				t.Fatalf("missing tight-budget advice: %+v, %v", before, err)
			}
			advice := before.WiredMemory
			if (advice.SuggestedLimitMiB > 0) != tc.canIncrease {
				t.Fatalf("wrong remedy: %+v", advice)
			}
			text := strings.Join(advice.Section().Lines, "\n")
			if !strings.Contains(text, "recommendation policy, not a macOS maximum") || !strings.Contains(text, "Metal recommended working set") {
				t.Fatalf("policy confused with a detected hardware cap: %s", text)
			}
			if !tc.canIncrease {
				return
			}
			if advice.SuggestedLimitMiB > 27852 {
				t.Fatalf("recommendation exceeds physical reserve policy: %+v", advice)
			}
			if !strings.Contains(text, "temper machine facts") || !strings.Contains(text, "wired_limit_source: live-metal") {
				t.Fatalf("instructions verify only the configured override: %s", text)
			}
			// Both values change when the newly reported Metal budget changes.
			observed.MetalDeviceMemoryMiB, observed.WiredLimitMiB = advice.SuggestedLimitMiB, advice.SuggestedLimitMiB
			after, err := setup.Assess(locked, observed)
			if err != nil || after.WiredMemory != nil || len(after.Refusals) != 0 {
				t.Fatalf("following advice asks for another increase: %+v, %v", after, err)
			}
		})
	}
}

func TestTightWiredBudgetRecommendsBoundedManualIncrease(t *testing.T) {
	d := catalogDocument(t)
	locked := selectedLock(t, d, largeLocal)
	for _, tc := range []struct {
		name, source string
		limit        int64
		canPrepare   bool
	}{
		{"fits narrowly under live limit", budget.WiredSourceLive, 24576, true},
		{"exceeds predicted default", budget.WiredSourcePredicted, 32768 * 65 / 100, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			machine := facts(32)
			machine.WiredLimitMiB, machine.WiredLimitSource = tc.limit, tc.source
			plan, err := setup.Build(filepath.Join(t.TempDir(), "root"), machine, 100<<30, []catalog.Lock{locked}, "")
			if err != nil {
				t.Fatal(err)
			}
			advice := plan.Modes[0].WiredMemory
			if advice == nil || advice.SuggestedLimitMiB != 26624 || advice.LimitMiB != tc.limit || advice.LimitSource != tc.source {
				t.Fatalf("wired advice = %+v", advice)
			}
			if plan.CanPrepare != tc.canPrepare {
				t.Fatalf("advice changed admission: can_prepare=%v", plan.CanPrepare)
			}
			sections := plan.Sections()
			if !sections[0].Warning || !strings.Contains(sections[0].Title, "Memory budget is tight") {
				t.Fatal("manual memory guidance does not lead review")
			}
			text := strings.Join(plan.Lines(), "\n")
			for _, want := range []string{"Increase the wired-memory limit", "[manual]", "sysctl -n iogpu.wired_limit_mb", "sudo sysctl iogpu.wired_limit_mb=26624", "administrator password", "rerun the same Temper setup command", "until reboot", "restore the value you noted", "6.00 GiB", "does not establish runtime fit"} {
				if !strings.Contains(text, want) {
					t.Errorf("review omitted %q", want)
				}
			}
			if tc.source == budget.WiredSourcePredicted && !strings.Contains(text, "predicted macOS default; verify") {
				t.Fatal("estimated default was presented as a live setting")
			}
			machine.WiredLimitMiB, machine.WiredLimitSource = advice.SuggestedLimitMiB, budget.WiredSourceLive
			reassessed, err := setup.Assess(locked, machine)
			if err != nil || reassessed.WiredMemory != nil || len(reassessed.Refusals) != 0 {
				t.Fatalf("fresh facts did not clear resolved warning: %+v, %v", reassessed, err)
			}
			if reassessed.Contexts["qwen3.8-27b-q4xl-mtp"].Status != "unknown" || reassessed.Lock.Digests.Profile != locked.Digests.Profile {
				t.Fatal("wired advice changed the execution or invented context evidence")
			}
		})
	}
}

func TestWiredAdvicePreservesRoomForMacOSOnSmallMachines(t *testing.T) {
	d := catalogDocument(t)
	for _, tc := range []struct {
		name, profile string
		gib           int64
		canPrepare    bool
	}{
		{"compact on 8 GiB", compactLocal, 8, true},
		{"oversized model on 16 GiB", largeLocal, 16, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			locked := selectedLock(t, d, tc.profile)
			plan, err := setup.Build(filepath.Join(t.TempDir(), "root"), facts(tc.gib), 100<<30, []catalog.Lock{locked}, "")
			if err != nil {
				t.Fatal(err)
			}
			advice := plan.Modes[0].WiredMemory
			if advice == nil || advice.SuggestedLimitMiB != 0 || plan.CanPrepare != tc.canPrepare {
				t.Fatalf("unsafe recommendation or changed admission: %+v", plan)
			}
			text := strings.Join(advice.Section().Lines, "\n")
			if strings.Contains(text, "sudo sysctl") || !strings.Contains(text, "reserving memory for macOS") || !strings.Contains(text, "smaller model") {
				t.Fatalf("incorrect remedy for limited physical RAM: %s", text)
			}
		})
	}
}

func TestWiredAdviceIncludesOnDemandUtilityAndSkipsCPUOnly(t *testing.T) {
	d := catalogDocument(t)
	p := d.Profiles[compactUtility]
	p.Bindings[0].Layout = "qwen3.8-27b-q4xl-mtp"
	d.Profiles[compactUtility] = p
	machine := facts(32)
	machine.WiredLimitMiB, machine.WiredLimitSource = 18000, budget.WiredSourceLive
	utility, err := setup.Assess(selectedLock(t, d, compactUtility), machine)
	if err != nil {
		t.Fatal(err)
	}
	if utility.Budget.Status != budget.StatusNotApplicable || utility.WiredMemory == nil || utility.WiredMemory.SuggestedLimitMiB != 20480 {
		t.Fatalf("on-demand helper escaped memory guidance: %+v", utility)
	}
	// Same large model and low limit, now explicitly CPU-only.
	l := d.Layouts[p.Bindings[0].Layout]
	l.EngineConfig.GPULayers = 0
	d.Layouts[p.Bindings[0].Layout] = l
	cpu, err := setup.Assess(selectedLock(t, d, compactUtility), machine)
	if err != nil || cpu.WiredMemory != nil {
		t.Fatalf("CPU-only setup received a GPU sysctl recommendation: %+v, %v", cpu, err)
	}
}

func TestComfortableWiredBudgetHasNoAdvisory(t *testing.T) {
	d := catalogDocument(t)
	for _, profile := range []string{compactLocal, compactUtility, largeLocal} {
		t.Run(profile, func(t *testing.T) {
			machine := facts(32)
			machine.WiredLimitMiB, machine.WiredLimitSource = 28672, budget.WiredSourceLive
			assessment, err := setup.Assess(selectedLock(t, d, profile), machine)
			if err != nil || assessment.WiredMemory != nil {
				t.Fatalf("comfortable configuration received needless advice: %+v, %v", assessment, err)
			}
		})
	}
}

func TestAlternativeModesRecommendOneLimitForTheLargestBudget(t *testing.T) {
	d := catalogDocument(t)
	p := d.Profiles[compactUtility]
	p.Bindings[0].Layout = "qwen3.8-27b-q4xl-mtp"
	d.Profiles[compactUtility] = p
	machine := facts(32)
	machine.WiredLimitMiB, machine.WiredLimitSource = 13000, budget.WiredSourceLive
	plan, err := setup.Build(filepath.Join(t.TempDir(), "root"), machine, 100<<30, []catalog.Lock{
		selectedLock(t, d, compactUtility), selectedLock(t, d, largeLocal),
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Modes[0].WiredMemory == nil || plan.Modes[1].WiredMemory == nil {
		t.Fatal("both modes should have a tight budget")
	}
	text := strings.Join(plan.Lines(), "\n")
	if !strings.Contains(text, "Memory budget is tight · local, utility") || strings.Count(text, "sudo sysctl iogpu.wired_limit_mb=26624") != 1 || strings.Contains(text, "sudo sysctl iogpu.wired_limit_mb=20480") {
		t.Fatalf("alternative modes supplied conflicting commands: %s", text)
	}
}

func TestRecommendedLimitClearsPercentageMarginOnLargerMachine(t *testing.T) {
	locked := selectedLock(t, catalogDocument(t), largeLocal)
	machine := facts(64)
	before, err := setup.Assess(locked, machine)
	if err != nil || before.WiredMemory == nil || before.WiredMemory.SuggestedLimitMiB == 0 {
		t.Fatalf("missing recommendation: %+v, %v", before, err)
	}
	machine.WiredLimitMiB, machine.WiredLimitSource = before.WiredMemory.SuggestedLimitMiB, budget.WiredSourceLive
	after, err := setup.Assess(locked, machine)
	if err != nil || after.WiredMemory != nil {
		t.Fatalf("following advice immediately requests another increase: %+v, %v", after, err)
	}
}
