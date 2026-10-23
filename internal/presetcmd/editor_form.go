package presetcmd

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/tui"
)

// editorControl projects supported catalog fields into decisions. Named modes
// and raw, possibly invalid text are editor state, never serialized schema.
type editorControl struct {
	field                                 editField
	key, label, kind, value, detail, unit string
	values, labels                        []string
	selected                              int
}

type controlChoice struct{ row, option int }

func (m *Editor) rawValue(f editField) string {
	if value, ok := m.values[f.Path]; ok {
		return value
	}
	return fieldText(f.current)
}

func namedModes(path string) (labels, values []string) {
	switch path {
	case "request_defaults.max_output_tokens":
		return []string{"Remaining context", "Custom"}, []string{"0", "custom"}
	case "request_defaults.reasoning_budget_tokens":
		return []string{"Unrestricted", "No thinking", "Custom"}, []string{"-1", "0", "custom"}
	case "request_defaults.sampling.seed":
		return []string{"Random", "Fixed"}, []string{"", "custom"}
	case "engine_config.controls.threads", "engine_config.controls.threads_batch":
		return []string{"Engine default", "Custom"}, []string{"0", "custom"}
	case "engine_config.prompt_cache_ram_mib":
		return []string{"Off", "Custom"}, []string{"0", "custom"}
	}
	return nil, nil
}

func controlLabel(path string) string {
	labels := map[string]string{
		"engine_config.kv_cache": "KV precision", "engine_config.controls.load_mode": "Weight loading",
		"engine_config.parallel": "Concurrent requests", "engine_config.batch_tokens": "Prompt batch",
		"engine_config.microbatch_tokens": "Physical batch", "engine_config.controls.threads": "Generation",
		"engine_config.controls.threads_batch": "Prompt processing", "engine_config.controls.context_shift": "Drop oldest context when full",
		"engine_config.controls.cache_prompt": "Reuse cached prompts", "engine_config.controls.fit": "Fit settings to available memory",
		"engine_config.prompt_cache_ram_mib": "Prompt cache memory", "engine_config.controls.checkpoint_min_step": "Checkpoint interval",
		"engine_config.controls.cache_reuse": "Minimum reusable chunk", "engine_config.controls.preserve_reasoning": "Keep previous reasoning",
		"speculation.method": "Configuration",
	}
	if label := labels[path]; label != "" {
		return label
	}
	return fieldLabel(path)
}

func controlOption(path, value string) string {
	if path == "engine_config.controls.load_mode" {
		return map[string]string{"auto": "Engine default", "none": "Standard", "mmap": "Memory map", "mlock": "Lock in RAM", "mmap+mlock": "Map + lock", "dio": "Direct I/O"}[value]
	}
	switch value {
	case "cpu", "gpu", "q4", "q8", "f16", "bf16":
		return strings.ToUpper(value)
	case "xhigh":
		return "Extra high"
	}
	if value == "" {
		return "Not set"
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func (m *Editor) controls() []editorControl {
	if m.mode == "form" {
		var controls []editorControl
		for _, i := range m.visibleFormRows() {
			f := m.form[i]
			c := editorControl{key: fmt.Sprintf("source/%s/%d", m.formKind, i), label: f.label, kind: "text", value: f.value, selected: -1,
				field: editField{Field: catalog.Field{Path: fmt.Sprint(i)}}}
			if len(f.choices) > 0 {
				c.kind, c.values, c.labels, c.selected = "choice", f.choices, slices.Clone(f.choices), slices.Index(f.choices, f.value)
				if m.formKind == "template" && i == 0 {
					c.label = "Template source"
					c.labels = []string{"From model", "Local file", "Hugging Face"}
				}
			}
			if m.formKind == "template" && m.form[0].value == "local" && i == 3 {
				c.kind, c.label = "browse", "Local file"
			}
			if f.label == "Repository" {
				c.detail = "Repository name or Hugging Face URL."
			}
			if f.label == "Revision" {
				c.detail = "Branch, tag or commit."
			}
			controls = append(controls, c)
		}
		return controls
	}
	fields := m.fieldsFor(m.page)
	order := []string{"name", "artifact", "template", "context_window_tokens", "compute", "request_defaults.max_output_tokens", "request_defaults.reasoning", "engine_config.controls.reasoning_effort", "request_defaults.reasoning_budget_tokens", "engine_config.controls.preserve_reasoning"}
	if m.page == 1 {
		order = []string{"request_defaults.sampling.temperature", "request_defaults.sampling.top_p", "request_defaults.sampling.top_k", "request_defaults.sampling.min_p", "request_defaults.sampling.repeat_penalty", "request_defaults.sampling.presence_penalty", "request_defaults.sampling.seed"}
	}
	if m.page == 2 {
		order = []string{"engine_config.kv_cache", "engine_config.flash_attention", "engine_config.gpu_layers", "engine_config.controls.fit", "engine_config.controls.load_mode", "engine_config.parallel", "engine_config.batch_tokens", "engine_config.microbatch_tokens", "engine_config.controls.threads", "engine_config.controls.threads_batch", "engine_config.controls.context_shift", "engine_config.controls.cache_prompt", "engine_config.prompt_cache_ram_mib", "engine_config.context_checkpoints", "engine_config.controls.checkpoint_min_step", "engine_config.controls.cache_reuse"}
	}
	slices.SortStableFunc(fields, func(a, b editField) int {
		rank := func(path string) int {
			if i := slices.Index(order, path); i >= 0 {
				return i
			}
			return len(order)
		}
		return rank(a.Path) - rank(b.Path)
	})
	var controls []editorControl
	for _, f := range fields {
		c := editorControl{field: f, key: f.Path, label: controlLabel(f.Path), kind: "text", value: m.rawValue(f), detail: f.Help, selected: -1}
		if help, ok := map[string]string{
			"name": "", "request_defaults.sampling.temperature": "0 chooses the most likely token.",
			"request_defaults.sampling.top_p": "", "request_defaults.sampling.top_k": "0 disables this filter.",
			"request_defaults.sampling.min_p": "", "request_defaults.sampling.repeat_penalty": "1 leaves repetition unchanged.",
			"request_defaults.sampling.presence_penalty": "", "engine_config.gpu_layers": "", "engine_config.controls.fit": "",
			"engine_config.controls.context_shift": "", "engine_config.controls.cache_prompt": "", "engine_config.controls.preserve_reasoning": "",
			"request_defaults.reasoning": "Allow thinking before the answer.",
		}[f.Path]; ok {
			c.detail = help
		}
		if f.Path == "context_window_tokens" || strings.HasSuffix(f.Path, "_tokens") {
			c.unit = "tokens"
		}
		if f.Path == "context_window_tokens" {
			c.detail = "Input and reply together."
			if limit := m.draft.base(m.view, m.memory).ContextLimitTokens; limit > 0 {
				c.detail += fmt.Sprintf(" Maximum %d.", limit)
			}
		}
		if f.Path == "artifact" || f.Path == "template" {
			c.kind = "resource"
			c.detail = ""
		}
		if f.Type == catalog.FieldSwitch || slices.Equal(f.Choices, []string{"on", "off"}) {
			c.kind = "toggle"
			c.values = []string{"false", "true"}
			if f.Type != catalog.FieldSwitch {
				c.values = []string{"off", "on"}
			}
			c.selected = slices.Index(c.values, c.value)
		} else if len(f.Choices) > 0 {
			c.kind, c.values, c.selected = "choice", f.Choices, slices.Index(f.Choices, c.value)
			for _, v := range c.values {
				c.labels = append(c.labels, controlOption(f.Path, v))
			}
		}
		labels, values := namedModes(f.Path)
		if f.Path == "request_defaults.max_output_tokens" && m.draft.base(m.view, m.memory).Config().Kind == "rapid-mlx/v1" {
			labels, values = nil, nil
		}
		if len(labels) > 0 {
			c.kind, c.key, c.labels, c.values, c.unit = "mode", f.Path+"/mode", labels, values, ""
			c.selected = slices.Index(values, c.value)
			if m.customModes[f.Path] || c.selected < 0 && c.value != "" {
				c.selected = len(values) - 1
			}
			if c.selected < 0 {
				c.selected = 0
			}
			controls = append(controls, c)
			if c.values[c.selected] == "custom" {
				c.kind, c.key, c.label, c.detail, c.values, c.labels = "text", f.Path, c.label+" value", "", nil, nil
				if strings.HasSuffix(f.Path, "_tokens") {
					c.unit = "tokens"
				}
				if f.Path == "engine_config.prompt_cache_ram_mib" {
					c.unit = "MiB"
				}
				controls = append(controls, c)
			}
			continue
		}
		controls = append(controls, c)
	}
	return controls
}

func (m *Editor) visibleForm() bool {
	return m.input == "" && m.confirm == "" && (m.mode == "form" || m.mode == "editor" && m.page != 4)
}

func (m *Editor) syncControl(c editorControl) {
	if c.kind != "text" {
		m.text.Blur()
		return
	}
	if m.controlKey != c.key {
		m.controlKey = c.key
		m.text.SetValue(c.value)
		m.text.CursorEnd()
	}
	if m.area == contentArea {
		m.text.Focus()
	} else {
		m.text.Blur()
	}
}

func (m *Editor) controlKeypress(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !m.visibleForm() || m.area != contentArea {
		return nil, false
	}
	controls := m.controls()
	if m.cursor >= len(controls) {
		return nil, false
	}
	c := controls[m.cursor]
	key := msg.String()
	switch key {
	case "up", "down", "tab", "shift+tab", "esc", "ctrl+c", "ctrl+s", "ctrl+r", "pgup", "pgdown":
		return nil, false
	}
	if key == "alt+r" && m.mode == "editor" {
		if err := m.resetEditorField(c.field.Path); err != nil {
			m.notice = err.Error()
		}
		delete(m.values, c.field.Path)
		delete(m.valueErrors, c.field.Path)
		delete(m.customModes, c.field.Path)
		m.clearHiddenInputs()
		m.controlKey = ""
		return nil, true
	}
	if c.kind == "text" {
		if key == "enter" {
			m.moveVertical(1)
			return nil, true
		}
		m.syncControl(c)
		var cmd tea.Cmd
		before := m.text.Value()
		m.text, cmd = m.text.Update(msg)
		if m.text.Value() != before {
			m.changeControlText(c)
		}
		return cmd, true
	}
	if key == "left" || key == "right" || key == "space" || key == "enter" || key == "home" || key == "end" {
		if c.kind == "browse" {
			if key == "enter" || key == "space" {
				m.browseTemplate()
			}
			return nil, true
		}
		if c.kind == "resource" {
			if key == "enter" || key == "space" {
				if c.field.Path == "artifact" {
					m.openWeights()
				} else {
					m.openTemplate()
				}
			}
			return nil, true
		}
		if key == "enter" && c.kind != "toggle" {
			m.moveVertical(1)
			return nil, true
		}
		delta := 1
		if key == "left" {
			delta = -1
		}
		selected := tui.CycleChoice(c.selected, len(c.values), delta)
		if key == "home" {
			selected = 0
		}
		if key == "end" {
			selected = len(c.values) - 1
		}
		if c.selected < 0 && delta < 0 {
			selected = len(c.values) - 1
		}
		m.chooseControl(m.cursor, selected)
		return nil, true
	}
	return nil, false
}

func (m *Editor) changeControlText(c editorControl) {
	value := m.text.Value()
	if m.mode == "form" {
		rows := m.visibleFormRows()
		m.form[rows[m.cursor]].value = value
		return
	}
	if m.values == nil {
		m.values = map[string]string{}
		m.valueErrors = map[string]string{}
	}
	m.values[c.field.Path] = value
	err := validateCustomNumber(c, value)
	if err == nil {
		err = m.setEditorField(c.field.Field, value)
	}
	if err != nil {
		m.valueErrors[c.field.Path] = err.Error()
	} else {
		delete(m.valueErrors, c.field.Path)
	}
}

func validateCustomNumber(c editorControl, value string) error {
	if labels, _ := namedModes(c.field.Path); len(labels) == 0 {
		return nil
	}
	parsed, err := parseField(c.field.Field, value)
	if err != nil {
		return err
	}
	n, ok := parsed.(int)
	if !ok {
		return fmt.Errorf("enter a whole number")
	}
	minimum := 1
	if c.field.Path == "request_defaults.sampling.seed" {
		minimum = 0
	}
	if n < minimum {
		return fmt.Errorf("enter a whole number of at least %d", minimum)
	}
	return nil
}

func (m *Editor) chooseControl(row, option int) {
	controls := m.controls()
	if row < 0 || row >= len(controls) {
		return
	}
	c := controls[row]
	if option < 0 || option >= len(c.values) {
		return
	}
	m.cursor, m.area, m.focus = row, contentArea, true
	value := c.values[option]
	if m.mode == "form" {
		index := m.visibleFormRows()[row]
		if m.formKind == "template" && index == 0 && m.form[0].value != value {
			m.form[3].value = ""
			m.selectedTemplate = nil
		}
		m.form[index].value = value
		m.controlKey = ""
		return
	}
	if m.values == nil {
		m.values = map[string]string{}
		m.valueErrors = map[string]string{}
	}
	if m.customModes == nil {
		m.customModes = map[string]bool{}
	}
	if c.kind == "mode" && value == "custom" {
		m.customModes[c.field.Path] = true
		if slices.Contains(c.values, c.value) || c.value == "" {
			m.values[c.field.Path] = ""
			m.valueErrors[c.field.Path] = "Enter a value."
		}
	} else {
		var err error
		if c.kind == "mode" && value == "" {
			err = m.draft.setValue(c.field.Path, nil, m.memory)
		} else {
			err = m.setEditorField(c.field.Field, value)
		}
		if err != nil {
			m.notice = err.Error()
			return
		}
		delete(m.values, c.field.Path)
		delete(m.valueErrors, c.field.Path)
		delete(m.customModes, c.field.Path)
		m.clearHiddenInputs()
	}
	m.controlKey = ""
}

// A controlling choice can remove a dependent input. Its invalid text must not
// leave an invisible Save blocker; valid authored values remain in the draft.
func (m *Editor) clearHiddenInputs() {
	visible := map[string]bool{}
	for page := range 4 {
		for _, f := range m.fieldsFor(page) {
			visible[f.Path] = true
		}
	}
	for path := range m.values {
		if !visible[path] {
			delete(m.values, path)
			delete(m.valueErrors, path)
			delete(m.customModes, path)
		}
	}
}

func (m *Editor) firstFormError() string {
	for _, path := range sortedKeys(m.valueErrors) {
		if problem := m.valueErrors[path]; problem != "" {
			return controlLabel(path) + ": " + problem
		}
	}
	return ""
}
func (m *Editor) formErrors() bool { return m.firstFormError() != "" }

func (m *Editor) clearFormInputs() {
	m.values = nil
	m.valueErrors = nil
	m.customModes = nil
	m.controlKey = ""
}

func controlGroup(path string) string {
	switch path {
	case "request_defaults.max_output_tokens":
		return "Response"
	case "engine_config.kv_cache":
		return "Memory"
	case "engine_config.parallel":
		return "Processing"
	case "engine_config.controls.threads":
		return "CPU threads"
	case "engine_config.controls.context_shift":
		return "Context and cache"
	}
	return ""
}

func (m *Editor) formRows(controls []editorControl, width int) [][]int {
	var rows [][]int
	for i := 0; i < len(controls); i++ {
		row := []int{i}
		if width >= 68 && i+1 < len(controls) && m.mode == "editor" {
			a, b := controls[i], controls[i+1]
			pair := m.page == 1 && a.kind == "text" && b.kind == "text" && a.field.Path != "request_defaults.sampling.seed" && b.field.Path != "request_defaults.sampling.seed"
			pair = pair || a.field.Path == "context_window_tokens" && b.field.Path == "compute"
			pair = pair || a.field.Path == "engine_config.kv_cache" && b.field.Path == "engine_config.flash_attention"
			pair = pair || a.field.Path == "engine_config.parallel" && b.field.Path == "engine_config.batch_tokens"
			pair = pair || a.field.Path == "engine_config.context_checkpoints" && b.field.Path == "engine_config.controls.checkpoint_min_step"
			if pair {
				row = append(row, i+1)
				i++
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func (m *Editor) controlBody(width int) (string, int, int, []tui.Target, []tui.Target) {
	var lines []string
	var targets, choices []tui.Target
	m.pointerChoices = nil
	start, end := 0, 0
	controls := m.controls()
	for _, row := range m.formRows(controls, width) {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		if row[0] > 0 && controls[row[0]].kind == "text" && controls[row[0]-1].field.Path == controls[row[0]].field.Path {
			// A custom number belongs directly to its preceding mode selector.
		} else if m.mode == "editor" {
			if group := controlGroup(controls[row[0]].field.Path); group != "" {
				lines = append(lines, tui.Style(tui.Heading).Render(group))
			}
		}
		y := len(lines)
		var blocks []string
		for col, i := range row {
			c := controls[i]
			outerWidth := width
			if len(row) == 2 {
				outerWidth = (width - 3) / 2
			}
			width := outerWidth
			x := col * (width + 3)

			focused := i == m.cursor && m.area == contentArea
			if m.mode == "editor" && c.field.override && focused {
				if c.detail != "" {
					c.detail += "\n"
				}
				c.detail += "Override · catalog: " + shownFieldValue(c.field.Path, c.field.origin)
			}
			var block string
			var buttons []tui.Target
			switch c.kind {
			case "browse":
				label := "Browse…"
				detail := "Choose a template from this computer."
				if c.value != "" {
					label = "Choose another file…"
					detail = c.value
				}
				if m.selectedTemplate != nil {
					detail = fmt.Sprintf("%s · %d bytes\nCopied when you Save.", m.selectedTemplate.Name, len(m.selectedTemplate.Content))
				}
				action, _ := tui.ActionsWithTargets(tui.Presets, []tui.Button{{Label: label, Kind: tui.Action, Focused: focused}}, width)
				block = tui.Style(tui.Body).Render(c.label) + "\n" + action + "\n" + tui.Style(tui.Muted).Render(ansi.Wrap(terminalText(detail), width, ""))
			case "resource":
				value, detail := "From model", ""
				if c.field.Path == "artifact" {
					a := m.currentWeights()
					detail = a.HuggingFace().Repo
					if len(a.Files) > 0 {
						value = a.Files[0].Path
						if len(a.Files) > 1 {
							value += fmt.Sprintf(" (+%d files)", len(a.Files)-1)
						}
					}
				} else {
					id, t := m.currentTemplate()
					if id != catalog.BuiltinTemplate && len(t.Files) > 0 {
						value = t.Files[0].Path
						detail = t.HuggingFace().Repo
						if len(t.Sources) > 0 && t.Sources[0].Hub == catalog.HubLocal {
							detail = "Local file"
						}
					}
				}
				action, _ := tui.ActionsWithTargets(tui.Presets, []tui.Button{{Label: "Change…", Kind: tui.Action, Focused: focused}}, width)
				gap := max(1, width-ansi.StringWidth(c.label)-lipgloss.Width(action))
				block = tui.Style(tui.Body).Render(c.label) + strings.Repeat(" ", gap) + action + "\n" + tui.Style(tui.Body).Border(lipgloss.Border{Left: "│"}, false, false, false, true).PaddingLeft(1).Render(ansi.Wrap(terminalText(value), max(1, width-2), ""))
				if detail != "" {
					block += "\n" + tui.Style(tui.Muted).Render(ansi.Wrap(terminalText(detail), width, ""))
				}
			case "toggle":
				block, buttons = tui.FormToggle(tui.Presets, c.label, c.selected == 1, focused, width)
				if c.detail != "" {
					block += "\n" + tui.Style(tui.Muted).Render(ansi.Wrap(c.detail, width, ""))
				}
			case "choice", "mode":
				options, ts := tui.FormChoices(tui.Presets, c.labels, c.selected, width, focused)
				block = tui.Style(tui.Body).Render(c.label) + "\n" + options
				for _, t := range ts {
					t.Y++
					buttons = append(buttons, t)
				}
			default:
				value := terminalText(c.value)
				if i == m.cursor {
					m.syncControl(c)
					if focused {
						value = tui.FormInputView(&m.text, width, c.unit)
					}
				}
				block = (tui.FormField{Label: c.label, Value: value, Unit: c.unit, Detail: terminalText(c.detail), Error: terminalText(m.valueErrors[c.field.Path]), Focused: focused}).Render(tui.Presets, width)
			}
			h := lipgloss.Height(block)
			blocks = append(blocks, lipgloss.NewStyle().Width(width).Render(block))
			if i == m.cursor {
				start, end = y, y+h-1
			}
			targets = append(targets, tui.Target{Index: i, X: x, Y: y, Width: width, Height: h})
			for _, target := range buttons {
				option := target.Index
				if c.kind == "toggle" {
					option = 1
					if c.selected == 1 {
						option = 0
					}
				}
				m.pointerChoices = append(m.pointerChoices, controlChoice{i, option})
				target.Index = len(m.pointerChoices) - 1
				target.X += x
				target.Y += y
				choices = append(choices, target)
			}
		}
		block := blocks[0]
		if len(blocks) == 2 {
			block = lipgloss.JoinHorizontal(lipgloss.Top, blocks[0], "   ", blocks[1])
		}
		lines = append(lines, strings.Split(block, "\n")...)
	}

	return strings.Join(lines, "\n"), start, end, targets, choices
}
