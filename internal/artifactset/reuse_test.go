package artifactset_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/artifactset"
	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/manifest"
)

func TestFindModelsUsesPublishedV1ModelAcrossLayoutAndTemplateChoices(t *testing.T) {
	root := t.TempDir()
	// This published set has a template. A different layout/template choice
	// requests the same model hash without depending on either source identity.
	source, data := fixtureSet(t, root)
	materialize(t, source, data)
	modelHash := hash(data["model/nested/model.gguf"])
	patchHash := hash(data["patches/stable-template/chat.jinja"])
	models, err := artifactset.FindModels(root, []string{modelHash, patchHash})
	if err != nil {
		t.Fatal(err)
	}
	model, ok := models[modelHash]
	if !ok || model.Path != filepath.Join(source.Path(), "model", "nested", "model.gguf") || model.Size != int64(len(data["model/nested/model.gguf"])) {
		t.Fatalf("published model was not found by hash: %+v", models)
	}
	if _, ok := models[patchHash]; ok {
		t.Fatalf("patch hash was credited as model material: %+v", models)
	}
}

func TestFindModelsIgnoresStagesAndUnrelatedDamagedSets(t *testing.T) {
	root := t.TempDir()
	stage, stageData := fixtureSet(t, root)
	materialize(t, stage, stageData)
	private := filepath.Join(filepath.Dir(stage.Path()), ".stage-private")
	if err := os.Rename(stage.Path(), private); err != nil {
		t.Fatal(err)
	}
	modelHash := hash(stageData["model/nested/model.gguf"])
	models, err := artifactset.FindModels(root, []string{modelHash})
	if err != nil || len(models) != 0 {
		t.Fatalf("private stage was credited: %+v, %v", models, err)
	}

	badReceipt := reuseFixtureSet(t, root, "aaa", []byte("other-one"))
	if err := os.MkdirAll(badReceipt.Path(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badReceipt.Path(), "receipt.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	badShape := reuseFixtureSet(t, root, "aab", []byte("other-two"))
	materialize(t, badShape, map[string][]byte{"model/model.gguf": []byte("other-two")})
	if err := os.WriteFile(filepath.Join(badShape.Path(), "unexpected"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	valid, validData := fixtureSet(t, root)
	materialize(t, valid, validData)
	models, err = artifactset.FindModels(root, []string{modelHash})
	if err != nil {
		t.Fatalf("unrelated damaged sets blocked discovery: %v", err)
	}
	if got := models[modelHash].Path; got != filepath.Join(valid.Path(), "model", "nested", "model.gguf") {
		t.Fatalf("valid published model path = %q", got)
	}
}

func TestFindModelsRefusesMalformedSetClaimingRequestedModel(t *testing.T) {
	for _, damage := range []string{"changed size", "symlinked model"} {
		t.Run(damage, func(t *testing.T) {
			root := t.TempDir()
			set, data := fixtureSet(t, root)
			materialize(t, set, data)
			path := filepath.Join(set.Path(), "model", "nested", "model.gguf")
			if damage == "changed size" {
				if err := os.WriteFile(path, []byte("short"), 0o644); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("elsewhere", path); err != nil {
					t.Fatal(err)
				}
			}
			models, err := artifactset.FindModels(root, []string{hash(data["model/nested/model.gguf"])})
			if err == nil || len(models) != 0 {
				t.Fatalf("malformed matching set was credited: %+v, %v", models, err)
			}
			if damage == "changed size" && !strings.Contains(err.Error(), "receipt records") {
				t.Fatalf("changed size error = %v", err)
			}
			if damage == "symlinked model" && !strings.Contains(err.Error(), "symlink") {
				t.Fatalf("symlink error = %v", err)
			}
		})
	}
}

func reuseFixtureSet(t *testing.T, root, layoutID string, model []byte) artifactset.Set {
	t.Helper()
	entry := lockfile.Entry{
		Repo: "owner/model", Revision: strings.Repeat("1", 40), Resolved: "2026-08-20",
		Files: []lockfile.File{{Name: "model.gguf", SHA256: hash(model)}},
	}
	set, err := artifactset.New(root, layoutID,
		manifest.Layout{Model: manifest.Model{Repo: "owner/model", File: "model.gguf"}}, entry, nil)
	if err != nil {
		t.Fatal(err)
	}
	return set
}
