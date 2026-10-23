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
		return []layoutAction{{"back", "Cancel", tui.QuietAction, false}, {"apply", "Apply", tui.PrimaryAction, false}}
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
		save := "Save"
		if m.dry {
			save = "Check save"
		}
		return []layoutAction{{"back", "Discard", tui.QuietAction, false}, {"review", "Review", tui.Action, false}, {"save", save, tui.PrimaryAction, false}}
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

func layoutText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, ansi.Strip(s))
}

func (m *LayoutModel) pageExplanation(width int) string {
	if m.width < 50 || m.height < 18 {
		return ""
	}
	title, explanation := "Choose a layout", "Layouts group the presets you want available together. Open one to edit it, or create a new layout."
	switch {
	case m.field == "new":
		title, explanation = "Create a layout", "Name a new layout, then choose its presets. It starts empty; Save keeps your changes."
	case m.field == "name":
		title, explanation = "Rename this layout", "Change the name shown in the layout list. Apply updates the draft; Save keeps it."
	case m.field == "idle":
		title, explanation = "Set idle unloading", "Unload models after this many seconds without use, including preloaded models. They load again when requested."
	case m.mode == "confirm":
		title, explanation = "Confirm the change", "Choose whether to keep your current choices or apply the change described below."
	case m.mode == "review":
		title, explanation = "Review this layout", "Check its presets, loading choices and memory needs. Save keeps the layout; activate it to use the changes."
		if m.dry {
			explanation = "Check its presets, loading choices and memory needs. Check save validates the changes without saving them."
		}
	case m.mode == "edit" && m.page == 1:
		title, explanation = "Layout settings", "Set the layout name and idle timeout. Idle models unload to free memory, then load again when requested."
	case m.mode == "edit":
		title, explanation = "Choose how presets are used", "Included: available in this layout. Load on activation: preload into memory; otherwise load on demand. Default: answer requests for the “default” model."
	}
	return tui.PageExplanation(title, explanation, width)
}

func (m *LayoutModel) View() tea.View {
	m.hits = tui.HitMap{}
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
	if explanation := m.pageExplanation(w); explanation != "" {
		header += explanation + "\n\n"
	}
	crumb := -1
	if m.area == layoutBreadcrumbs {
		crumb = m.crumb
	}
	path, primary := tui.BreadcrumbsWithTargets(m.breadcrumbs(), crumb, w)
	for i := range primary {
		primary[i].Y += strings.Count(header, "\n")
	}
	header += path + "\n"
	var secondary []tui.Target
	if m.mode == "edit" && m.field == "" {
		var sections string
		sections, secondary = tui.SecondaryWithTargets(tui.Layouts, []string{"Presets", "Settings"}, m.page, w, m.area == layoutSections)
		for i := range secondary {
			secondary[i].Y += strings.Count(header, "\n")
		}
		header += sections + "\n"
	}
	header += "\n"
	body, start, end, rows, controls := m.body(w)
	footer, actions := m.footer(w)
	frame := tui.Frame{Width: m.width, Height: m.height, Header: header, Footer: footer}
	m.viewport.SetWidth(w)
	m.viewport.SetHeight(frame.BodyHeight())
	m.viewport.SetContent(body)
	if m.focus && m.area == layoutContent && m.mode != "review" {
		if m.mode == "edit" && m.page == 0 && end-start >= frame.BodyHeight() {
			for _, target := range controls {
				if target.Index == m.cursor*len(layoutPresetActions)+m.control {
					start, end = target.Y, target.Y
					break
				}
			}
		}
		m.viewport.EnsureVisible(end, 0, 0)
		m.viewport.EnsureVisible(start, 0, 0)
	}
	m.focus = false
	if m.field == "" && m.mode != "confirm" && m.loading == "" {
		m.hits.Add(int(layoutBreadcrumbs), frame.HeaderTargets(primary))
		m.hits.Add(int(layoutSections), frame.HeaderTargets(secondary))
	}
	if m.loading == "" {
		m.hits.Add(int(layoutPresetControls), frame.BodyTargets(controls, m.viewport.YOffset()))
		m.hits.Add(int(layoutContent), frame.BodyTargets(rows, m.viewport.YOffset()))
	}
	m.hits.Add(int(layoutActions), frame.FooterTargets(actions))
	v := tea.NewView(frame.Render(m.viewport.View()))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.BackgroundColor, v.ForegroundColor = tui.Colors(tui.Canvas).Background, tui.Colors(tui.Body).Text
	return v
}

func (m *LayoutModel) footer(width int) (string, []tui.Target) {
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
	if m.mode == "edit" && m.field == "" {
		status := "No unsaved changes"
		if m.dirty() {
			status = "Unsaved changes"
		}
		parts = append(parts, tui.Style(tui.Muted).Render(status))
	}
	buttons := []tui.Button{}
	for i, a := range m.actions() {
		buttons = append(buttons, tui.Button{Label: a.label, Kind: a.kind,
			Focused: m.area == layoutActions && m.action == i, Disabled: a.disabled || m.loading != ""})
	}
	actions, targets := tui.ActionsWithTargets(tui.Layouts, buttons, width)
	for i := range targets {
		targets[i].Y += lipgloss.Height(strings.Join(parts, "\n"))
	}
	parts = append(parts, actions)
	hints := []tui.Hint{{Key: "↑↓", Action: "move"}, {Key: "Enter", Action: "choose"}, {Key: "Tab", Action: "controls"}, {Key: "Esc", Action: "back"}}
	if m.field != "" {
		hints = tui.InputHints(m.area == layoutActions)
	} else if m.area == layoutActions || m.area == layoutBreadcrumbs || m.area == layoutSections {
		hints = tui.FocusHints(m.area)
	} else if m.mode == "edit" && m.page == 0 {
		hints = []tui.Hint{{Key: "↑↓", Action: "move"}, {Key: "←→", Action: "option"}, {Key: "Enter", Action: "toggle"}, {Key: "Esc", Action: "back"}}
	} else if m.mode == "edit" && m.page == 1 {
		hints = []tui.Hint{{Key: "↑↓", Action: "controls"}, {Key: "←→", Action: "choose"}, {Key: "Enter", Action: "use"}}
	} else if m.mode == "review" {
		hints = []tui.Hint{{Key: "↑↓", Action: "scroll"}, {Key: "Tab", Action: "actions"}, {Key: "Esc", Action: "back"}}
	}
	return strings.Join(append(parts, tui.KeyHelp(hints, width)), "\n"), targets
}

func (m *LayoutModel) body(width int) (string, int, int, []tui.Target, []tui.Target) {
	var lines []string
	var targets []tui.Target
	var controls []tui.Target
	hit := func(index, start int) {
		targets = append(targets, tui.Target{Index: index, Y: start, Width: width, Height: len(lines) - start})
	}
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
		hit(0, start)
	} else if m.mode == "confirm" {
		switch m.confirm {
		case "discard":
			add("Discard unsaved layout changes?", tui.Body, false)
		case "delete":
			add("Delete layout “"+m.saved.Layouts[m.id].Name+"”?", tui.Body, false)
			blank()
			add("Saved presets stay available. A running layout keeps serving until you stop or activate it.", tui.Muted, false)
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
			rowStart := len(lines)
			l := m.draft.Layouts[id]
			add(l.Name, tui.Body, m.cursor == i)
			add(fmt.Sprintf("  %d presets · %d preload", len(l.Presets), len(l.Startup)), tui.Muted, false)
			hit(i, rowStart)
			blank()
		}
	} else if m.mode == "review" {
		add("Included", tui.Body, false)
		if len(layout.Presets) == 0 {
			add("None", tui.Muted, false)
		}
		for _, id := range layout.Presets {
			add(m.presetLabel(id), tui.Body, false)
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
		return m.settingsBody(width)
	} else {
		if len(m.presetIDs()) == 0 {
			add("No saved presets. Create one with temper preset, then reopen this editor.", tui.Muted, false)
		}
		for i, id := range m.presetIDs() {
			rowStart := len(lines)
			focused := i == m.cursor && m.area == layoutContent
			card, switches := m.presetCard(id, focused, width)
			lines = append(lines, strings.Split(card, "\n")...)
			if i == m.cursor {
				start, end = rowStart+1, rowStart+switches[len(switches)-1].Y
			}
			for _, target := range switches {
				target.Y += rowStart
				target.Index += i * len(layoutPresetActions)
				controls = append(controls, target)
			}
			hit(i, rowStart)
		}
	}
	return strings.Join(lines, "\n"), start, end, targets, controls
}

func (m *LayoutModel) presetLabel(id string) string {
	layout := m.draft.Layouts[m.id]
	preset := m.draft.Presets[id]
	name := preset.Name
	if name == "" {
		name = id
	}
	label := "• " + name
	if preset.UserPreset != "" {
		label += " · untested"
	}
	if slices.Contains(layout.Startup, id) {
		label += " · preload"
	}
	if layout.Default == id {
		label += " · default"
	}
	return label
}
