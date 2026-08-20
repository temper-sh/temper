// Package lockstore owns concurrency-safe reads and atomic commits for the
// mechanically managed Temper lock file.
package lockstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/temper-sh/temper/internal/lockfile"
)

// Snapshot is one observed lock-file state. Commit succeeds only while the
// destination still contains exactly this state.
type Snapshot struct {
	Document lockfile.Document

	path   string
	data   []byte
	exists bool
	mode   fs.FileMode
}

// Read returns an absent snapshot with an empty lock document when path does
// not exist. It refuses directories and symlinks.
func Read(path string) (Snapshot, error) {
	if path == "" {
		return Snapshot{}, errors.New("lock path is required")
	}
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Snapshot{path: path, Document: lockfile.Empty(), mode: 0o644}, nil
		}
		return Snapshot{}, fmt.Errorf("inspect lock: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return Snapshot{}, errors.New("read lock: expected a regular file, not a directory or symlink")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read lock: %w", err)
	}
	document, err := lockfile.Parse(data)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		path:     path,
		data:     data,
		exists:   true,
		mode:     info.Mode().Perm(),
		Document: document,
	}, nil
}

func (s Snapshot) Exists() bool {
	return s.exists
}

// Commit stages candidate beside the destination and atomically replaces the
// lock only if the snapshot is still current.
func (s Snapshot) Commit(ctx context.Context, candidate []byte) (returnErr error) {
	if s.path == "" {
		return errors.New("lock snapshot has no path")
	}
	directory := filepath.Dir(s.path)
	stage, err := os.CreateTemp(directory, ".temper-lock-*")
	if err != nil {
		return fmt.Errorf("stage lock: %w", err)
	}
	stagePath := stage.Name()
	defer func() { _ = os.Remove(stagePath) }()
	if err := stage.Chmod(s.mode); err != nil {
		stage.Close()
		return fmt.Errorf("set staged lock mode: %w", err)
	}
	if _, err := stage.Write(candidate); err != nil {
		stage.Close()
		return fmt.Errorf("write staged lock: %w", err)
	}
	if err := stage.Sync(); err != nil {
		stage.Close()
		return fmt.Errorf("sync staged lock: %w", err)
	}
	if err := stage.Close(); err != nil {
		return fmt.Errorf("close staged lock: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	current, err := Read(s.path)
	if err != nil {
		return fmt.Errorf("verify lock before commit: %w", err)
	}
	if current.exists != s.exists || !bytes.Equal(current.data, s.data) {
		return errors.New("lock changed concurrently; rerun command")
	}
	if err := os.Rename(stagePath, s.path); err != nil {
		return fmt.Errorf("commit lock: %w", err)
	}
	if err := syncDirectory(directory); err != nil {
		return fmt.Errorf("sync lock directory: %w", err)
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
