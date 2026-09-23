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
	"syscall"

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
	ManifestPath  string
	LockPath      string
	Root          string
	Layout        string
	DryRun        bool
	HFCache       string
	DownloadModel hfcache.DownloadFunc
}

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
	if err := materialization.set.Verify(); err == nil {
		return result, nil
	} else if !errors.Is(err, artifactset.ErrNotMaterialized) {
		return Result{}, err
	}
	result.Changed = true
	if options.DryRun {
		return result, nil
	}
	if err := publish(ctx, materialization, source, options.HFCache, options.DownloadModel); err != nil {
		return Result{}, err
	}
	return result, nil
}

func buildPlan(options Options) (plan, error) {
	if options.ManifestPath == "" || options.LockPath == "" || options.Root == "" || options.Layout == "" {
		return plan{}, errors.New("manifest, lock, root and layout are required")
	}
	root, err := datadir.Resolve(options.Root)
	if err != nil {
		return plan{}, err
	}
	manifestData, err := os.ReadFile(options.ManifestPath)
	if err != nil {
		return plan{}, fmt.Errorf("read manifest: %w", err)
	}
	document, err := manifest.Parse(manifestData)
	if err != nil {
		return plan{}, err
	}
	lockData, err := os.ReadFile(options.LockPath)
	if err != nil {
		return plan{}, fmt.Errorf("read lock: %w", err)
	}
	locked, err := lockfile.Parse(lockData)
	if err != nil {
		return plan{}, err
	}
	layout, ok := document.Layouts[options.Layout]
	if !ok {
		return plan{}, fmt.Errorf("layout %q is not declared in the manifest", options.Layout)
	}
	entry, ok := locked.Entry(options.Layout)
	if !ok {
		return plan{}, fmt.Errorf("layout %q has no lock entry; run temper resolve first", options.Layout)
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

func publish(ctx context.Context, materialization plan, source upstream.Reader, cacheRoot string, download hfcache.DownloadFunc) (returnErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	hashes := make([]string, 0, len(materialization.entry.Files))
	for _, model := range materialization.entry.Files {
		hashes = append(hashes, model.SHA256)
	}
	models, err := artifactset.FindModels(materialization.root, hashes)
	if err != nil {
		return err
	}
	layoutRoot := filepath.Join(materialization.root, "artifacts", "layouts", materialization.layoutID)
	created, err := prepareDirectories(materialization.root, layoutRoot)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if returnErr == nil || committed {
			return
		}
		for index := len(created) - 1; index >= 0; index-- {
			_ = os.Remove(created[index])
		}
	}()

	target := materialization.set.Path()
	if err := materialization.set.Verify(); err == nil {
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
	for _, model := range materialization.entry.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		modelPath := filepath.ToSlash(filepath.Join("model", model.Name))
		if existing, ok := models[model.SHA256]; ok {
			if err := linkModel(ctx, existing, stage, modelPath, model.SHA256, false); err != nil {
				return fmt.Errorf("reuse model %q: %w", model.Name, err)
			}
			receiptFiles = append(receiptFiles, artifactset.Record{Path: modelPath, SHA256: model.SHA256, Size: existing.Size})
			continue
		}
		entry := hfcache.Entry{Repo: materialization.entry.Repo, Revision: materialization.entry.Revision, Name: model.Name, SHA256: model.SHA256}
		cached, err := (hfcache.Cache{Root: cacheRoot}).Ensure(ctx, entry, download)
		if err != nil {
			return fmt.Errorf("cache model %q: %w", model.Name, err)
		}
		if err := linkModel(ctx, artifactset.ModelFile{Path: cached.Path, Size: cached.Size}, stage, modelPath, model.SHA256, true); err != nil {
			return fmt.Errorf("install cached model %q: %w", model.Name, err)
		}
		receiptFiles = append(receiptFiles, artifactset.Record{Path: modelPath, SHA256: model.SHA256, Size: cached.Size})
		models[model.SHA256] = artifactset.ModelFile{Path: filepath.Join(stage, filepath.FromSlash(modelPath)), Size: cached.Size}
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

	receiptData, err := materialization.set.Receipt(receiptFiles)
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
	if err := os.Rename(stage, target); err != nil {
		if verifyErr := materialization.set.Verify(); verifyErr != nil {
			return fmt.Errorf("publish artifact set: %w", err)
		}
		return nil
	}
	committed = true
	if err := syncDirectory(layoutRoot); err != nil {
		return fmt.Errorf("sync layout artifact directory: %w", err)
	}
	return nil
}

// linkModel keeps the composition's existing regular-file path while sharing
// model storage. Hash the staged link, not a pathname that could be replaced
// between verification and linking. Failure only removes this private stage.
func linkModel(ctx context.Context, model artifactset.ModelFile, stage, relative, expectedHash string, crossFilesystemCopy bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path := filepath.Join(stage, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.Link(model.Path, path); err != nil {
		if crossFilesystemCopy && errors.Is(err, syscall.EXDEV) {
			return copyModel(ctx, model, stage, relative, expectedHash)
		}
		return fmt.Errorf("link existing model: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != model.Size {
		return errors.New("existing model is no longer a regular file of its recorded size")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, readErr := copyWithContext(ctx, hash, file)
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return err
	}
	if actual := hex.EncodeToString(hash.Sum(nil)); actual != expectedHash {
		return fmt.Errorf("%w: SHA-256 mismatch: got %s, want %s", artifactset.ErrContentMismatch, actual, expectedHash)
	}
	return nil
}

func copyModel(ctx context.Context, model artifactset.ModelFile, stage, relative, expectedHash string) error {
	info, err := os.Lstat(model.Path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != model.Size {
		return errors.New("cached model is no longer a regular file of its inspected size")
	}
	source, err := os.Open(model.Path)
	if err != nil {
		return err
	}
	written, copyErr := writeStream(ctx, stage, relative, expectedHash, source)
	if err := errors.Join(copyErr, source.Close()); err != nil {
		return err
	}
	if written != model.Size {
		return errors.New("cached model size changed during copy")
	}
	return nil
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
