package setupui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/setup"
)

func (m *PresetModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, msg.Width), max(1, msg.Height)
		m.focusSelection = m.focus == presetContentFocus
		m.hitTargets = nil
		return m, nil
	case compositionPreview:
		if m.page == presetReviewPage && msg.Seq == m.previewSeq {
			m.cancelPreview()
			m.loading, m.reviewErr = false, msg.Err
			m.reviewReady = msg.Err == nil
			m.plan = msg.Plan
			m.view.GotoTop()
			// Enter must never land on an install that cannot run.
			if m.focus == presetFooterFocus && m.action == 0 && m.installRefused() {
				m.action = 2
			}
		}
		return m, nil
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft || m.width < 28 || m.height < 10 {
			return m, nil
		}
		targets := m.hitTargets
		m.hitTargets = nil
		for _, target := range targets {
			if target.contains(msg.X, msg.Y) {
				return m, m.activateTarget(target)
			}
		}
		return m, nil
	case tea.KeyPressMsg:
		m.hitTargets = nil
		key := msg.String()
		if key == "ctrl+c" || key == "q" && (m.field == "" || m.width < 28 || m.height < 10) {
			m.cancelPreview()
			m.Decision = PresetDecision{Action: "cancel"}
			return m, tea.Quit
		}
		if m.width < 28 || m.height < 10 {
			return m, nil // Never accept a choice hidden by the resize prompt.
		}
		switch key {
		case "tab", "shift+tab":
			m.cycleFocus(key == "shift+tab")
			return m, nil
		case "esc":
			if m.field != "" {
				return m, m.activateShortcut("cancel-edit")
			}
			if m.page == 0 {
				// Esc goes back; the first screen has nothing behind it, and one key
				// must not drop every choice.
				m.notice = "To leave without saving, press q or choose Cancel."
				return m, nil
			}
			return m, m.back()
		case "up", "down":
			delta := 1
			if key == "up" {
				delta = -1
			}
			m.moveVertical(delta)
			return m, nil
		case "left", "right":
			if m.field == "" || m.focus != presetContentFocus {
				delta := 1
				if key == "left" {
					delta = -1
				}
				m.moveHorizontal(delta)
				return m, nil
			}
		case "enter":
			return m, m.activateFocused()
		case "space", " ":
			if m.field == "" && m.page == 0 && m.control == "compute" && m.focus == presetContentFocus {
				m.moveHorizontal(1)
				return m, nil
			}
			if m.field == "" || m.focus != presetContentFocus {
				return m, m.activateFocused()
			}
		case "pgup", "pgdown":
			m.focusSelection = false
			var cmd tea.Cmd
			m.view, cmd = m.view.Update(msg)
			return m, cmd
		}
		if m.field != "" {
			if m.focus == presetContentFocus {
				var cmd tea.Cmd
				m.text, cmd = m.text.Update(msg)
				return m, cmd
			}
			return m, nil
		}
		m.notice = ""
		if m.page == 0 {
			if group, ok := m.groupShortcut(key); ok {
				m.selectGroup(group)
				return m, nil
			}
		}
		switch key {
		case "k", "j":
			delta := 1
			if key == "k" {
				delta = -1
			}
			m.moveVertical(delta)
		case "r":
			if m.page == presetReviewPage {
				return m, m.startPreview()
			}
			return m, m.activateShortcut(key)
		case "c", "p":
			return m, m.activateShortcut(key)
		}
		return m, nil
	}
	var cmd tea.Cmd
	if _, wheel := msg.(tea.MouseWheelMsg); wheel {
		m.focusSelection = false
		m.view, cmd = m.view.Update(msg)
	} else if m.field != "" && m.focus == presetContentFocus {
		m.text, cmd = m.text.Update(msg)
	}
	return m, cmd
}

func (m *PresetModel) activateTarget(target hitTarget) tea.Cmd {
	m.notice = ""
	switch target.kind {
	case hitPresetAction:
		m.action = target.index
		m.focusArea(presetFooterFocus)
		return m.activateFocused()
	case hitShortcut:
		return m.activateShortcut(target.key)
	case hitNext:
		return m.advance()
	case hitFilter:
		m.focusArea(presetFilterFocus)
		m.selectGroup(m.groups()[target.index].ID)
	case hitWizardCompute:
		m.cursor, m.control = target.index, "compute"
		m.focusArea(presetContentFocus)
		if p, ok := m.selectedOption(); ok {
			if m.customize(p, 0, catalog.ComputeDevice(target.key)) {
				m.notice = ""
			}
		}
	case hitChoice:
		m.control = ""
		m.cursor = target.index
		m.focusArea(presetContentFocus)
		return m.activateFocused()
	case hitStep:
		m.step = target.index
		return m.selectStep(target.index)
	case hitReview:
		m.action = target.index
		return m.finish()
	case hitField:
		m.focusArea(presetContentFocus)
	}
	return nil
}

func (m *PresetModel) selectGroup(group string) {
	m.group, m.cursor, m.control = group, 0, ""
	m.action = min(m.action, len(m.footerButtons())-1)
	m.view.GotoTop()
	m.focusSelection = m.focus == presetContentFocus
}

func (m *PresetModel) back() tea.Cmd {
	m.cancelPreview()
	m.notice = ""
	if m.page > 0 {
		m.page--
		m.cursor, m.control = 0, ""
	} else {
		m.Decision = PresetDecision{Action: "cancel"}
		return tea.Quit
	}
	m.action = m.navigationButton()
	m.focusArea(presetContentFocus)
	m.view.GotoTop()
	m.focusSelection = true
	return nil
}

func (m *PresetModel) advance() tea.Cmd {
	m.control = ""
	m.notice = ""
	if m.page == 0 {
		if m.input.NewConfiguration {
			m.draft.Layouts = setup.StarterLayouts(m.presetIDs())
		}
		m.page, m.cursor = presetHarnessPage, 0
	} else if m.page == presetHarnessPage {
		if err := m.draft.Validate(); err != nil {
			m.notice = err.Error()
			return nil
		}
		m.page, m.action, m.cursor = presetReviewPage, 0, 0
		m.focusArea(presetFooterFocus)
		return m.startPreview()
	}
	m.action = m.navigationButton()
	m.focusArea(presetContentFocus)
	if m.contentRows() == 0 {
		m.action = m.navigationButton()
		m.focusArea(presetFooterFocus)
	}
	m.view.GotoTop()
	m.focusSelection = true
	return nil
}

func (m *PresetModel) cancelPreview() {
	if m.previewCancel != nil {
		m.previewCancel()
		m.previewCancel = nil
	}
}

func (m *PresetModel) startPreview() tea.Cmd {
	m.cancelPreview()
	m.previewSeq++
	m.loading, m.reviewReady, m.reviewErr = true, false, nil
	if m.focus != presetFooterFocus {
		m.action = 0
		m.focusArea(presetFooterFocus)
	}
	m.notice = ""
	m.view.GotoTop()
	ctx, cancel := context.WithCancel(m.ctx)
	m.previewCancel = cancel
	seq := m.previewSeq
	// A cancelled preview may still finish after the user resumes editing.
	raw, _ := m.draft.Bytes()
	draft, _ := setup.ParseConfiguration(raw)
	return func() tea.Msg {
		p, err := m.preview(ctx, draft)
		return compositionPreview{Seq: seq, Plan: p, Err: err}
	}
}

// Review actions: 0 install and start, 1 save without installing, 2 back,
// 3 cancel.
func (m *PresetModel) actionAvailable(index int) bool {
	return index >= 2 || m.reviewReady && !m.loading && (index == 1 || m.plan.CanPrepare)
}

func (m *PresetModel) finish() tea.Cmd {
	if m.action == 2 {
		return m.back()
	}
	if m.action == 3 {
		m.cancelPreview()
		m.Decision = PresetDecision{Action: "cancel"}
		return tea.Quit
	}
	if !m.actionAvailable(m.action) {
		m.notice = "Wait until the check finishes."
		if m.reviewReady && m.action == 0 {
			m.notice = "Installation is not possible yet; see the reasons above."
		}
		return nil
	}
	action := "prepare"
	if m.action == 1 {
		action = "save"
	}
	m.Decision = PresetDecision{Configuration: m.draft, Action: action}
	return tea.Quit
}

// Primary navigation changes pages only through the same guarded transitions
// as footer actions. Editing a field keeps navigation outside the focus order.
func (m *PresetModel) selectStep(step int) tea.Cmd {
	if m.field != "" || step < 0 || step > presetReviewPage || step == m.page {
		return nil
	}
	if step > m.page+1 {
		m.notice = "Continue through Harnesses before reviewing."
		return nil
	}
	if step > m.page {
		return m.advance()
	}
	m.cancelPreview()
	m.page, m.cursor, m.notice, m.control = step, 0, "", ""
	m.focusArea(presetContentFocus)
	m.view.GotoTop()
	return nil
}
