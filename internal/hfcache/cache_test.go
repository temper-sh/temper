//go:build darwin || linux

package hfcache_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/hfcache"
)

func entry(content string) hfcache.Entry {
	hash := sha256.Sum256([]byte(content))
	return hfcache.Entry{Repo: "example/model", Revision: strings.Repeat("a", 40), Name: "weights/model.gguf", SHA256: hex.EncodeToString(hash[:])}
}

func paths(root string, e hfcache.Entry) (blob, snapshot string) {
	repo := "models--" + strings.ReplaceAll(e.Repo, "/", "--")
	return filepath.Join(root, repo, "blobs", e.SHA256), filepath.Join(root, repo, "snapshots", e.Revision, filepath.FromSlash(e.Name))
}

func put(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func pointer(t *testing.T, path, target string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(filepath.Dir(path), target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(rel, path); err != nil {
		t.Fatal(err)
	}
}

func TestCacheLocationFollowsHFConventions(t *testing.T) {
	home := t.TempDir()
	for _, tc := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{"default", nil, filepath.Join(home, ".cache", "huggingface", "hub")},
		{"xdg", map[string]string{"XDG_CACHE_HOME": filepath.Join(home, "xdg")}, filepath.Join(home, "xdg", "huggingface", "hub")},
		{"hf home wins", map[string]string{"XDG_CACHE_HOME": "ignored", "HF_HOME": filepath.Join(home, "hf")}, filepath.Join(home, "hf", "hub")},
		{"legacy wins over home", map[string]string{"HF_HOME": "ignored", "HUGGINGFACE_HUB_CACHE": filepath.Join(home, "legacy")}, filepath.Join(home, "legacy")},
		{"hub wins over all", map[string]string{"HF_HOME": "ignored", "HUGGINGFACE_HUB_CACHE": "ignored", "HF_HUB_CACHE": filepath.Join(home, "shared")}, filepath.Join(home, "shared")},
		{"expansion", map[string]string{"HF_HUB_CACHE": "~/$CACHE_NAME", "CACHE_NAME": "shared"}, filepath.Join(home, "shared")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := hfcache.ResolveRoot(home, func(key string) string { return tc.env[key] })
			if err != nil || got != tc.want {
				t.Fatalf("path=%q err=%v want %q", got, err, tc.want)
			}
			if _, err := os.Lstat(got); !os.IsNotExist(err) {
				t.Fatalf("resolution created a cache: %v", err)
			}
		})
	}
}

func TestNamedFilesTrackAllRevisionsWithoutClaimingSharedBlobs(t *testing.T) {
	cache := hfcache.Cache{Root: t.TempDir()}
	e := entry("shared weights")
	e.Name = "weights/model space%20.gguf"
	blob, first := paths(cache.Root, e)
	put(t, blob, "shared weights")
	pointer(t, first, blob)
	other := e
	other.Revision = strings.Repeat("c", 40)
	_, second := paths(cache.Root, other)
	pointer(t, second, blob)
	other.Name = "weights/keep.gguf"
	_, kept := paths(cache.Root, other)
	pointer(t, kept, blob)
	files, err := cache.NamedFiles(e)
	if err != nil || len(files) != 2 || files[0].Path != first || files[1].Path != second {
		t.Fatal(files, err)
	}
	for _, f := range files {
		if f.Size != int64(len("shared weights")) {
			t.Fatal("reported link length as file size", f)
		}
	}
	if err := os.Remove(first); err != nil {
		t.Fatal(err)
	}
	files, err = cache.NamedFiles(e)
	if err != nil || len(files) != 1 || files[0].Path != second {
		t.Fatal("missed another revision", files, err)
	}
	if err := os.Remove(second); err != nil {
		t.Fatal(err)
	}
	files, err = cache.NamedFiles(e)
	if err != nil || len(files) != 0 {
		t.Fatal("shared blob treated as undeleted filename", files, err)
	}
	if data, err := os.ReadFile(kept); err != nil || string(data) != "shared weights" {
		t.Fatal("inspection damaged shared content", err)
	}
}

func TestNamedFilesRefusesUnverifiableCachePaths(t *testing.T) {
	for _, kind := range []string{"dangling", "outside", "directory"} {
		t.Run(kind, func(t *testing.T) {
			cache := hfcache.Cache{Root: t.TempDir()}
			e := entry("weights")
			_, snapshot := paths(cache.Root, e)
			switch kind {
			case "dangling":
				pointer(t, snapshot, filepath.Join(cache.Root, "missing"))
			case "outside":
				outside := filepath.Join(t.TempDir(), "weights")
				put(t, outside, "weights")
				pointer(t, snapshot, outside)
			case "directory":
				if err := os.MkdirAll(snapshot, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if files, err := cache.NamedFiles(e); err == nil {
				t.Fatal("unverifiable cache counted as deleted", files)
			}
		})
	}
}

func TestInspectHFCacheLayoutsWithoutMutating(t *testing.T) {
	for _, layout := range []string{"snapshot", "regular snapshot", "shared xet blob", "blob without snapshot", "absent"} {
		t.Run(layout, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "hub")
			e := entry("weights")
			blob, snapshot := paths(root, e)
			if layout == "regular snapshot" {
				put(t, snapshot, "weights")
			}
			if layout == "snapshot" || layout == "blob without snapshot" {
				put(t, blob, "weights")
			}
			if layout == "shared xet blob" {
				shared := filepath.Join(root, "blobs", "ab", strings.Repeat("b", 64))
				put(t, shared, "weights")
				pointer(t, blob, shared)
			}
			if layout == "snapshot" || layout == "shared xet blob" {
				pointer(t, snapshot, blob)
			}
			file, found, err := (hfcache.Cache{Root: root}).Inspect(e)
			if err != nil {
				t.Fatal(err)
			}
			if found != (layout != "absent") {
				t.Fatalf("found=%v", found)
			}
			if found {
				info, err := os.Lstat(file.Path)
				if err != nil || !info.Mode().IsRegular() || file.Size != 7 {
					t.Fatalf("invalid candidate: %+v %v", file, err)
				}
			} else if _, err := os.Lstat(root); !os.IsNotExist(err) {
				t.Fatal("inspection created absent cache")
			}
		})
	}
}

func TestInspectRejectsEscapingOrNonregularMaterial(t *testing.T) {
	for _, kind := range []string{"escape", "directory", "path traversal"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			root := filepath.Join(base, "hub")
			e := entry("weights")
			_, snapshot := paths(root, e)
			switch kind {
			case "escape":
				outside := filepath.Join(base, "outside")
				put(t, outside, "weights")
				pointer(t, snapshot, outside)
			case "directory":
				if err := os.MkdirAll(snapshot, 0o755); err != nil {
					t.Fatal(err)
				}
			case "path traversal":
				e.Name = "../../outside"
			}
			if _, _, err := (hfcache.Cache{Root: root}).Inspect(e); err == nil {
				t.Fatal("unsafe cache candidate accepted")
			}
		})
	}
}
