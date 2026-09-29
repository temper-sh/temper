package setupui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
)

// Rendering, pointer activation and keyboard focus share the same buttons.
// A narrow terminal changes their labels and visible range, not their actions.
type presetButton struct {
	label, short string
	available    bool
	target       hitTarget
}

func (m *PresetModel) footerButtons() []presetButton {
	button := func(label, short, key string, available bool) presetButton {
		return presetButton{label, short, available, hitTarget{kind: hitShortcut, key: key}}
	}
	if m.field != "" {
		return []presetButton{button("Apply", "Apply", "apply", true), button("Cancel", "Cancel", "cancel-edit", true)}
	}
	if m.page == 2 {
		var buttons []presetButton
		for i, label := range []string{"Save configuration", "Prepare installation", "Back", "Cancel"} {
			buttons = append(buttons, presetButton{label, []string{"Save", "Prepare", "Back", "Cancel"}[i], m.actionAvailable(i), hitTarget{kind: hitReview, index: i}})
		}
		return append(buttons, button("Retry preview", "Retry", "retry", true))
	}
	back, next, short := "← Back", "Next: Layouts →", "Next →"
	if m.page == 0 {
		back = "Cancel"
	} else if m.layout != "" {
		next, short = "Done →", "Done →"
	} else {
		next, short = "Review →", "Review →"
	}
	buttons := []presetButton{button(back, back, "back", true), {next, short, true, hitTarget{kind: hitNext}}}
	selected := m.cursor >= 0 && m.cursor < m.count()
	switch {
	case m.page == 0:
		buttons = append(buttons, button("c Context", "Context", "c", selected), button("t Template", "Template", "t", selected))
	case m.layout == "":
		buttons = append(buttons, button("n New layout", "New", "n", true), button("r Rename", "Rename", "r", selected), button("x Remove", "Remove", "x", selected))
	default:
		buttons = append(buttons, button("i Idle unload", "Idle", "i", true))
	}
	return buttons
}

func (m *PresetModel) navigationButton(back bool) int {
	if m.field != "" {
		if back {
			return 1
		}
		return 0
	}
	if m.page == 2 {
		if back {
			return 2
		}
		return 0
	}
	if back {
		return 0
	}
	return 1
}

func (m *PresetModel) focusArea(area presetFocus) {
	m.focus = area
	m.focusSelection = area == presetContentFocus
	m.focusDownloads = false
	if area == presetFilterFocus {
		m.view.GotoTop()
	}
	if m.field != "" {
		if area == presetContentFocus {
			m.text.Focus()
		} else {
			m.text.Blur()
		}
	} else if m.page == 2 && area == presetContentFocus {
		m.focusDownloads = true
	}
}

func (m *PresetModel) contentRows() int {
	if m.field != "" {
		return 1
	}
	if m.page == 2 {
		if m.reviewReady && !m.loading && m.reviewErr == nil {
			for _, section := range m.plan.Sections() {
				if section.Downloads != nil {
					return 1
				}
			}
		}
		return 0
	}
	if m.layout != "" {
		return m.count() + 1 // The idle-unload control follows the preset rows.
	}
	return m.count()
}

func (m *PresetModel) moveVertical(delta int) {
	rows := m.contentRows()
	filters := m.page == 0 && m.field == ""
	switch m.focus {
	case presetFilterFocus:
		if delta > 0 && rows > 0 {
			m.cursor = 0
			m.focusArea(presetContentFocus)
			return
		}
	case presetFooterFocus:
		if filters && (delta > 0 || rows == 0) {
			m.focusArea(presetFilterFocus)
		} else if rows > 0 {
			if m.field == "" {
				m.cursor = 0
				if delta < 0 {
					m.cursor = rows - 1
				}
			}
			m.focusArea(presetContentFocus)
		}
		return
	case presetContentFocus:
		if m.field == "" {
			next := m.cursor + delta
			if next >= 0 && next < rows {
				m.cursor = next
				m.focusArea(presetContentFocus)
				return
			}
			if delta < 0 && filters {
				m.focusArea(presetFilterFocus)
				return
			}
		}
	}
	m.action = m.navigationButton(false)
	m.focusArea(presetFooterFocus)
}

func (m *PresetModel) moveHorizontal(delta int) {
	switch m.focus {
	case presetFilterFocus:
		m.selectFilter(delta > 0)
	case presetFooterFocus:
		n := len(m.footerButtons())
		m.action = (m.action + delta + n) % n
	case presetContentFocus:
		if m.layout != "" && m.field == "" && m.cursor < m.count() {
			m.control = (m.control + delta + 3) % 3
			m.focusSelection = true
		}
	}
}

func (m *PresetModel) activateFocused() tea.Cmd {
	m.notice = ""
	switch m.focus {
	case presetFooterFocus:
		buttons := m.footerButtons()
		if m.action >= 0 && m.action < len(buttons) {
			// Save/prepare explain their refusal; unavailable contextual actions
			// have no selected row to act upon.
			b := buttons[m.action]
			if b.available || b.target.kind == hitReview {
				return m.activateTarget(b.target)
			}
		}
	case presetFilterFocus:
		m.moveVertical(1)
	case presetContentFocus:
		if m.field != "" {
			return m.activateShortcut("apply")
		}
		if m.page == 2 {
			if m.contentRows() > 0 {
				m.downloadsOpen = !m.downloadsOpen
				m.focusDownloads = true
			}
		} else if m.layout != "" && m.cursor == m.count() {
			return m.activateShortcut("i")
		} else if m.cursor < m.count() {
			m.focusSelection = true
			if m.page == 1 && m.layout == "" {
				m.layout = m.layoutIDs()[m.cursor]
				m.cursor, m.control = 0, 0
				m.view.GotoTop()
			} else if m.layout != "" && m.control != 0 {
				m.loadingChoice([]string{"", "s", "d"}[m.control])
			} else {
				m.toggleMember()
			}
		}
	}
	return nil
}

func (m *PresetModel) activateShortcut(key string) tea.Cmd {
	switch key {
	case "apply":
		m.acceptEdit()
		if m.field != "" {
			m.focusArea(presetContentFocus)
		}
		m.focusSelection = true
	case "cancel-edit":
		m.field, m.notice = "", ""
		m.text.Blur()
		m.focus, m.action = m.editFocus, m.editAction
		m.focusSelection = true
	case "back":
		return m.back()
	case "retry":
		return m.startPreview()
	case "n", "r", "x":
		if m.page != 1 || m.layout != "" {
			return nil
		}
		if key == "n" {
			return m.edit("new", "")
		}
		ids := m.layoutIDs()
		if m.cursor < len(ids) {
			if key == "r" {
				return m.edit("rename", m.draft.Layouts[ids[m.cursor]].Name)
			}
			delete(m.draft.Layouts, ids[m.cursor])
			m.cursor = max(0, m.cursor-1)
			m.focusSelection = m.focus == presetContentFocus
		}
	case "i":
		if m.page == 1 && m.layout != "" {
			return m.edit("idle", strconv.Itoa(m.draft.Layouts[m.layout].IdleSeconds))
		}
	case "c":
		if p, ok := m.selectedOption(); m.page == 0 && ok {
			lock := p.Lock
			if chosen, ok := m.draft.Presets[p.ID]; ok {
				lock = chosen.Lock
			}
			_, record := presetRecord(lock)
			return m.edit("context", strconv.Itoa(record.ContextWindowTokens))
		}
	case "t":
		if m.page == 0 {
			m.cycleTemplate()
			m.focusSelection = m.focus == presetContentFocus
		}
	}
	return nil
}
