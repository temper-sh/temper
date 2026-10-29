package setupui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/setup"
)

func TestPresetTabCyclesControlsWithoutActivatingThem(t *testing.T) {
	m := presetEditor(t)
	for _, area := range []presetFocus{presetFooterFocus, presetStepsFocus, presetFilterFocus, presetContentFocus} {
		m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		if m.focus != area || m.page != 0 || m.Decision.Action != "" || len(m.draft.Presets) != 0 {
			t.Fatalf("Tab did not focus area %d without activating it", area)
		}
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.footerButtons()[m.action].target.kind != hitNext {
		t.Fatal("Tab must focus Next")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.page != presetHarnessPage {
		t.Fatal("Enter did not activate Next")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.focus != presetStepsFocus {
		t.Fatal("reverse focus cycle did not skip empty harness content")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.page != presetHarnessPage {
		t.Fatal("moving primary focus changed the page")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.page != 0 {
		t.Fatal("Enter did not return to Presets")
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

func TestReviewArrowsScrollAndReachRetryWithoutSaving(t *testing.T) {
	m := populatedPresetEditor(t)
	m.page = presetReviewPage
	m.plan.CanPrepare = false
	m.preview = func(_ context.Context, c setup.Configuration) (setup.Plan, error) {
		return setup.Plan{Configuration: &c, CanPrepare: true}, nil
	}
	m.Update(tea.WindowSizeMsg{Width: 28, Height: 18})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // Back: installation is impossible.
	if b := m.footerButtons()[m.action]; b.short != "Back" {
		t.Fatalf("Tab focused %q, not Back, while installation is impossible", b.label)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m.View()
	if m.focus != presetContentFocus {
		t.Fatal("Review content cannot receive arrow focus")
	}
	offset := m.view.YOffset()
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.focus != presetContentFocus || m.view.YOffset() <= offset {
		t.Fatal("Down did not scroll Review content")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.view.YOffset() != offset {
		t.Fatal("Up did not scroll back to the previous position")
	}
	before, _ := m.draft.Bytes()
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	after, _ := m.draft.Bytes()
	if string(before) != string(after) || m.Decision.Action != "" || m.view.YOffset() != offset {
		t.Fatal("read-only Review content activated an action or changed the draft")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	for i := 0; i < 5 && m.footerButtons()[m.action].short != "Install"; i++ {
		m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	}
	if b := m.footerButtons()[m.action]; b.label != "Can't install yet" || b.available {
		t.Fatalf("the install button does not say it cannot run: %+v", b)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.Decision.Action != "" || m.notice == "" {
		t.Fatal("keyboard activation bypassed the installation refusal")
	}
	// Retry appears only after a failed check.
	if slices.ContainsFunc(m.footerButtons(), func(b presetButton) bool { return b.target.key == "retry" }) {
		t.Fatal("Retry offered without a failed check")
	}
	m.reviewErr = errors.New("catalog unreachable")
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft}) // Wraps to Retry.
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
	if m.page != presetReviewPage || m.Decision.Action != "" {
		t.Fatal("Shift+Tab activated Back or saved without confirmation")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight}) // Save.
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight}) // Back; reverse Tab also enters on the primary action.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.page != presetHarnessPage || len(m.draft.Presets) != 2 {
		t.Fatal("Back discarded the draft")
	}
}

func TestWizardContextPromptAppliesOrDiscardsChanges(t *testing.T) {
	for _, cancel := range []string{"Escape", "Cancel"} {
		t.Run(cancel, func(t *testing.T) {
			m := presetEditor(t)
			editorKey(m, ' ')
			p, _ := m.selectedOption()
			_, original := presetRecord(m.draft.Presets[p.ID].Lock)
			clickPresetText(t, m, "c Context")
			if !strings.Contains(m.View().Content, "Apply") {
				t.Fatal("Context did not open its Apply/Cancel prompt")
			}
			wizardType(m, "8192")
			_, draft := presetRecord(m.draft.Presets[p.ID].Lock)
			if draft.Window() != original.Window() {
				t.Fatal("typing changed the draft before Apply")
			}
			if cancel == "Escape" {
				m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
			} else {
				clickPresetText(t, m, "Cancel")
			}
			editorKey(m, 'c')
			if m.text.Value() != strconv.Itoa(original.Window()) || m.Decision.Action != "" {
				t.Fatal("cancelling kept the uncommitted value or quit the wizard")
			}
			wizardType(m, "8192")
			clickPresetText(t, m, "Apply")
			_, draft = presetRecord(m.draft.Presets[p.ID].Lock)
			if draft.Window() != 8192 || m.Decision.Action != "" {
				t.Fatal("Apply did not update the revocable draft")
			}
			if !strings.Contains(ansi.Strip(m.View().Content), "Context "+tokenCount(8192)+" tokens") {
				t.Fatal("card lost its context summary")
			}
		})
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
	if view := ansi.Strip(m.View().Content); m.focus != presetContentFocus || !strings.Contains(view, "[ ]") {
		t.Fatalf("Cancel did not return to the unchanged preset row:\n%s", view)
	}
}

func TestWizardArrowsApplyContextAndReachSaveWithoutTab(t *testing.T) {
	m := presetEditor(t)
	m.preview = func(_ context.Context, c setup.Configuration) (setup.Plan, error) {
		return setup.Plan{Configuration: &c, CanPrepare: true}, nil
	}
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // Include the selected preset.
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight}) // Context.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m.Update(tea.PasteMsg{Content: "bad"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp}) // Apply, from either direction.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.field == "" || m.focus != presetContentFocus || m.notice == "" {
		t.Fatal("invalid Apply did not retain the input with typing focus")
	}
	m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m.Update(tea.PasteMsg{Content: "8192"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if button := m.footerButtons()[m.action]; button.target.key != "cancel-edit" {
		t.Fatal("Cancel is not left of Apply")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.field != "" || m.focus != presetContentFocus || m.Decision.Action != "" {
		t.Fatal("Apply did not return to content with an unpublished draft")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp}) // Secondary.
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp}) // Primary.
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp}) // Next.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp}) // Skip empty Harnesses.
	if m.focus != presetStepsFocus {
		t.Fatal("empty harness content trapped arrow focus")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	_, preview := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if preview == nil || m.Decision.Action != "" {
		t.Fatal("arrow Review did not start an unpublished preview")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // Pending check cannot commit.
	if m.Decision.Action != "" {
		t.Fatal("Enter bypassed the pending review")
	}
	m.Update(preview())
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight}) // Save without installing.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.Decision.Action != "save" || len(m.Decision.Configuration.Presets) != 1 {
		t.Fatalf("arrow Save returned %q", m.Decision.Action)
	}
	for _, p := range m.Decision.Configuration.Presets {
		_, record := presetRecord(p.Lock)
		if record.Window() != 8192 {
			t.Fatal("Save lost the context applied through arrow navigation")
		}
	}
}

func TestEscOnTheFirstScreenKeepsTheChoices(t *testing.T) {
	m := populatedPresetEditor(t)
	m.page = 0
	selected := len(m.draft.Presets)
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.Decision.Action != "" || len(m.draft.Presets) != selected || !strings.Contains(m.notice, "press q") {
		t.Fatalf("Esc left the wizard or dropped choices: %q %d", m.Decision.Action, len(m.draft.Presets))
	}
	m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if m.Decision.Action != "cancel" {
		t.Fatal("q no longer leaves")
	}
}
