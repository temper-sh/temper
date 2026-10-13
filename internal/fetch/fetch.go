// Package fetch materializes one immutable, content-addressed layout artifact
// set with a stage -> validate -> atomic publish flow.
package fetch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/temper-sh/temper/internal/artifactset"
	"github.com/temper-sh/temper/internal/datadir"
	"github.com/temper-sh/temper/internal/hfcache"
	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/patch"
	"github.com/temper-sh/temper/internal/upstream"
)

const (
	maxPatchSource = 16 << 20
)

type Options struct {
	Manifest      manifest.Document
	Lock          lockfile.Document
	Root          string
	Layout        string
	DryRun        bool
	HFCache       string
	DownloadModel hfcache.DownloadFunc
}

// Func materializes one layout's exact artifact set below root. The caller
// binds the upstream reader, shared HF cache and model downloader.
type Func func(ctx context.Context, root, layout string, document manifest.Document, locked lockfile.Document) error

type Result struct {
	Changed     bool
	DryRun      bool
	Layout      string
	ArtifactSet string
	Files       []string
}

type plan struct {
	root        string
	layoutID    string
	set         artifactset.Set
	entry       lockfile.Entry
	patchID     string
	patch       manifest.Patch
	patchSource patch.Source
}

func Run(ctx context.Context, options Options, source upstream.Reader) (Result, error) {
	materialization, err := buildPlan(options)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		DryRun:      options.DryRun,
		Layout:      materialization.layoutID,
		ArtifactSet: materialization.set.Digest(),
		Files:       materialization.set.RootRelativeFiles(),
	}
	if verifyErr := materialization.set.Verify(); verifyErr == nil {
		return result, nil
	} else if errors.Is(verifyErr, artifactset.ErrCacheMissing) || errors.Is(verifyErr, artifactset.ErrCacheChanged) || errors.Is(verifyErr, artifactset.ErrCacheRedirected) {
		result.Changed = true
		if options.DryRun {
			return result, nil
		}
		cacheRoot, err := materialization.set.CacheRoot()
		if err != nil {
			return Result{}, err
		}
		configured := options.HFCache
		if configured == "" {
			configured = cacheRoot
		}
		if err := cacheVolumeAvailable(configured); err != nil {
			return Result{}, err
		}
		if err := datadir.Ensure(materialization.root); err != nil {
			return Result{}, err
		}
		if errors.Is(verifyErr, artifactset.ErrCacheRedirected) || !sameDirectory(cacheRoot, configured) {
			// The cache moved. Rebuild the set's links from the cache the user
			// configured now; never write into the recorded one.
			if err := publish(ctx, materialization, source, configured, options.DownloadModel, true); err != nil {
				return Result{}, err
			}
			return result, nil
		}
		for _, model := range selectedModels(materialization.entry) {
			entry := hfcache.Entry{Repo: model.Repo, Revision: model.Revision, Name: model.Name, SHA256: model.SHA256}
			if _, err := (hfcache.Cache{Root: cacheRoot}).Ensure(ctx, entry, options.DownloadModel); err != nil {
				return Result{}, err
			}
		}
		if err := materialization.set.RefreshCacheReceipt(ctx); err != nil {
			return Result{}, err
		}
		return result, nil
	} else if !errors.Is(verifyErr, artifactset.ErrNotMaterialized) {
		return Result{}, verifyErr
	}
	result.Changed = true
	if options.DryRun {
		return result, nil
	}
	if err := publish(ctx, materialization, source, options.HFCache, options.DownloadModel, false); err != nil {
		return Result{}, err
	}
	return result, nil
}

func buildPlan(options Options) (plan, error) {
	if options.Root == "" || options.Layout == "" {
		return plan{}, errors.New("root and layout are required")
	}
	root, err := datadir.Resolve(options.Root)
	if err != nil {
		return plan{}, err
	}
	document, locked := options.Manifest, options.Lock
	if err := document.Validate(); err != nil {
		return plan{}, err
	}
	if err := locked.Validate(); err != nil {
		return plan{}, err
	}
	layout, ok := document.Layouts[options.Layout]
	if !ok {
		return plan{}, fmt.Errorf("layout %q is not declared in the manifest", options.Layout)
	}
	entry, ok := locked.Entry(options.Layout)
	if !ok {
		return plan{}, fmt.Errorf("layout %q has no lock entry", options.Layout)
	}
	set, err := artifactset.New(root, options.Layout, layout, entry, document.Patches)
	if err != nil {
		return plan{}, err
	}
	materialization := plan{
		root:     root,
		layoutID: options.Layout,
		set:      set,
		entry:    entry,
	}
	if layout.ChatTemplate != "" {
		definition := document.Patches[layout.ChatTemplate]
		source, err := patch.ParseSource(definition.Source)
		if err != nil {
			return plan{}, fmt.Errorf("patch %q source: %w", layout.ChatTemplate, err)
		}
		materialization.patchID = layout.ChatTemplate
		materialization.patch = definition
		materialization.patchSource = source
	}
	return materialization, nil
}

type selectedModel struct {
	lockfile.File
	Repo, Revision, Prefix string
}

func selectedModels(entry lockfile.Entry) []selectedModel {
	var selected []selectedModel
	for _, file := range entry.Files {
		selected = append(selected, selectedModel{file, entry.Repo, entry.Revision, "model"})
	}
	if draft := entry.Draft; draft != nil {
		for _, file := range draft.Files {
			selected = append(selected, selectedModel{file, draft.Repo, draft.Revision, "draft"})
		}
	}
	if projector := entry.Projector; projector != nil {
		for _, file := range projector.Files {
			selected = append(selected, selectedModel{file, projector.Repo, projector.Revision, "projector"})
		}
	}
	return selected
}

// sameDirectory compares two cache roots after following links.
func sameDirectory(a, b string) bool {
	if resolved, err := filepath.EvalSymlinks(a); err == nil {
		a = resolved
	}
	if resolved, err := filepath.EvalSymlinks(b); err == nil {
		b = resolved
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// cacheVolumeAvailable refuses a cache on an external disk that is not
// connected, where a download would fill the startup disk under /Volumes.
func cacheVolumeAvailable(root string) error {
	parts := strings.Split(filepath.Clean(root), string(filepath.Separator))
	if len(parts) < 3 || parts[0] != "" || parts[1] != "Volumes" {
		return nil
	}
	volume := filepath.Join("/Volumes", parts[2])
	if _, err := os.Stat(volume); err != nil {
		return fmt.Errorf("HF cache volume unavailable: %s; connect it or change HF_HOME or HF_HUB_CACHE", volume)
	}
	return nil
}

// publish builds a layout's artifact set in a stage. With replace, an
// existing set is swapped for the new one (the cache moved); content
// identity is unchanged.
func publish(ctx context.Context, materialization plan, source upstream.Reader, cacheRoot string, download hfcache.DownloadFunc, replace bool) (returnErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	selected := selectedModels(materialization.entry)
	layoutRoot := filepath.Join(materialization.root, "artifacts", "layouts", materialization.layoutID)
	_, statErr := os.Lstat(materialization.root)
	fresh := errors.Is(statErr, fs.ErrNotExist)
	if err := datadir.Ensure(materialization.root); err != nil {
		return err
	}
	created, err := prepareDirectories(materialization.root, layoutRoot)
	committed := false
	defer func() {
		if returnErr == nil || committed {
			return
		}
		for index := len(created) - 1; index >= 0; index-- {
			_ = os.Remove(created[index])
		}
		// A failed fetch into a new root leaves no root behind, unless
		// another writer has put content there.
		if entries, err := os.ReadDir(materialization.root); fresh && err == nil && len(entries) == 1 && entries[0].Name() == datadir.MarkerFile {
			_ = os.Remove(filepath.Join(materialization.root, datadir.MarkerFile))
			_ = os.Remove(materialization.root)
		}
	}()
	if err != nil {
		return err
	}

	target := materialization.set.Path()
	if replace {
		// The caller saw this set's cache links fail.
	} else if err := materialization.set.Verify(); err == nil {
		return nil
	} else if !errors.Is(err, artifactset.ErrNotMaterialized) {
		return err
	}
	stage, err := os.MkdirTemp(layoutRoot, ".stage-")
	if err != nil {
		return fmt.Errorf("create artifact stage: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()

	var receiptFiles []artifactset.Record
	for _, model := range selected {
		if err := ctx.Err(); err != nil {
			return err
		}
		modelPath := filepath.ToSlash(filepath.Join(model.Prefix, model.Name))
		entry := hfcache.Entry{Repo: model.Repo, Revision: model.Revision, Name: model.Name, SHA256: model.SHA256}
		cached, err := (hfcache.Cache{Root: cacheRoot}).Ensure(ctx, entry, download)
		if err != nil {
			return fmt.Errorf("cache model %q: %w", model.Name, err)
		}
		modified, err := referenceModel(ctx, cached, stage, modelPath, model.SHA256)
		if err != nil {
			return fmt.Errorf("install cached model %q: %w", model.Name, err)
		}
		receiptFiles = append(receiptFiles, artifactset.Record{Path: modelPath, SHA256: model.SHA256, Size: cached.Size, Source: cached.Path, ModifiedNS: modified})
	}

	if materialization.patchID != "" {
		if err := ctx.Err(); err != nil {
			return err
		}
		if source == nil {
			return errors.New("upstream reader is required to fetch an absent template")
		}
		reader, err := source.Open(ctx, materialization.patchSource.Repo, materialization.patchSource.Revision, materialization.patchSource.File)
		if err != nil {
			return fmt.Errorf("fetch patch %q: %w", materialization.patchID, err)
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, maxPatchSource+1))
		closeErr := reader.Close()
		if readErr != nil {
			return fmt.Errorf("read patch %q: %w", materialization.patchID, readErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close patch %q download: %w", materialization.patchID, closeErr)
		}
		if len(data) > maxPatchSource {
			return fmt.Errorf("patch %q source exceeds %d bytes", materialization.patchID, maxPatchSource)
		}
		final, err := patch.Apply(materialization.patchSource.Transform, data)
		if err != nil {
			return fmt.Errorf("transform patch %q: %w", materialization.patchID, err)
		}
		patchPath := filepath.ToSlash(filepath.Join("patches", materialization.patchID, materialization.patch.File))
		expectedHash := materialization.entry.Patches[0].SHA256
		size, err := writeBytes(ctx, stage, patchPath, expectedHash, final)
		if err != nil {
			return fmt.Errorf("fetch patch %q: %w", materialization.patchID, err)
		}
		receiptFiles = append(receiptFiles, artifactset.Record{Path: patchPath, SHA256: expectedHash, Size: size})
	}

	cacheRoot, err = filepath.EvalSymlinks(cacheRoot)
	if err != nil {
		return err
	}
	receiptData, err := materialization.set.Receipt(receiptFiles, cacheRoot)
	if err != nil {
		return err
	}
	if _, err := writeBytes(ctx, stage, "receipt.json", sha256Hex(receiptData), receiptData); err != nil {
		return fmt.Errorf("write receipt: %w", err)
	}
	if err := syncTree(stage); err != nil {
		return fmt.Errorf("sync artifact stage: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if replace {
		if err := swapSet(stage, target, layoutRoot); err != nil {
			return err
		}
		committed = true
		return errors.Join(syncDirectory(layoutRoot), datadir.Mark(materialization.root))
	}
	if err := os.Rename(stage, target); err != nil {
		// Another identical fetch won. Its receipt may record a cache file
		// that was replaced since; refresh it rather than fail the converged set.
		verifyErr := materialization.set.Verify()
		if errors.Is(verifyErr, artifactset.ErrCacheChanged) {
			verifyErr = materialization.set.RefreshCacheReceipt(ctx)
		}
		if verifyErr != nil {
			return fmt.Errorf("publish artifact set: %w", errors.Join(err, verifyErr))
		}
		return nil
	}
	committed = true
	if err := syncDirectory(layoutRoot); err != nil {
		return fmt.Errorf("sync layout artifact directory: %w", err)
	}
	// The marker comes after the commit, so a failed fetch leaves no root.
	return datadir.Mark(materialization.root)
}

// swapSet moves the old set aside, publishes the stage and removes the old
// set. If publishing fails the old set returns; if the process stops between
// the renames, the next preparation finds no set and builds one.
func swapSet(stage, target, layoutRoot string) error {
	aside, err := os.MkdirTemp(layoutRoot, ".replaced-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(aside) }()
	old := filepath.Join(aside, "set")
	if err := os.Rename(target, old); err != nil {
		return fmt.Errorf("replace artifact set: %w", err)
	}
	if err := os.Rename(stage, target); err != nil {
		return fmt.Errorf("replace artifact set: %w", errors.Join(err, os.Rename(old, target)))
	}
	return nil
}

// referenceModel verifies a cache file and gives the engine a stable symbolic
// reference. Source bytes are never retained beneath the Temper root.
func referenceModel(ctx context.Context, model hfcache.File, stage, relative, expectedHash string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	file, err := os.Open(model.Path)
	if err != nil {
		return 0, err
	}
	before, err := file.Stat()
	if err != nil {
		file.Close()
		return 0, err
	}
	if !before.Mode().IsRegular() || before.Size() != model.Size {
		file.Close()
		return 0, errors.New("cached model is no longer a regular file of its recorded size")
	}
	hash := sha256.New()
	_, readErr := copyWithContext(ctx, hash, file)
	if err := errors.Join(readErr, file.Close()); err != nil {
		return 0, err
	}
	if actual := hex.EncodeToString(hash.Sum(nil)); actual != expectedHash {
		return 0, fmt.Errorf("%w: SHA-256 mismatch: got %s, want %s", artifactset.ErrContentMismatch, actual, expectedHash)
	}
	after, err := os.Lstat(model.Path)
	if err != nil {
		return 0, err
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || before.ModTime() != after.ModTime() {
		return 0, errors.New("cached model changed during verification")
	}
	path := filepath.Join(stage, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return 0, err
	}
	if err := os.Symlink(model.Path, path); err != nil {
		return 0, err
	}
	return before.ModTime().UnixNano(), nil
}

func prepareDirectories(root, final string) ([]string, error) {
	paths := []string{root}
	current := root
	relative, err := filepath.Rel(root, final)
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		paths = append(paths, current)
	}
	var created []string
	for _, path := range paths {
		made, err := ensureDirectory(path)
		if err != nil {
			for index := len(created) - 1; index >= 0; index-- {
				_ = os.Remove(created[index])
			}
			return nil, fmt.Errorf("prepare artifact directory %q: %w", path, err)
		}
		if made {
			created = append(created, path)
		}
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
		return false, err
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		if errors.Is(err, fs.ErrExist) {
			info, inspectErr := os.Lstat(path)
			if inspectErr == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
				return false, nil
			}
		}
		return false, err
	}
	return true, nil
}

func writeStream(ctx context.Context, stage, relative, expectedHash string, reader io.Reader) (int64, error) {
	path := filepath.Join(stage, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return 0, err
	}
	hash := sha256.New()
	written, copyErr := copyWithContext(ctx, io.MultiWriter(file, hash), reader)
	if copyErr == nil && hex.EncodeToString(hash.Sum(nil)) != expectedHash {
		copyErr = fmt.Errorf("SHA-256 mismatch: got %s, want %s", hex.EncodeToString(hash.Sum(nil)), expectedHash)
	}
	if copyErr == nil {
		copyErr = file.Sync()
	}
	closeErr := file.Close()
	if copyErr != nil {
		return 0, copyErr
	}
	if closeErr != nil {
		return 0, closeErr
	}
	return written, nil
}

func writeBytes(ctx context.Context, stage, relative, expectedHash string, data []byte) (int64, error) {
	actualHash := sha256Hex(data)
	if actualHash != expectedHash {
		return 0, fmt.Errorf("SHA-256 mismatch: got %s, want %s", actualHash, expectedHash)
	}
	return writeStream(ctx, stage, relative, expectedHash, bytes.NewReader(data))
}

func copyWithContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 128<<10)
	var written int64
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		count, readErr := source.Read(buffer)
		if count > 0 {
			output, writeErr := destination.Write(buffer[:count])
			written += int64(output)
			if writeErr != nil {
				return written, writeErr
			}
			if output != count {
				return written, io.ErrShortWrite
			}
		}
		if errors.Is(readErr, io.EOF) {
			return written, nil
		}
		if readErr != nil {
			return written, readErr
		}
	}
}

func syncTree(root string) error {
	var directories []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			directories = append(directories, path)
		}
		return nil
	})
	if err != nil {
		return err
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
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func sha256Hex(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
