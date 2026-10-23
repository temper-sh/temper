package presetcmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/userpreset"
)

func TestMoEPlacementCanBeEditedInForksAndCopies(t *testing.T) {
	for _, form := range []string{"fork", "copy"} {
		t.Run(form, func(t *testing.T) {
			c, root := command(t)
			if _, d, code := run(t, c, root, form, mtp, "mine"); code != 0 {
				t.Fatal(d)
			}
			before, _ := userpreset.Read(root, "mine")
			m := editor(t, c, root, "mine", false)
			editorClick(t, m, "Engine")
			focusField(t, m, "MoE CPU layers")
			typeText(m, "-1")
			saveEditor(m)
			after, _ := userpreset.Read(root, "mine")
			if !bytes.Equal(before, after) {
				t.Fatal("negative placement saved")
			}
			focusField(t, m, "MoE CPU layers")
			typeText(m, "4")
			saveEditor(m)
			view, _ := c.Catalog(root)
			loaded, err := userpreset.Load(root, "mine", view)
			if err != nil || loaded.Document.Presets["mine"].Config().Controls.CPUMoELayers != 4 {
				t.Fatal("placement not saved", err)
			}
		})
	}
}

func TestNamedSeedModesOverrideInheritanceAndRetainFixedZero(t *testing.T) {
	c, root := command(t)
	view, _ := c.Catalog(root)
	p := view.Document.Presets[mtp]
	seed := 17
	p.RequestDefaults.Sampling.Seed = &seed
	view.Document.Presets[mtp] = p
	c.Catalog = func(string) (catalog.View, error) { return view, nil }
	if _, d, code := run(t, c, root, "fork", mtp, "mine"); code != 0 {
		t.Fatal(d)
	}
	m := editor(t, c, root, "mine", false)
	editorClick(t, m, "Sampling")
	focusField(t, m, "Seed")
	editorClick(t, m, "Random")
	saveEditor(m)
	loaded, err := userpreset.Load(root, "mine", view)
	if err != nil || loaded.Document.Presets["mine"].RequestDefaults.Sampling.Seed != nil {
		t.Fatalf("random inherited fixed seed: %v", err)
	}
	key(m, tea.KeyEnter)
	editorClick(t, m, "Sampling")
	focusField(t, m, "Seed")
	editorClick(t, m, "Fixed")
	saveEditor(m)
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "before saving") {
		t.Fatalf("empty fixed seed saved: %s", frame)
	}
	focusField(t, m, "Seed value")
	typeText(m, "0")
	saveEditor(m)
	loaded, err = userpreset.Load(root, "mine", view)
	if err != nil {
		t.Fatal(err)
	}
	got := loaded.Document.Presets["mine"].RequestDefaults.Sampling.Seed
	if got == nil || *got != 0 {
		t.Fatal("fixed zero became random")
	}
}

func TestInvalidDependentControlCanBeTurnedOffWithoutBlockingSave(t *testing.T) {
	c, root := command(t)
	if _, d, code := run(t, c, root, "fork", mtp, "mine"); code != 0 {
		t.Fatal(d)
	}
	m := editor(t, c, root, "mine", false)
	editorClick(t, m, "Speculation")
	focusField(t, m, "Draft tokens")
	typeText(m, "bad")
	focusField(t, m, "Configuration")
	key(m, tea.KeyRight) // MTP to Off removes its dependent control.
	saveEditor(m)
	if read(t, root, "mine").Changes["speculation"].(map[string]any)["method"] != "none" {
		t.Fatal("hidden invalid input blocked Off")
	}
}

func TestLocalTemplateCancelAndDrySaveDoNotCopyTheSelectedFile(t *testing.T) {
	c, root := command(t)
	if _, d, code := run(t, c, root, "fork", mtp, "mine"); code != 0 {
		t.Fatal(d)
	}
	before, _ := userpreset.Read(root, "mine")
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "chat.jinja"), []byte(templateText), 0600); err != nil {
		t.Fatal(err)
	}
	m := editor(t, c, root, "mine", true)
	choose := func() {
		focusField(t, m, "Template")
		editorClick(t, m, "Local file")
		editorClick(t, m, "Browse…")
		editorClick(t, m, "Location…")
		input(m, directory)
		completeRead(t, m, editorClick(t, m, "chat.jinja"))
	}
	choose()
	editorClick(t, m, "Cancel")
	after, _ := userpreset.Read(root, "mine")
	if !bytes.Equal(before, after) {
		t.Fatal("source cancellation published")
	}
	choose()
	completeRead(t, m, editorClick(t, m, "Use template"))
	if cmd := editorClick(t, m, "Review"); cmd != nil {
		m.Update(cmd())
	}
	saveEditor(m)
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "Dry run: preset validates") {
		t.Fatalf("snapshot did not resolve before publication: %s", frame)
	}
	after, _ = userpreset.Read(root, "mine")
	if !bytes.Equal(before, after) {
		t.Fatal("dry save changed preset")
	}
	files, err := os.ReadDir(userpreset.Dir(root))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".jinja") {
			t.Fatal("selection/cancel/review/dry run copied template")
		}
	}
}

func TestMovingTheTextCursorDoesNotCreateAnOverride(t *testing.T) {
	c, root := command(t)
	if _, d, code := run(t, c, root, "fork", mtp, "mine"); code != 0 {
		t.Fatal(d)
	}
	before, _ := userpreset.Read(root, "mine")
	m := editor(t, c, root, "mine", false)
	editorClick(t, m, "Sampling")
	key(m, tea.KeyHome)
	key(m, tea.KeyRight)
	key(m, tea.KeyEnd)
	saveEditor(m)
	after, _ := userpreset.Read(root, "mine")
	if !bytes.Equal(before, after) {
		t.Fatal("cursor movement authored an override")
	}
}

func TestCustomLimitRejectsSentinelUntilANamedModeIsChosen(t *testing.T) {
	c, root := command(t)
	if _, d, code := run(t, c, root, "fork", mtp, "mine"); code != 0 {
		t.Fatal(d)
	}
	before, _ := userpreset.Read(root, "mine")
	m := editor(t, c, root, "mine", false)
	focusField(t, m, "Output limit")
	editorClick(t, m, "Custom")
	focusField(t, m, "Output limit value")
	typeText(m, "0")
	saveEditor(m)
	after, _ := userpreset.Read(root, "mine")
	if !bytes.Equal(before, after) {
		t.Fatal("custom zero silently became remaining context")
	}
	focusField(t, m, "Output limit")
	editorClick(t, m, "Remaining context")
	saveEditor(m)
	defaults := read(t, root, "mine").Changes["request_defaults"].(map[string]any)
	if defaults["max_output_tokens"] != 0 {
		t.Fatal("named remaining-context mode not saved")
	}
}

func TestCopyOfUnsavedLocalTemplateKeepsItsSelectedBytes(t *testing.T) {
	c, root := command(t)
	m := editor(t, singleSource(c), root, "", false)
	key(m, 'n')
	key(m, 'f')
	input(m, "First")
	directory := t.TempDir()
	path := filepath.Join(directory, "chat.jinja")
	if err := os.WriteFile(path, []byte(templateText), 0600); err != nil {
		t.Fatal(err)
	}
	focusField(t, m, "Template")
	editorClick(t, m, "Local file")
	editorClick(t, m, "Browse…")
	editorClick(t, m, "Location…")
	input(m, directory)
	completeRead(t, m, editorClick(t, m, "chat.jinja"))
	completeRead(t, m, editorClick(t, m, "Use template"))
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	editorClick(t, m, "Review")
	key(m, 'c')
	input(m, "Second")
	saveEditor(m)
	u := read(t, root, "second")
	if u.Fork() {
		t.Fatal("copy remained a fork")
	}
	template := u.Templates[u.Preset.Template]
	got, err := os.ReadFile(filepath.Join(userpreset.Dir(root), template.Files[0].Path))
	if err != nil || string(got) != templateText {
		t.Fatalf("copy lost pending template: %q %v", got, err)
	}
	if _, err := userpreset.Read(root, "first"); !os.IsNotExist(err) {
		t.Fatal("copy also published its unsaved source")
	}
}
