package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// FieldRow presents a label and value without owning editing or validation.
// Value may contain a rendered input; Detail and Error are plain supporting text.
// Callers decide when supporting text is visible.
type FieldRow struct {
	Label, Value, Detail, Error string
	Focused, Overridden         bool
}

func fieldLabelWidth(width int) int {
	preferred := 22
	if width < 70 {
		preferred = 16
	}
	return min(preferred, max(1, (width-2)/2))
}

// FieldValueWidth reserves the same label, gaps and override marker as Render.
// Input owners use it to size their control before passing its view as Value.
func FieldValueWidth(width int) int {
	return max(1, width-2-fieldLabelWidth(width)-3)
}

func (f FieldRow) Render(width int) string {
	labelWidth, valueWidth := fieldLabelWidth(width), FieldValueWidth(width)
	labelStyle := Style(Muted)
	if f.Focused {
		labelStyle = Style(Body)
	}
	label := labelStyle.Width(labelWidth).Render(ansi.Wrap(f.Label, labelWidth, ""))
	value := Style(Body).Width(valueWidth).Render(ansi.Wrap(f.Value, valueWidth, ""))
	marker := " "
	if f.Overridden {
		marker = "*"
	}
	row := lipgloss.JoinHorizontal(lipgloss.Top, label, " ", value, " ", Style(Muted).Render(marker))
	parts := []string{row}
	for _, text := range []struct {
		value string
		tone  Element
	}{{f.Detail, Muted}, {f.Error, Error}} {
		if text.value != "" {
			parts = append(parts, Style(text.tone).Width(max(1, width-2)).Render(ansi.Wrap(text.value, max(1, width-2), "")))
		}
	}
	style := Style(Body).Width(width).PaddingLeft(2)
	if f.Focused {
		style = style.PaddingLeft(1).Border(lipgloss.Border{Left: "▎"}, false, false, false, true).
			BorderForeground(Colors(EditorRowFocused).Border)
	}
	return style.Render(strings.Join(parts, "\n"))
}
