package setupui

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/tui"
)

// LayoutInput contains available exact presets, saved layouts and file errors,
// never a catalog. Newly discovered presets remain in memory until placement.
type LayoutInput struct {
	Configuration setup.Configuration
	PresetErrors  map[string]string
	ID            string
	DryRun        bool
}

// LayoutSave publishes composition and captures newly included presets.
// Published remains true for a post-commit durability error; a dry run returns
// false. The command owns the source and configuration revision guards.
type LayoutSave func(context.Context, map[string]setup.Layout) (published bool, err error)
type LayoutPreview func(context.Context, setup.Configuration, string) (setup.LayoutPlan, error)

type layoutJob struct {
	seq       int
	kind      string
	published bool
	plan      setup.LayoutPlan
	err       error
}

// LayoutModel owns a revocable composition draft. Its callbacks are the only
// effect/read edges; it cannot author or rebuild a preset.
type LayoutModel struct {
	ctx                      context.Context
	cancel, jobCancel        context.CancelFunc
	save                     LayoutSave
	preview                  LayoutPreview
	saved, draft             setup.Configuration
	presetErrors             map[string]string
	dry                      bool
	mode, id, field, confirm string
	returnMode, notice       string
	noticeTone               tui.Element
	quitAfter                bool
	page, cursor, action     int
	control                  int // Included, load on activation or default within a preset card.
	area                     tui.FocusArea
	crumb, seq               int
	loading                  string
	width, height            int
	text                     textinput.Model
	viewport                 viewport.Model
	assessment               *setup.LayoutPlan
	assessmentError          string
	focus                    bool
	hits                     tui.HitMap
	settings                 map[string]*layoutSettings
	settingInput             string
}

const (
	layoutContent     = tui.ContentFocus
	layoutActions     = tui.ActionsFocus
	layoutBreadcrumbs = tui.PrimaryFocus
	layoutSections    = tui.SecondaryFocus
)

func NewLayoutModel(ctx context.Context, input LayoutInput, save LayoutSave, preview LayoutPreview) (*LayoutModel, error) {
	raw, err := input.Configuration.Bytes()
	if err != nil {
		return nil, err
	}
	configuration, err := setup.ParseConfiguration(raw)
	if err != nil {
		return nil, err
	}
	if input.ID != "" {
		if _, ok := configuration.Layouts[input.ID]; !ok {
			return nil, fmt.Errorf("unknown saved layout %q", input.ID)
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	m := &LayoutModel{ctx: ctx, cancel: cancel, save: save, preview: preview,
		saved: configuration, draft: configuration, presetErrors: maps.Clone(input.PresetErrors), dry: input.DryRun, mode: "list",
		width: 100, height: 32, viewport: viewport.New(), focus: true,
		text: tui.TextInput(160, tui.Colors(tui.Input).Text, tui.Colors(tui.InputPrompt).Text, tui.Layouts.Colors(tui.Heading).Text)}
	m.draft.Layouts = cloneLayouts(configuration.Layouts)
	if input.ID != "" {
		m.open(input.ID)
	}
	return m, nil
}

func cloneLayouts(layouts map[string]setup.Layout) map[string]setup.Layout {
	result := maps.Clone(layouts)
	for id, layout := range result {
		layout.Presets, layout.Startup = slices.Clone(layout.Presets), slices.Clone(layout.Startup)
		result[id] = layout
	}
	return result
}

func (m *LayoutModel) Init() tea.Cmd { return nil }
func (m *LayoutModel) Close() {
	if m.jobCancel != nil {
		m.jobCancel()
	}
	m.cancel()
}
func (m *LayoutModel) dirty() bool {
	return m.settingsError() != "" || !reflect.DeepEqual(m.saved.Layouts, m.draft.Layouts)
}
func (m *LayoutModel) layoutIDs() []string { return slices.Sorted(maps.Keys(m.draft.Layouts)) }
func (m *LayoutModel) presetIDs() []string {
	ids := slices.Collect(maps.Keys(m.draft.Presets))
	for id := range m.presetErrors {
		if _, ok := m.draft.Presets[id]; !ok {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}
func (m *LayoutModel) selectedPreset() string {
	ids := m.presetIDs()
	if m.cursor < 0 || m.cursor >= len(ids) {
		return ""
	}
	return ids[m.cursor]
}
func (m *LayoutModel) open(id string) {
	m.id, m.mode, m.page, m.cursor, m.area, m.action = id, "edit", 0, 0, layoutContent, 0
	m.control = 0
	m.settingInput = ""
	m.notice = ""
	m.viewport.GotoTop()
}
func (m *LayoutModel) list() {
	m.mode, m.id, m.cursor, m.area, m.action = "list", "", 0, layoutContent, 0
	m.viewport.GotoTop()
}

func (m *LayoutModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	hits := m.hits
	m.hits = tui.HitMap{}
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft || m.width < 28 || m.height < 10 {
			return m, nil
		}
		if area, index, ok := hits.At(msg.X, msg.Y); ok {
			return m, m.click(tui.FocusArea(area), index)
		}
		return m, nil
	case tea.MouseWheelMsg:
		if m.field != "" || m.loading != "" {
			return m, nil
		}
		m.focus = false
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, msg.Width), max(1, msg.Height)
		m.focus = true
		return m, nil
	case layoutJob:
		if msg.seq != m.seq {
			return m, nil
		}
		m.loading = ""
		m.focus = true
		if m.jobCancel != nil {
			m.jobCancel()
			m.jobCancel = nil
		}
		if msg.kind == "review" {
			m.assessment, m.assessmentError = nil, ""
			if msg.err != nil {
				m.assessmentError = msg.err.Error()
			} else {
				m.assessment = &msg.plan
			}
			return m, nil
		}
		if msg.published {
			m.saved = m.draft
			m.saved.Layouts = cloneLayouts(m.draft.Layouts)
			if msg.kind == "delete" {
				m.list()
			}
		}
		if msg.err != nil {
			m.notice, m.noticeTone = msg.err.Error(), tui.Error
			return m, nil
		}
		if m.dry {
			m.notice, m.noticeTone = "Dry run: valid; nothing saved.", tui.Success
			return m, nil
		}
		if msg.kind == "delete" {
			m.list()
			m.notice, m.noticeTone = "Layout deleted.", tui.Success
		} else {
			m.mode, m.area, m.action = "edit", layoutContent, 0
			m.notice, m.noticeTone = "Layout saved.", tui.Success
		}
		return m, nil
	case tea.KeyPressMsg:
		key := msg.String()
		m.focus = true
		if m.loading != "" {
			if key == "esc" || key == "ctrl+c" {
				m.jobCancel()
				if m.loading == "review" {
					m.seq++
					m.loading, m.mode = "", "edit"
				} else {
					m.notice, m.noticeTone = "Cancelling save…", tui.Notice
				}
			}
			return m, nil
		}
		if m.field != "" {
			return m, m.inputKey(msg)
		}
		if cmd, handled := m.settingsKey(msg); handled {
			return m, cmd
		}
		if key == "pgup" || key == "pgdown" {
			m.focus = false
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
		if key == "ctrl+c" {
			if m.mode == "confirm" {
				return m, m.back()
			}
			return m, m.leave(true)
		}
		if key == "esc" {
			return m, m.back()
		}
		if key == "ctrl+s" {
			if m.mode == "edit" {
				return m, m.invoke("review")
			}
			if m.mode == "review" {
				return m, m.invoke("save")
			}
		}
		if key == "tab" || key == "shift+tab" {
			m.moveArea(key == "shift+tab")
			return m, nil
		}
		if key == "up" || key == "k" || key == "down" || key == "j" {
			delta := 1
			if key == "up" || key == "k" {
				delta = -1
			}
			m.moveVertical(delta)
			return m, nil
		}
		if m.area == layoutActions {
			switch key {
			case "left", "h":
				m.action = tui.CycleChoice(m.action, len(m.actions()), -1)
			case "right", "l":
				m.action = tui.CycleChoice(m.action, len(m.actions()), 1)
			case "home":
				m.action = 0
			case "end":
				m.action = len(m.actions()) - 1
			case "enter", "space":
				return m, m.activateAction(m.action)
			}
			return m, nil
		}
		if m.area == layoutBreadcrumbs {
			switch key {
			case "left":
				m.crumb = max(0, m.crumb-1)
			case "right":
				m.crumb = min(len(m.breadcrumbs())-2, m.crumb+1)
			case "enter", "space":
				if m.crumb == 0 {
					return m, m.leave(false)
				}
				return m, m.back()
			}
			return m, nil
		}
		if m.mode == "edit" && m.page == 0 && m.area == layoutContent && (key == "left" || key == "right") {
			delta := 1
			if key == "left" {
				delta = -1
			}
			m.control = tui.CycleChoice(m.control, len(layoutPresetActions), delta)
			return m, nil
		}
		if m.mode == "edit" && (key == "left" || key == "right") {
			m.page = 1 - m.page
			m.cursor = 0
			m.viewport.GotoTop()
			return m, nil
		}
		if m.area == layoutSections {
			if key == "enter" || key == "space" {
				m.moveVertical(1)
			}
			return m, nil
		}
		switch key {
		case "enter", "space":
			switch m.mode {
			case "list":
				return m, m.invoke("edit")
			case "edit":
				if m.page == 0 {
					return m, m.activatePresetControl()
				}
				m.moveVertical(1)
				return m, nil
			}
		case "n":
			if m.mode == "list" {
				return m, m.invoke("new")
			}
		case "delete", "backspace":
			if m.mode == "list" {
				return m, m.invoke("delete")
			}
		case "p", "d":
			if m.mode == "edit" && m.page == 0 {
				m.control = 1
				if key == "d" {
					m.control = 2
				}
				m.choice(key)
			}
		}
	}
	var cmd tea.Cmd
	if m.field != "" {
		m.text, cmd = m.text.Update(msg)
	} else if m.mode == "edit" && m.page == 1 && m.area == layoutContent && m.cursor < 2 && m.loading == "" {
		m.syncSettingInput()
		m.text, cmd = m.text.Update(msg)
		m.changeSetting()
	} else if m.mode == "review" {
		m.viewport, cmd = m.viewport.Update(msg)
	}
	return m, cmd
}

func (m *LayoutModel) count() int {
	if m.mode == "list" {
		return len(m.draft.Layouts)
	}
	if m.page == 1 {
		return 3
	}
	return len(m.presetIDs())
}

func (m *LayoutModel) edit(field, value string) tea.Cmd {
	m.field, m.area, m.action, m.notice = field, layoutContent, 1, ""
	m.text.SetValue(value)
	m.text.CursorEnd()
	return m.text.Focus()
}

var layoutNameRun = regexp.MustCompile(`[^a-z0-9]+`)

func (m *LayoutModel) inputKey(msg tea.KeyPressMsg) tea.Cmd {
	if msg.String() == "space" && m.area == layoutActions {
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	}
	switch msg.String() {
	case "esc", "ctrl+c":
		m.field, m.notice = "", ""
		m.area = layoutContent
		m.text.Blur()
		return nil
	case "tab", "shift+tab", "up", "down":
		m.area = (tui.FocusOrder{Rows: 1}).Cycle(m.area, 1)
		if m.area == layoutContent {
			return m.text.Focus()
		}
		m.action = 1
		m.text.Blur()
		return nil
	case "left", "right", "home", "end":
		if m.area == layoutActions {
			switch msg.String() {
			case "home":
				m.action = 0
			case "end":
				m.action = 1
			default:
				m.action = tui.CycleChoice(m.action, 2, 1)
			}
			return nil
		}
	case "enter":
		if m.area == layoutActions && m.action == 0 {
			m.field, m.notice = "", ""
			m.area = layoutContent
			m.text.Blur()
			return nil
		}
		m.area = layoutContent
		focus := m.text.Focus()
		value := strings.TrimSpace(m.text.Value())
		if value == "" || strings.ContainsAny(value, "\x00\r\n\x1b") {
			m.notice, m.noticeTone = "Enter a valid value.", tui.Error
			return focus
		}
		layout := m.draft.Layouts[m.id]
		switch m.field {
		case "new":
			base := strings.Trim(layoutNameRun.ReplaceAllString(strings.ToLower(value), "-"), "-")
			if base == "" {
				base = "layout"
			}
			id := base
			for n := 2; ; n++ {
				if _, ok := m.draft.Layouts[id]; !ok {
					break
				}
				id = fmt.Sprintf("%s-%d", base, n)
			}
			m.draft.Layouts[id] = setup.Layout{Name: value, Presets: []string{}, Startup: []string{}, IdleSeconds: 1800}
			m.open(id)
		case "name":
			layout.Name = value
			m.draft.Layouts[m.id] = layout
		case "idle":
			n, err := strconv.Atoi(value)
			if err != nil || n <= 0 {
				m.notice, m.noticeTone = "Enter a positive number of seconds.", tui.Error
				return focus
			}
			layout.IdleSeconds = n
			m.draft.Layouts[m.id] = layout
		}
		m.field, m.notice, m.area = "", "", layoutContent
		m.text.Blur()
		return nil
	}
	if m.area == layoutContent {
		var cmd tea.Cmd
		m.text, cmd = m.text.Update(msg)
		return cmd
	}
	return nil
}

func (m *LayoutModel) choice(key string) {
	id := m.selectedPreset()
	if id == "" {
		return
	}
	layout := m.draft.Layouts[m.id]
	if !slices.Contains(layout.Presets, id) {
		m.notice, m.noticeTone = "Include this preset first.", tui.Notice
		return
	}
	m.notice = ""
	if key == "p" {
		if slices.Contains(layout.Startup, id) {
			layout.Startup = slices.DeleteFunc(layout.Startup, func(s string) bool { return s == id })
		} else {
			layout.Startup = append(layout.Startup, id)
		}
	} else if layout.Default == id {
		layout.Default = ""
	} else {
		layout.Default = id
	}
	m.draft.Layouts[m.id] = layout
}

func (m *LayoutModel) removeMember() {
	id := m.selectedPreset()
	layout := m.draft.Layouts[m.id]
	layout.Presets = slices.DeleteFunc(layout.Presets, func(s string) bool { return s == id })
	layout.Startup = slices.DeleteFunc(layout.Startup, func(s string) bool { return s == id })
	if layout.Default == id {
		layout.Default = ""
	}
	m.draft.Layouts[m.id] = layout
}

func (m *LayoutModel) leave(quit bool) tea.Cmd {
	if m.dirty() {
		m.ask("discard")
		m.quitAfter = quit
		return nil
	}
	if quit {
		return tea.Quit
	}
	m.list()
	return nil
}

func (m *LayoutModel) ask(kind string) {
	m.notice = ""
	m.returnMode, m.mode, m.confirm, m.area, m.action = m.mode, "confirm", kind, layoutActions, 0
}

func (m *LayoutModel) back() tea.Cmd {
	if m.mode == "confirm" {
		if m.confirm == "delete" {
			m.draft.Layouts = cloneLayouts(m.saved.Layouts)
		}
		m.mode, m.confirm, m.area, m.action = m.returnMode, "", layoutContent, 0
		m.notice = ""
		return nil
	}
	if m.mode == "review" {
		m.mode, m.area, m.action = "edit", layoutContent, 0
		return nil
	}
	return m.leave(m.mode == "list")
}

func (m *LayoutModel) invoke(action string) tea.Cmd {
	switch action {
	case "field":
		m.syncSettingInput()
		return nil
	case "new":
		return m.edit("new", "")
	case "edit", "delete":
		ids := m.layoutIDs()
		if len(ids) == 0 {
			return nil
		}
		id := ids[min(m.cursor, len(ids)-1)]
		if action == "edit" {
			m.open(id)
		} else {
			m.id = id
			m.ask("delete")
		}
	case "member":
		m.notice = ""
		id := m.selectedPreset()
		if id == "" {
			return nil
		}
		layout := m.draft.Layouts[m.id]
		if !slices.Contains(layout.Presets, id) {
			if reason := m.presetErrors[id]; reason != "" {
				m.notice, m.noticeTone = reason, tui.Error
				return nil
			}
			layout.Presets = append(layout.Presets, id)
			m.draft.Layouts[m.id] = layout
		} else if slices.Contains(layout.Startup, id) || layout.Default == id {
			m.ask("member")
		} else {
			m.removeMember()
		}
	case "preload":
		m.choice("p")
	case "default":
		m.choice("d")
	case "back":
		return m.back()
	case "done":
		return m.leave(true)
	case "confirm":
		switch m.confirm {
		case "member":
			m.removeMember()
			return m.back()
		case "discard":
			m.draft.Layouts = cloneLayouts(m.saved.Layouts)
			m.settings, m.settingInput = nil, ""
			return m.leave(m.quitAfter)
		case "delete":
			delete(m.draft.Layouts, m.id)
			return m.publish("delete")
		}
	case "review":
		if err := m.settingsError(); err != "" {
			m.notice, m.noticeTone = err, tui.Error
			return nil
		}
		m.mode, m.area, m.action, m.notice = "review", layoutActions, 0, ""
		m.assessment, m.assessmentError = nil, ""
		m.viewport.GotoTop()
		if m.preview == nil {
			m.assessmentError = "Memory assessment unavailable."
			return nil
		}
		ctx, seq := m.begin("review")
		candidate, id, preview := m.draft, m.id, m.preview
		candidate.Layouts = cloneLayouts(candidate.Layouts)
		return func() tea.Msg {
			plan, err := preview(ctx, candidate, id)
			return layoutJob{seq: seq, kind: "review", plan: plan, err: err}
		}
	case "save":
		return m.publish("save")
	}
	return nil
}

func (m *LayoutModel) begin(kind string) (context.Context, int) {
	ctx, cancel := context.WithCancel(m.ctx)
	m.jobCancel, m.loading = cancel, kind
	m.seq++
	return ctx, m.seq
}

func (m *LayoutModel) publish(kind string) tea.Cmd {
	if err := m.settingsError(); err != "" {
		m.notice, m.noticeTone = err, tui.Error
		return nil
	}
	if err := m.draft.Validate(); err != nil {
		m.notice, m.noticeTone = err.Error(), tui.Error
		return nil
	}
	ctx, seq := m.begin(kind)
	layouts, save := cloneLayouts(m.draft.Layouts), m.save
	return func() tea.Msg {
		published, err := save(ctx, layouts)
		return layoutJob{seq: seq, kind: kind, published: published, err: err}
	}
}
