package presetcmd_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/presetcmd"
	"github.com/temper-sh/temper/internal/userpreset"
)

func editorClick(t *testing.T, m *presetcmd.Editor, label string) tea.Cmd {
	t.Helper()
	v := m.View()
	if v.MouseMode != tea.MouseModeCellMotion {
		t.Fatal("preset editor does not enable mouse reporting")
	}
	lines := strings.Split(ansi.Strip(v.Content), "\n")
	for y := len(lines) - 1; y >= 0; y-- {
		line := lines[y]
		if x := strings.Index(line, label); x >= 0 {
			_, cmd := m.Update(tea.MouseClickMsg{X: ansi.StringWidth(line[:x]), Y: y, Button: tea.MouseLeft})
			return cmd
		}
	}
	t.Fatalf("click target %q is not visible:\n%s", label, ansi.Strip(v.Content))
	return nil
}

func TestPresetMouseUsesVisibleScrolledRowsAndInvalidatesTargetsOnResize(t *testing.T) {
	c, root := command(t)
	view, _ := c.Catalog(root)
	base := view.Document.Presets[mtp]
	view.Document.Presets = map[string]catalog.Preset{}
	for i := range 20 {
		p := base
		p.Presentation.DisplayName = fmt.Sprintf("Choice %02d %sTail %02d", i, strings.Repeat("long ", 8), i)
		view.Document.Presets[fmt.Sprintf("choice-%02d", i)] = p
	}
	c.Catalog = func(string) (catalog.View, error) { return view, nil }
	m := editor(t, c, root, "", false)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 18})
	editorClick(t, m, "Catalog")
	for range 60 {
		m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
		m.View()
	}
	editorClick(t, m, "Tail 19") // A wrapped continuation line, after scrolling.
	frame := m.View().Content
	for y, line := range strings.Split(ansi.Strip(frame), "\n") {
		if x := strings.Index(line, "Open"); x >= 0 {
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			m.Update(tea.MouseClickMsg{X: ansi.StringWidth(line[:x]), Y: y, Button: tea.MouseLeft})
			break
		}
	}
	if frame := ansi.Strip(m.View().Content); strings.Contains(frame, "❯") {
		t.Fatal("a stale pre-resize click opened a preset")
	}
	editorClick(t, m, "Open")
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "Choice 19") || !strings.Contains(frame, "Fork") {
		t.Fatalf("scrolled click selected the wrong preset:\n%s", frame)
	}
}

func TestPresetMouseCancelsPendingReadsAndSelectsMaterialFiles(t *testing.T) {
	c, root := withHub(t)
	if _, diagnostic, code := run(t, c, root, "fork", mtp, "mine"); code != 0 {
		t.Fatal(diagnostic)
	}
	before, _ := userpreset.Read(root, "mine")
	m := editor(t, c, root, "mine", false)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 32})
	editorClick(t, m, "Weights")
	editorClick(t, m, "Revision")
	old := editorClick(t, m, "Find files")
	if old == nil {
		t.Fatal("clicking the lookup action did not start its read")
	}
	editorClick(t, m, "Cancel")
	m.Update(old())
	if frame := ansi.Strip(m.View().Content); strings.Contains(frame, "Select weights files") {
		t.Fatal("a cancelled read changed the visible form")
	}
	completeRead(t, m, editorClick(t, m, "Find files"))
	editorClick(t, m, "Qwen3.8-27B-Q6_K.gguf")
	completeRead(t, m, editorClick(t, m, "Use weights"))
	after, _ := userpreset.Read(root, "mine")
	if !bytes.Equal(before, after) {
		t.Fatal("choosing files published before Save")
	}
	editorClick(t, m, "Save")
	u := read(t, root, "mine")
	artifact := u.Artifacts[u.Changes["artifact"].(string)]
	if len(artifact.Files) != 1 || artifact.Files[0].Path != "Qwen3.8-27B-Q6_K.gguf" {
		t.Fatalf("mouse file selection was lost: %+v", artifact.Files)
	}
}

func TestPresetMouseEditsSelectedFileAndKeepsConfirmationAndSaveGuards(t *testing.T) {
	for _, width := range []int{40, 80, 112} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			c, root := command(t)
			for _, id := range []string{"alpha", "beta"} {
				if _, diagnostic, code := run(t, c, root, "fork", mtp, id, "--name", strings.Title(id)); code != 0 {
					t.Fatal(diagnostic)
				}
			}
			m := editor(t, c, root, "", false)
			m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
			editorClick(t, m, "Beta")
			editorClick(t, m, "Open")
			editorClick(t, m, "Sampling")
			editorClick(t, m, "Temperature")
			m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
			m.Update(tea.PasteMsg{Content: "invalid"})
			editorClick(t, m, "Save")
			if view := ansi.Strip(m.View().Content); !strings.Contains(view, "invalid") {
				t.Fatalf("invalid input was discarded:\n%s", view)
			}
			editorClick(t, m, "Temperature") // Return to the invalid draft input.
			m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
			m.Update(tea.PasteMsg{Content: "0.6"})
			editorClick(t, m, "Presets")
			editorClick(t, m, "Keep editing")
			if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Unsaved changes") {
				t.Fatal("selecting a confirmation choice activated it")
			}
			editorClick(t, m, "Confirm")
			editorClick(t, m, "Save")
			beta := read(t, root, "beta")
			defaults, ok := beta.Changes["request_defaults"].(map[string]any)
			if !ok {
				t.Fatal("mouse Save did not retain the sampling edit")
			}
			if defaults["sampling"].(map[string]any)["temperature"] != 0.6 || len(read(t, root, "alpha").Changes) != 0 {
				t.Fatal("mouse editing changed the wrong preset or lost the value")
			}
		})
	}
}
