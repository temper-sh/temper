package presetcmd

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/huggingface"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/tui"
	"github.com/temper-sh/temper/internal/userpreset"
)

// EditorOptions describe authoring only. No selection or activation is implied.
type EditorOptions struct {
	Root, ID  string
	MemoryGiB int
	DryRun    bool
}

type managerPreset struct {
	id, name, status string
	mine             bool
}
type formField struct {
	label, value string
	choices      []string
}
type editorJob struct {
	template   *userpreset.TemplateFile
	seq        int
	kind       string
	repository huggingface.Repository
	file       catalog.UserPreset
	lines      []string
	warning    string
	err        error
}

// Editor is the revocable preset authoring UI. Its effects are explicit Save
// and Delete actions; asynchronous work is limited to reads and validation.
type Editor struct {
	command          Command
	options          EditorOptions
	view             catalog.View
	memory           int
	facts            *machine.Facts
	ctx              context.Context
	cancel           context.CancelFunc
	jobCancel        context.CancelFunc
	seq              int
	loading          bool
	mode             string
	catalogTab       bool
	items            []managerPreset
	cursor, page     int
	draft            *presetDraft
	pending          catalog.UserPreset
	pendingTemplates map[string][]byte
	beforeConversion []byte
	input            string
	text             textinput.Model
	formKind         string
	form             []formField
	repository       huggingface.Repository
	files            map[string]bool
	fileFilter       string
	report           []string
	overview         *presetOverview
	reportTitle      string
	returnMode       string
	confirm          string
	confirmCursor    int
	quitAfter        bool
	deleteID         string
	deleteRaw        []byte
	notice           string
	width, height    int
	viewport         viewport.Model
	focus            bool
	area             editorArea
	navCursor        int
	actionCursor     int
	hits             tui.HitMap
	values           map[string]string
	valueErrors      map[string]string
	customModes      map[string]bool
	controlKey       string
	pointerChoices   []controlChoice
	selectedTemplate *userpreset.TemplateFile
	directory        string
	directoryEntries []os.DirEntry
}

// NewEditor reads local state without creating a root or making a network call.
// Close cancels outstanding metadata or planning reads when the caller leaves.
func (c Command) NewEditor(ctx context.Context, o EditorOptions) (*Editor, error) {
	root, err := c.root(o.Root)
	if err != nil {
		return nil, err
	}
	view, err := c.view(root)
	if err != nil {
		return nil, err
	}
	memory, facts, err := c.memory(ctx, options{memoryGiB: o.MemoryGiB})
	if err != nil {
		return nil, err
	}
	o.Root = root
	ctx, cancel := context.WithCancel(ctx)
	m := &Editor{command: c, options: o, view: view, memory: memory, facts: facts, ctx: ctx, cancel: cancel,
		mode: "manager", width: 100, height: 32, viewport: viewport.New(), focus: true,
		text: tui.TextInput(512, tui.Colors(tui.Input).Text, tui.Colors(tui.InputPrompt).Text, tui.Colors(tui.InputCursor).Text)}
	if err := m.reload(); err != nil {
		cancel()
		return nil, err
	}
	if o.ID != "" {
		if err := m.open(o.ID); err != nil {
			cancel()
			return nil, err
		}
	}
	return m, nil
}

func (m *Editor) Close()        { m.cancelJob(); m.cancel() }
func (m *Editor) Init() tea.Cmd { return nil }

func (c Command) interactive(ctx context.Context, args []string, out, diagnostics io.Writer) int {
	o := EditorOptions{}
	edit := len(args) > 0 && args[0] == "edit"
	if edit {
		args = args[1:]
		if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
			o.ID, args = args[0], args[1:]
		}
	}
	f := flag.NewFlagSet("temper preset", flag.ContinueOnError)
	f.SetOutput(diagnostics)
	f.StringVar(&o.Root, "root", "", "Temper folder (default ~/.temper)")
	f.IntVar(&o.MemoryGiB, "memory-gib", 0, "profile memory size (default this Mac)")
	f.BoolVar(&o.DryRun, "dry-run", false, "preview authoring without saving or deleting files")
	f.Usage = func() {
		fmt.Fprintln(diagnostics, "Usage: temper preset [--root PATH] [--memory-gib N] [--dry-run]\n       temper preset edit ID [options]\nOpens the preset manager/editor in a terminal. Save is explicit; layouts keep their locks.")
		f.PrintDefaults()
	}
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if f.NArg() != 0 || o.MemoryGiB < 0 || edit && o.ID == "" {
		f.Usage()
		return 2
	}
	inFile, inOK := c.Input.(*os.File)
	outFile, outOK := out.(*os.File)
	if !inOK || !outOK || !term.IsTerminal(inFile.Fd()) || !term.IsTerminal(outFile.Fd()) {
		fmt.Fprintln(diagnostics, "The preset editor needs terminal input and output. For scripts, use temper preset fork, copy, check, fields, weights or template; add --json for structured output.")
		return 2
	}
	m, err := c.NewEditor(ctx, o)
	if err != nil {
		return failed(diagnostics, err)
	}
	defer m.Close()
	_, err = tea.NewProgram(m, tea.WithInput(c.Input), tea.WithOutput(out), tea.WithContext(ctx)).Run()
	if err != nil && ctx.Err() == nil {
		return failed(diagnostics, err)
	}
	return 0
}

func (m *Editor) reload() error {
	ids, err := userpreset.List(m.options.Root)
	if err != nil {
		return err
	}
	m.items = nil
	for _, id := range ids {
		r := checkOne(m.options.Root, id, m.view, m.memory, m.facts)
		status := "valid · fit unknown"
		if !r.Valid {
			status = "invalid: " + r.Error
		} else if r.Fits != nil && !*r.Fits {
			status = "valid · does not fit this Mac"
		} else if r.Memory != nil && r.Memory.Basis == "measured" {
			status = "valid · measured workload within budget"
		}
		m.items = append(m.items, managerPreset{id: id, name: r.Name, status: status, mine: true})
	}
	for _, id := range sortedKeys(m.view.Document.Presets) {
		p := m.view.Document.Presets[id]
		m.items = append(m.items, managerPreset{id: id, name: p.Presentation.DisplayName, status: "catalog source"})
	}
	for _, unavailable := range m.view.Unavailable {
		if unavailable.Kind == "preset" {
			m.items = append(m.items, managerPreset{id: unavailable.ID, name: unavailable.Presentation.DisplayName, status: "unavailable on this Temper"})
		}
	}
	return nil
}

func (m *Editor) visiblePresets() []managerPreset {
	var rows []managerPreset
	for _, p := range m.items {
		if p.mine != m.catalogTab {
			rows = append(rows, p)
		}
	}
	return rows
}

func (m *Editor) open(id string) error {
	raw, err := userpreset.Read(m.options.Root, id)
	if err != nil {
		return err
	}
	u, err := catalog.ParseUserPreset(id, raw)
	if err != nil {
		return fmt.Errorf("%s: %w; repair the file's syntax before opening the form", id, err)
	}
	if u.Fork() {
		if _, ok := m.view.Document.Presets[u.Origin]; !ok {
			return fmt.Errorf("origin %s is unavailable; restore a catalog with this origin before editing", u.Origin)
		}
	}
	m.draft = draftFrom(id, u, raw)
	m.clearFormInputs()
	m.mode, m.page, m.cursor, m.beforeConversion = "editor", 0, 0, nil
	return nil
}

func (m *Editor) beginNew(copy bool) tea.Cmd {
	var u catalog.UserPreset
	m.pendingTemplates = nil
	if m.mode == "editor" {
		if m.formErrors() {
			m.notice = "Correct invalid draft inputs before making a copy."
			return nil
		}
		m.pendingTemplates = maps.Clone(m.draft.templates)
		lock, _, err := m.draft.compile(m.options.Root, m.view, m.memory)
		if err != nil {
			m.notice = err.Error()
			return nil
		}
		u = catalog.CopyOf(lock, "")
	} else {
		var source managerPreset
		if m.mode == "overview" {
			if m.overview.lock == nil {
				return nil
			}
			source = m.overview.source
		} else {
			rows := m.visiblePresets()
			if len(rows) == 0 {
				m.catalogTab = true
				m.cursor = 0
				return nil
			}
			source = rows[m.cursor]
		}
		if !copy {
			if source.mine {
				m.notice = "Forks start from Catalog; copy your preset to make an independent variant."
				return nil
			}
			u = catalog.UserPreset{Schema: catalog.UserPresetSchema, Origin: source.id}
		} else {
			d := m.view.Document
			if source.mine {
				loaded, err := userpreset.Load(m.options.Root, source.id, m.view)
				if err != nil {
					m.notice = err.Error()
					return nil
				}
				d = loaded.Document
			}
			lock, err := catalog.CompilePreset(d, source.id, m.memory, 0, target)
			if err != nil {
				m.notice = err.Error()
				return nil
			}
			u = catalog.CopyOf(lock, "")
		}
	}
	m.pending = u
	name := u.Name
	if u.Fork() {
		name = m.view.Document.Presets[u.Origin].Presentation.DisplayName + " (yours)"
	} else {
		name = u.Preset.Presentation.DisplayName + " (copy)"
	}
	return m.prompt("new-name", name)
}

func (m *Editor) prompt(kind, value string) tea.Cmd {
	m.input = kind
	m.area, m.actionCursor, m.focus = contentArea, 1, true
	m.text.Placeholder = ""

	m.text.SetValue(value)
	m.text.CursorEnd()
	return m.text.Focus()
}

func (m *Editor) cancelJob() {
	if m.jobCancel != nil {
		m.jobCancel()
		m.jobCancel = nil
	}
	m.seq++
	m.loading = false
}

func (m *Editor) job(kind string, read func(context.Context) editorJob) tea.Cmd {
	m.cancelJob()
	ctx, cancel := context.WithTimeout(m.ctx, 5*time.Minute)
	m.jobCancel = cancel
	m.loading = true
	seq := m.seq
	return func() tea.Msg {
		defer cancel()
		r := read(ctx)
		r.seq, r.kind = seq, kind
		return r
	}
}

func (m *Editor) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	hits := m.hits
	m.hits = tui.HitMap{}
	switch msg := msg.(type) {
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft || m.width < 28 || m.height < 10 {
			return m, nil
		}
		if area, index, ok := hits.At(msg.X, msg.Y); ok {
			return m, m.click(editorArea(area), index)
		}
		return m, nil
	case tea.MouseWheelMsg:
		if m.input != "" || m.loading {
			return m, nil
		}
		m.focus = false
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, msg.Width), max(1, msg.Height)
		m.focus = true
	case editorJob:
		if msg.seq != m.seq {
			return m, nil
		}
		m.cancelJob()
		if msg.err != nil {
			m.notice = msg.err.Error() + " · Retry or go back; your draft is unchanged."
			return m, nil
		}
		switch msg.kind {
		case "repository":
			m.repository = msg.repository
			m.files = map[string]bool{}
			m.fileFilter = ""
			m.mode = "files"
			m.cursor = 0
		case "material":
			m.draft.file = msg.file
			m.clearHiddenInputs()
			if msg.template != nil {
				if m.draft.templates == nil {
					m.draft.templates = map[string][]byte{}
				}
				m.draft.templates[msg.template.ManagedName] = msg.template.Content
			}
			m.controlKey = ""
			m.mode = "editor"
			m.page = 0
			m.cursor = 0
			m.notice = msg.warning
		case "local-template":
			m.selectedTemplate = msg.template
			m.form[3].value = msg.template.ManagedName
			m.mode, m.cursor, m.controlKey = "form", 1, ""
		case "review":
			m.report = append(m.report, msg.lines...)
		}
		m.focus = true
	case tea.KeyPressMsg:
		key := msg.String()
		if key == "ctrl+c" {
			m.Close()
			return m, tea.Quit
		}
		if m.width < 28 || m.height < 10 {
			if key == "q" {
				m.Close()
				return m, tea.Quit
			}
			return m, nil
		}
		if m.loading {
			if key == "esc" {
				return m, m.activateAction(key)
			}
			return m, nil
		}
		if m.input != "" {
			return m, m.inputKey(msg)
		}
		if cmd, handled := m.controlKeypress(msg); handled {
			m.focus = true
			return m, cmd
		}
		m.notice = ""
		m.focus = true
		if m.confirm != "" {
			return m, m.confirmKey(key)
		}
		if cmd, handled := m.navigationKey(key); handled {
			return m, cmd
		}
		if key == "pgdown" || key == "pgup" || key == "ctrl+d" || key == "ctrl+u" {
			m.focus = false
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
		m.area = contentArea
		switch m.mode {
		case "manager":
			return m, m.managerKey(key)
		case "editor":
			return m, m.editorKey(key)
		case "form":
			return m, m.formKey(key)
		case "files":
			return m, m.filesKey(key)
		case "local-files":
			return m, m.localFileKey(key)
		case "overview":
			return m, m.overviewKey(key)
		case "report":
			if key == "esc" || key == "q" {
				m.mode = m.returnMode
				m.viewport.GotoTop()
			}
		}
	default:
		if m.input != "" {
			var cmd tea.Cmd
			m.text, cmd = m.text.Update(msg)
			return m, cmd
		}
		if m.visibleForm() && m.area == contentArea && !m.loading {
			controls := m.controls()
			if m.cursor < len(controls) && controls[m.cursor].kind == "text" {
				c := controls[m.cursor]
				m.syncControl(c)
				var cmd tea.Cmd
				before := m.text.Value()
				m.text, cmd = m.text.Update(msg)
				if m.text.Value() != before {
					m.changeControlText(c)
				}
				return m, cmd
			}
		}
	}
	return m, nil
}

func (m *Editor) managerKey(key string) tea.Cmd {
	rows := m.visiblePresets()
	switch key {
	case "q", "esc":
		m.Close()
		return tea.Quit
	case "left", "right", "tab":
		m.catalogTab = !m.catalogTab
		m.cursor = 0
	case "n":
		m.catalogTab = true
		m.cursor = 0
		m.notice = "Choose a catalog starting point, then Fork (f) or Copy (c)."
	case "f":
		return m.beginNew(false)
	case "c":
		return m.beginNew(true)
	case "enter", "space", " ", "e":
		if len(rows) > 0 {
			if rows[m.cursor].mine {
				if err := m.open(rows[m.cursor].id); err != nil {
					m.notice = err.Error()
				}
			} else {
				return m.inspect(rows[m.cursor])
			}
		}
	case "v":
		if len(rows) > 0 {
			return m.inspect(rows[m.cursor])
		}
	case "r":
		if err := m.refreshCatalog(); err != nil {
			m.notice = err.Error()
		}
		m.cursor = 0
	case "d":
		if len(rows) == 0 || !rows[m.cursor].mine {
			return nil
		}
		if m.options.DryRun {
			m.notice = "Dry run: deletion is disabled."
			return nil
		}
		id := rows[m.cursor].id
		raw, err := userpreset.Read(m.options.Root, id)
		if err != nil {
			m.notice = err.Error()
			return nil
		}
		m.deleteID, m.deleteRaw = id, raw
		m.confirm, m.confirmCursor = "delete", 0
	}
	return nil
}

func section(path string) int {
	switch {
	case path == "engine_config.controls.reasoning_effort" || path == "engine_config.controls.preserve_reasoning":
		return 0
	case strings.HasPrefix(path, "request_defaults.sampling."):
		return 1
	case strings.HasPrefix(path, "engine_config."):
		return 2
	case strings.HasPrefix(path, "speculation."):
		return 3
	default:
		return 0
	}
}

func (m *Editor) editorKey(key string) tea.Cmd {
	var rows []editField
	if m.page != 4 {
		for _, c := range m.controls() {
			rows = append(rows, c.field)
		}
	}
	switch key {
	case "ctrl+r":
		view, err := m.command.view(m.options.Root)
		if err != nil {
			m.notice = err.Error()
			return nil
		}
		m.view = view
		m.page, m.cursor = 4, 0
		return m.review()
	case "q", "esc":
		m.quitAfter = key == "q"
		if m.draft.dirty() || m.formErrors() {
			m.confirm, m.confirmCursor = "leave", 0
			return nil
		}
		return m.leave()
	case "left":
		return m.stepSection(-1)
	case "right", "tab":
		return m.stepSection(1)
	case "enter", "space", " ":
		if m.page == 4 {
			return nil // Review content is read-only; Save is a footer action.
		}
		if len(rows) > 0 && m.visibleForm() {
			cmd, _ := m.controlKeypress(tea.KeyPressMsg{Code: tea.KeyEnter})
			return cmd
		}
	case "r":
		if m.page == 4 {
			return m.review()
		}
		if len(rows) > 0 {
			if err := m.resetEditorField(rows[m.cursor].Path); err != nil {
				m.notice = err.Error()
			}
			delete(m.values, rows[m.cursor].Path)
			delete(m.valueErrors, rows[m.cursor].Path)
			delete(m.customModes, rows[m.cursor].Path)
			m.clearHiddenInputs()
			m.controlKey = ""
			m.cursor = min(m.cursor, max(0, len(m.controls())-1))
		}
	case "v":
		m.page, m.cursor = 4, 0
		return m.review()
	case "s", "ctrl+s":
		return m.save()
	case "c":
		return m.beginNew(true)
	case "w":
		m.openWeights()
	case "t":
		m.openTemplate()
	case "d":
		m.sourceDetails()
	case "u":
		if m.beforeConversion != nil {
			m.confirm, m.confirmCursor = "undo", 0
		}
	}
	return nil
}

func (m *Editor) refreshCatalog() error {
	view, err := m.command.view(m.options.Root)
	if err != nil {
		return err
	}
	m.view = view
	return m.reload()
}

func fieldText(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func (m *Editor) inputKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	m.focus = true
	if key == "tab" || key == "shift+tab" || key == "up" || key == "down" {
		m.area = (tui.FocusOrder{Rows: 1}).Cycle(m.area, 1)
		if m.area == contentArea {
			return m.focusInput()
		}
		m.actionCursor = 1
		m.text.Blur()
		return nil
	}
	if m.area == actionsArea && key != "esc" {
		switch key {
		case "left":
			m.actionCursor = tui.CycleChoice(m.actionCursor, 2, -1)
		case "right":
			m.actionCursor = tui.CycleChoice(m.actionCursor, 2, 1)
		case "home":
			m.actionCursor = 0
		case "end":
			m.actionCursor = 1
		case "enter", "space":
			key = m.actions()[m.actionCursor].key
		default:
			return nil
		}
		if key != "enter" && key != "esc" {
			return nil
		}
	}
	if key == "esc" {
		m.input = ""
		m.area = contentArea
		m.text.Blur()
		m.notice = ""
		return nil
	}
	if key != "enter" {
		var cmd tea.Cmd
		m.text, cmd = m.text.Update(msg)
		return cmd
	}
	m.area = contentArea
	focus := m.focusInput()
	text := m.text.Value()
	switch m.input {
	case "new-name":
		if _, err := parseField(catalog.Field{Type: catalog.FieldText}, text); err != nil {
			m.notice = err.Error()
			return focus
		}
		text = strings.TrimSpace(text)
		if text == "" {
			m.notice = "Enter a preset name."
			return focus
		}
		id, err := m.availablePresetID(text)
		if err != nil {
			m.notice = err.Error()
			return focus
		}
		if m.pending.Fork() {
			m.pending.Name = text
		} else if text != "" {
			m.pending.Preset.Presentation.DisplayName = text
		}
		m.draft = draftFrom(id, m.pending, nil)
		m.draft.templates = m.pendingTemplates
		m.pendingTemplates = nil
		m.clearFormInputs()
		m.overview = nil
		m.mode, m.page, m.cursor = "editor", 0, 0
		m.beforeConversion = nil
	case "browse-path":
		if err := m.readDirectory(text); err != nil {
			m.notice = err.Error()
			return focus
		}
	case "filter":
		m.fileFilter = text
		m.cursor = 0
	}
	m.input = ""
	m.area = contentArea
	m.notice = ""
	m.text.Blur()
	m.focus = true
	return nil
}

func (m *Editor) leave() tea.Cmd {
	m.confirm = ""
	m.draft = nil
	m.mode = "manager"
	m.page, m.cursor = 0, 0
	m.beforeConversion = nil
	m.clearFormInputs()
	if m.quitAfter {
		m.Close()
		return tea.Quit
	}
	return nil
}

func (m *Editor) confirmKey(key string) tea.Cmd {
	count := 2
	if m.confirm == "leave" {
		count = 3
	}
	order := tui.FocusOrder{Rows: count}
	switch key {
	case "up", "down", "tab", "shift+tab":
		delta := 1
		if key == "up" || key == "shift+tab" {
			delta = -1
		}
		if key == "tab" || key == "shift+tab" {
			m.area = order.Cycle(m.area, delta)
		} else {
			m.area, m.confirmCursor = order.Vertical(m.area, m.confirmCursor, delta)
		}
		if m.area == actionsArea {
			m.actionCursor = 1
		}
		return nil
	case "left", "right":
		if m.area == actionsArea {
			m.actionCursor = tui.CycleChoice(m.actionCursor, 2, 1)
			return nil
		}
	case "enter", "space", " ":
		key = "enter"
		if m.area == actionsArea && m.actionCursor == 0 {
			key = "esc"
		}
		m.area = contentArea
	}
	switch key {
	case "esc":
		m.confirm = ""
		m.area = contentArea
	case "left":
		m.confirmCursor = max(0, m.confirmCursor-1)
	case "right":
		m.confirmCursor = (m.confirmCursor + 1) % count
	case "enter":
		if m.confirmCursor == 0 {
			m.confirm = ""
			return nil
		}
		switch m.confirm {
		case "leave":
			if m.confirmCursor == 1 {
				return m.leave()
			}
			m.confirm = ""
			return m.save()
		case "delete":
			changed, err := userpreset.Delete(m.ctx, m.options.Root, m.deleteID, m.deleteRaw, false)
			m.confirm = ""
			if err != nil {
				m.notice = err.Error()
			} else {
				m.notice = "Deleted " + m.deleteID + ". Saved layout locks are unchanged."
			}
			if err == nil || changed {
				if e := m.reload(); e != nil {
					m.notice += " · " + e.Error()
				}
				m.cursor = 0
			}
		case "undo":
			u, err := catalog.ParseUserPreset(m.draft.id, m.beforeConversion)
			if err != nil {
				m.notice = err.Error()
			} else {
				m.draft.file = u
				m.clearFormInputs()
				m.beforeConversion = nil
				m.page, m.cursor = 0, 0
			}
			m.confirm = ""
		}
	}
	return nil
}

func (m *Editor) save() tea.Cmd {
	if m.formErrors() {
		m.notice = "Correct " + m.firstFormError() + " before saving. Your draft has not been saved."
		return nil
	}
	changed, err := m.command.saveDraft(m.ctx, m.options.Root, m.draft, m.view, m.memory, m.options.DryRun)
	if err != nil && !changed {
		m.notice = "Cannot save: " + err.Error()
		return nil
	}
	if m.options.DryRun {
		m.notice = "Dry run: preset validates; nothing was saved."
		return nil
	}
	id := m.draft.id
	m.quitAfter = false
	m.leave()
	m.catalogTab = false
	if e := m.reload(); e != nil {
		m.notice = e.Error()
		return nil
	}
	m.notice = "Saved " + id + ". Add or replace it in a layout explicitly; existing locks are unchanged."
	if !changed {
		m.notice = "No changes to " + id + "."
	}
	if err != nil {
		m.notice = err.Error()
	}
	return nil
}

func (m *Editor) references(id string) string {
	c, _, err := setup.ReadConfiguration(m.options.Root)
	if err != nil {
		return "Layout references unavailable: " + err.Error()
	}
	var refs []string
	for key, preset := range c.Presets {
		if key == id || preset.UserPreset == id {
			refs = append(refs, c.References(key)...)
		}
	}
	slices.Sort(refs)
	refs = slices.Compact(refs)
	if len(refs) == 0 {
		return "No saved layout references."
	}
	return "Saved layouts: " + strings.Join(refs, ", ") + ". Their exact locks stay unchanged."
}

func (m *Editor) inspect(p managerPreset) tea.Cmd {
	if !p.mine {
		m.inspectCatalog(p)
		return nil
	}
	r := checkOne(m.options.Root, p.id, m.view, m.memory, m.facts)
	var b strings.Builder
	describe(&b, r)
	m.report = strings.Split(b.String(), "\n")
	m.report = append(m.report, m.references(p.id))
	m.reportTitle = "Check"
	m.returnMode, m.mode = "manager", "report"
	m.viewport.GotoTop()
	return nil
}

func (m *Editor) review() tea.Cmd {
	if m.formErrors() {
		m.report = []string{"Correct " + m.firstFormError() + " before reviewing or saving."}
		return nil
	}
	raw := mustDraftBytes(m.draft.file)
	loaded, err := userpreset.ResolveWithTemplates(m.options.Root, m.draft.id, raw, m.view, m.draft.templates)
	if err != nil {
		m.report = []string{"Invalid: " + err.Error()}
		return nil
	}
	r := checkLoaded(m.draft.id, loaded, m.view, m.memory, m.facts)
	var b strings.Builder
	describe(&b, r)
	m.report = append(strings.Split(b.String(), "\n"), m.references(m.draft.id), "Your preset is untested; no quality or speed claim.")
	m.viewport.GotoTop()
	if !r.Valid {
		return nil
	}
	lock, err := catalog.CompilePreset(loaded.Document, m.draft.id, m.memory, 0, target)
	if err != nil {
		m.report = append(m.report, err.Error())
		return nil
	}
	if m.command.Plan == nil {
		m.report = append(m.report, "Download/disk estimate unavailable.")
		return nil
	}
	next := setup.EmptyConfiguration()
	next.Presets[m.draft.id] = setup.Preset{Name: r.Name, Lock: lock}
	return m.job("review", func(ctx context.Context) editorJob {
		plan, err := m.command.Plan(ctx, m.options.Root, next)
		if err != nil {
			return editorJob{lines: []string{"Download/disk estimate unavailable: " + err.Error()}}
		}
		lines := []string{"Remaining downloads: " + setup.Size(plan.RemainingDownloadBytes), "Remaining disk allowance: " + setup.Size(plan.RemainingDiskBytes)}
		if plan.HFCache != nil {
			lines = append(lines, "Hugging Face cache disk allowance: "+setup.Size(plan.HFCache.RemainingDiskBytes))
		}
		lines = append(lines, plan.Refusals...)
		return editorJob{lines: lines}
	})
}

// cloneUser keeps asynchronous reads from sharing mutable maps with a draft.
func cloneUser(u catalog.UserPreset) catalog.UserPreset {
	raw, _ := json.Marshal(u)
	var clone catalog.UserPreset
	_ = json.Unmarshal(raw, &clone)
	return clone
}
