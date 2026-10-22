package presetcmd_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/userpreset"
)

func TestBreadcrumbBackKeepsTheUnsavedDraftUntilDiscardIsChosen(t *testing.T) {
	c, root := command(t)
	m := editor(t, singleSource(c), root, "", false)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	key(m, 'n')
	key(m, 'f')
	input(m, "Writing")
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "Presets ❯ Writing") {
		t.Fatalf("breadcrumb does not identify the authored preset:\n%s", frame)
	}

	key(m, tea.KeyTab) // Content → actions.
	key(m, tea.KeyTab) // Actions → parent breadcrumb.
	key(m, tea.KeyEnter)
	frame := ansi.Strip(m.View().Content)
	if !strings.Contains(frame, "Unsaved changes") || !strings.Contains(frame, "Keep editing") {
		t.Fatalf("breadcrumb bypassed the draft guard:\n%s", frame)
	}
	key(m, tea.KeyEnter) // Keep editing.
	frame = ansi.Strip(m.View().Content)
	if !strings.Contains(frame, "Writing") || !strings.Contains(frame, "unsaved") {
		t.Fatalf("draft was lost:\n%s", frame)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("navigation wrote authoring state: %v", err)
	}
}

func TestFooterSaveUsesTheExistingExplicitAuthoringSave(t *testing.T) {
	c, root := command(t)
	m := editor(t, singleSource(c), root, "", false)
	key(m, 'n')
	key(m, 'f')
	input(m, "Writing")

	key(m, tea.KeyTab)
	key(m, tea.KeyEnd) // Save is the last footer action.
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("focusing Save wrote authoring state: %v", err)
	}
	key(m, tea.KeyEnter)
	if _, err := userpreset.Read(root, "writing"); err != nil {
		t.Fatalf("footer Save did not publish the authoring file: %v", err)
	}
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "Saved writing") || !strings.Contains(frame, "[Yours]") {
		t.Fatalf("Save did not return to the manager:\n%s", frame)
	}
}

func TestSecondaryFocusPreservesTheEditingContext(t *testing.T) {
	c, root := command(t)
	m := editor(t, singleSource(c), root, "", false)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	key(m, 'n')
	key(m, 'f')
	input(m, "Writing")
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}) // Content → sections.
	key(m, tea.KeyRight)
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "[Sampling]") {
		t.Fatalf("focused section is hidden:\n%s", frame)
	}
	key(m, '?')
	if frame := ansi.Strip(m.View().Content); strings.Contains(frame, "Keyboard shortcuts") || !strings.Contains(frame, "[Sampling]") || !strings.Contains(frame, "unsaved") {
		t.Fatalf("unused key changed the editing context:\n%s", frame)
	}
	key(m, 'w')
	key(m, tea.KeyTab)
	key(m, tea.KeyTab)
	key(m, tea.KeyEnd) // The preset is the nearest ancestor of Weights.
	key(m, tea.KeyEnter)
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "[Sampling]") {
		t.Fatalf("returning from the weights form lost the editing section:\n%s", frame)
	}
	assertEditorFrame(t, m, 40, 24)
}
