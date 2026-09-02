// Package archive owns the provider-neutral safe archive boundary used by
// software installation adapters. Adapters retain authority over artifact
// identity, publication, receipts, and lifecycle; this package only validates
// and materializes one already-downloaded tar.gz payload.
package archive

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// TarGzSpec is the complete extraction policy for one archive. Exact values
// are optional; maxima are always required so untrusted compressed input is
// bounded even when upstream metadata does not publish exact expanded facts.
type TarGzSpec struct {
	Root               string
	MaxEntries         int
	ExactEntries       int
	MaxUnpackedBytes   int64
	ExactUnpackedBytes int64
	Label              string
}

// Entry is a canonical inventory record shared by archive inspection,
// extraction verification, installation markers, and later drift checks.
type Entry struct {
	Path   string `json:"path"`
	Type   string `json:"type"`
	Mode   uint32 `json:"mode"`
	Size   int64  `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	Target string `json:"target,omitempty"`
}

// InspectTarGz fully consumes an archive and returns its canonical payload
// inventory without writing to the filesystem.
func InspectTarGz(ctx context.Context, archivePath string, spec TarGzSpec) ([]Entry, error) {
	if err := validateSpec(spec); err != nil {
		return nil, err
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", spec.label(), err)
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("open %s gzip stream: %w", spec.label(), err)
	}
	defer compressed.Close()
	reader := tar.NewReader(compressed)
	entries := map[string]Entry{}
	var unpacked int64
	headers := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read %s tar stream: %w", spec.label(), err)
		}
		headers++
		if headers > spec.MaxEntries+1 {
			return nil, fmt.Errorf("%s exceeds entry limit", spec.label())
		}
		if header.Mode < 0 || header.Mode&0o7000 != 0 {
			return nil, fmt.Errorf("%s path %q has privileged mode bits", spec.label(), header.Name)
		}
		relative, included, err := RelativePath(header.Name, spec.Root, header.Typeflag == tar.TypeDir)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", spec.label(), err)
		}
		if !included {
			continue
		}
		if _, exists := entries[relative]; exists {
			return nil, fmt.Errorf("%s repeats path %q", spec.label(), relative)
		}
		if len(entries) >= spec.MaxEntries {
			return nil, fmt.Errorf("%s exceeds entry limit", spec.label())
		}
		entry := Entry{Path: relative}
		switch header.Typeflag {
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || header.Size > spec.MaxUnpackedBytes-unpacked {
				return nil, fmt.Errorf("%s exceeds unpacked byte limit", spec.label())
			}
			unpacked += header.Size
			hash := sha256.New()
			written, err := copyWithContext(ctx, hash, reader)
			if err != nil || written != header.Size {
				return nil, fmt.Errorf("read %s file %q", spec.label(), relative)
			}
			entry.Type, entry.Mode, entry.Size, entry.SHA256 = "file", normalizedFileMode(header.Mode), header.Size, hex.EncodeToString(hash.Sum(nil))
		case tar.TypeDir:
			if header.Size != 0 {
				return nil, fmt.Errorf("%s directory %q contains data", spec.label(), relative)
			}
			entry.Type, entry.Mode = "directory", 0o755
		case tar.TypeSymlink:
			if header.Size != 0 || !ValidSymlink(relative, header.Linkname) {
				return nil, fmt.Errorf("%s symlink %q has unsafe target %q", spec.label(), relative, header.Linkname)
			}
			entry.Type, entry.Mode, entry.Target = "symlink", 0o777, header.Linkname
		default:
			return nil, fmt.Errorf("%s path %q has unsupported tar type %d", spec.label(), relative, header.Typeflag)
		}
		entries[relative] = entry
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%s payload is empty", spec.label())
	}
	var trailing [1]byte
	if count, trailingErr := compressed.Read(trailing[:]); count != 0 || !errors.Is(trailingErr, io.EOF) {
		if trailingErr != nil && !errors.Is(trailingErr, io.EOF) {
			return nil, fmt.Errorf("finish %s gzip stream: %w", spec.label(), trailingErr)
		}
		return nil, fmt.Errorf("%s has trailing decompressed content", spec.label())
	}
	addImplicitDirectories(entries)
	if len(entries) > spec.MaxEntries {
		return nil, fmt.Errorf("%s exceeds entry limit", spec.label())
	}
	if spec.ExactEntries > 0 && len(entries) != spec.ExactEntries {
		return nil, fmt.Errorf("%s installed entry count is %d, want %d", spec.label(), len(entries), spec.ExactEntries)
	}
	if spec.ExactUnpackedBytes > 0 && unpacked != spec.ExactUnpackedBytes {
		return nil, fmt.Errorf("%s unpacked size is %d, want %d", spec.label(), unpacked, spec.ExactUnpackedBytes)
	}
	if err := validateParents(entries, spec.label()); err != nil {
		return nil, err
	}
	result := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}

// ExtractTarGz materializes an inspected archive into an absent or empty real
// directory and rechecks every file while writing it.
func ExtractTarGz(ctx context.Context, archivePath, destination string, spec TarGzSpec, expected []Entry) error {
	if err := validateSpec(spec); err != nil {
		return err
	}
	if len(expected) == 0 {
		return fmt.Errorf("%s expected inventory is empty", spec.label())
	}
	if err := prepareEmptyDestination(destination, spec.label()); err != nil {
		return err
	}
	byPath := make(map[string]Entry, len(expected))
	for _, entry := range expected {
		if !ValidPath(entry.Path, false) {
			return fmt.Errorf("%s expected path %q is unsafe", spec.label(), entry.Path)
		}
		if _, exists := byPath[entry.Path]; exists {
			return fmt.Errorf("%s expected inventory repeats path %q", spec.label(), entry.Path)
		}
		byPath[entry.Path] = entry
		if entry.Type == "directory" {
			if err := ensureRealDirectory(destination, filepath.Join(destination, filepath.FromSlash(entry.Path))); err != nil {
				return fmt.Errorf("create %s directory %q: %w", spec.label(), entry.Path, err)
			}
		}
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open %s for extraction: %w", spec.label(), err)
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("open %s gzip stream for extraction: %w", spec.label(), err)
	}
	defer compressed.Close()
	reader := tar.NewReader(compressed)
	seen := map[string]bool{}
	var symlinks []Entry
	headers := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read %s during extraction: %w", spec.label(), err)
		}
		headers++
		if headers > spec.MaxEntries+1 {
			return fmt.Errorf("%s exceeds entry limit during extraction", spec.label())
		}
		if header.Mode < 0 || header.Mode&0o7000 != 0 {
			return fmt.Errorf("%s path %q has privileged mode bits during extraction", spec.label(), header.Name)
		}
		relative, included, err := RelativePath(header.Name, spec.Root, header.Typeflag == tar.TypeDir)
		if err != nil {
			return fmt.Errorf("%s: %w", spec.label(), err)
		}
		if !included {
			continue
		}
		entry, ok := byPath[relative]
		if !ok || seen[relative] || !headerMatches(entry, header) {
			return fmt.Errorf("%s path %q differs from inspected inventory", spec.label(), relative)
		}
		seen[relative] = true
		switch entry.Type {
		case "file":
			destinationPath := filepath.Join(destination, filepath.FromSlash(relative))
			if err := ensureRealDirectory(destination, filepath.Dir(destinationPath)); err != nil {
				return err
			}
			output, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fs.FileMode(entry.Mode))
			if err != nil {
				return err
			}
			hash := sha256.New()
			written, copyErr := copyWithContext(ctx, io.MultiWriter(output, hash), reader)
			syncErr, closeErr := output.Sync(), output.Close()
			if copyErr != nil || syncErr != nil || closeErr != nil || written != entry.Size || hex.EncodeToString(hash.Sum(nil)) != entry.SHA256 {
				return fmt.Errorf("extract %s file %q", spec.label(), relative)
			}
		case "symlink":
			symlinks = append(symlinks, entry)
		}
	}
	for _, entry := range expected {
		if entry.Type != "directory" && !seen[entry.Path] {
			return fmt.Errorf("%s path %q disappeared during extraction", spec.label(), entry.Path)
		}
	}
	for _, entry := range symlinks {
		destinationPath := filepath.Join(destination, filepath.FromSlash(entry.Path))
		if err := ensureRealDirectory(destination, filepath.Dir(destinationPath)); err != nil {
			return err
		}
		if err := os.Symlink(entry.Target, destinationPath); err != nil {
			return fmt.Errorf("create %s symlink %q: %w", spec.label(), entry.Path, err)
		}
	}
	for _, entry := range symlinks {
		resolved, err := filepath.EvalSymlinks(filepath.Join(destination, filepath.FromSlash(entry.Path)))
		if err != nil || !withinResolvedRoot(destination, resolved) {
			return fmt.Errorf("%s symlink %q is dangling, cyclic, or escaping", spec.label(), entry.Path)
		}
	}
	return nil
}

// ScanTree creates the same canonical inventory from an installed tree. It
// does not follow symlinks and refuses links that do not resolve within root.
func ScanTree(ctx context.Context, root, label string) ([]Entry, error) {
	if label == "" {
		label = "installed tree"
	}
	if !realDirectory(root) {
		return nil, fmt.Errorf("%s is not a real directory", label)
	}
	var entries []Entry
	err := filepath.WalkDir(root, func(current string, directoryEntry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if current == root {
			return nil
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if !ValidPath(relative, false) {
			return fmt.Errorf("%s path %q is unsafe", label, relative)
		}
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		entry := Entry{Path: relative, Mode: uint32(info.Mode().Perm())}
		switch {
		case info.IsDir():
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("%s directory %q is a symlink", label, relative)
			}
			entry.Type = "directory"
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(current)
			if err != nil || !ValidSymlink(relative, filepath.ToSlash(target)) {
				return fmt.Errorf("%s symlink %q has an unsafe target", label, relative)
			}
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil || !withinResolvedRoot(root, resolved) {
				return fmt.Errorf("%s symlink %q is dangling, cyclic, or escaping", label, relative)
			}
			entry.Type, entry.Mode, entry.Target = "symlink", 0o777, target
		case info.Mode().IsRegular():
			entry.Type, entry.Size = "file", info.Size()
			hash, err := hashFile(ctx, current)
			if err != nil {
				return err
			}
			entry.SHA256 = hash
		default:
			return fmt.Errorf("%s path %q is not a regular file, directory, or symlink", label, relative)
		}
		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%s is empty", label)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

// RelativePath strips one declared archive root and rejects every spelling
// that could escape or behave differently across platforms.
func RelativePath(name, root string, directory bool) (string, bool, error) {
	cleanedName := strings.TrimSuffix(name, "/")
	for strings.HasPrefix(cleanedName, "./") {
		cleanedName = strings.TrimPrefix(cleanedName, "./")
	}
	if cleanedName == "." && root == "." && directory {
		return "", false, nil
	}
	if !ValidPath(cleanedName, false) {
		return "", false, fmt.Errorf("archive path %q is unsafe", name)
	}
	if root == "." {
		return cleanedName, true, nil
	}
	if cleanedName == root {
		if !directory {
			return "", false, fmt.Errorf("archive root %q is not a directory", root)
		}
		return "", false, nil
	}
	prefix := root + "/"
	if !strings.HasPrefix(cleanedName, prefix) {
		return "", false, fmt.Errorf("archive path %q is outside archive root %q", name, root)
	}
	return strings.TrimPrefix(cleanedName, prefix), true, nil
}

func ValidPath(value string, allowDot bool) bool {
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, `\`) || strings.ContainsAny(value, "\r\n\x00") || path.Clean(value) != value {
		return false
	}
	if value == "." {
		return allowDot
	}
	return value != ".." && !strings.HasPrefix(value, "../")
}

func ValidSymlink(relative, target string) bool {
	if target == "" || path.IsAbs(target) || strings.Contains(target, `\`) || strings.ContainsAny(target, "\r\n\x00") {
		return false
	}
	resolved := path.Clean(path.Join(path.Dir(relative), target))
	return resolved != ".." && !strings.HasPrefix(resolved, "../")
}

func validateSpec(spec TarGzSpec) error {
	if !ValidPath(spec.Root, true) {
		return errors.New("archive root is invalid")
	}
	if spec.MaxEntries <= 0 || spec.ExactEntries < 0 || spec.ExactEntries > spec.MaxEntries {
		return errors.New("archive entry bounds are invalid")
	}
	if spec.MaxUnpackedBytes <= 0 || spec.ExactUnpackedBytes < 0 || spec.ExactUnpackedBytes > spec.MaxUnpackedBytes {
		return errors.New("archive byte bounds are invalid")
	}
	return nil
}

func (s TarGzSpec) label() string {
	if s.Label == "" {
		return "archive"
	}
	return s.Label
}

func normalizedFileMode(mode int64) uint32 {
	if mode&0o111 != 0 {
		return 0o755
	}
	return 0o644
}

func addImplicitDirectories(entries map[string]Entry) {
	paths := make([]string, 0, len(entries))
	for value := range entries {
		paths = append(paths, value)
	}
	for _, value := range paths {
		for parent := path.Dir(value); parent != "."; parent = path.Dir(parent) {
			if _, exists := entries[parent]; !exists {
				entries[parent] = Entry{Path: parent, Type: "directory", Mode: 0o755}
			}
		}
	}
}

func validateParents(entries map[string]Entry, label string) error {
	for value, entry := range entries {
		for parent := path.Dir(value); parent != "."; parent = path.Dir(parent) {
			if entries[parent].Type != "directory" {
				return fmt.Errorf("%s path %q has non-directory parent %q", label, value, parent)
			}
		}
		if entry.Type != "symlink" {
			continue
		}
		target := path.Clean(path.Join(path.Dir(value), entry.Target))
		if _, exists := entries[target]; !exists {
			return fmt.Errorf("%s symlink %q targets missing path %q", label, value, target)
		}
		seen := map[string]bool{value: true}
		for entries[target].Type == "symlink" {
			if seen[target] {
				return fmt.Errorf("%s symlink %q belongs to a cycle", label, value)
			}
			seen[target] = true
			target = path.Clean(path.Join(path.Dir(target), entries[target].Target))
			if _, exists := entries[target]; !exists {
				return fmt.Errorf("%s symlink %q resolves through missing path %q", label, value, target)
			}
		}
	}
	return nil
}

func headerMatches(entry Entry, header *tar.Header) bool {
	switch entry.Type {
	case "file":
		return (header.Typeflag == tar.TypeReg || header.Typeflag == tar.TypeRegA) && header.Size == entry.Size && normalizedFileMode(header.Mode) == entry.Mode
	case "directory":
		return header.Typeflag == tar.TypeDir && header.Size == 0
	case "symlink":
		return header.Typeflag == tar.TypeSymlink && header.Size == 0 && header.Linkname == entry.Target
	default:
		return false
	}
}

func prepareEmptyDestination(destination, label string) error {
	info, err := os.Lstat(destination)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.Mkdir(destination, 0o755); err != nil {
			return fmt.Errorf("create %s destination: %w", label, err)
		}
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s destination is not a real directory", label)
	}
	entries, err := os.ReadDir(destination)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("%s destination is not empty", label)
	}
	return nil
}

func ensureRealDirectory(root, directory string) error {
	relative, err := filepath.Rel(root, directory)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return errors.New("archive extraction parent escapes destination")
	}
	current := root
	if relative == "." {
		return nil
	}
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			if err := os.Mkdir(current, 0o755); err != nil {
				return err
			}
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("archive extraction parent %q is not a real directory", current)
		}
	}
	return nil
}

func realDirectory(value string) bool {
	info, err := os.Lstat(value)
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

func withinResolvedRoot(root, candidate string) bool {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(resolvedRoot, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func hashFile(ctx context.Context, value string) (string, error) {
	file, err := os.Open(value)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := copyWithContext(ctx, hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
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
