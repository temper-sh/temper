package setupui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/setup"
)

func TestPresetTabFocusesNavigationWithoutActivatingIt(t *testing.T) {
	m := presetEditor(t)
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if view := ansi.Strip(m.View().Content); m.page != 0 || m.Decision.Action != "" || m.focus != presetFooterFocus || m.footerButtons()[m.action].target.kind != hitNext || !strings.Contains(view, "Next: Layouts") {
		t.Fatalf("Tab must focus Next without leaving Presets:\n%s", view)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.page != 1 {
		t.Fatal("Enter did not activate Next")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if view := ansi.Strip(m.View().Content); m.page != 1 || m.focus != presetFooterFocus || m.footerButtons()[m.action].target.key != "back" || !strings.Contains(view, "← Back") {
		t.Fatalf("Shift+Tab must focus Back without leaving Layouts:\n%s", view)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.page != 0 {
		t.Fatal("Enter did not activate Back")
	}
}

func TestPresetHorizontalArrowsNeverLeaveTheirScreen(t *testing.T) {
	for _, page := range []int{0, 1, 2} {
		t.Run(fmt.Sprint(page), func(t *testing.T) {
			m := populatedPresetEditor(t)
			m.page = page
			before, _ := m.draft.Bytes()
			for _, key := range []rune{tea.KeyLeft, tea.KeyRight, tea.KeyRight, tea.KeyLeft} {
				m.Update(tea.KeyPressMsg{Code: key})
			}
			after, _ := m.draft.Bytes()
			if m.page != page || m.Decision.Action != "" || string(before) != string(after) {
				t.Fatal("horizontal navigation left the screen or changed choices")
			}
		})
	}
}

func TestLayoutCheckboxesAreIndependentlyReachableWithArrows(t *testing.T) {
	for _, width := range []int{100, 28} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := presetEditor(t)
			m.Update(tea.WindowSizeMsg{Width: width, Height: 18})
			editorKey(m, ' ')
			m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
			m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // Local.
			m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // Included.
			m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
			if view := ansi.Strip(m.View().Content); m.focus != presetContentFocus || m.control != 1 || !strings.Contains(view, "[ ] Load on activation") {
				t.Fatalf("startup checkbox is not focused and visible:\n%s", view)
			}
			editorKey(m, ' ')
			m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
			m.Update(tea.WindowSizeMsg{Width: 28, Height: 10})
			if view := ansi.Strip(m.View().Content); m.focus != presetContentFocus || m.control != 2 || !strings.Contains(view, "[ ] Default") {
				t.Fatalf("default checkbox is not focused and visible:\n%s", view)
			}
			assertFrameFits(t, m.View().Content, 28, 10)
			editorKey(m, ' ')
			l := m.draft.Layouts["local"]
			if len(l.Presets) != 1 || len(l.Startup) != 1 || l.Default != l.Presets[0] {
				t.Fatalf("checkbox focus changed the wrong choice: %+v", l)
			}
			m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
			editorKey(m, ' ')
			l = m.draft.Layouts["local"]
			if len(l.Startup) != 0 || len(l.Presets) != 1 || l.Default == "" || m.layout != "local" {
				t.Fatal("clearing startup changed membership/default or left the editor")
			}
			m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
			m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			if m.field != "idle" {
				t.Fatal("idle-unload control is not reachable with arrows")
			}
		})
	}
}

func TestLayoutCreationRenameAndRemovalNeedNoLetterShortcuts(t *testing.T) {
	m := presetEditor(t)
	m.Update(tea.WindowSizeMsg{Width: 28, Height: 18})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight}) // New.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.field != "new" {
		t.Fatal("New layout is not reachable with arrows")
	}
	m.Update(tea.PasteMsg{Content: "Writing desk"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.draft.Layouts["writing-desk"].Name != "Writing desk" {
		t.Fatal("Apply did not create the layout")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight}) // Rename.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.field != "rename" || m.text.Value() != "Writing desk" {
		t.Fatal("Rename lost the selected layout while navigating the footer")
	}
	m.text.SetValue("Discard this name")
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight}) // Cancel.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.field != "" || m.draft.Layouts["writing-desk"].Name != "Writing desk" {
		t.Fatal("Cancel changed the saved draft name")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // Rename again.
	m.text.SetValue("Drafting desk")
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.draft.Layouts["writing-desk"].Name != "Drafting desk" {
		t.Fatal("Apply did not rename the same layout")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight}) // Remove.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, exists := m.draft.Layouts["writing-desk"]; exists || len(m.draft.Layouts) != 2 {
		t.Fatal("Remove did not remove only the selected layout")
	}
}

func TestReviewArrowsReachDownloadsAndRetryWithoutSaving(t *testing.T) {
	m := populatedPresetEditor(t)
	m.page = 2
	m.plan.CanPrepare = false
	m.preview = func(_ context.Context, c setup.Configuration) (setup.Plan, error) {
		return setup.Plan{Configuration: &c, CanPrepare: true}, nil
	}
	m.Update(tea.WindowSizeMsg{Width: 28, Height: 18})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // Save, without activating.
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if view := ansi.Strip(m.View().Content); m.focus != presetContentFocus || !strings.Contains(view, "▸ Downloads") {
		t.Fatalf("Downloads cannot receive arrow focus:\n%s", view)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.downloadsOpen {
		t.Fatal("arrow navigation activated the Downloads control")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.downloadsOpen {
		t.Fatal("Enter did not expand Downloads")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight}) // Prepare.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.Decision.Action != "" || m.notice == "" {
		t.Fatal("keyboard activation bypassed the preparation refusal")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight}) // Back.
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight}) // Cancel.
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight}) // Retry.
	if view := ansi.Strip(m.View().Content); m.focus != presetFooterFocus || m.footerButtons()[m.action].target.key != "retry" || !strings.Contains(view, "Retry") {
		t.Fatalf("Retry is not visible when focused:\n%s", view)
	}
	_, retry := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if retry == nil || !m.loading || m.Decision.Action != "" {
		t.Fatal("Retry did not start a preview without saving")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.Decision.Action != "" {
		t.Fatal("Save committed while the new preview was pending")
	}
	m.Update(retry())
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.page != 2 || m.Decision.Action != "" {
		t.Fatal("Shift+Tab activated Back or saved without confirmation")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.page != 1 || len(m.draft.Presets) != 2 {
		t.Fatal("Back discarded the draft")
	}
}

func TestPresetContextAndFormButtonsAreReachableInNarrowTerminal(t *testing.T) {
	m := presetEditor(t)
	m.Update(tea.WindowSizeMsg{Width: 28, Height: 12})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // Footer, after the one recommendation.
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if view := ansi.Strip(m.View().Content); m.focus != presetFooterFocus || m.footerButtons()[m.action].target.key != "c" || !strings.Contains(view, "Context") {
		t.Fatalf("Context is not reachable in a narrow terminal:\n%s", view)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.field != "context" {
		t.Fatal("focused Context did not open its editor")
	}
	m.text.SetValue("invalid")
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.field != "context" || m.text.Value() != "invalid" || m.notice == "" {
		t.Fatal("Apply discarded invalid input")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if view := ansi.Strip(m.View().Content); m.focus != presetFooterFocus || m.footerButtons()[m.action].target.key != "cancel-edit" || !strings.Contains(view, "Cancel") {
		t.Fatalf("Cancel is not focused and visible:\n%s", view)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.field != "" || len(m.draft.Presets) != 0 {
		t.Fatal("Cancel applied the context edit")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if view := ansi.Strip(m.View().Content); m.focus != presetFooterFocus || m.footerButtons()[m.action].target.key != "t" || !strings.Contains(view, "Template") {
		t.Fatalf("Template is not reachable after returning from the form:\n%s", view)
	}
}
