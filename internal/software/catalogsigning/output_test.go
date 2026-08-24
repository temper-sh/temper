package catalogsigning_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software/catalogsigning"
)

func TestOutputCommitIsAtomicReplaceExplicitAndSecondRunClean(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.signature.yaml")
	first := []byte("first signature\n")
	second := []byte("second signature\n")

	snapshot, err := catalogsigning.ReadOutput(path)
	if err != nil {
		t.Fatal(err)
	}
	if change, err := snapshot.Plan(first, false); err != nil || change != catalogsigning.ChangeCreated {
		t.Fatalf("Plan(create) = %q, %v", change, err)
	}
	if change, err := snapshot.Commit(context.Background(), first, false); err != nil || change != catalogsigning.ChangeCreated {
		t.Fatalf("Commit(create) = %q, %v", change, err)
	}

	snapshot, err = catalogsigning.ReadOutput(path)
	if err != nil {
		t.Fatal(err)
	}
	if change, err := snapshot.Commit(context.Background(), first, false); err != nil || change != catalogsigning.ChangeUnchanged {
		t.Fatalf("Commit(unchanged) = %q, %v", change, err)
	}
	if _, err := snapshot.Commit(context.Background(), second, false); err == nil || !strings.Contains(err.Error(), "--replace") {
		t.Fatalf("Commit(different) error = %v", err)
	}
	if change, err := snapshot.Commit(context.Background(), second, true); err != nil || change != catalogsigning.ChangeReplaced {
		t.Fatalf("Commit(replace) = %q, %v", change, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(second) {
		t.Fatalf("committed data = %q, error %v", data, err)
	}
}

func TestOutputCommitRefusesConcurrentChangeAndSymlink(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "catalog.signature.yaml")
	if err := os.WriteFile(path, []byte("original\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	snapshot, err := catalogsigning.ReadOutput(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("concurrent\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshot.Commit(context.Background(), []byte("candidate\n"), true); err == nil || !strings.Contains(err.Error(), "changed concurrently") {
		t.Fatalf("Commit(concurrent) error = %v", err)
	}

	target := filepath.Join(directory, "target")
	link := filepath.Join(directory, "link")
	if err := os.WriteFile(target, []byte("target\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := catalogsigning.ReadOutput(link); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("ReadOutput(symlink) error = %v", err)
	}
}
