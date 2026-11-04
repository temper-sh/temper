package setupcmd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/hfcache"
	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/managed"
	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/setupui"
	"github.com/temper-sh/temper/internal/userpreset"
)

func retirementFixture(t *testing.T) (Command, string, string, *[]string) {
	t.Helper()
	started := &[]string{}
	c := installableCommand(t, started, nil)
	c.CheckHFRemoval = func(context.Context) error { return nil }
	root := filepath.Join(t.TempDir(), "root")
	if code, _, diagnostic := configureRun(c, "--root", root, "--preset", compactLayout, "--json"); code != 0 {
		t.Fatal(diagnostic)
	}
	current, rev, err := setup.ReadConfiguration(root)
	if err != nil {
		t.Fatal(err)
	}
	current.Layouts["work"] = setup.Layout{Name: "Work", Presets: []string{compactLayout}, Startup: []string{compactLayout}, Default: compactLayout, IdleSeconds: 60}
	if _, _, err = setup.SaveConfiguration(context.Background(), root, current, rev, false); err != nil {
		t.Fatal(err)
	}
	d := guidedCatalog(t)
	delete(d.Presets, compactLayout)
	d.RetiredPresets = map[string]catalog.Retirement{compactLayout: {RetiredOn: d.Date, Reason: "Better resource choice", Successor: extraction}}
	c.Catalog = func(context.Context, string, string, bool) (catalog.View, error) {
		return catalog.View{Document: d}, nil
	}
	cache := t.TempDir()
	repo := guidedCatalog(t).Artifacts[guidedCatalog(t).Presets[compactLayout].Artifact].HuggingFace().Repo
	dir := filepath.Join(cache, "models--"+strings.ReplaceAll(repo, "/", "--"), "blobs")
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "old-weight"), []byte("retained until explicit deletion"), 0600); err != nil {
		t.Fatal(err)
	}
	artifact := guidedCatalog(t).Artifacts[guidedCatalog(t).Presets[compactLayout].Artifact]
	snapshot := filepath.Join(filepath.Dir(dir), "snapshots", artifact.HuggingFace().Revision, artifact.Files[0].Path)
	if err = os.MkdirAll(filepath.Dir(snapshot), 0700); err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(filepath.Dir(snapshot), filepath.Join(dir, "old-weight"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(relative, snapshot); err != nil {
		t.Fatal(err)
	}
	c.CacheRoot = func() (string, error) { return cache, nil }
	return c, root, cache, started
}

func retirementArgs(t *testing.T, root string, extra ...string) []string {
	t.Helper()
	_, rev, err := setup.ReadConfiguration(root)
	if err != nil {
		t.Fatal(err)
	}
	return append([]string{"--root", root, "--replace-retired", compactLayout, "--revision", rev}, extra...)
}

func TestRetirementDryRunAndPreparationFailureKeepOldSelection(t *testing.T) {
	c, root, _, started := retirementFixture(t)
	before, rev, _ := setup.ReadConfiguration(root)
	beforeBytes, _ := before.Bytes()
	c.Start = func(context.Context, string, string) (*managed.Job, *managed.Status, error) {
		t.Fatal("dry run or failed preparation activated a layout")
		return nil, nil, nil
	}
	if code, _, diagnostic := configureRun(c, retirementArgs(t, root, "--dry-run", "--json")...); code != 0 {
		t.Fatal(diagnostic)
	}
	if state, err := setup.ReadRetirement(root); err != nil || state != nil {
		t.Fatal("dry run wrote retirement intent", state, err)
	}
	c.Effects.Fetch = func(context.Context, string, string, manifest.Document, lockfile.Document) error {
		return errors.New("download interrupted")
	}
	if code, _, diagnostic := configureRun(c, retirementArgs(t, root, "--prepare")...); code != 1 || !strings.Contains(diagnostic, "old selection retained") {
		t.Fatal(code, diagnostic)
	}
	after, afterRev, _ := setup.ReadConfiguration(root)
	afterBytes, _ := after.Bytes()
	if rev != afterRev || string(beforeBytes) != string(afterBytes) || len(*started) != 0 {
		t.Fatal("failed preparation changed installed selection")
	}
	if state, _ := setup.ReadRetirement(root); state != nil {
		t.Fatal("failure before preparation left an intent")
	}
}

func TestRetirementDryRunNamesSharedFilesBeforeDeletionConsent(t *testing.T) {
	c, root, _, _ := retirementFixture(t)
	c.RemoveHF = func(context.Context, string, hfcache.Entry, io.Writer) error {
		t.Fatal("dry run deleted a shared file")
		return nil
	}
	artifact := guidedCatalog(t).Artifacts[guidedCatalog(t).Presets[compactLayout].Artifact]
	name := artifact.HuggingFace().Repo + "/" + artifact.Files[0].Path
	code, output, diagnostic := configureRun(c, retirementArgs(t, root, "--dry-run")...)
	if code != 0 || !strings.Contains(output, name+" · ") || !strings.Contains(output, "add --delete-retired-weights") {
		t.Fatal(code, output, diagnostic)
	}
	code, output, diagnostic = configureRun(c, retirementArgs(t, root, "--dry-run", "--json", "--delete-retired-weights")...)
	var result struct {
		Files []struct {
			Repo, Name string
			Bytes      int64
		} `json:"unused_hf_files"`
	}
	if code != 0 || json.Unmarshal([]byte(output), &result) != nil || len(result.Files) != 1 || result.Files[0].Repo+"/"+result.Files[0].Name != name || result.Files[0].Bytes == 0 {
		t.Fatal(code, output, diagnostic)
	}
	if state, err := setup.ReadRetirement(root); err != nil || state != nil {
		t.Fatal("dry run wrote retirement intent", state, err)
	}
}

func TestRetirementReplacementKeepsSharedWeightsUnlessDeletionIsChosen(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(map[bool]string{false: "keep shared weights", true: "delete shared weights"}[remove], func(t *testing.T) {
			c, root, cache, started := retirementFixture(t)
			calls := 0
			c.RemoveHF = func(_ context.Context, actual string, file hfcache.Entry, _ io.Writer) error {
				calls++
				if !remove || actual != cache || file.Repo != "unsloth/Qwen3.5-4B-GGUF" || file.Name == "" {
					t.Fatal(actual, file)
				}
				return removeFixtureHF(cache, file)
			}
			args := retirementArgs(t, root, "--prepare", "--json")
			if remove {
				args = append(args, "--delete-retired-weights")
			}
			if code, _, diagnostic := configureRun(c, args...); code != 0 {
				t.Fatal(diagnostic)
			}
			after, _, err := setup.ReadConfiguration(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(after.Presets) != 1 || after.Layouts["work"].Default != extraction || after.Layouts["work"].Startup[0] != extraction || len(*started) != 1 {
				t.Fatal(after, *started)
			}
			if _, ok := after.Presets[compactLayout]; ok {
				t.Fatal("old saved selection retained")
			}
			if state, err := setup.ReadRetirement(root); err != nil || state != nil {
				t.Fatal(state, err)
			}
			if remove && calls != 1 || !remove && calls != 0 {
				t.Fatal("wrong deletion consent", calls)
			}
			_, err = os.Stat(filepath.Join(cache, "models--unsloth--Qwen3.5-4B-GGUF", "blobs", "old-weight"))
			if remove && !os.IsNotExist(err) || !remove && err != nil {
				t.Fatal("wrong shared weight outcome", err)
			}
		})
	}
}

func TestRetirementActivationFailureRetainsIntentAndResumes(t *testing.T) {
	c, root, cache, started := retirementFixture(t)
	start := c.Start
	c.Start = func(context.Context, string, string) (*managed.Job, *managed.Status, error) {
		return nil, nil, errors.New("service busy")
	}
	if code, _, diagnostic := configureRun(c, retirementArgs(t, root, "--prepare")...); code != 1 || !strings.Contains(diagnostic, "--resume --prepare") {
		t.Fatal(code, diagnostic)
	}
	state, err := setup.ReadRetirement(root)
	if err != nil || state == nil || state.Activated {
		t.Fatal(state, err)
	}
	if _, err = os.Stat(filepath.Join(cache, "models--unsloth--Qwen3.5-4B-GGUF", "blobs", "old-weight")); err != nil {
		t.Fatal("activation failure removed weights", err)
	}
	if code, _, _ := configureRun(c, "--root", root); code == 0 {
		t.Fatal("pending retirement allowed a new wizard session")
	}
	c.Start = start
	if code, _, diagnostic := configureRun(c, "--root", root, "--resume", "--prepare", "--json"); code != 0 {
		t.Fatal(diagnostic)
	}
	if len(*started) != 1 {
		t.Fatal(*started)
	}
	if state, err = setup.ReadRetirement(root); err != nil || state != nil {
		t.Fatal(state, err)
	}
}

func TestRetirementResumePreparesTheSwitchedSelectionBeforeActivation(t *testing.T) {
	c, root, _, started := retirementFixture(t)
	prepared := false
	fetch := c.Effects.Fetch
	c.Effects.Fetch = func(ctx context.Context, r, id string, m manifest.Document, l lockfile.Document) error {
		prepared = true
		return fetch(ctx, r, id, m, l)
	}
	start := c.Start
	c.Start = func(context.Context, string, string) (*managed.Job, *managed.Status, error) {
		return nil, nil, errors.New("service busy")
	}
	if code, _, diagnostic := configureRun(c, retirementArgs(t, root, "--prepare")...); code != 1 || !strings.Contains(diagnostic, "activation") {
		t.Fatal(code, diagnostic)
	}
	// Prepared material may disappear after the switch, for example through a
	// concurrent temper cleanup. A retry must restore it before activation.
	prepared = false
	c.Start = func(ctx context.Context, root, layout string) (*managed.Job, *managed.Status, error) {
		if !prepared {
			t.Fatal("resume activated the switched selection without preparing it")
		}
		return start(ctx, root, layout)
	}
	if code, _, diagnostic := configureRun(c, "--root", root, "--resume", "--prepare"); code != 0 {
		t.Fatal(diagnostic)
	}
	if state, err := setup.ReadRetirement(root); err != nil || state != nil || len(*started) != 1 {
		t.Fatal(state, err, *started)
	}
}

func TestRetirementCleanupRetryDoesNotReinstallOrReactivate(t *testing.T) {
	c, root, cache, started := retirementFixture(t)
	calls := 0
	c.RemoveHF = func(context.Context, string, hfcache.Entry, io.Writer) error {
		calls++
		return errors.New("cache is busy")
	}
	if code, _, diagnostic := configureRun(c, retirementArgs(t, root, "--prepare", "--delete-retired-weights")...); code != 1 || !strings.Contains(diagnostic, "shared weight cleanup") {
		t.Fatal(code, diagnostic)
	}
	state, err := setup.ReadRetirement(root)
	if err != nil || state == nil || !state.Activated || len(*started) != 1 {
		t.Fatal(state, *started, err)
	}
	before, _ := os.ReadFile(filepath.Join(root, setup.RetirementFile))
	configBefore, _ := os.ReadFile(filepath.Join(root, setup.ConfigurationFile))
	c.Start = func(context.Context, string, string) (*managed.Job, *managed.Status, error) {
		t.Fatal("cleanup retry reactivated")
		return nil, nil, nil
	}
	c.Effects.Fetch = func(context.Context, string, string, manifest.Document, lockfile.Document) error {
		t.Fatal("cleanup retry downloaded")
		return nil
	}
	if code, _, diagnostic := configureRun(c, "--root", root, "--resume", "--prepare", "--dry-run", "--json"); code != 0 {
		t.Fatal(diagnostic)
	}
	after, _ := os.ReadFile(filepath.Join(root, setup.RetirementFile))
	configAfter, _ := os.ReadFile(filepath.Join(root, setup.ConfigurationFile))
	if string(before) != string(after) || string(configBefore) != string(configAfter) || calls != 1 {
		t.Fatal("dry resume mutated state or cleaned files")
	}
	c.RemoveHF = func(_ context.Context, _ string, file hfcache.Entry, _ io.Writer) error {
		calls++
		return removeFixtureHF(cache, file)
	}
	if code, _, diagnostic := configureRun(c, "--root", root, "--resume", "--prepare"); code != 0 {
		t.Fatal(diagnostic)
	}
	if calls != 2 || len(*started) != 1 {
		t.Fatal("cleanup replayed successful stages", calls, *started)
	}
}

func TestUnreadableRetirementIntentBlocksEditsUntilExplicitlyDiscarded(t *testing.T) {
	c, root, _, _ := retirementFixture(t)
	intent := filepath.Join(root, setup.RetirementFile)
	// A newer or damaged intent may still protect an unfinished switch.
	if err := os.WriteFile(intent, []byte(`{"schema":"temper-retirement/v1","future_field":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	current, rev, _ := setup.ReadConfiguration(root)
	if code, _, diagnostic := configureRun(c, "--root", root, "--preset", extraction, "--revision", rev); code == 0 || !strings.Contains(diagnostic, "--discard-retirement") {
		t.Fatal(code, diagnostic)
	}
	current.Layouts["work"] = setup.Layout{Name: "Edited", IdleSeconds: 60}
	if _, _, err := setup.SaveConfiguration(context.Background(), root, current, rev, false); err == nil || !strings.Contains(err.Error(), "--discard-retirement") {
		t.Fatal("unreadable intent allowed an edit or hid its way out", err)
	}
	if code, _, diagnostic := configureRun(c, "--root", root, "--discard-retirement", "--dry-run"); code != 0 {
		t.Fatal(diagnostic)
	}
	if _, err := os.Stat(intent); err != nil {
		t.Fatal("dry run discarded the intent", err)
	}
	if code, _, diagnostic := configureRun(c, "--root", root, "--discard-retirement"); code != 0 {
		t.Fatal(diagnostic)
	}
	if _, err := os.Stat(intent); !os.IsNotExist(err) {
		t.Fatal("intent retained", err)
	}
	if _, _, err := setup.SaveConfiguration(context.Background(), root, current, rev, false); err != nil {
		t.Fatal("discarded intent still blocks edits", err)
	}
}

func TestRetirementRemovalClearsMembershipWithoutRequiringSuccessor(t *testing.T) {
	c, root, _, started := retirementFixture(t)
	_, rev, _ := setup.ReadConfiguration(root)
	if code, _, diagnostic := configureRun(c, "--root", root, "--remove-retired", compactLayout, "--revision", rev, "--prepare", "--json"); code != 0 {
		t.Fatal(diagnostic)
	}
	after, _, _ := setup.ReadConfiguration(root)
	l := after.Layouts["work"]
	if len(after.Presets)+len(l.Presets)+len(l.Startup) != 0 || l.Default != "" || len(*started) != 0 {
		t.Fatal(after, *started)
	}
}

func TestRetirementRejectsMissingConsentAndNonRetiredSelections(t *testing.T) {
	c, root, _, _ := retirementFixture(t)
	for _, args := range [][]string{retirementArgs(t, root), {"--root", root, "--delete-retired-weights"}, retirementArgs(t, root, "--preset", extraction, "--prepare")} {
		if code, _, _ := configureRun(c, args...); code == 0 {
			t.Fatal("invalid retirement accepted", args)
		}
	}
	c.Catalog = func(context.Context, string, string, bool) (catalog.View, error) {
		return catalog.View{Document: guidedCatalog(t)}, nil
	}
	if code, _, diagnostic := configureRun(c, retirementArgs(t, root, "--prepare")...); code == 0 || !strings.Contains(diagnostic, "not explicitly retired") {
		t.Fatal(code, diagnostic)
	}
}

func TestRetirementPreparationCannotOverwriteAConcurrentSelection(t *testing.T) {
	c, root, _, started := retirementFixture(t)
	fetch := c.Effects.Fetch
	changed := false
	c.Effects.Fetch = func(ctx context.Context, r, id string, m manifest.Document, l lockfile.Document) error {
		if !changed {
			changed = true
			current, rev, err := setup.ReadConfiguration(root)
			if err != nil {
				t.Fatal(err)
			}
			p := current.Presets[compactLayout]
			p.Name = "Concurrent edit"
			current.Presets[compactLayout] = p
			if _, _, err = setup.SaveConfiguration(ctx, root, current, rev, false); err != nil {
				t.Fatal(err)
			}
		}
		return fetch(ctx, r, id, m, l)
	}
	if code, _, diagnostic := configureRun(c, retirementArgs(t, root, "--prepare")...); code != 1 || !strings.Contains(diagnostic, "selection changed during preparation") {
		t.Fatal(code, diagnostic)
	}
	current, _, _ := setup.ReadConfiguration(root)
	if current.Presets[compactLayout].Name != "Concurrent edit" || len(*started) != 0 {
		t.Fatal("overwrote a concurrent choice")
	}
	if pending, _ := setup.ReadRetirement(root); pending != nil {
		t.Fatal("conflicted preparation left an intent")
	}
}

func TestRetirementSharedWeightConsentPinsTheReviewedCacheAcrossResume(t *testing.T) {
	c, root, cache, _ := retirementFixture(t)
	c.RemoveHF = func(context.Context, string, hfcache.Entry, io.Writer) error { return errors.New("retry cleanup") }
	if code, _, _ := configureRun(c, retirementArgs(t, root, "--prepare", "--delete-retired-weights")...); code != 1 {
		t.Fatal(code)
	}
	c.CacheRoot = func() (string, error) { t.Fatal("cleanup resume consulted a different cache setting"); return "", nil }
	c.RemoveHF = func(_ context.Context, actual string, file hfcache.Entry, _ io.Writer) error {
		if actual != cache {
			t.Fatal("changed the reviewed deletion target", actual, cache)
		}
		return removeFixtureHF(cache, file)
	}
	if code, _, diagnostic := configureRun(c, "--root", root, "--resume", "--prepare"); code != 0 {
		t.Fatal(diagnostic)
	}
}

func TestRetirementForkIdentityAndAuthoredFileSurviveReplacement(t *testing.T) {
	c, root, _, _ := retirementFixture(t)
	doc := guidedCatalog(t)
	file := catalog.UserPreset{Schema: catalog.UserPresetSchema, Origin: compactLayout, Name: "My helper", Changes: map[string]any{"context_window_tokens": 8192}}
	writeLayoutPreset(t, root, "my-helper", file)
	loaded, err := userpreset.Load(root, "my-helper", catalog.View{Document: doc})
	if err != nil {
		t.Fatal(err)
	}
	lock, err := setup.BuildPreset(loaded.Document, "my-helper", 8, setup.Choices{})
	if err != nil {
		t.Fatal(err)
	}
	current, rev, _ := setup.ReadConfiguration(root)
	delete(current.Presets, compactLayout)
	current.Presets["my-helper"] = setup.Preset{Name: "Mine", UserPreset: "my-helper", Lock: lock, ForkBaseline: setup.CaptureForkBaseline(file, doc, 8)}
	l := current.Layouts["work"]
	l.Presets = []string{"my-helper"}
	l.Startup = []string{"my-helper"}
	l.Default = "my-helper"
	current.Layouts["work"] = l
	if _, _, err = setup.SaveConfiguration(context.Background(), root, current, rev, false); err != nil {
		t.Fatal(err)
	}
	before, err := userpreset.Read(root, "my-helper")
	if err != nil {
		t.Fatal(err)
	}
	_, rev, _ = setup.ReadConfiguration(root)
	if code, _, diagnostic := configureRun(c, "--root", root, "--replace-retired", "my-helper", "--revision", rev, "--prepare"); code != 0 {
		t.Fatal(diagnostic)
	}
	after, _ := userpreset.Read(root, "my-helper")
	if string(before) != string(after) {
		t.Fatal("migration rewrote or deleted the authored fork")
	}
	current, _, _ = setup.ReadConfiguration(root)
	if current.Presets[extraction].UserPreset != "" || current.Layouts["work"].Default != extraction {
		t.Fatal("transferred incompatible overrides or lost membership")
	}
	copyFile := catalog.CopyOf(lock, "Independent copy")
	writeLayoutPreset(t, root, "independent", copyFile)
	saved := setup.Preset{UserPreset: "independent", Lock: lock}
	if setup.RetirementOrigin(saved) != "" {
		t.Fatal("independent copy inherited retirement by weight similarity")
	}
}

func TestRetirementNotReadyActivationNeverAuthorizesCleanup(t *testing.T) {
	c, root, _, _ := retirementFixture(t)
	c.Start = func(context.Context, string, string) (*managed.Job, *managed.Status, error) {
		return nil, &managed.Status{Observed: managed.Observation{Exists: true, Ready: false}}, nil
	}
	c.RemoveHF = func(context.Context, string, hfcache.Entry, io.Writer) error {
		t.Fatal("cleaned before readiness")
		return nil
	}
	if code, _, diagnostic := configureRun(c, retirementArgs(t, root, "--prepare", "--delete-retired-weights")...); code != 1 || !strings.Contains(diagnostic, "not reached") {
		t.Fatal(code, diagnostic)
	}
	state, _ := setup.ReadRetirement(root)
	if state == nil || state.Activated {
		t.Fatal(state)
	}
}

func removeFixtureHF(cache string, entry hfcache.Entry) error {
	file, present, err := (hfcache.Cache{Root: cache}).Inspect(entry)
	if err != nil || !present {
		return err
	}
	snapshots, err := (hfcache.Cache{Root: cache}).NamedFiles(entry)
	if err != nil {
		return err
	}
	for _, snapshot := range snapshots {
		if err := os.Remove(snapshot.Path); err != nil {
			return err
		}
	}
	return os.Remove(file.Path)
}

func TestRetirementResumesAnIntentCreatedBeforeTheConfigurationSwitch(t *testing.T) {
	c, root, cache, started := retirementFixture(t)
	current, rev, err := setup.ReadConfiguration(root)
	if err != nil {
		t.Fatal(err)
	}
	d := guidedCatalog(t)
	lock, err := setup.BuildPreset(d, extraction, 8, setup.Choices{})
	if err != nil {
		t.Fatal(err)
	}
	next, err := setup.ChangeRetired(current, compactLayout, extraction, setup.Preset{Name: "Successor", Lock: lock})
	if err != nil {
		t.Fatal(err)
	}
	after, _, err := setup.SaveConfiguration(context.Background(), root, next, rev, true)
	if err != nil {
		t.Fatal(err)
	}
	state := &setup.RetirementState{Schema: setup.RetirementSchema, BeforeRevision: rev, AfterRevision: after, Configuration: next, Retired: map[string]setup.Preset{compactLayout: current.Presets[compactLayout]}, Changes: []setup.RetirementChange{{Preset: compactLayout, Successor: extraction}}, HFCache: cache}
	if err = setup.SaveRetirement(context.Background(), root, state); err != nil {
		t.Fatal(err)
	}
	c.Catalog = func(context.Context, string, string, bool) (catalog.View, error) {
		t.Fatal("recovery consulted moving authoring state")
		return catalog.View{}, nil
	}
	if code, _, diagnostic := configureRun(c, "--root", root, "--resume", "--prepare"); code != 0 {
		t.Fatal(diagnostic)
	}
	if len(*started) != 1 {
		t.Fatal(*started)
	}
	current, _, _ = setup.ReadConfiguration(root)
	if current.Layouts["work"].Default != extraction {
		t.Fatal("did not recover the reviewed switch")
	}
	if pending, err := setup.ReadRetirement(root); err != nil || pending != nil {
		t.Fatal(pending, err)
	}
}

func TestRetirementNoticeDoesNotAutomaticallyReplaceASavedPreset(t *testing.T) {
	c, root, _, started := retirementFixture(t)
	// Cleanup fixtures use tiny placeholder weights. Update independently
	// verifies cache sizes, so use an empty cache and its stubbed preparation.
	cache := t.TempDir()
	c.CacheRoot = func() (string, error) { return cache, nil }
	before, _, _ := setup.ReadConfiguration(root)
	view, err := c.Catalog(context.Background(), root, "", false)
	if err != nil {
		t.Fatal(err)
	}
	c.Newest = func(context.Context, string, bool) (catalog.View, error) { return view, nil }
	code, output, diagnostic := updateRun(c, "--root", root, "--json")
	if code != 0 {
		t.Fatal(diagnostic)
	}
	_, records := decodeUpdate(t, output)
	if records[compactLayout].Status != "kept" || !strings.Contains(records[compactLayout].Reason, "retired on") || !strings.Contains(records[compactLayout].Reason, extraction) {
		t.Fatal(records)
	}
	after, _, _ := setup.ReadConfiguration(root)
	if len(*started) != 0 || len(after.Presets) != 1 || after.Presets[compactLayout].Lock.Records.Presets[compactLayout].Artifact != before.Presets[compactLayout].Lock.Records.Presets[compactLayout].Artifact {
		t.Fatal("a notice changed selection or started a layout")
	}
	if _, ok := after.Presets[extraction]; ok {
		t.Fatal("successor was silently selected")
	}
}

func TestRetirementChecksHFDeletionBeforeChangingSelection(t *testing.T) {
	c, root, cache, started := retirementFixture(t)
	before, rev, err := setup.ReadConfiguration(root)
	if err != nil {
		t.Fatal(err)
	}
	c.CheckHFRemoval = func(context.Context) error { return errors.New("upgrade hf for named-file deletion") }
	c.RemoveHF = func(ctx context.Context, actual string, file hfcache.Entry, out io.Writer) error {
		return removeFixtureHF(actual, file)
	}
	code, _, diagnostic := configureRun(c, retirementArgs(t, root, "--prepare", "--delete-retired-weights")...)
	if code == 0 || !strings.Contains(diagnostic, "upgrade hf") {
		t.Fatal(code, diagnostic)
	}
	after, afterRev, err := setup.ReadConfiguration(root)
	if err != nil || afterRev != rev || len(after.Presets) != len(before.Presets) || len(*started) != 0 {
		t.Fatal("unsupported client changed selection", err)
	}
	if state, err := setup.ReadRetirement(root); err != nil || state != nil {
		t.Fatal("unsupported client created intent", state, err)
	}
	artifact := guidedCatalog(t).Artifacts[guidedCatalog(t).Presets[compactLayout].Artifact]
	entry := hfcache.Entry{Repo: artifact.HuggingFace().Repo, Revision: artifact.HuggingFace().Revision, Name: artifact.Files[0].Path, SHA256: artifact.Files[0].SHA256}
	if files, err := (hfcache.Cache{Root: cache}).NamedFiles(entry); err != nil || len(files) != 1 {
		t.Fatal("unsupported client lost cached files", files, err)
	}
	c.CheckHFRemoval = func(context.Context) error { return nil }
	if code, _, diagnostic := configureRun(c, retirementArgs(t, root, "--prepare", "--delete-retired-weights")...); code != 0 {
		t.Fatal(diagnostic)
	}
	if len(*started) != 1 {
		t.Fatal("retry did not complete", *started)
	}
}

func TestRetirementVerifiesDeletionAcrossRevisionsAndResumes(t *testing.T) {
	c, root, cache, started := retirementFixture(t)
	artifact := guidedCatalog(t).Artifacts[guidedCatalog(t).Presets[compactLayout].Artifact]
	entry := hfcache.Entry{Repo: artifact.HuggingFace().Repo, Revision: artifact.HuggingFace().Revision, Name: artifact.Files[0].Path, SHA256: artifact.Files[0].SHA256}
	other := filepath.Join(cache, "models--"+strings.ReplaceAll(entry.Repo, "/", "--"), "snapshots", strings.Repeat("e", 40), entry.Name)
	if err := os.MkdirAll(filepath.Dir(other), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("another revision"), 0600); err != nil {
		t.Fatal(err)
	}
	c.RemoveHF = func(context.Context, string, hfcache.Entry, io.Writer) error {
		// A client returning zero after deleting only the reviewed revision
		// has not fulfilled cross-revision consent.
		path := filepath.Join(cache, "models--"+strings.ReplaceAll(entry.Repo, "/", "--"), "snapshots", entry.Revision, entry.Name)
		return os.Remove(path)
	}
	code, output, diagnostic := configureRun(c, retirementArgs(t, root, "--prepare", "--delete-retired-weights", "--json")...)
	if code == 0 || strings.Contains(output, "deleted_hf_files") || !strings.Contains(diagnostic, "remains in 1 cached revisions") {
		t.Fatal(code, output, diagnostic)
	}
	state, err := setup.ReadRetirement(root)
	if err != nil || state == nil || !state.Activated {
		t.Fatal("lost recovery state", state, err)
	}
	c.CheckHFRemoval = func(context.Context) error { return errors.New("upgrade hf") }
	if code, _, diagnostic := configureRun(c, "--root", root, "--resume", "--prepare"); code == 0 || !strings.Contains(diagnostic, "upgrade hf") {
		t.Fatal(code, diagnostic)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("failed prerequisite changed cache", err)
	}
	c.CheckHFRemoval = func(context.Context) error { return nil }
	c.RemoveHF = func(context.Context, string, hfcache.Entry, io.Writer) error { return os.Remove(other) }
	if code, _, diagnostic := configureRun(c, "--root", root, "--resume", "--prepare"); code != 0 {
		t.Fatal(diagnostic)
	}
	if state, err := setup.ReadRetirement(root); state != nil || err != nil || len(*started) != 1 {
		t.Fatal("retry did not finish without reactivation", state, err, *started)
	}
	if files, err := (hfcache.Cache{Root: cache}).NamedFiles(entry); err != nil || len(files) != 0 {
		t.Fatal(files, err)
	}
}

func TestRetirementRejectsHFNoOpSuccessThroughTheDefaultAdapter(t *testing.T) {
	c, root, cache, started := retirementFixture(t)
	dir := t.TempDir()
	script := "#!/bin/sh\nif [ \"$*\" = 'cache rm --help' ]; then printf '%s\\n' 'hf:// URIs (repo or file)'; fi\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "hf"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	c.CheckHFRemoval, c.RemoveHF = nil, nil
	code, output, diagnostic := configureRun(c, retirementArgs(t, root, "--prepare", "--delete-retired-weights", "--json")...)
	if code == 0 || strings.Contains(output, "deleted_hf_files") || !strings.Contains(diagnostic, "hf reported success but") {
		t.Fatal(code, output, diagnostic)
	}
	state, err := setup.ReadRetirement(root)
	if err != nil || state == nil || !state.Activated {
		t.Fatal(state, err)
	}
	c.RemoveHF = func(_ context.Context, actual string, file hfcache.Entry, _ io.Writer) error {
		if actual != cache {
			t.Fatal("retry changed cache")
		}
		return removeFixtureHF(actual, file)
	}
	if code, _, diagnostic := configureRun(c, "--root", root, "--resume", "--prepare"); code != 0 {
		t.Fatal(diagnostic)
	}
	if state, err := setup.ReadRetirement(root); state != nil || err != nil || len(*started) != 1 {
		t.Fatal(state, err, *started)
	}
}

func TestRetirementDryRunAndKeptWeightsDoNotRequireHFDeletion(t *testing.T) {
	for _, flags := range [][]string{{"--dry-run", "--delete-retired-weights"}, {"--prepare"}} {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			c, root, _, _ := retirementFixture(t)
			c.CheckHFRemoval = func(context.Context) error { t.Fatal("checked deletion without applying deletion consent"); return nil }
			if code, _, diagnostic := configureRun(c, retirementArgs(t, root, flags...)...); code != 0 {
				t.Fatal(diagnostic)
			}
		})
	}
}

func TestRetirementNeverBorrowsAnUnappliedAuthoringOrigin(t *testing.T) {
	for _, origin := range []string{compactLayout, extraction, ""} {
		t.Run("authored-origin="+origin, func(t *testing.T) {
			c, root, _, _ := retirementFixture(t)
			if origin != "" {
				writeLayoutPreset(t, root, "untracked", catalog.UserPreset{Schema: catalog.UserPresetSchema, Origin: origin, Name: "Unapplied fork"})
			}
			view, err := c.Catalog(context.Background(), root, "", false)
			if err != nil {
				t.Fatal(err)
			}
			saved := setup.Preset{UserPreset: "untracked"}
			if got := retirementOption(saved, setupui.PresetOption{ID: "untracked"}, view); got.Retirement != nil {
				t.Fatal("unapplied authoring file retired a saved selection", got.Retirement)
			}
		})
	}
}

func TestRetirementUsesTheInstalledForkOriginBeforeMovingAuthoringState(t *testing.T) {
	_, root, _, _ := retirementFixture(t)
	file := catalog.UserPreset{Schema: catalog.UserPresetSchema, Origin: extraction, Name: "Changed authoring origin"}
	writeLayoutPreset(t, root, "changed-fork", file)
	saved := setup.Preset{UserPreset: "changed-fork", ForkBaseline: &setup.ForkBaseline{Origin: compactLayout, Values: map[string]json.RawMessage{}}}
	if origin := setup.RetirementOrigin(saved); origin != compactLayout {
		t.Fatal("moving authoring state hid the installed retirement", origin)
	}
}
