// Package setupui presents guided setup choices. It owns no catalog reads or
// installation effects; the caller supplies choices and an asynchronous preview.
package setupui

import (
	"context"
	"errors"
	"io"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

type Option struct {
	ID, Name, Description, DisabledReason string
	Details, AssessmentURL                string
}

type Template struct {
	Layout, Name, Default string
	Options               []Option // Empty ID means the embedded model template.
}

type Profile struct {
	Option
	Mode                string
	Templates           []Template
	Contexts            []ContextWindow
	SoftwareUnavailable map[string]string // Software choice ID to unavailability reason.
}

type Input struct {
	Machine  string
	Modes    []Option
	Profiles []Profile
}

type Choice struct {
	Mode, Profile  string
	Templates      map[string]string // Layout ID to patch ID; empty means embedded.
	ContextWindows map[string]int    // Layout ID to total input-plus-output tokens.
}

type Choices struct {
	Profiles []Choice
	Software string // recorded, latest, or tested; explicitly selected.
}

type Section struct {
	Title     string
	Lines     []string
	Downloads []Download // Non-nil sections have a collapsible file table.
}

type Download struct {
	File, Size, Status string
}

type Review struct {
	Sections      []Section
	CanPrepare    bool
	PrepareReason string
	Token         string // Opaque identity of the exact plan shown to the user.
}

type Decision struct {
	Choices     Choices
	Action      string // save, prepare, or cancel
	ReviewToken string // The accepted review's token; empty for cancel.
}

type stage uint8

const (
	stageModes stage = iota
	stageProfile
	stageTemplates
	stageContext
	stageSoftware
	stageReview
)

type previewMsg struct {
	seq    int
	review Review
	err    error
}

// Model is the testable Bubble Tea state machine. NewModel is free of effects.
type Model struct {
	input          Input
	preview        func(context.Context, Choices) (Review, error)
	ctx            context.Context
	previewCancel  context.CancelFunc
	stage          stage
	cursor         int
	modeAt         int
	selected       map[string]bool
	profiles       map[string]string
	patches        map[string]map[string]map[string]string // mode -> profile -> layout -> patch
	contexts       map[contextKey]textinput.Model
	software       string
	review         Review
	loading        bool
	reviewErr      error
	seq            int
	notice         string
	decision       Decision
	width          int
	height         int
	viewport       viewport.Model
	focusSelection bool
	downloadsOpen  bool
	focusDownloads bool
	hitTargets     []hitTarget
}

func NewModel(ctx context.Context, input Input, preview func(context.Context, Choices) (Review, error)) *Model {
	if ctx == nil {
		ctx = context.Background()
	}
	v := viewport.New(viewport.WithWidth(78), viewport.WithHeight(16))
	v.SoftWrap = false // Lip Gloss wraps each block before viewport placement.
	return &Model{input: input, preview: preview, ctx: ctx,
		selected: make(map[string]bool), profiles: make(map[string]string),
		patches: make(map[string]map[string]map[string]string), width: 80, height: 24,
		contexts: make(map[contextKey]textinput.Model),
		viewport: v, focusSelection: true}
}

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, msg.Width), max(1, msg.Height)
		m.focusSelection = true
		m.hitTargets = nil
		return m, nil
	case previewMsg:
		if m.stage == stageReview && msg.seq == m.seq {
			m.cancelPreview()
			m.loading, m.reviewErr = false, msg.err
			if msg.err == nil {
				m.review = msg.review
			} else {
				m.review = Review{}
			}
		}
		return m, nil
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft || m.width < 28 || m.height < 10 {
			return m, nil
		}
		for _, target := range m.hitTargets {
			if !target.contains(msg.X, msg.Y) {
				continue
			}
			switch target.kind {
			case hitNext:
				return m.advance()
			case hitChoice:
				m.cursor = target.index
				m.activate()
			case hitReview:
				m.cursor = target.index
				return m.finish()
			case hitDownloads:
				m.toggleDownloads()
			}
			return m, nil
		}
		return m, nil
	case tea.KeyPressMsg:
		if (m.width < 28 || m.height < 10) && msg.String() != "q" && msg.String() != "ctrl+c" {
			return m, nil // Do not confirm an action hidden by the resize prompt.
		}
		if m.stage == stageContext {
			switch msg.String() {
			case "enter":
				if m.cursor < m.optionCount()-1 {
					m.move(1)
					return m, nil
				}
				return m.advance()
			case "left", "right", "backspace", "delete", "home", "end", "ctrl+a", "ctrl+e", "ctrl+k", "ctrl+u", "ctrl+w", "alt+backspace":
				return m.updateContext(msg)
			case "a":
				m.resetContext()
				return m, nil
			}
		}
		switch msg.String() {
		case "ctrl+c", "q":
			m.seq++
			m.cancelPreview()
			m.decision = Decision{Action: "cancel"}
			return m, tea.Quit
		case "shift+tab", "left":
			if m.stage == stageReview {
				m.move(-1)
				return m, nil
			}
			if m.stage != stageModes {
				m.back()
			}
			return m, nil
		case "esc", "b", "backspace":
			if m.stage == stageModes {
				m.seq++
				m.cancelPreview()
				m.decision = Decision{Action: "cancel"}
				return m, tea.Quit
			}
			m.back()
			return m, nil
		case "up", "k":
			if m.stage == stageReview {
				m.viewport.ScrollUp(1)
				return m, nil
			}
			m.move(-1)
			return m, nil
		case "down", "j":
			if m.stage == stageReview {
				m.viewport.ScrollDown(1)
				return m, nil
			}
			m.move(1)
			return m, nil
		case "space":
			if m.stage == stageReview {
				m.viewport.PageDown()
				return m, nil
			}
			if m.cursor == m.optionCount() {
				return m.advance()
			}
			m.activate()
			return m, nil
		case "enter":
			if m.stage == stageReview {
				return m.finish()
			}
			if m.cursor == m.optionCount() {
				return m.advance()
			}
			m.activate()
			return m, nil
		case "tab", "right":
			if m.stage == stageReview {
				m.move(1)
				return m, nil
			}
			return m.advance()
		case "n":
			if m.stage != stageReview {
				return m.advance()
			}
		case "d":
			if m.stage == stageReview {
				m.toggleDownloads()
			}
			return m, nil
		case "r":
			if m.stage == stageReview {
				return m.startPreview()
			}
		}
	}
	if m.stage == stageContext {
		switch msg.(type) {
		case tea.KeyPressMsg, tea.PasteMsg:
			return m.updateContext(msg)
		}
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *Model) toggleDownloads() {
	for _, section := range m.review.Sections {
		if section.Downloads != nil {
			m.downloadsOpen = !m.downloadsOpen
			m.focusDownloads = true
			return
		}
	}
}

func (m *Model) move(delta int) {
	n := m.rowCount()
	if n == 0 {
		return
	}
	m.cursor = (m.cursor + delta + n) % n
	m.focusSelection = true
	m.notice = ""
}

func (m *Model) activate() {
	m.notice = ""
	m.focusSelection = true
	switch m.stage {
	case stageModes:
		if m.cursor >= len(m.input.Modes) {
			return
		}
		o := m.input.Modes[m.cursor]
		if o.DisabledReason != "" {
			m.notice = o.DisabledReason
			return
		}
		m.selected[o.ID] = !m.selected[o.ID]
	case stageProfile:
		ps := m.availableProfiles()
		if m.cursor >= len(ps) {
			return
		}
		p := ps[m.cursor]
		if p.DisabledReason != "" {
			m.notice = p.DisabledReason
			return
		}
		m.profiles[m.currentMode()] = p.ID
		m.initTemplates(m.currentMode(), p)
		m.initContexts(m.currentMode(), p)
	case stageTemplates:
		p, ok := m.currentProfile()
		if !ok {
			return
		}
		layout, o, ok := templateRow(p, m.cursor)
		if !ok {
			return
		}
		if o.DisabledReason != "" {
			m.notice = o.DisabledReason
			return
		}
		m.patches[m.currentMode()][p.ID][layout] = o.ID
	case stageContext:
		// Clicking a context card focuses its text input through m.cursor.
	case stageSoftware:
		if m.cursor < len(softwareOptions) {
			o := m.softwareOption(softwareOptions[m.cursor])
			if o.DisabledReason != "" {
				m.notice = o.DisabledReason
				return
			}
			m.software = o.ID
		}
	case stageReview:
		// Review actions are committed by Enter, avoiding accidental Space commits.
	}
}

func (m *Model) advance() (tea.Model, tea.Cmd) {
	m.notice = ""
	switch m.stage {
	case stageModes:
		if len(m.chosenModes()) == 0 {
			m.notice = "Choose at least one available mode."
			return m, nil
		}
		m.modeAt, m.stage, m.cursor = 0, stageProfile, 0
	case stageProfile:
		p, ok := m.currentProfile()
		if !ok {
			m.notice = "Choose a profile for this mode."
			return m, nil
		}
		if hasTemplateChoices(p) {
			m.stage, m.cursor = stageTemplates, 0
		} else {
			m.contextOrNextMode()
		}
	case stageTemplates:
		m.contextOrNextMode()
	case stageContext:
		if err := m.validateContexts(); err != nil {
			m.notice = err.Error()
			return m, nil
		}
		m.nextMode()
	case stageSoftware:
		if m.software == "" {
			m.notice = "Choose a software version policy."
			return m, nil
		}
		for _, o := range softwareOptions {
			if o.ID == m.software {
				if reason := m.softwareOption(o).DisabledReason; reason != "" {
					m.notice = reason
					return m, nil
				}
			}
		}
		m.stage, m.cursor = stageReview, 0
		m.viewport.SetYOffset(0)
		return m.startPreview()
	}
	m.viewport.SetYOffset(0)
	m.focusSelection = true
	m.hitTargets = nil
	return m, nil
}

func (m *Model) nextMode() {
	if m.modeAt+1 < len(m.chosenModes()) {
		m.modeAt++
		m.stage = stageProfile
	} else {
		m.stage = stageSoftware
	}
	m.cursor = 0
}

func (m Model) lastModelStage() stage {
	if p, ok := m.currentProfile(); ok {
		if len(p.Contexts) > 0 {
			return stageContext
		}
		if hasTemplateChoices(p) {
			return stageTemplates
		}
	}
	return stageProfile
}

func (m *Model) back() {
	m.notice = ""
	switch m.stage {
	case stageModes:
		m.decision = Decision{Action: "cancel"}
	case stageProfile:
		if m.modeAt == 0 {
			m.stage = stageModes
		} else {
			m.modeAt--
			m.stage = m.lastModelStage()
		}
	case stageTemplates:
		m.stage = stageProfile
	case stageContext:
		m.stage = stageProfile
		if p, ok := m.currentProfile(); ok && hasTemplateChoices(p) {
			m.stage = stageTemplates
		}
	case stageSoftware:
		m.modeAt = len(m.chosenModes()) - 1
		m.stage = m.lastModelStage()
	case stageReview:
		m.seq++ // Ignore an in-flight preview from the old selection.
		m.cancelPreview()
		m.loading = false
		m.review = Review{}
		m.reviewErr = nil
		m.stage = stageSoftware
	}
	m.cursor = 0
	m.viewport.SetYOffset(0)
	m.focusSelection = true
	m.hitTargets = nil
}

func (m *Model) startPreview() (tea.Model, tea.Cmd) {
	m.cancelPreview()
	m.hitTargets = nil
	m.seq++
	if m.preview == nil {
		m.reviewErr = errors.New("preview is unavailable")
		m.loading = false
		return m, nil
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.previewCancel = cancel
	seq, choices, preview := m.seq, m.choices(), m.preview
	m.loading, m.reviewErr, m.review = true, nil, Review{}
	return m, func() tea.Msg {
		review, err := preview(ctx, choices)
		return previewMsg{seq: seq, review: review, err: err}
	}
}

func (m *Model) cancelPreview() {
	if m.previewCancel != nil {
		m.previewCancel()
		m.previewCancel = nil
	}
}

func (m *Model) finish() (tea.Model, tea.Cmd) {
	if m.cursor == 2 {
		m.back()
		return m, nil
	}
	if m.cursor == 3 {
		m.seq++
		m.cancelPreview()
		m.decision = Decision{Action: "cancel"}
		return m, tea.Quit
	}
	if m.loading {
		m.notice = "Wait for the preview to finish."
		return m, nil
	}
	if m.reviewErr != nil || m.review.Token == "" {
		m.notice = "A successful preview is required before saving or preparing. Press r to retry."
		return m, nil
	}
	if m.cursor == 0 {
		m.decision = Decision{Choices: m.choices(), Action: "save", ReviewToken: m.review.Token}
		return m, tea.Quit
	}
	if m.cursor == 1 {
		if m.reviewErr != nil {
			m.notice = "Resolve the preview error before preparing."
			return m, nil
		}
		if !m.review.CanPrepare {
			m.notice = m.review.PrepareReason
			if m.notice == "" {
				m.notice = "Preparation is unavailable."
			}
			return m, nil
		}
		m.decision = Decision{Choices: m.choices(), Action: "prepare", ReviewToken: m.review.Token}
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) chosenModes() []string {
	var ids []string
	for _, o := range m.input.Modes {
		if m.selected[o.ID] && o.DisabledReason == "" {
			ids = append(ids, o.ID)
		}
	}
	return ids
}

func (m Model) currentMode() string {
	ids := m.chosenModes()
	if m.modeAt < 0 || m.modeAt >= len(ids) {
		return ""
	}
	return ids[m.modeAt]
}

func (m Model) availableProfiles() []Profile {
	var ps []Profile
	for _, p := range m.input.Profiles {
		if p.Mode == m.currentMode() {
			ps = append(ps, p)
		}
	}
	return ps
}

func (m Model) currentProfile() (Profile, bool) {
	return m.profileForMode(m.currentMode())
}

func (m Model) profileForMode(mode string) (Profile, bool) {
	for _, p := range m.input.Profiles {
		if p.Mode == mode && p.ID == m.profiles[mode] && p.DisabledReason == "" {
			return p, true
		}
	}
	return Profile{}, false
}

func (m *Model) initTemplates(mode string, p Profile) {
	if m.patches[mode] == nil {
		m.patches[mode] = make(map[string]map[string]string)
	}
	if m.patches[mode][p.ID] == nil {
		m.patches[mode][p.ID] = make(map[string]string)
	}
	for _, t := range p.Templates {
		if _, exists := m.patches[mode][p.ID][t.Layout]; !exists {
			choice := t.Default
			if options := templateChoices(t); len(options) == 1 {
				choice = options[0]
			}
			m.patches[mode][p.ID][t.Layout] = choice
		}
	}
}

func templateChoices(t Template) []string {
	var ids []string
	seen := make(map[string]bool)
	for _, o := range t.Options {
		if o.DisabledReason == "" && !seen[o.ID] {
			ids = append(ids, o.ID)
			seen[o.ID] = true
		}
	}
	return ids
}

func hasTemplateChoices(p Profile) bool {
	for _, t := range p.Templates {
		if len(templateChoices(t)) > 1 {
			return true
		}
	}
	return false
}

func templateRow(p Profile, row int) (string, Option, bool) {
	for _, t := range p.Templates {
		if len(templateChoices(t)) < 2 {
			continue
		}
		for _, o := range t.Options {
			if row == 0 {
				return t.Layout, o, true
			}
			row--
		}
	}
	return "", Option{}, false
}

var softwareOptions = []Option{
	{ID: "recorded", Name: "Recorded", Description: "Use the catalog's recorded software version."},
	{ID: "latest", Name: "Latest", Description: "Newest available software, including llama.cpp nightly builds; preview verifies software archives without downloading model weights."},
	{ID: "tested", Name: "Tested", Description: "Check an applicable tested software version; preview reads upstream archives for verification, not model weights."},
}

func (m Model) softwareOption(o Option) Option {
	var reasons []string
	for _, mode := range m.chosenModes() {
		for _, p := range m.input.Profiles {
			if p.Mode == mode && p.ID == m.profiles[mode] {
				if reason := p.SoftwareUnavailable[o.ID]; reason != "" {
					reasons = append(reasons, mode+": "+reason)
				}
				break
			}
		}
	}
	o.DisabledReason = strings.Join(reasons, "; ")
	return o
}

func (m Model) rowCount() int {
	if m.stage == stageReview {
		return 4
	}
	return m.optionCount() + 1 // The fixed Next button follows the choices.
}

func (m Model) optionCount() int {
	switch m.stage {
	case stageModes:
		return len(m.input.Modes)
	case stageProfile:
		return len(m.availableProfiles())
	case stageTemplates:
		p, ok := m.currentProfile()
		if !ok {
			return 0
		}
		n := 0
		for _, t := range p.Templates {
			if len(templateChoices(t)) > 1 {
				n += len(t.Options)
			}
		}
		return n
	case stageSoftware:
		return len(softwareOptions)
	case stageContext:
		if p, ok := m.currentProfile(); ok {
			return len(p.Contexts)
		}
	}
	return 0
}

func (m Model) choices() Choices {
	c := Choices{Software: m.software}
	for _, mode := range m.chosenModes() {
		profile := m.profiles[mode]
		patches := make(map[string]string)
		for layout, patch := range m.patches[mode][profile] {
			patches[layout] = patch
		}
		c.Profiles = append(c.Profiles, Choice{Mode: mode, Profile: profile, Templates: patches, ContextWindows: m.contextChoices(mode, profile)})
	}
	return c
}

// Run returns the user's decision. The preview callback may read upstream but
// must not install or write state. Only the caller acts on save or prepare.
func Run(ctx context.Context, in io.Reader, out io.Writer, input Input, preview func(context.Context, Choices) (Review, error)) (Decision, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if in == nil || out == nil {
		return Decision{}, errors.New("setup UI requires input and output")
	}
	if preview == nil {
		return Decision{}, errors.New("setup UI requires a preview callback")
	}
	p := tea.NewProgram(NewModel(ctx, input, preview), tea.WithContext(ctx), tea.WithInput(in), tea.WithOutput(out))
	final, err := p.Run()
	if err != nil {
		return Decision{}, err
	}
	m, ok := final.(*Model)
	if !ok {
		return Decision{}, errors.New("setup UI returned unexpected model")
	}
	if m.decision.Action == "" {
		return Decision{Action: "cancel"}, nil
	}
	return m.decision, nil
}
