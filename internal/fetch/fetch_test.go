package fetch_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/temper-sh/temper/internal/fetch"
	"github.com/temper-sh/temper/internal/hfcache"
	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/upstream"
)

const (
	modelRevision = "1111111111111111111111111111111111111111"
	patchRevision = "3333333333333333333333333333333333333333"
	oldGuard      = "{%- if (_preserve_thinking or loop.index0 > ns.last_query_index) and reasoning_content %}"
	newGuard      = "{%- if (_preserve_thinking or loop.index0 > ns.last_query_index) and (reasoning_content or not ns_state.thinking) %}"
)

type fakeSource struct {
	files     map[string]string
	openCalls int
}

type barrierSource struct {
	ready chan struct{}
	once  sync.Once
	calls atomic.Int32
}

func (b *barrierSource) Resolve(context.Context, string, string) (upstream.FilePin, error) {
	return upstream.FilePin{}, errors.New("fetch must not resolve")
}

func (b *barrierSource) Open(_ context.Context, _, _, _ string) (io.ReadCloser, error) {
	if b.calls.Add(1) == 2 {
		b.once.Do(func() { close(b.ready) })
	}
	<-b.ready
	return io.NopCloser(strings.NewReader("weights")), nil
}

func (f *fakeSource) Resolve(context.Context, string, string) (upstream.FilePin, error) {
	return upstream.FilePin{}, errors.New("fetch must not resolve")
}

func (f *fakeSource) Open(_ context.Context, repo, revision, file string) (io.ReadCloser, error) {
	f.openCalls++
	value, ok := f.files[repo+"@"+revision+"/"+file]
	if !ok {
		return nil, errors.New("unexpected download")
	}
	return io.NopCloser(strings.NewReader(value)), nil
}

// Replace only the official HF process in these offline fetch tests. The real
// cache lookup, staged byte verification and installation publication still run.
func runFetch(ctx context.Context, options fetch.Options, source upstream.Reader) (fetch.Result, error) {
	if options.HFCache == "" {
		options.HFCache = options.Root + "-hf"
	}
	if options.DownloadModel == nil && source != nil {
		options.DownloadModel = func(ctx context.Context, e hfcache.Entry, cache string) error {
			reader, err := source.Open(ctx, e.Repo, e.Revision, e.Name)
			if err != nil {
				return err
			}
			data, err := io.ReadAll(reader)
			if err := errors.Join(err, reader.Close(), ctx.Err()); err != nil {
				return err
			}
			snapshot := filepath.Join(cache, "models--"+strings.ReplaceAll(e.Repo, "/", "--"), "snapshots", e.Revision, e.Name)
			if err := os.MkdirAll(filepath.Dir(snapshot), 0o755); err != nil {
				return err
			}
			file, err := os.CreateTemp(filepath.Dir(snapshot), "fixture-")
			if err != nil {
				return err
			}
			defer os.Remove(file.Name())
			_, err = file.Write(data)
			if err := errors.Join(err, file.Close()); err != nil {
				return err
			}
			return os.Rename(file.Name(), snapshot)
		}
	}
	return fetch.Run(ctx, options, source)
}

func TestRunPublishesExactLayoutSetAndSecondRunIsClean(t *testing.T) {
	directory := t.TempDir()
	manifestPath, lockPath, entry := writeInputs(t, directory, true, "weights")
	root := filepath.Join(directory, "root")
	source := &fakeSource{files: map[string]string{
		"owner/model@" + modelRevision + "/nested/model.gguf": "weights",
		"patches/templates@" + patchRevision + "/chat.jinja":  "before\n" + oldGuard + "\nafter\n",
	}}
	result, err := runFetch(context.Background(), fetch.Options{
		ManifestPath: manifestPath,
		LockPath:     lockPath,
		Root:         root,
		Layout:       "coder",
	}, source)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Layout != "coder" || result.ArtifactSet != entry.Digest() || len(result.Files) != 3 {
		t.Fatalf("result = %#v", result)
	}
	base := filepath.Join(root, "artifacts", "layouts", "coder", entry.Digest())
	assertFile(t, filepath.Join(base, "model", "nested", "model.gguf"), "weights")
	assertFile(t, filepath.Join(base, "patches", "stable-template", "chat.jinja"), "before\n"+newGuard+"\nafter\n")
	if _, err := os.Stat(filepath.Join(base, "receipt.json")); err != nil {
		t.Fatal(err)
	}

	second, err := runFetch(context.Background(), fetch.Options{
		ManifestPath: manifestPath,
		LockPath:     lockPath,
		Root:         root,
		Layout:       "coder",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed {
		t.Fatalf("second result = %#v", second)
	}
	if source.openCalls != 2 {
		t.Fatalf("downloads = %d, want 2 from first run only", source.openCalls)
	}
}

func TestTemplateChangeReusesInstalledWeightsAndKeepsSetsIndependent(t *testing.T) {
	directory := t.TempDir()
	root := filepath.Join(directory, "root")
	source := &fakeSource{files: map[string]string{
		"owner/model@" + modelRevision + "/nested/model.gguf": "weights",
		"patches/templates@" + patchRevision + "/chat.jinja":  "before\n" + oldGuard + "\nafter\n",
	}}
	var sets []string
	for _, withPatch := range []bool{false, true} {
		manifestPath, lockPath, entry := writeInputs(t, directory, withPatch, "weights")
		options := fetch.Options{ManifestPath: manifestPath, LockPath: lockPath, Root: root, Layout: "coder"}
		result, err := runFetch(context.Background(), options, source)
		if err != nil {
			t.Fatal(err)
		}
		if !result.Changed {
			t.Fatal("a new template composition must publish its own set")
		}
		sets = append(sets, filepath.Join(root, "artifacts", "layouts", "coder", entry.Digest()))
		if replay, err := runFetch(context.Background(), options, nil); err != nil || replay.Changed {
			t.Fatalf("unchanged replay: %+v, %v", replay, err)
		}
	}
	if source.openCalls != 2 {
		t.Fatalf("downloads = %d, want one model and one template", source.openCalls)
	}
	first, err := os.Stat(filepath.Join(sets[0], "model", "nested", "model.gguf"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.Stat(filepath.Join(sets[1], "model", "nested", "model.gguf"))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(first, second) {
		t.Fatal("template variants consume separate copies of the same weights")
	}
	if err := os.RemoveAll(sets[0]); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(sets[1], "model", "nested", "model.gguf"), "weights")
	assertFile(t, filepath.Join(sets[1], "patches", "stable-template", "chat.jinja"), "before\n"+newGuard+"\nafter\n")
}

func TestTemplateChangeFailurePreservesInstalledSet(t *testing.T) {
	for _, failure := range []string{"corrupt weights", "failed patch", "canceled patch", "symlinked weights"} {
		t.Run(failure, func(t *testing.T) {
			directory := t.TempDir()
			root := filepath.Join(directory, "root")
			manifestPath, lockPath, original := writeInputs(t, directory, false, "weights")
			options := fetch.Options{ManifestPath: manifestPath, LockPath: lockPath, Root: root, Layout: "coder"}
			if _, err := runFetch(context.Background(), options, &fakeSource{files: map[string]string{
				"owner/model@" + modelRevision + "/nested/model.gguf": "weights",
			}}); err != nil {
				t.Fatal(err)
			}
			originalPath := filepath.Join(root, "artifacts", "layouts", "coder", original.Digest())
			modelPath := filepath.Join(originalPath, "model", "nested", "model.gguf")
			if failure == "corrupt weights" {
				if err := os.WriteFile(modelPath, []byte("changed"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if failure == "symlinked weights" {
				outside := filepath.Join(directory, "outside.gguf")
				if err := os.Rename(modelPath, outside); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, modelPath); err != nil {
					t.Fatal(err)
				}
			}
			_, _, changed := writeInputs(t, directory, true, "weights")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			source := &patchFailureSource{cancel: cancel, canceled: failure == "canceled patch"}
			_, err := runFetch(ctx, options, source)
			if err == nil {
				t.Fatal("invalid composition was published")
			}
			if failure == "corrupt weights" && !strings.Contains(err.Error(), "SHA-256") {
				t.Fatalf("reused bytes were not verified: %v", err)
			}
			if source.modelCalls != 0 {
				t.Fatal("template change tried to download the already installed model")
			}
			if failure == "canceled patch" && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
			changedPath := filepath.Join(root, "artifacts", "layouts", "coder", changed.Digest())
			if _, err := os.Lstat(changedPath); !os.IsNotExist(err) {
				t.Fatalf("failed composition exists: %v", err)
			}
			entries, err := os.ReadDir(filepath.Dir(originalPath))
			if err != nil || len(entries) != 1 || entries[0].Name() != original.Digest() {
				t.Fatalf("failed composition left staging material or removed original: %v, %v", entries, err)
			}
			want := "weights"
			if failure == "corrupt weights" {
				want = "changed" // No in-place repair of existing material.
			}
			assertFile(t, modelPath, want)
		})
	}
}

func TestModelReuseFollowsContentAcrossLayoutNames(t *testing.T) {
	directory := t.TempDir()
	root := filepath.Join(directory, "root")
	manifestPath, lockPath, original := writeInputs(t, directory, true, "weights")
	options := fetch.Options{ManifestPath: manifestPath, LockPath: lockPath, Root: root, Layout: "coder"}
	if _, err := runFetch(context.Background(), options, &fakeSource{files: map[string]string{
		"owner/model@" + modelRevision + "/nested/model.gguf": "weights",
		"patches/templates@" + patchRevision + "/chat.jinja":  "before\n" + oldGuard + "\nafter\n",
	}}); err != nil {
		t.Fatal(err)
	}
	// A different layout, filename and upstream revision can still select the
	// same model bytes. Removing its template needs no upstream reader at all.
	_, _, changed := writeInputs(t, directory, false, "weights")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.NewReplacer("  coder:\n", "  helper:\n", "layout: coder", "layout: helper", "nested/model.gguf", "other.gguf").Replace(string(data)))
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	changed.Revision = strings.Repeat("2", 40)
	changed.Files[0].Name = "other.gguf"
	lockData, err := lockfile.Marshal(lockfile.Document{Schema: lockfile.SchemaV1, Entries: map[string]lockfile.Entry{"helper": changed}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, lockData, 0o644); err != nil {
		t.Fatal(err)
	}
	options.Layout = "helper"
	options.DryRun = true
	if result, err := runFetch(context.Background(), options, nil); err != nil || !result.Changed || !result.DryRun {
		t.Fatalf("reuse preview = %+v, %v", result, err)
	}
	helperDir := filepath.Join(root, "artifacts", "layouts", "helper")
	if _, err := os.Lstat(helperDir); !os.IsNotExist(err) {
		t.Fatalf("reuse preview created layout: %v", err)
	}
	options.DryRun = false
	if _, err := runFetch(context.Background(), options, nil); err != nil {
		t.Fatal(err)
	}
	first, err := os.Stat(filepath.Join(root, "artifacts", "layouts", "coder", original.Digest(), "model", "nested", "model.gguf"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.Stat(filepath.Join(helperDir, changed.Digest(), "model", "other.gguf"))
	if err != nil || !os.SameFile(first, second) {
		t.Fatalf("same hash was not shared across layouts: %v", err)
	}
	// A later revision with different bytes must still fetch those exact bytes.
	manifestPath, lockPath, replacement := writeInputs(t, directory, false, "updated")
	replacement.Revision = strings.Repeat("4", 40)
	updatedLock, err := lockfile.Marshal(lockfile.Document{Schema: lockfile.SchemaV1, Entries: map[string]lockfile.Entry{"coder": replacement}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, updatedLock, 0o644); err != nil {
		t.Fatal(err)
	}
	source := &fakeSource{files: map[string]string{
		"owner/model@" + replacement.Revision + "/nested/model.gguf": "updated",
	}}
	if _, err := runFetch(context.Background(), fetch.Options{ManifestPath: manifestPath, LockPath: lockPath, Root: root, Layout: "coder"}, source); err != nil {
		t.Fatal(err)
	}
	if source.openCalls != 1 {
		t.Fatalf("changed model needs one download, got %d", source.openCalls)
	}
	assertFile(t, filepath.Join(root, "artifacts", "layouts", "coder", replacement.Digest(), "model", "nested", "model.gguf"), "updated")
}

type patchFailureSource struct {
	cancel     context.CancelFunc
	canceled   bool
	modelCalls int
}

func (f *patchFailureSource) Resolve(context.Context, string, string) (upstream.FilePin, error) {
	return upstream.FilePin{}, errors.New("fetch must not resolve")
}

func (f *patchFailureSource) Open(_ context.Context, _, _, file string) (io.ReadCloser, error) {
	if file != "chat.jinja" {
		f.modelCalls++
		return nil, errors.New("model download was not authorized")
	}
	if f.canceled {
		f.cancel()
		return nil, context.Canceled
	}
	return nil, errors.New("template unavailable")
}

func TestRunPublishesEveryFileInAV2Snapshot(t *testing.T) {
	directory := t.TempDir()
	manifestData := `schema: temper-manifest/v2
defaults: {ttl: 300, gpu_memory_utilization: 0.9}
layouts:
  large:
    display_name: Large
    model:
      repo: owner/model
      format: mlx-safetensors
      files: [config.json, model.safetensors, tokenizer.json]
    engine: rapid-mlx
    interface: chat-completions
    modalities: [text]
    window: 32768
    max_tokens: 4096
    thinking: off
    speculation: {method: none}
    rapid_mlx:
      max_num_seqs: 1
      max_concurrent_requests: 1
      prefill_batch_size: 1
      completion_batch_size: 1
      gpu_memory_utilization: 0.9
      prefix_cache: off
      kv_cache_dtype: bf16
      pflash: off
tools: {}
modes:
  local:
    foreground: large
    members:
      resident: [{layout: large}]
      on_demand: []
`
	manifestPath := filepath.Join(directory, "manifest.yaml")
	if err := os.WriteFile(manifestPath, []byte(manifestData), 0o644); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"config.json": "config", "model.safetensors": "weights", "tokenizer.json": "tokens"}
	entry := lockfile.Entry{Repo: "owner/model", Revision: modelRevision, Resolved: "2026-09-02"}
	for _, name := range []string{"config.json", "model.safetensors", "tokenizer.json"} {
		digest := sha256.Sum256([]byte(values[name]))
		entry.Files = append(entry.Files, lockfile.File{Name: name, SHA256: hex.EncodeToString(digest[:])})
	}
	lockData, err := lockfile.Marshal(lockfile.Document{Schema: lockfile.SchemaV1, Entries: map[string]lockfile.Entry{"large": entry}})
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(directory, "manifest.lock.yaml")
	if err := os.WriteFile(lockPath, lockData, 0o644); err != nil {
		t.Fatal(err)
	}
	sourceFiles := map[string]string{}
	for name, value := range values {
		sourceFiles["owner/model@"+modelRevision+"/"+name] = value
	}
	root := filepath.Join(directory, "root")
	result, err := runFetch(context.Background(), fetch.Options{
		ManifestPath: manifestPath, LockPath: lockPath, Root: root, Layout: "large",
	}, &fakeSource{files: sourceFiles})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || len(result.Files) != 4 {
		t.Fatalf("result = %#v", result)
	}
	base := filepath.Join(root, "artifacts", "layouts", "large", entry.Digest(), "model")
	for name, value := range values {
		assertFile(t, filepath.Join(base, name), value)
	}
}

func TestDryRunDoesNotDownloadOrCreateRoot(t *testing.T) {
	directory := t.TempDir()
	manifestPath, lockPath, _ := writeInputs(t, directory, false, "weights")
	root := filepath.Join(directory, "root")
	source := &fakeSource{}
	result, err := runFetch(context.Background(), fetch.Options{
		ManifestPath: manifestPath,
		LockPath:     lockPath,
		Root:         root,
		Layout:       "coder",
		DryRun:       true,
	}, source)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || !result.DryRun || source.openCalls != 0 {
		t.Fatalf("result = %#v, downloads = %d", result, source.openCalls)
	}
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("root exists after dry-run: %v", err)
	}
}

func TestHashMismatchPublishesNothingAndRollsBackPreparation(t *testing.T) {
	directory := t.TempDir()
	manifestPath, lockPath, _ := writeInputs(t, directory, false, "different locked bytes")
	root := filepath.Join(directory, "root")
	source := &fakeSource{files: map[string]string{
		"owner/model@" + modelRevision + "/nested/model.gguf": "downloaded bytes",
	}}
	_, err := runFetch(context.Background(), fetch.Options{
		ManifestPath: manifestPath,
		LockPath:     lockPath,
		Root:         root,
		Layout:       "coder",
	}, source)
	if err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("root exists after failed fetch: %v", err)
	}
}

func TestMalformedExistingImmutableSetRefusesWithoutDownload(t *testing.T) {
	directory := t.TempDir()
	manifestPath, lockPath, entry := writeInputs(t, directory, false, "weights")
	root := filepath.Join(directory, "root")
	target := filepath.Join(root, "artifacts", "layouts", "coder", entry.Digest())
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "receipt.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := &fakeSource{}
	_, err := runFetch(context.Background(), fetch.Options{ManifestPath: manifestPath, LockPath: lockPath, Root: root, Layout: "coder"}, source)
	if err == nil || !strings.Contains(err.Error(), "does not identify") {
		t.Fatalf("error = %v", err)
	}
	if source.openCalls != 0 {
		t.Fatalf("downloads = %d", source.openCalls)
	}
}

func TestConcurrentIdenticalFetchesConverge(t *testing.T) {
	directory := t.TempDir()
	manifestPath, lockPath, _ := writeInputs(t, directory, false, "weights")
	root := filepath.Join(directory, "root")
	source := &barrierSource{ready: make(chan struct{})}
	options := fetch.Options{ManifestPath: manifestPath, LockPath: lockPath, Root: root, Layout: "coder"}

	errors := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := runFetch(context.Background(), options, source)
			errors <- err
		}()
	}
	for range 2 {
		if err := <-errors; err != nil {
			t.Fatalf("concurrent fetch: %v", err)
		}
	}
	if source.calls.Load() != 2 {
		t.Fatalf("downloads = %d, want both staged contenders", source.calls.Load())
	}
	result, err := runFetch(context.Background(), options, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Fatalf("verified winner reported changed: %#v", result)
	}
}

func writeInputs(t *testing.T, directory string, withPatch bool, lockedModelBytes string) (string, string, lockfile.Entry) {
	t.Helper()
	patches := "patches: {}\n"
	chatTemplate := ""
	if withPatch {
		patches = "patches:\n  stable-template:\n    source: hf://patches/templates@" + patchRevision + "/chat.jinja?transform=qwen38-prefix-stability-v1\n    file: chat.jinja\n"
		chatTemplate = "    chat_template: stable-template\n"
	}
	manifestData := "schema: temper-manifest/v1\n" +
		"defaults:\n  ttl: 300\n  gpu_memory_utilization: 0.9\n" + patches +
		"layouts:\n  coder:\n    display_name: Coder\n    model:\n      repo: owner/model\n      file: nested/model.gguf\n    engine: llama-server\n    role: coder\n    window: 32768\n    max_tokens: 4096\n    kv: q8\n    thinking: off\n" + chatTemplate +
		"    llama:\n      parallel: 1\n      flash_attention: on\n      batch: 1024\n      ubatch: 256\n" +
		"tools: {}\nmodes:\n  local:\n    foreground: local\n    tools: []\n    harnesses: []\n    members:\n      resident:\n        - layout: coder\n          preferred: true\n      on_demand: []\n"
	manifestPath := filepath.Join(directory, "manifest.yaml")
	if err := os.WriteFile(manifestPath, []byte(manifestData), 0o644); err != nil {
		t.Fatal(err)
	}
	modelHash := sha256.Sum256([]byte(lockedModelBytes))
	entry := lockfile.Entry{
		Repo:     "owner/model",
		Revision: modelRevision,
		Files:    []lockfile.File{{Name: "nested/model.gguf", SHA256: hex.EncodeToString(modelHash[:])}},
		Resolved: "2026-08-20",
	}
	if withPatch {
		finalPatch := "before\n" + newGuard + "\nafter\n"
		patchHash := sha256.Sum256([]byte(finalPatch))
		entry.Patches = []lockfile.Patch{{Name: "stable-template", SHA256: hex.EncodeToString(patchHash[:])}}
	}
	lockData, err := lockfile.Marshal(lockfile.Document{Schema: lockfile.SchemaV1, Entries: map[string]lockfile.Entry{"coder": entry}})
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(directory, "manifest.lock.yaml")
	if err := os.WriteFile(lockPath, lockData, 0o644); err != nil {
		t.Fatal(err)
	}
	return manifestPath, lockPath, entry
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}
