package presetcmd

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/tui"
)

type editorArea = tui.FocusArea

const (
	contentArea   = tui.ContentFocus
	primaryArea   = tui.PrimaryFocus
	secondaryArea = tui.SecondaryFocus
	actionsArea   = tui.ActionsFocus
)

type editorAction struct {
	label, key string
	primary    bool
	disabled   bool
}

func (m *Editor) breadcrumbs() []string {
	path := []string{"Presets"}
	if m.mode == "overview" {
		path = append(path, m.overview.name())
	}
	if m.draft != nil && m.mode != "manager" {
		name := m.draft.displayName(m.view, m.memory)
		if name == "" {
			name = m.draft.id
		}
		path = append(path, name)
	}
	switch {
	case m.input == "new-name":
		path = append(path, "New preset")
	case m.mode == "form":
		path = append(path, strings.Title(m.formKind))
	case m.mode == "local-files":
		path = append(path, "Template", "Browse")
	case m.mode == "files":
		path = append(path, "Weights", "Files")
	case m.mode == "report":
		path = append(path, m.reportTitle)
	}
	return path
}

func (m *Editor) secondary() ([]string, int) {
	switch m.mode {
	case "manager":
		current := 0
		if m.catalogTab {
			current = 1
		}
		return []string{"Yours", "Catalog"}, current
	case "editor":
		labels, pages := m.sections()
		for i, page := range pages {
			if page == m.page {
				return labels, i
			}
		}
		return labels, 0
	default:
		return nil, 0
	}
}

func (m *Editor) actions() []editorAction {
	switch {
	case m.loading:
		return []editorAction{{label: "Cancel", key: "esc"}}
	case m.input != "":
		return []editorAction{{label: "Cancel", key: "esc"}, {label: "Apply", key: "enter", primary: true}}
	case m.confirm != "":
		return []editorAction{{label: "Back", key: "esc"}, {label: "Confirm", key: "enter", primary: true}}
	}
	switch m.mode {
	case "manager":
		return []editorAction{{label: "New", key: "n"}, {label: "Open", key: "enter", primary: true, disabled: len(m.visiblePresets()) == 0}, {label: "Quit", key: "q"}}
	case "editor":
		save := "Save"
		if m.options.DryRun {
			save = "Validate"
		}
		return []editorAction{{label: "Discard", key: "esc"}, {label: "Review", key: "v"}, {label: save, key: "s", primary: true}}
	case "form":
		label := "Find files"
		if m.formKind == "template" {
			label = "Use template"
		}
		return []editorAction{{label: "Cancel", key: "esc"}, {label: label, key: "submit-form", primary: true}}
	case "local-files":
		return []editorAction{{label: "Cancel", key: "esc"}, {label: "Location…", key: "location"}, {label: "Choose", key: "enter", primary: true}}
	case "files":
		return []editorAction{{label: "Cancel", key: "cancel-source"}, {label: "Back", key: "esc"}, {label: "Use weights", key: "use-weights", primary: true}}
	case "overview":
		return []editorAction{{label: "Back", key: "esc"},
			{label: "Fork", key: "f", primary: true, disabled: m.overview.lock == nil},
			{label: "Copy", key: "c", disabled: m.overview.lock == nil}}
	default:
		return []editorAction{{label: "Back", key: "esc"}}
	}
}

func (m *Editor) hints() []tui.Hint {
	switch {
	case m.loading:
		return []tui.Hint{{Key: "esc", Action: "cancel read"}}
	case m.input != "":
		return tui.InputHints(m.area == actionsArea)
	case m.confirm != "":
		if m.area == actionsArea {
			return tui.FocusHints(m.area)
		}
		return []tui.Hint{{Key: "↑↓", Action: "choose"}, {Key: "enter", Action: "confirm"}, {Key: "esc", Action: "back"}}
	case m.area != contentArea:
		return tui.FocusHints(m.area)
	}
	if m.mode == "manager" {
		return []tui.Hint{{Key: "↑↓", Action: "move"}, {Key: "f", Action: "fork"}, {Key: "c", Action: "copy"}, {Key: "tab", Action: "focus"}}
	}
	if m.mode == "files" {
		return []tui.Hint{{Key: "↑↓", Action: "move"}, {Key: "space", Action: "toggle"}, {Key: "/", Action: "filter"}, {Key: "tab", Action: "focus"}}
	}
	if m.mode == "overview" {
		return []tui.Hint{{Key: "↑↓", Action: "scroll"}, {Key: "f", Action: "fork"}, {Key: "c", Action: "copy"}, {Key: "tab", Action: "focus"}}
	}
	if m.mode == "report" || m.mode == "editor" && m.page == 4 {
		return []tui.Hint{{Key: "↑↓", Action: "scroll"}, {Key: "tab", Action: "focus"}, {Key: "esc", Action: "back"}}
	}
	hints := []tui.Hint{{Key: "↑↓", Action: "controls"}, {Key: "←→", Action: "choose"}, {Key: "enter", Action: "use"}}
	if m.mode == "editor" {
		hints = append(hints, tui.Hint{Key: "alt+r", Action: "reset"})
	}
	return hints
}

func (m *Editor) renderFooter(width int) (string, []tui.Target) {
	var buttons []tui.Button
	for i, action := range m.actions() {
		kind := tui.Action
		if action.primary {
			kind = tui.PrimaryAction
		}
		buttons = append(buttons, tui.Button{Label: action.label, Kind: kind, Disabled: action.disabled,
			Focused: m.area == actionsArea && i == m.actionCursor && !m.loading})
	}
	footer := tui.Style(tui.Divider).Render(strings.Repeat("─", width)) + "\n"
	if m.mode == "editor" && m.input == "" && m.confirm == "" {
		status := "No unsaved changes"
		if m.draft.dirty() || m.formErrors() {
			status = "Unsaved changes"
		}
		footer += tui.Style(tui.Muted).Render(status) + "\n"
	}
	notice := m.notice
	if m.loading {
		notice = "Reading…"
	}
	if notice != "" {
		rows := strings.Split(ansi.Wrap(terminalText(notice), width, ""), "\n")
		if len(rows) > 4 {
			rows = append([]string{rows[0], "…"}, rows[len(rows)-2:]...)
		}
		footer = tui.Style(tui.Notice).Render(strings.Join(rows, "\n")) + "\n" + footer
	}
	actions, targets := tui.ActionsWithTargets(tui.Presets, buttons, width)
	for i := range targets {
		targets[i].Y += strings.Count(footer, "\n")
	}
	return footer + actions + "\n" + tui.KeyHelp(m.hints(), width), targets
}

func (m *Editor) selectSection(index int) tea.Cmd {
	m.page, m.cursor = index, 0
	m.viewport.GotoTop()
	if m.page == 4 {
		return m.review()
	}
	return nil
}

func (m *Editor) navigationKey(key string) (tea.Cmd, bool) {
	if key == "up" || key == "k" || key == "down" || key == "j" {
		delta := 1
		if key == "up" || key == "k" {
			delta = -1
		}
		m.moveVertical(delta)
		return nil, true
	}
	if key == "tab" || key == "shift+tab" {
		delta := 1
		if key == "shift+tab" {
			delta = -1
		}
		m.focusArea(m.focusOrder().Cycle(m.area, delta))
		return nil, true
	}
	if m.area == contentArea {
		return nil, false
	}
	if key == "enter" || key == "space" || key == " " {
		switch m.area {
		case primaryArea:
			m.area = contentArea
			return m.goToAncestor(m.navCursor), true
		case secondaryArea:
			m.moveVertical(1)
			return nil, true
		case actionsArea:
			action := m.actions()[m.actionCursor]
			if action.disabled {
				return nil, true
			}
			m.area = contentArea
			return m.activateAction(action.key), true
		}
	}
	if key != "left" && key != "right" && key != "home" && key != "end" {
		return nil, false
	}
	current, count := 0, 0
	switch m.area {
	case primaryArea:
		current, count = m.navCursor, len(m.breadcrumbs())-1
	case secondaryArea:
		labels, selected := m.secondary()
		current, count = selected, len(labels)
	case actionsArea:
		current, count = m.actionCursor, len(m.actions())
	}
	if count == 0 {
		return nil, true
	}
	next := current
	switch key {
	case "left":
		next = tui.CycleChoice(current, count, -1)
		if m.area == primaryArea {
			next = max(0, current-1)
		}
	case "right":
		next = tui.CycleChoice(current, count, 1)
		if m.area == primaryArea {
			next = min(count-1, current+1)
		}
	case "home":
		next = 0
	case "end":
		next = count - 1
	}
	switch m.area {
	case primaryArea:
		m.navCursor = next
	case actionsArea:
		m.actionCursor = next
	case secondaryArea:
		if m.mode == "editor" {
			_, pages := m.sections()
			return m.selectSection(pages[next]), true
		}
		m.catalogTab, m.cursor = next == 1, 0
	}
	return nil, true
}

func (m *Editor) scrollableContent() bool {
	return m.mode == "overview" || m.mode == "report" || m.mode == "editor" && m.page == 4
}

func (m *Editor) focusOrder() tui.FocusOrder {
	labels, _ := m.secondary()
	return tui.FocusOrder{Primary: len(m.breadcrumbs()) > 1, Secondary: len(labels) > 0, Rows: len(m.contentRows())}
}

func (m *Editor) contentRows() []int {
	if m.mode == "form" {
		rows := make([]int, len(m.controls()))
		for i := range rows {
			rows[i] = i
		}
		return rows
	}
	count := 0
	switch {
	case m.scrollableContent():
		count = 1 // One scrollable region, with no selectable rows.
	case m.mode == "manager":
		count = len(m.visiblePresets())
	case m.mode == "editor":
		count = len(m.controls())
	case m.mode == "files":
		count = len(m.visibleFiles())
		if m.needsModelRelationship() {
			count++
		}
	case m.mode == "local-files":
		count = 1 + len(m.directoryEntries)
	}
	rows := make([]int, count)
	for i := range rows {
		rows[i] = i
	}
	return rows
}

func (m *Editor) focusArea(area editorArea) {
	m.area = area
	m.focus = area == contentArea && !m.scrollableContent()
	switch area {
	case primaryArea:
		m.navCursor = max(0, len(m.breadcrumbs())-2)
	case actionsArea:
		m.actionCursor = 0
		for i, action := range m.actions() {
			if !action.disabled && action.primary {
				m.actionCursor = i
				return
			}
		}
		for i, action := range m.actions() {
			if !action.disabled {
				m.actionCursor = i
				return
			}
		}
	}
}

// Vertical movement follows the visual order, crossing a content boundary only
// after its last row or scroll line. Modal inputs and confirmations stay local.
func (m *Editor) moveVertical(delta int) {
	rows := m.contentRows()
	row := max(0, slices.Index(rows, m.cursor))
	if m.scrollableContent() {
		row = 0
	}
	if m.area == contentArea {
		if m.scrollableContent() {
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
	if area == contentArea && !m.scrollableContent() {
		m.cursor = rows[row]
	}
	m.focusArea(area)
}

func (m *Editor) activateAction(key string) tea.Cmd {
	if m.loading {
		if key == "esc" {
			m.cancelJob()
			m.notice = "Read cancelled; your draft is unchanged."
		}
		return nil
	}
	if m.input != "" {
		code := tea.KeyEnter
		if key == "esc" {
			code = tea.KeyEsc
		}
		return m.inputKey(tea.KeyPressMsg{Code: code})
	}
	if m.confirm != "" {
		return m.confirmKey(key)
	}
	switch m.mode {
	case "manager":
		return m.managerKey(key)
	case "editor":
		return m.editorKey(key)
	case "form":
		if key == "submit-form" {
			return m.submitForm()
		}
		return m.formKey(key)
	case "local-files":
		return m.localFileKey(key)
	case "files":
		if key == "cancel-source" {
			m.mode, m.cursor, m.controlKey = "editor", 0, ""
			return nil
		}
		if key == "use-weights" {
			return m.selectWeights()
		}
		return m.filesKey(key)
	case "overview":
		return m.overviewKey(key)
	case "report":
		m.mode = m.returnMode
		m.viewport.GotoTop()
	}
	return nil
}

func (m *Editor) goToAncestor(index int) tea.Cmd {
	if m.mode == "overview" {
		return m.overviewKey("esc")
	}
	if m.mode == "report" {
		m.activateAction("esc")
		if m.mode == "editor" && index == 0 {
			return m.editorKey("esc")
		}
		return nil
	}
	if m.mode == "local-files" {
		m.localFileKey("esc")
		if index >= 2 {
			return nil
		}
	}
	if m.mode == "files" && index == 2 {
		return m.filesKey("esc")
	}
	if m.mode == "files" {
		m.filesKey("esc")
	}
	if m.mode == "form" {
		m.formKey("esc")
	}
	if index == 0 {
		return m.editorKey("esc")
	}
	return nil
}

func (m *Editor) renderHeader(width int) (string, []tui.Target, []tui.Target) {
	badge := ""
	if m.options.DryRun {
		badge = "DRY RUN"
	}
	header := tui.Header(tui.Presets, badge, width) + "\n"
	if m.height >= 18 {
		header += "\n"
	}
	if explanation := m.pageExplanation(width); explanation != "" {
		header += explanation + "\n\n"
	}
	focus := -1
	if m.area == primaryArea && m.input == "" && m.confirm == "" && !m.loading {
		focus = m.navCursor
	}
	path, primary := tui.BreadcrumbsWithTargets(m.breadcrumbs(), focus, width)
	for i := range primary {
		primary[i].Y += strings.Count(header, "\n")
	}
	header += path + "\n"
	labels, current := m.secondary()
	sections, secondary := tui.SecondaryWithTargets(tui.Presets, labels, current, width,
		m.area == secondaryArea && m.input == "" && m.confirm == "" && !m.loading)
	for i := range secondary {
		secondary[i].Y += strings.Count(header, "\n")
	}
	header += sections
	if m.height >= 18 {
		header += "\n"
	}
	return header, primary, secondary
}

// Keep the selected field's explanatory text visible even while focus is on a
// different region; moving focus should not rearrange the content.
func (m *Editor) contentFocused() bool {
	return m.area == contentArea
}

func (m *Editor) draftSummary(width int) string {
	profile := m.draft.base(m.view, m.memory).Profiles[0]
	summary := fmt.Sprintf("%d+ GiB profile", profile.MinMemoryGiB)
	if m.draft.dirty() {
		summary += " · unsaved"
	} else {
		summary += " · saved"
	}
	return tui.Style(tui.Muted).Render(ansi.Wrap(summary, width, ""))
}
