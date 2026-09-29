package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/hfcache"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/software"
)

func TestHFCacheDiskAccounting(t *testing.T) {
	data, err := os.ReadFile("../../catalog/guided-setup.json")
	if err != nil {
		t.Fatal(err)
	}
	document, err := catalog.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	locks := []catalog.Lock{}
	for _, profile := range []string{"qwen3.5-4b-q4km-off"} {
		locked, err := catalog.CompilePreset(document, profile, "", 0, software.Target{OS: "darwin", Arch: "arm64"})
		if err != nil {
			t.Fatal(err)
		}
		locks = append(locks, locked)
	}
	artifact := locks[0].Records.Artifacts["qwen3.5-4b-q4km"]
	model := artifact.Files[0]
	entry := hfcache.Entry{Repo: artifact.Repo, Revision: artifact.Revision, Name: model.Path, SHA256: model.SHA256}
	facts := machine.Facts{
		Schema: machine.FactsSchemaV1, Target: software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "26.6"},
		HardwareModel: "MacTest,1", Chip: "Apple test chip", OSBuild: "25G1",
		PhysicalMemoryBytes: 32 << 30, MetalDeviceMemoryMiB: 32768 * 81 / 100, WiredLimitMiB: 32768 * 65 / 100,
		MetalDeviceMemorySource: machine.MetalDeviceSourcePredicted, WiredLimitSource: budget.WiredSourcePredicted,
	}
	root := filepath.Join(t.TempDir(), "temper")
	fresh, err := build(root, facts, 100<<30, locks, Material{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, cacheSource string
		cached, shared    bool
		cacheFree         int64
		diskCredit        int64
		copyBytes         int64
		refuse            bool
	}{
		{"cached same disk", "huggingface", true, true, 0, model.Bytes, 0, false},
		{"cached other disk", "huggingface", true, false, 0, 0, model.Bytes, false},
		{"missing same disk", "", false, true, 0, 0, 0, false},
		{"missing other disk", "", false, false, 100 << 30, 0, model.Bytes, false},
		{"cache disk full", "", false, false, model.Bytes - 1, 0, model.Bytes, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			material := Material{root: root, hfCache: &CachePlan{Root: "/shared/hub", SharedFilesystem: tc.shared, FreeDiskBytes: tc.cacheFree}}
			if tc.cached {
				material.hfModels = map[hfcache.Entry]cachedModel{entry: {copy: !tc.shared}}
			}
			plan, err := build(root, facts, 100<<30, locks, material)
			if err != nil {
				t.Fatal(err)
			}
			transfer, cacheDisk := fresh.DownloadBytes, model.Bytes
			if tc.cached {
				transfer -= model.Bytes
				cacheDisk = 0
			}
			if plan.RemainingDownloadBytes != transfer || plan.RemainingDiskBytes != fresh.FreshDiskBytes-tc.diskCredit || plan.HFCache.RemainingDiskBytes != cacheDisk || plan.HFCache.CopyBytes != tc.copyBytes {
				t.Fatalf("wrong transfer or disk accounting: %+v cache=%+v", plan, plan.HFCache)
			}
			if plan.CanPrepare == tc.refuse || tc.refuse && !strings.Contains(strings.Join(plan.Refusals, " "), "Hugging Face cache needs") {
				t.Fatalf("cache capacity refusal: %v", plan.Refusals)
			}
			if material.hfCache.RemainingDiskBytes != 0 || material.hfCache.CopyBytes != 0 {
				t.Fatal("planning modified observed material")
			}
			for _, file := range plan.Downloads {
				if file.Kind == "model" && file.Cache != tc.cacheSource {
					t.Fatalf("incorrect model cache status: %+v", file)
				}
			}
		})
	}
}
