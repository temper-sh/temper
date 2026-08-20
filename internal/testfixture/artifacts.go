// Package testfixture provides hermetic builders shared by black-box tests.
package testfixture

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/temper-sh/temper/internal/artifactset"
	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/manifest"
)

// MaterializeLayout writes a small, canonical artifact set whose supplied
// bytes must actually match the fixture lock.
func MaterializeLayout(t testing.TB, root, manifestPath, lockPath, layoutID string, contents map[string][]byte) artifactset.Set {
	t.Helper()
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	document, err := manifest.Parse(manifestData)
	if err != nil {
		t.Fatal(err)
	}
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := lockfile.Parse(lockData)
	if err != nil {
		t.Fatal(err)
	}
	layout, ok := document.Layouts[layoutID]
	if !ok {
		t.Fatalf("fixture manifest has no layout %q", layoutID)
	}
	entry, ok := locked.Entry(layoutID)
	if !ok {
		t.Fatalf("fixture lock has no entry %q", layoutID)
	}
	set, err := artifactset.New(root, layoutID, layout, entry, document.Patches)
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != len(set.Files()) {
		t.Fatalf("fixture content count = %d, want %d", len(contents), len(set.Files()))
	}
	records := make([]artifactset.Record, 0, len(set.Files()))
	for _, file := range set.Files() {
		content, ok := contents[file.Path]
		if !ok {
			t.Fatalf("fixture has no bytes for %q", file.Path)
		}
		digest := sha256.Sum256(content)
		if actual := hex.EncodeToString(digest[:]); actual != file.SHA256 {
			t.Fatalf("fixture %q SHA-256 = %s, lock requires %s", file.Path, actual, file.SHA256)
		}
		path := filepath.Join(set.Path(), filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
		records = append(records, artifactset.Record{Path: file.Path, SHA256: file.SHA256, Size: int64(len(content))})
	}
	receipt, err := set.Receipt(records)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(set.Path(), "receipt.json"), receipt, 0o644); err != nil {
		t.Fatal(err)
	}
	return set
}
