package setup_test

import (
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/setup"
)

// A model that needs more GPU memory than macOS allows is refused in plain
// words with a choice the user can make in the wizard. Temper reads the live
// limit, so an administrator's change applies without Temper suggesting one.
func TestGPUMemoryRefusalNamesTheNeedAndTheLimitWithoutSuggestingSysctl(t *testing.T) {
	locked := selectedLock(t, catalogDocument(t), largeLocal)
	observed := facts(32)
	observed.MetalDeviceMemoryMiB, observed.WiredLimitMiB = 16000, 16000
	got, err := setup.Assess(locked, observed)
	refusal := strings.Join(got.Refusals, "; ")
	if err != nil || !strings.Contains(refusal, "of GPU memory, but macOS lets the GPU use 15.62 GiB on this Mac") || !strings.Contains(refusal, "Choose a smaller model or a shorter Context.") {
		t.Fatalf("GPU refusal is unclear: %q %v", refusal, err)
	}
	for _, jargon := range []string{"sysctl", "wired", "allowance"} {
		if strings.Contains(refusal, jargon) {
			t.Fatalf("refusal uses %q: %s", jargon, refusal)
		}
	}
	observed.MetalDeviceMemoryMiB, observed.WiredLimitMiB = 30000, 30000
	if got, err = setup.Assess(locked, observed); err != nil || len(got.Refusals) != 0 {
		t.Fatalf("a larger live limit did not admit the preset: %v %v", got.Refusals, err)
	}
}

// Tight but sufficient GPU memory is not a problem to report: Review stays
// quiet and preparation is allowed.
func TestTightGPUMemoryProducesNoAdvice(t *testing.T) {
	observed := facts(32)
	observed.MetalDeviceMemoryMiB, observed.WiredLimitMiB = 24*1024, 24*1024
	locked := withMemoryMeasurement(attentionLock(t, 4096, catalog.ComputeGPU), observed, int64(21<<30+128<<20))
	c := setup.EmptyConfiguration()
	c.Presets[locked.Preset] = setup.Preset{Name: "Tight fixture", Lock: locked}
	plan, err := setup.BuildConfiguration(t.TempDir(), observed, 100<<30, c, setup.Material{})
	text := strings.Join(plan.Lines(), "\n")
	if err != nil || !plan.CanPrepare || len(plan.Refusals) != 0 {
		t.Fatal("tight but sufficient memory refused preparation", plan.Refusals, err)
	}
	for _, advice := range []string{"sysctl", "headroom", "wired-memory"} {
		if strings.Contains(text, advice) {
			t.Fatalf("plan still advises %q:\n%s", advice, text)
		}
	}
}

func TestCPUOnlyPresetIsChargedAgainstRAMNotGPUMemory(t *testing.T) {
	d := catalogDocument(t)
	p := d.Presets[largeLocal]
	p.Compute = catalog.ComputeCPU
	p.ComputeOptions = []catalog.ComputeDevice{catalog.ComputeCPU}
	d.Presets[largeLocal] = p
	observed := facts(32)
	observed.MetalDeviceMemoryMiB, observed.WiredLimitMiB = 8000, 8000
	got, err := setup.Assess(selectedLock(t, d, largeLocal), observed)
	if err != nil || len(got.Refusals) != 0 {
		t.Fatalf("CPU-only preset charged to GPU memory: %v %v", got.Refusals, err)
	}
	got, err = setup.Assess(selectedLock(t, d, largeLocal), facts(16))
	if err != nil || !strings.Contains(strings.Join(got.Refusals, "; "), "of memory including macOS, but this Mac has 16.00 GiB") {
		t.Fatalf("CPU weights escaped the RAM limit: %v %v", got.Refusals, err)
	}
}
