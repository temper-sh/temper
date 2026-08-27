// Package receiptstore owns derived-path, concurrency-safe reads and atomic
// commits for one canonical installation receipt.
package receiptstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/temper-sh/temper/internal/datadir"
	"github.com/temper-sh/temper/internal/software/receipt"
)

const filename = "installation-receipt.yaml"

var installationIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)

type Snapshot struct {
	Document receipt.Document
	Data     []byte

	root         string
	installation string
	path         string
	exists       bool
}

func Read(root, installation string) (Snapshot, error) {
	resolved, err := datadir.Resolve(root)
	if err != nil {
		return Snapshot{}, err
	}
	if !installationIDPattern.MatchString(installation) {
		return Snapshot{}, fmt.Errorf("installation id %q is not a lowercase stable id", installation)
	}
	installationRoot := filepath.Join(resolved, "software", "installations", installation)
	path := filepath.Join(installationRoot, filename)
	for _, directory := range []string{resolved, filepath.Join(resolved, "software"), filepath.Join(resolved, "software", "installations"), installationRoot} {
		if err := validateDirectoryIfExists(directory); err != nil {
			return Snapshot{}, fmt.Errorf("inspect software receipt store: %w", err)
		}
	}
	document, data, exists, err := readPath(path)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Document: document, Data: data, root: resolved, installation: installation, path: path, exists: exists}, nil
}

// ReadFile reads a caller-supplied required receipt without deriving another
// path. The file must still be regular, canonical, and free of symlink
// indirection.
func ReadFile(path string) (receipt.Document, []byte, error) {
	document, data, exists, err := readPath(path)
	if err != nil {
		return receipt.Document{}, nil, err
	}
	if !exists {
		return receipt.Document{}, nil, fmt.Errorf("required software receipt %q does not exist", path)
	}
	return document, data, nil
}

func (s Snapshot) Exists() bool { return s.exists }
func (s Snapshot) Path() string { return s.path }

// Remove conditionally deletes the exact canonical receipt bytes represented
// by this snapshot. A missing snapshot is an idempotent no-op.
func (s Snapshot) Remove(ctx context.Context) (bool, error) {
	if s.path == "" || s.root == "" {
		return false, errors.New("software receipt snapshot has no derived path")
	}
	if !s.exists {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	_, currentData, currentExists, err := readPath(s.path)
	if err != nil {
		return false, fmt.Errorf("verify software receipt before removal: %w", err)
	}
	if !currentExists || !bytes.Equal(currentData, s.Data) {
		return false, errors.New("software installation receipt changed concurrently; rerun command")
	}
	if err := os.Remove(s.path); err != nil {
		return false, fmt.Errorf("remove software installation receipt: %w", err)
	}
	if err := syncDirectory(filepath.Dir(s.path)); err != nil {
		return true, fmt.Errorf("sync removed software installation receipt directory: %w", err)
	}
	return true, nil
}

func (s Snapshot) Commit(ctx context.Context, candidate receipt.Document) (returnErr error) {
	if s.path == "" || s.root == "" {
		return errors.New("software receipt snapshot has no derived path")
	}
	if candidate.Root != s.root || candidate.Installation != s.installation {
		return errors.New("software receipt candidate belongs to another root or installation")
	}
	data, err := receipt.Marshal(candidate)
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
	for _, directory := range []string{s.root, filepath.Join(s.root, "software"), filepath.Join(s.root, "software", "installations"), filepath.Dir(s.path)} {
		wasCreated, err := ensureDirectory(directory)
		if err != nil {
			removeEmptyDirectories(created)
			return fmt.Errorf("prepare software receipt store: %w", err)
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
	stage, err := os.CreateTemp(filepath.Dir(s.path), ".temper-installation-receipt-*")
	if err != nil {
		return fmt.Errorf("stage software installation receipt: %w", err)
	}
	stagePath := stage.Name()
	defer func() { _ = os.Remove(stagePath) }()
	if err := stage.Chmod(0o644); err != nil {
		stage.Close()
		return fmt.Errorf("set staged software receipt mode: %w", err)
	}
	if _, err := stage.Write(data); err != nil {
		stage.Close()
		return fmt.Errorf("write staged software receipt: %w", err)
	}
	if err := stage.Sync(); err != nil {
		stage.Close()
		return fmt.Errorf("sync staged software receipt: %w", err)
	}
	if err := stage.Close(); err != nil {
		return fmt.Errorf("close staged software receipt: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, currentData, currentExists, err := readPath(s.path)
	if err != nil {
		return fmt.Errorf("verify software receipt before commit: %w", err)
	}
	if currentExists != s.exists || !bytes.Equal(currentData, s.Data) {
		return errors.New("software installation receipt changed concurrently; rerun command")
	}
	if err := os.Rename(stagePath, s.path); err != nil {
		return fmt.Errorf("commit software installation receipt: %w", err)
	}
	committed = true
	if err := syncDirectory(filepath.Dir(s.path)); err != nil {
		return fmt.Errorf("sync software installation receipt directory: %w", err)
	}
	return nil
}

func readPath(path string) (receipt.Document, []byte, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return receipt.Document{}, nil, false, nil
	}
	if err != nil {
		return receipt.Document{}, nil, false, fmt.Errorf("inspect software installation receipt: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return receipt.Document{}, nil, false, errors.New("software installation receipt must be a regular file, not a directory or symlink")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return receipt.Document{}, nil, false, fmt.Errorf("read software installation receipt: %w", err)
	}
	document, err := receipt.Parse(data)
	if err != nil {
		return receipt.Document{}, nil, false, err
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
