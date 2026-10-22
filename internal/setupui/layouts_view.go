package setupui

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/tui"
)

type layoutAction struct {
	id, label string
	kind      tui.Element
	disabled  bool
}

func (m *LayoutModel) actions() []layoutAction {
	if m.field != "" {
		return []layoutAction{{"apply", "Apply", tui.PrimaryAction, false}, {"back", "Cancel", tui.QuietAction, false}}
	}
	switch m.mode {
	case "confirm":
		keep, accept := "Keep editing", "Discard"
		if m.confirm == "delete" {
			keep, accept = "Keep layout", "Delete"
		} else if m.confirm == "member" {
			keep, accept = "Keep included", "Exclude"
		}
		return []layoutAction{{"back", keep, tui.QuietAction, false}, {"confirm", accept, tui.DangerAction, false}}
	case "review":
		label := "Save"
		if m.dry {
			label = "Check save"
		}
		return []layoutAction{{"save", label, tui.PrimaryAction, false}, {"back", "Back", tui.QuietAction, false}}
	case "edit":
		buttons := []layoutAction{{"review", "Review", tui.PrimaryAction, false}}
		if m.page == 0 {
			id := m.selectedPreset()
			included := slices.Contains(m.draft.Layouts[m.id].Presets, id)
			label := "Include"
			if included {
				label = "Exclude"
			}
			buttons = append(buttons, layoutAction{"member", label, tui.Action, id == ""},
				layoutAction{"preload", "Preload", tui.Action, !included}, layoutAction{"default", "Default", tui.Action, !included})
		} else {
			buttons = append(buttons, layoutAction{"field", "Edit", tui.Action, false})
		}
		return append(buttons, layoutAction{"back", "Back", tui.QuietAction, false})
	default:
		empty := len(m.draft.Layouts) == 0
		if empty {
			return []layoutAction{{"new", "New", tui.PrimaryAction, false}, {"done", "Done", tui.QuietAction, false}}
		}
		return []layoutAction{{"edit", "Edit", tui.PrimaryAction, empty}, {"new", "New", tui.Action, false},
			{"delete", "Delete", tui.DangerAction, empty}, {"done", "Done", tui.QuietAction, false}}
	}
}

func (m *LayoutModel) breadcrumbs() []string {
	labels := []string{"Layouts"}
	if m.mode != "list" && m.id != "" {
		layout, ok := m.draft.Layouts[m.id]
		if !ok {
			layout = m.saved.Layouts[m.id]
		}
		labels = append(labels, layout.Name)
	}
	if m.field == "new" {
		labels = append(labels, "New layout")
	} else if m.mode == "review" {
		labels = append(labels, "Review")
	}
	return labels
}

func (m *LayoutModel) moveArea(back bool) {
	areas := []int{layoutContent, layoutActions}
	if m.mode == "confirm" {
		areas = []int{layoutActions}
	} else if len(m.breadcrumbs()) > 1 {
		areas = append(areas, layoutBreadcrumbs)
		if m.mode == "edit" {
			areas = append(areas, layoutSections)
		}
	}
	delta := 1
	if back {
		delta = -1
	}
	i := slices.Index(areas, m.area)
	m.area = areas[(max(0, i)+delta+len(areas))%len(areas)]
	m.action, m.crumb = 0, 0
}

func layoutText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, ansi.Strip(s))
}

func (m *LayoutModel) View() tea.View {
	if m.width < 28 || m.height < 10 {
		v := tea.NewView(ansi.Truncate("Enlarge terminal to 28 × 10.", m.width, ""))
		v.AltScreen = true
		return v
	}
	w := tui.ContentWidth(m.width)
	badge := ""
	if m.dry {
		badge = "dry run"
	}
	header := tui.Header(tui.Layouts, badge, w) + "\n\n"
	crumb := -1
	if m.area == layoutBreadcrumbs {
		crumb = m.crumb
	}
	header += tui.Breadcrumbs(m.breadcrumbs(), crumb, w) + "\n"
	if m.mode == "edit" && m.field == "" {
		header += tui.Secondary(tui.Layouts, []string{"Presets", "Settings"}, m.page, w, m.area == layoutSections) + "\n"
	}
	header += "\n"
	body, start, end := m.body(w)
	frame := tui.Frame{Width: m.width, Height: m.height, Header: header, Footer: m.footer(w)}
	m.viewport.SetWidth(w)
	m.viewport.SetHeight(frame.BodyHeight())
	m.viewport.SetContent(body)
	if m.area == layoutContent && m.mode != "review" {
		m.viewport.EnsureVisible(end, 0, 0)
		m.viewport.EnsureVisible(start, 0, 0)
	}
	v := tea.NewView(frame.Render(m.viewport.View()))
	v.AltScreen = true
	v.BackgroundColor, v.ForegroundColor = tui.Colors(tui.Canvas).Background, tui.Colors(tui.Body).Text
	return v
}

func (m *LayoutModel) footer(width int) string {
	var parts []string
	if m.notice != "" {
		parts = append(parts, tui.Style(m.noticeTone).Render(ansi.Wrap(layoutText(m.notice), width, "")))
	}
	if m.loading != "" {
		message := "Saving…"
		if m.loading == "review" {
			message = "Checking memory…"
		}
		parts = append(parts, tui.Style(tui.Muted).Render(message))
	}
	parts = append(parts, tui.Style(tui.Divider).Render(strings.Repeat("─", width)))
	buttons := []tui.Button{}
	for i, a := range m.actions() {
		buttons = append(buttons, tui.Button{Label: a.label, Kind: a.kind,
			Focused: m.area == layoutActions && m.action == i, Disabled: a.disabled || m.loading != ""})
	}
	parts = append(parts, tui.Actions(tui.Layouts, buttons, width))
	hints := []tui.Hint{{Key: "↑↓", Action: "move"}, {Key: "Enter", Action: "choose"}, {Key: "Tab", Action: "controls"}, {Key: "Esc", Action: "back"}}
	if m.field != "" {
		hints = []tui.Hint{{Key: "Enter", Action: "apply"}, {Key: "Esc", Action: "cancel"}}
	} else if m.area == layoutActions || m.area == layoutBreadcrumbs || m.area == layoutSections {
		hints = []tui.Hint{{Key: "←→", Action: "choose"}, {Key: "Enter", Action: "open"}, {Key: "Tab", Action: "controls"}, {Key: "Esc", Action: "back"}}
	} else if m.mode == "edit" && m.page == 0 {
		hints = []tui.Hint{{Key: "Space", Action: "toggle"}, {Key: "p", Action: "preload"}, {Key: "d", Action: "default"}, {Key: "Tab", Action: "controls"}}
	} else if m.mode == "review" {
		hints = []tui.Hint{{Key: "↑↓", Action: "scroll"}, {Key: "Tab", Action: "actions"}, {Key: "Esc", Action: "back"}}
	}
	return strings.Join(append(parts, tui.KeyHelp(hints, width)), "\n")
}

func (m *LayoutModel) body(width int) (string, int, int) {
	var lines []string
	start, end := 0, 0
	add := func(text string, tone tui.Element, focus bool) {
		style := tui.Style(tone).Width(width)
		room := width
		if focus && m.area == layoutContent {
			room--
			style = tui.Style(tui.EditorRowFocused).Width(width).Bold(true).
				Border(lipgloss.Border{Left: "▎"}, false, false, false, true)
		}
		wrapped := ansi.Wrap(layoutText(text), room, "")
		if focus {
			start, end = len(lines), len(lines)+strings.Count(wrapped, "\n")
		}
		lines = append(lines, strings.Split(style.Render(wrapped), "\n")...)
	}
	blank := func() { lines = append(lines, "") }
	layout := m.draft.Layouts[m.id]
	if m.field != "" {
		label := "Layout name"
		if m.field == "idle" {
			label = "Idle unload (seconds)"
		}
		add(label, tui.Body, false)
		m.text.SetWidth(max(1, width-3))
		start, end = len(lines), len(lines)
		lines = append(lines, m.text.View())
	} else if m.mode == "confirm" {
		switch m.confirm {
		case "discard":
			add("Discard unsaved layout changes?", tui.Body, false)
		case "delete":
			add("Delete layout “"+m.saved.Layouts[m.id].Name+"”?", tui.Body, false)
			blank()
			add("Selected presets stay available. A running layout keeps serving until you stop or activate it.", tui.Muted, false)
		case "member":
			id := m.selectedPreset()
			add("Exclude “"+m.draft.Presets[id].Name+"”?", tui.Body, false)
			blank()
			add("This also clears its preload and default choices in this layout.", tui.Muted, false)
		}
	} else if m.mode == "list" {
		if len(m.draft.Layouts) == 0 {
			add("No layouts yet. Choose New to create one.", tui.Muted, false)
		}
		for i, id := range m.layoutIDs() {
			l := m.draft.Layouts[id]
			add(l.Name, tui.Body, m.cursor == i)
			add(fmt.Sprintf("  %d presets · %d preload", len(l.Presets), len(l.Startup)), tui.Muted, false)
			blank()
		}
	} else if m.mode == "review" {
		add("Included", tui.Body, false)
		if len(layout.Presets) == 0 {
			add("None", tui.Muted, false)
		}
		for _, id := range layout.Presets {
			add(m.presetLabel(id, true), tui.Body, false)
		}
		blank()
		if len(layout.Startup) == 0 {
			add("Models load on demand; none preload.", tui.Muted, false)
		}
		if layout.Default == "" {
			add("Default model: none", tui.Muted, false)
		}
		add(fmt.Sprintf("Idle unload: %d seconds", layout.IdleSeconds), tui.Body, false)
		blank()
		if m.assessment != nil && len(layout.Presets) > 0 {
			add(setup.LayoutMemory(*m.assessment), tui.Muted, false)
			if m.assessment.MemoryIncomplete {
				add("Memory accounting is incomplete; total runtime fit is unknown.", tui.Warning, false)
			}
			for _, refusal := range m.assessment.Refusals {
				add(refusal, tui.Warning, false)
			}
		} else if m.assessmentError != "" {
			add("Memory check unavailable: "+m.assessmentError, tui.Warning, false)
		}
		blank()
		add("Save changes this layout. Activate it when ready to use the changes.", tui.Muted, false)
	} else if m.page == 1 {
		add("Name: "+layout.Name, tui.Body, m.cursor == 0)
		blank()
		add(fmt.Sprintf("Idle unload (seconds): %d", layout.IdleSeconds), tui.Body, m.cursor == 1)
	} else {
		if len(m.draft.Presets) == 0 {
			add("No selected presets. Select presets through temper configure first.", tui.Muted, false)
		}
		for i, id := range m.presetIDs() {
			add(m.presetLabel(id, false), tui.Body, i == m.cursor)
			blank()
		}
	}
	return strings.Join(lines, "\n"), start, end
}

func (m *LayoutModel) presetLabel(id string, review bool) string {
	layout := m.draft.Layouts[m.id]
	marker := "[ ] "
	if slices.Contains(layout.Presets, id) {
		marker = "[x] "
	}
	if review {
		marker = "• "
	}
	label := marker + m.draft.Presets[id].Name
	if slices.Contains(layout.Startup, id) {
		label += " · preload"
	}
	if layout.Default == id {
		label += " · default"
	}
	return label
}
