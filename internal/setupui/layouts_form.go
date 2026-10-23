package setupui

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/temper-sh/temper/internal/tui"
)

type layoutSettings struct {
	name, idle string
	minutes    bool
	errors     [2]string
}

func (m *LayoutModel) settingsDraft() *layoutSettings {
	if m.settings == nil {
		m.settings = map[string]*layoutSettings{}
	}
	if s := m.settings[m.id]; s != nil {
		return s
	}
	l := m.draft.Layouts[m.id]
	s := &layoutSettings{name: l.Name, idle: strconv.Itoa(l.IdleSeconds)}
	if l.IdleSeconds%60 == 0 {
		s.minutes, s.idle = true, strconv.Itoa(l.IdleSeconds/60)
	}
	m.settings[m.id] = s
	return s
}

func (m *LayoutModel) settingsError() string {
	for _, id := range m.layoutIDs() {
		if s := m.settings[id]; s != nil {
			for _, err := range s.errors {
				if err != "" {
					return err
				}
			}
		}
	}
	return ""
}

func (m *LayoutModel) syncSettingInput() {
	if m.mode != "edit" || m.page != 1 || m.cursor > 1 || m.field != "" {
		return
	}
	s := m.settingsDraft()
	value := s.name
	if m.cursor == 1 {
		value = s.idle
	}
	key := fmt.Sprintf("%s/%d", m.id, m.cursor)
	if m.settingInput != key {
		m.settingInput = key
		m.text.SetValue(value)
		m.text.CursorEnd()
	}
	if m.area == layoutContent {
		m.text.Focus()
	} else {
		m.text.Blur()
	}
}

func (m *LayoutModel) settingsKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.mode != "edit" || m.page != 1 || m.area != layoutContent || m.field != "" {
		return nil, false
	}
	key := msg.String()
	switch key {
	case "up", "down", "tab", "shift+tab", "esc", "ctrl+c", "ctrl+s", "pgup", "pgdown":
		return nil, false
	}
	if m.cursor == 2 {
		if key == "left" || key == "right" || key == "enter" || key == "space" {
			m.changeTimeUnit(!m.settingsDraft().minutes)
			return nil, true
		}
		return nil, false
	}
	if key == "enter" {
		m.moveVertical(1)
		return nil, true
	}
	m.syncSettingInput()
	var cmd tea.Cmd
	m.text, cmd = m.text.Update(msg)
	m.changeSetting()
	return cmd, true
}

func (m *LayoutModel) changeSetting() {
	s := m.settingsDraft()
	l := m.draft.Layouts[m.id]
	value := m.text.Value()
	s.errors[m.cursor] = ""
	if m.cursor == 0 {
		s.name = value
		if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\x00\r\n\x1b") {
			s.errors[0] = "Enter a layout name."
			return
		}
		l.Name = value
	} else {
		s.idle = value
		n, err := strconv.ParseFloat(value, 64)
		if s.minutes {
			n *= 60
		}
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 || n >= float64(math.MaxInt) || math.Abs(n-math.Round(n)) > 1e-8 {
			s.errors[1] = "Enter a positive duration in whole seconds."
			return
		}
		l.IdleSeconds = int(math.Round(n))
	}
	m.draft.Layouts[m.id] = l
	m.notice = ""
}

func (m *LayoutModel) changeTimeUnit(minutes bool) {
	s := m.settingsDraft()
	if s.errors[1] != "" {
		m.notice, m.noticeTone = s.errors[1], tui.Error
		return
	}
	value := float64(m.draft.Layouts[m.id].IdleSeconds)
	if minutes {
		value /= 60
	}
	s.minutes, s.idle = minutes, strconv.FormatFloat(value, 'f', -1, 64)
	m.settingInput = ""
}

func (m *LayoutModel) settingsBody(width int) (string, int, int, []tui.Target, []tui.Target) {
	s := m.settingsDraft()
	m.syncSettingInput()
	var parts []string
	var targets, choices []tui.Target
	start, end, y := 0, 0, 0
	unit := "seconds"
	if s.minutes {
		unit = "minutes"
	}
	for i, f := range []tui.FormField{
		{Label: "Name", Value: layoutText(s.name), Error: s.errors[0]},
		{Label: "Unload after inactivity", Value: layoutText(s.idle), Unit: unit, Detail: "Models load again when requested.", Error: s.errors[1]},
	} {
		f.Focused = m.cursor == i && m.area == layoutContent
		if f.Focused {
			f.Value = tui.FormInputView(&m.text, width, f.Unit)
		}
		block := f.Render(tui.Layouts, width)
		h := lipgloss.Height(block)
		if m.cursor == i {
			start, end = y, y+h-1
		}
		targets = append(targets, tui.Target{Index: i, Y: y, Width: width, Height: h})
		parts = append(parts, block)
		y += h + 1
	}
	selected := 0
	if s.minutes {
		selected = 1
	}
	options, optionsTargets := tui.FormChoices(tui.Layouts, []string{"Seconds", "Minutes"}, selected, width, m.cursor == 2 && m.area == layoutContent)
	parts = append(parts, tui.Style(tui.Body).Render("Time unit")+"\n"+options)
	for _, target := range optionsTargets {
		target.Y += y + 1
		choices = append(choices, target)
	}
	if m.cursor == 2 {
		start, end = y, y+lipgloss.Height(options)
	}
	targets = append(targets, tui.Target{Index: 2, Y: y, Width: width, Height: lipgloss.Height(options) + 1})
	return strings.Join(parts, "\n\n"), start, end, targets, choices
}
