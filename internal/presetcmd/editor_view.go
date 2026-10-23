package presetcmd

import (
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/tui"
	"github.com/temper-sh/temper/internal/userpreset"
)

var editorSections = []string{"General", "Sampling", "Engine", "Speculation", "Review"}

// terminalText strips terminal controls from catalog names, file paths and
// provider errors before applying Temper's own styling.
func terminalText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, ansi.Strip(s))
}

func (m *Editor) View() tea.View {
	m.hits = tui.HitMap{}
	if m.width < 28 || m.height < 10 {
		v := tea.NewView(ansi.Truncate("Enlarge terminal to 28 × 10. q exits.", m.width, ""))
		v.AltScreen = true
		return v
	}
	w := tui.ContentWidth(m.width)
	header, primary, secondary := m.renderHeader(w)
	body, focusStart, focusEnd, rows, choices := m.body(w)
	if m.mode == "editor" && !m.visibleForm() && m.input == "" && m.confirm == "" {
		summary := m.draftSummary(w) + "\n\n"
		body = summary + body
		shift := strings.Count(summary, "\n")
		focusStart, focusEnd = focusStart+shift, focusEnd+shift
		for i := range rows {
			rows[i].Y += shift
		}
		for i := range choices {
			choices[i].Y += shift
		}
	}
	footer, actions := m.renderFooter(w)
	frame := tui.Frame{Width: m.width, Height: m.height, Header: header, Footer: footer}
	m.viewport.SetWidth(w)
	m.viewport.SetHeight(frame.BodyHeight())
	m.viewport.SetContent(body)
	if m.focus && (m.contentFocused() || m.input != "" || m.confirm != "") {
		m.viewport.EnsureVisible(focusEnd, 0, 0)
		m.viewport.EnsureVisible(focusStart, 0, 0)
		m.focus = false
	}
	if m.input == "" && m.confirm == "" && !m.loading {
		m.hits.Add(int(primaryArea), frame.HeaderTargets(primary))
		m.hits.Add(int(secondaryArea), frame.HeaderTargets(secondary))
	}
	if !m.loading {
		m.hits.Add(int(choiceArea), frame.BodyTargets(choices, m.viewport.YOffset()))
		m.hits.Add(int(contentArea), frame.BodyTargets(rows, m.viewport.YOffset()))
	}
	m.hits.Add(int(actionsArea), frame.FooterTargets(actions))
	content := frame.Render(m.viewport.View())
	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.BackgroundColor, v.ForegroundColor = tui.Colors(tui.Canvas).Background, tui.Colors(tui.Canvas).Text
	return v
}

func (m *Editor) body(width int) (string, int, int, []tui.Target, []tui.Target) {
	if m.mode == "local-files" && m.input == "" {
		return m.localFilesBody(width)
	}
	if m.visibleForm() {
		return m.controlBody(width)
	}
	var lines []string
	var targets []tui.Target
	var choices []tui.Target
	hit := func(index, start int) {
		targets = append(targets, tui.Target{Index: index, Y: start, Width: width, Height: len(lines) - start})
	}
	separate := func(index int) {
		if index > 0 {
			lines = append(lines, "")
		}
	}
	start, end := 0, 0
	addTone := func(s string, element tui.Element, focused bool) {
		focused = focused && (m.contentFocused() || m.confirm != "")
		lineWidth := width
		if focused {
			if element == tui.Body {
				element = tui.EditorRowFocused
			}
			lineWidth--
			if strings.HasPrefix(s, "› ") {
				s = " " + strings.TrimPrefix(s, "› ")
			}
		}
		wrapped := ansi.Wrap(terminalText(s), lineWidth, "")
		style := tui.Style(element).Width(width).Bold(element == tui.Heading)
		if focused {
			start = len(lines)
			end = start + strings.Count(wrapped, "\n")
			style = style.Bold(true).Border(lipgloss.Border{Left: "▎"}, false, false, false, true)
		}
		wrapped = style.Render(wrapped)
		lines = append(lines, strings.Split(wrapped, "\n")...)
	}
	add := func(s string, focused bool) { addTone(s, tui.Body, focused) }
	if m.confirm != "" {
		choices := []string{"Keep editing", "Discard", "Save"}
		switch m.confirm {
		case "leave":
			add("Unsaved changes. Choose what to do with this draft.", false)
		case "delete":
			add("Delete "+userpreset.Path(m.options.Root, m.deleteID)+"?", false)
			add(m.references(m.deleteID), false)
			add("Only this authoring file is removed. Templates, weights and exact locks remain.", false)
			choices = []string{"Keep file", "Delete file"}
		case "undo":
			add("Restore the fork from before conversion? Changes made after conversion will be discarded.", false)
			choices = []string{"Keep editing", "Undo conversion"}
		}
		for i, s := range choices {
			separate(i)
			rowStart := len(lines)
			element := tui.Body
			if m.confirm == "delete" && i == 1 {
				element = tui.DangerAction
				if i == m.confirmCursor {
					element = tui.DangerActionFocused
				}
			}
			addTone(mark(i == m.confirmCursor)+s, element, i == m.confirmCursor)
			hit(i, rowStart)
		}
		return strings.Join(lines, "\n"), start, end, targets, nil
	}
	if m.input != "" {
		title, help := "", ""
		switch m.input {
		case "new-name":
			title = "Preset name"
		case "browse-path":
			title, help = "Directory", "Enter a folder path to browse."
		case "filter":
			title = "Filter file names"
			if m.pageExplanation(width) == "" {
				help = "Selected files stay selected when filtered out."
			}
		}
		addTone(title, tui.Heading, false)
		addTone(help, tui.Muted, false)
		m.text.Prompt = "› "
		m.text.SetWidth(max(1, width-3))
		m.text.SetCursor(m.text.Position())
		rowStart := len(lines)
		lines = append(lines, m.text.View())
		hit(0, rowStart)
		return strings.Join(lines, "\n"), 0, len(lines) - 1, targets, nil
	}
	switch m.mode {
	case "overview":
		return m.overview.render(width), 0, 0, nil, nil
	case "manager":
		rows := m.visiblePresets()
		if len(rows) == 0 {
			add("No presets yet. Press n to choose a catalog starting point.", false)
		}
		for i, p := range rows {
			separate(i)
			rowStart := len(lines)
			name := p.name
			if name == "" {
				name = p.id
			}
			add(mark(i == m.cursor)+name, i == m.cursor)
			addTone("  "+p.id+" · "+p.status, tui.Muted, false)
			hit(i, rowStart)
		}
	case "editor":
		if m.page == 4 {
			for _, s := range m.report {
				add(s, false)
			}
			break
		}
	case "files":
		if m.pageExplanation(width) == "" {
			add("Select weights files", false)
		}
		add("Commit: "+m.repository.Commit, false)
		selected := 0
		for _, yes := range m.files {
			if yes {
				selected++
			}
		}
		add(fmt.Sprintf("%d selected · filter: %s", selected, m.fileFilter), false)
		files := m.visibleFiles()
		if len(files) == 0 {
			add("No matching files. / changes the filter; Escape returns to the repository.", false)
		}
		for i, file := range files {
			separate(i)
			rowStart := len(lines)
			check := "[ ] "
			element := tui.Toggle
			if i == m.cursor {
				element = tui.ToggleFocused
			}
			if m.files[file] {
				check = "[x] "
				element = tui.ToggleOn
				if i == m.cursor {
					element = tui.ToggleOnFocused
				}
			}
			addTone(mark(i == m.cursor)+check+file, element, i == m.cursor)
			hit(i, rowStart)
		}
		if m.needsModelRelationship() {
			lines = append(lines, "", tui.Style(tui.Body).Render("Model relationship"))
			y := len(lines)
			selected := -1
			for i, value := range m.form[2].choices {
				if value == m.form[2].value {
					selected = i
				}
			}
			block, ts := tui.FormChoices(tui.Presets, []string{"Same model", "Different model"}, selected, width, m.cursor == len(files) && m.area == contentArea)
			lines = append(lines, strings.Split(block, "\n")...)
			for _, target := range ts {
				target.Y += y
				choices = append(choices, target)
			}
			if selected == 1 {
				lines = append(lines, strings.Split(ansi.Wrap("Use weights will make this draft an independent copy.", width, ""), "\n")...)
			}
			hit(len(files), y)
			if m.cursor == len(files) {
				start, end = y, len(lines)-1
			}
		}

	case "report":
		for _, s := range m.report {
			add(s, false)
		}
	}
	return strings.Join(lines, "\n"), start, end, targets, choices
}

func mark(focused bool) string {
	if focused {
		return "› "
	}
	return "  "
}
func shownValue(v any) string {
	if v == nil {
		return "(not set)"
	}
	return fieldText(v)
}
func fieldLabel(path string) string {
	if path == "name" {
		return "Name"
	}
	if path == "artifact" {
		return "Weights"
	}
	if path == "template" {
		return "Template"
	}
	switch path {
	case "context_window_tokens":
		return "Context"
	case "request_defaults.max_output_tokens":
		return "Output limit"
	case "request_defaults.reasoning_budget_tokens":
		return "Reasoning budget"
	case "speculation.method":
		return "Speculation"
	case "speculation.max_draft_tokens":
		return "Draft tokens"
	}
	path = strings.TrimPrefix(path, "request_defaults.")
	path = strings.TrimPrefix(path, "sampling.")
	path = strings.TrimPrefix(path, "engine_config.")
	path = strings.TrimPrefix(path, "speculation.")
	path = strings.TrimPrefix(path, "controls.")
	path = strings.TrimPrefix(path, "rapid_mlx.")
	path = strings.TrimPrefix(path, "vllm_metal.")
	label := strings.ReplaceAll(path, "_", " ")
	label = strings.NewReplacer("kv ", "KV ", "gpu ", "GPU ", "cpu ", "CPU ", " mib", " MiB").Replace(label)
	return strings.ToUpper(label[:1]) + label[1:]
}
