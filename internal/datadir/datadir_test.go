package datadir_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/datadir"
)

func TestResolveReturnsAnAbsoluteCleanRoot(t *testing.T) {
	want, err := filepath.Abs(filepath.Join("fixture", "root"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := datadir.Resolve(filepath.Join("fixture", "nested", "..", "root"))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Resolve() = %q, want %q", got, want)
	}
}

func TestResolveRefusesUnsafeRoots(t *testing.T) {
	tests := []struct {
		name    string
		root    string
		message string
	}{
		{name: "missing", root: "", message: "root is required"},
		{name: "control character", root: "bad\nroot", message: "control character"},
		{name: "filesystem root", root: string(filepath.Separator), message: "filesystem root"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := datadir.Resolve(test.root)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("Resolve() error = %v, want %q", err, test.message)
			}
		})
	}
}

func TestRootMarkerVersionsTheWholeRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	if err := datadir.Check(root); err != nil {
		t.Fatalf("a missing root was refused: %v", err)
	}
	if err := datadir.Open(root, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("a read created the root")
	}
	if err := datadir.Ensure(root); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, datadir.MarkerFile))
	if err != nil || string(raw) != `{"schema":"temper-root/v1","format":1}`+"\n" {
		t.Fatalf("marker = %q, %v", raw, err)
	}
	if err := datadir.Ensure(root); err != nil {
		t.Fatal("a second open failed", err)
	}
	if err := os.WriteFile(filepath.Join(root, datadir.MarkerFile), []byte(`{"schema":"temper-root/v1","format":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := datadir.Check(root); err == nil || !strings.Contains(err.Error(), "written by a newer Temper") {
		t.Fatalf("newer root = %v", err)
	}

	foreign := t.TempDir()
	if err := os.WriteFile(filepath.Join(foreign, ".field-kit-owner.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := datadir.Ensure(foreign); err != nil {
		t.Fatalf("files of another tool were refused: %v", err)
	}
	old := t.TempDir()
	if err := os.Mkdir(filepath.Join(old, "software"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, write := range []bool{false, true} {
		if err := datadir.Open(old, write); err == nil || !strings.Contains(err.Error(), "before 0.1.0") {
			t.Fatalf("Temper content without a marker = %v", err)
		}
	}
}
