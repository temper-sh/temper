package setupui

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/setup"
)

func layoutConfiguration(t *testing.T) setup.Configuration {
	t.Helper()
	raw, err := os.ReadFile("../testfixture/testdata/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := catalog.CompilePreset(doc, "qwen3.5-4b-q4km-off", 0, 0, doc.Runtime.Router.Target)
	if err != nil {
		t.Fatal(err)
	}
	c := setup.EmptyConfiguration()
	c.Presets["chat"] = setup.Preset{Name: "Chat", Lock: lock}
	c.Layouts["writing"] = setup.Layout{Name: "Writing", Presets: []string{"chat"}, Startup: []string{}, IdleSeconds: 1800}
	return c
}

func layoutKey(m *LayoutModel, code rune) tea.Cmd {
	msg := tea.KeyPressMsg{Code: code}
	if code >= 32 && code < 127 {
		msg.Text = string(code)
	}
	_, cmd := m.Update(msg)
	return cmd
}

func layoutCtrl(m *LayoutModel, code rune) tea.Cmd {
	_, cmd := m.Update(tea.KeyPressMsg{Code: code, Mod: tea.ModCtrl})
	return cmd
}

func TestStandaloneLayoutMembershipClearsDependenciesOnlyAfterConfirmation(t *testing.T) {
	input := layoutConfiguration(t)
	l := input.Layouts["writing"]
	l.Startup, l.Default = []string{"chat"}, "chat"
	input.Layouts["writing"] = l
	var saved map[string]setup.Layout
	m, err := NewLayoutModel(context.Background(), LayoutInput{Configuration: input, ID: "writing"}, func(_ context.Context, layouts map[string]setup.Layout) (bool, error) {
		saved = layouts
		return true, nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	layoutKey(m, ' ')
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "clears its preload and default") {
		t.Fatalf("dependent choices were not disclosed:\n%s", view)
	}
	layoutKey(m, tea.KeyEnter) // Keep included.
	if !reflect.DeepEqual(m.draft.Layouts["writing"], l) || saved != nil {
		t.Fatal("default confirmation changed layout membership")
	}
	layoutKey(m, ' ')
	layoutKey(m, tea.KeyRight)
	layoutKey(m, tea.KeyEnter)
	layoutCtrl(m, 's') // A memory assessment is optional; validation is not.
	m.Update(layoutCtrl(m, 's')())
	want := setup.Layout{Name: "Writing", Presets: []string{}, Startup: []string{}, IdleSeconds: 1800}
	if !reflect.DeepEqual(saved["writing"], want) {
		t.Fatalf("dependency removal left invalid references: %+v", saved["writing"])
	}
	if !reflect.DeepEqual(input.Layouts["writing"], l) {
		t.Fatal("editing mutated caller-owned slices")
	}
}

func TestLayoutSettingsAndDiscardAreReachableThroughNavigation(t *testing.T) {
	input := layoutConfiguration(t)
	m, err := NewLayoutModel(context.Background(), LayoutInput{Configuration: input}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	if view := ansi.Strip(m.View().Content); strings.Contains(view, "Catalog") || strings.Contains(view, "Yours") {
		t.Fatal("layout list acquired preset source navigation")
	}
	layoutKey(m, tea.KeyEnter)
	layoutKey(m, tea.KeyTab) // Actions.
	layoutKey(m, tea.KeyTab) // Breadcrumb.
	layoutKey(m, tea.KeyTab) // Sections.
	layoutKey(m, tea.KeyRight)
	layoutKey(m, tea.KeyEnter)
	layoutCtrl(m, 'u')
	m.Update(tea.PasteMsg{Content: "Quiet writing"})
	layoutKey(m, tea.KeyEnter)
	layoutCtrl(m, 'u')
	m.Update(tea.PasteMsg{Content: "-1"})
	layoutKey(m, tea.KeyEnter)
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "positive duration") {
		t.Fatalf("invalid idle timeout was accepted:\n%s", view)
	}
	layoutKey(m, tea.KeyUp) // Return from units to the invalid duration.
	layoutCtrl(m, 'u')
	m.Update(tea.PasteMsg{Content: "120"})
	layoutKey(m, tea.KeyEnter)
	layoutKey(m, tea.KeyTab)
	layoutKey(m, tea.KeyTab)
	layoutKey(m, tea.KeyEnter) // Back through Layouts breadcrumb.
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Discard unsaved") {
		t.Fatalf("breadcrumb lost unsaved changes without asking:\n%s", view)
	}
	layoutKey(m, tea.KeyEnter) // Keep editing.
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Quiet writing") || !strings.Contains(view, "120") {
		t.Fatalf("Keep editing lost the draft or its section:\n%s", view)
	}
	layoutKey(m, tea.KeyEsc)
	layoutKey(m, tea.KeyRight)
	layoutKey(m, tea.KeyEnter) // Discard.
	if view := ansi.Strip(m.View().Content); strings.Contains(view, "Quiet writing") || !strings.Contains(view, "Writing") {
		t.Fatalf("discard did not restore the saved layout:\n%s", view)
	}
	if !reflect.DeepEqual(m.draft, input) {
		t.Fatal("discard changed the baseline configuration")
	}
}

func TestNewLayoutDerivesUnusedIDsAndKeepsNonLatinNames(t *testing.T) {
	for _, tc := range []struct{ name, id string }{{"Writing", "writing-2"}, {"執筆", "layout"}} {
		t.Run(tc.id, func(t *testing.T) {
			input := layoutConfiguration(t)
			var saved map[string]setup.Layout
			m, err := NewLayoutModel(context.Background(), LayoutInput{Configuration: input}, func(_ context.Context, layouts map[string]setup.Layout) (bool, error) {
				saved = layouts
				return true, nil
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			layoutKey(m, 'n')
			layoutKey(m, tea.KeyEnter)
			if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Enter a valid value") {
				t.Fatal("empty layout name was accepted")
			}
			m.Update(tea.PasteMsg{Content: tc.name})
			layoutKey(m, tea.KeyEnter)
			layoutCtrl(m, 's')
			m.Update(layoutCtrl(m, 's')())
			if saved[tc.id].Name != tc.name || len(saved[tc.id].Presets) != 0 || !reflect.DeepEqual(saved["writing"], input.Layouts["writing"]) {
				t.Fatal("new layout overwrote a saved one or selected presets implicitly", saved)
			}
		})
	}
}

func TestLateLayoutAssessmentCannotReplaceANewerReview(t *testing.T) {
	m, err := NewLayoutModel(context.Background(), LayoutInput{Configuration: layoutConfiguration(t), ID: "writing"}, nil,
		func(ctx context.Context, c setup.Configuration, id string) (setup.LayoutPlan, error) {
			if ctx.Err() != nil {
				return setup.LayoutPlan{}, errors.New("stale assessment")
			}
			return setup.LayoutPlan{ID: id, Layout: c.Layouts[id]}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	old := layoutCtrl(m, 's')
	layoutKey(m, tea.KeyEsc)
	current := layoutCtrl(m, 's')
	m.Update(current())
	m.Update(old())
	if view := ansi.Strip(m.View().Content); strings.Contains(view, "stale assessment") || !strings.Contains(view, "Included") {
		t.Fatalf("cancelled assessment replaced current review:\n%s", view)
	}
}

func TestLayoutSavedDurabilityErrorIsNotAnUnsavedDraft(t *testing.T) {
	m, err := NewLayoutModel(context.Background(), LayoutInput{Configuration: layoutConfiguration(t), ID: "writing"},
		func(context.Context, map[string]setup.Layout) (bool, error) {
			return true, errors.New("configuration saved but directory sync failed")
		}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	layoutKey(m, 'p')
	layoutCtrl(m, 's')
	m.Update(layoutCtrl(m, 's')())
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "configuration saved but") {
		t.Fatalf("post-publication error was hidden:\n%s", view)
	}
	layoutKey(m, tea.KeyEsc)
	layoutKey(m, tea.KeyEsc)
	if view := ansi.Strip(m.View().Content); strings.Contains(view, "unsaved") {
		t.Fatalf("published configuration was offered for discard:\n%s", view)
	}
}

func TestLayoutFramesKeepControlsVisibleAtSupportedSizes(t *testing.T) {
	for _, width := range []int{40, 80, 112} {
		for _, height := range []int{24, 32} {
			m, err := NewLayoutModel(context.Background(), LayoutInput{Configuration: layoutConfiguration(t), ID: "writing"}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(m.Close)
			m.Update(tea.WindowSizeMsg{Width: width, Height: height})
			view := m.View().Content
			assertFrameFits(t, view, width, height)
			for _, label := range []string{"TEMPER", "Layouts", "Writing", "[Presets]", "Chat", "[x] Included", "Review", "Load on activation", "Default"} {
				if !strings.Contains(ansi.Strip(view), label) {
					t.Fatalf("%dx%d hides %s:\n%s", width, height, label, ansi.Strip(view))
				}
			}
			layoutKey(m, tea.KeyUp) // Sections, then Settings.
			layoutKey(m, tea.KeyRight)
			layoutKey(m, tea.KeyDown)
			layoutKey(m, tea.KeyEnter)
			assertFrameFits(t, m.View().Content, width, height)
			layoutKey(m, tea.KeyEsc)
			layoutCtrl(m, 's')
			assertFrameFits(t, m.View().Content, width, height)
		}
	}
}
