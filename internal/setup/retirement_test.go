package setup_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/setup"
)

func TestRetirementReplacementPreservesEveryReferenceAndExistingSuccessor(t *testing.T) {
	current := composition(t)
	original, _ := current.Bytes()
	old := current.Presets[compactLayout]
	existing := old
	existing.Name = "My chosen successor"
	existing.Choices.ContextWindow = 8192
	current.Presets["successor"] = existing
	l := current.Layouts["review"]
	l.Presets = append(l.Presets, "successor")
	l.Startup = append(l.Startup, "successor")
	current.Layouts["review"] = l
	next, err := setup.ChangeRetired(current, compactLayout, "successor", setup.Preset{})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Presets) != 1 || next.Presets["successor"].Name != existing.Name || next.Presets["successor"].Choices != existing.Choices {
		t.Fatal("overwrote the existing successor")
	}
	for id, l := range next.Layouts {
		if !reflect.DeepEqual(l.Presets, []string{"successor"}) {
			t.Fatal(id, l)
		}
	}
	if l := next.Layouts["review"]; l.Default != "successor" || !reflect.DeepEqual(l.Startup, []string{"successor"}) || l.IdleSeconds != 60 {
		t.Fatal(l)
	}
	if current.Layouts["review"].Default != compactLayout || len(original) == 0 {
		t.Fatal("mutated the caller")
	}
	removed, err := setup.ChangeRetired(next, "successor", "", setup.Preset{})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range removed.Layouts {
		if len(l.Presets)+len(l.Startup) != 0 || l.Default != "" {
			t.Fatal(l)
		}
	}
}

func TestRetirementIntentGuardsConfigurationAndCheckpointWrites(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "root")
	current := composition(t)
	before, _, err := setup.SaveConfiguration(ctx, root, current, "", false)
	if err != nil {
		t.Fatal(err)
	}
	next, err := setup.ChangeRetired(current, compactLayout, "successor", current.Presets[compactLayout])
	if err != nil {
		t.Fatal(err)
	}
	after, _, err := setup.SaveConfiguration(ctx, root, next, before, true)
	if err != nil {
		t.Fatal(err)
	}
	state := &setup.RetirementState{Schema: setup.RetirementSchema, BeforeRevision: before, AfterRevision: after, Configuration: next, Retired: current.Presets, Changes: []setup.RetirementChange{{Preset: compactLayout, Successor: "successor"}}}
	if err = setup.SaveRetirement(ctx, root, state); err != nil {
		t.Fatal(err)
	}
	stale := *state
	unrelated := current
	l := unrelated.Layouts["writing"]
	l.Name = "Unreviewed"
	unrelated.Layouts["writing"] = l
	if _, _, err = setup.SaveConfiguration(ctx, root, unrelated, before, false); err == nil {
		t.Fatal("pending retirement allowed an unrelated write")
	}
	if _, _, err = setup.SaveConfiguration(ctx, root, next, before, false); err != nil {
		t.Fatal(err)
	}
	state.Activated = true
	if err = setup.SaveRetirement(ctx, root, state); err != nil {
		t.Fatal(err)
	}
	if err = setup.SaveRetirement(ctx, root, &stale); err == nil {
		t.Fatal("stale checkpoint overwrote activation")
	}
	loaded, err := setup.ReadRetirement(root)
	if err != nil || !loaded.Activated || loaded.Revision != state.Revision {
		t.Fatal(loaded, err)
	}
	unlock, err := setup.LockRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	err = setup.ClearRetirement(root, state)
	unlock()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err = setup.ReadRetirement(root)
	if err != nil || loaded != nil {
		t.Fatal(loaded, err)
	}
}

func TestRetirementIntentReadRefusesSymlinkAndTamperedSelection(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	raw := []byte(`{"schema":"temper-retirement/v1"}`)
	if err := os.WriteFile(outside, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, setup.RetirementFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.ReadRetirement(root); err == nil {
		t.Fatal("followed a retirement symlink")
	}
	got, _ := os.ReadFile(outside)
	if !bytes.Equal(raw, got) {
		t.Fatal("modified an outside file")
	}
}

func TestEngineReplacementNamesEachVersionsOwningEngine(t *testing.T) {
	d := catalogDocument(t)
	old := selectedLock(t, d, "qwen3.8-27b-q4xl-mtp")
	next := selectedLock(t, d, "qwen3.8-27b-q4xl-splash")
	a, b := old.Records.Presets[old.Preset], next.Records.Presets[next.Preset]
	oldVersion := old.Records.Engines[a.Engine].Supply.Release.Version
	newVersion := next.Records.Engines[b.Engine].Supply.Release.Version
	found := false
	for _, line := range setup.Changes(old, next) {
		if strings.Contains(line, "engine "+a.Engine+" "+oldVersion+" → "+b.Engine+" "+newVersion) {
			found = true
		}
		if strings.Contains(line, "engine "+b.Engine+" "+oldVersion+" →") {
			t.Fatal("misattributed the old version to the successor engine", line)
		}
	}
	if !found {
		t.Fatal("engine transition omitted owning engine/version", setup.Changes(old, next))
	}
}
