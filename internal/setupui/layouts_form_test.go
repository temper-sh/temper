package setupui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/setup"
)

func TestLayoutVisibleInputsKeepInvalidTextAndSaveOnlyExplicitly(t *testing.T) {
	var saved map[string]setup.Layout
	m, err := NewLayoutModel(context.Background(), LayoutInput{Configuration: layoutConfiguration(t), ID: "writing"}, func(_ context.Context, layouts map[string]setup.Layout) (bool, error) {
		saved = layouts
		return true, nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 32})
	m.click(layoutSections, 1)
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Time unit") || strings.Contains(view, "Apply") {
		t.Fatalf("settings are not a visible form:\n%s", view)
	}
	layoutCtrl(m, 'u')
	m.Update(tea.PasteMsg{Content: "Quiet writing"})
	layoutKey(m, tea.KeyDown)
	layoutCtrl(m, 'u')
	m.Update(tea.PasteMsg{Content: "bad"})
	layoutKey(m, tea.KeyDown)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.click(layoutSections, 0)
	m.click(layoutSections, 1)
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "bad") {
		t.Fatalf("navigation lost invalid text:\n%s", view)
	}
	if cmd := m.invoke("save"); cmd != nil || saved != nil {
		t.Fatal("invalid form saved an older valid value")
	}
	m.click(layoutContent, 1)
	layoutCtrl(m, 'u')
	m.Update(tea.PasteMsg{Content: "2"})
	if m.draft.Layouts["writing"].IdleSeconds != 120 || saved != nil {
		t.Fatal("minutes were not applied to the unsaved draft")
	}
	m.Update(m.invoke("save")())
	if saved["writing"].Name != "Quiet writing" || saved["writing"].IdleSeconds != 120 {
		t.Fatalf("saved wrong form: %+v", saved)
	}
}

func TestLayoutUnitChangePreservesDurationAndDiscardRestoresDraft(t *testing.T) {
	input := layoutConfiguration(t)
	l := input.Layouts["writing"]
	l.IdleSeconds = 90
	input.Layouts["writing"] = l
	m, err := NewLayoutModel(context.Background(), LayoutInput{Configuration: input, ID: "writing"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.click(layoutSections, 1)
	m.click(layoutContent, 2)
	layoutKey(m, tea.KeyRight)
	if m.draft.Layouts["writing"].IdleSeconds != 90 || m.dirty() {
		t.Fatal("changing display units changed the layout")
	}
	m.click(layoutContent, 0)
	layoutCtrl(m, 'u')
	m.Update(tea.PasteMsg{Content: ""})
	layoutKey(m, tea.KeyEsc)
	if m.mode != "confirm" {
		t.Fatal("invalid empty name bypassed discard guard")
	}
	m.invoke("confirm")
	if m.dirty() || m.draft.Layouts["writing"].Name != "Writing" {
		t.Fatal("discard retained form edits")
	}
}
