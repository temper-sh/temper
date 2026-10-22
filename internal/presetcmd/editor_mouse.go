package presetcmd

import tea "charm.land/bubbletea/v2"

func (m *Editor) click(area editorArea, index int) tea.Cmd {
	if area == actionsArea {
		actions := m.actions()
		if index < 0 || index >= len(actions) || actions[index].disabled {
			return nil
		}
		m.area, m.actionCursor, m.focus = contentArea, index, true
		m.notice = ""
		return m.activateAction(actions[index].key)
	}
	if m.loading {
		return nil
	}
	if m.input != "" || m.confirm != "" {
		if area == contentArea {
			m.area, m.focus = contentArea, true
			if m.input != "" {
				return m.text.Focus()
			}
			m.confirmCursor = index // Confirmation still requires its explicit action.
		}
		return nil
	}
	m.area, m.focus, m.notice = contentArea, true, ""
	switch area {
	case primaryArea:
		if index < len(m.breadcrumbs())-1 {
			m.navCursor = index
			return m.goToAncestor(index)
		}
	case secondaryArea:
		if m.mode == "editor" {
			_, pages := m.sections()
			if index < len(pages) && pages[index] != m.page {
				return m.selectSection(pages[index])
			}
		} else if m.mode == "manager" && m.catalogTab != (index == 1) {
			m.catalogTab, m.cursor = index == 1, 0
			m.viewport.GotoTop()
		}
	case contentArea:
		m.cursor = index
		switch m.mode {
		case "editor":
			return m.editorKey("enter")
		case "form":
			return m.formKey("enter")
		case "files":
			return m.filesKey("space")
		}
	}
	return nil
}
