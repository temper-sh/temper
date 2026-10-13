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
