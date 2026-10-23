package setupui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/temper-sh/temper/internal/tui"
)

func (m *LayoutModel) activateAction(index int) tea.Cmd {
	actions := m.actions()
	if m.loading != "" || index < 0 || index >= len(actions) || actions[index].disabled {
		return nil
	}
	m.area, m.action, m.focus = layoutActions, index, true
	if m.field != "" {
		return m.inputKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	}
	return m.invoke(actions[index].id)
}

func (m *LayoutModel) click(area tui.FocusArea, index int) tea.Cmd {
	if area == layoutActions {
		return m.activateAction(index)
	}
	if m.loading != "" || m.mode == "confirm" {
		return nil
	}
	if m.field != "" {
		if area == layoutContent {
			m.area, m.focus = layoutContent, true
			return m.text.Focus()
		}
		return nil
	}
	m.area, m.focus = layoutContent, true
	switch area {
	case layoutPresetControls:
		if m.mode == "edit" && m.page == 1 {
			m.cursor = 2
			m.changeTimeUnit(index == 1)
			return nil
		}
		m.cursor, m.control = index/len(layoutPresetActions), index%len(layoutPresetActions)
		return m.activatePresetControl()
	case layoutBreadcrumbs:
		if index >= len(m.breadcrumbs())-1 {
			return nil
		}
		m.crumb = index
		if index == 0 {
			return m.leave(false)
		}
		return m.back()
	case layoutSections:
		if m.mode == "edit" && m.page != index {
			m.page, m.cursor = index, 0
			m.viewport.GotoTop()
		}
	case layoutContent:
		m.cursor = index
		if m.mode == "edit" {
			if m.page == 0 {
				return nil // The card selects; only its explicit controls toggle.
			}
			return m.invoke("field")
		}
	}
	return nil
}
