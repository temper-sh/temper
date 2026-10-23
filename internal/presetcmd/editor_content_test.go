package presetcmd_test

import (
	"bytes"
	tea "charm.land/bubbletea/v2"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/userpreset"
	"strings"
	"testing"
)

func TestVisiblePresetInputsRetainInvalidTextAcrossNavigationAndResize(t *testing.T) {
	for _, width := range []int{40, 80, 112} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			c, root := command(t)
			if _, d, code := run(t, c, root, "fork", mtp, "mine"); code != 0 {
				t.Fatal(d)
			}
			before, _ := userpreset.Read(root, "mine")
			m := editor(t, c, root, "mine", false)
			m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
			editorClick(t, m, "Sampling")
			typeText(m, "bad")
			frame := ansi.Strip(m.View().Content)
			if !strings.Contains(frame, "bad") || !strings.Contains(frame, "finite number") {
				t.Fatalf("invalid input lost: %s", frame)
			}
			editorClick(t, m, "Engine")
			saveEditor(m)
			after, _ := userpreset.Read(root, "mine")
			if !bytes.Equal(before, after) {
				t.Fatal("hidden invalid draft saved")
			}
			m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
			editorClick(t, m, "Sampling")
			if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "bad") {
				t.Fatalf("navigation lost raw input: %s", frame)
			}
			typeText(m, "0.7")
			key(m, tea.KeyDown)
			after, _ = userpreset.Read(root, "mine")
			if !bytes.Equal(before, after) {
				t.Fatal("navigation saved draft")
			}
			editorClick(t, m, "Save")
			defaults := read(t, root, "mine").Changes["request_defaults"].(map[string]any)
			if defaults["sampling"].(map[string]any)["temperature"] != 0.7 {
				t.Fatal("save lost corrected input")
			}
		})
	}
}

func TestVisibleNameSurvivesResizeAndDiscardRestoresSavedValue(t *testing.T) {
	c, root := command(t)
	m := editor(t, singleSource(c), root, "", false)
	key(m, 'n')
	key(m, 'f')
	input(m, "Writing")
	name := strings.Repeat("日本語 · écrire ", 8) + "tail"
	typeText(m, name)
	for _, width := range []int{112, 40, 28, 80} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		assertEditorFrame(t, m, width, 24)
	}
	saveEditor(m)
	if read(t, root, "writing").Name != name {
		t.Fatal("resize lost input")
	}
	key(m, tea.KeyEnter)
	typeText(m, "Cancelled")
	editorClick(t, m, "Discard")
	key(m, tea.KeyDown)
	key(m, tea.KeyEnter)
	if read(t, root, "writing").Name != name {
		t.Fatal("discard published the draft")
	}
}

func TestVisibleMaterialInputKeepsItsSourceForm(t *testing.T) {
	c, root := command(t)
	m := editor(t, singleSource(c), root, "", false)
	key(m, 'n')
	key(m, 'f')
	input(m, "Writing")
	focusField(t, m, "Weights")
	focusField(t, m, "Revision")
	typeText(m, "my-branch")
	frame := ansi.Strip(m.View().Content)
	for _, want := range []string{"my-branch", "Repository", "Revision", "Find files"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("missing %s: %s", want, frame)
		}
	}
}
