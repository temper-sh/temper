package statestore_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/installplan"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/rootstate"
	"github.com/temper-sh/temper/internal/software/statestore"
)

func TestCommitUsesOneConditionalAtomicStatePath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "temper-root")
	document := preparedState(t, root)
	first, err := statestore.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	concurrent, err := statestore.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Commit(context.Background(), document); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if first.Path() != filepath.Join(root, "software", "state.yaml") {
		t.Fatalf("Path() = %q", first.Path())
	}

	other := document
	other.Generation++
	err = concurrent.Commit(context.Background(), other)
	if err == nil || !strings.Contains(err.Error(), "changed concurrently") {
		t.Fatalf("concurrent Commit() error = %v", err)
	}
	stored, err := statestore.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Exists() || stored.Document.Generation != document.Generation {
		t.Fatalf("stored state generation = %d", stored.Document.Generation)
	}
}

func TestCanceledCommitCreatesNothing(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "temper-root")
	snapshot, err := statestore.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := snapshot.Commit(ctx, preparedState(t, root)); err == nil {
		t.Fatal("Commit() succeeded with a canceled context")
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("canceled commit left root: %v", err)
	}
}

func preparedState(t *testing.T, root string) rootstate.Document {
	t.Helper()
	desired := softwarelock.Document{
		Schema: softwarelock.SchemaV1,
		Provenance: softwarelock.Provenance{Experiment: &softwarelock.ExperimentIdentity{
			Schema: "field-kit-experiment/v1", ID: "store-fixture", DefinitionSHA256: strings.Repeat("a", 64),
		}},
		Target:   software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6"},
		Resolved: "2026-08-23",
		Selections: map[string]softwarelock.Selection{
			"tool": {Provenance: softwarelock.ProvenanceExperiment, Method: "python-environment", Adapter: "uv", RecipeRevision: "tool/v1", RootUnit: "uv:probe:tool"},
		},
		Units: map[string]softwarelock.Unit{
			"uv:probe:tool": {
				Adapter: "uv", Scope: "probe", NativeName: "tool", Version: "1.0.0", Dependencies: []string{},
				Artifacts: []software.Artifact{{Locator: "https://example.invalid/tool", SHA256: strings.Repeat("b", 64)}},
			},
		},
	}
	installation := installplan.Installation{ID: "probe", Root: root}
	location := installplan.InstallationRoot(installation) + "/environment/tool"
	observed := installplan.Observation{Target: desired.Target, Root: root, Units: map[string]installplan.ObservedUnit{
		"uv:probe:tool": {InstallLocation: location},
	}}
	plan, err := installplan.Build(desired, installation, map[string]installplan.EffectModel{"uv": installplan.EffectIsolated}, observed, installplan.State{})
	if err != nil {
		t.Fatal(err)
	}
	document, _, _, err := rootstate.Prepare(nil, desired, plan, observed, rootstate.Lease{
		InvocationID: "store-run", Now: time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC), Duration: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	return document
}
