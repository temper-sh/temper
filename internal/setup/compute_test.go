package setup_test

import (
	"os"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/buildinfo"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/setup"
)

func TestLayoutComputeBudgetsSeparateRAMFromGPUAllowance(t *testing.T) {
	for _, tc := range []struct {
		name       string
		devices    []catalog.ComputeDevice
		ram, wired int64
		fit        bool
	}{
		{"two GPU models exceed wired allowance", []catalog.ComputeDevice{catalog.ComputeGPU, catalog.ComputeGPU}, 8, 4, false},
		{"CPU and GPU share physical RAM", []catalog.ComputeDevice{catalog.ComputeCPU, catalog.ComputeGPU}, 8, 4, true},
		{"CPU models need no GPU allowance", []catalog.ComputeDevice{catalog.ComputeCPU, catalog.ComputeCPU}, 8, 1, true},
		{"mixed models exceed physical RAM", []catalog.ComputeDevice{catalog.ComputeCPU, catalog.ComputeGPU}, 6, 4, false},
		{"CPU models exceed physical RAM", []catalog.ComputeDevice{catalog.ComputeCPU, catalog.ComputeCPU}, 6, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := catalogDocument(t)
			p := d.Presets[compactLocal]
			a := d.Artifacts[p.Artifact]
			a.Files = []catalog.File{{Path: "model.gguf", Bytes: 3 << 30, SHA256: strings.Repeat("a", 64)}}
			d.Artifacts[p.Artifact] = a
			c := setup.EmptyConfiguration()
			ids := []string{compactLocal, compactUtility}
			for i, id := range ids {
				p := d.Presets[id]
				p.Compute = tc.devices[i]
				p.ComputeOptions = []catalog.ComputeDevice{catalog.ComputeCPU, catalog.ComputeGPU}
				d.Presets[id] = p
				c.Presets[id] = setup.Preset{Name: id, Lock: selectedLock(t, d, id)}
			}
			c.Layouts["helpers"] = setup.Layout{Name: "Helpers", Presets: ids, Startup: ids, IdleSeconds: 60}
			observed := facts(tc.ram)
			observed.MetalDeviceMemoryMiB, observed.WiredLimitMiB = tc.wired*1024, tc.wired*1024
			plan, err := setup.BuildConfiguration(t.TempDir(), observed, 100<<30, c, setup.Material{})
			if err != nil {
				t.Fatal(err)
			}
			if plan.CanPrepare != tc.fit || plan.Layouts[0].AllFit != tc.fit || plan.Layouts[0].StartupBytes != 6<<30 {
				t.Fatalf("unexpected placement budget: %+v, refusals %v", plan.Layouts[0], plan.Refusals)
			}
		})
	}
}

func TestPythonGPUPresetsAreChargedAgainstWiredMemory(t *testing.T) {
	raw, err := os.ReadFile("../testfixture/testdata/study-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	view, err := catalog.Read(raw, buildinfo.DevVersion)
	if err != nil {
		t.Fatal(err)
	}
	d := view.Document
	for _, id := range []string{"rapid-mlx", "vllm-metal"} {
		t.Run(id, func(t *testing.T) {
			observed := facts(64)
			observed.MetalDeviceMemoryMiB, observed.WiredLimitMiB = 8192, 8192
			plan, err := setup.Assess(selectedLock(t, d, id), observed)
			if err != nil || !strings.Contains(strings.Join(plan.Refusals, "; "), "wired-memory limit") {
				t.Fatalf("GPU weights escaped the wired allowance: %+v %v", plan, err)
			}
		})
	}
}
