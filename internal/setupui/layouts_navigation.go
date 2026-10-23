package setupui

import "github.com/temper-sh/temper/internal/tui"

func (m *LayoutModel) focusOrder() tui.FocusOrder {
	if m.field != "" {
		return tui.FocusOrder{Rows: 1}
	}
	if m.mode == "confirm" {
		return tui.FocusOrder{} // Only the guarded confirmation actions.
	}
	rows := m.count()
	if m.mode == "review" {
		rows = 1
	}
	return tui.FocusOrder{Primary: len(m.breadcrumbs()) > 1, Secondary: m.mode == "edit", Rows: rows}
}

func (m *LayoutModel) focusArea(area tui.FocusArea) {
	m.area = area
	m.focus = area == layoutContent && m.mode != "review"
	if area == layoutBreadcrumbs {
		m.crumb = max(0, len(m.breadcrumbs())-2)
	}
	if area == layoutActions {
		m.action = 0
		for i, action := range m.actions() {
			if action.kind == tui.PrimaryAction && !action.disabled {
				m.action = i
				break
			}
		}
	}
}

func (m *LayoutModel) moveArea(back bool) {
	delta := 1
	if back {
		delta = -1
	}
	m.focusArea(m.focusOrder().Cycle(m.area, delta))
}

func (m *LayoutModel) moveVertical(delta int) {
	row := m.cursor
	if m.mode == "review" {
		row = 0
		if m.area == layoutContent {
			m.focus = false
			if delta < 0 && !m.viewport.AtTop() {
				m.viewport.ScrollUp(1)
				return
			}
			if delta > 0 && !m.viewport.AtBottom() {
				m.viewport.ScrollDown(1)
				return
			}
		}
	}
	area, row := m.focusOrder().Vertical(m.area, row, delta)
	if area == layoutContent && m.mode != "review" {
		m.cursor = row
	}
	m.focusArea(area)
}
