package receiptstore_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/installplan"
	"github.com/temper-sh/temper/internal/software/receipt"
	"github.com/temper-sh/temper/internal/software/receiptstore"
)

func TestCommitUsesDerivedPathAndRejectsAConcurrentWriter(t *testing.T) {
	root := filepath.Join(t.TempDir(), "temper-root")
	first, err := receiptstore.Read(root, "probe")
	if err != nil {
		t.Fatal(err)
	}
	concurrent, err := receiptstore.Read(root, "probe")
	if err != nil {
		t.Fatal(err)
	}
	document := receiptDocument(root)
	if err := first.Commit(context.Background(), document); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	path := filepath.Join(root, "software", "installations", "probe", "installation-receipt.yaml")
	if first.Path() != path {
		t.Fatalf("Path() = %q, want %q", first.Path(), path)
	}

	other := document
	other.ObservedAt = "2026-08-23T09:00:01Z"
	err = concurrent.Commit(context.Background(), other)
	if err == nil || !strings.Contains(err.Error(), "changed concurrently") {
		t.Fatalf("concurrent Commit() error = %v", err)
	}
	stored, err := receiptstore.Read(root, "probe")
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Exists() || stored.Document.ObservedAt != document.ObservedAt {
		t.Fatalf("stored receipt = %#v", stored.Document)
	}
}

func TestCommitCancellationLeavesNoRootOrStage(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "temper-root")
	snapshot, err := receiptstore.Read(root, "probe")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := snapshot.Commit(ctx, receiptDocument(root)); err == nil {
		t.Fatal("Commit() succeeded with a canceled context")
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("canceled commit left root: %v", err)
	}
}

func TestRemoveIsConditionalAndSecondRunClean(t *testing.T) {
	root := filepath.Join(t.TempDir(), "temper-root")
	initial, err := receiptstore.Read(root, "probe")
	if err != nil {
		t.Fatal(err)
	}
	if err := initial.Commit(context.Background(), receiptDocument(root)); err != nil {
		t.Fatal(err)
	}
	stale, err := receiptstore.Read(root, "probe")
	if err != nil {
		t.Fatal(err)
	}
	current, err := receiptstore.Read(root, "probe")
	if err != nil {
		t.Fatal(err)
	}
	changed := current.Document
	changed.ObservedAt = "2026-08-23T09:00:01Z"
	if err := current.Commit(context.Background(), changed); err != nil {
		t.Fatal(err)
	}
	if _, err := stale.Remove(context.Background()); err == nil || !strings.Contains(err.Error(), "changed concurrently") {
		t.Fatalf("stale Remove() error = %v", err)
	}

	fresh, err := receiptstore.Read(root, "probe")
	if err != nil {
		t.Fatal(err)
	}
	removed, err := fresh.Remove(context.Background())
	if err != nil || !removed {
		t.Fatalf("Remove() = %v, %v", removed, err)
	}
	missing, err := receiptstore.Read(root, "probe")
	if err != nil {
		t.Fatal(err)
	}
	removed, err = missing.Remove(context.Background())
	if err != nil || removed {
		t.Fatalf("second Remove() = %v, %v", removed, err)
	}
}

func receiptDocument(root string) receipt.Document {
	unitID := "uv:probe:tool"
	return receipt.Document{
		Schema: receipt.SchemaV1, Installation: "probe", SoftwareLockDigest: strings.Repeat("a", 64),
		Target: software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6"},
		Root:   root, ObservedAt: "2026-08-23T09:00:00Z", Requirements: []receipt.Requirement{},
		Selections: map[string]receipt.Selection{
			"tool": {Provenance: "experiment", Method: "python-environment", Adapter: "uv", RecipeRevision: "tool/v1", RootUnit: unitID},
		},
		Units: map[string]receipt.Unit{
			unitID: {
				Adapter: "uv", Scope: "probe", NativeName: "tool", Version: "1.0.0", Dependencies: []string{},
				Artifacts: []software.Artifact{{Locator: "https://example.invalid/tool", SHA256: strings.Repeat("b", 64)}},
				Location:  filepath.Join(root, "software", "installations", "probe", "environment", "tool"),
				Ownership: installplan.OwnershipTemperAdded,
			},
		},
	}
}
