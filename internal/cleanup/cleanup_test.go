package cleanup

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/managed"
	"github.com/temper-sh/temper/internal/preset"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/installplan"
	"github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/receipt"
	"github.com/temper-sh/temper/internal/testfixture"
)

const savedPreset = "qwen3.5-4b-q4km-off"

func compiled(t *testing.T, id string) catalog.Lock {
	t.Helper()
	raw, err := os.ReadFile(testfixture.CatalogPath())
	if err != nil {
		t.Fatal(err)
	}
	d, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := catalog.CompilePreset(d, id, 0, 0, software.Target{OS: "darwin", Arch: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	return lock
}

func mkdir(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeReceipt records an installation of lock as software install would.
func writeReceipt(t *testing.T, root string, lock lockfile.Document) string {
	t.Helper()
	digest, err := lock.SemanticDigest()
	if err != nil {
		t.Fatal(err)
	}
	id := "preset-" + digest
	dir := mkdir(t, filepath.Join(root, "software", "installations", id))
	r := receipt.Document{Schema: receipt.SchemaV1, Installation: id, SoftwareLockDigest: digest, Target: lock.Target, Root: root,
		ObservedAt: "2026-10-14T10:00:00Z", Requirements: []receipt.Requirement{}, Selections: map[string]receipt.Selection{}, Units: map[string]receipt.Unit{}}
	for sid, s := range lock.Selections {
		r.Selections[sid] = receipt.Selection{Provenance: s.Provenance, Method: s.Method, Adapter: s.Adapter, RecipeRevision: s.RecipeRevision, RootUnit: s.RootUnit}
	}
	for uid, u := range lock.Units {
		r.Units[uid] = receipt.Unit{Adapter: u.Adapter, Scope: u.Scope, NativeName: u.NativeName, Version: u.Version, Revision: u.Revision,
			Dependencies: u.Dependencies, Artifacts: u.Artifacts, Location: filepath.Join(dir, u.Adapter, u.Scope), Ownership: installplan.OwnershipTemperAdded}
	}
	raw, err := receipt.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "installation-receipt.yaml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return id
}

type recordingRemover struct{ removed []string }

func (r *recordingRemover) Remove(_ context.Context, _ string, installation string, lock lockfile.Document, _ io.Writer) error {
	digest, err := lock.SemanticDigest()
	if err != nil || "preset-"+digest != installation {
		return err
	}
	r.removed = append(r.removed, installation)
	return nil
}

func TestCleanupRemovesOnlyWhatNothingUses(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	saved := compiled(t, savedPreset)
	configuration := setup.EmptyConfiguration()
	configuration.Presets[savedPreset] = setup.Preset{Name: "Saved", Lock: saved}
	sets, err := preset.Software(saved)
	if err != nil {
		t.Fatal(err)
	}
	usedInstallation := mkdir(t, filepath.Join(root, "software", "installations", sets[0].ID))

	// An old preset's software, models and Splash cache.
	old := compiled(t, "ettin-reranker-150m-q8-crispembed")
	oldSets, err := preset.Software(old)
	if err != nil {
		t.Fatal(err)
	}
	var oldInstallation string
	for _, set := range oldSets {
		if set.ID != sets[0].ID && !slices.ContainsFunc(sets, func(s preset.Installation) bool { return s.ID == set.ID }) {
			oldInstallation = writeReceipt(t, root, set.Lock)
			break
		}
	}
	if oldInstallation == "" {
		t.Fatal("fixture presets share all software; pick presets with different engines")
	}
	unverified := mkdir(t, filepath.Join(root, "software", "installations", "preset-"+strings.Repeat("c", 64)))
	oldModels := mkdir(t, filepath.Join(root, "artifacts", "layouts", "old-preset", strings.Repeat("d", 64)))
	oldCache := mkdir(t, filepath.Join(root, "runtime", "splash", strings.Repeat("e", 64)))
	oldGeneration := mkdir(t, filepath.Join(root, "managed", "generations", strings.Repeat("f", 64)))
	hf := t.TempDir()
	mkdir(t, filepath.Join(hf, "models--someone--unused-model", "blobs"))
	for _, a := range saved.Records.Artifacts {
		for _, s := range a.Sources {
			mkdir(t, filepath.Join(hf, "models--"+strings.ReplaceAll(s.Repo, "/", "--"), "blobs"))
		}
	}

	plan, err := Build(root, configuration, managed.State{Schema: managed.StateSchema}, hf)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, item := range plan.Remove {
		paths = append(paths, item.Path)
	}
	for _, want := range []string{filepath.Join(root, "software", "installations", oldInstallation), oldModels, oldCache, oldGeneration} {
		if !slices.Contains(paths, want) {
			t.Errorf("plan keeps unused %s; plan: %v", want, paths)
		}
	}
	if slices.Contains(paths, usedInstallation) {
		t.Error("plan removes the saved preset's software")
	}
	if slices.Contains(paths, unverified) || len(plan.Kept) != 1 || !strings.Contains(plan.Kept[0], "no installation receipt") {
		t.Errorf("an installation without a receipt must stay: %v", plan.Kept)
	}
	if len(plan.UnusedRepos) != 1 || plan.UnusedRepos[0].ID != "someone/unused-model" {
		t.Errorf("unused repos = %+v", plan.UnusedRepos)
	}

	remover := &recordingRemover{}
	if err := Apply(context.Background(), root, plan, remover); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(remover.removed, []string{oldInstallation}) {
		t.Errorf("software went around the remover: %v", remover.removed)
	}
	for _, path := range paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s still exists", path)
		}
	}
	if _, err := os.Stat(usedInstallation); err != nil {
		t.Errorf("used installation removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(hf, "models--someone--unused-model")); err != nil {
		t.Error("cleanup touched the Hugging Face cache")
	}
}

// A running layout may still use material its saved presets no longer name.
func TestCleanupKeepsWhatTheRunningLayoutUses(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	old := compiled(t, "ettin-reranker-150m-q8-crispembed")
	sets, err := preset.Software(old)
	if err != nil {
		t.Fatal(err)
	}
	id := writeReceipt(t, root, sets[0].Lock)
	generation := strings.Repeat("a", 64)
	mkdir(t, filepath.Join(root, "managed", "generations", generation))
	engine := filepath.Join(root, "software", "installations", id, "upstream-release", "llama-cpp", "current", "payload", "llama-server")
	calibration := mkdir(t, filepath.Join(root, "runtime", "kapitsa", strings.Repeat("b", 64)))
	job := &managed.Job{Generation: generation, Processes: []managed.Process{{Role: "x", Path: engine}}, StatePaths: []string{calibration}}
	plan, err := Build(root, setup.EmptyConfiguration(), managed.State{Schema: managed.StateSchema, Current: job}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Remove) != 0 {
		t.Fatalf("plan removes what the running layout uses: %+v", plan.Remove)
	}
	plan, err = Build(root, setup.EmptyConfiguration(), managed.State{Schema: managed.StateSchema}, "")
	if err != nil || !slices.ContainsFunc(plan.Remove, func(i Item) bool { return i.Path == calibration }) {
		t.Fatal("unused calibration not eligible for explicit cleanup", err, plan.Remove)
	}
}

func TestApplyRefusesPathsOutsideTheRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	err := Apply(context.Background(), root, Plan{Remove: []Item{{Kind: KindRuntime, Path: outside}}}, &recordingRemover{})
	if err == nil {
		t.Fatal("removed a path outside the root")
	}
	if _, statErr := os.Stat(outside); statErr != nil {
		t.Fatal(statErr)
	}
}
