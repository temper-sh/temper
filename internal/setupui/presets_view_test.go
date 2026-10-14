package setupui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/setup"
)

func populatedPresetEditor(t *testing.T) *PresetModel {
	t.Helper()
	m := presetEditor(t)
	m.input.Machine = "Apple silicon · 32 GiB RAM · 40 GiB free"
	for i := range m.input.Presets {
		p := &m.input.Presets[i]
		p.Group = "general"
		p.Description = "A model for local work. Review its output in your own project."
		p.ModelBytes = 3 << 30
		p.AssessmentURL = "https://example.test/models/assessment"
		m.draft.Presets[p.ID] = setup.Preset{Name: p.Name, Lock: p.Lock}
	}
	m.advance()
	m.back()
	m.plan = setup.Plan{Configuration: &m.draft, CanPrepare: true, Downloads: []setup.Download{
		{Name: "A-long-model-filename-模型.Q4_K_M.gguf", Bytes: 3 << 30, Kind: "model", Cached: true, Cache: "huggingface"},
	}}
	m.reviewReady = true
	return m
}

func TestCategoriesAreExclusiveAndSpecialistsShowTaskGuidance(t *testing.T) {
	m := presetEditor(t)
	base := m.input.Presets[0]
	m.input.Groups = []catalog.Group{
		{ID: "general", DisplayName: "General", Description: "General presets."},
		{ID: "specialist", DisplayName: "Specialist", Description: "Specialists by task.", Sections: []catalog.Section{
			{ID: "extraction", DisplayName: "Extraction", Description: "Turn source text into structured records."},
			{ID: "reranking", DisplayName: "Reranking", Description: "Order retrieved passages by relevance."},
		}},
		{ID: "other", DisplayName: "Other", Description: "Other presets."},
	}
	m.input.Presets = nil
	for _, choice := range []struct {
		id, specialty, description string
		group                      string
	}{
		{"General choice", "", "A recommended general assistant.", "general"},
		{"English scorer", "reranking", "For English search; lower memory in the measured setup.", "specialist"},
		{"Record extractor", "extraction", "Accepts text and returns structured records.", "specialist"},
		{"Multilingual scorer", "reranking", "Multilingual search; higher cost in the measured setup.", "specialist"},
		{"Niche choice", "", "An additional preset for a narrow use.", "other"},
	} {
		p := base
		p.ID, p.Name, p.Section, p.Description, p.Group = choice.id, choice.id, choice.specialty, choice.description, choice.group
		m.input.Presets = append(m.input.Presets, p)
	}
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 60})
	for _, tc := range []struct {
		key          rune
		want, absent []string
	}{
		{'g', []string{"General choice"}, []string{"English scorer", "Record extractor", "Multilingual scorer", "Niche choice"}},
		{'s', []string{"Extraction", "Turn source text into structured records.", "Record extractor", "Reranking", "Order retrieved passages by relevance.", "English scorer", "Multilingual scorer"}, []string{"General choice", "Niche choice"}},
		{'o', []string{"Niche choice"}, []string{"General choice", "English scorer", "Record extractor", "Multilingual scorer"}},
	} {
		editorKey(m, tc.key)
		view := ansi.Strip(m.View().Content)
		if !strings.Contains(view, " · Apache-2.0") || !strings.Contains(m.View().Content, "https://choosealicense.com/licenses/apache-2.0/") {
			t.Fatalf("tab %c hid the model license or its link:\n%s", tc.key, view)
		}
		for _, want := range tc.want {
			if !strings.Contains(view, want) {
				t.Fatalf("tab %c missing %q:\n%s", tc.key, want, view)
			}
		}
		for _, absent := range tc.absent {
			if strings.Contains(view, absent) {
				t.Fatalf("tab %c contains %q from another category", tc.key, absent)
			}
		}
		editorKey(m, ' ')
	}
	if len(m.draft.Presets) != 3 || len(m.input.Configuration.Presets) != 0 {
		t.Fatal("cross-category choices were lost, preselected or committed")
	}
	editorKey(m, 's')
	view := ansi.Strip(m.View().Content)
	if strings.Count(view, "Order retrieved passages by relevance.") != 1 || strings.Index(view, "Record extractor") > strings.Index(view, "English scorer") || strings.Index(view, "English scorer") > strings.Index(view, "Multilingual scorer") {
		t.Fatalf("specialists were not grouped by task with stable within-task order:\n%s", view)
	}
}

func TestThreeCategoryTabsFitAndCanBeClickedInSmallTerminals(t *testing.T) {
	for _, width := range []int{28, 30, 36, 42, 52, 80} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := presetEditor(t)
			m.Update(tea.WindowSizeMsg{Width: width, Height: 12})
			for _, group := range []struct{ id, label string }{{"specialist", "Specialist"}, {"other", "Other"}, {"general", "General"}} {
				assertFrameFits(t, m.View().Content, width, 12)
				clickPresetText(t, m, group.label)
				if m.group != group.id || m.page != 0 || len(m.draft.Presets) != 0 {
					t.Fatalf("click did not switch to %s without selecting a preset", group.id)
				}
			}
		})
	}
}

func TestSavedLicenseStaysLinkedInSelectionAndResponsiveReview(t *testing.T) {
	for _, tc := range []struct {
		name     string
		width    int
		assigned bool
	}{{"wide layout", 120, true}, {"unassigned", 80, false}, {"narrow layout", 48, true}, {"wrapped license", 28, true}} {
		t.Run(tc.name, func(t *testing.T) {
			m := presetEditor(t)
			m.input.Presets[0].Description = "Local work."
			choice := m.input.Presets[0]
			id, preset := presetRecord(choice.Lock)
			d := choice.Lock.Records
			a := d.Artifacts[preset.Artifact]
			a.License = "CC-BY-NC-4.0"
			d.Artifacts[preset.Artifact] = a
			lock, err := catalog.CompilePreset(d, id, 0, 0, choice.Lock.Target)
			if err != nil {
				t.Fatal(err)
			}
			m.draft.Presets[choice.ID] = setup.Preset{Name: choice.Name, Lock: lock}
			if tc.assigned {
				m.draft.Layouts["work"] = setup.Layout{Name: "Work", Presets: []string{choice.ID}, IdleSeconds: 60}
			}
			m.plan = setup.Plan{Configuration: &m.draft, CanPrepare: true}
			m.reviewReady = true
			m.Update(tea.WindowSizeMsg{Width: tc.width, Height: 60})

			for _, screen := range []struct {
				page  int
				focus presetFocus
				style uv.Style
			}{
				{0, presetContentFocus, uv.Style{Fg: colorLink, Bg: colorSurface, Underline: uv.UnderlineSingle}},
				{0, presetFilterFocus, uv.Style{Fg: colorLink, Bg: colorBackground, Underline: uv.UnderlineSingle}},
				{2, presetContentFocus, uv.Style{Fg: colorLink, Bg: colorSurface, Underline: uv.UnderlineSingle, Attrs: uv.AttrBold}},
			} {
				m.page, m.focus = screen.page, screen.focus
				view := m.View().Content
				assertFrameFits(t, view, tc.width, 60)
				buffer := uv.NewScreenBuffer(tc.width, 60)
				uv.NewStyledString(view).Draw(buffer, buffer.Bounds())
				var linkedText strings.Builder
				for _, line := range buffer.Lines {
					for _, cell := range line {
						if cell.Link.URL != "https://creativecommons.org/licenses/by-nc/4.0/" {
							continue
						}
						linkedText.WriteString(cell.Content)
						if !cell.Style.Equal(&screen.style) {
							t.Fatalf("page %d focus %d: license character %q has style %+v, want %+v", screen.page, screen.focus, cell.Content, cell.Style, screen.style)
						}
					}
				}
				if linkedText.String() != "CC-BY-NC-4.0" {
					t.Fatalf("page %d hid or replaced the saved license link: %q", screen.page, linkedText.String())
				}
			}
		})
	}
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
					m.page, m.layout = 1, "default"
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
	clickPresetText(t, m, "Specialist")
	clickPresetText(t, m, "Other")
	clickPresetText(t, m, "General")
	if len(m.draft.Presets) != 1 {
		t.Fatal("filter click changed selected presets")
	}
	clickPresetText(t, m, "Next: Layouts")
	clickPresetText(t, m, "│ Default")
	clickPresetText(t, m, "[ ] Default")
	l := m.draft.Layouts["default"]
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
	if view := ansi.Strip(m.View().Content); m.focus != presetFilterFocus || m.group != "general" || !strings.Contains(view, "General") {
		t.Fatalf("Up from the first preset did not focus the filters:\n%s", view)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft}) // Stay at General.
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.group != "specialist" {
		t.Fatal("Right did not reach Specialist")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if view := ansi.Strip(m.View().Content); m.focus != presetFilterFocus || m.group != "other" || m.page != 0 || m.Decision.Action != "" {
		t.Fatalf("filter arrows left the preset screen or lost focus:\n%s", view)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	editorKey(m, ' ')
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.group != "general" || len(m.draft.Presets) != 2 || len(m.input.Configuration.Presets) != 0 {
		t.Fatal("returning to General discarded or committed selections")
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

func TestEmptyCategoriesKeepKeyboardRouteToOther(t *testing.T) {
	input := presetEditor(t).input
	for i := range input.Presets {
		input.Presets[i].Group = "other"
	}
	m := NewPresetModel(context.Background(), input, nil)
	if view := ansi.Strip(m.View().Content); m.group != "general" || !strings.Contains(view, "No general presets") {
		t.Fatalf("empty General has no explanation:\n%s", view)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "No specialist presets") {
		t.Fatalf("empty Specialist has no explanation:\n%s", view)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if view := ansi.Strip(m.View().Content); m.focus != presetContentFocus || m.cursor != 0 || !strings.Contains(view, "[ ] "+m.input.Presets[0].Name) || len(m.draft.Presets) != 0 || m.page != 0 {
		t.Fatalf("entering Other did not focus its first preset without selecting it:\n%s", view)
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
	if m.view.YOffset() != offset || !strings.Contains(view, "General") || !strings.Contains(view, "Next: Layouts") {
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
	if m.view.YOffset() == 0 || !strings.Contains(view, "Save without installing") || !strings.Contains(view, "• Review") {
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
	clickPresetText(t, m, "Save without installing")
	if m.Decision.Action != "" {
		t.Fatal("pending preview allowed saving")
	}
	m.loading, m.reviewReady = false, true
	x, y := textPosition(t, m.View().Content, "Save without installing")
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
	if len(old.Plan.Configuration.Layouts) != 1 {
		t.Fatal("editing changed a pending preview's snapshot")
	}
	m.Update(old)
	if m.reviewReady {
		t.Fatal("cancelled preview replaced the current review")
	}
	m.Update(second())
	clickPresetText(t, m, "Save without installing")
	if m.Decision.Action != "save" || len(m.Decision.Configuration.Layouts) != 0 {
		t.Fatal("current preview did not save the latest choices")
	}
}

func clickPresetText(t *testing.T, m *PresetModel, label string) {
	t.Helper()
	x, y := textPosition(t, m.View().Content, label)
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
}
