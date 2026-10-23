package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// FormField is an always-visible draft control. Value may be a textinput view;
// parsing, conditional visibility and persistence belong to the owning tool.
type FormField struct {
	Label, Value, Unit, Detail, Error string
	Focused                           bool
}

func FormInputWidth(width int, unit string) int {
	if unit != "" {
		width -= ansi.StringWidth(unit) + 1
	}
	return max(1, width-2)
}

// FormInputView uses the same surface for the text, cursor and surrounding box.
func FormInputView(input *textinput.Model, width int, unit string) string {
	input.Prompt = ""
	input.SetWidth(max(1, FormInputWidth(width, unit)-1))
	input.SetCursor(input.Position())
	styles := input.Styles()
	styles.Focused.Text = Style(Action)
	styles.Blurred.Text = Style(Action)
	input.SetStyles(styles)
	return input.View()
}

func (f FormField) Render(tool Tool, width int) string {
	valueWidth := FormInputWidth(width, f.Unit)
	border := Colors(Divider).Text
	if f.Focused {
		border = tool.Colors(Heading).Text
	}
	input := Style(Action).Width(valueWidth).Padding(0, 1).
		Border(lipgloss.Border{Bottom: "─"}, false, false, true, false).
		BorderForeground(border).Render(ansi.Truncate(f.Value, valueWidth, ""))
	if f.Unit != "" {
		input = lipgloss.JoinHorizontal(lipgloss.Top, input, " ", Style(Muted).Render(f.Unit))
	}
	lines := []string{Style(Body).Render(ansi.Wrap(f.Label, width, "")), input}
	if f.Detail != "" {
		lines = append(lines, Style(Muted).Render(ansi.Wrap(f.Detail, width, "")))
	}
	if f.Error != "" {
		lines = append(lines, Style(Error).Render(ansi.Wrap(f.Error, width, "")))
	}
	return strings.Join(lines, "\n")
}

// FormChoices distinguishes the chosen value from keyboard focus and keeps
// pointer targets tied to the same wrapping geometry as the visible buttons.
func FormChoices(tool Tool, labels []string, selected, width int, focused bool) (string, []Target) {
	buttons := make([]Button, len(labels))
	for i, label := range labels {
		mark, kind := "○ ", Action
		if i == selected {
			mark, kind = "● ", PrimaryAction
		}
		buttons[i] = Button{Label: mark + label, Kind: kind, Focused: focused && (i == selected || selected < 0 && i == 0)}
	}
	return ActionsWithTargets(tool, buttons, width)
}

func FormToggle(tool Tool, label string, checked, focused bool, width int) (string, []Target) {
	mark := "[ ] "
	if checked {
		mark = "[✓] "
	}
	return ActionsWithTargets(tool, []Button{{Label: mark + label, Kind: Action, Focused: focused}}, width)
}
