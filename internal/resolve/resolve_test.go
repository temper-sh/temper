package resolve_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/resolve"
	"github.com/temper-sh/temper/internal/upstream"
)

const (
	testRevision = "1111111111111111111111111111111111111111"
	testSHA      = "2222222222222222222222222222222222222222222222222222222222222222"
	patchSource  = "hf://patches/templates@3333333333333333333333333333333333333333/chat.jinja?transform=qwen38-prefix-stability-v1"
	oldGuard     = "{%- if (_preserve_thinking or loop.index0 > ns.last_query_index) and reasoning_content %}"
)

type fakeSource struct {
	pin          upstream.FilePin
	patch        string
	resolveErr   error
	resolveCalls int
	openCalls    int
	onResolve    func()
}

func (f *fakeSource) Resolve(_ context.Context, _, _ string) (upstream.FilePin, error) {
	f.resolveCalls++
	if f.onResolve != nil {
		f.onResolve()
	}
	return f.pin, f.resolveErr
}

func (f *fakeSource) Open(_ context.Context, _, _, _ string) (io.ReadCloser, error) {
	f.openCalls++
	return io.NopCloser(strings.NewReader(f.patch)), nil
}

func TestRunAddsMissingRowsAndSecondRunIsClean(t *testing.T) {
	directory := t.TempDir()
	manifestPath := writeManifest(t, directory, true)
	lockPath := filepath.Join(directory, "manifest.lock.yaml")
	source := &fakeSource{
		pin:   upstream.FilePin{Revision: testRevision, SHA256: testSHA},
		patch: "before\n" + oldGuard + "\nafter\n",
	}

	result, err := resolve.Run(context.Background(), resolve.Options{
		ManifestPath: manifestPath,
		LockPath:     lockPath,
		Now:          func() time.Time { return time.Date(2026, 8, 20, 22, 0, 0, 0, time.UTC) },
	}, source)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || len(result.Entries) != 1 || result.Entries[0].ID != "coder" {
		t.Fatalf("result = %#v", result)
	}
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := lockfile.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := locked.Entry("coder")
	if !ok || entry.Revision != testRevision || entry.Resolved != "2026-08-20" {
		t.Fatalf("entry = %#v, present = %t", entry, ok)
	}
	transformed := strings.Replace("before\n"+oldGuard+"\nafter\n", oldGuard, "{%- if (_preserve_thinking or loop.index0 > ns.last_query_index) and (reasoning_content or not ns_state.thinking) %}", 1)
	wantPatchHash := sha256.Sum256([]byte(transformed))
	if len(entry.Patches) != 1 || entry.Patches[0].SHA256 != hex.EncodeToString(wantPatchHash[:]) {
		t.Fatalf("patch lock = %#v", entry.Patches)
	}

	before := string(data)
	second, err := resolve.Run(context.Background(), resolve.Options{ManifestPath: manifestPath, LockPath: lockPath}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed || len(second.Entries) != 0 {
		t.Fatalf("second result = %#v", second)
	}
	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != before {
		t.Fatal("second resolve changed lock bytes")
	}
}

func TestDryRunDoesNotCreateLock(t *testing.T) {
	directory := t.TempDir()
	manifestPath := writeManifest(t, directory, false)
	lockPath := filepath.Join(directory, "manifest.lock.yaml")
	source := &fakeSource{pin: upstream.FilePin{Revision: testRevision, SHA256: testSHA}}
	result, err := resolve.Run(context.Background(), resolve.Options{ManifestPath: manifestPath, LockPath: lockPath, DryRun: true}, source)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || !result.DryRun {
		t.Fatalf("result = %#v", result)
	}
	if _, err := os.Lstat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock exists after dry-run: %v", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory entries after dry-run = %v", entries)
	}
}

func TestUpstreamFailureLeavesAbsentLockAbsent(t *testing.T) {
	directory := t.TempDir()
	manifestPath := writeManifest(t, directory, false)
	lockPath := filepath.Join(directory, "manifest.lock.yaml")
	source := &fakeSource{resolveErr: errors.New("offline")}
	if _, err := resolve.Run(context.Background(), resolve.Options{ManifestPath: manifestPath, LockPath: lockPath}, source); err == nil {
		t.Fatal("resolve succeeded")
	}
	if _, err := os.Lstat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock exists after failure: %v", err)
	}
}

func TestRunPreservesExistingRowWhileAddingMissingRow(t *testing.T) {
	directory := t.TempDir()
	manifestPath := writeTwoLayoutManifest(t, directory)
	lockPath := filepath.Join(directory, "manifest.lock.yaml")
	existing := lockfile.Entry{
		Repo:     "owner/model",
		Revision: "4444444444444444444444444444444444444444",
		Files:    []lockfile.File{{Name: "model.gguf", SHA256: "5555555555555555555555555555555555555555555555555555555555555555"}},
		Resolved: "2026-08-19",
	}
	lockData, err := lockfile.Marshal(lockfile.Document{Schema: lockfile.SchemaV1, Entries: map[string]lockfile.Entry{"coder": existing}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, lockData, 0o644); err != nil {
		t.Fatal(err)
	}
	source := &fakeSource{pin: upstream.FilePin{Revision: testRevision, SHA256: testSHA}}
	if _, err := resolve.Run(context.Background(), resolve.Options{ManifestPath: manifestPath, LockPath: lockPath}, source); err != nil {
		t.Fatal(err)
	}
	resolvedData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	resolvedLock, err := lockfile.Parse(resolvedData)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resolvedLock.Entries["coder"], existing) {
		t.Fatalf("existing row changed: %#v", resolvedLock.Entries["coder"])
	}
	if added, ok := resolvedLock.Entry("alternate"); !ok || added.Revision != testRevision {
		t.Fatalf("missing row = %#v, present = %t", added, ok)
	}
	if source.resolveCalls != 1 {
		t.Fatalf("metadata reads = %d, want only the missing row", source.resolveCalls)
	}
}

func TestConcurrentLockWriterIsNotOverwritten(t *testing.T) {
	directory := t.TempDir()
	manifestPath := writeManifest(t, directory, false)
	lockPath := filepath.Join(directory, "manifest.lock.yaml")
	concurrent := "schema: temper-lock/v1\nentries:\n  coder:\n    repo: owner/model\n    revision: 4444444444444444444444444444444444444444\n    files:\n      - name: model.gguf\n        sha256: 5555555555555555555555555555555555555555555555555555555555555555\n    resolved: 2026-08-19\n"
	source := &fakeSource{
		pin: upstream.FilePin{Revision: testRevision, SHA256: testSHA},
		onResolve: func() {
			if err := os.WriteFile(lockPath, []byte(concurrent), 0o644); err != nil {
				t.Fatal(err)
			}
		},
	}
	_, err := resolve.Run(context.Background(), resolve.Options{ManifestPath: manifestPath, LockPath: lockPath}, source)
	if err == nil || !strings.Contains(err.Error(), "changed concurrently") {
		t.Fatalf("error = %v", err)
	}
	got, readErr := os.ReadFile(lockPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != concurrent {
		t.Fatal("concurrent lock was overwritten")
	}
}

func writeManifest(t *testing.T, directory string, withPatch bool) string {
	t.Helper()
	chatTemplate := ""
	patches := "patches: {}\n"
	if withPatch {
		chatTemplate = "    chat_template: stable-template\n"
		patches = "patches:\n  stable-template:\n    source: " + patchSource + "\n    file: chat.jinja\n"
	}
	data := "schema: temper-manifest/v1\n" +
		"defaults:\n  ttl: 300\n  gpu_memory_utilization: 0.9\n" +
		patches +
		"layouts:\n  coder:\n    display_name: Coder\n    model:\n      repo: owner/model\n      file: model.gguf\n    engine: llama-server\n    role: coder\n    window: 32768\n    max_tokens: 4096\n    kv: q8\n    thinking: off\n" + chatTemplate +
		"    llama:\n      parallel: 1\n      flash_attention: on\n      batch: 1024\n      ubatch: 256\n" +
		"tools: {}\n" +
		"modes:\n  local:\n    foreground: local\n    tools: []\n    harnesses: []\n    members:\n      resident:\n        - layout: coder\n          preferred: true\n      on_demand: []\n"
	path := filepath.Join(directory, "manifest.yaml")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeTwoLayoutManifest(t *testing.T, directory string) string {
	t.Helper()
	data := `schema: temper-manifest/v1
defaults: {ttl: 300, gpu_memory_utilization: 0.9}
patches: {}
layouts:
  coder:
    display_name: Coder
    model: {repo: owner/model, file: model.gguf}
    engine: llama-server
    role: coder
    window: 32768
    max_tokens: 4096
    kv: q8
    thinking: off
    llama: {parallel: 1, flash_attention: on, batch: 1024, ubatch: 256}
  alternate:
    display_name: Alternate
    model: {repo: owner/alternate, file: alternate.gguf}
    engine: llama-server
    role: coder
    window: 16384
    max_tokens: 2048
    kv: q8
    thinking: off
    llama: {parallel: 1, flash_attention: on, batch: 1024, ubatch: 256}
tools: {}
modes:
  local:
    foreground: local
    tools: []
    harnesses: []
    members:
      resident:
        - {layout: coder, preferred: true}
        - {layout: alternate}
      on_demand: []
`
	path := filepath.Join(directory, "manifest.yaml")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
