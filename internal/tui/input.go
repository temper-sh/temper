// Package tui holds controls shared by Temper's setup and authoring screens.
package tui

import (
	"image/color"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

// TextInput shares input behavior while each tool supplies its current colors.
func TextInput(limit int, text, prompt, cursor color.Color) textinput.Model {
	t := textinput.New()
	t.CharLimit = limit
	t.Prompt = "› "
	t.SetVirtualCursor(true)
	styles := textinput.DefaultDarkStyles()
	styles.Focused.Text = lipgloss.NewStyle().Foreground(text)
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(prompt)
	styles.Cursor.Color, styles.Cursor.Blink = cursor, false
	t.SetStyles(styles)
	return t
}
