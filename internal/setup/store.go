package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/datadir"
)

// Root resolves the guided setup default; lower-level execution operations
// continue to accept explicit roots.
func Root(path, home string) (string, error) {
	if path == "" {
		if home == "" {
			return "", errors.New("home directory is unavailable; supply --root")
		}
		path = filepath.Join(home, ".temper")
	}
	return datadir.Resolve(path)
}

// ExistingDirectory finds the filesystem on which a new root will be created.
// It reads only and refuses an existing non-directory or symlink at the boundary.
func ExistingDirectory(path string) (string, error) {
	for {
		info, err := os.Lstat(path)
		if err == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("%s must be a real directory", path)
			}
			return path, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		path = parent
	}
}

// Save publishes the complete selected configuration once. A different
// configuration is an advisory refusal, never authority to replace user files.
func Save(ctx context.Context, plan Plan, dry bool) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	root, err := datadir.Resolve(plan.Root)
	if err != nil {
		return false, err
	}
	if _, err := ExistingDirectory(root); err != nil {
		return false, err
	}
	files, err := plan.Files()
	if err != nil {
		return false, err
	}
	if len(files) == 0 {
		return false, errors.New("setup has no selected configuration")
	}
	path := filepath.Join(root, ConfigurationDir)
	if exists, err := sameConfiguration(path, files); exists || err != nil {
		return false, err
	}
	// Do not coexist silently with a user-owned manifest from the explicit path.
	if _, err := os.Lstat(filepath.Join(root, "manifest.yaml")); err == nil {
		return false, errors.New("this root already contains a user manifest; use its explicit workflow or choose another --root")
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if dry {
		return true, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return false, err
	}
	unlock, err := lockRoot(root)
	if err != nil {
		return false, err
	}
	defer unlock()
	if err := noManifest(root); err != nil {
		return false, err
	}
	if exists, err := sameConfiguration(path, files); exists || err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	stage, err := os.MkdirTemp(root, ".setup-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(stage)
	for _, name := range keys(files) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		f, err := os.OpenFile(filepath.Join(stage, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return false, err
		}
		_, writeErr := f.Write(files[name])
		if writeErr == nil {
			writeErr = f.Sync()
		}
		closeErr := f.Close()
		if writeErr != nil {
			return false, writeErr
		}
		if closeErr != nil {
			return false, closeErr
		}
	}
	if err := syncDir(stage); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if exists, err := sameConfiguration(path, files); exists || err != nil {
		return false, err
	}
	if err := noManifest(root); err != nil {
		return false, err
	}
	if err := publishDirectory(stage, path); err != nil {
		return false, err
	}
	if err := syncDir(root); err != nil {
		return true, fmt.Errorf("configuration saved; directory sync failed: %w", err)
	}
	return true, nil
}

func sameConfiguration(path string, files map[string][]byte) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("configuration must be a real directory")
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	if len(entries) != len(files) {
		return false, errors.New("existing configuration differs; preserved user files, choose another root to save a new setup")
	}
	for _, entry := range entries {
		want, ok := files[entry.Name()]
		if !ok {
			return false, fmt.Errorf("preserved unrelated configuration file %q", entry.Name())
		}
		got, err := readRegular(filepath.Join(path, entry.Name()), 4<<20)
		if err != nil {
			return false, err
		}
		if !bytes.Equal(want, got) {
			return false, fmt.Errorf("existing %s differs; preserved user configuration, choose another root to save a new setup", entry.Name())
		}
	}
	return true, nil
}

func readRegular(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("%s must be a regular file no larger than %d bytes", path, limit)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, fmt.Errorf("%s changed while reading", path)
	}
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, errors.New("configuration file exceeds size limit")
	}
	return raw, nil
}

// Saved contains the exact configurations and the selected local default.
type Saved struct {
	Locks          []catalog.Lock
	DefaultProfile string
}

// Load resumes the saved exact locks without contacting a moving catalog.
// Edited selections are refused until the user explicitly resolves new locks.
func Load(root string) (Saved, error) {
	root, err := datadir.Resolve(root)
	if err != nil {
		return Saved{}, err
	}
	if _, err := ExistingDirectory(root); err != nil {
		return Saved{}, err
	}
	path := filepath.Join(root, ConfigurationDir)
	info, err := os.Lstat(path)
	if err != nil {
		return Saved{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Saved{}, errors.New("configuration must be a real directory")
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return Saved{}, err
	}
	names := map[string]bool{}
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name(), ".selection.json")
		if name == entry.Name() {
			name = strings.TrimSuffix(entry.Name(), ".execution.lock.json")
		}
		if name == entry.Name() || name != "local" && name != "utility" && !strings.HasPrefix(name, "local.") {
			return Saved{}, fmt.Errorf("unrecognized configuration file %q", entry.Name())
		}
		names[name] = true
	}
	var result Saved
	for _, name := range keys(names) {
		selectionPath := filepath.Join(path, name+".selection.json")
		lockPath := filepath.Join(path, name+".execution.lock.json")
		raw, err := readRegular(selectionPath, 256<<10)
		if err != nil {
			return Saved{}, err
		}
		selection, err := catalog.ParseSelection(raw)
		if err != nil {
			return Saved{}, err
		}
		raw, err = readRegular(lockPath, 4<<20)
		if err != nil {
			return Saved{}, err
		}
		locked, err := catalog.ParseLock(raw)
		if err != nil {
			return Saved{}, err
		}
		if !reflect.DeepEqual(selection, locked.Selection) {
			return Saved{}, fmt.Errorf("%s selection differs from its exact lock; resolve the edited choices explicitly", name)
		}
		mode := Mode(locked.Records.Profiles[selection.Profile])
		if name == "local" {
			result.DefaultProfile = selection.Profile
		}
		if configurationName(mode, selection.Profile, result.DefaultProfile) != name {
			return Saved{}, fmt.Errorf("%s lock belongs to another mode or profile", name)
		}
		result.Locks = append(result.Locks, locked)
	}
	if len(result.Locks) == 0 {
		return Saved{}, errors.New("no saved setup found")
	}
	selected, err := selectedDefault(result.Locks, result.DefaultProfile)
	if err != nil {
		return Saved{}, err
	}
	if selected != result.DefaultProfile {
		return Saved{}, errors.New("saved setup is missing its default local configuration")
	}
	return result, nil
}

func syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func noManifest(root string) error {
	_, err := os.Lstat(filepath.Join(root, "manifest.yaml"))
	if err == nil {
		return errors.New("preserved existing user manifest; choose another root for guided setup")
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// EqualChoices ignores formatting while enforcing one choice writer.
func EqualChoices(a, b catalog.Selection) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}
