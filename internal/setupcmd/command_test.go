package setupcmd

import (
	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/preset"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"

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
	largePatch    = "froggeric-qwen38"
	GiB           = int64(1 << 30)
)

func guidedCatalog(t *testing.T) catalog.Document {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "testfixture", "testdata", "catalog.json"))
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
		PhysicalMemoryBytes: 8 * GiB, MetalDeviceMemoryMiB: 6500,
		MetalDeviceMemorySource: machine.MetalDeviceSourceLive,
		WiredLimitMiB:           6500, WiredLimitSource: budget.WiredSourceMetal,
	}
}

func fixtureCommand(t *testing.T) Command {
	t.Helper()
	doc := guidedCatalog(t)
	return Command{
		Detect: func(context.Context) (machine.Facts, error) { return eightGiBFacts(), nil },
		Disk:   func(string) (int64, error) { return 40 * GiB, nil },
		Home:   func() (string, error) { return t.TempDir(), nil },
		Catalog: func(context.Context, string, string, bool) (catalog.View, error) {
			return catalog.View{Document: doc}, nil
		},
		Newest: func(context.Context, string, bool) (catalog.View, error) {
			return catalog.View{Document: doc}, nil
		},
		Resolve: func(ctx context.Context, d catalog.Document, s string, choice string) (catalog.Document, []catalog.Resolution, error) {
			if choice != "stable" {
				return catalog.Document{}, nil, errors.New("unexpected moving software request")
			}
			return catalog.ResolveSoftware(ctx, d, s, choice, nil, false)
		},
		Effects: preset.Effects{Software: unexpectedSoftware{t}, Fetch: func(context.Context, string, string, manifest.Document, lockfile.Document) error {
			t.Error("unexpected model fetch")
			return errors.New("unexpected model fetch")
		}},
	}
}

// unexpectedSoftware fails a test that reaches preparation.
type unexpectedSoftware struct{ t *testing.T }

func (u unexpectedSoftware) Install(context.Context, string, string, softwarelock.Document, io.Writer) error {
	u.t.Error("unexpected software install")
	return errors.New("unexpected software install")
}

func (u unexpectedSoftware) Check(context.Context, string, string, softwarelock.Document, io.Writer) error {
	u.t.Error("unexpected software check")
	return errors.New("unexpected software check")
}
