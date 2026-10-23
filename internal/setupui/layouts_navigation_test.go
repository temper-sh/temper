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

func TestLayoutArrowsEditCancelValidateAndSaveWithoutTab(t *testing.T) {
	for _, width := range []int{40, 112} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			var saved map[string]setup.Layout
			m, err := NewLayoutModel(context.Background(), LayoutInput{Configuration: layoutConfiguration(t)},
				func(_ context.Context, layouts map[string]setup.Layout) (bool, error) {
					saved = layouts
					return true, nil
				}, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(m.Close)
			m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
			layoutKey(m, tea.KeyDown) // Last layout → Edit.
			layoutKey(m, tea.KeyEnter)
			layoutKey(m, tea.KeyUp) // Preset list → sections.
			layoutKey(m, tea.KeyRight)
			layoutKey(m, tea.KeyDown)
			layoutCtrl(m, 'u')
			m.Update(tea.PasteMsg{Content: "Arrow writing"})
			layoutKey(m, tea.KeyDown) // Duration.
			layoutKey(m, tea.KeyDown) // Units.
			layoutKey(m, tea.KeyLeft) // Seconds.
			layoutKey(m, tea.KeyUp)   // Duration.
			layoutCtrl(m, 'u')
			m.Update(tea.PasteMsg{Content: "-1"})
			if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "positive duration") {
				t.Fatalf("invalid text lost: %s", frame)
			}
			layoutKey(m, tea.KeyDown) // Units.
			layoutKey(m, tea.KeyDown) // Save.
			if cmd := layoutKey(m, tea.KeyEnter); cmd != nil || saved != nil {
				t.Fatal("invalid duration saved")
			}
			layoutKey(m, tea.KeyUp) // Units.
			layoutKey(m, tea.KeyUp) // Duration.
			layoutCtrl(m, 'u')
			m.Update(tea.PasteMsg{Content: "60"})
			layoutKey(m, tea.KeyUp)
			layoutKey(m, tea.KeyDown)
			layoutKey(m, tea.KeyLeft)
			m.Update(tea.PasteMsg{Content: "2"}) // 620 seconds.
			layoutKey(m, tea.KeyDown)            // Units.
			layoutKey(m, tea.KeyDown)            // Save.
			layoutKey(m, tea.KeyLeft)            // Review.
			layoutKey(m, tea.KeyEnter)

			if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "620") || !strings.Contains(frame, "Save") {
				t.Fatalf("arrows did not reach Review with the edited timeout:\n%s", frame)
			}
			if saved != nil {
				t.Fatal("arrow navigation saved before explicit Save")
			}
			layoutKey(m, tea.KeyUp) // Review content.
			frame := ansi.Strip(m.View().Content)
			for steps := 0; steps < 200 && !strings.Contains(frame, "←→ actions"); steps++ {
				layoutKey(m, tea.KeyDown)
				frame = ansi.Strip(m.View().Content)
			}
			if !strings.Contains(frame, "←→ actions") {
				t.Fatalf("Review trapped arrow focus:\n%s", frame)
			}
			cmd := layoutKey(m, tea.KeyEnter)
			if cmd == nil {
				t.Fatal("arrow Save did not publish")
			}
			m.Update(cmd())
			if saved["writing"].IdleSeconds != 620 || len(saved["writing"].Presets) != 1 {
				t.Fatalf("arrow editing changed the wrong layout values: %+v", saved)
			}
		})
	}
}

func TestLayoutArrowsSkipEmptyListsAndWrapActionsWithoutBypassingGuards(t *testing.T) {
	c := setup.EmptyConfiguration()
	m, err := NewLayoutModel(context.Background(), LayoutInput{Configuration: c, PresetErrors: map[string]string{"broken": "Invalid preset file."}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	layoutKey(m, tea.KeyUp)    // Empty list → New.
	layoutKey(m, tea.KeyLeft)  // Wrap to Done.
	layoutKey(m, tea.KeyRight) // Wrap to New.
	layoutKey(m, tea.KeyEnter)
	m.Update(tea.PasteMsg{Content: "New layout"})
	layoutKey(m, tea.KeyDown)
	layoutKey(m, tea.KeyEnter)
	for range 3 {
		layoutKey(m, tea.KeyEnter) // Every checkbox on an invalid preset is disabled.
		layoutKey(m, tea.KeyRight)
	}
	if m.mode != "edit" || len(m.draft.Layouts["new-layout"].Presets) != 0 {
		t.Fatal("arrow activation included an invalid preset")
	}
	layoutKey(m, tea.KeyDown)  // Last preset → Review.
	layoutKey(m, tea.KeyLeft)  // Wrap to Back.
	layoutKey(m, tea.KeyRight) // Wrap to Review.
	layoutKey(m, tea.KeyDown)  // Footer → breadcrumb.
	layoutKey(m, tea.KeyEnter)
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "Discard unsaved") {
		t.Fatalf("arrow breadcrumb bypassed the draft guard:\n%s", frame)
	}
	layoutKey(m, tea.KeyDown) // A confirmation must retain its safe default.
	layoutKey(m, tea.KeyEnter)
	if m.mode != "edit" || m.draft.Layouts["new-layout"].Name != "New layout" {
		t.Fatal("arrow focus escaped the confirmation or discarded the draft")
	}
}
