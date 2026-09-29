package setupui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestTabsFollowEachModeAndPreviousScreenPreservesChoices(t *testing.T) {
	m := NewModel(context.Background(), setupInput(), readyPreview)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	press(m, tea.KeyEnter) // Local
	press(m, tea.KeyDown)
	press(m, tea.KeyEnter) // Utility
	press(m, tea.KeyRight)
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "• Local") || !strings.Contains(view, "Utility") || !strings.Contains(view, "2 / 5") {
		t.Fatalf("selected modes do not have separate tabs: %s", view)
	}
	press(m, tea.KeyRight)
	if m.stage != stageProfile || m.profiles["local"] != "" {
		t.Fatal("tab navigation implicitly chose a model")
	}
	press(m, tea.KeyEnter)
	press(m, tea.KeyTab)
	press(m, tea.KeyDown)
	press(m, tea.KeyEnter) // Sharp
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.stage != stageProfile || m.profiles["local"] != "compact" || m.patches["local"]["compact"]["small"] != "sharp" {
		t.Fatalf("Shift+Tab did not preserve the preceding screen: %+v", m.choices())
	}
	press(m, tea.KeyTab)
	press(m, tea.KeyTab) // Utility profile
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "• Utility") || !strings.Contains(view, "4 / 6") {
		t.Fatalf("utility screen has no distinct tab: %s", view)
	}
	press(m, tea.KeyLeft)
	if m.stage != stageTemplates || m.currentMode() != "local" {
		t.Fatal("Left did not return to the preceding mode's template screen")
	}
}

func TestEveryScreenFitsTerminalWithWrappedContent(t *testing.T) {
	for _, size := range []struct{ width, height int }{{28, 10}, {36, 10}, {42, 12}, {28, 24}, {36, 24}, {80, 12}, {60, 18}, {80, 24}, {120, 36}, {20, 6}} {
		for _, screen := range []stage{stageModes, stageProfile, stageTemplates, stageSoftware, stageReview} {
			t.Run(fmt.Sprintf("%dx%d/screen%d", size.width, size.height, screen), func(t *testing.T) {
				input := setupInput()
				input.Profiles[0].Name = "小さなモデル · Compact general assistant"
				input.Profiles[0].Description = strings.Repeat("A long description of the chosen model and its limits. ", 8)
				input.Profiles[0].AssessmentURL = "https://example.test/assessments/model-with-a-long-readable-name"
				input.Profiles[0].Details = "Model files 3 GiB; memory wall within limit (prediction)."
				input.Profiles[0].Advice = &Section{Title: "Memory budget is tight", Warning: true, Lines: []string{
					"Increase the wired-memory limit before running this setup.", "[manual] sudo sysctl iogpu.wired_limit_mb=26624",
				}}
				m := NewModel(context.Background(), input, readyPreview)
				m.selected["local"], m.selected["utility"] = true, true
				m.profiles["local"], m.profiles["utility"] = "compact", "specialists"
				m.software = "recorded"
				m.review, _ = readyPreview(context.Background(), m.choices())
				m.review.Sections[0].Downloads = []Download{{File: "A-long-model-filename-模型.Q4_K_M.gguf", Size: "3.25 GiB", Status: "Download on Prepare"}}
				m.review.Sections = append(m.review.Sections, *input.Profiles[0].Advice)
				m.stage = screen
				m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
				view := m.View().Content
				assertFrameFits(t, view, size.width, size.height)
				if size.width >= 28 && size.height >= 10 {
					if !strings.Contains(ansi.Strip(view), "• ") || !strings.Contains(ansi.Strip(view), "TEMPER") {
						t.Fatalf("navigation left the frame: %s", ansi.Strip(view))
					}
					if screen == stageReview {
						press(m, 'd')
						assertFrameFits(t, m.View().Content, size.width, size.height)
					}
				}
			})
		}
	}
}

func TestMemoryInstructionsLeadReviewAndRemainVisibleWhenDownloadsCollapse(t *testing.T) {
	m := NewModel(context.Background(), setupInput(), readyPreview)
	m.stage = stageReview
	m.review = Review{Token: "reviewed", CanPrepare: true, Sections: []Section{
		{Title: "Downloads", Lines: []string{"Weights are cached."}, Downloads: []Download{{File: "model.gguf", Size: "16.35 GiB", Status: "Cached in Hugging Face"}}},
		{Title: "Memory budget is tight", Warning: true, Lines: []string{"Increase the wired-memory limit to 26 GiB.", "[manual] sudo sysctl iogpu.wired_limit_mb=26624"}},
	}}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	for _, expanded := range []bool{false, true} {
		m.downloadsOpen = expanded
		view := ansi.Strip(m.View().Content)
		warning, downloads := strings.Index(view, "Memory budget is tight"), strings.Index(view, "Downloads")
		if warning < 0 || downloads <= warning || !strings.Contains(view, "sudo sysctl iogpu.wired_limit_mb=26624") {
			t.Fatalf("memory action buried by download disclosure: %s", view)
		}
		if strings.Count(view, "Memory budget is tight") != 1 {
			t.Fatal("memory warning duplicated in review")
		}
		assertFrameFits(t, m.View().Content, 100, 30)
	}
	press(m, tea.KeyRight)
	press(m, tea.KeyEnter)
	if m.decision.Action != "prepare" {
		t.Fatal("advisory prevented otherwise eligible preparation")
	}
}

func TestModelChoiceShowsMemoryRecommendationBeforeSelection(t *testing.T) {
	input := setupInput()
	input.Profiles[0].Advice = &Section{Title: "Memory budget is tight", Warning: true, Lines: []string{
		"Increase the wired-memory limit to 26 GiB.", "[manual] sudo sysctl iogpu.wired_limit_mb=26624",
	}}
	m := NewModel(context.Background(), input, readyPreview)
	m.selected["local"] = true
	m.stage = stageProfile
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Increase the wired-memory limit") || !strings.Contains(view, "sudo sysctl iogpu.wired_limit_mb=26624") || m.profiles["local"] != "" {
		t.Fatalf("model memory recommendation hidden or implicitly selected: %s", view)
	}
	press(m, tea.KeyEnter)
	if m.profiles["local"] != "compact" {
		t.Fatal("soft memory advice prevented explicit selection")
	}
}

func TestTooSmallTerminalCannotConfirmHiddenAction(t *testing.T) {
	for _, cursor := range []int{0, 1} {
		t.Run(fmt.Sprintf("action%d", cursor), func(t *testing.T) {
			m := NewModel(context.Background(), setupInput(), readyPreview)
			m.stage, m.cursor = stageReview, cursor
			m.review, _ = readyPreview(context.Background(), m.choices())
			m.Update(tea.WindowSizeMsg{Width: 20, Height: 6})
			if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Enlarge terminal") {
				t.Fatalf("missing resize prompt: %s", view)
			}
			if cmd := press(m, tea.KeyEnter); cmd != nil || m.decision.Action != "" {
				t.Fatalf("confirmed a hidden action: %+v", m.decision)
			}
			if cmd := press(m, 'q'); cmd == nil || m.decision.Action != "cancel" {
				t.Fatal("resize prompt prevented cancellation")
			}
		})
	}
}

func TestNoticesAndFailedPreviewFitSmallTerminal(t *testing.T) {
	for _, screen := range []stage{stageModes, stageSoftware, stageReview} {
		t.Run(fmt.Sprintf("screen%d", screen), func(t *testing.T) {
			m := NewModel(context.Background(), setupInput(), readyPreview)
			m.selected["local"], m.profiles["local"] = true, "compact"
			m.stage = screen
			m.reviewErr = errors.New("upstream unavailable; retry later")
			m.Update(tea.WindowSizeMsg{Width: 28, Height: 10})
			press(m, tea.KeyTab)
			press(m, tea.KeyEnter)
			view := m.View().Content
			assertFrameFits(t, view, 28, 10)
			if m.decision.Action != "" {
				t.Fatalf("failed preview or unchosen software accepted: %+v", m.decision)
			}
		})
	}
}

func TestSelectionPageScrollKeepsHeaderAndDoesNotSnapBack(t *testing.T) {
	input := setupInput()
	input.Profiles[0].Description = strings.Repeat("Model details that can be read by scrolling. ", 30)
	m := NewModel(context.Background(), input, readyPreview)
	m.selected["local"] = true
	m.stage = stageProfile
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 18})
	m.View()
	press(m, tea.KeyPgDown)
	offset := m.viewport.YOffset()
	if offset == 0 {
		t.Fatal("long model details cannot be scrolled")
	}
	view := ansi.Strip(m.View().Content)
	if m.viewport.YOffset() != offset || !strings.Contains(view, "• Local") || !strings.Contains(view, "Next →") {
		t.Fatalf("scroll moved the navigation or snapped back: %s", view)
	}
	press(m, tea.KeyDown) // Next button
	press(m, tea.KeyDown) // Back to the only profile
	if view := ansi.Strip(m.View().Content); m.cursor != 0 || !strings.Contains(view, "[ ] ( ) Compact general assistant") {
		t.Fatalf("moving focus did not reveal the chosen row: %s", view)
	}
}

func TestReviewScrollsWithArrowsAndTrackpadWithoutChangingAction(t *testing.T) {
	m := NewModel(context.Background(), setupInput(), readyPreview)
	m.stage = stageReview
	m.review = Review{Token: "accepted", CanPrepare: true, Sections: []Section{{Title: "Configuration", Lines: []string{
		strings.Repeat("A review detail\n", 50) + "Last review detail",
	}}}}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 18})
	view := m.View()
	if view.MouseMode != tea.MouseModeCellMotion {
		t.Fatal("terminal mouse reporting is not enabled")
	}
	press(m, tea.KeyDown)
	if m.viewport.YOffset() == 0 || m.cursor != 0 || m.decision.Action != "" {
		t.Fatal("Down should scroll without choosing an action")
	}
	press(m, tea.KeyUp)
	if m.viewport.YOffset() != 0 {
		t.Fatal("Up did not scroll back")
	}
	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if m.viewport.YOffset() == 0 || m.cursor != 0 {
		t.Fatal("trackpad did not scroll independently of the action")
	}
	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if m.viewport.YOffset() != 0 {
		t.Fatal("trackpad could not return to the top")
	}
	for i := 0; i < 100; i++ {
		press(m, tea.KeyDown)
	}
	last := ansi.Strip(m.View().Content)
	if m.cursor != 0 || !strings.Contains(last, "Last review detail") || !strings.Contains(last, "Save configuration") || !strings.Contains(last, "• Review") {
		t.Fatalf("ordinary keys cannot reach the end with controls visible: %s", last)
	}
	offset := m.viewport.YOffset()
	press(m, tea.KeyRight)
	press(m, tea.KeyLeft)
	press(m, tea.KeyTab)
	if m.cursor != 1 || m.viewport.YOffset() != offset || m.decision.Action != "" {
		t.Fatal("action selection changed review scroll or committed without Enter")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.stage != stageReview || m.cursor != 0 {
		t.Fatal("Shift+Tab should select the previous review action")
	}
	press(m, tea.KeySpace)
	if m.decision.Action != "" {
		t.Fatal("Space committed a review action")
	}
}

func TestMouseNextAndChoicesUseVisiblePositions(t *testing.T) {
	m := NewModel(context.Background(), setupInput(), readyPreview)
	clickText(t, m, "Next →")
	if m.stage != stageModes || len(m.chosenModes()) != 0 {
		t.Fatal("mouse Next chose a mode implicitly")
	}
	clickText(t, m, "[ ] Local")
	clickText(t, m, "Next →")
	if m.stage != stageProfile || m.currentMode() != "local" {
		t.Fatal("mouse choice and Next did not advance")
	}
	// A short viewport scrolls to the option, changing its on-screen position.
	m.Update(tea.WindowSizeMsg{Width: 36, Height: 10})
	clickText(t, m, "Compact general")
	clickText(t, m, "Next →")
	if m.stage != stageTemplates || m.profiles["local"] != "compact" {
		t.Fatal("mouse targets did not follow resize and scrolling")
	}
}

func TestMouseReviewRequiresSuccessfulPreviewAndPreparationEligibility(t *testing.T) {
	for _, tc := range []struct {
		name, button, want string
		review             Review
		loading            bool
	}{
		{name: "loading", button: "Save configuration", loading: true},
		{name: "missing preview", button: "Save configuration"},
		{name: "cannot prepare", button: "Prepare installation", review: Review{Token: "accepted"}},
		{name: "save", button: "Save configuration", review: Review{Token: "accepted"}, want: "save"},
		{name: "prepare", button: "Prepare installation", review: Review{Token: "accepted", CanPrepare: true}, want: "prepare"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewModel(context.Background(), setupInput(), readyPreview)
			m.stage, m.review, m.loading = stageReview, tc.review, tc.loading
			m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
			clickText(t, m, tc.button)
			if m.decision.Action != tc.want {
				t.Fatalf("mouse decision = %+v, want %q", m.decision, tc.want)
			}
			if tc.want != "" && m.decision.ReviewToken != "accepted" {
				t.Fatal("mouse action did not retain the reviewed plan identity")
			}
		})
	}
}

func TestMouseCannotCommitAfterTerminalBecomesTooSmall(t *testing.T) {
	m := NewModel(context.Background(), setupInput(), readyPreview)
	m.stage, m.review = stageReview, Review{Token: "accepted", CanPrepare: true}
	x, y := textPosition(t, m.View().Content, "Save configuration")
	m.Update(tea.WindowSizeMsg{Width: 20, Height: 6})
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if m.decision.Action != "" {
		t.Fatal("stale button position confirmed a hidden action")
	}
}

func TestDownloadsCollapseKeepsTransferDisclosure(t *testing.T) {
	m := NewModel(context.Background(), setupInput(), readyPreview)
	m.stage = stageReview
	m.review = Review{Token: "accepted", CanPrepare: true, Sections: []Section{
		{Title: "Configuration", Lines: []string{strings.Repeat("Configuration detail\n", 30)}},
		{Title: "Downloads", Lines: []string{
			"Weights: all cached in Temper (3.00 GiB). No weight download on Prepare.",
			"Save downloads no weights. Prepare verifies cached weights.",
			"Temper cache checked: /tmp/temper/artifacts/layouts",
		}, Downloads: []Download{
			{File: "model.gguf", Size: "3.00 GiB", Status: "Cached in Temper"},
			{File: "local/llama-cpp b123", Size: "33.00 MiB", Status: "May download"},
		}},
	}}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	assertSummary := func() string {
		t.Helper()
		view := ansi.Strip(m.View().Content)
		for _, want := range []string{"No weight download on Prepare", "Save downloads no weights", "Temper cache checked:"} {
			if !strings.Contains(view, want) {
				t.Fatalf("transfer disclosure lost: %q in\n%s", want, view)
			}
		}
		return view
	}
	if view := assertSummary(); strings.Contains(view, "model.gguf") || !strings.Contains(view, "d expand") {
		t.Fatalf("file table was not initially collapsed: %s", view)
	}
	press(m, tea.KeyEnd) // Expanding from elsewhere should reveal the section.
	press(m, 'd')
	if view := assertSummary(); !strings.Contains(view, "model.gguf") || !strings.Contains(view, "Cached in Temper") || !strings.Contains(view, "On Prepare") {
		t.Fatalf("expanded table missing: %s", view)
	}
	clickText(t, m, "▾ Downloads")
	if view := assertSummary(); strings.Contains(view, "model.gguf") || !strings.Contains(view, "d expand") {
		t.Fatalf("click did not collapse the table: %s", view)
	}
	clickText(t, m, "▸ Downloads")
	if view := assertSummary(); !strings.Contains(view, "model.gguf") || m.decision.Action != "" {
		t.Fatalf("click did not expand safely: %s", view)
	}
}

func TestDownloadTableFitsAndPreservesFullFileNames(t *testing.T) {
	item := Download{File: "模型-Qwen-with-a-very-long-name-Q4_K_M-00001-of-00002.gguf", Size: "3.25 GiB", Status: "Cached in Hugging Face"}
	for width := 22; width <= 104; width++ {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			view := ansi.Strip(downloadTable([]Download{item}, width))
			var file, action strings.Builder
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > width {
					t.Fatalf("table exceeds %d columns: %s", width, view)
				}
				cells := strings.Split(line, "│")
				if len(cells) > 2 {
					file.WriteString(strings.TrimSpace(cells[1]))
					action.WriteString(strings.TrimSpace(cells[len(cells)-2]))
				}
			}
			if !strings.Contains(file.String(), item.File) || !strings.Contains(strings.ReplaceAll(action.String(), " ", ""), "CachedinHuggingFace") {
				t.Fatalf("table truncated file or status: %s", view)
			}
		})
	}
}

func TestDownloadsCardDoesNotRewrapTableBorders(t *testing.T) {
	items := []Download{
		{File: "Qwen3.5-4B-Q4_K_M.gguf", Size: "2.55 GiB", Status: "Download on Prepare"},
		{File: "local/llama-cpp b10964", Size: "10.63 MiB", Status: "May download"},
	}
	for _, width := range []int{26, 40, 76, 96, 108} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := Model{downloadsOpen: true}
			block, _ := m.downloadsBlock(Section{Downloads: items}, width)
			frame := ansi.Strip(block)
			for _, line := range strings.Split(ansi.Strip(downloadTable(items, width-4)), "\n") {
				if !strings.Contains(frame, "│ "+line+" │") {
					t.Fatalf("table rewrapped inside its card:\n%s", frame)
				}
			}
		})
	}
}

func clickText(t *testing.T, m *Model, label string) {
	t.Helper()
	x, y := textPosition(t, m.View().Content, label)
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
}

func textPosition(t *testing.T, frame, label string) (int, int) {
	t.Helper()
	for y, line := range strings.Split(ansi.Strip(frame), "\n") {
		if x := strings.Index(line, label); x >= 0 {
			return ansi.StringWidth(line[:x]), y
		}
	}
	t.Fatalf("label %q is not visible:\n%s", label, ansi.Strip(frame))
	return 0, 0
}

func assertFrameFits(t *testing.T, frame string, width, height int) {
	t.Helper()
	lines := strings.Split(frame, "\n")
	if len(lines) > height {
		t.Fatalf("frame is %d lines in a %d-line terminal:\n%s", len(lines), height, ansi.Strip(frame))
	}
	for _, line := range lines {
		if got := ansi.StringWidth(line); got > width {
			t.Fatalf("line is %d cells in a %d-cell terminal: %q", got, width, ansi.Strip(line))
		}
	}
}
