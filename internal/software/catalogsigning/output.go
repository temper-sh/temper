package catalogsigning

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

type Change string

const (
	ChangeCreated   Change = "created"
	ChangeReplaced  Change = "replaced"
	ChangeUnchanged Change = "unchanged"
)

// OutputSnapshot is one observed detached-signature destination. Commit
// refuses if the destination changes after this read.
type OutputSnapshot struct {
	path   string
	data   []byte
	exists bool
	mode   fs.FileMode
}

func ReadOutput(path string) (OutputSnapshot, error) {
	if path == "" {
		return OutputSnapshot{}, errors.New("catalog signature output path is required")
	}
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return OutputSnapshot{path: path, mode: 0o644}, nil
		}
		return OutputSnapshot{}, fmt.Errorf("inspect catalog signature output: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return OutputSnapshot{}, errors.New("catalog signature output must be a regular file, not a directory or symlink")
	}
	file, err := os.Open(path)
	if err != nil {
		return OutputSnapshot{}, fmt.Errorf("read catalog signature output: %w", err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return OutputSnapshot{}, fmt.Errorf("inspect opened catalog signature output: %w", err)
	}
	if !os.SameFile(info, openedInfo) {
		return OutputSnapshot{}, errors.New("catalog signature output changed while it was being read; rerun command")
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return OutputSnapshot{}, fmt.Errorf("read catalog signature output: %w", err)
	}
	return OutputSnapshot{path: path, data: data, exists: true, mode: info.Mode().Perm()}, nil
}

func (s OutputSnapshot) Plan(candidate []byte, replace bool) (Change, error) {
	if len(candidate) == 0 {
		return "", errors.New("catalog signature candidate is empty")
	}
	if s.exists && bytes.Equal(s.data, candidate) {
		return ChangeUnchanged, nil
	}
	if s.exists && !replace {
		return "", errors.New("catalog signature output differs; rerun with --replace only after reviewing the changed artifact")
	}
	if s.exists {
		return ChangeReplaced, nil
	}
	return ChangeCreated, nil
}

// Commit stages and syncs the complete candidate, rechecks the original
// snapshot, then atomically places the output once.
func (s OutputSnapshot) Commit(ctx context.Context, candidate []byte, replace bool) (Change, error) {
	change, err := s.Plan(candidate, replace)
	if err != nil || change == ChangeUnchanged {
		return change, err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	directory := filepath.Dir(s.path)
	stage, err := os.CreateTemp(directory, ".temper-catalog-signature-*")
	if err != nil {
		return "", fmt.Errorf("stage catalog signature: %w", err)
	}
	stagePath := stage.Name()
	defer func() { _ = os.Remove(stagePath) }()
	if err := stage.Chmod(s.mode); err != nil {
		stage.Close()
		return "", fmt.Errorf("set staged catalog signature mode: %w", err)
	}
	if _, err := stage.Write(candidate); err != nil {
		stage.Close()
		return "", fmt.Errorf("write staged catalog signature: %w", err)
	}
	if err := stage.Sync(); err != nil {
		stage.Close()
		return "", fmt.Errorf("sync staged catalog signature: %w", err)
	}
	if err := stage.Close(); err != nil {
		return "", fmt.Errorf("close staged catalog signature: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	current, err := ReadOutput(s.path)
	if err != nil {
		return "", fmt.Errorf("verify catalog signature before commit: %w", err)
	}
	if current.exists != s.exists || !bytes.Equal(current.data, s.data) {
		return "", errors.New("catalog signature output changed concurrently; rerun command")
	}
	if err := os.Rename(stagePath, s.path); err != nil {
		return "", fmt.Errorf("commit catalog signature: %w", err)
	}
	if err := syncDirectory(directory); err != nil {
		return "", fmt.Errorf("sync catalog signature directory: %w", err)
	}
	return change, nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
