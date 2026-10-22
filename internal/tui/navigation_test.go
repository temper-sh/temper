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
