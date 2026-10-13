// Package hfcache inspects the shared Hugging Face Hub cache and delegates
// downloads to the official HF client. It never writes or prunes cache files.
package hfcache

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Entry struct {
	Repo, Revision, Name, SHA256 string
}

type File struct {
	Path string // Resolved regular file, including HF's shared-blob symlink layout.
	Size int64
}

type Cache struct{ Root string }

// DefaultRoot reads only environment and home-directory configuration.
func DefaultRoot() (string, error) {
	home, _ := os.UserHomeDir()
	return ResolveRoot(home, os.Getenv)
}

// ResolveRoot follows HF_HUB_CACHE, its legacy alias, HF_HOME and XDG_CACHE_HOME.
// The caller supplies environment values; resolving a path creates nothing.
func ResolveRoot(home string, getenv func(string) string) (string, error) {
	root := getenv("HF_HUB_CACHE")
	if root == "" {
		root = getenv("HUGGINGFACE_HUB_CACHE")
	}
	if root == "" {
		base := getenv("HF_HOME")
		if base == "" {
			base = getenv("XDG_CACHE_HOME")
			if base == "" {
				if home == "" {
					return "", errors.New("Hugging Face cache needs a home directory or HF_HUB_CACHE")
				}
				base = filepath.Join(home, ".cache")
			}
			base = filepath.Join(base, "huggingface")
		}
		root = filepath.Join(base, "hub")
	}
	root = os.Expand(root, func(key string) string {
		if value := getenv(key); value != "" {
			return value
		}
		return "$" + key
	})
	if root == "~" || strings.HasPrefix(root, "~/") {
		if home == "" {
			return "", errors.New("cannot expand HF cache path without a home directory")
		}
		root = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(root, "~"), "/"))
	}
	return filepath.Abs(root)
}

var (
	repoPattern     = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)
	revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	hashPattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func (e Entry) validate() error {
	if !repoPattern.MatchString(e.Repo) || strings.Contains(e.Repo, "--") {
		return errors.New("HF cache repository must be an unambiguous owner/name")
	}
	if !revisionPattern.MatchString(e.Revision) || !hashPattern.MatchString(e.SHA256) {
		return errors.New("HF cache requires an exact revision and SHA-256")
	}
	if !fs.ValidPath(e.Name) || e.Name == "." || strings.ContainsAny(e.Name, "\\\x00") {
		return errors.New("HF cache filename must be a relative repository path")
	}
	return nil
}

func (e Entry) repository() string { return "models--" + strings.ReplaceAll(e.Repo, "/", "--") }
func (e Entry) snapshot() string {
	return filepath.Join(e.repository(), "snapshots", e.Revision, filepath.FromSlash(e.Name))
}
func (e Entry) blob() string { return filepath.Join(e.repository(), "blobs", e.SHA256) }

// Inspect checks exact snapshot presence (or the same repo's SHA-256 blob),
// containment and regular-file shape. Preparation must still verify the bytes.
func (c Cache) Inspect(e Entry) (File, bool, error) {
	if err := e.validate(); err != nil {
		return File{}, false, err
	}
	if c.Root == "" {
		return File{}, false, nil
	}
	root, err := os.OpenRoot(c.Root)
	if errors.Is(err, fs.ErrNotExist) {
		return File{}, false, nil
	}
	if err != nil {
		return File{}, false, err
	}
	defer root.Close()
	for _, name := range []string{e.snapshot(), e.blob()} {
		info, err := root.Stat(name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return File{}, false, fmt.Errorf("inspect HF cache %s: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return File{}, false, fmt.Errorf("HF cache file %s is not regular", name)
		}
		path, err := filepath.EvalSymlinks(filepath.Join(c.Root, name))
		if err != nil {
			return File{}, false, err
		}
		base, err := filepath.EvalSymlinks(c.Root)
		if err != nil {
			return File{}, false, err
		}
		rel, err := filepath.Rel(base, path)
		if err != nil || !filepath.IsLocal(rel) {
			return File{}, false, errors.New("HF cache file escapes the cache root")
		}
		return File{Path: path, Size: info.Size()}, true, nil
	}
	return File{}, false, nil
}
