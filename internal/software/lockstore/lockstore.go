// Package lockstore reads exact desired software locks.
package lockstore

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
)

// Snapshot is one observed software-lock state.
type Snapshot struct {
	Document softwarelock.Document

	exists bool
}

func Read(path string) (Snapshot, error) {
	if path == "" {
		return Snapshot{}, errors.New("software lock path is required")
	}
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Snapshot{}, nil
		}
		return Snapshot{}, fmt.Errorf("inspect software lock: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return Snapshot{}, errors.New("read software lock: expected a regular file, not a directory or symlink")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read software lock: %w", err)
	}
	document, err := softwarelock.Parse(data)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{exists: true, Document: document}, nil
}

func (s Snapshot) Exists() bool { return s.exists }
