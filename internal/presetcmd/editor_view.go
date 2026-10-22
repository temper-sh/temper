package presetcmd

import (
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/catalog"
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
	body, focusStart, focusEnd, rows := m.body(w)
	if m.mode == "editor" && m.input == "" && m.confirm == "" {
		summary := m.draftSummary(w) + "\n\n"
		body = summary + body
		shift := strings.Count(summary, "\n")
		focusStart, focusEnd = focusStart+shift, focusEnd+shift
		for i := range rows {
			rows[i].Y += shift
		}
	}
	footer, actions := m.renderFooter(w)
	frame := tui.Frame{Width: m.width, Height: m.height, Header: header, Footer: footer}
	m.viewport.SetWidth(w)
	m.viewport.SetHeight(frame.BodyHeight())
	m.viewport.SetContent(body)
	if m.focus && (m.contentFocused() || m.confirm != "") {
		m.viewport.EnsureVisible(focusEnd, 0, 0)
		m.viewport.EnsureVisible(focusStart, 0, 0)
		m.focus = false
	}
	if m.input == "" && m.confirm == "" && !m.loading {
		m.hits.Add(int(primaryArea), frame.HeaderTargets(primary))
		m.hits.Add(int(secondaryArea), frame.HeaderTargets(secondary))
	}
	if !m.loading {
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

func (m *Editor) body(width int) (string, int, int, []tui.Target) {
	var lines []string
	var targets []tui.Target
	hit := func(index, start int) {
		targets = append(targets, tui.Target{Index: index, Y: start, Width: width, Height: len(lines) - start})
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
		return strings.Join(lines, "\n"), start, end, targets
	}
	if m.input != "" {
		title, help := "", ""
		switch m.input {
		case "new-name":
			title = "Preset name"
		case "field":
			title = fieldLabel(m.field.Path)
			help = m.field.Help
			if len(m.field.Choices) > 0 {
				help += " Choices: " + strings.Join(m.field.Choices, ", ")
			}
			if m.field.Type == catalog.FieldSwitch {
				help += " Choices: true, false"
			}
		case "form":
			title = m.form[m.cursor].label
		case "filter":
			title = "Filter file names"
			help = "Selected files stay selected when filtered out."
		}
		addTone(title, tui.Heading, false)
		addTone(help, tui.Muted, false)
		m.text.SetWidth(max(1, width-3))
		rowStart := len(lines)
		lines = append(lines, m.text.View())
		hit(0, rowStart)
		return strings.Join(lines, "\n"), 0, len(lines) - 1, targets
	}
	switch m.mode {
	case "manager":
		rows := m.visiblePresets()
		if len(rows) == 0 {
			add("No presets yet. Press n to choose a catalog starting point.", false)
		}
		for i, p := range rows {
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
		rows := m.fields()
		for i, f := range rows {
			rowStart := len(lines)
			switch f.Path {
			case "artifact":
				a := m.currentWeights()
				if len(a.Files) > 0 {
					f.current = a.Files[0].Path
					if len(a.Files) > 1 {
						f.current = fmt.Sprintf("%s (+%d files)", a.Files[0].Path, len(a.Files)-1)
					}
				}
				f.Help = "Choose model weights."
			case "template":
				id, template := m.currentTemplate()
				if id == catalog.BuiltinTemplate {
					f.current = "From model"
				} else if len(template.Files) > 0 {
					f.current = template.Files[0].Path
				}
				f.Help = "Choose the model's template, a local file or Hugging Face."
			}
			selected := i == m.cursor
			if selected {
				start = len(lines)
			}
			row := fieldRow(f, m.draft.file.Fork(), selected && m.contentFocused(), width)
			lines = append(lines, strings.Split(row, "\n")...)
			if selected {
				addTone("  "+f.Help, tui.Muted, false)
				end = len(lines) - 1
			}
			hit(i, rowStart)
		}
	case "form":
		add(strings.Title(m.formKind), false)
		addTone(m.formExplanation(), tui.Muted, false)
		for _, i := range m.visibleFormRows() {
			rowStart := len(lines)
			f := m.form[i]
			v := f.value
			if m.formKind == "template" && i == 0 {
				v = map[string]string{"builtin": "From model", "local": "Local file", "huggingface": "Hugging Face"}[v]
			}
			if v == "" {
				v = "(not set)"
			}
			add(mark(i == m.cursor)+f.label+": "+v, i == m.cursor)
			hit(i, rowStart)
		}
		action := "Look up files"
		if m.formKind == "template" {
			action = "Use template in draft"
		}
		rowStart := len(lines)
		add(mark(m.cursor == len(m.form))+action, m.cursor == len(m.form))
		hit(len(m.form), rowStart)
	case "files":
		add("Select weights files", false)
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
	case "report":
		for _, s := range m.report {
			add(s, false)
		}
	}
	return strings.Join(lines, "\n"), start, end, targets
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
		return "Display name"
	}
	if path == "artifact" {
		return "Weights"
	}
	if path == "template" {
		return "Template"
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

// Wide rows align labels, values and overrides; narrow rows keep the same
// information stacked. Only the selected field adds its explanatory help.
func fieldRow(f editField, fork, focused bool, width int) string {
	element := tui.EditorRow
	if focused {
		element = tui.EditorRowFocused
	}
	p := tui.Colors(element)
	inner := width - 2
	label := terminalText(fieldLabel(f.Path))
	value := terminalText(shownValue(f.current))
	labelStyle := tui.Style(tui.Muted).Background(p.Background)
	if focused {
		labelStyle = labelStyle.Foreground(p.Text).Bold(true)
	}
	valueStyle := tui.Style(element).Bold(focused)
	if width >= 70 {
		label = fmt.Sprintf("%-27s", label)
	} else {
		label += ":"
	}
	row := labelStyle.Render(label) + valueStyle.Render(" "+value)
	if fork && f.override {
		status := "Override"
		if f.Path != "artifact" && f.Path != "template" {
			status += " · origin: " + terminalText(shownValue(f.origin))
		}
		gap := inner - ansi.StringWidth(row) - ansi.StringWidth(status)
		status = tui.Style(tui.Override).Background(p.Background).Render(status)
		if width >= 70 && gap >= 3 {
			row += tui.Style(element).Render(strings.Repeat(" ", gap)) + status
		} else {
			row += "\n" + status
		}
	}
	row = ansi.Wrap(row, inner, "")
	style := tui.Style(element).Width(width).PaddingLeft(2)
	if focused {
		style = style.PaddingLeft(1).Border(lipgloss.Border{Left: "▎"}, false, false, false, true)
	}
	return style.Render(row)
}
