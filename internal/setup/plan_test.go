package setup_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/artifactset"
	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/software"
)

const (
	compactLocal   = "qwen3.5-4b-q4km-off"
	compactUtility = "compact-alternative"
	largeLocal     = "qwen3.8-27b-q4xl-mtp"
	compactLayout  = "qwen3.5-4b-q4km-off"
)

func catalogDocument(t *testing.T) catalog.Document {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "catalog", "guided-setup.json"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	alternative := d.Presets[compactLayout]
	alternative.ContextWindowTokens = 16384
	d.Presets[compactUtility] = alternative
	return d
}

func selectedLock(t *testing.T, d catalog.Document, profile string) catalog.Lock {
	t.Helper()
	locked, err := catalog.CompilePreset(d, profile, "", 0, software.Target{OS: "darwin", Arch: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	return locked
}

func facts(gib int64) machine.Facts {
	mib := gib * 1024
	return machine.Facts{
		Schema:        machine.FactsSchemaV1,
		Target:        software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "26.6"},
		HardwareModel: "MacTest,1", Chip: "Apple test chip", OSBuild: "25G1",
		PhysicalMemoryBytes:     gib << 30,
		MetalDeviceMemoryMiB:    mib * 81 / 100,
		MetalDeviceMemorySource: machine.MetalDeviceSourcePredicted,
		WiredLimitMiB:           mib * 65 / 100,
		WiredLimitSource:        budget.WiredSourcePredicted,
	}
}

func planFor(t *testing.T, root string, gib int64, profiles ...string) setup.Plan {
	t.Helper()
	d := catalogDocument(t)
	locks := make([]catalog.Lock, 0, len(profiles))
	for _, profile := range profiles {
		locks = append(locks, selectedLock(t, d, profile))
	}
	plan, err := buildPlan(root, facts(gib), 100<<30, locks, setup.Material{})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestCompactMainIsEligibleAtPredictedSmallMemoryWalls(t *testing.T) {
	d := catalogDocument(t)
	compact := selectedLock(t, d, compactLocal)
	large := selectedLock(t, d, largeLocal)
	for _, gib := range []int64{8, 16, 24, 32} {
		t.Run(setup.Size(gib<<30), func(t *testing.T) {
			plan, err := buildPlan(filepath.Join(t.TempDir(), "root"), facts(gib), 100<<30, []catalog.Lock{compact}, setup.Material{})
			if err != nil {
				t.Fatal(err)
			}
			if !plan.CanPrepare || len(plan.Refusals) != 0 || plan.Presets[0].Budget.Status != budget.StatusNotApplicable {
				t.Fatalf("compact plan at %d GiB: can_prepare=%v, wall=%s, refusals=%v", gib, plan.CanPrepare, plan.Presets[0].Budget.Status, plan.Refusals)
			}
		})
	}
	for _, gib := range []int64{8, 16, 24} {
		plan, err := buildPlan(filepath.Join(t.TempDir(), "root"), facts(gib), 100<<30, []catalog.Lock{large}, setup.Material{})
		if err != nil {
			t.Fatal(err)
		}
		if plan.CanPrepare || len(plan.Refusals) == 0 {
			t.Fatalf("large model unexpectedly eligible at %d GiB", gib)
		}
	}
}

func TestDiskShortfallRefusesPreparationWithoutDiscardingChoices(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	ready := planFor(t, root, 16, compactLocal)
	plan, err := buildPlan(root, facts(16), ready.FreshDiskBytes-1, []catalog.Lock{ready.Presets[0].Lock}, setup.Material{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.CanPrepare || !strings.Contains(strings.Join(plan.Refusals, " "), "disk") || len(plan.Presets) != 1 {
		t.Fatalf("disk shortfall did not retain reviewable choice and refusal: %+v", plan)
	}
}

func tinyModelLocks(t *testing.T) []catalog.Lock {
	t.Helper()
	d := catalogDocument(t)
	artifact := d.Artifacts["qwen3.5-4b-q4km"]
	content := []byte("tiny")
	sum := sha256.Sum256(content)
	artifact.Files[0].Bytes = int64(len(content))
	artifact.Files[0].SHA256 = hex.EncodeToString(sum[:])
	d.Artifacts["qwen3.5-4b-q4km"] = artifact
	return []catalog.Lock{selectedLock(t, d, compactLocal), selectedLock(t, d, compactUtility)}
}

func writeTinyModelSet(t *testing.T, root string, locked catalog.Lock) string {
	t.Helper()
	projection, err := locked.Projections()
	if err != nil {
		t.Fatal(err)
	}
	set, err := artifactset.New(root, locked.Preset, projection.Manifest.Layouts[locked.Preset], projection.Artifacts.Entries[locked.Preset], projection.Manifest.Patches)
	if err != nil {
		t.Fatal(err)
	}
	model := locked.Records.Artifacts["qwen3.5-4b-q4km"].Files[0]
	modelPath := filepath.Join(set.Path(), "model", model.Path)
	if err := os.MkdirAll(filepath.Dir(modelPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modelPath, []byte("tiny"), 0o600); err != nil {
		t.Fatal(err)
	}
	receipt, err := set.Receipt([]artifactset.Record{{Path: "model/" + model.Path, SHA256: model.SHA256, Size: model.Bytes}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(set.Path(), "receipt.json"), receipt, 0o600); err != nil {
		t.Fatal(err)
	}
	return set.Path()
}

func TestInspectedModelSetReducesRemainingDiskAndDownloadAllowance(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	locks := tinyModelLocks(t)
	fresh, err := buildPlan(root, facts(16), 100<<30, locks, setup.Material{})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.RemainingDiskBytes != fresh.FreshDiskBytes || fresh.RemainingDownloadBytes != fresh.DownloadBytes {
		t.Fatal("fresh wrapper unexpectedly credited absent material")
	}
	absent, err := setup.InspectModels(root, locks)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("inspection created root: %v", err)
	}
	without, err := buildPlan(root, facts(16), fresh.FreshDiskBytes-1, locks, absent)
	if err != nil {
		t.Fatal(err)
	}
	if without.CanPrepare {
		t.Fatal("absent model received disk credit")
	}
	if summary := without.WeightSummary(); summary != "Weights: 4 B to download on Prepare." {
		t.Fatalf("absent weights not disclosed: %s", summary)
	}
	for _, item := range without.Downloads {
		if item.Cached || item.Kind == "model" && item.Action() != "Download on Prepare" {
			t.Fatalf("absent file presented as reusable: %+v", item)
		}
	}
	writeTinyModelSet(t, root, locks[0])
	material, err := setup.InspectModels(root, locks)
	if err != nil {
		t.Fatal(err)
	}
	remaining, err := buildPlan(root, facts(16), fresh.FreshDiskBytes-1, locks, material)
	if err != nil {
		t.Fatal(err)
	}
	if !remaining.CanPrepare || remaining.FreshDiskBytes != fresh.FreshDiskBytes || remaining.DownloadBytes != fresh.DownloadBytes {
		t.Fatalf("verified reuse changed fresh totals or still refused: %+v", remaining)
	}
	if remaining.RemainingDiskBytes != fresh.FreshDiskBytes-4 || remaining.RemainingDownloadBytes != fresh.DownloadBytes-4 {
		t.Fatalf("shared four-byte model was not credited exactly once: fresh=%+v, remaining=%+v", fresh, remaining)
	}
	for _, item := range remaining.RemainingDownloads {
		if item.Name == "Qwen3.5-4B-Q4_K_M.gguf" {
			t.Fatal("verified model remains in download list")
		}
	}
	if !strings.Contains(strings.Join(remaining.Lines(), "\n"), "Remaining installation allowance") {
		t.Fatal("preview omitted remaining allowance")
	}
	if summary := remaining.WeightSummary(); summary != "Weights: all cached in Temper (4 B). No weight download on Prepare." {
		t.Fatalf("cached weights not disclosed: %s", summary)
	}
	for _, item := range remaining.Downloads {
		if item.Kind == "model" && (!item.Cached || item.Action() != "Cached in Temper") {
			t.Fatalf("cached weight still presented as a transfer: %+v", item)
		}
		if item.Kind == "software" && (item.Cached || item.Action() != "May download") {
			t.Fatalf("uninspected software presented as certain: %+v", item)
		}
	}
	lines := strings.Join(remaining.Lines(), "\n")
	for _, want := range []string{"Qwen3.5-4B-Q4_K_M.gguf — 4 B — Cached in Temper"} {
		if !strings.Contains(lines, want) {
			t.Fatalf("missing transfer disclosure %q: %s", want, lines)
		}
	}
}

func TestTemplateVariantsCountWeightsOnceAndCreditOtherInstalledComposition(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	locks := tinyModelLocks(t)
	baseline, err := buildPlan(root, facts(16), 100<<30, locks, setup.Material{})
	if err != nil {
		t.Fatal(err)
	}
	d := locks[1].Records
	patchBytes := "template"
	sum := sha256.Sum256([]byte(patchBytes))
	d.Patches = map[string]catalog.Patch{"compact-template": {
		Repo: "example/templates", Revision: strings.Repeat("a", 40), License: "Apache-2.0",
		Files:               []catalog.File{{Path: "chat.jinja", Bytes: int64(len(patchBytes)), SHA256: hex.EncodeToString(sum[:])}},
		CompatibleArtifacts: []string{"qwen3.5-4b-q4km"},
	}}
	variant, err := catalog.CompilePreset(d, compactUtility, "compact-template", 0, software.Target{OS: "darwin", Arch: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	locks[1] = variant
	fresh, err := buildPlan(root, facts(16), 100<<30, locks, setup.Material{})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.DownloadBytes != baseline.DownloadBytes+8 || fresh.FreshDiskBytes != baseline.FreshDiskBytes+8 {
		t.Fatalf("template variant counted another model copy: download delta=%d, disk delta=%d, want 8 each", fresh.DownloadBytes-baseline.DownloadBytes, fresh.FreshDiskBytes-baseline.FreshDiskBytes)
	}
	writeTinyModelSet(t, root, locks[0])
	// Only the other composition is selected: its model is still reusable.
	selected := []catalog.Lock{variant}
	material, err := setup.InspectModels(root, selected)
	if err != nil {
		t.Fatal(err)
	}
	without, err := buildPlan(root, facts(16), 100<<30, selected, setup.Material{})
	if err != nil {
		t.Fatal(err)
	}
	remaining, err := buildPlan(root, facts(16), without.FreshDiskBytes-1, selected, material)
	if err != nil {
		t.Fatal(err)
	}
	if !remaining.CanPrepare || remaining.RemainingDiskBytes != without.FreshDiskBytes-4 || remaining.RemainingDownloadBytes != without.DownloadBytes-4 {
		t.Fatalf("installed weights from another template were not credited: %+v", remaining)
	}
	var templateDownloads int
	for _, item := range remaining.RemainingDownloads {
		if item.Name == "compact-template/chat.jinja" {
			templateDownloads++
		}
		if item.Name == "Qwen3.5-4B-Q4_K_M.gguf" {
			t.Fatal("reused weights still listed for download")
		}
	}
	if templateDownloads != 1 {
		t.Fatal("new template must still be downloaded")
	}
	for _, item := range remaining.Downloads {
		if item.Kind == "template" && (item.Cached || item.Action() != "Download on Prepare") {
			t.Fatalf("new template incorrectly reported as cached: %+v", item)
		}
		if item.Kind == "model" && !item.Cached {
			t.Fatalf("reused weights incorrectly reported as missing: %+v", item)
		}
	}
}

func TestWeightSummarySeparatesMissingWeightsFromTemplatesAndSoftware(t *testing.T) {
	plan := setup.Plan{Downloads: []setup.Download{
		{Name: "cached.gguf", Bytes: 2 << 30, Kind: "model", Cached: true},
		{Name: "missing.gguf", Bytes: 3 << 30, Kind: "model"},
		{Name: "chat.jinja", Bytes: 3 << 10, Kind: "template"},
		{Name: "engine", Bytes: 33 << 20, Kind: "software"},
	}}
	if got := plan.WeightSummary(); got != "Weights: 3.00 GiB to download on Prepare; 2.00 GiB cached in Temper." {
		t.Fatalf("partial cache transfer disclosure = %s", got)
	}
}

func TestMalformedExistingSetRefusesReuse(t *testing.T) {
	t.Run("unexpected file", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "root")
		locks := tinyModelLocks(t)
		setPath := writeTinyModelSet(t, root, locks[0])
		if err := os.WriteFile(filepath.Join(setPath, "unexpected"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := setup.InspectModels(root, locks); err == nil || !strings.Contains(err.Error(), "malformed") {
			t.Fatalf("tampered set was credited or silently treated as absent: %v", err)
		}
	})
	t.Run("symlinked parent", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "root")
		locks := tinyModelLocks(t)
		writeTinyModelSet(t, root, locks[0])
		artifacts := filepath.Join(root, "artifacts")
		moved := filepath.Join(root, "elsewhere")
		if err := os.Rename(artifacts, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(moved, artifacts); err != nil {
			t.Fatal(err)
		}
		if _, err := setup.InspectModels(root, locks); err == nil || !strings.Contains(err.Error(), "real directory") {
			t.Fatalf("symlinked parent was credited or silently treated as absent: %v", err)
		}
	})
}

func buildPlan(root string, facts machine.Facts, free int64, locks []catalog.Lock, material setup.Material) (setup.Plan, error) {
	c := setup.EmptyConfiguration()
	var ids []string
	for i, locked := range locks {
		id := fmt.Sprintf("preset-%d", i)
		c.Presets[id] = setup.Preset{Name: id, Lock: locked}
		ids = append(ids, id)
	}
	c.Layouts["work"] = setup.Layout{Name: "Work", Presets: ids, IdleSeconds: 60}
	return setup.BuildConfiguration(root, facts, free, c, material)
}
