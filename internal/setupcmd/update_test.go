package setupcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"path/filepath"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/userpreset"
)

const extraction = "nuextract3-q4km-llama"

func updateRun(c Command, args ...string) (int, string, string) {
	var out, err bytes.Buffer
	code := c.Update(context.Background(), args, &out, &err)
	return code, out.String(), err.String()
}

// newerRouter is the guided catalog with a later router release.
func newerRouter(t *testing.T) catalog.Document {
	t.Helper()
	d := guidedCatalog(t)
	router := d.Runtime.Router
	release := *router.Supply.Release
	release.Version = "v263"
	release.Artifact.Locator = router.Supply.Source.ReleaseURL("v263")
	release.Artifact.SHA256 = strings.Repeat("1", 64)
	router.Supply.Release = &release
	d.Runtime.Router = router
	return d
}

func newest(d catalog.Document) func(context.Context, string, bool) (catalog.View, error) {
	return func(context.Context, string, bool) (catalog.View, error) { return catalog.View{Document: d}, nil }
}

type updateResult struct {
	Changed bool `json:"changed"`
	Presets []presetUpdate
}

func decodeUpdate(t *testing.T, output string) (updateResult, map[string]presetUpdate) {
	t.Helper()
	var result updateResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err, output)
	}
	byID := map[string]presetUpdate{}
	for _, p := range result.Presets {
		byID[p.ID] = p
	}
	return result, byID
}

func TestUpdateRebuildsFromTheNewestCatalogAndKeepsChoices(t *testing.T) {
	// Update installs what changed; nothing runs, so nothing restarts.
	var started []string
	c := installableCommand(t, &started, nil)
	defer func() {
		if len(started) != 0 {
			t.Errorf("update started a stopped root: %v", started)
		}
	}()
	root := filepath.Join(t.TempDir(), "root")
	if code, _, err := configureRun(c, "--root", root, "--preset", extraction, "--preset", compactLayout,
		"--context", extraction+"=4096", "--compute", extraction+"=cpu"); code != 0 {
		t.Fatal(err)
	}
	_, rev, err := setup.ReadConfiguration(root)
	if err != nil {
		t.Fatal(err)
	}
	next := newerRouter(t)
	delete(next.Presets, compactLayout)
	c.Newest = newest(next)
	if code, _, err := updateRun(c, "--root", root, "--dry-run", "--json"); code != 0 {
		t.Fatal(err)
	}
	if _, after, _ := setup.ReadConfiguration(root); after != rev {
		t.Fatal("dry run saved")
	}
	code, output, diagnostics := updateRun(c, "--root", root, "--json")
	if code != 0 {
		t.Fatal(diagnostics)
	}
	result, presets := decodeUpdate(t, output)
	if got := presets[extraction]; !result.Changed || got.Status != "updated" || !slices.Contains(got.Changes, "router llama-swap v262 → v263") || len(got.Changes) != 1 {
		t.Fatalf("updated preset = %+v", got)
	}
	if got := presets[compactLayout]; got.Status != "kept" || got.Reason != "left the catalog; software updated" {
		t.Fatalf("preset that left the catalog = %+v", got)
	}
	saved, _, err := setup.ReadConfiguration(root)
	if err != nil {
		t.Fatal(err)
	}
	lock := saved.Presets[extraction].Lock
	if p := lock.Records.Presets[lock.Preset]; p.Compute != catalog.ComputeCPU || p.Window() != 4096 || saved.Presets[extraction].Choices.ContextWindow != 4096 {
		t.Fatal("update lost the user's choices")
	}
	for id, p := range saved.Presets {
		if v := p.Lock.Records.Runtime.Router.Supply.Release.Version; v != "v263" {
			t.Fatalf("%s kept router %s; one router per layout", id, v)
		}
	}
	code, output, diagnostics = updateRun(c, "--root", root, "--json")
	if result, presets := decodeUpdate(t, output); code != 0 || result.Changed || presets[extraction].Status != "unchanged" {
		t.Fatalf("second update: %d %s %s", code, output, diagnostics)
	}
}

func TestUpdateUsesAuthoredSourceAndKeepsItsLockWhenFileIsInvalidOrMissing(t *testing.T) {
	for _, form := range []string{"fork", "copy"} {
		t.Run(form, func(t *testing.T) {
			ctx := context.Background()
			root := filepath.Join(t.TempDir(), "root")
			var started []string
			c := installableCommand(t, &started, nil)
			doc := guidedCatalog(t)
			file := catalog.UserPreset{Schema: catalog.UserPresetSchema, Origin: compactLayout, Name: "Mine",
				Changes: map[string]any{"request_defaults": map[string]any{"sampling": map[string]any{"temperature": 0.6}}}}
			if form == "copy" {
				lock, err := setup.BuildPreset(doc, compactLayout, 8, setup.Choices{})
				if err != nil {
					t.Fatal(err)
				}
				file = catalog.CopyOf(lock, "Mine")
			}
			writeLayoutPreset(t, root, "mine", file)
			loaded, err := userpreset.Load(root, "mine", catalog.View{Document: doc})
			if err != nil {
				t.Fatal(err)
			}
			lock, err := setup.BuildPreset(loaded.Document, "mine", 8, setup.Choices{})
			if err != nil {
				t.Fatal(err)
			}
			before := setup.EmptyConfiguration()
			before.Presets["mine"] = setup.Preset{Name: "Mine", UserPreset: "mine", Lock: lock}
			before.Layouts = setup.StarterLayouts([]string{"mine"})
			if _, _, err := setup.SaveConfiguration(ctx, root, before, "", false); err != nil {
				t.Fatal(err)
			}
			next := newerRouter(t)
			origin := next.Presets[compactLayout]
			origin.RequestDefaults.MaxOutputTokens = 1024
			next.Presets[compactLayout] = origin
			// A catalog collision must never change which source owns this preset.
			next.Presets["mine"] = next.Presets[extraction]
			c.Newest = newest(next)
			if code, _, diagnostics := updateRun(c, "--root", root, "--json"); code != 0 {
				t.Fatal(diagnostics)
			}
			saved, _, err := setup.ReadConfiguration(root)
			if err != nil {
				t.Fatal(err)
			}
			preset := saved.Presets["mine"]
			record := preset.Lock.Records.Presets["mine"]
			wantOutput, wantTemperature := 1024, 0.6
			if form == "copy" {
				wantOutput, wantTemperature = 4096, 0.7
			}
			if preset.UserPreset != "mine" || record.RequestDefaults.MaxOutputTokens != wantOutput || *record.RequestDefaults.Sampling.Temperature != wantTemperature || preset.Lock.Records.Runtime.Router.Supply.Release.Version != "v263" {
				t.Fatalf("update lost authored ownership: source=%q output=%d temperature=%g", preset.UserPreset, record.RequestDefaults.MaxOutputTokens, *record.RequestDefaults.Sampling.Temperature)
			}
			if form == "copy" && preset.ForkBaseline != nil {
				t.Fatal("independent copy acquired fork tracking")
			}
			if code, output, diagnostics := updateRun(c, "--root", root, "--json"); code != 0 {
				t.Fatal(diagnostics)
			} else if result, _ := decodeUpdate(t, output); result.Changed {
				t.Fatal("unchanged authored update rewrote configuration")
			}
			router := next.Runtime.Router
			release := *router.Supply.Release
			release.Version = "v264"
			release.Artifact.Locator = router.Supply.Source.ReleaseURL(release.Version)
			router.Supply.Release = &release
			next.Runtime.Router = router
			c.Newest = newest(next)
			for _, missing := range []bool{false, true} {
				if missing {
					err = os.Remove(userpreset.Path(root, "mine"))
				} else {
					err = os.WriteFile(userpreset.Path(root, "mine"), []byte("invalid json"), 0600)
				}
				if err != nil {
					t.Fatal(err)
				}
				code, output, diagnostics := updateRun(c, "--root", root, "--json")
				result, records := decodeUpdate(t, output)
				if code != 0 || result.Changed || records["mine"].Status != "kept" || !strings.Contains(records["mine"].Reason, "user preset") {
					t.Fatalf("invalid/missing file did not keep working lock: %d %s %s", code, output, diagnostics)
				}
				after, _, err := setup.ReadConfiguration(root)
				if err != nil || !sameLock(preset.Lock, after.Presets["mine"].Lock) || after.Presets["mine"].UserPreset != "mine" {
					t.Fatal("unavailable authoring changed source or exact lock", err)
				}
			}
			if len(started) != 0 {
				t.Fatal("update activated a stopped root", started)
			}
		})
	}
}

func TestUpdateSoftwareKeepsSettingsAndMarksUntestedCombinations(t *testing.T) {
	// Update installs what changed; nothing runs, so nothing restarts.
	var started []string
	c := installableCommand(t, &started, nil)
	defer func() {
		if len(started) != 0 {
			t.Errorf("update started a stopped root: %v", started)
		}
	}()
	root := filepath.Join(t.TempDir(), "root")
	if code, _, err := configureRun(c, "--root", root, "--preset", extraction); code != 0 {
		t.Fatal(err)
	}
	next := newerRouter(t)
	p := next.Presets[extraction]
	p.RequestDefaults.MaxOutputTokens = 0
	next.Presets[extraction] = p
	c.Newest = newest(next)
	code, output, diagnostics := updateRun(c, "--root", root, "--software", "--json")
	_, presets := decodeUpdate(t, output)
	if got := presets[extraction]; code != 0 || got.Status != "updated" || len(got.Changes) != 1 || got.Note == "" {
		t.Fatalf("software update: %d %+v %s", code, got, diagnostics)
	}
	saved, _, _ := setup.ReadConfiguration(root)
	if lock := saved.Presets[extraction].Lock; lock.Records.Presets[extraction].RequestDefaults.MaxOutputTokens != 1536 {
		t.Fatal("software update changed settings")
	}
	code, output, diagnostics = updateRun(c, "--root", root, "--json")
	_, presets = decodeUpdate(t, output)
	if got := presets[extraction]; code != 0 || got.Status != "updated" || got.Note != "" || !slices.Contains(got.Changes, "settings changed") {
		t.Fatalf("full update after software update: %d %+v %s", code, got, diagnostics)
	}
	saved, _, err := setup.ReadConfiguration(root)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Presets[extraction].Lock.Records.Presets[extraction].RequestDefaults.MaxOutputTokens != 0 {
		t.Fatal("full update did not adopt the catalog's omitted output default")
	}
}

func TestConfigureSavedPresetShowsOldToNew(t *testing.T) {
	c := fixtureCommand(t)
	root := filepath.Join(t.TempDir(), "root")
	if code, _, err := configureRun(c, "--root", root, "--preset", extraction); code != 0 {
		t.Fatal(err)
	}
	_, rev, _ := setup.ReadConfiguration(root)
	doc := newerRouter(t)
	c.Catalog = func(context.Context, string, string, bool) (catalog.View, error) {
		return catalog.View{Document: doc}, nil
	}
	code, output, diagnostics := configureRun(c, "--root", root, "--preset", extraction, "--revision", rev)
	if code != 0 || !strings.Contains(output, extraction+": updated\n  - router llama-swap v262 → v263\n") {
		t.Fatalf("%d %s %s", code, output, diagnostics)
	}
}

func TestConfigureJSONIsOneDocument(t *testing.T) {
	c := fixtureCommand(t)
	root := filepath.Join(t.TempDir(), "root")
	if code, _, err := configureRun(c, "--root", root, "--preset", extraction); code != 0 {
		t.Fatal(err)
	}
	_, rev, _ := setup.ReadConfiguration(root)
	doc := newerRouter(t)
	c.Catalog = func(context.Context, string, string, bool) (catalog.View, error) {
		return catalog.View{Document: doc}, nil
	}
	// The edit has old → new changes to report; with --json they must not
	// reach stdout ahead of the document.
	code, output, diagnostics := configureRun(c, "--root", root, "--preset", extraction, "--revision", rev, "--json")
	var result map[string]any
	decoder := json.NewDecoder(strings.NewReader(output))
	if code != 0 || decoder.Decode(&result) != nil || result["schema"] != "temper-configuration-result/v1" || decoder.More() {
		t.Fatalf("stdout is not exactly one JSON document: %d %q %s", code, output, diagnostics)
	}
}

func TestUpdateNeedsASavedConfiguration(t *testing.T) {
	c := fixtureCommand(t)
	c.Newest = func(context.Context, string, bool) (catalog.View, error) {
		t.Fatal("update downloaded a catalog without a configuration")
		return catalog.View{}, nil
	}
	if code, _, err := updateRun(c, "--root", filepath.Join(t.TempDir(), "root")); code == 0 || !strings.Contains(err, "temper init") {
		t.Fatalf("%d %s", code, err)
	}
}
