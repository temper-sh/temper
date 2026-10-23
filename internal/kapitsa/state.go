// Package kapitsa owns native calibration state. Model material remains owned
// by artifactset: its verified file links form the engine's scan-safe view.
package kapitsa

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// StatePath isolates engine/model calibration while allowing context tuning to
// reuse it. Calibration is observed runtime state, never frozen model evidence.
func StatePath(root, layout, artifacts, software string) string {
	id := sha256.Sum256([]byte(layout + "\n" + artifacts + "\n" + software))
	return filepath.Join(root, "runtime", "kapitsa", fmt.Sprintf("%x", id))
}

// PrepareState creates only owned directories and preserves existing calibration.
func PrepareState(root, state string) error { return directories(root, state, true) }

type Calibration struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

type State struct {
	Path         string        `json:"path"`
	Calibrations []Calibration `json:"calibrations"`
}

// Inspect reads calibration identities for diagnosis; an unused home stays absent.
func Inspect(root, state string) (State, error) {
	result := State{Path: state, Calibrations: []Calibration{}}
	dir := filepath.Join(state, "kflash")
	if err := directories(root, dir, false); errors.Is(err, fs.ErrNotExist) {
		return result, nil
	} else if err != nil {
		return result, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return result, err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return result, err
		}
		if !info.Mode().IsRegular() || info.Size() > 1<<20 {
			return result, errors.New("Kapitsa calibration must be a bounded regular file")
		}
		f, err := os.Open(filepath.Join(dir, entry.Name()))
		if err != nil {
			return result, err
		}
		data, readErr := io.ReadAll(io.LimitReader(f, (1<<20)+1))
		after, statErr := f.Stat()
		closeErr := f.Close()
		if err := errors.Join(readErr, statErr, closeErr); err != nil {
			return result, err
		}
		if !os.SameFile(info, after) || info.Size() != int64(len(data)) || !info.ModTime().Equal(after.ModTime()) {
			return result, errors.New("Kapitsa calibration changed during inspection; retry")
		}
		result.Calibrations = append(result.Calibrations, Calibration{Name: entry.Name(), SHA256: fmt.Sprintf("%x", sha256.Sum256(data))})
	}
	return result, nil
}

func directories(root, path string, create bool) error {
	rel, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsAbs(root) || !strings.HasPrefix(rel, "runtime"+string(filepath.Separator)+"kapitsa"+string(filepath.Separator)) {
		return errors.New("Kapitsa state escapes its owned runtime directory")
	}
	current := root
	for _, part := range append([]string{""}, strings.Split(rel, string(filepath.Separator))...) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if create && errors.Is(err, fs.ErrNotExist) {
			if err = os.Mkdir(current, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
				return err
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("Kapitsa state requires a real directory: %s", current)
		}
	}
	return nil
}
