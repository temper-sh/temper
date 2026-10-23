package tui_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/tui"
)

func TestFieldRowsKeepRenderedInputsAndSupportingTextWithinTheFrame(t *testing.T) {
	for _, width := range []int{26, 38, 76, 108} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			input := tui.TextInput(200, tui.Colors(tui.Input).Text, tui.Colors(tui.InputPrompt).Text, tui.Colors(tui.InputCursor).Text)
			input.Prompt = ""
			input.SetWidth(tui.FieldValueWidth(width) - 1)
			input.SetValue("0.6")
			input.Focus()
			styles := input.Styles()
			styles.Focused.Text = styles.Focused.Text.Underline(true).UnderlineSpaces(true)
			input.SetStyles(styles)
			row := tui.FieldRow{Label: "Temperature", Value: input.View(), Detail: "Sampling temperature; 0 is greedy.", Error: "Enter a number.", Focused: true, Overridden: true}.Render(width)
			plain := ansi.Strip(row)
			for _, part := range []string{"0.6", "*", "Enter a number."} {
				if !strings.Contains(plain, part) {
					t.Fatalf("lost field content %q: %q", part, plain)
				}
			}
			if strings.Contains(plain, "[38;") || strings.Contains(plain, "[48;") {
				t.Fatalf("input colors became visible text: %q", plain)
			}
			for _, line := range strings.Split(row, "\n") {
				if ansi.StringWidth(line) > width {
					t.Fatalf("field exceeds available width: %q", plain)
				}
			}
		})
	}
}
