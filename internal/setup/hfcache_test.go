package setup_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/setup"
)

func TestPreviewInspectsHFCacheWithoutCreatingRootsOrHashingWeights(t *testing.T) {
	directory := t.TempDir()
	root, cache := filepath.Join(directory, "temper"), filepath.Join(directory, "hub")
	locks := tinyModelLocks(t)
	material, err := setup.InspectModelsWithCache(root, locks, cache)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := buildPlan(root, facts(16), 100<<30, locks, material)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{root, cache} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("preview created %s: %v", path, err)
		}
	}
	artifact := locks[0].Records.Artifacts["qwen3.5-4b-q4km"]
	snapshot := filepath.Join(cache, "models--"+strings.ReplaceAll(artifact.HuggingFace().Repo, "/", "--"), "snapshots", artifact.HuggingFace().Revision, artifact.Files[0].Path)
	if err := os.MkdirAll(filepath.Dir(snapshot), 0o755); err != nil {
		t.Fatal(err)
	}
	// Same-sized bytes deliberately differ: preview checks presence/size;
	// the fetch tests establish that preparation rejects the content mismatch.
	if err := os.WriteFile(snapshot, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	material, err = setup.InspectModelsWithCache(root, locks, cache)
	if err != nil {
		t.Fatal(err)
	}
	material.InspectDownloadClient(func() error { return errors.New("hf missing") })
	plan, err := buildPlan(root, facts(16), 100<<30, locks, material)
	if err != nil {
		t.Fatal(err)
	}
	if plan.RemainingDownloadBytes != fresh.RemainingDownloadBytes-4 || plan.RemainingDiskBytes != fresh.RemainingDiskBytes-4 {
		t.Fatal("shared cached weights were not credited once")
	}
	if !plan.CanPrepare {
		t.Fatalf("a missing download client refused cached weights: %v", plan.Refusals)
	}
	for _, want := range []string{"Cached in Hugging Face", cache, "No weights to download", "verifies cached weights"} {
		if !strings.Contains(strings.Join(plan.Lines(), "\n"), want) {
			t.Fatalf("preview omitted %q", want)
		}
	}
	for _, path := range []string{root, filepath.Join(cache, ".locks")} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("cache preview wrote %s: %v", path, err)
		}
	}
	if err := os.WriteFile(snapshot, []byte("wrong size"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.InspectModelsWithCache(root, locks, cache); err == nil || !strings.Contains(err.Error(), "catalog requires 4") {
		t.Fatalf("malformed cache material was credited: %v", err)
	}
}

func TestMissingDownloadClientRefusesOnlyRemainingModelDownloads(t *testing.T) {
	directory := t.TempDir()
	root, cache := filepath.Join(directory, "temper"), filepath.Join(directory, "hub")
	locks := tinyModelLocks(t)
	material, err := setup.InspectModelsWithCache(root, locks, cache)
	if err != nil {
		t.Fatal(err)
	}
	material.InspectDownloadClient(func() error { return errors.New("hf missing; install it") })
	plan, err := buildPlan(root, facts(16), 100<<30, locks, material)
	if err != nil {
		t.Fatal(err)
	}
	if plan.CanPrepare || !strings.Contains(strings.Join(plan.Refusals, "\n"), "hf missing; install it") {
		t.Fatalf("missing client did not refuse a model download: %+v", plan.Refusals)
	}
	material.InspectDownloadClient(func() error { return nil })
	if plan, err = buildPlan(root, facts(16), 100<<30, locks, material); err != nil || !plan.CanPrepare {
		t.Fatalf("available client refused: %v %v", plan.Refusals, err)
	}
}
