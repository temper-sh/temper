package fetch_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/fetch"
	"github.com/temper-sh/temper/internal/lockfile"
)

func hfPaths(cache string, entry lockfile.Entry) (string, string) {
	repo := filepath.Join(cache, "models--"+strings.ReplaceAll(entry.Repo, "/", "--"))
	return filepath.Join(repo, "blobs", entry.Files[0].SHA256), filepath.Join(repo, "snapshots", entry.Revision, entry.Files[0].Name)
}

func seedHF(t *testing.T, cache string, entry lockfile.Entry, content string) string {
	t.Helper()
	blob, snapshot := hfPaths(cache, entry)
	for _, path := range []string{blob, snapshot} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(blob, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	link, err := filepath.Rel(filepath.Dir(snapshot), blob)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(link, snapshot); err != nil {
		t.Fatal(err)
	}
	return blob
}

func TestHFCacheReuseKeepsInstallationAndCacheIndependent(t *testing.T) {
	for _, remove := range []string{"cache", "installation"} {
		t.Run(remove, func(t *testing.T) {
			directory := t.TempDir()
			manifest, lock, entry := writeInputs(t, directory, false, "weights")
			root, cache := filepath.Join(directory, "temper"), filepath.Join(directory, "hub")
			blob := seedHF(t, cache, entry, "weights")
			options := fetch.Options{ManifestPath: manifest, LockPath: lock, Root: root, Layout: "coder", HFCache: cache}
			if _, err := fetch.Run(context.Background(), options, nil); err != nil {
				t.Fatal(err)
			}
			installed := filepath.Join(root, "artifacts", "layouts", "coder", entry.Digest(), "model", entry.Files[0].Name)
			original, err := os.Stat(blob)
			if err != nil {
				t.Fatal(err)
			}
			linked, err := os.Lstat(installed)
			if err != nil || !linked.Mode().IsRegular() || !os.SameFile(original, linked) {
				t.Fatalf("cache reuse duplicated bytes or left a symlink: %v", err)
			}
			if remove == "cache" {
				if err := os.RemoveAll(cache); err != nil {
					t.Fatal(err)
				}
				if result, err := fetch.Run(context.Background(), options, nil); err != nil || result.Changed {
					t.Fatalf("cache cleanup broke prepared installation: %+v %v", result, err)
				}
				assertFile(t, installed, "weights")
				if _, err := os.Lstat(cache); !os.IsNotExist(err) {
					t.Fatalf("unchanged replay recreated cache: %v", err)
				}
			} else {
				if err := os.RemoveAll(root); err != nil {
					t.Fatal(err)
				}
				assertFile(t, blob, "weights")
				if _, err := fetch.Run(context.Background(), options, nil); err != nil {
					t.Fatalf("installation removal broke shared cache: %v", err)
				}
			}
		})
	}
}

func TestFetchPopulatesSharedHFCacheAndKeepsItAfterTemplateFailure(t *testing.T) {
	directory := t.TempDir()
	manifest, lock, entry := writeInputs(t, directory, true, "weights")
	root, cache := filepath.Join(directory, "temper"), filepath.Join(directory, "hub")
	options := fetch.Options{ManifestPath: manifest, LockPath: lock, Root: root, Layout: "coder", HFCache: cache}
	modelKey := "owner/model@" + modelRevision + "/nested/model.gguf"
	source := &fakeSource{files: map[string]string{modelKey: "weights"}}
	if _, err := runFetch(context.Background(), options, source); err == nil || !strings.Contains(err.Error(), "fetch patch") {
		t.Fatalf("missing template error: %v", err)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("failed installation retained its stage: %v", err)
	}
	_, snapshot := hfPaths(cache, entry)
	assertFile(t, snapshot, "weights")
	// Only the missing template remains a download on retry.
	delete(source.files, modelKey)
	source.files["patches/templates@"+patchRevision+"/chat.jinja"] = "before\n" + oldGuard + "\nafter\n"
	if _, err := runFetch(context.Background(), options, source); err != nil {
		t.Fatal(err)
	}
	if source.openCalls != 3 {
		t.Fatalf("expected model, failed template, retried template; got %d upstream calls", source.openCalls)
	}
	installed := filepath.Join(root, "artifacts", "layouts", "coder", entry.Digest())
	assertFile(t, filepath.Join(installed, "patches", "stable-template", "chat.jinja"), "before\n"+newGuard+"\nafter\n")
	if _, err := os.Lstat(filepath.Join(cache, "models--patches--templates")); !os.IsNotExist(err) {
		t.Fatalf("Temper template was published as shared model material: %v", err)
	}
}

func TestCorruptHFCacheFailsVerificationWithoutChangingSharedBytes(t *testing.T) {
	directory := t.TempDir()
	manifest, lock, entry := writeInputs(t, directory, false, "weights")
	root, cache := filepath.Join(directory, "temper"), filepath.Join(directory, "hub")
	blob := seedHF(t, cache, entry, "spoiled") // Same length; size inspection alone cannot detect this.
	source := &fakeSource{}
	_, err := fetch.Run(context.Background(), fetch.Options{ManifestPath: manifest, LockPath: lock, Root: root, Layout: "coder", HFCache: cache}, source)
	if err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("corrupt cache accepted: %v", err)
	}
	if source.openCalls != 0 {
		t.Fatal("corrupt shared bytes triggered an implicit replacement download")
	}
	assertFile(t, blob, "spoiled")
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("bad cache left an installation: %v", err)
	}
}

func TestFetchDryRunCreatesNeitherInstallationNorSharedCache(t *testing.T) {
	directory := t.TempDir()
	manifest, lock, _ := writeInputs(t, directory, false, "weights")
	root, cache := filepath.Join(directory, "temper"), filepath.Join(directory, "hub")
	result, err := fetch.Run(context.Background(), fetch.Options{ManifestPath: manifest, LockPath: lock, Root: root, Layout: "coder", HFCache: cache, DryRun: true}, nil)
	if err != nil || !result.Changed || !result.DryRun {
		t.Fatalf("preview: %+v %v", result, err)
	}
	for _, path := range []string{root, cache} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("dry run created %s: %v", path, err)
		}
	}
}
