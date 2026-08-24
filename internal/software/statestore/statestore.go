// Package statestore owns the one concurrency-safe atomic commit point for
// root-wide software operation intent and shared claims.
package statestore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/temper-sh/temper/internal/datadir"
	"github.com/temper-sh/temper/internal/software/rootstate"
)

type Snapshot struct {
	Document rootstate.Document
	Data     []byte

	root   string
	path   string
	exists bool
}

func Read(root string) (Snapshot, error) {
	resolved, err := datadir.Resolve(root)
	if err != nil {
		return Snapshot{}, err
	}
	softwareRoot := filepath.Join(resolved, "software")
	for _, directory := range []string{resolved, softwareRoot} {
		if err := validateDirectoryIfExists(directory); err != nil {
			return Snapshot{}, fmt.Errorf("inspect software state store: %w", err)
		}
	}
	path := filepath.Join(softwareRoot, "state.yaml")
	document, data, exists, err := readPath(path)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Document: document, Data: data, root: resolved, path: path, exists: exists}, nil
}

func (s Snapshot) Exists() bool { return s.exists }
func (s Snapshot) Path() string { return s.path }

func (s Snapshot) Commit(ctx context.Context, candidate rootstate.Document) (returnErr error) {
	if s.path == "" || s.root == "" {
		return errors.New("software state snapshot has no derived path")
	}
	if candidate.Root != s.root {
		return errors.New("software state candidate belongs to another root")
	}
	data, err := rootstate.Marshal(candidate)
	if err != nil {
		return err
	}
	if s.exists && bytes.Equal(s.Data, data) {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	created := []string{}
	for _, directory := range []string{s.root, filepath.Join(s.root, "software")} {
		wasCreated, err := ensureDirectory(directory)
		if err != nil {
			removeEmptyDirectories(created)
			return fmt.Errorf("prepare software state store: %w", err)
		}
		if wasCreated {
			created = append(created, directory)
		}
	}
	committed := false
	defer func() {
		if returnErr != nil && !committed {
			removeEmptyDirectories(created)
		}
	}()
	stage, err := os.CreateTemp(filepath.Dir(s.path), ".temper-software-state-*")
	if err != nil {
		return fmt.Errorf("stage software root state: %w", err)
	}
	stagePath := stage.Name()
	defer func() { _ = os.Remove(stagePath) }()
	if err := stage.Chmod(0o644); err != nil {
		stage.Close()
		return fmt.Errorf("set staged software state mode: %w", err)
	}
	if _, err := stage.Write(data); err != nil {
		stage.Close()
		return fmt.Errorf("write staged software state: %w", err)
	}
	if err := stage.Sync(); err != nil {
		stage.Close()
		return fmt.Errorf("sync staged software state: %w", err)
	}
	if err := stage.Close(); err != nil {
		return fmt.Errorf("close staged software state: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, currentData, currentExists, err := readPath(s.path)
	if err != nil {
		return fmt.Errorf("verify software state before commit: %w", err)
	}
	if currentExists != s.exists || !bytes.Equal(currentData, s.Data) {
		return errors.New("software root state changed concurrently; rerun command")
	}
	if err := os.Rename(stagePath, s.path); err != nil {
		return fmt.Errorf("commit software root state: %w", err)
	}
	committed = true
	if err := syncDirectory(filepath.Dir(s.path)); err != nil {
		return fmt.Errorf("sync software root state directory: %w", err)
	}
	return nil
}

func readPath(path string) (rootstate.Document, []byte, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return rootstate.Document{}, nil, false, nil
	}
	if err != nil {
		return rootstate.Document{}, nil, false, fmt.Errorf("inspect software root state: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return rootstate.Document{}, nil, false, errors.New("software root state must be a regular file, not a directory or symlink")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return rootstate.Document{}, nil, false, fmt.Errorf("read software root state: %w", err)
	}
	document, err := rootstate.Parse(data)
	if err != nil {
		return rootstate.Document{}, nil, false, err
	}
	return document, data, true, nil
}

func ensureDirectory(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false, fmt.Errorf("%s must be a directory, not a file or symlink", path)
		}
		return false, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return ensureDirectory(path)
		}
		return false, err
	}
	return true, nil
}

func validateDirectoryIfExists(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s must be a directory, not a file or symlink", path)
	}
	return nil
}

func removeEmptyDirectories(paths []string) {
	for index := len(paths) - 1; index >= 0; index-- {
		_ = os.Remove(paths[index])
	}
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
