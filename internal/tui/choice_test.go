package tui_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/tui"
)

func TestChoiceTargetsFollowWrappedOptionsAndAllowNoSelection(t *testing.T) {
	labels := []string{"auto", "none", "mmap", "mlock", "mmap+mlock", "dio"}
	for _, width := range []int{26, 38, 76} {
		for selected := -1; selected < len(labels); selected++ {
			view, targets := tui.Choices(tui.Presets, labels, selected, width, true)
			plain := ansi.Strip(view)
			rows := strings.Split(plain, "\n")
			if selected < 0 && strings.Contains(plain, "[x]") {
				t.Fatal("rendering selected an option without a choice")
			}
			if len(targets) != len(labels) {
				t.Fatal("an option has no click target")
			}
			for i, target := range targets {
				mark := "[ ] "
				if i == selected {
					mark = "[x] "
				}
				text := strings.TrimSpace(ansi.Cut(rows[target.Y], target.X, target.X+target.Width))
				if target.Index != i || text != mark+labels[i] || target.X+target.Width > width {
					t.Fatalf("choice %q at width %d has mismatched geometry: %+v covers %q", labels[i], width, target, text)
				}
			}
		}
	}
}
