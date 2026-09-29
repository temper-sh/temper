package setupui

import (
	"context"
	"slices"

	tea "charm.land/bubbletea/v2"
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
		if m.page == 2 && msg.Seq == m.previewSeq {
			m.cancelPreview()
			m.loading, m.reviewErr = false, msg.Err
			m.reviewReady = msg.Err == nil
			m.plan = msg.Plan
			m.view.GotoTop()
			if m.focus == presetContentFocus && m.contentRows() == 0 {
				m.action = 0
				m.focusArea(presetFooterFocus)
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
			m.action = m.navigationButton(key == "shift+tab")
			m.focusArea(presetFooterFocus)
			return m, nil
		case "esc":
			if m.field != "" {
				return m, m.activateShortcut("cancel-edit")
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
			if m.field == "" || m.focus != presetContentFocus {
				return m, m.activateFocused()
			}
		case "pgup", "pgdown":
			m.focusSelection, m.focusDownloads = false, false
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
		switch key {
		case "k", "j":
			delta := 1
			if key == "k" {
				delta = -1
			}
			m.moveVertical(delta)
		case "a":
			if m.page == 0 {
				m.selectFilter(!m.all)
			}
		case "d", "s":
			if m.page == 2 && key == "d" && m.contentRows() > 0 {
				m.downloadsOpen = !m.downloadsOpen
				m.focusDownloads = true
			} else if m.page == 1 && m.layout != "" {
				m.loadingChoice(key)
				m.control = 1
				if key == "d" {
					m.control = 2
				}
				m.focusArea(presetContentFocus)
			}
		case "r":
			if m.page == 2 {
				return m, m.startPreview()
			}
			return m, m.activateShortcut(key)
		case "n", "x", "i", "c", "t":
			return m, m.activateShortcut(key)
		}
		return m, nil
	}
	var cmd tea.Cmd
	if _, wheel := msg.(tea.MouseWheelMsg); wheel {
		m.focusSelection, m.focusDownloads = false, false
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
		m.selectFilter(target.index == 1)
	case hitChoice:
		m.cursor, m.control = target.index, 0
		m.focusArea(presetContentFocus)
		return m.activateFocused()
	case hitFocus:
		m.cursor = target.index
		m.focusArea(presetContentFocus)
	case hitMember, hitStartup, hitDefault:
		m.cursor = target.index
		m.control = slices.Index([]hitKind{hitMember, hitStartup, hitDefault}, target.kind)
		m.focusArea(presetContentFocus)
		return m.activateFocused()
	case hitReview:
		m.action = target.index
		return m.finish()
	case hitDownloads:
		m.cursor = 0
		m.focusArea(presetContentFocus)
		return m.activateFocused()
	case hitIdle:
		m.cursor = m.count()
		m.focusArea(presetContentFocus)
		return m.activateFocused()
	case hitField:
		m.focusArea(presetContentFocus)
	}
	return nil
}

func (m *PresetModel) selectFilter(all bool) {
	m.all, m.cursor = all, 0
	m.view.GotoTop()
	m.focusSelection = m.focus == presetContentFocus
}

func (m *PresetModel) back() tea.Cmd {
	m.cancelPreview()
	m.notice = ""
	m.control = 0
	if m.layout != "" {
		m.cursor = slices.Index(m.layoutIDs(), m.layout)
		m.layout = ""
	} else if m.page > 0 {
		m.page--
		m.cursor = 0
	} else {
		m.Decision = PresetDecision{Action: "cancel"}
		return tea.Quit
	}
	m.action = m.navigationButton(false)
	m.focusArea(presetContentFocus)
	m.view.GotoTop()
	m.focusSelection = true
	return nil
}

func (m *PresetModel) advance() tea.Cmd {
	m.notice = ""
	if m.page == 0 {
		m.page, m.cursor = 1, 0
	} else if m.layout != "" {
		return m.back()
	} else if m.page == 1 {
		if err := m.draft.Validate(); err != nil {
			m.notice = err.Error()
			return nil
		}
		m.page, m.action, m.cursor = 2, 0, 0
		m.focusArea(presetFooterFocus)
		return m.startPreview()
	}
	m.control = 0
	m.action = m.navigationButton(false)
	m.focusArea(presetContentFocus)
	if m.contentRows() == 0 {
		m.action = m.navigationButton(false)
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

func (m *PresetModel) actionAvailable(index int) bool {
	return index >= 2 || m.reviewReady && !m.loading && (index == 0 || m.plan.CanPrepare)
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
		m.notice = "Review must complete before saving."
		if m.reviewReady && m.action == 1 {
			m.notice = "Preparation unavailable; review the reasons above."
		}
		return nil
	}
	action := "save"
	if m.action == 1 {
		action = "prepare"
	}
	m.Decision = PresetDecision{Configuration: m.draft, Action: action}
	return tea.Quit
}
