package presetcmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/presetcmd"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/userpreset"
)

func editor(t *testing.T, c presetcmd.Command, root, id string, dry bool) *presetcmd.Editor {
	t.Helper()
	m, err := c.NewEditor(context.Background(), presetcmd.EditorOptions{Root: root, ID: id, DryRun: dry})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m
}

func key(m *presetcmd.Editor, k rune) tea.Cmd {
	msg := tea.KeyPressMsg{Code: k}
	if k >= 32 && k < 127 {
		msg.Text = string(k)
	}
	_, cmd := m.Update(msg)
	return cmd
}

func input(m *presetcmd.Editor, text string) {
	m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m.Update(tea.PasteMsg{Content: text})
	key(m, tea.KeyEnter)
}

// singleSource keeps the manager's chosen starting point explicit in stories.
func singleSource(c presetcmd.Command) presetcmd.Command {
	read := c.Catalog
	c.Catalog = func(root string) (catalog.View, error) {
		v, err := read(root)
		v.Document.Presets = map[string]catalog.Preset{mtp: v.Document.Presets[mtp]}
		return v, err
	}
	return c
}

func TestEditorCreatesRevocableForkThenSavesOnlyTheAuthoredFile(t *testing.T) {
	c, root := command(t)
	c = singleSource(c)
	m := editor(t, c, root, "", false)
	key(m, 'n')
	key(m, 'f')
	input(m, "My Qwen")
	key(m, tea.KeyRight)
	key(m, tea.KeyEnter)
	input(m, "0.6")
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("draft created root: %v", err)
	}
	key(m, tea.KeyEsc)
	key(m, tea.KeyEnter) // Keep editing is the default.
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "unsaved") {
		t.Fatalf("default discarded draft:\n%s", frame)
	}
	key(m, 'v')
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "0.6") {
		t.Fatalf("review lost override:\n%s", frame)
	}
	key(m, 's')
	u := read(t, root, "my-qwen")
	sampling := u.Changes["request_defaults"].(map[string]any)["sampling"].(map[string]any)
	if !u.Fork() || u.Origin != mtp || u.Name != "My Qwen" || sampling["temperature"] != 0.6 {
		t.Fatalf("saved wrong preset: %+v", u)
	}
	if _, err := os.Stat(filepath.Join(root, setup.ConfigurationFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("save created selection: %v", err)
	}
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "Saved my-qwen") {
		t.Fatalf("save outcome missing:\n%s", frame)
	}
}

func TestDiscardAndDryRunDoNotCreateARoot(t *testing.T) {
	for _, dry := range []bool{false, true} {
		t.Run(fmt.Sprint(dry), func(t *testing.T) {
			c, root := command(t)
			c = singleSource(c)
			m := editor(t, c, root, "", dry)
			key(m, 'n')
			key(m, 'c')
			input(m, "Mine")
			if dry {
				key(m, 's')
			}
			key(m, tea.KeyEsc)
			key(m, tea.KeyDown)
			key(m, tea.KeyEnter)
			if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("abandoned authoring created root: %v", err)
			}
		})
	}
}

func TestEditorPreservesSelectionsLocksAndTemplatesThroughSaveAndDelete(t *testing.T) {
	c, root := command(t)
	if _, d, code := run(t, c, root, "fork", mtp, "mine"); code != 0 {
		t.Fatal(d)
	}
	view, _ := c.Catalog(root)
	loaded, err := userpreset.Load(root, "mine", view)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := catalog.CompilePreset(loaded.Document, "mine", 32, 0, software.Target{OS: "darwin", Arch: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	configuration := setup.EmptyConfiguration()
	configuration.Presets["mine"] = setup.Preset{Name: "Mine", Lock: lock}
	configuration.Layouts = setup.StarterLayouts([]string{"mine"})
	if _, _, err := setup.SaveConfiguration(context.Background(), root, configuration, "", false); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, setup.ConfigurationFile))
	if err != nil {
		t.Fatal(err)
	}
	template := filepath.Join(userpreset.Dir(root), "keep.jinja")
	if err := os.WriteFile(template, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := editor(t, c, root, "mine", false)
	key(m, tea.KeyEnter)
	input(m, "Renamed")
	key(m, 's')
	key(m, 'd')
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "default") {
		t.Fatalf("delete hides layout references:\n%s", frame)
	}
	key(m, tea.KeyDown)
	key(m, tea.KeyEnter)
	after, err := os.ReadFile(filepath.Join(root, setup.ConfigurationFile))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("authoring changed saved configuration/lock")
	}
	if _, err := userpreset.Read(root, "mine"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("delete did not remove preset: %v", err)
	}
	if data, err := os.ReadFile(template); err != nil || string(data) != "keep" {
		t.Fatalf("delete changed shared template: %s %v", data, err)
	}
}

func TestEditorKeepsTypedErrorsAndRefusesInvalidDraftsWithoutLosingEdits(t *testing.T) {
	c, root := command(t)
	if _, d, code := run(t, c, root, "fork", mtp, "mine"); code != 0 {
		t.Fatal(d)
	}
	before, _ := userpreset.Read(root, "mine")
	m := editor(t, c, root, "mine", false)
	focusField(t, m, "Context window tokens")
	key(m, tea.KeyEnter)
	input(m, "not-a-number")
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "whole number") {
		t.Fatalf("type error missing:\n%s", frame)
	}
	input(m, "-1")
	key(m, 's')
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "Cannot save") {
		t.Fatalf("invalid draft accepted:\n%s", frame)
	}
	after, _ := userpreset.Read(root, "mine")
	if !bytes.Equal(before, after) {
		t.Fatal("invalid draft replaced saved file")
	}
	key(m, tea.KeyEnter)
	input(m, "16384")
	key(m, 's')
	if u := read(t, root, "mine"); u.Changes["context_window_tokens"] != 16384 {
		t.Fatalf("could not repair draft: %+v", u)
	}
}

func TestResetRemovesEqualOverrideAndUnchangedSaveKeepsOriginalBytes(t *testing.T) {
	c, root := command(t)
	if _, d, code := run(t, c, root, "fork", mtp, "mine"); code != 0 {
		t.Fatal(d)
	}
	before, _ := userpreset.Read(root, "mine")
	info, _ := os.Stat(userpreset.Path(root, "mine"))
	m := editor(t, c, root, "mine", false)
	key(m, tea.KeyRight)
	key(m, tea.KeyEnter)
	input(m, "1")
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "Override") {
		t.Fatalf("equal override became inheritance:\n%s", frame)
	}
	key(m, 'r')
	key(m, 's')
	after, _ := userpreset.Read(root, "mine")
	current, _ := os.Stat(userpreset.Path(root, "mine"))
	if !bytes.Equal(before, after) || !os.SameFile(info, current) || !info.ModTime().Equal(current.ModTime()) {
		t.Fatal("reset/no-op save rewrote original file")
	}
}

func TestConcurrentEditIsRefusedAndCanBeCopiedWithoutLosingDraft(t *testing.T) {
	c, root := command(t)
	if _, d, code := run(t, c, root, "fork", mtp, "mine"); code != 0 {
		t.Fatal(d)
	}
	a := editor(t, c, root, "mine", false)
	b := editor(t, c, root, "mine", false)
	key(a, tea.KeyEnter)
	input(a, "First")
	key(a, 's')
	key(b, tea.KeyEnter)
	input(b, "Second")
	key(b, 's')
	if frame := ansi.Strip(b.View().Content); !strings.Contains(frame, "changed while Temper") {
		t.Fatalf("lost-update refusal missing:\n%s", frame)
	}
	if read(t, root, "mine").Name != "First" {
		t.Fatal("overwrote concurrent writer")
	}
	key(b, 'c')
	input(b, "Second")
	key(b, 's')
	u := read(t, root, "second")
	if u.Fork() || u.Preset.Presentation.DisplayName != "Second" {
		t.Fatalf("copy lost draft: %+v", u)
	}
}

func TestEditorEntryRequiresTerminalAndDoesNotReadOrWriteState(t *testing.T) {
	c, root := command(t)
	c.Catalog = func(string) (catalog.View, error) {
		t.Fatal("nonterminal entry read catalog")
		return catalog.View{}, nil
	}
	for _, args := range [][]string{{}, {"edit", "mine"}, {"--json"}} {
		_, diagnostics, code := run(t, c, root, args...)
		if code != 2 || diagnostics == "" {
			t.Fatalf("nonterminal: %d %s", code, diagnostics)
		}
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("nonterminal entry made root: %v", err)
	}
}

func TestEditorRendersRealStatesAtSupportedWidths(t *testing.T) {
	c, root := command(t)
	c = singleSource(c)
	for _, width := range []int{112, 80, 40} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := editor(t, c, root, "", false)
			m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
			assertEditorFrame(t, m, width, 24)
			key(m, 'n')
			key(m, 'f')
			input(m, "Mine")
			for range 5 {
				assertEditorFrame(t, m, width, 24)
				key(m, tea.KeyRight)
			}
			key(m, 'w')
			assertEditorFrame(t, m, width, 24)
			key(m, tea.KeyEsc)
			key(m, 't')
			assertEditorFrame(t, m, width, 24)
			key(m, tea.KeyEsc)
			key(m, tea.KeyEsc)
			assertEditorFrame(t, m, width, 24)
		})
	}
}

func TestWeightsPickerStagesPinnedMetadataAndCanConvertAForkToACopy(t *testing.T) {
	c, root := withHub(t)
	if _, d, code := run(t, c, root, "fork", mtp, "mine"); code != 0 {
		t.Fatal(d)
	}
	before, _ := userpreset.Read(root, "mine")
	m := editor(t, c, root, "mine", false)
	key(m, 'w')
	key(m, tea.KeyEnter)
	input(m, "other/Qwen3.8-27B-GGUF")
	key(m, tea.KeyDown) // Revision.
	key(m, tea.KeyDown) // Relationship appears only for another repository.
	key(m, tea.KeyEnter)
	key(m, tea.KeyEnter) // Different model: convert to copy.
	lookup := primaryAction(m)
	if lookup == nil {
		t.Fatalf("lookup did not start:\n%s", ansi.Strip(m.View().Content))
	}
	m.Update(lookup())
	for _, width := range []int{112, 80, 40} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		assertEditorFrame(t, m, width, 24)
	}
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, q6Commit[:20]) {
		t.Fatalf("picker hid commit:\n%s", frame)
	}
	key(m, ' ')
	metadata := key(m, tea.KeyEnter)
	if metadata == nil {
		t.Fatalf("metadata read did not start:\n%s", ansi.Strip(m.View().Content))
	}
	m.Update(metadata())
	after, _ := userpreset.Read(root, "mine")
	if !bytes.Equal(before, after) {
		t.Fatal("picker saved without Save")
	}
	key(m, 's')
	u := read(t, root, "mine")
	if u.Fork() || u.Preset.Artifact != "qwen3.8-27b-q6-k" {
		t.Fatalf("did not convert: %+v\n%s", u, ansi.Strip(m.View().Content))
	}
	record := u.Artifacts[u.Preset.Artifact]
	if record.Sources[0].Revision != q6Commit || record.Files[0].SHA256 != q6SHA || record.Files[0].Bytes != 22900000000 || record.SameModelAs != "" {
		t.Fatalf("incorrect exact metadata or invented same-model claim: %+v", record)
	}
}

func TestCancelledMetadataResultCannotReplaceTheDraft(t *testing.T) {
	c, root := withHub(t)
	if _, d, code := run(t, c, root, "fork", mtp, "mine"); code != 0 {
		t.Fatal(d)
	}
	before, _ := userpreset.Read(root, "mine")
	m := editor(t, c, root, "mine", false)
	key(m, 'w')
	lookup := primaryAction(m)
	if lookup == nil {
		t.Fatal("lookup did not start")
	}
	result := lookup() // Read finishes, but delivery races cancellation.
	key(m, tea.KeyEsc)
	m.Update(result)
	if frame := ansi.Strip(m.View().Content); strings.Contains(frame, "Select weights files") {
		t.Fatalf("stale read replaced form:\n%s", frame)
	}
	key(m, tea.KeyEsc)
	key(m, 's')
	after, _ := userpreset.Read(root, "mine")
	if !bytes.Equal(before, after) {
		t.Fatal("cancelled read changed file")
	}
}

func TestOfflinePickerKeepsExistingFieldsEditable(t *testing.T) {
	c, root := command(t)
	if _, d, code := run(t, c, root, "fork", mtp, "mine"); code != 0 {
		t.Fatal(d)
	}
	m := editor(t, c, root, "mine", false)
	key(m, 'w')
	primaryAction(m)
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "offline") {
		t.Fatalf("offline state missing:\n%s", frame)
	}
	key(m, tea.KeyEsc)
	key(m, tea.KeyEnter)
	input(m, "Offline edit")
	key(m, 's')
	if read(t, root, "mine").Name != "Offline edit" {
		t.Fatal("offline lookup prevented ordinary edit")
	}
}

func TestBuiltinAndLocalTemplateEditsRemainDraftsUntilSave(t *testing.T) {
	c, root := command(t)
	if _, d, code := run(t, c, root, "fork", mtp, "mine"); code != 0 {
		t.Fatal(d)
	}
	if err := os.WriteFile(filepath.Join(userpreset.Dir(root), "mine.jinja"), []byte(templateText), 0o600); err != nil {
		t.Fatal(err)
	}
	m := editor(t, c, root, "mine", false)
	key(m, 't')
	key(m, tea.KeyEnter) // Local.
	key(m, tea.KeyDown)  // Only the file is required.
	key(m, tea.KeyEnter)
	input(m, "mine.jinja")
	load := primaryAction(m)
	if load == nil {
		t.Fatal("local template action missing")
	}
	m.Update(load())
	if len(read(t, root, "mine").Templates) != 0 {
		t.Fatal("template saved before Save")
	}
	key(m, 's')
	u := read(t, root, "mine")
	if u.Templates["mine-template"].Files[0].SHA256 != "" {
		t.Fatal("authoring file froze local template content")
	}
	key(m, tea.KeyEnter)
	key(m, 't')
	load = primaryAction(m)
	if load == nil {
		t.Fatal("builtin action missing")
	}
	m.Update(load())
	key(m, 's')
	if read(t, root, "mine").Changes["template"] != catalog.BuiltinTemplate {
		t.Fatal("builtin was not selected")
	}
}

func TestCatalogChangeRequiresAnotherReviewBeforeSaving(t *testing.T) {
	c, root := command(t)
	if _, d, code := run(t, c, root, "fork", mtp, "mine"); code != 0 {
		t.Fatal(d)
	}
	before, _ := userpreset.Read(root, "mine")
	readCatalog := c.Catalog
	changed := false
	c.Catalog = func(root string) (catalog.View, error) {
		v, err := readCatalog(root)
		// Every read is an independent snapshot, as in the real catalog store.
		raw, _ := json.Marshal(v)
		var copy catalog.View
		if e := json.Unmarshal(raw, &copy); e != nil {
			return copy, e
		}
		if changed {
			p := copy.Document.Presets[mtp]
			p.Presentation.DisplayName = "Changed upstream"
			copy.Document.Presets[mtp] = p
		}
		return copy, err
	}
	m := editor(t, c, root, "mine", false)
	key(m, tea.KeyEnter)
	input(m, "Mine changed")
	changed = true
	key(m, 's')
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "catalog changed") {
		t.Fatalf("catalog conflict missing:\n%s", frame)
	}
	after, _ := userpreset.Read(root, "mine")
	if !bytes.Equal(before, after) {
		t.Fatal("saved against unreviewed catalog")
	}
	m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	key(m, 's')
	if read(t, root, "mine").Name != "Mine changed" {
		t.Fatal("catalog reload lost draft edits")
	}
}

func TestCopyEditsOnlyTheSelectedProfileAndKeepsOtherAuthoredValues(t *testing.T) {
	c, root := command(t)
	if _, d, code := run(t, c, root, "copy", mtp, "mine"); code != 0 {
		t.Fatal(d)
	}
	u := read(t, root, "mine")
	other := u.Preset.Profiles[0]
	other.MinMemoryGiB = 64
	other.ContextWindowTokens = 32768
	u.Preset.Profiles = append(u.Preset.Profiles, other)
	u.Preset.Presentation.Description = "Preserve my description"
	raw, err := catalog.MarshalUserPreset(u)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userpreset.Path(root, "mine"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := c.NewEditor(context.Background(), presetcmd.EditorOptions{Root: root, ID: "mine", MemoryGiB: 64})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	focusField(t, m, "Context window tokens")
	key(m, tea.KeyEnter)
	input(m, "16384")
	key(m, 's')
	after := read(t, root, "mine")
	if !reflect.DeepEqual(after.Preset.Profiles[0], u.Preset.Profiles[0]) || after.Preset.Profiles[1].ContextWindowTokens != 16384 || after.Preset.Presentation.Description != "Preserve my description" {
		t.Fatalf("copy edit damaged other values: %+v", after.Preset)
	}
}

func TestReviewUsesSharedPlannerAndMemoryRefusalDoesNotPreventAuthoring(t *testing.T) {
	c, root := command(t)
	if _, d, code := run(t, c, root, "fork", mtp, "mine"); code != 0 {
		t.Fatal(d)
	}
	facts := c.Facts
	c.Facts = func(ctx context.Context) (machine.Facts, error) {
		f, err := facts(ctx)
		f.PhysicalMemoryBytes = 8 << 30
		f.MetalDeviceMemoryMiB = 4 << 10
		f.WiredLimitMiB = 4 << 10
		return f, err
	}
	c.Plan = func(_ context.Context, gotRoot string, c setup.Configuration) (setup.Plan, error) {
		if gotRoot != root || len(c.Presets) != 1 || len(c.Layouts) != 0 {
			t.Fatal("review changed planning scope")
		}
		if c.Presets["mine"].Lock.Records.Presets["mine"].Window() != 16384 {
			t.Fatal("review did not use edited draft")
		}
		return setup.Plan{RemainingDownloadBytes: 2 << 30, RemainingDiskBytes: 3 << 30}, nil
	}
	m := editor(t, c, root, "mine", false)
	focusField(t, m, "Context window tokens")
	key(m, tea.KeyEnter)
	input(m, "16384")
	review := key(m, 'v')
	if review == nil {
		t.Fatal("review did not plan")
	}
	m.Update(review())
	// Scroll through the actual report to find memory and disk disclosures.
	m.Update(tea.WindowSizeMsg{Width: 112, Height: 100})
	frame := ansi.Strip(m.View().Content)
	for _, want := range []string{"does not fit", "KV/context cache", "Remaining downloads: 2.00 GiB", "Remaining disk allowance: 3.00 GiB"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("review missing %q:\n%s", want, frame)
		}
	}
	key(m, 's')
	if read(t, root, "mine").Changes["context_window_tokens"] != 16384 {
		t.Fatal("machine fit became an authoring gate")
	}
}

func assertEditorFrame(t *testing.T, m *presetcmd.Editor, width, height int) {
	t.Helper()
	frame := m.View().Content
	if lines := strings.Split(frame, "\n"); len(lines) > height {
		t.Fatalf("frame has %d lines at height %d:\n%s", len(lines), height, ansi.Strip(frame))
	}
	for _, line := range strings.Split(frame, "\n") {
		if ansi.StringWidth(line) > width {
			t.Fatalf("frame exceeds %d columns:\n%s", width, ansi.Strip(frame))
		}
	}
}
