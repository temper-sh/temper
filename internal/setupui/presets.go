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
	ModelBytes                                                                               int64
	// Group and Section place the preset in a tab and under a heading.
	Group, Section string
	// Choices are the user's saved settings for this preset.
	Choices setup.Choices
	// Lock is empty when the catalog preset cannot be built on this Temper.
	Lock catalog.Lock
}

// buildable reports whether the option has a lock to select or customize.
func (p PresetOption) buildable() bool { return len(p.Lock.Records.Presets) > 0 }

func presetRecord(lock catalog.Lock) (string, catalog.Preset) {
	for id, record := range lock.Records.Presets {
		return id, record
	}
	return "", catalog.Preset{}
}

type PresetInput struct {
	Machine string
	Presets []PresetOption
	// Groups are the catalog's tabs in order; each may have sections.
	Groups           []catalog.Group
	Configuration    setup.Configuration
	NewConfiguration bool
	Assess           func(catalog.Lock) (setup.PresetPlan, error)
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

// PresetModel holds a revocable draft. All sub-tabs operate on its one selected
// map; layout membership never makes another selection behind the user's back.
type PresetModel struct {
	input          PresetInput
	assessments    map[string]setup.PresetPlan
	draft          setup.Configuration
	ctx            context.Context
	preview        func(context.Context, setup.Configuration) (setup.Plan, error)
	page           int
	group          string
	cursor         int
	control        int // Included, startup or default within a layout's preset row.
	focus          presetFocus
	editFocus      presetFocus
	editAction     int
	layout         string
	suggestLayout  bool // A fresh wizard seeds its layout once, after preset selection.
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
	m := &PresetModel{input: input, assessments: map[string]setup.PresetPlan{}, draft: draft, ctx: ctx, preview: preview, text: t, width: 100, height: 35, view: viewport.New(), focusSelection: true, suggestLayout: input.NewConfiguration}
	m.group = m.groups()[0].ID
	m.view.SoftWrap = false // Each card wraps before placement in the viewport.
	return m
}
func (m *PresetModel) Init() tea.Cmd { return nil }
func (m *PresetModel) visible() []PresetOption {
	var result []PresetOption
	for _, p := range m.input.Presets {
		if p.Group == m.group {
			if selected, ok := m.draft.Presets[p.ID]; ok {
				if assessment, ok := m.assessments[p.ID]; ok && catalog.SameExecution(assessment.Lock, selected.Lock) {
					p.ModelBytes = assessment.ModelBytes
					p.Unavailable = strings.Join(assessment.Refusals, "; ")
				}
			}
			result = append(result, p)
		}
	}
	// Keep each section together in the catalog's section order, retaining
	// the machine/tier/catalog order within it.
	sections := m.currentGroup().Sections
	rank := func(id string) int {
		return slices.IndexFunc(sections, func(s catalog.Section) bool { return s.ID == id })
	}
	slices.SortStableFunc(result, func(a, b PresetOption) int { return rank(a.Section) - rank(b.Section) })
	return result
}

// groups are the tabs: the catalog's groups, or the built-in Other group.
func (m *PresetModel) groups() []catalog.Group {
	if len(m.input.Groups) == 0 {
		return []catalog.Group{catalog.OtherGroup}
	}
	return m.input.Groups
}

func (m *PresetModel) currentGroup() catalog.Group {
	for _, g := range m.groups() {
		if g.ID == m.group {
			return g
		}
	}
	return m.groups()[0]
}

// section returns a heading of the current group, if the preset names one.
func (m *PresetModel) section(id string) (catalog.Section, bool) {
	for _, s := range m.currentGroup().Sections {
		if s.ID == id {
			return s, true
		}
	}
	return catalog.Section{}, false
}

// reservedKeys are the actions of the preset page; group shortcuts never
// shadow them.
var reservedKeys = []string{"c", "d", "i", "j", "k", "n", "p", "q", "r", "t", "x"}

// groupShortcut maps a key to the group whose name starts with it, when no
// other group starts with the same letter. Left/Right always work.
func (m *PresetModel) groupShortcut(key string) (string, bool) {
	if slices.Contains(reservedKeys, key) {
		return "", false
	}
	match, count := "", 0
	for _, g := range m.groups() {
		if first, _ := firstLetter(g.DisplayName); first == key {
			match, count = g.ID, count+1
		}
	}
	return match, count == 1
}

func firstLetter(name string) (string, bool) {
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) {
			return string(r), true
		}
	}
	return "", false
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
		} else if !p.buildable() {
			m.notice = "Unavailable: " + p.Unavailable
		} else {
			m.draft.Presets[p.ID] = setup.Preset{Name: p.Name, Choices: p.Choices, Lock: p.Lock}
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
		if !m.customize(p, n, "") {
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
func (m *PresetModel) customize(p PresetOption, window int, compute catalog.ComputeDevice) bool {
	base, choices := p.Lock, p.Choices
	if selected, ok := m.draft.Presets[p.ID]; ok {
		base, choices = selected.Lock, selected.Choices
	}
	if len(base.Records.Presets) == 0 {
		m.notice = "Unavailable: " + p.Unavailable
		return false
	}
	id, record := presetRecord(base)
	if compute != "" {
		record.Compute = compute
		choices.Compute = compute
	}
	if window > 0 {
		choices.ContextWindow = window
	}
	// Retain all current settings; the lock holds only the selected profile.
	document := base.Records
	document.Presets = map[string]catalog.Preset{id: record}
	lock, err := catalog.CompilePreset(document, id, 0, window, base.Target)
	if err != nil {
		m.notice = err.Error()
		return false
	}
	if m.input.Assess != nil {
		assessment, err := m.input.Assess(lock)
		if err != nil {
			m.notice = err.Error()
			return false
		}
		m.assessments[p.ID] = assessment
	}
	name := strings.TrimSuffix(p.Name, " (customized)") + " (customized)"
	m.draft.Presets[p.ID] = setup.Preset{Name: name, Choices: choices, Lock: lock}
	m.notice = "Preset customized. Review context and memory fit before preparing."
	if refs := m.draft.References(p.ID); len(refs) > 0 {
		m.notice += " Updated in layouts: " + strings.Join(refs, ", ") + "."
	}
	return true
}
func (m *PresetModel) cycleCompute() {
	p, ok := m.selectedOption()
	if !ok {
		return
	}
	lock := p.Lock
	if selected, ok := m.draft.Presets[p.ID]; ok {
		lock = selected.Lock
	}
	_, record := presetRecord(lock)
	if len(record.ComputeOptions) < 2 {
		return
	}
	next := record.ComputeOptions[(slices.Index(record.ComputeOptions, record.Compute)+1)%len(record.ComputeOptions)]
	m.customize(p, 0, next)
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
