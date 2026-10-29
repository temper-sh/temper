package setupui

import (
	"slices"
	"strconv"

	tea "charm.land/bubbletea/v2"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/tui"
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
		return []presetButton{button("Cancel", "Cancel", "cancel-edit", true), button("Apply", "Apply", "apply", true)}
	}
	if m.page == presetReviewPage {
		install := "Install and start"
		if len(m.draft.Layouts) == 0 || len(m.draft.Presets) == 0 {
			install = "Install"
		}
		if m.reviewReady && !m.loading && !m.plan.CanPrepare {
			install = "Can't install yet"
		}
		var buttons []presetButton
		for i, label := range []string{install, "Save without installing", "Back", "Cancel"} {
			buttons = append(buttons, presetButton{label, []string{"Install", "Save", "Back", "Cancel"}[i], m.actionAvailable(i), hitTarget{kind: hitReview, index: i}})
		}
		if m.reviewErr != nil {
			buttons = append(buttons, button("Retry check", "Retry", "retry", true))
		}
		return buttons
	}
	back, next, short := "← Back", "Next: Harnesses →", "Next →"
	if m.page == 0 {
		back = "Cancel"
	}
	if m.page == presetHarnessPage {
		next, short = "Review →", "Review →"
	}
	buttons := []presetButton{button(back, back, "back", true), {next, short, true, hitTarget{kind: hitNext}}}
	if m.page == presetHarnessPage {
		return buttons
	}
	selected := m.cursor >= 0 && m.cursor < m.count()
	switch {
	case m.page == 0:
		p, _ := m.selectedOption()
		lock := p.Lock
		if selected, ok := m.draft.Presets[p.ID]; ok {
			lock = selected.Lock
		}
		_, record := presetRecord(lock)
		buttons = append(buttons, button("c Context", "Context", "c", selected && record.ContextConfigurable()))
		if len(record.ComputeOptions) > 1 {
			_, included := m.draft.Presets[p.ID]
			buttons = append(buttons, button("p Compute", "Compute", "p", selected && included))
		}
	}
	return buttons
}

// installRefused is true once the review check finished and refuses install.
func (m *PresetModel) installRefused() bool {
	return m.reviewReady && !m.loading && !m.plan.CanPrepare
}

func (m *PresetModel) navigationButton() int {
	if m.field != "" {
		return 1
	}
	if m.page == presetReviewPage {
		// Prefer Back while installation is unavailable, including a pending check.
		if !m.actionAvailable(0) {
			return 2
		}
		return 0
	}
	return 1
}

func (m *PresetModel) focusArea(area presetFocus) {
	m.focus = area
	m.focusSelection = area == presetContentFocus
	if area == presetFilterFocus {
		m.view.GotoTop()
	}
	if m.field == "context" {
		if area == presetContentFocus {
			m.text.Focus()
		} else {
			m.text.Blur()
		}
	}
}

func (m *PresetModel) cycleFocus(back bool) {
	delta := 1
	if back {
		delta = -1
	}
	m.navigateTo(m.focusOrder().Cycle(m.focus, delta))
}

func (m *PresetModel) navigateTo(area presetFocus) {
	if area == presetFooterFocus {
		m.action = m.navigationButton()
	}
	if area == presetStepsFocus {
		m.step = m.page
	}
	m.focusArea(area)
}

func (m *PresetModel) focusOrder() tui.FocusOrder {
	return tui.FocusOrder{Primary: m.field == "", Secondary: m.field == "" && m.page == 0, Rows: m.contentRows()}
}

func (m *PresetModel) contentRows() int {
	if m.field != "" || m.page == presetReviewPage {
		// Review has one scrollable content area, with no selectable rows.
		return 1
	}
	return m.count()
}

func (m *PresetModel) moveVertical(delta int) {
	if m.focus == presetContentFocus && m.field == "" && m.page == presetReviewPage {
		if delta < 0 && !m.view.AtTop() {
			m.view.ScrollUp(1)
			return
		}
		if delta > 0 && !m.view.AtBottom() {
			m.view.ScrollDown(1)
			return
		}
	}
	row := m.cursor
	if m.field != "" || m.page == presetReviewPage {
		row = 0
	}
	area, row := m.focusOrder().Vertical(m.focus, row, delta)
	if m.field == "" && m.page != presetReviewPage {
		m.cursor = row
	}
	m.navigateTo(area)
}

func (m *PresetModel) moveHorizontal(delta int) {
	switch m.focus {
	case presetFilterFocus, presetContentFocus:
		if m.field == "compute" && m.focus == presetContentFocus {
			p, ok := m.selectedOption()
			if !ok {
				return
			}
			_, record := presetRecord(m.draft.Presets[p.ID].Lock)
			next := tui.CycleChoice(slices.Index(record.ComputeOptions, m.compute), len(record.ComputeOptions), delta)
			m.compute = record.ComputeOptions[next]
			m.notice = ""
			return
		}
		if m.field != "" || m.page != 0 {
			return
		}
		groups := m.groups()
		i := slices.IndexFunc(groups, func(g catalog.Group) bool { return g.ID == m.group })
		m.selectGroup(groups[tui.CycleChoice(i, len(groups), delta)].ID)
	case presetFooterFocus:
		n := len(m.footerButtons())
		m.action = tui.CycleChoice(m.action, n, delta)
	case presetStepsFocus:
		m.step = max(0, min(presetReviewPage, m.step+delta))
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
	case presetStepsFocus:
		return m.selectStep(m.step)
	case presetFilterFocus:
		m.moveVertical(1)
	case presetContentFocus:
		if m.field != "" {
			return m.activateShortcut("apply")
		}
		if m.page != presetReviewPage && m.cursor < m.count() {
			m.focusSelection = true
			m.toggleMember()
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
		m.focusArea(presetContentFocus)
	case "back":
		return m.back()
	case "retry":
		return m.startPreview()
	case "c":
		if p, ok := m.selectedOption(); m.page == 0 && ok {
			lock := p.Lock
			if chosen, ok := m.draft.Presets[p.ID]; ok {
				lock = chosen.Lock
			}
			_, record := presetRecord(lock)
			if len(lock.Records.Presets) == 0 {
				m.notice = "Unavailable: " + p.Unavailable
				return nil
			}
			if !record.ContextConfigurable() {
				m.notice = "Context is fixed by this model; the engine has no context override."
				return nil
			}
			return m.edit("context", strconv.Itoa(record.Window()))
		}
	case "p":
		if p, ok := m.selectedOption(); m.page == 0 && ok {
			lock := p.Lock
			chosen, included := m.draft.Presets[p.ID]
			if included {
				lock = chosen.Lock
			}
			_, record := presetRecord(lock)
			if len(record.ComputeOptions) < 2 {
				return nil
			}
			if !included {
				m.notice = "Include the preset before customizing it."
				return nil
			}
			return m.edit("compute", string(record.Compute))
		}
	}
	return nil
}
