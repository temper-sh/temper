package setupcmd

import (
	"context"

	"errors"
	"io"
	"os"
	"path/filepath"

	"testing"

	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/machine"

	"github.com/temper-sh/temper/internal/software"
)

const (
	compactLayout = "qwen3.5-4b-q4km-off"
	largePatch    = "froggeric-qwen38-v22.5"
	GiB           = int64(1 << 30)
	fixtureDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func guidedCatalog(t *testing.T) catalog.Document {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "catalog", "guided-setup.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func eightGiBFacts() machine.Facts {
	return machine.Facts{
		Schema:        machine.FactsSchemaV1,
		Target:        software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6"},
		HardwareModel: "Mac14,2", Chip: "Apple M2", OSBuild: "24G90",
		PhysicalMemoryBytes: 8 * GiB, MetalDeviceMemoryMiB: 8192 * 81 / 100,
		MetalDeviceMemorySource: machine.MetalDeviceSourcePredicted,
		WiredLimitMiB:           6500, WiredLimitSource: budget.WiredSourceLive,
	}
}

func fixtureCommand(t *testing.T) Command {
	t.Helper()
	doc := guidedCatalog(t)
	return Command{
		Detect: func(context.Context) (machine.Facts, error) { return eightGiBFacts(), nil },
		Disk:   func(string) (int64, error) { return 40 * GiB, nil },
		Home:   func() (string, error) { return t.TempDir(), nil },
		Catalog: func(context.Context, string, string) (catalog.Document, string, error) {
			return doc, fixtureDigest, nil
		},
		Resolve: func(ctx context.Context, d catalog.Document, s string, choice string) (catalog.Document, error) {
			if choice != "recorded" {
				return catalog.Document{}, errors.New("unexpected moving software request")
			}
			return catalog.ResolveSoftware(ctx, d, s, choice, nil)
		},
		Dispatch: func(context.Context, []string, io.Writer, io.Writer) int { t.Error("unexpected preparation"); return 1 },
	}
}
