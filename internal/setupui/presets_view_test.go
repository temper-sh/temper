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

func TestCategoryTabsAndOverflowRemainClickableInSmallTerminals(t *testing.T) {
	for _, width := range []int{28, 30, 36, 42, 52, 80} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := presetEditor(t)
			m.Update(tea.WindowSizeMsg{Width: width, Height: 12})
			for _, group := range []struct{ id, label string }{{"specialist", "Specialist"}, {"other", "Other"}, {"general", "General"}} {
				for attempts := 0; attempts < len(m.groups()); attempts++ {
					view := ansi.Strip(m.View().Content)
					assertFrameFits(t, view, width, 12)
					if strings.Contains(view, group.label) {
						break
					}
					arrow := "›"
					if group.id == "general" {
						arrow = "‹"
					}
					clickPresetText(t, m, arrow)
				}
				clickPresetText(t, m, group.label)
				if m.group != group.id || m.page != 0 || len(m.draft.Presets) != 0 {
					t.Fatalf("click did not switch to %s without selecting a preset", group.id)
				}
			}
		})
	}
}

func TestSavedLicenseStaysLinkedOnPresetCards(t *testing.T) {
	for _, tc := range []struct {
		name  string
		width int
	}{{"wide", 120}, {"medium", 80}, {"narrow", 48}, {"wrapped license", 28}} {
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
			m.Update(tea.WindowSizeMsg{Width: tc.width, Height: 60})

			for _, screen := range []struct {
				page  int
				focus presetFocus
				style uv.Style
			}{
				{0, presetContentFocus, uv.Style{Fg: presetColors.Link, Bg: presetColors.Surface, Underline: uv.UnderlineSingle}},
				{0, presetFilterFocus, uv.Style{Fg: presetColors.Link, Bg: colorBackground, Underline: uv.UnderlineSingle}},
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
		for _, screen := range []string{"presets", "steps", "filters", "harnesses", "context", "review", "error"} {
			t.Run(fmt.Sprintf("%dx%d/%s", size.width, size.height, screen), func(t *testing.T) {
				m := populatedPresetEditor(t)
				m.input.Presets[0].Name = "小さなモデル · Compact general assistant"
				m.input.Presets[0].Description = strings.Repeat("Model details and limitations. ", 30)
				m.notice = "A long notice should remain readable by scrolling without hiding the navigation."
				switch screen {
				case "harnesses":
					m.page = presetHarnessPage
				case "steps":
					m.focus = presetStepsFocus
				case "filters":
					m.focus = presetFilterFocus
				case "context":
					m.activateShortcut("c")
					wizardType(m, "40960")
				case "review", "error":
					m.page = presetReviewPage
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
				if !strings.Contains(text, "TEMPER") || !strings.Contains(text, "❯") {
					t.Fatalf("navigation left the frame:\n%s", text)
				}
				if view.MouseMode != tea.MouseModeCellMotion {
					t.Fatal("mouse reporting is disabled")
				}
			})
		}
	}
}

func TestPresetMouseNavigationUsesTheSameGuardedThreeStepFlow(t *testing.T) {
	m := presetEditor(t)
	m.preview = func(_ context.Context, c setup.Configuration) (setup.Plan, error) {
		return setup.Plan{Configuration: &c, CanPrepare: true}, nil
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 35})
	clickPresetText(t, m, "Review")
	if m.page != 0 || m.notice == "" {
		t.Fatal("primary navigation skipped Harnesses")
	}
	clickPresetText(t, m, "[ ]")
	clickPresetText(t, m, "Other")
	clickPresetText(t, m, "General")
	if len(m.draft.Presets) != 1 {
		t.Fatal("filter click changed selected presets")
	}
	clickPresetText(t, m, "Harnesses")
	layout := m.draft.Layouts["default"]
	if m.page != presetHarnessPage || len(layout.Presets) != 1 || len(layout.Startup) != 0 || layout.Default != "" {
		t.Fatalf("primary navigation did not create an on-demand Default: %+v", layout)
	}
	preview := clickPresetText(t, m, "Review")
	if preview == nil || !m.loading || m.Decision.Action != "" {
		t.Fatal("Review did not start a revocable preview")
	}
	m.Update(preview())
	clickPresetText(t, m, "Presets")
	if m.page != 0 || len(m.draft.Presets) != 1 || m.Decision.Action != "" {
		t.Fatal("primary back navigation changed the selection")
	}
	editorKey(m, 'c')
	wizardType(m, "bad")
	clickPresetText(t, m, "Harnesses")
	if m.page != 0 {
		t.Fatal("primary navigation bypassed the context prompt")
	}
	clickPresetText(t, m, "Cancel")
	if m.field != "" || m.Decision.Action != "" {
		t.Fatal("Cancel did not leave only the context prompt")
	}
	clickPresetText(t, m, "Cancel")
	if m.Decision.Action != "cancel" || len(m.input.Configuration.Layouts) != 0 {
		t.Fatal("cancel committed changes")
	}

}

func TestPresetFiltersAreReachableWithArrowsAndPreserveSelections(t *testing.T) {
	m := presetEditor(t)
	editorKey(m, ' ')
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if view := ansi.Strip(m.View().Content); m.focus != presetFilterFocus || m.group != "general" || !strings.Contains(view, "General") {
		t.Fatalf("Up from the first preset did not focus the filters:\n%s", view)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft}) // Secondary choices wrap, like buttons.
	if m.group != "other" {
		t.Fatal("Left did not wrap to Other")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight}) // General.
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
	if m.view.YOffset() != offset || !strings.Contains(view, "General") || !strings.Contains(view, "Next") {
		t.Fatalf("scroll lost navigation or snapped back:\n%s", view)
	}
	for m.cursor == 0 {
		m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		m.View()
	}
	view = ansi.Strip(m.View().Content)
	if !strings.Contains(view, m.input.Presets[1].Name) {
		t.Fatalf("moving focus did not reveal the next preset:\n%s", view)
	}
}

func TestPresetReviewShowsDownloadsAndKeepsActionsVisibleWhileScrolling(t *testing.T) {
	m := populatedPresetEditor(t)
	m.page = presetReviewPage
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Downloads · 0 B") || !strings.Contains(view, "On install") || !strings.Contains(view, "Cached in Hugging Face") {
		t.Fatalf("file details and cache status are not visible on arrival:\n%s", view)
	}
	for i := 0; i < 100; i++ {
		m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	}
	view = ansi.Strip(m.View().Content)
	if m.view.YOffset() == 0 || !strings.Contains(view, "Save without installing") || !strings.Contains(view, "Review") {
		t.Fatalf("review scrolling hid actions:\n%s", view)
	}
	offset := m.view.YOffset()
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.action != 1 || m.view.YOffset() != offset || m.Decision.Action != "" {
		t.Fatal("action navigation changed scroll or confirmed a choice")
	}
	clickPresetText(t, m, "Back")
	if m.page != presetHarnessPage || len(m.draft.Presets) != 2 {
		t.Fatal("review Back discarded selections")
	}
}

func TestPresetContextKeepsInvalidValueAndAcceptsPastedTokens(t *testing.T) {
	m := populatedPresetEditor(t)
	editorKey(m, 'c')
	wizardType(m, "not a number")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	p, _ := m.selectedOption()
	for _, width := range []int{40, 112, 28} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		assertFrameFits(t, m.View().Content, width, 24)
		if m.field != "context" || m.notice == "" || m.text.Value() != "not a number" {
			t.Fatal("resize lost invalid text")
		}
	}
	wizardType(m, "32768")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, record := presetRecord(m.draft.Presets[p.ID].Lock)
	if record.Window() != 32768 || m.field != "" || m.Decision.Action != "" {
		t.Fatal("paste did not update the draft")
	}
}

func TestPresetPreviewCannotCommitWhilePendingOrAfterResize(t *testing.T) {
	m := populatedPresetEditor(t)
	m.page, m.loading, m.reviewReady = presetReviewPage, true, false
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
	m.page = presetHarnessPage
	m.preview = func(_ context.Context, c setup.Configuration) (setup.Plan, error) {
		return setup.Plan{Configuration: &c, CanPrepare: true}, nil
	}
	first := m.advance()
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc}) // Back through Harnesses to Presets.
	editorKey(m, ' ')                           // Editing must not mutate the preview's snapshot.
	m.advance()                                 // Harnesses.
	second := m.advance()
	old := first().(compositionPreview)
	if len(old.Plan.Configuration.Layouts["default"].Presets) != 2 {
		t.Fatal("editing changed a pending preview's snapshot")
	}
	m.Update(old)
	if m.reviewReady {
		t.Fatal("cancelled preview replaced the current review")
	}
	m.Update(second())
	clickPresetText(t, m, "Save without installing")
	if m.Decision.Action != "save" || len(m.Decision.Configuration.Layouts["default"].Presets) != 1 {
		t.Fatal("current preview did not save the latest choices")
	}
}

func TestWizardFooterKeepsFocusedActionsVisibleAndClickableAfterResize(t *testing.T) {
	for _, screen := range []string{"presets", "harnesses", "review", "error", "context"} {
		for _, size := range [][2]int{{28, 10}, {40, 24}, {80, 32}, {112, 24}} {
			t.Run(fmt.Sprintf("%s/%dx%d", screen, size[0], size[1]), func(t *testing.T) {
				m := populatedPresetEditor(t)
				switch screen {
				case "harnesses":
					m.page = presetHarnessPage
				case "review", "error":
					m.page = presetReviewPage
					if screen == "error" {
						m.reviewErr = errors.New("offline")
						m.reviewReady = false
					}
				case "context":
					m.activateShortcut("c")
					wizardType(m, "40960")
				}
				m.notice = "Used by Writing desk. Change membership with temper layout before removing this preset."
				m.focus = presetFooterFocus
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				for i, button := range m.footerButtons() {
					m.action = i
					view := m.View().Content
					label := button.label
					if size[0] < 74 {
						label = button.short
					}
					plain := ansi.Strip(view)
					at := strings.LastIndex(plain, label)
					if at < 0 {
						t.Fatalf("focused action %q is hidden:\n%s", label, plain)
					}
					prefix := strings.Split(plain[:at], "\n")
					x, y := ansi.StringWidth(prefix[len(prefix)-1]), len(prefix)-1
					matched := false
					for _, target := range m.hitTargets {
						if target.contains(x, y) && target.kind == hitPresetAction && target.index == i {
							matched = true
						}
					}
					if !matched {
						t.Fatalf("visible button %q has no matching action at %d,%d", label, x, y)
					}
				}
			})
		}
	}
}

func TestWizardReviewExplainsInitialAndExistingLayoutAvailability(t *testing.T) {
	for _, fresh := range []bool{true, false} {
		m := populatedPresetEditor(t)
		m.input.NewConfiguration = fresh
		m.page = presetReviewPage
		if !fresh {
			layout := m.draft.Layouts["default"]
			layout.Presets = layout.Presets[:1]
			m.draft.Layouts["default"] = layout
		}
		body := m.presetContent(76)
		text := strings.Join(strings.Fields(ansi.Strip(strings.Join(body.blocks, "\n"))), " ")
		if fresh && (!strings.Contains(text, "Default layout") || !strings.Contains(text, "load on demand")) {
			t.Fatalf("first setup does not explain on-demand behavior:\n%s", text)
		}
		if !fresh && (!strings.Contains(text, "kept unchanged") || !strings.Contains(text, "Outside layouts") || !strings.Contains(text, "temper layout")) {
			t.Fatalf("rerun does not explain preserved layouts or unassigned presets:\n%s", text)
		}
	}
}

func clickPresetText(t *testing.T, m *PresetModel, label string) tea.Cmd {
	t.Helper()
	x, y := textPosition(t, m.View().Content, label)
	_, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	return cmd
}
