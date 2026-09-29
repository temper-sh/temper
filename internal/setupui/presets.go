package setupui

import (
	"context"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/setup"
)

type PresetOption struct {
	ID, Name, Model, Weights, Engine, Description, AssessmentURL, Tier, Details, Unavailable string
	Recommended                                                                              bool
	Templates                                                                                []string
	Lock                                                                                     catalog.Lock
	Document                                                                                 catalog.Document
}

func presetRecord(lock catalog.Lock) (string, catalog.Layout) {
	for id, record := range lock.Records.Layouts {
		return id, record
	}
	return "", catalog.Layout{}
}

type PresetInput struct {
	Machine       string
	Presets       []PresetOption
	Configuration setup.Configuration
}
type PresetDecision struct {
	Configuration setup.Configuration
	Action        string
}
type compositionPreview struct {
	Seq  int
	Plan setup.Plan
	Err  error
}

type presetFocus uint8

const (
	presetContentFocus presetFocus = iota
	presetFilterFocus
	presetFooterFocus
)

// PresetModel holds a revocable draft. Both sub-tabs operate on its one selected
// map; layout membership never makes another selection behind the user's back.
type PresetModel struct {
	input          PresetInput
	draft          setup.Configuration
	ctx            context.Context
	preview        func(context.Context, setup.Configuration) (setup.Plan, error)
	page           int
	all            bool
	cursor         int
	control        int // Included, startup or default within a layout's preset row.
	focus          presetFocus
	editFocus      presetFocus
	editAction     int
	layout         string
	field          string
	text           textinput.Model
	notice         string
	plan           setup.Plan
	loading        bool
	reviewReady    bool
	reviewErr      error
	previewSeq     int
	previewCancel  context.CancelFunc
	action         int
	width, height  int
	view           viewport.Model
	focusSelection bool
	downloadsOpen  bool
	focusDownloads bool
	hitTargets     []hitTarget
	Decision       PresetDecision
}

func NewPresetModel(ctx context.Context, input PresetInput, preview func(context.Context, setup.Configuration) (setup.Plan, error)) *PresetModel {
	// Clone at the edit boundary; cancel must not modify caller-owned selections.
	raw, _ := input.Configuration.Bytes()
	draft, _ := setup.ParseConfiguration(raw)
	t := textinput.New()
	t.CharLimit = 160
	t.Prompt = "› "
	t.SetVirtualCursor(true)
	styles := textinput.DefaultDarkStyles()
	styles.Focused.Text = lipgloss.NewStyle().Foreground(nightGreen)
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(nightCyan)
	styles.Cursor.Color, styles.Cursor.Blink = nightBlue, false
	t.SetStyles(styles)
	if ctx == nil {
		ctx = context.Background()
	}
	m := &PresetModel{input: input, draft: draft, ctx: ctx, preview: preview, text: t, width: 100, height: 35, view: viewport.New(), focusSelection: true}
	m.view.SoftWrap = false // Each card wraps before placement in the viewport.
	if !slices.ContainsFunc(input.Presets, func(p PresetOption) bool { return p.Recommended }) {
		m.all = true
	}
	return m
}
func (m *PresetModel) Init() tea.Cmd { return nil }
func (m *PresetModel) visible() []PresetOption {
	var result []PresetOption
	for _, p := range m.input.Presets {
		if m.all || p.Recommended {
			result = append(result, p)
		}
	}
	return result
}
func (m *PresetModel) layoutIDs() []string {
	var ids []string
	for id := range m.draft.Layouts {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
func (m *PresetModel) presetIDs() []string {
	var ids []string
	for id := range m.draft.Presets {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
func (m *PresetModel) selectedOption() (PresetOption, bool) {
	rows := m.visible()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return PresetOption{}, false
	}
	return rows[m.cursor], true
}
func (m *PresetModel) count() int {
	if m.page == 0 {
		return len(m.visible())
	}
	if m.layout != "" {
		return len(m.presetIDs())
	}
	return len(m.layoutIDs())
}
func (m *PresetModel) edit(field, value string) tea.Cmd {
	m.editFocus, m.editAction = m.focus, m.action
	m.focus, m.action = presetContentFocus, 0
	m.field = field
	m.notice = ""
	m.focusSelection = true
	m.view.GotoTop()
	m.text.SetValue(value)
	m.text.CursorEnd()
	return m.text.Focus()
}

func (m *PresetModel) toggleMember() {
	if m.page == 0 {
		p, ok := m.selectedOption()
		if !ok {
			return
		}
		if _, selected := m.draft.Presets[p.ID]; selected {
			if refs := m.draft.References(p.ID); len(refs) > 0 {
				m.notice = "Included in " + strings.Join(refs, ", ") + ". Remove those memberships, startup selections and defaults first."
				return
			}
			delete(m.draft.Presets, p.ID)
		} else {
			m.draft.Presets[p.ID] = setup.Preset{Name: p.Name, Lock: p.Lock}
		}
	} else if m.layout != "" {
		ids := m.presetIDs()
		if m.cursor >= len(ids) {
			return
		}
		id := ids[m.cursor]
		l := m.draft.Layouts[m.layout]
		if i := slices.Index(l.Presets, id); i >= 0 {
			l.Presets = slices.Delete(l.Presets, i, i+1)
			if slices.Contains(l.Startup, id) || l.Default == id {
				m.notice = "Membership removed. Clear its startup/default choice explicitly before saving."
			}
		} else {
			l.Presets = append(l.Presets, id)
		}
		m.draft.Layouts[m.layout] = l
	}
}
func (m *PresetModel) loadingChoice(key string) {
	ids := m.presetIDs()
	if m.cursor >= len(ids) {
		return
	}
	id := ids[m.cursor]
	l := m.draft.Layouts[m.layout]
	if key == "d" {
		if l.Default == id {
			l.Default = ""
		} else if slices.Contains(l.Presets, id) {
			l.Default = id
		} else {
			m.notice = "Include this preset before setting it as default."
		}
	} else {
		if i := slices.Index(l.Startup, id); i >= 0 {
			l.Startup = slices.Delete(l.Startup, i, i+1)
		} else if slices.Contains(l.Presets, id) {
			l.Startup = append(l.Startup, id)
		} else {
			m.notice = "Include this preset before selecting startup loading."
		}
	}
	m.draft.Layouts[m.layout] = l
}
func (m *PresetModel) acceptEdit() {
	value := strings.TrimSpace(m.text.Value())
	field := m.field
	m.notice = ""
	switch field {
	case "new":
		id := strings.Trim(strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
				return r
			}
			if unicode.IsSpace(r) || r == '-' {
				return '-'
			}
			return -1
		}, strings.ToLower(value)), "-")
		for strings.Contains(id, "--") {
			id = strings.ReplaceAll(id, "--", "-")
		}
		if id == "" {
			m.notice = "Choose a name containing letters or numbers."
			return
		}
		if _, ok := m.draft.Layouts[id]; ok {
			m.notice = "That layout ID already exists."
			return
		}
		m.draft.Layouts[id] = setup.Layout{Name: value, Presets: []string{}, Startup: []string{}, IdleSeconds: 1800}
		m.cursor = slices.Index(m.layoutIDs(), id)
	case "rename":
		if value == "" {
			m.notice = "Name cannot be blank."
			return
		}
		id := m.layoutIDs()[m.cursor]
		l := m.draft.Layouts[id]
		l.Name = value
		m.draft.Layouts[id] = l
	case "idle":
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			m.notice = "Idle seconds must be positive."
			return
		}
		l := m.draft.Layouts[m.layout]
		l.IdleSeconds = n
		m.draft.Layouts[m.layout] = l
	case "context":
		p, ok := m.selectedOption()
		if !ok {
			return
		}
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			m.notice = "Context must be a positive token count."
			return
		}
		template := ""
		if selected, ok := m.draft.Presets[p.ID]; ok {
			_, record := presetRecord(selected.Lock)
			template = strings.Join(record.Patches, "")
			if template == "" {
				template = "builtin"
			}
		}
		if !m.customize(p, template, n) {
			return
		}
	}
	m.field = ""
	m.text.Blur()
	m.focus, m.action = m.editFocus, m.editAction
	if field == "new" {
		m.focus = presetContentFocus
	}
}
func (m *PresetModel) customize(p PresetOption, template string, window int) bool {
	id, _ := presetRecord(p.Lock)
	lock, err := catalog.CompilePreset(p.Document, id, template, window, p.Lock.Target)
	if err != nil {
		m.notice = err.Error()
		return false
	}
	name := strings.TrimSuffix(p.Name, " (customized)") + " (customized)"
	m.draft.Presets[p.ID] = setup.Preset{Name: name, Lock: lock}
	m.notice = "Preset customized. Review context and memory fit before preparing."
	if refs := m.draft.References(p.ID); len(refs) > 0 {
		m.notice += " Updated in layouts: " + strings.Join(refs, ", ") + "."
	}
	return true
}
func (m *PresetModel) cycleTemplate() {
	p, ok := m.selectedOption()
	if !ok {
		return
	}
	options := append([]string{"builtin"}, p.Templates...)
	current := "builtin"
	_, record := presetRecord(p.Lock)
	window := record.ContextWindowTokens
	if s, ok := m.draft.Presets[p.ID]; ok {
		_, record = presetRecord(s.Lock)
		window = record.ContextWindowTokens
	}
	if len(record.Patches) > 0 {
		current = record.Patches[0]
	}
	m.customize(p, options[(slices.Index(options, current)+1)%len(options)], window)
}

func RunPresets(ctx context.Context, in io.Reader, out io.Writer, input PresetInput, preview func(context.Context, setup.Configuration) (setup.Plan, error)) (PresetDecision, error) {
	m := NewPresetModel(ctx, input, preview)
	defer m.cancelPreview()
	result, err := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(out), tea.WithContext(ctx)).Run()
	if err != nil {
		return PresetDecision{}, err
	}
	return result.(*PresetModel).Decision, nil
}
