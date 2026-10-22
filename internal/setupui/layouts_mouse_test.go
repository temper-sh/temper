package setupui

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/setup"
)

func layoutClick(t *testing.T, m *LayoutModel, label string) tea.Cmd {
	t.Helper()
	v := m.View()
	if v.MouseMode != tea.MouseModeCellMotion {
		t.Fatal("layout editor does not enable mouse reporting")
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

func TestLayoutMouseEditsMembershipSettingsAndReviewedSave(t *testing.T) {
	for _, width := range []int{40, 80, 112} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			input := layoutConfiguration(t)
			input.Layouts["other"] = setup.Layout{Name: "Other", Presets: []string{}, Startup: []string{}, IdleSeconds: 1800}
			var saved map[string]setup.Layout
			m, err := NewLayoutModel(context.Background(), LayoutInput{Configuration: input}, func(_ context.Context, layouts map[string]setup.Layout) (bool, error) {
				saved = layouts
				return true, nil
			}, func(_ context.Context, c setup.Configuration, id string) (setup.LayoutPlan, error) {
				return setup.LayoutPlan{ID: id, Layout: c.Layouts[id]}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
			layoutClick(t, m, "Writing")
			layoutClick(t, m, "Edit")
			layoutClick(t, m, "Default")
			layoutClick(t, m, "[x] Chat")
			layoutClick(t, m, "Keep included")
			if m.draft.Layouts["writing"].Default != "chat" {
				t.Fatal("mouse cancellation lost the default model")
			}
			layoutClick(t, m, "Exclude")
			layoutClick(t, m, "Exclude")
			layoutClick(t, m, "Preload") // Disabled while the preset is excluded.
			if len(m.draft.Layouts["writing"].Presets) != 0 || len(m.draft.Layouts["writing"].Startup) != 0 {
				t.Fatal("disabled mouse action changed membership")
			}
			layoutClick(t, m, "[ ] Chat")
			layoutClick(t, m, "Preload")
			layoutClick(t, m, "Settings")
			layoutClick(t, m, "Idle unload")
			m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
			m.Update(tea.PasteMsg{Content: "120"})
			layoutClick(t, m, "Apply")
			layoutClick(t, m, "Layouts")
			layoutClick(t, m, "Keep editing")
			preview := layoutClick(t, m, "Review")
			if saved != nil {
				t.Fatal("review published the draft before Save")
			}
			if cmd := layoutClick(t, m, "Save"); cmd != nil || saved != nil {
				t.Fatal("mouse Save bypassed the pending review")
			}
			m.Update(preview())
			cmd := layoutClick(t, m, "Save")
			if cmd == nil {
				t.Fatalf("mouse Save did not publish:\n%s", ansi.Strip(m.View().Content))
			}
			if repeated := layoutClick(t, m, "Save"); repeated != nil {
				t.Fatal("a second click started another pending save")
			}
			m.Update(cmd())
			want := setup.Layout{Name: "Writing", Presets: []string{"chat"}, Startup: []string{"chat"}, IdleSeconds: 120}
			if !reflect.DeepEqual(saved["writing"], want) || !reflect.DeepEqual(saved["other"], input.Layouts["other"]) || !strings.Contains(ansi.Strip(m.View().Content), "Layout saved") {
				t.Fatalf("mouse save lost the intended layout or changed another: %+v", saved)
			}
		})
	}
}

func TestLayoutMouseUsesScrolledWrappedRowsAndIgnoresOldTargetsAfterResize(t *testing.T) {
	input := layoutConfiguration(t)
	input.Layouts = map[string]setup.Layout{}
	for i := range 20 {
		id := fmt.Sprintf("layout-%02d", i)
		input.Layouts[id] = setup.Layout{Name: fmt.Sprintf("Layout %02d %sTail %02d", i, strings.Repeat("long ", 8), i), Presets: []string{}, Startup: []string{}, IdleSeconds: 1800}
	}
	m, err := NewLayoutModel(context.Background(), LayoutInput{Configuration: input}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 18})
	m.View()
	for range 60 {
		m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
		m.View()
	}
	layoutClick(t, m, "Tail 19")
	x, y := textPosition(t, m.View().Content, "Edit")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if m.mode != "list" {
		t.Fatal("a stale pre-resize click opened a layout")
	}
	x, y = textPosition(t, m.View().Content, "Edit")
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseRight})
	if m.mode != "list" {
		t.Fatal("right click activated Edit")
	}
	layoutClick(t, m, "Edit")
	if m.id != "layout-19" || m.mode != "edit" {
		t.Fatalf("scrolled click opened the wrong layout: %s", m.id)
	}
}
