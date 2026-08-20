package catalogstore_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software/catalog"
	"github.com/temper-sh/temper/internal/software/catalogstore"
)

func TestCommitStoresImmutablePublicationAndMovesActivePointer(t *testing.T) {
	root := filepath.Join(t.TempDir(), "temper-data")
	observed, err := catalogstore.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Exists() {
		t.Fatal("new store unexpectedly has an active catalog")
	}
	data := storeCatalog(42)
	digest := catalog.SnapshotDigest(data)
	signature := []byte("fixture signature\n")
	publication := catalogstore.Publication{CatalogData: data, SignatureData: signature, Digest: digest}

	if err := observed.Commit(context.Background(), publication); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	active, err := catalogstore.Read(root)
	if err != nil {
		t.Fatalf("Read() after commit error = %v", err)
	}
	if !active.Exists() || active.Catalog.SHA256 != digest || active.Catalog.Document.Sequence != 42 {
		t.Fatalf("active snapshot = exists %v digest %q sequence %d", active.Exists(), active.Catalog.SHA256, active.Catalog.Document.Sequence)
	}
	if string(active.SignatureData) != string(signature) {
		t.Errorf("stored signature = %q", active.SignatureData)
	}
	pointer, err := os.ReadFile(filepath.Join(root, "software", "catalog", "active"))
	if err != nil {
		t.Fatal(err)
	}
	if string(pointer) != digest+"\n" {
		t.Errorf("active pointer = %q, want digest and newline", pointer)
	}

	before := treeState(t, root)
	if err := active.Commit(context.Background(), publication); err != nil {
		t.Fatalf("clean second Commit() error = %v", err)
	}
	after := treeState(t, root)
	if before != after {
		t.Errorf("clean second commit changed store\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestCommitRefusesConcurrentActivePointerChange(t *testing.T) {
	root := filepath.Join(t.TempDir(), "temper-data")
	firstObservation, err := catalogstore.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	staleObservation, err := catalogstore.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	firstData := storeCatalog(41)
	firstDigest := catalog.SnapshotDigest(firstData)
	if err := firstObservation.Commit(context.Background(), catalogstore.Publication{
		CatalogData: firstData, SignatureData: []byte("first signature\n"), Digest: firstDigest,
	}); err != nil {
		t.Fatal(err)
	}
	secondData := storeCatalog(42)
	err = staleObservation.Commit(context.Background(), catalogstore.Publication{
		CatalogData: secondData, SignatureData: []byte("second signature\n"), Digest: catalog.SnapshotDigest(secondData),
	})
	if err == nil || !strings.Contains(err.Error(), "changed concurrently") {
		t.Fatalf("stale Commit() error = %v, want concurrency refusal", err)
	}
	active, err := catalogstore.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	if active.Catalog.SHA256 != firstDigest {
		t.Errorf("concurrent failure changed active digest to %q, want %q", active.Catalog.SHA256, firstDigest)
	}
}

func TestCommitRefusesConflictingImmutableSnapshot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "temper-data")
	observed, err := catalogstore.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	data := storeCatalog(42)
	digest := catalog.SnapshotDigest(data)
	snapshotPath := filepath.Join(root, "software", "catalog", "snapshots", digest)
	if err := os.MkdirAll(snapshotPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshotPath, "catalog.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshotPath, "catalog.signature.yaml"), []byte("different signature\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err = observed.Commit(context.Background(), catalogstore.Publication{
		CatalogData: data, SignatureData: []byte("expected signature\n"), Digest: digest,
	})
	if err == nil || !strings.Contains(err.Error(), "differs from publication") {
		t.Fatalf("Commit() error = %v, want immutable collision refusal", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "software", "catalog", "active")); !os.IsNotExist(err) {
		t.Fatalf("failed commit created active pointer: %v", err)
	}
}

func TestCommitValidatesBeforeCreatingStorePaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "temper-data")
	observed, err := catalogstore.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	invalid := []byte("schema: wrong\n")
	err = observed.Commit(context.Background(), catalogstore.Publication{
		CatalogData: invalid, SignatureData: []byte("fixture signature\n"), Digest: catalog.SnapshotDigest(invalid),
	})
	if err == nil || !strings.Contains(err.Error(), "before staging") {
		t.Fatalf("Commit() error = %v, want validation refusal", err)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("invalid publication created root: %v", err)
	}
}

func TestReadRefusesActivePointerSymlink(t *testing.T) {
	root := filepath.Join(t.TempDir(), "temper-data")
	catalogRoot := filepath.Join(root, "software", "catalog")
	if err := os.MkdirAll(catalogRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(target, []byte(strings.Repeat("a", 64)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(catalogRoot, "active")); err != nil {
		t.Fatal(err)
	}

	_, err := catalogstore.Read(root)
	if err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("Read() error = %v, want symlink refusal", err)
	}
}

func TestReadRefusesManagedDirectorySymlink(t *testing.T) {
	root := filepath.Join(t.TempDir(), "temper-data")
	if err := os.MkdirAll(filepath.Join(root, "software"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "software", "catalog")); err != nil {
		t.Fatal(err)
	}

	_, err := catalogstore.Read(root)
	if err == nil || !strings.Contains(err.Error(), "directory, not a file or symlink") {
		t.Fatalf("Read() error = %v, want managed-directory symlink refusal", err)
	}
}

func treeState(t *testing.T, root string) string {
	t.Helper()
	var state strings.Builder
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(&state, "%s %s %d %d\n", strings.TrimPrefix(path, root), info.Mode(), info.Size(), info.ModTime().UnixNano())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return state.String()
}

func storeCatalog(sequence uint64) []byte {
	return []byte(fmt.Sprintf(`schema: temper-software-supply/v1
sequence: %d
published_at: 2026-08-20T18:30:00Z
methods:
  system-package: {description: Shared target package manager}
adapters:
  homebrew: {method: system-package, protocol: temper-installer-adapter/v1, effect_model: shared}
target_bindings:
  - method: system-package
    target: {os: darwin, arch: arm64}
    adapter: homebrew
packages:
  llama-cpp:
    description: Primary runtime
    recipes:
      homebrew:
        method: system-package
        recipe_revision: llama-cpp-homebrew/v1
        source: {kind: homebrew-formula, tap: homebrew/core, formula: llama.cpp}
        version_scheme: semver
        selection: {policy: latest, minimum_compatible: 1.0.0}
        dependencies: []
        exclude: []
        gates: [runtime-smoke.v1]
        tested:
          - root_version: 1.0.0
            closure_digest: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
            target: {os: darwin, arch: arm64}
            evidence: results/software/llama-cpp-1.0.0
`, sequence))
}
