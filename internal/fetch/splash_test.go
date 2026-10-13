package fetch_test

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
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/fetch"
	"github.com/temper-sh/temper/internal/lockfile"
)

func TestSplashFetchPinsDraftReusesTargetAndRejectsPartialPublication(t *testing.T) {
	raw, err := os.ReadFile("../testfixture/testdata/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	source := &fakeSource{files: map[string]string{}}
	hash := func(data string) string { sum := sha256.Sum256([]byte(data)); return hex.EncodeToString(sum[:]) }
	for id, artifact := range doc.Artifacts {
		for i, file := range artifact.Files {
			data := artifact.HuggingFace().Repo + "/" + file.Path
			source.files[artifact.HuggingFace().Repo+"@"+artifact.HuggingFace().Revision+"/"+file.Path] = data
			artifact.Files[i].Bytes = int64(len(data))
			artifact.Files[i].SHA256 = hash(data)
		}
		doc.Artifacts[id] = artifact
	}
	for id, template := range doc.Templates {
		for i, file := range template.Files {
			data := "template " + id
			source.files[template.HuggingFace().Repo+"@"+template.HuggingFace().Revision+"/"+file.Path] = data
			template.Files[i].Bytes = int64(len(data))
			template.Files[i].SHA256 = hash(data)
		}
		doc.Templates[id] = template
	}
	root := filepath.Join(t.TempDir(), "root")
	write := func(profile, layout string) (fetch.Options, catalog.Projections) {
		t.Helper()
		locked, err := catalog.CompilePreset(doc, layout, 0, 32768, doc.Runtime.Router.Target)
		if err != nil {
			t.Fatal(err)
		}
		projection, err := locked.Projections()
		if err != nil {
			t.Fatal(err)
		}
		return fetch.Options{Root: root, Layout: layout, Manifest: projection.Manifest, Lock: projection.Artifacts}, projection
	}
	baseline, b := write("qwen3.8-27b-q4xl-local", "qwen3.8-27b-q4xl-mtp")
	if _, err := runFetch(context.Background(), baseline, source); err != nil {
		t.Fatal(err)
	}
	selected, p := write("qwen3.8-27b-splash-local", "qwen3.8-27b-q4xl-splash")
	entry := p.Artifacts.Entries[selected.Layout]
	key := entry.Draft.Repo + "@" + entry.Draft.Revision + "/model.safetensors"
	exact := source.files[key]
	source.files[key] = "broken download"
	if _, err := runFetch(context.Background(), selected, source); err == nil {
		t.Fatal("incorrect draft bytes accepted")
	}
	set, err := artifactset.New(root, selected.Layout, p.Manifest.Layouts[selected.Layout], entry, p.Manifest.Patches)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(set.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("partial draft published", err)
	}
	// HF owns cache repair. A corrupt completed download remains a refusal
	// until its cache entry is explicitly repaired; Temper never silently edits it.
	if _, err := runFetch(context.Background(), selected, source); err == nil {
		t.Fatal("corrupt cache silently accepted")
	}
	cached := filepath.Join(root+"-hf", "models--"+strings.ReplaceAll(entry.Draft.Repo, "/", "--"), "blobs", entry.Draft.Files[1].SHA256)
	if err := os.Remove(cached); err != nil {
		t.Fatal(err)
	}
	source.files[key] = exact
	source.openCalls = 0
	if _, err := runFetch(context.Background(), selected, source); err != nil {
		t.Fatal(err)
	}
	// The target is already admitted in the baseline; the draft config is cached
	// from the failed attempt. Retry downloads only the draft weights and patch.
	if source.openCalls != 2 {
		t.Fatalf("retry downloaded %d files", source.openCalls)
	}
	prior, err := artifactset.New(root, baseline.Layout, b.Manifest.Layouts[baseline.Layout], b.Artifacts.Entries[baseline.Layout], b.Manifest.Patches)
	if err != nil {
		t.Fatal(err)
	}
	original, _ := os.Stat(prior.ModelPath())
	shared, _ := os.Stat(set.ModelPath())
	if !os.SameFile(original, shared) {
		t.Fatal("target weights copied instead of reused")
	}
	if _, err := set.InspectContent(context.Background()); err != nil {
		t.Fatal(err)
	}
	if result, err := runFetch(context.Background(), selected, nil); err != nil || result.Changed {
		t.Fatal("second-run fetch changed the set", result, err)
	}
	// An explicit draft update must select a different set, independently of target.
	changed := entry
	draft := *entry.Draft
	draft.Files = append([]lockfile.File(nil), draft.Files...)
	draft.Files[1].SHA256 = strings.Repeat("0", 64)
	changed.Draft = &draft
	if changed.Digest() == entry.Digest() {
		t.Fatal("draft hash omitted from set identity")
	}
}
