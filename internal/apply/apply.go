// Package apply orchestrates the read -> render -> stage -> commit flow for
// Temper config generations.
package apply

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/temper-sh/temper/internal/artifactset"
	"github.com/temper-sh/temper/internal/datadir"
	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/render"
)

type Options struct {
	ManifestPath       string
	LockPath           string
	Root               string
	Mode               string
	PiModelsBasePath   string
	PiSettingsBasePath string
	DryRun             bool
}

type Result struct {
	Changed    bool
	DryRun     bool
	Mode       string
	Generation string
	Artifacts  []string
}

func Run(ctx context.Context, options Options) (Result, error) {
	root, err := datadir.Resolve(options.Root)
	if err != nil {
		return Result{}, err
	}
	if options.ManifestPath == "" || options.LockPath == "" || options.Mode == "" {
		return Result{}, errors.New("manifest, lock, root and mode are required")
	}

	manifestData, err := os.ReadFile(options.ManifestPath)
	if err != nil {
		return Result{}, fmt.Errorf("read manifest: %w", err)
	}
	document, err := manifest.Parse(manifestData)
	if err != nil {
		return Result{}, err
	}
	lockData, err := os.ReadFile(options.LockPath)
	if err != nil {
		return Result{}, fmt.Errorf("read lock: %w", err)
	}
	locked, err := lockfile.Parse(lockData)
	if err != nil {
		return Result{}, err
	}
	if err := verifySelectedArtifactSets(root, options.Mode, document, locked); err != nil {
		return Result{}, err
	}
	piModelsBase, err := readOptional(options.PiModelsBasePath)
	if err != nil {
		return Result{}, fmt.Errorf("read Pi models base: %w", err)
	}
	piSettingsBase, err := readOptional(options.PiSettingsBasePath)
	if err != nil {
		return Result{}, fmt.Errorf("read Pi settings base: %w", err)
	}

	bundle, err := render.Build(render.Inputs{
		Manifest:       document,
		Lock:           locked,
		Mode:           options.Mode,
		Root:           root,
		PiModelsBase:   piModelsBase,
		PiSettingsBase: piSettingsBase,
	})
	if err != nil {
		return Result{}, err
	}
	digest := bundle.Digest()
	result := Result{
		DryRun:     options.DryRun,
		Mode:       options.Mode,
		Generation: digest,
		Artifacts:  artifactPaths(bundle),
	}

	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	current, err := currentGeneration(root)
	if err != nil {
		return Result{}, err
	}
	expectedTarget := filepath.Join("generations", digest)
	if current == expectedTarget {
		if err := verifyGeneration(root, bundle); err != nil {
			return Result{}, err
		}
		return result, nil
	}
	result.Changed = true
	if options.DryRun {
		return result, nil
	}

	if err := commit(ctx, root, expectedTarget, bundle); err != nil {
		return Result{}, err
	}
	return result, nil
}

func verifySelectedArtifactSets(root, modeName string, document manifest.Document, locked lockfile.Document) error {
	mode, err := document.Mode(modeName)
	if err != nil {
		return err
	}
	members := append([]manifest.Member(nil), mode.Members.Resident...)
	members = append(members, mode.Members.OnDemand...)
	for _, member := range members {
		layout := document.Layouts[member.Layout]
		entry, ok := locked.Entry(member.Layout)
		if !ok {
			return fmt.Errorf("layout %q has no lock entry; run temper resolve first", member.Layout)
		}
		set, err := artifactset.New(root, member.Layout, layout, entry, document.Patches)
		if err != nil {
			return err
		}
		if err := set.Verify(); err != nil {
			if errors.Is(err, artifactset.ErrNotMaterialized) {
				return fmt.Errorf("layout %q artifact set is not materialized; run temper fetch %s", member.Layout, member.Layout)
			}
			return fmt.Errorf("verify layout %q artifact set: %w", member.Layout, err)
		}
	}
	return nil
}

func readOptional(path string) ([]byte, error) {
	if path == "" {
		return nil, nil
	}
	return os.ReadFile(path)
}

func currentGeneration(root string) (string, error) {
	path := filepath.Join(root, "rendered", "current")
	target, err := os.Readlink(path)
	if err == nil {
		return target, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return "", fmt.Errorf("inspect rendered/current: expected a symlink: %w", err)
}

func commit(ctx context.Context, root, expectedTarget string, bundle render.Bundle) (returnErr error) {
	committed := false
	rootCreated, err := ensureRoot(root)
	if err != nil {
		return err
	}
	if rootCreated {
		defer func() {
			if returnErr != nil && !committed {
				_ = os.Remove(root)
			}
		}()
	}

	renderedRoot := filepath.Join(root, "rendered")
	generationsRoot := filepath.Join(renderedRoot, "generations")
	renderedCreated, err := ensureDirectory(renderedRoot)
	if err != nil {
		return fmt.Errorf("prepare rendered root: %w", err)
	}
	generationsCreated, err := ensureDirectory(generationsRoot)
	if err != nil {
		if renderedCreated {
			_ = os.Remove(renderedRoot)
		}
		return fmt.Errorf("prepare generation root: %w", err)
	}
	defer func() {
		if returnErr == nil || committed {
			return
		}
		if generationsCreated {
			_ = os.Remove(generationsRoot)
		}
		if renderedCreated {
			_ = os.Remove(renderedRoot)
		}
	}()

	finalPath := filepath.Join(renderedRoot, expectedTarget)
	installedGeneration := false
	if _, err := os.Lstat(finalPath); err == nil {
		if err := verifyGeneration(root, bundle); err != nil {
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect generation: %w", err)
	} else {
		stagePath, err := os.MkdirTemp(generationsRoot, ".stage-")
		if err != nil {
			return fmt.Errorf("create generation stage: %w", err)
		}
		defer func() { _ = os.RemoveAll(stagePath) }()

		if err := writeBundle(stagePath, bundle); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := os.Rename(stagePath, finalPath); err != nil {
			// Another identical apply may have won the generation race. Its
			// bytes must still prove the content-derived identity before use.
			if verifyErr := verifyGeneration(root, bundle); verifyErr != nil {
				return fmt.Errorf("install generation: %w", err)
			}
		} else {
			installedGeneration = true
			if err := syncDirectory(generationsRoot); err != nil {
				_ = os.RemoveAll(finalPath)
				return err
			}
		}
	}
	if installedGeneration {
		defer func() {
			if returnErr != nil && !committed {
				_ = os.RemoveAll(finalPath)
			}
		}()
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	temporaryLink, err := stageCurrentPointer(renderedRoot, expectedTarget)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(temporaryLink) }()
	if err := os.Rename(temporaryLink, filepath.Join(renderedRoot, "current")); err != nil {
		return fmt.Errorf("commit current pointer: %w", err)
	}
	committed = true
	if err := syncDirectory(renderedRoot); err != nil {
		return err
	}
	return nil
}

func stageCurrentPointer(renderedRoot, target string) (string, error) {
	for attempt := 0; attempt < 10; attempt++ {
		var random [8]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", fmt.Errorf("name temporary current pointer: %w", err)
		}
		path := filepath.Join(renderedRoot, ".current-"+hex.EncodeToString(random[:]))
		if err := os.Symlink(target, path); err == nil {
			return path, nil
		} else if !errors.Is(err, fs.ErrExist) {
			return "", fmt.Errorf("stage current pointer: %w", err)
		}
	}
	return "", errors.New("stage current pointer: could not reserve a unique name")
}

func ensureRoot(root string) (bool, error) {
	created, err := ensureDirectory(root)
	if err != nil {
		return false, fmt.Errorf("prepare Temper root (its parent must already exist): %w", err)
	}
	return created, nil
}

func ensureDirectory(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false, errors.New("expected a directory, not a file or symlink")
		}
		return false, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("inspect Temper root: %w", err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		return false, err
	}
	return true, nil
}

func writeBundle(stagePath string, bundle render.Bundle) error {
	for _, artifact := range bundle.Artifacts {
		path := filepath.Join(stagePath, filepath.FromSlash(artifact.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("stage %s: %w", artifact.Path, err)
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return fmt.Errorf("stage %s: %w", artifact.Path, err)
		}
		if _, err := file.Write(artifact.Data); err != nil {
			_ = file.Close()
			return fmt.Errorf("stage %s: %w", artifact.Path, err)
		}
		if err := file.Sync(); err != nil {
			_ = file.Close()
			return fmt.Errorf("sync %s: %w", artifact.Path, err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("close %s: %w", artifact.Path, err)
		}
	}
	return syncTreeDirectories(stagePath)
}

func verifyGeneration(root string, bundle render.Bundle) error {
	path := filepath.Join(root, "rendered", "generations", bundle.Digest())
	want := map[string][]byte{}
	for _, artifact := range bundle.Artifacts {
		want[filepath.FromSlash(artifact.Path)] = artifact.Data
	}
	seen := map[string]bool{}
	err := filepath.WalkDir(path, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == path || entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(path, current)
		if err != nil {
			return err
		}
		expected, ok := want[relative]
		if !ok {
			return fmt.Errorf("generation %s contains unexpected artifact %s", bundle.Digest(), filepath.ToSlash(relative))
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return fmt.Errorf("generation artifact %s is not a regular file", filepath.ToSlash(relative))
		}
		actual, err := os.ReadFile(current)
		if err != nil {
			return err
		}
		if !bytesEqual(actual, expected) {
			return fmt.Errorf("generation %s artifact %s does not match its content digest", bundle.Digest(), filepath.ToSlash(relative))
		}
		seen[relative] = true
		return nil
	})
	if err != nil {
		return fmt.Errorf("verify generation: %w", err)
	}
	for relative := range want {
		if !seen[relative] {
			return fmt.Errorf("verify generation: artifact %s is missing", filepath.ToSlash(relative))
		}
	}
	return nil
}

func syncTreeDirectories(root string) error {
	var directories []string
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			directories = append(directories, path)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("inspect staged generation: %w", err)
	}
	sort.Slice(directories, func(i, j int) bool { return len(directories[i]) > len(directories[j]) })
	for _, directory := range directories {
		if err := syncDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory for sync: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync directory %s: %w", path, err)
	}
	return nil
}

func artifactPaths(bundle render.Bundle) []string {
	paths := make([]string, 0, len(bundle.Artifacts))
	for _, artifact := range bundle.Artifacts {
		paths = append(paths, artifact.Path)
	}
	sort.Strings(paths)
	return paths
}

func bytesEqual(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
