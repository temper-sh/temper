package fetch_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/temper-sh/temper/internal/artifactset"
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

func TestSourceWeightsLiveOnlyInHFCacheAndMissingCacheIsRecoverable(t *testing.T) {
	for _, remove := range []string{"cache", "installation"} {
		t.Run(remove, func(t *testing.T) {
			directory := t.TempDir()
			manifest, lock, entry := writeInputs(t, directory, false, "weights")
			root, cache := filepath.Join(directory, "temper"), filepath.Join(directory, "hub")
			blob := seedHF(t, cache, entry, "weights")
			options := fetch.Options{Manifest: readManifest(t, manifest), Lock: readLock(t, lock), Root: root, Layout: "coder", HFCache: cache}
			if _, err := fetch.Run(context.Background(), options, nil); err != nil {
				t.Fatal(err)
			}
			installed := filepath.Join(root, "artifacts", "layouts", "coder", entry.Digest(), "model", entry.Files[0].Name)
			original, err := os.Stat(blob)
			if err != nil {
				t.Fatal(err)
			}
			linked, err := os.Lstat(installed)
			if err != nil || linked.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("prepared model must be a symbolic reference, not retained weights: %v", err)
			}
			resolved, err := os.Stat(installed)
			if err != nil || !os.SameFile(original, resolved) {
				t.Fatalf("model does not read the cache file: %v", err)
			}
			if remove == "cache" {
				if err := os.RemoveAll(cache); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(installed); !os.IsNotExist(err) {
					t.Fatalf("Temper retained weights after cache removal: %v", err)
				}
				options.DryRun = true
				if result, err := fetch.Run(context.Background(), options, nil); err != nil || !result.Changed {
					t.Fatalf("missing cache preview: %+v %v", result, err)
				}
				if _, err := os.Lstat(cache); !os.IsNotExist(err) {
					t.Fatalf("preview recreated cache: %v", err)
				}
				options.DryRun = false
				source := &fakeSource{files: map[string]string{"owner/model@" + modelRevision + "/nested/model.gguf": "weights"}}
				if result, err := runFetch(context.Background(), options, source); err != nil || !result.Changed {
					t.Fatalf("cache restoration: %+v %v", result, err)
				}
				assertFile(t, installed, "weights")
				if result, err := fetch.Run(context.Background(), options, nil); err != nil || result.Changed {
					t.Fatalf("restored cache is not second-run clean: %+v %v", result, err)
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
	options := fetch.Options{Manifest: readManifest(t, manifest), Lock: readLock(t, lock), Root: root, Layout: "coder", HFCache: cache}
	modelKey := "owner/model@" + modelRevision + "/nested/model.gguf"
	source := &fakeSource{files: map[string]string{modelKey: "weights"}}
	if _, err := runFetch(context.Background(), options, source); err == nil || !strings.Contains(err.Error(), "fetch patch") {
		t.Fatalf("missing template error: %v", err)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("failed installation retained its stage: %v", err)
	}
	blob, _ := hfPaths(cache, entry)
	assertFile(t, blob, "weights")
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
	_, err := fetch.Run(context.Background(), fetch.Options{Manifest: readManifest(t, manifest), Lock: readLock(t, lock), Root: root, Layout: "coder", HFCache: cache}, source)
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
	result, err := fetch.Run(context.Background(), fetch.Options{Manifest: readManifest(t, manifest), Lock: readLock(t, lock), Root: root, Layout: "coder", HFCache: cache, DryRun: true}, nil)
	if err != nil || !result.Changed || !result.DryRun {
		t.Fatalf("preview: %+v %v", result, err)
	}
	for _, path := range []string{root, cache} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("dry run created %s: %v", path, err)
		}
	}
}

func TestFailedCacheReverificationStaysUnpreparedUntilCorrected(t *testing.T) {
	directory := t.TempDir()
	manifest, lock, entry := writeInputs(t, directory, false, "weights")
	root, cache := filepath.Join(directory, "temper"), filepath.Join(directory, "hub")
	blob := seedHF(t, cache, entry, "weights")
	options := fetch.Options{Manifest: readManifest(t, manifest), Lock: readLock(t, lock), Root: root, Layout: "coder", HFCache: cache}
	if _, err := fetch.Run(context.Background(), options, nil); err != nil {
		t.Fatal(err)
	}
	receipt := filepath.Join(root, "artifacts", "layouts", "coder", entry.Digest(), "receipt.json")
	before, err := os.ReadFile(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blob, []byte("spoiled"), 0644); err != nil {
		t.Fatal(err)
	}
	changed := time.Unix(1700000000, 0)
	if err := os.Chtimes(blob, changed, changed); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := fetch.Run(context.Background(), options, nil); !errors.Is(err, artifactset.ErrContentMismatch) {
			t.Fatalf("attempt %d accepted unverified cache bytes: %v", attempt, err)
		}
	}
	after, err := os.ReadFile(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("failed verification changed the receipt")
	}
	if err := os.WriteFile(blob, []byte("weights"), 0644); err != nil {
		t.Fatal(err)
	}
	if result, err := fetch.Run(context.Background(), options, nil); err != nil || !result.Changed {
		t.Fatalf("corrected cache: %+v %v", result, err)
	}
	if result, err := fetch.Run(context.Background(), options, nil); err != nil || result.Changed {
		t.Fatalf("verified retry: %+v %v", result, err)
	}
}

func TestMovedHFCacheRelinksFromTheConfiguredCacheOnly(t *testing.T) {
	for _, move := range []string{"link left behind", "new cache root", "volume missing"} {
		t.Run(move, func(t *testing.T) {
			directory := t.TempDir()
			manifest, lock, entry := writeInputs(t, directory, false, "weights")
			root, old, moved := filepath.Join(directory, "temper"), filepath.Join(directory, "hub"), filepath.Join(directory, "moved-hub")
			seedHF(t, old, entry, "weights")
			options := fetch.Options{Manifest: readManifest(t, manifest), Lock: readLock(t, lock), Root: root, Layout: "coder", HFCache: old}
			if _, err := fetch.Run(context.Background(), options, nil); err != nil {
				t.Fatal(err)
			}
			installed := filepath.Join(root, "artifacts", "layouts", "coder", entry.Digest(), "model", entry.Files[0].Name)
			source := &fakeSource{files: map[string]string{"owner/model@" + modelRevision + "/nested/model.gguf": "weights"}}
			switch move {
			case "link left behind":
				if err := os.Rename(old, moved); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(moved, old); err != nil {
					t.Fatal(err)
				}
			case "new cache root":
				if err := os.RemoveAll(old); err != nil {
					t.Fatal(err)
				}
			case "volume missing":
				if err := os.RemoveAll(old); err != nil {
					t.Fatal(err)
				}
				options.HFCache = "/Volumes/temper-test-absent-volume/hub"
				if _, err := runFetch(context.Background(), options, source); err == nil || !strings.Contains(err.Error(), "HF cache volume unavailable") {
					t.Fatalf("missing volume = %v", err)
				}
				return
			}
			options.HFCache = moved
			if result, err := runFetch(context.Background(), options, source); err != nil || !result.Changed {
				t.Fatalf("relink: %+v %v", result, err)
			}
			if link, err := os.Readlink(installed); err != nil || !strings.HasPrefix(link, resolvedPath(t, moved)) {
				t.Fatalf("set links to %q, want the configured cache %s: %v", link, moved, err)
			}
			if move == "new cache root" {
				if _, err := os.Lstat(old); !os.IsNotExist(err) {
					t.Fatalf("wrote into the cache the user no longer configures: %v", err)
				}
			}
			assertFile(t, installed, "weights")
			if result, err := fetch.Run(context.Background(), options, nil); err != nil || result.Changed {
				t.Fatalf("relinked set is not second-run clean: %+v %v", result, err)
			}
		})
	}
}

func resolvedPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}
