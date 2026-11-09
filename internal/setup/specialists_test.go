package setup_test

import (
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/setup"
)

func TestProjectorCountsTowardMemoryAdmission(t *testing.T) {
	d := catalogDocument(t)
	p := d.Presets["nuextract3-q4km-llama"]
	p.Compute = catalog.ComputeCPU
	d.Presets["nuextract3-q4km-llama"] = p
	for id, size := range map[string]int64{p.Artifact: 2 << 30, p.ProjectorArtifact: 6 << 30} {
		a := d.Artifacts[id]
		a.Files = append([]catalog.File(nil), a.Files...)
		a.Files[0].Bytes = size
		d.Artifacts[id] = a
	}
	plan, err := setup.Assess(selectedLock(t, d, "nuextract3-q4km-llama"), facts(8))
	if err != nil {
		t.Fatal(err)
	}
	if plan.ModelBytes != 8<<30 || !strings.Contains(strings.Join(plan.Refusals, "; "), "needs about 9.00 GiB of memory including macOS") {
		t.Fatalf("projector admission: bytes=%d, refusals=%v", plan.ModelBytes, plan.Refusals)
	}
}
