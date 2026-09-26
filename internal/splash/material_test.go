package splash

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/temper-sh/temper/internal/artifactset"
	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/manifest"
)

func fixture(t *testing.T) Material {
	t.Helper()
	sum := func(data string) string { h := sha256.Sum256([]byte(data)); return hex.EncodeToString(h[:]) }
	layout := manifest.Layout{Model: manifest.Model{Repo: "owner/target", Format: "gguf", Files: []string{"nested/model.gguf"}}, Draft: &manifest.Model{Repo: "owner/draft", Format: "safetensors", Files: []string{"config.json", "model.safetensors"}}, ChatTemplate: "frog", Splash: &manifest.SplashTuning{SoftwareSHA256: strings.Repeat("a", 64)}}
	entry := lockfile.Entry{Repo: layout.Model.Repo, Revision: strings.Repeat("b", 40), Files: []lockfile.File{{Name: "nested/model.gguf", SHA256: sum("target")}}, Patches: []lockfile.Patch{{Name: "frog", SHA256: sum("Frog template")}}, Draft: &lockfile.Draft{Repo: "owner/draft", Revision: strings.Repeat("c", 40), Files: []lockfile.File{{Name: "config.json", SHA256: sum("config")}, {Name: "model.safetensors", SHA256: sum("draft")}}}}
	patches := map[string]manifest.Patch{"frog": {File: "chat.jinja"}}
	m, err := New(t.TempDir(), "qwen", layout, entry, patches)
	if err != nil {
		t.Fatal(err)
	}
	contents := map[string]string{"model/nested/model.gguf": "target", "draft/config.json": "config", "draft/model.safetensors": "draft", "patches/frog/chat.jinja": "Frog template"}
	var records []artifactset.Record
	for _, file := range m.set.Files() {
		path := filepath.Join(m.set.Path(), file.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		data := contents[file.Path]
		if err := os.WriteFile(path, []byte(data), 0444); err != nil {
			t.Fatal(err)
		}
		records = append(records, artifactset.Record{Path: file.Path, SHA256: file.SHA256, Size: int64(len(data))})
	}
	receipt, err := m.set.Receipt(records)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.set.Path(), "receipt.json"), receipt, 0444); err != nil {
		t.Fatal(err)
	}
	return m
}

func deriveFixture(context.Context, string, string, string, string) (map[string][]byte, error) {
	return map[string][]byte{"config.json": []byte(`{"model_type":"test"}`), "tokenizer/tokenizer.json": []byte(`{"version":"1.0"}`), "tokenizer/tokenizer_config.json": []byte(`{}`), "tokenizer/chat_template.jinja": []byte("vendor template")}, nil
}

func TestPreparePublishesFrogAndSharedWeightsThenIsSecondRunClean(t *testing.T) {
	m := fixture(t)
	if err := m.Prepare(context.Background(), "/release/python", deriveFixture); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.state); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preparation created runtime state", err)
	}
	if err := m.PrepareState(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(m.state, "weights")); err != nil || !info.IsDir() {
		t.Fatal("runtime cache root missing", err)
	}
	data, err := os.ReadFile(filepath.Join(m.path, "tokenizer/chat_template.jinja"))
	if err != nil || string(data) != "Frog template" {
		t.Fatal(string(data), err)
	}
	descriptor, err := os.ReadFile(filepath.Join(m.path, "model.json"))
	if err != nil {
		t.Fatal(err)
	}
	var source map[string]any
	if err := json.Unmarshal(descriptor, &source); err != nil {
		t.Fatal(err)
	}
	if source["version"] != float64(1) || source["model"] != "owner/target" || source["target_format"] != "gguf" || source["vision_format"] != "none" {
		t.Fatal("native source descriptor is incomplete", source)
	}
	receipt, _ := os.ReadFile(filepath.Join(m.path, "receipt.json"))
	fail := func(context.Context, string, string, string, string) (map[string][]byte, error) {
		t.Fatal("second prepare invoked Python")
		return nil, nil
	}
	if err := m.Prepare(context.Background(), "/release/python", fail); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(m.path, "receipt.json"))
	if string(after) != string(receipt) {
		t.Fatal("second run changed receipt")
	}
	if err := m.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestPreparationFailureLeavesNoAssemblyAndCanRetry(t *testing.T) {
	for _, failure := range []DeriveFunc{
		func(context.Context, string, string, string, string) (map[string][]byte, error) {
			return nil, errors.New("incompatible DFlash2 configuration")
		},
		func(context.Context, string, string, string, string) (map[string][]byte, error) {
			f, _ := deriveFixture(context.Background(), "", "", "", "")
			delete(f, "config.json")
			f["../escape"] = []byte("bad")
			return f, nil
		},
	} {
		m := fixture(t)
		if err := m.Prepare(context.Background(), "/python", failure); err == nil {
			t.Fatal("invalid metadata accepted")
		}
		if _, err := os.Stat(m.path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("failure published an assembly", err)
		}
		if err := m.Prepare(context.Background(), "/python", deriveFixture); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPreparedMetadataDriftIsRefusedWithoutOverwriting(t *testing.T) {
	m := fixture(t)
	if err := m.Prepare(context.Background(), "/python", deriveFixture); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(m.path, "tokenizer/chat_template.jinja")
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("Other template"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := m.Verify(); err == nil {
		t.Fatal("changed template accepted")
	}
	if err := m.Prepare(context.Background(), "/python", deriveFixture); err == nil {
		t.Fatal("changed assembly overwritten")
	}
}

func TestConcurrentPreparationAdmitsOnlyCompleteWinner(t *testing.T) {
	m := fixture(t)
	ready := make(chan struct{})
	var mu sync.Mutex
	calls := 0
	derive := func(ctx context.Context, a, b, c, d string) (map[string][]byte, error) {
		mu.Lock()
		calls++
		if calls == 2 {
			close(ready)
		}
		mu.Unlock()
		<-ready
		return deriveFixture(ctx, a, b, c, d)
	}
	results := make(chan error, 2)
	for range 2 {
		go func() { results <- m.Prepare(context.Background(), "/python", derive) }()
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Verify(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(m.path))
	if err != nil || len(entries) != 1 {
		t.Fatal("orphan stage", entries, err)
	}
}

func TestServingStateRefusesSymlinkedRuntimeRoot(t *testing.T) {
	m := fixture(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(m.root, "runtime")); err != nil {
		t.Fatal(err)
	}
	if err := m.PrepareState(); err == nil {
		t.Fatal("redirected writable runtime state outside root")
	}
	files, err := os.ReadDir(outside)
	if err != nil || len(files) != 0 {
		t.Fatal(files, err)
	}
}
