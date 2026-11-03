package setup_test

import (
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/setup"
)

func TestPresetMemoryAdviceUsesEffectiveBudgetAndClearsAfterManualIncrease(t *testing.T) {
	d := catalogDocument(t)
	locked := selectedLock(t, d, largeLocal)
	observed := facts(32)
	observed.MetalDeviceMemorySource, observed.WiredLimitSource = machine.MetalDeviceSourceLive, budget.WiredSourceMetal
	observed.MetalDeviceMemoryMiB, observed.WiredLimitMiB = 16000, 16000
	before, err := setup.Assess(locked, observed)
	if err != nil || before.WiredMemory == nil || len(before.Refusals) == 0 {
		t.Fatalf("tight budget: %+v %v", before, err)
	}
	advice := before.WiredMemory
	if advice.SuggestedLimitMiB <= 16000 || advice.SuggestedLimitMiB > 27852 {
		t.Fatalf("unsafe recommendation: %+v", advice)
	}
	text := strings.Join(advice.Section().Lines, "\n")
	if advice.Section().Title != "Wired-memory budget exceeded" || strings.Contains(text, "does not block preparation") || strings.Contains(text, "Optional:") {
		t.Fatalf("actual refusal presented as optional headroom: %s", text)
	}
	for _, want := range []string{"[manual]", "temper machine facts", "Metal recommended working set"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	observed.MetalDeviceMemoryMiB, observed.WiredLimitMiB = advice.SuggestedLimitMiB, advice.SuggestedLimitMiB
	after, err := setup.Assess(locked, observed)
	if err != nil || after.WiredMemory != nil || len(after.Refusals) != 0 {
		t.Fatalf("remedy did not clear refusal: %+v %v", after, err)
	}
	if !catalog.SameExecution(after.Lock, locked) {
		t.Fatal("advice altered execution")
	}
}

func TestWithinBudgetHeadroomAdviceIsOptionalAndKeepsTheManualCommand(t *testing.T) {
	observed := facts(32)
	observed.MetalDeviceMemoryMiB, observed.WiredLimitMiB = 24*1024, 24*1024
	peak := int64(21<<30 + 128<<20)
	locked := withMemoryMeasurement(attentionLock(t, 4096, catalog.ComputeGPU), observed, peak)
	got, err := setup.Assess(locked, observed)
	if err != nil || len(got.Refusals) != 0 || got.WiredMemory == nil {
		t.Fatalf("within-budget selection: %+v %v", got, err)
	}
	advice := got.WiredMemory
	if got.MemoryBytes != peak || advice.RequiredMiB != 22656 || advice.LimitMiB != 24576 || advice.SuggestedLimitMiB != 25600 {
		t.Fatalf("headroom arithmetic changed: memory=%d advice=%+v", got.MemoryBytes, advice)
	}
	section := advice.Section()
	text := strings.Join(section.Lines, "\n")
	if section.Title != "Optional wired-memory headroom" {
		t.Fatal(section.Title)
	}
	for _, want := range []string{"Optional:", "24.00 GiB", "25.00 GiB", "does not block preparation", "[manual] Optional:", "sysctl -n iogpu.wired_limit_mb", "sudo sysctl iogpu.wired_limit_mb=25600", "temper machine facts", "until reboot", "restore the value", "not a measured runtime requirement", "does not establish runtime fit"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "before running this setup") || strings.Index(text, "[manual] Optional:") > strings.Index(text, "sudo sysctl") {
		t.Fatal("manual increase still appears mandatory", text)
	}
	c := setup.EmptyConfiguration()
	c.Presets[locked.Preset] = setup.Preset{Name: "Within-budget fixture", Lock: locked}
	plan, err := setup.BuildConfiguration(t.TempDir(), observed, 100<<30, c, setup.Material{})
	if err != nil || !plan.CanPrepare || len(plan.Refusals) != 0 || !strings.Contains(strings.Join(plan.Lines(), "\n"), text) {
		t.Fatal("review lost optional advice or changed admission", plan.Refusals, err)
	}
}

func TestWithinBudgetHeadroomWithoutASafeIncreaseRemainsOptional(t *testing.T) {
	a := setup.WiredMemoryAdvice{RequiredMiB: 26 * 1024, LimitMiB: 27 * 1024, PhysicalMiB: 32 * 1024}
	section := a.Section()
	text := strings.Join(section.Lines, "\n")
	if section.Title != "Optional wired-memory headroom" || !strings.Contains(text, "does not block preparation") || strings.Contains(text, "sudo sysctl") || strings.Contains(text, "Choose a smaller model") {
		t.Fatal("insufficient reserve turned headroom advice into a requirement", section)
	}
}

func TestCPUOnlyPresetDoesNotReceiveWiredMemoryAdvice(t *testing.T) {
	d := catalogDocument(t)
	p := d.Presets[largeLocal]
	p.Compute = catalog.ComputeCPU
	p.ComputeOptions = []catalog.ComputeDevice{catalog.ComputeCPU}
	d.Presets[largeLocal] = p
	observed := facts(32)
	observed.MetalDeviceMemoryMiB, observed.WiredLimitMiB = 8000, 8000
	observed.WiredLimitSource = budget.WiredSourceMetal
	got, err := setup.Assess(selectedLock(t, d, largeLocal), observed)
	if err != nil || got.WiredMemory != nil || len(got.Refusals) != 0 {
		t.Fatalf("CPU-only advice: %+v %v", got, err)
	}
	got, err = setup.Assess(selectedLock(t, d, largeLocal), facts(16))
	if err != nil || len(got.Refusals) == 0 || !strings.Contains(strings.Join(got.Refusals, "; "), "this machine has") {
		t.Fatalf("CPU weights escaped the physical RAM limit: %+v %v", got, err)
	}
}
func TestOversizedPresetKeepsPhysicalMemoryReserve(t *testing.T) {
	got, err := setup.Assess(selectedLock(t, catalogDocument(t), largeLocal), facts(16))
	if err != nil || got.WiredMemory == nil || got.WiredMemory.SuggestedLimitMiB != 0 || len(got.Refusals) == 0 {
		t.Fatalf("unsafe recommendation: %+v %v", got, err)
	}
	if strings.Contains(strings.Join(got.WiredMemory.Section().Lines, "\n"), "sudo sysctl") {
		t.Fatal("impossible remedy offered")
	}
}
