package setupui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/setup"
)

func populatedPresetEditor(t *testing.T) *PresetModel {
	t.Helper()
	m := presetEditor(t)
	m.input.Machine = "Apple silicon · 32 GiB RAM · 40 GiB free"
	m.all = true
	for i := range m.input.Presets {
		p := &m.input.Presets[i]
		p.Description = "A model for local work. Review its output in your own project."
		p.Details = "Model files 3 GiB · memory within limit (prediction)"
		p.AssessmentURL = "https://example.test/models/assessment"
		m.draft.Presets[p.ID] = setup.Preset{Name: p.Name, Lock: p.Lock}
	}
	l := m.draft.Layouts["local"]
	l.Presets = m.presetIDs()
	m.draft.Layouts["local"] = l
	m.plan = setup.Plan{Configuration: &m.draft, CanPrepare: true, Downloads: []setup.Download{
		{Name: "A-long-model-filename-模型.Q4_K_M.gguf", Bytes: 3 << 30, Kind: "model", Cached: true, Cache: "huggingface"},
	}}
	m.reviewReady = true
	return m
}

func TestPresetEditorScreensFitAndKeepNavigationVisible(t *testing.T) {
	for _, size := range []struct{ width, height int }{{28, 10}, {36, 10}, {42, 12}, {48, 24}, {52, 18}, {72, 24}, {80, 12}, {100, 30}, {120, 40}, {20, 6}} {
		for _, screen := range []string{"presets", "layouts", "members", "context", "rename", "review", "downloads", "error"} {
			t.Run(fmt.Sprintf("%dx%d/%s", size.width, size.height, screen), func(t *testing.T) {
				m := populatedPresetEditor(t)
				m.input.Presets[0].Name = "小さなモデル · Compact general assistant"
				m.input.Presets[0].Description = strings.Repeat("Model details and limitations. ", 30)
				m.notice = "A long notice should remain readable by scrolling without hiding the navigation."
				switch screen {
				case "layouts":
					m.page = 1
				case "members":
					m.page, m.layout = 1, "local"
				case "context":
					m.edit("context", "40960")
				case "rename":
					m.page = 1
					m.edit("rename", strings.Repeat("長い名前", 30))
				case "review", "downloads", "error":
					m.page = 2
					m.downloadsOpen = screen == "downloads"
					if screen == "error" {
						m.reviewReady = false
						m.reviewErr = errors.New("preview unavailable; retry later")
					}
				}
				m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
				view := m.View()
				assertFrameFits(t, view.Content, size.width, size.height)
				if size.width < 28 || size.height < 10 {
					return
				}
				text := ansi.Strip(view.Content)
				if !strings.Contains(text, "TEMPER") || !strings.Contains(text, "• ") {
					t.Fatalf("navigation left the frame:\n%s", text)
				}
				if view.MouseMode != tea.MouseModeCellMotion {
					t.Fatal("mouse reporting is disabled")
				}
			})
		}
	}
}

func TestPresetMouseFlowKeepsThreeLayoutChoicesIndependent(t *testing.T) {
	m := presetEditor(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 35})
	clickPresetText(t, m, "[ ]")
	clickPresetText(t, m, "All")
	clickPresetText(t, m, "Recommended")
	if len(m.draft.Presets) != 1 {
		t.Fatal("filter click changed selected presets")
	}
	clickPresetText(t, m, "Next: Layouts")
	clickPresetText(t, m, "│ Local")
	clickPresetText(t, m, "[ ] Included")
	clickPresetText(t, m, "[ ] Default")
	l := m.draft.Layouts["local"]
	if len(l.Presets) != 1 || len(l.Startup) != 0 || l.Default == "" {
		t.Fatalf("default click changed membership or startup: %+v", l)
	}
	clickPresetText(t, m, "[ ] Load on activation")
	clickPresetText(t, m, "[x] Included")
	if err := m.draft.Validate(); err == nil {
		t.Fatal("removing membership silently cleared explicit choices")
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "clear its startup/default") {
		t.Fatalf("invalid choices have no visible correction:\n%s", view)
	}
	clickPresetText(t, m, "[x] Load on activation")
	clickPresetText(t, m, "[x] Default")
	clickPresetText(t, m, "Done →")
	if m.layout != "" || m.page != 1 || m.draft.Validate() != nil {
		t.Fatal("Done did not return to the layout list with corrected choices")
	}
}

func TestPresetFiltersAreReachableWithArrowsAndPreserveSelections(t *testing.T) {
	m := presetEditor(t)
	editorKey(m, ' ')
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if view := ansi.Strip(m.View().Content); m.focus != presetFilterFocus || m.all || !strings.Contains(view, "Recommended") {
		t.Fatalf("Up from the first preset did not focus the filters:\n%s", view)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if view := ansi.Strip(m.View().Content); m.focus != presetFilterFocus || !m.all || !strings.Contains(view, "All") || m.page != 0 || m.Decision.Action != "" {
		t.Fatalf("filter arrows left the preset screen or lost focus:\n%s", view)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	editorKey(m, ' ')
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.all || len(m.draft.Presets) != 2 || len(m.input.Configuration.Presets) != 0 {
		t.Fatal("returning to Recommended discarded or committed selections")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if view := ansi.Strip(m.View().Content); m.focus != presetContentFocus || m.cursor != 0 || !strings.Contains(view, "[x] "+m.input.Presets[0].Name) {
		t.Fatalf("Down did not return focus to the selected preset:\n%s", view)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.page != 1 || len(m.draft.Presets) != 2 {
		t.Fatal("activating Next did not preserve selections")
	}
}

func TestEmptyRecommendedFilterKeepsKeyboardRouteToAll(t *testing.T) {
	m := presetEditor(t)
	for i := range m.input.Presets {
		m.input.Presets[i].Recommended = false
	}
	m.all = true
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "No recommended presets") {
		t.Fatalf("empty Recommended has no explanation:\n%s", view)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if view := ansi.Strip(m.View().Content); m.focus != presetContentFocus || m.cursor != 0 || !strings.Contains(view, "[ ] "+m.input.Presets[0].Name) || len(m.draft.Presets) != 0 || m.page != 0 {
		t.Fatalf("entering All did not focus its first preset without selecting it:\n%s", view)
	}
}

func TestPresetCardsScrollWithoutMovingControlsOrSnappingBack(t *testing.T) {
	m := populatedPresetEditor(t)
	m.input.Presets[0].Description = strings.Repeat("Long model details. ", 100)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 18})
	m.View()
	m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	offset := m.view.YOffset()
	if offset == 0 {
		t.Fatal("details cannot be scrolled")
	}
	view := ansi.Strip(m.View().Content)
	if m.view.YOffset() != offset || !strings.Contains(view, "Recommended") || !strings.Contains(view, "Next: Layouts") {
		t.Fatalf("scroll lost navigation or snapped back:\n%s", view)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	view = ansi.Strip(m.View().Content)
	if !strings.Contains(view, m.input.Presets[1].Name) {
		t.Fatalf("moving focus did not reveal the next preset:\n%s", view)
	}
}

func TestPresetReviewScrollAndDownloadDisclosureKeepActionsVisible(t *testing.T) {
	m := populatedPresetEditor(t)
	m.page = 2
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 28})
	view := ansi.Strip(m.View().Content)
	if strings.Contains(view, m.plan.Downloads[0].Name) || !strings.Contains(view, "Downloads (1 file)") {
		t.Fatal("file details are not initially collapsed")
	}
	clickPresetText(t, m, "▸ Downloads")
	if !strings.Contains(ansi.Strip(m.View().Content), "Cached in Hugging Face") {
		t.Fatal("expanded download table lost the cache status")
	}
	for i := 0; i < 100; i++ {
		m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	}
	view = ansi.Strip(m.View().Content)
	if m.view.YOffset() == 0 || !strings.Contains(view, "Save configuration") || !strings.Contains(view, "• Review") {
		t.Fatalf("review scrolling hid actions:\n%s", view)
	}
	offset := m.view.YOffset()
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.action != 1 || m.view.YOffset() != offset || m.Decision.Action != "" {
		t.Fatal("action navigation changed scroll or confirmed a choice")
	}
	clickPresetText(t, m, "Back")
	if m.page != 1 || len(m.draft.Presets) != 2 {
		t.Fatal("review Back discarded selections")
	}
}

func TestPresetFormsKeepInvalidValueAndAcceptPastedNames(t *testing.T) {
	m := populatedPresetEditor(t)
	editorKey(m, 'c')
	m.text.SetValue("not a number")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.field != "context" || m.text.Value() != "not a number" || m.notice == "" {
		t.Fatal("invalid input dismissed the editor or discarded the value")
	}
	m.text.SetValue("32768")
	clickPresetText(t, m, "Apply")
	if m.field != "" {
		t.Fatal("valid edit did not return to presets")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	editorKey(m, 'n')
	m.Update(tea.PasteMsg{Content: "Writing desk"})
	clickPresetText(t, m, "Apply")
	if m.draft.Layouts["writing-desk"].Name != "Writing desk" {
		t.Fatal("pasted layout name was not accepted")
	}
}

func TestPresetPreviewCannotCommitWhilePendingOrAfterResize(t *testing.T) {
	m := populatedPresetEditor(t)
	m.page, m.loading, m.reviewReady = 2, true, false
	clickPresetText(t, m, "Save configuration")
	if m.Decision.Action != "" {
		t.Fatal("pending preview allowed saving")
	}
	m.loading, m.reviewReady = false, true
	x, y := textPosition(t, m.View().Content, "Prepare installation")
	m.Update(tea.WindowSizeMsg{Width: 20, Height: 6})
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.Decision.Action != "" {
		t.Fatal("resize allowed confirming a hidden action")
	}
	editorKey(m, 'q')
	if m.Decision.Action != "cancel" {
		t.Fatal("resize prompt prevented cancelling")
	}
}

func TestPresetPreviewBackDiscardsStaleResultAndRetries(t *testing.T) {
	m := populatedPresetEditor(t)
	m.page = 1
	m.preview = func(_ context.Context, c setup.Configuration) (setup.Plan, error) {
		return setup.Plan{Configuration: &c, CanPrepare: true}, nil
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	_, first := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	editorKey(m, 'x') // Editing after cancelling must not mutate the preview's snapshot.
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	_, second := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	old := first().(compositionPreview)
	if len(old.Plan.Configuration.Layouts) != 2 {
		t.Fatal("editing changed a pending preview's snapshot")
	}
	m.Update(old)
	if m.reviewReady {
		t.Fatal("cancelled preview replaced the current review")
	}
	m.Update(second())
	clickPresetText(t, m, "Save configuration")
	if m.Decision.Action != "save" || len(m.Decision.Configuration.Layouts) != 1 {
		t.Fatal("current preview did not save the latest choices")
	}
}

func clickPresetText(t *testing.T, m *PresetModel, label string) {
	t.Helper()
	x, y := textPosition(t, m.View().Content, label)
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
}
