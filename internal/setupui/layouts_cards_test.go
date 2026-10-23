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

func TestLayoutCardsExposeAllChoicesAndComposeWithArrows(t *testing.T) {
	for _, width := range []int{40, 80, 112} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			configuration := layoutConfiguration(t)
			quiet := configuration.Presets["chat"]
			quiet.Name, quiet.UserPreset = "Quiet writing", quiet.Lock.Preset
			configuration.Presets["quiet"] = quiet
			var saved map[string]setup.Layout
			m, err := NewLayoutModel(context.Background(), LayoutInput{Configuration: configuration, ID: "writing"},
				func(_ context.Context, layouts map[string]setup.Layout) (bool, error) {
					saved = layouts
					return true, nil
				}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
			layoutKey(m, tea.KeyDown)
			frame := ansi.Strip(m.View().Content)
			for _, want := range []string{"Quiet writing", "untested", "[ ] Included", "[ ] Load on activation", "[ ] Default"} {
				if !strings.Contains(frame, want) {
					t.Fatalf("selected preset hides %q:\n%s", want, frame)
				}
			}
			layoutKey(m, tea.KeyRight) // Disabled preload.
			layoutKey(m, tea.KeyEnter)
			layoutKey(m, tea.KeyRight) // Disabled default.
			layoutKey(m, ' ')
			if !reflect.DeepEqual(m.draft.Layouts["writing"], configuration.Layouts["writing"]) {
				t.Fatal("checkboxes implicitly included a preset")
			}
			layoutKey(m, tea.KeyRight) // Wrap to Included.
			layoutKey(m, tea.KeyEnter)
			layoutKey(m, tea.KeyRight)
			layoutKey(m, tea.KeyEnter)
			layoutKey(m, tea.KeyRight)
			layoutKey(m, ' ')
			frame = ansi.Strip(m.View().Content)
			for _, want := range []string{"[x] Included", "[x] Load on activation", "[x] Default"} {
				if !strings.Contains(frame, want) {
					t.Fatalf("chosen option %q is not visible:\n%s", want, frame)
				}
			}
			layoutKey(m, tea.KeyUp) // Retain the Default column on Chat.
			layoutKey(m, tea.KeyEnter)
			if m.draft.Layouts["writing"].Default != "chat" {
				t.Fatal("choosing another default did not replace the previous one")
			}
			layoutKey(m, tea.KeyLeft)
			layoutKey(m, tea.KeyLeft)
			layoutKey(m, tea.KeyEnter) // Excluding the default asks first.
			if frame := ansi.Strip(m.View().Content); !strings.Contains(strings.Join(strings.Fields(frame), " "), "clears its preload and default") {
				t.Fatalf("checkbox bypassed the reference guard:\n%s", frame)
			}
			layoutKey(m, tea.KeyEnter) // Keep included.
			layoutKey(m, tea.KeyRight)
			layoutKey(m, tea.KeyRight)
			layoutKey(m, tea.KeyEnter) // Unset the optional default.
			if m.draft.Layouts["writing"].Default != "" {
				t.Fatal("default checkbox could not be cleared")
			}
			layoutKey(m, tea.KeyDown)
			layoutKey(m, tea.KeyEnter) // Quiet writing is default again.
			layoutKey(m, tea.KeyDown)  // Last preset → Save.
			layoutKey(m, tea.KeyLeft)  // Review.
			layoutKey(m, tea.KeyEnter)
			if saved != nil {
				t.Fatal("reading or choosing cards published before Save")
			}
			m.Update(layoutKey(m, tea.KeyEnter)())
			layout := saved["writing"]
			if !reflect.DeepEqual(layout.Presets, []string{"chat", "quiet"}) || !reflect.DeepEqual(layout.Startup, []string{"quiet"}) || layout.Default != "quiet" {
				t.Fatalf("inline controls changed the wrong member: %+v", layout)
			}
			if !reflect.DeepEqual(m.draft.Presets, configuration.Presets) {
				t.Fatal("composition changed an exact preset")
			}
		})
	}
}

func TestLayoutCheckboxTargetsFollowWrappedCardsAndScrolling(t *testing.T) {
	configuration := layoutConfiguration(t)
	for i := range 12 {
		preset := configuration.Presets["chat"]
		preset.Name = fmt.Sprintf("Variant %02d %s", i, strings.Repeat("long name ", 10))
		configuration.Presets[fmt.Sprintf("variant-%02d", i)] = preset
	}
	m, err := NewLayoutModel(context.Background(), LayoutInput{Configuration: configuration, ID: "writing"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	m.View()
	for range 12 {
		layoutKey(m, tea.KeyDown)
		m.View()
	}
	layoutClick(t, m, "[ ] Included")
	layoutClick(t, m, "[ ] Load on activation")
	layoutClick(t, m, "[ ] Default")
	layout := m.draft.Layouts["writing"]
	if !reflect.DeepEqual(layout.Presets, []string{"chat", "variant-11"}) || !reflect.DeepEqual(layout.Startup, []string{"variant-11"}) || layout.Default != "variant-11" {
		t.Fatalf("wrapped/scrolled checkbox changed another preset: %+v", layout)
	}
	x, y := textPosition(t, m.View().Content, "[x] Included")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if m.mode != "edit" || !reflect.DeepEqual(m.draft.Layouts["writing"], layout) {
		t.Fatal("stale checkbox coordinates changed composition")
	}
	m.View()
	layoutClick(t, m, "Variant 11")
	if !reflect.DeepEqual(m.draft.Layouts["writing"], layout) {
		t.Fatal("clicking a card name toggled membership")
	}
}
