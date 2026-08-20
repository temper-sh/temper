package update_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/update"
	"github.com/temper-sh/temper/internal/upstream"
)

const (
	oldCoderRevision   = "1111111111111111111111111111111111111111"
	newCoderRevision   = "2222222222222222222222222222222222222222"
	oldRerankRevision  = "3333333333333333333333333333333333333333"
	newRerankRevision  = "4444444444444444444444444444444444444444"
	newCoderSHA        = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	newRerankSHA       = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	patchSourceCommit  = "5555555555555555555555555555555555555555"
	oldPatchSHA        = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	prefixStabilityOld = "{%- if (_preserve_thinking or loop.index0 > ns.last_query_index) and reasoning_content %}"
)

type fakeSource struct {
	pins      map[string]upstream.FilePin
	patch     string
	err       error
	calls     []string
	onResolve func()
}

func (f *fakeSource) Resolve(_ context.Context, repo, file string) (upstream.FilePin, error) {
	f.calls = append(f.calls, repo+"/"+file)
	if f.onResolve != nil {
		f.onResolve()
	}
	if f.err != nil {
		return upstream.FilePin{}, f.err
	}
	return f.pins[repo+"/"+file], nil
}

func (f *fakeSource) Open(_ context.Context, _, _, _ string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(f.patch)), nil
}

func TestRunMovesOnlyTargetedRowAndPrintsCoderGates(t *testing.T) {
	manifestPath, lockPath := writeInputs(t)
	before := readLock(t, lockPath)
	source := newSource()

	result, err := update.Run(context.Background(), update.Options{
		ManifestPath: manifestPath,
		LockPath:     lockPath,
		Layout:       "coder",
		Now:          fixedNow,
	}, source)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.All || result.ChangeCount() != 1 || len(result.Entries) != 1 {
		t.Fatalf("result = %#v", result)
	}
	entry := result.Entries[0]
	if entry.ID != "coder" || !entry.Changed || entry.OldRevision != oldCoderRevision || entry.NewRevision != newCoderRevision {
		t.Fatalf("entry = %#v", entry)
	}
	if len(entry.Gates) != 2 || entry.Gates[0].Step != "plain-completion" || entry.Gates[1].Step != "streaming-tool-call" {
		t.Fatalf("gates = %#v", entry.Gates)
	}
	for _, gate := range entry.Gates {
		if !strings.Contains(gate.Command, `"model":"coder"`) {
			t.Fatalf("gate does not address coder: %s", gate.Command)
		}
	}
	if !strings.Contains(entry.Gates[1].Command, "delta.tool_calls") {
		t.Fatalf("stream gate does not assert a tool_calls delta: %s", entry.Gates[1].Command)
	}

	after := readLock(t, lockPath)
	if after.Entries["coder"].Revision != newCoderRevision || after.Entries["coder"].Files[0].SHA256 != newCoderSHA || after.Entries["coder"].Resolved != "2026-08-20" {
		t.Fatalf("updated coder = %#v", after.Entries["coder"])
	}
	if !reflect.DeepEqual(after.Entries["reranker"], before.Entries["reranker"]) {
		t.Fatalf("non-target row changed: %#v", after.Entries["reranker"])
	}
	if got, want := source.calls, []string{"org/Coder/coder.gguf"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("upstream calls = %v, want %v", got, want)
	}
}

func TestRunIsSecondRunCleanWhenIdentityDidNotMove(t *testing.T) {
	manifestPath, lockPath := writeInputs(t)
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	source := newSource()
	source.pins["org/Coder/coder.gguf"] = upstream.FilePin{
		Revision: oldCoderRevision,
		SHA256:   strings.Repeat("1", 64),
	}
	// Make the recomputed transformed patch equal the existing row.
	locked := readLock(t, lockPath)
	entry := locked.Entries["coder"]
	first, err := update.Run(context.Background(), update.Options{
		ManifestPath: manifestPath, LockPath: lockPath, Layout: "coder", Now: fixedNow,
	}, source)
	if err != nil {
		t.Fatal(err)
	}
	// The fixture's placeholder patch hash makes the first resolution a move;
	// run once more against the committed identity to prove cleanliness.
	if !first.Changed {
		t.Fatal("fixture setup did not move the patch identity")
	}
	committed, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := update.Run(context.Background(), update.Options{
		ManifestPath: manifestPath, LockPath: lockPath, Layout: "coder", Now: func() time.Time {
			return time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
		},
	}, source)
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed || second.ChangeCount() != 0 || len(second.Entries[0].Gates) != 0 {
		t.Fatalf("second result = %#v", second)
	}
	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, committed) {
		t.Fatal("second update changed lock bytes")
	}
	if reflect.DeepEqual(before, after) || entry.Resolved == readLock(t, lockPath).Entries["coder"].Resolved {
		t.Fatal("fixture did not establish the initial changed row")
	}
}

func TestDryRunReportsAllMovesWithoutWriting(t *testing.T) {
	manifestPath, lockPath := writeInputs(t)
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	result, err := update.Run(context.Background(), update.Options{
		ManifestPath: manifestPath, LockPath: lockPath, DryRun: true, Now: fixedNow,
	}, newSource())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || !result.DryRun || !result.All || result.ChangeCount() != 2 {
		t.Fatalf("result = %#v", result)
	}
	if result.Entries[0].ID != "coder" || result.Entries[1].ID != "reranker" {
		t.Fatalf("entries are not sorted: %#v", result.Entries)
	}
	if gates := result.Entries[1].Gates; len(gates) != 1 || gates[0].Step != "rerank-order-and-magnitude" || !strings.Contains(gates[0].Command, "> 0.001") {
		t.Fatalf("reranker gate = %#v", gates)
	}
	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatal("dry-run changed lock")
	}
	entries, err := os.ReadDir(filepath.Dir(lockPath))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("dry-run left temporary files: %v", entries)
	}
}

func TestFailureAndConcurrentWriterLeaveNoPartialUpdate(t *testing.T) {
	t.Run("upstream failure", func(t *testing.T) {
		manifestPath, lockPath := writeInputs(t)
		before, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatal(err)
		}
		source := newSource()
		source.err = errors.New("offline")
		if _, err := update.Run(context.Background(), update.Options{ManifestPath: manifestPath, LockPath: lockPath}, source); err == nil {
			t.Fatal("update succeeded")
		}
		after, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(after, before) {
			t.Fatal("upstream failure changed lock")
		}
	})

	t.Run("concurrent writer", func(t *testing.T) {
		manifestPath, lockPath := writeInputs(t)
		concurrent := strings.Replace(updateLock, oldCoderRevision, strings.Repeat("f", 40), 1)
		source := newSource()
		source.onResolve = func() {
			source.onResolve = nil
			if err := os.WriteFile(lockPath, []byte(concurrent), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		_, err := update.Run(context.Background(), update.Options{
			ManifestPath: manifestPath, LockPath: lockPath, Layout: "coder", Now: fixedNow,
		}, source)
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
	})
}

func TestSelectionDriftAndMissingRowsRefuseBeforeUpstreamRead(t *testing.T) {
	manifestPath, lockPath := writeInputs(t)
	locked := readLock(t, lockPath)
	coder := locked.Entries["coder"]
	coder.Repo = "org/Other"
	locked.Entries["coder"] = coder
	writeLock(t, lockPath, locked)
	source := newSource()
	_, err := update.Run(context.Background(), update.Options{
		ManifestPath: manifestPath, LockPath: lockPath, Layout: "coder",
	}, source)
	if err == nil || !strings.Contains(err.Error(), "repo drift") {
		t.Fatalf("error = %v", err)
	}
	if len(source.calls) != 0 {
		t.Fatalf("drifting selection reached upstream: %v", source.calls)
	}

	delete(locked.Entries, "reranker")
	writeLock(t, lockPath, locked)
	_, err = update.Run(context.Background(), update.Options{
		ManifestPath: manifestPath, LockPath: lockPath, Layout: "reranker",
	}, source)
	if err == nil || !strings.Contains(err.Error(), "run temper resolve first") {
		t.Fatalf("missing-row error = %v", err)
	}
}

func newSource() *fakeSource {
	return &fakeSource{
		pins: map[string]upstream.FilePin{
			"org/Coder/coder.gguf":       {Revision: newCoderRevision, SHA256: newCoderSHA},
			"org/Reranker/reranker.gguf": {Revision: newRerankRevision, SHA256: newRerankSHA},
		},
		patch: "before\n" + prefixStabilityOld + "\nafter\n",
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 8, 20, 22, 0, 0, 0, time.UTC)
}

func writeInputs(t *testing.T) (string, string) {
	t.Helper()
	directory := t.TempDir()
	manifestPath := filepath.Join(directory, "manifest.yaml")
	lockPath := filepath.Join(directory, "manifest.lock.yaml")
	if err := os.WriteFile(manifestPath, []byte(updateManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte(updateLock), 0o644); err != nil {
		t.Fatal(err)
	}
	return manifestPath, lockPath
}

func readLock(t *testing.T, path string) lockfile.Document {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := lockfile.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return locked
}

func writeLock(t *testing.T, path string, locked lockfile.Document) {
	t.Helper()
	data, err := lockfile.Marshal(locked)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

const updateManifest = `schema: temper-manifest/v1
defaults: {ttl: 300, gpu_memory_utilization: 0.9}
patches:
  stable-template:
    source: hf://patches/templates@` + patchSourceCommit + `/chat.jinja?transform=qwen38-prefix-stability-v1
    file: chat.jinja
layouts:
  coder:
    display_name: Coder
    model: {repo: org/Coder, file: coder.gguf}
    engine: llama-server
    role: coder
    window: 32768
    max_tokens: 4096
    kv: q8
    thinking: off
    chat_template: stable-template
    llama: {parallel: 1, flash_attention: on, batch: 1024, ubatch: 256}
  reranker:
    display_name: Reranker
    model: {repo: org/Reranker, file: reranker.gguf}
    engine: llama-server
    role: rerank
    window: 4096
    llama: {parallel: 1, flash_attention: off, batch: 512, ubatch: 512}
tools:
  project-search: {source: builtin/project-search, needs: [rerank]}
modes:
  local:
    foreground: local
    tools: [project-search]
    harnesses: []
    members:
      resident: [{layout: coder, preferred: true}]
      on_demand: [{layout: reranker, ngl: 0}]
`

const updateLock = `schema: temper-lock/v1
entries:
  coder:
    repo: org/Coder
    revision: ` + oldCoderRevision + `
    files:
      - {name: coder.gguf, sha256: 1111111111111111111111111111111111111111111111111111111111111111}
    patches:
      - {name: stable-template, sha256: ` + oldPatchSHA + `}
    resolved: 2026-08-19
  reranker:
    repo: org/Reranker
    revision: ` + oldRerankRevision + `
    files:
      - {name: reranker.gguf, sha256: 2222222222222222222222222222222222222222222222222222222222222222}
    resolved: 2026-08-19
`
