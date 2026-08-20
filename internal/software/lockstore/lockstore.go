// Package lockstore owns concurrency-safe reads and atomic commits for the
// mechanically managed software lock.
package lockstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
)

// Snapshot is one observed software-lock state. Commit succeeds only while
// the destination still contains exactly that state.
type Snapshot struct {
	Document softwarelock.Document

	path   string
	data   []byte
	exists bool
	mode   fs.FileMode
}

func Read(path string) (Snapshot, error) {
	if path == "" {
		return Snapshot{}, errors.New("software lock path is required")
	}
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Snapshot{path: path, mode: 0o644}, nil
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
	return Snapshot{path: path, data: data, exists: true, mode: info.Mode().Perm(), Document: document}, nil
}

func (s Snapshot) Exists() bool { return s.exists }

// Commit validates the candidate before any filesystem effect, stages it next
// to the destination, and atomically replaces the target only if the original
// snapshot is still current.
func (s Snapshot) Commit(ctx context.Context, candidate []byte) (returnErr error) {
	if s.path == "" {
		return errors.New("software lock snapshot has no path")
	}
	if _, err := softwarelock.Parse(candidate); err != nil {
		return fmt.Errorf("validate software lock before staging: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	directory := filepath.Dir(s.path)
	stage, err := os.CreateTemp(directory, ".temper-software-lock-*")
	if err != nil {
		return fmt.Errorf("stage software lock: %w", err)
	}
	stagePath := stage.Name()
	defer func() { _ = os.Remove(stagePath) }()
	if err := stage.Chmod(s.mode); err != nil {
		stage.Close()
		return fmt.Errorf("set staged software lock mode: %w", err)
	}
	if _, err := stage.Write(candidate); err != nil {
		stage.Close()
		return fmt.Errorf("write staged software lock: %w", err)
	}
	if err := stage.Sync(); err != nil {
		stage.Close()
		return fmt.Errorf("sync staged software lock: %w", err)
	}
	if err := stage.Close(); err != nil {
		return fmt.Errorf("close staged software lock: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	current, err := Read(s.path)
	if err != nil {
		return fmt.Errorf("verify software lock before commit: %w", err)
	}
	if current.exists != s.exists || !bytes.Equal(current.data, s.data) {
		return errors.New("software lock changed concurrently; rerun command")
	}
	if err := os.Rename(stagePath, s.path); err != nil {
		return fmt.Errorf("commit software lock: %w", err)
	}
	if err := syncDirectory(directory); err != nil {
		return fmt.Errorf("sync software lock directory: %w", err)
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
