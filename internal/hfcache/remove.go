package hfcache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// CheckFileRemoval reads the installed client's advertised command surface.
// Finding hf alone does not establish support for named-file cache deletion.
func CheckFileRemoval(ctx context.Context) error {
	path, err := FindClient(exec.LookPath)
	if err != nil {
		return err
	}
	// Help is local introspection, not model work or a cache scan. Bound a
	// broken launcher so a prerequisite check cannot hang configuration.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, path, "cache", "rm", "--help")
	command.Env = append(os.Environ(), "HF_HUB_DISABLE_TELEMETRY=1", "HF_HUB_DISABLE_UPDATE_CHECK=1", "NO_COLOR=1")
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	err = runDownload(command)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	help := strings.Join(strings.Fields(output.String()), " ")
	if err != nil || !strings.Contains(help, "hf://") || !strings.Contains(help, "repo or file") {
		return errors.New("shared weight deletion requires hf cache rm with named-file URI support; upgrade your Hugging Face CLI (for example: brew upgrade hf), or keep the shared weights")
	}
	return nil
}

// NamedFiles reads the named repository path in every cached revision. Unlike
// Inspect, it does not fall back to a blob: another filename may share that
// blob after this name has been removed, and that is not a deletion failure.
func (c Cache) NamedFiles(e Entry) ([]File, error) {
	if err := e.validate(); err != nil {
		return nil, err
	}
	if c.Root == "" {
		return nil, nil
	}
	root, err := os.OpenRoot(c.Root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	snapshots := filepath.Join(e.repository(), "snapshots")
	dir, err := root.Open(snapshots)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	revisions, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	var files []File
	for _, rev := range revisions {
		if !revisionPattern.MatchString(rev.Name()) {
			continue
		}
		name := filepath.Join(snapshots, rev.Name(), filepath.FromSlash(e.Name))
		if _, err := root.Lstat(name); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		info, err := root.Stat(name)
		if err != nil {
			return nil, fmt.Errorf("inspect HF file %s: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("HF cache file %s is not regular", name)
		}
		files = append(files, File{Path: filepath.Join(c.Root, name), Size: info.Size()})
	}
	slices.SortFunc(files, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	return files, nil
}

// RemoveFile deletes one explicitly named file from cached revisions. The
// official client owns snapshot/blob reference accounting; no shell is involved.
// The caller protects every currently selected use of this repository/file pair.
func RemoveFile(ctx context.Context, cache string, entry Entry, out io.Writer) error {
	path, args, err := removeCommand(cache, entry, exec.LookPath)
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, path, args...)
	command.Env = append(os.Environ(), "HF_HUB_DISABLE_TELEMETRY=1", "HF_HUB_DISABLE_UPDATE_CHECK=1")
	command.Stdout, command.Stderr = out, out
	if err := runDownload(command); err != nil {
		return fmt.Errorf("remove shared HF file %s/%s: %w", entry.Repo, entry.Name, err)
	}
	return nil
}

func removeCommand(cache string, entry Entry, lookPath func(string) (string, error)) (string, []string, error) {
	if !filepath.IsAbs(cache) {
		return "", nil, fmt.Errorf("HF removal requires an absolute reviewed cache path")
	}
	if err := entry.validate(); err != nil {
		return "", nil, err
	}
	path, err := FindClient(lookPath)
	if err != nil {
		return "", nil, err
	}
	// HF's URI grammar takes a literal path; it does not URL-decode % escapes.
	// This is one argv element, never shell text.
	uri := "hf://models/" + entry.Repo + "/" + entry.Name
	return path, []string{"cache", "rm", uri, "--cache-dir", cache, "--yes"}, nil
}
