package lockstore_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/temper-sh/temper/internal/software/lockstore"
)

func TestCommitRejectsInvalidCandidateBeforeCreatingStage(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "software.lock.yaml")
	snapshot, err := lockstore.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	err = snapshot.Commit(context.Background(), []byte("schema: invalid\n"))
	if err == nil {
		t.Fatal("Commit() accepted invalid candidate")
	}
	if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("destination exists after invalid commit: %v", statErr)
	}
	entries, readErr := os.ReadDir(directory)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid commit left staged files: %v", entries)
	}
}
