package artifactset_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/artifactset"
	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/manifest"
)

func TestVerifyAcceptsCanonicalHashVerifiedSet(t *testing.T) {
	set, data := fixtureSet(t, t.TempDir())
	materialize(t, set, data)
	if err := set.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestInspectReturnsTheAdmittedModelSize(t *testing.T) {
	set, data := fixtureSet(t, t.TempDir())
	materialize(t, set, data)
	inspection, err := set.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if inspection.ModelBytes != int64(len(data["model/nested/model.gguf"])) {
		t.Fatalf("inspection = %#v", inspection)
	}
	verified, err := set.InspectContent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if verified != inspection {
		t.Fatalf("verified inspection = %#v, want %#v", verified, inspection)
	}
}

func TestSnapshotSetPassesTheWholeModelDirectoryAndSumsEveryFile(t *testing.T) {
	root := t.TempDir()
	data := map[string][]byte{
		"model/config.json":       []byte("config"),
		"model/model.safetensors": []byte("weights"),
		"model/tokenizer.json":    []byte("tokenizer"),
	}
	names := []string{"config.json", "model.safetensors", "tokenizer.json"}
	entry := lockfile.Entry{Repo: "owner/model", Revision: strings.Repeat("1", 40), Resolved: "2026-09-02"}
	for _, name := range names {
		entry.Files = append(entry.Files, lockfile.File{Name: name, SHA256: hash(data["model/"+name])})
	}
	layout := manifest.Layout{Model: manifest.Model{Repo: "owner/model", Format: "mlx-safetensors", Files: names}}
	set, err := artifactset.New(root, "large", layout, entry, nil)
	if err != nil {
		t.Fatal(err)
	}
	materialize(t, set, data)
	inspection, err := set.InspectContent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantBytes := int64(len("config") + len("weights") + len("tokenizer"))
	if inspection.ModelBytes != wantBytes || set.ModelPath() != filepath.Join(set.Path(), "model") {
		t.Fatalf("inspection=%#v modelPath=%q", inspection, set.ModelPath())
	}
}

func TestVerifyDistinguishesAnAbsentSet(t *testing.T) {
	set, _ := fixtureSet(t, t.TempDir())
	err := set.Verify()
	if !errors.Is(err, artifactset.ErrNotMaterialized) {
		t.Fatalf("error = %v, want ErrNotMaterialized", err)
	}
}

func TestVerifyContentDetectsSameSizeByteDrift(t *testing.T) {
	set, data := fixtureSet(t, t.TempDir())
	materialize(t, set, data)
	modelPath := filepath.Join(set.Path(), filepath.FromSlash(set.Files()[0].Path))
	if err := os.WriteFile(modelPath, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := set.Verify(); err != nil {
		t.Fatalf("routine verification should trust the recorded size: %v", err)
	}
	if err := set.VerifyContent(context.Background()); !errors.Is(err, artifactset.ErrContentMismatch) {
		t.Fatalf("VerifyContent() error = %v, want ErrContentMismatch", err)
	}
}

func TestVerifyContentHonorsCancellation(t *testing.T) {
	set, data := fixtureSet(t, t.TempDir())
	materialize(t, set, data)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := set.VerifyContent(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("VerifyContent() error = %v, want context.Canceled", err)
	}
}

func TestVerifyRefusesReceiptOrTreeDrift(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*testing.T, artifactset.Set)
		message string
	}{
		{
			name: "noncanonical receipt",
			mutate: func(t *testing.T, set artifactset.Set) {
				path := filepath.Join(set.Path(), "receipt.json")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			message: "not in canonical form",
		},
		{
			name: "changed size",
			mutate: func(t *testing.T, set artifactset.Set) {
				path := filepath.Join(set.Path(), filepath.FromSlash(set.Files()[0].Path))
				if err := os.WriteFile(path, []byte("short"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			message: "receipt records",
		},
		{
			name: "unexpected file",
			mutate: func(t *testing.T, set artifactset.Set) {
				if err := os.WriteFile(filepath.Join(set.Path(), "extra"), []byte("extra"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			message: "unexpected file",
		},
		{
			name: "symlinked data file",
			mutate: func(t *testing.T, set artifactset.Set) {
				path := filepath.Join(set.Path(), filepath.FromSlash(set.Files()[0].Path))
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("elsewhere", path); err != nil {
					t.Fatal(err)
				}
			},
			message: "is a symlink",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			set, data := fixtureSet(t, t.TempDir())
			materialize(t, set, data)
			test.mutate(t, set)
			if err := set.Verify(); err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error = %v, want %q", err, test.message)
			}
		})
	}
}

func fixtureSet(t *testing.T, root string) (artifactset.Set, map[string][]byte) {
	t.Helper()
	data := map[string][]byte{
		"model/nested/model.gguf":            []byte("weights"),
		"patches/stable-template/chat.jinja": []byte("template"),
	}
	entry := lockfile.Entry{
		Repo:     "owner/model",
		Revision: strings.Repeat("1", 40),
		Files: []lockfile.File{{
			Name:   "nested/model.gguf",
			SHA256: hash(data["model/nested/model.gguf"]),
		}},
		Patches: []lockfile.Patch{{
			Name:   "stable-template",
			SHA256: hash(data["patches/stable-template/chat.jinja"]),
		}},
		Resolved: "2026-08-20",
	}
	layout := manifest.Layout{
		Model:        manifest.Model{Repo: "owner/model", File: "nested/model.gguf"},
		ChatTemplate: "stable-template",
	}
	set, err := artifactset.New(root, "coder", layout, entry, map[string]manifest.Patch{
		"stable-template": {File: "chat.jinja"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return set, data
}

func materialize(t *testing.T, set artifactset.Set, data map[string][]byte) {
	t.Helper()
	records := make([]artifactset.Record, 0, len(set.Files()))
	for _, file := range set.Files() {
		content := data[file.Path]
		path := filepath.Join(set.Path(), filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
		records = append(records, artifactset.Record{Path: file.Path, SHA256: file.SHA256, Size: int64(len(content))})
	}
	receipt, err := set.Receipt(records)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(set.Path(), "receipt.json"), receipt, 0o644); err != nil {
		t.Fatal(err)
	}
}

func hash(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
