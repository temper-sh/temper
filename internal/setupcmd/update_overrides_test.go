package setupcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/userpreset"
)

// placeFork follows the real first-placement route so a hand-built baseline
// cannot hide a missing connection between layout save and update.
func placeFork(t *testing.T, c Command) (string, setup.Configuration, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "root")
	writeLayoutPreset(t, root, "mine", catalog.UserPreset{Schema: catalog.UserPresetSchema, Origin: compactLayout, Name: "Mine",
		Changes: map[string]any{"request_defaults": map[string]any{"sampling": map[string]any{"temperature": 0.6}}}})
	m := newFileLayout(t, context.Background(), c, root, false)
	layoutPress(m, ' ')
	layoutComplete(t, m, layoutControl(m, 's'))
	layoutComplete(t, m, layoutControl(m, 's'))
	saved, revision, err := setup.ReadConfiguration(root)
	if err != nil || saved.Presets["mine"].ForkBaseline == nil || string(saved.Presets["mine"].ForkBaseline.Values["request_defaults.sampling.temperature"]) != "0.7" {
		t.Fatal("first placement did not retain the origin's overridden value", err)
	}
	return root, saved, revision
}

func changedTemperature(t *testing.T) catalog.Document {
	t.Helper()
	d := guidedCatalog(t)
	p := d.Presets[compactLayout]
	p.RequestDefaults.Sampling.Temperature = new(float64(0.9))
	d.Presets[compactLayout] = p
	return d
}

func requireTemperatureReport(t *testing.T, output string) updateResult {
	t.Helper()
	result, records := decodeUpdate(t, output)
	want := []setup.OverrideChange{{Path: "request_defaults.sampling.temperature", From: json.RawMessage("0.7"), To: json.RawMessage("0.9"), Override: json.RawMessage("0.6")}}
	if !reflect.DeepEqual(records["mine"].OverrideChanges, want) {
		t.Fatalf("missing or incorrect override change: %s", output)
	}
	return result
}

func TestUpdateReportsOverridesWithoutChangingExecutionAndRepeatsCleanly(t *testing.T) {
	c := layoutFolderCommand(t) // Preparation is forbidden by this fixture.
	root, before, rev := placeFork(t, c)
	raw, _ := userpreset.Read(root, "mine")
	next := changedTemperature(t)
	c.Newest = newest(next)
	// A catalog refresh before update must not replace the historical value.
	c.StoredCatalog = func(string) (catalog.View, error) { return catalog.View{Document: next}, nil }
	code, output, diagnostics := updateRun(c, "--root", root, "--dry-run")
	if code != 0 || !strings.Contains(output, "catalog request_defaults.sampling.temperature: 0.7 → 0.9; your override: 0.6") {
		t.Fatalf("text comparison: %d %s %s", code, output, diagnostics)
	}
	for _, flags := range [][]string{{"--dry-run", "--json"}, {"--software", "--json"}} {
		code, output, diagnostics := updateRun(c, append([]string{"--root", root}, flags...)...)
		if code != 0 {
			t.Fatal(diagnostics)
		}
		requireTemperatureReport(t, output)
		if _, after, _ := setup.ReadConfiguration(root); after != rev {
			t.Fatal("preview or software-only update advanced the baseline")
		}
	}
	code, output, diagnostics = updateRun(c, "--root", root, "--json")
	if code != 0 || !requireTemperatureReport(t, output).Changed {
		t.Fatal("full update did not save comparison data", code, output, diagnostics)
	}
	after, _, err := setup.ReadConfiguration(root)
	if err != nil || !sameLock(after.Presets["mine"].Lock, before.Presets["mine"].Lock) || string(after.Presets["mine"].ForkBaseline.Values["request_defaults.sampling.temperature"]) != "0.9" {
		t.Fatal("comparison-only save changed execution or lost the next baseline", err)
	}
	first, _ := os.Stat(filepath.Join(root, setup.ConfigurationFile))
	code, output, diagnostics = updateRun(c, "--root", root, "--json")
	result, records := decodeUpdate(t, output)
	second, _ := os.Stat(filepath.Join(root, setup.ConfigurationFile))
	if code != 0 || result.Changed || len(records["mine"].OverrideChanges) != 0 || !first.ModTime().Equal(second.ModTime()) {
		t.Fatal("repeat rewrote configuration or repeated the acknowledged change", output, diagnostics)
	}
	authored, _ := userpreset.Read(root, "mine")
	if !bytes.Equal(raw, authored) {
		t.Fatal("update rewrote authored overrides")
	}
}

func TestRefusedForkRebuildKeepsItsLockAndOverrideComparison(t *testing.T) {
	c := layoutFolderCommand(t)
	root, before, rev := placeFork(t, c)
	c.Newest = newest(changedTemperature(t))
	raw, _ := userpreset.Read(root, "mine")
	file, err := catalog.ParseUserPreset("mine", raw)
	if err != nil {
		t.Fatal(err)
	}
	file.Changes["template"] = "missing-template"
	invalid, _ := catalog.MarshalUserPreset(file)
	if err := userpreset.Replace(root, "mine", raw, invalid); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		code, output, diagnostics := updateRun(c, "--root", root, "--json")
		result := requireTemperatureReport(t, output)
		_, records := decodeUpdate(t, output)
		after, afterRev, err := setup.ReadConfiguration(root)
		if code != 0 || result.Changed || records["mine"].Status != "kept" || records["mine"].Reason == "" || err != nil || afterRev != rev || !sameLock(before.Presets["mine"].Lock, after.Presets["mine"].Lock) {
			t.Fatal("refused update changed the working preset or lost its report", output, diagnostics, err)
		}
	}
	if err := userpreset.Replace(root, "mine", invalid, raw); err != nil {
		t.Fatal(err)
	}
	code, output, diagnostics := updateRun(c, "--root", root, "--json")
	if code != 0 || !requireTemperatureReport(t, output).Changed {
		t.Fatal("retry lost the pending comparison", code, output, diagnostics)
	}
}

func TestInterruptedUpdateDoesNotConsumeOverrideComparison(t *testing.T) {
	c := layoutFolderCommand(t)
	root, _, rev := placeFork(t, c)
	c.Newest = newest(changedTemperature(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resolve := c.Resolve
	c.Resolve = func(ctx context.Context, d catalog.Document, id, software string) (catalog.Document, []catalog.Resolution, error) {
		doc, resolutions, err := resolve(ctx, d, id, software)
		cancel()
		return doc, resolutions, err
	}
	var out, diagnostics bytes.Buffer
	if code := c.Update(ctx, []string{"--root", root, "--json"}, &out, &diagnostics); code == 0 {
		t.Fatal("interrupted update succeeded")
	}
	if _, afterRev, _ := setup.ReadConfiguration(root); afterRev != rev {
		t.Fatal("interruption consumed the comparison")
	}
	c.Resolve = resolve
	code, output, errors := updateRun(c, "--root", root, "--json")
	if code != 0 || !requireTemperatureReport(t, output).Changed {
		t.Fatal("retry lost the pending comparison", output, errors)
	}
}

func TestConcurrentEditDoesNotConsumeForkComparison(t *testing.T) {
	c := layoutFolderCommand(t)
	root, saved, rev := placeFork(t, c)
	c.Newest = newest(changedTemperature(t))
	resolve := c.Resolve
	c.Resolve = func(ctx context.Context, d catalog.Document, id, software string) (catalog.Document, []catalog.Resolution, error) {
		layout := saved.Layouts["work"]
		layout.Name = "Concurrent rename"
		saved.Layouts["work"] = layout
		if _, _, err := setup.SaveConfiguration(ctx, root, saved, rev, false); err != nil {
			t.Fatal(err)
		}
		return resolve(ctx, d, id, software)
	}
	code, _, diagnostics := updateRun(c, "--root", root, "--json")
	after, _, err := setup.ReadConfiguration(root)
	if code == 0 || err != nil || !strings.Contains(diagnostics, "changed") || after.Layouts["work"].Name != "Concurrent rename" || string(after.Presets["mine"].ForkBaseline.Values["request_defaults.sampling.temperature"]) != "0.7" {
		t.Fatal("update lost the concurrent edit or consumed its comparison", diagnostics, err)
	}
	c.Resolve = resolve
	code, output, diagnostics := updateRun(c, "--root", root, "--json")
	if code != 0 || !requireTemperatureReport(t, output).Changed {
		t.Fatal("retry lost the pending comparison", output, diagnostics)
	}
}

func TestSoftwareUpgradeDoesNotConsumeForkComparison(t *testing.T) {
	var started []string
	c := installableCommand(t, &started, nil)
	c.StoredCatalog = layoutFolderCommand(t).StoredCatalog
	root, _, _ := placeFork(t, c)
	next := changedTemperature(t)
	next.Runtime = newerRouter(t).Runtime
	c.Newest = newest(next)
	code, output, diagnostics := updateRun(c, "--root", root, "--software", "--json")
	if code != 0 || !requireTemperatureReport(t, output).Changed {
		t.Fatal("software upgrade failed", output, diagnostics)
	}
	saved, _, _ := setup.ReadConfiguration(root)
	if string(saved.Presets["mine"].ForkBaseline.Values["request_defaults.sampling.temperature"]) != "0.7" {
		t.Fatal("software upgrade consumed the upstream change")
	}
	c.Effects = fixtureCommand(t).Effects // Following full save is comparison-only.
	code, output, diagnostics = updateRun(c, "--root", root, "--json")
	if code != 0 || !requireTemperatureReport(t, output).Changed || len(started) != 0 {
		t.Fatal("full update lost comparison or activated stopped root", output, diagnostics, started)
	}
}

func TestLegacyForkEstablishesBaselineAndRemovingOverrideResumesInheritance(t *testing.T) {
	var started []string
	c := installableCommand(t, &started, nil)
	c.StoredCatalog = layoutFolderCommand(t).StoredCatalog
	root, saved, rev := placeFork(t, c)
	p := saved.Presets["mine"]
	p.ForkBaseline = nil
	saved.Presets["mine"] = p
	if _, _, err := setup.SaveConfiguration(context.Background(), root, saved, rev, false); err != nil {
		t.Fatal(err)
	}
	c.Newest = newest(changedTemperature(t))
	code, output, diagnostics := updateRun(c, "--root", root, "--json")
	_, records := decodeUpdate(t, output)
	if code != 0 || len(records["mine"].OverrideChanges) != 0 || !reflect.DeepEqual(records["mine"].UntrackedOverrides, []string{"request_defaults.sampling.temperature"}) {
		t.Fatal("legacy fork invented its previous catalog value", output, diagnostics)
	}
	raw, _ := userpreset.Read(root, "mine")
	file, err := catalog.ParseUserPreset("mine", raw)
	if err != nil {
		t.Fatal(err)
	}
	file.Changes = nil
	cleared, _ := catalog.MarshalUserPreset(file)
	if err := userpreset.Replace(root, "mine", raw, cleared); err != nil {
		t.Fatal(err)
	}
	code, output, diagnostics = updateRun(c, "--root", root, "--json")
	_, records = decodeUpdate(t, output)
	after, _, err := setup.ReadConfiguration(root)
	if code != 0 || err != nil || len(records["mine"].OverrideChanges) != 0 || len(after.Presets["mine"].ForkBaseline.Values) != 0 || *after.Presets["mine"].Lock.Records.Presets["mine"].RequestDefaults.Sampling.Temperature != 0.9 {
		t.Fatal("removed override did not resume inheritance", output, diagnostics, err)
	}
}

func TestPreparationFailureStillReportsCommittedOverrideComparison(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "json"}[jsonOutput], func(t *testing.T) {
			var started []string
			c := installableCommand(t, &started, nil)
			c.StoredCatalog = layoutFolderCommand(t).StoredCatalog
			root, _, rev := placeFork(t, c)
			next := changedTemperature(t)
			next.Runtime = newerRouter(t).Runtime
			c.Newest = newest(next)
			c.Effects.Fetch = func(context.Context, string, string, manifest.Document, lockfile.Document) error {
				return errors.New("interrupted fetch")
			}
			args := []string{"--root", root}
			if jsonOutput {
				args = append(args, "--json")
			}
			code, output, diagnostics := updateRun(c, args...)
			if code == 0 || !strings.Contains(diagnostics, "configuration saved") || !strings.Contains(diagnostics, "interrupted fetch") || len(started) != 0 {
				t.Fatal("preparation failure lost its commit boundary", code, output, diagnostics, started)
			}
			if jsonOutput {
				requireTemperatureReport(t, output)
				var doc map[string]any
				if json.Unmarshal([]byte(output), &doc) != nil || doc["error"] == nil {
					t.Fatal("preparation failure did not produce one structured error report", output)
				}
			} else if !strings.Contains(output, "0.7 → 0.9; your override: 0.6") {
				t.Fatal("committed comparison silently consumed by preparation failure", output)
			}
			saved, afterRev, err := setup.ReadConfiguration(root)
			if err != nil || afterRev == rev || string(saved.Presets["mine"].ForkBaseline.Values["request_defaults.sampling.temperature"]) != "0.9" {
				t.Fatal("report did not describe the committed configuration", err)
			}
		})
	}
}
