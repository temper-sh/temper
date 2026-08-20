package datadir_test

import (
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
