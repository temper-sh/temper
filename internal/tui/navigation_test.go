package tui_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/tui"
)

func TestSecondaryKeepsEverySelectedSectionVisibleAtTerminalWidths(t *testing.T) {
	labels := []string{"General", "Sampling", "Engine", "Speculation", "Review"}
	for _, width := range []int{38, 76, 108} {
		for current, name := range labels {
			row := ansi.Strip(tui.Secondary(tui.Presets, labels, current, width, true))
			if strings.Contains(row, "\n") || ansi.StringWidth(row) > width || !strings.Contains(row, "["+name+"]") {
				t.Errorf("section %s at %d columns is not visible on one row: %q", name, width, row)
			}
		}
	}
}

func TestBreadcrumbsContainUserNamesWithinTheFrame(t *testing.T) {
	path := []string{"Presets", "長い名前 " + strings.Repeat("writing ", 12), "Weights", "Files"}
	for _, width := range []int{26, 38, 76, 108} {
		frame := ansi.Strip(tui.Breadcrumbs(path, 0, width))
		rows := strings.Split(frame, "\n")
		if len(rows) != 3 || !strings.Contains(rows[1], "Files") {
			t.Fatalf("current breadcrumb missing at %d columns: %q", width, frame)
		}
		for _, row := range rows {
			if ansi.StringWidth(row) > width {
				t.Errorf("breadcrumb overflow at %d columns: %q", width, row)
			}
		}
	}
}

func TestMetadataCannotInjectTerminalControlsIntoNavigation(t *testing.T) {
	frame := tui.Breadcrumbs([]string{"Presets", "Writing\x1b[2J\n\tcopy\a"}, -1, 38)
	plain := ansi.Strip(frame)
	if strings.Contains(frame, "\x1b[2J") || strings.Contains(frame, "\a") || strings.Count(plain, "\n") != 2 || !strings.Contains(plain, "Writing  copy") {
		t.Fatalf("metadata changed the terminal layout: %q", frame)
	}
}

func TestFrameKeepsActionsAndHelpBelowScrollingContent(t *testing.T) {
	for _, size := range [][2]int{{40, 24}, {80, 32}, {112, 24}, {28, 10}} {
		width, height := size[0], size[1]
		w := tui.ContentWidth(width)
		footer := tui.Actions(tui.Presets, []tui.Button{{Label: "Save", Kind: tui.PrimaryAction}}, w) + "\n" +
			tui.KeyHelp([]tui.Hint{{Key: "enter", Action: "save"}, {Key: "esc", Action: "back"}}, w)
		frame := tui.Frame{Width: width, Height: height, Header: tui.Header(tui.Presets, "", w), Footer: footer}
		view := ansi.Strip(frame.Render(strings.Repeat("content\n", 100)))
		rows := strings.Split(view, "\n")
		if len(rows) != height || !strings.Contains(view, "Save") || !strings.Contains(view, "esc back") {
			t.Fatalf("footer clipped at %dx%d: %s", width, height, view)
		}
		for _, row := range rows {
			if ansi.StringWidth(row) > width {
				t.Errorf("frame exceeds %d columns: %q", width, row)
			}
		}
	}
}

func TestKeyHelpWrapsWholeHints(t *testing.T) {
	view := ansi.Strip(tui.KeyHelp([]tui.Hint{{Key: "↑↓", Action: "move"}, {Key: "enter", Action: "activate"}, {Key: "esc", Action: "back"}}, 20))
	for _, hint := range []string{"↑↓ move", "enter activate", "esc back"} {
		if !strings.Contains(view, hint) {
			t.Errorf("split hint %q: %q", hint, view)
		}
	}
}

func TestPrimaryActionFocusSwapsEachToolsColors(t *testing.T) {
	for _, tool := range []tui.Tool{tui.Wizard, tui.Layouts, tui.Presets} {
		normal, focused := tool.Colors(tui.PrimaryAction), tool.Colors(tui.PrimaryActionFocused)
		if normal.Text != focused.Background || normal.Background != focused.Text {
			t.Errorf("%s focus does not invert its action colors", tool)
		}
	}
}

func TestStepsKeepTheCurrentStepAndCompletedMarksWithinClickTargets(t *testing.T) {
	labels := []string{"Presets", "Harnesses", "Review"}
	for _, width := range []int{26, 38, 76, 108} {
		for current, name := range labels {
			view, targets := tui.Steps(labels, current, -1, width)
			row := strings.Split(ansi.Strip(view), "\n")[1]
			if !strings.Contains(row, name) || strings.Count(row, "✓") != current {
				t.Fatalf("step %s missing its location/completed marks at %d columns: %q", name, width, row)
			}
			for i, target := range targets {
				text := strings.TrimSpace(ansi.Cut(row, target.X, target.X+target.Width))
				if target.Index != i || target.Y != 1 || target.Height != 1 || target.X+target.Width > width || text == "" || strings.Contains(text, "❯") {
					t.Fatalf("step %d target does not cover its label at %d columns: %+v %q", i, width, target, text)
				}
				if i == current && text != name {
					t.Fatalf("current step click target clipped %q: %q", name, text)
				}
			}
		}
	}
}

func TestSecondaryAndWrappedActionTargetsFollowTheirRenderedCells(t *testing.T) {
	for _, width := range []int{26, 27, 38, 76, 108} {
		labels := []string{"General", "Specialist", "Other"}
		for current := range labels {
			row, targets := tui.SecondaryWithTargets(tui.Wizard, labels, current, width, false)
			for _, target := range targets {
				text := strings.TrimSpace(ansi.Cut(ansi.Strip(row), target.X, target.X+target.Width))
				want := labels[target.Index]
				if target.Index == current {
					want = "[" + want + "]"
				}
				if text != want && text != "‹" && text != "›" {
					t.Fatalf("filter target at %d columns covers %q, want %q", width, text, want)
				}
			}
		}
		buttons := []tui.Button{{Label: "Install"}, {Label: "Save"}, {Label: "Back"}, {Label: "Cancel"}, {Label: "Retry"}}
		actions, targets := tui.ActionsWithTargets(tui.Wizard, buttons, width)
		rows := strings.Split(ansi.Strip(actions), "\n")
		for _, target := range targets {
			text := strings.TrimSpace(ansi.Cut(rows[target.Y], target.X, target.X+target.Width))
			if target.X+target.Width > width || text != buttons[target.Index].Label {
				t.Fatalf("wrapped action target at %d columns covers %q, want %q", width, text, buttons[target.Index].Label)
			}
		}
	}
}
