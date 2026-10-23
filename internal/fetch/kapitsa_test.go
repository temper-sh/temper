package fetch_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/artifactset"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/fetch"
	"github.com/temper-sh/temper/internal/testfixture"
)

func TestKapitsaFetchPublishesOnlyVerifiedDeclaredFilesAndRetriesCleanly(t *testing.T) {
	d := testfixture.KapitsaCatalog(t)
	id := "qwen3.8-27b-mlx4-kapitsa"
	source := &fakeSource{files: map[string]string{}}
	for id, a := range d.Artifacts {
		for i, f := range a.Files {
			data := a.HuggingFace().Repo + "/" + f.Path
			source.files[a.HuggingFace().Repo+"@"+a.HuggingFace().Revision+"/"+f.Path] = data
			a.Files[i].Bytes = int64(len(data))
			a.Files[i].SHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(data)))
		}
		d.Artifacts[id] = a
	}
	preset := d.Presets[id]
	preset.Profiles[0].Memory = nil
	d.Presets[id] = preset
	l, err := catalog.CompilePreset(d, id, 32, 0, d.Runtime.Router.Target)
	if err != nil {
		t.Fatal(err)
	}
	p, err := l.Projections()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "root")
	options := fetch.Options{Root: root, Layout: id, Manifest: p.Manifest, Lock: p.Artifacts}
	set, err := artifactset.New(root, id, p.Manifest.Layouts[id], p.Artifacts.Entries[id], p.Manifest.Patches)
	if err != nil {
		t.Fatal(err)
	}
	entry := p.Artifacts.Entries[id]
	key := entry.Draft.Repo + "@" + entry.Draft.Revision + "/model.safetensors"
	exact := source.files[key]
	source.files[key] = "corrupt"
	if _, err := runFetch(context.Background(), options, source); err == nil {
		t.Fatal("corrupt draft published")
	}
	if _, err := os.Stat(set.Path()); !os.IsNotExist(err) {
		t.Fatal("partial artifact set published")
	}
	source.files[key] = exact
	// An untrusted HF blob remains refused until the user repairs that cache.
	// Fetch never silently replaces a user's corrupt source material.
	blob := filepath.Join(root+"-hf", "models--"+strings.ReplaceAll(entry.Draft.Repo, "/", "--"), "blobs")
	for _, f := range entry.Draft.Files {
		if f.Name == "model.safetensors" {
			if err := os.Remove(filepath.Join(blob, f.SHA256)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := runFetch(context.Background(), options, source); err != nil {
		t.Fatal(err)
	}
	if err := set.Verify(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(set.ModelPath(), "model-00001-of-00003.safetensors"))
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("view copied or lost source weights", err)
	}
	before, err := os.Stat(filepath.Join(set.Path(), "receipt.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runFetch(context.Background(), options, source); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(filepath.Join(set.Path(), "receipt.json"))
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("clean rerun changed receipt")
	}
	if err := os.WriteFile(filepath.Join(set.ModelPath(), "._model-00001-of-00003.safetensors"), []byte("metadata"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := set.Verify(); err == nil {
		t.Fatal("engine would scan unexpected AppleDouble metadata")
	}
}
