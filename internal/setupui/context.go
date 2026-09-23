package setupui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ContextWindow describes the range accepted by the catalog for one layout.
// Automatic choices are resolved after software selection, during review.
type ContextWindow struct {
	Layout, Name     string
	Minimum, Maximum int
	RecordedDefaults map[string]int // Template ID to highest tested point with recorded software.
}

type contextKey struct{ mode, profile, layout string }

func (m *Model) initContexts(mode string, p Profile) {
	for _, window := range p.Contexts {
		key := contextKey{mode, p.ID, window.Layout}
		if _, exists := m.contexts[key]; exists {
			continue
		}
		input := textinput.New()
		input.Prompt = "Tokens: "
		input.SetWidth(12)
		input.SetVirtualCursor(true)
		styles := textinput.DefaultDarkStyles()
		styles.Focused.Text = lipgloss.NewStyle().Foreground(nightGreen)
		styles.Focused.Prompt = lipgloss.NewStyle().Foreground(nightCyan)
		styles.Blurred.Text = lipgloss.NewStyle().Foreground(nightText)
		styles.Blurred.Prompt = lipgloss.NewStyle().Foreground(nightMuted)
		styles.Cursor.Color, styles.Cursor.Blink = nightBlue, false
		input.SetStyles(styles)
		input.SetValue("auto")
		m.contexts[key] = input
	}
}

func (m *Model) contextOrNextMode() {
	if p, ok := m.currentProfile(); ok && len(p.Contexts) > 0 {
		m.stage, m.cursor = stageContext, 0
	} else {
		m.nextMode()
	}
}

func (m Model) currentContext() (contextKey, ContextWindow, bool) {
	p, ok := m.currentProfile()
	if !ok || m.cursor >= len(p.Contexts) {
		return contextKey{}, ContextWindow{}, false
	}
	window := p.Contexts[m.cursor]
	return contextKey{m.currentMode(), p.ID, window.Layout}, window, true
}

func (m *Model) updateContext(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, _, ok := m.currentContext()
	if !ok {
		return m, nil
	}
	input := m.contexts[key]
	input.Focus()
	var cmd tea.Cmd
	input, cmd = input.Update(msg)
	m.contexts[key] = input
	m.notice = ""
	m.focusSelection = true
	return m, cmd
}

func (m *Model) resetContext() {
	key, _, ok := m.currentContext()
	if !ok {
		return
	}
	input := m.contexts[key]
	input.SetValue("auto")
	input.CursorEnd()
	m.contexts[key] = input
	m.notice = ""
	m.focusSelection = true
}

func (m Model) validateContexts() error {
	p, ok := m.currentProfile()
	if !ok {
		return fmt.Errorf("Choose a profile for this mode.")
	}
	for _, window := range p.Contexts {
		input := m.contexts[contextKey{m.currentMode(), p.ID, window.Layout}]
		if strings.TrimSpace(input.Value()) == "auto" {
			continue
		}
		tokens, err := strconv.Atoi(input.Value())
		if err != nil || tokens < window.Minimum || tokens > window.Maximum {
			return fmt.Errorf("%s: enter a context between %d and %d tokens.", window.Name, window.Minimum, window.Maximum)
		}
	}
	return nil
}

func (m Model) contextChoices(mode, profile string) map[string]int {
	var result map[string]int
	for key, input := range m.contexts {
		if key.mode == mode && key.profile == profile {
			if strings.TrimSpace(input.Value()) == "auto" {
				continue
			}
			if result == nil {
				result = make(map[string]int)
			}
			result[key.layout], _ = strconv.Atoi(input.Value()) // Advancing validates the complete screen.
		}
	}
	return result
}

func (m Model) contextCard(p Profile, window ContextWindow, row, width int) (string, int) {
	input := m.contexts[contextKey{m.currentMode(), p.ID, window.Layout}]
	border := nightBorder
	if row == m.cursor {
		input.Focus()
		border = nightBlue
	} else {
		input.Blur()
	}
	inner := width - 4
	input.SetWidth(min(12, max(1, inner-lipgloss.Width(input.Prompt)-1)))
	heading := lipgloss.NewStyle().Foreground(nightCyan).Bold(true).Width(inner).Render(window.Name)
	known := "Recorded software: tested context unknown for this machine and template."
	patch := m.patches[m.currentMode()][p.ID][window.Layout]
	if tokens := window.RecordedDefaults[patch]; tokens > 0 {
		known = fmt.Sprintf("Recorded software: largest verified window %d tokens.", tokens)
	}
	text := heading + "\n" + input.View() + "\n" +
		lipgloss.NewStyle().Foreground(nightMuted).Width(inner).Render(known+fmt.Sprintf("\nModel limit: %d tokens. Minimum: %d (includes output).", window.Maximum, window.Minimum))
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(0, 1).Width(width).Render(text), 1 + lipgloss.Height(heading)
}
