package presetcmd

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/temper-sh/temper/internal/catalog"
)

func (m *Editor) currentWeights() catalog.Artifact {
	p := m.draft.base(m.view, m.memory)
	a := m.view.Document.Artifacts[p.Artifact]
	if own, ok := m.draft.file.Artifacts[p.Artifact]; ok {
		a = own.Artifact
	}
	if id, ok := m.draft.file.Changes["artifact"].(string); ok {
		if own, ok := m.draft.file.Artifacts[id]; ok {
			a = own.Artifact
		} else if record, ok := m.view.Document.Artifacts[id]; ok {
			a = record
		}
	}
	return a
}

func (m *Editor) currentTemplate() (string, catalog.Template) {
	id := m.draft.base(m.view, m.memory).Template
	if changed, ok := m.draft.file.Changes["template"].(string); ok {
		id = changed
	}
	if own, ok := m.draft.file.Templates[id]; ok {
		return id, own
	}
	return id, m.view.Document.Templates[id]
}

func (m *Editor) openWeights() {
	a := m.currentWeights()
	m.formKind = "weights"
	m.form = []formField{
		{label: "Repository", value: a.HuggingFace().Repo},
		{label: "Revision", value: "main"},
	}
	if m.draft.file.Fork() {
		m.form = append(m.form, formField{label: "Model relationship", value: "Choose…", choices: []string{"same model, another build", "different model: convert to copy"}})
	}
	m.mode, m.cursor = "form", 0
}

func (m *Editor) openTemplate() {
	m.formKind = "template"
	m.form = []formField{
		{label: "Source", value: "builtin", choices: []string{"builtin", "local", "huggingface"}},
		{label: "Repository"},
		{label: "Revision", value: "main"},
		{label: "File"},
	}
	m.mode, m.cursor = "form", 0
}

func (m *Editor) needsModelRelationship() bool {
	if !m.draft.file.Fork() {
		return false
	}
	origin := m.view.Document.Presets[m.draft.file.Origin]
	return strings.TrimSpace(m.form[0].value) != m.view.Document.Artifacts[origin.Artifact].HuggingFace().Repo
}

func (m *Editor) visibleFormRows() []int {
	rows := []int{0}
	if m.formKind == "weights" {
		rows = append(rows, 1)
		if m.needsModelRelationship() {
			rows = append(rows, 2)
		}
		return rows
	}
	switch m.form[0].value {
	case "local":
		rows = append(rows, 3)
	case "huggingface":
		rows = append(rows, 1, 2, 3)
	}
	return rows
}

func (m *Editor) formKey(key string) tea.Cmd {
	rows := append(m.visibleFormRows(), len(m.form))
	position := max(0, slices.Index(rows, m.cursor))
	switch key {
	case "esc", "q":
		m.mode = "editor"
		m.cursor = 0
	case "up", "k":
		m.cursor = rows[max(0, position-1)]
	case "down", "j", "tab":
		m.cursor = rows[(position+1)%len(rows)]
	case "enter", "space", " ":
		if m.cursor == len(m.form) {
			return m.submitForm()
		}
		f := &m.form[m.cursor]
		if len(f.choices) > 0 {
			f.value = f.choices[(slices.Index(f.choices, f.value)+1)%len(f.choices)]
			return nil
		}
		return m.prompt("form", f.value)
	}
	return nil
}

func (m *Editor) submitForm() tea.Cmd {
	if m.formKind == "weights" {
		repo, revision := strings.TrimSpace(m.form[0].value), strings.TrimSpace(m.form[1].value)
		if repo == "" || revision == "" {
			m.notice = "Enter a repository and revision."
			return nil
		}
		if m.needsModelRelationship() && m.form[2].value == "Choose…" {
			m.notice = "Choose whether these weights are the same model or a different model."
			return nil
		}
		if m.command.Hub == nil {
			m.notice = "Hugging Face is unavailable. Existing records can still be edited offline."
			return nil
		}
		return m.job("repository", func(ctx context.Context) editorJob {
			r, err := m.command.Hub.Revision(ctx, repo, revision)
			return editorJob{repository: r, err: err}
		})
	}
	u := cloneUser(m.draft.file)
	id := m.draft.id
	o := options{root: m.options.Root}
	values := []string{id}
	switch m.form[0].value {
	case "builtin":
		values = append(values, catalog.BuiltinTemplate)
	case "local":
		o.local = m.form[3].value
		o.transform = m.templateTransform("", o.local)
		if o.local == "" {
			m.notice = "Enter the local template file name in presets.d."
			return nil
		}
	case "huggingface":
		o.transform = m.templateTransform(m.form[1].value, m.form[3].value)
		if m.form[1].value == "" || m.form[2].value == "" || m.form[3].value == "" {
			m.notice = "Enter the repository, revision and template file."
			return nil
		}
		values = append(values, m.form[1].value+"@"+m.form[2].value, m.form[3].value)
	}
	builtin := m.form[0].value == "builtin"
	view := m.view
	return m.job("material", func(ctx context.Context) editorJob {
		tid, record, _, err := m.command.templateRecord(ctx, id, values, o)
		if err == nil {
			err = applyTemplate(&u, view, tid, record, builtin)
		}
		return editorJob{file: u, err: err, warning: "Template changed in the draft. Review and Save when ready."}
	})
}

func (m *Editor) visibleFiles() []string {
	var files []string
	for _, file := range m.repository.Files {
		if strings.Contains(strings.ToLower(file), strings.ToLower(m.fileFilter)) {
			files = append(files, file)
		}
	}
	return files
}

func (m *Editor) filesKey(key string) tea.Cmd {
	files := m.visibleFiles()
	switch key {
	case "esc", "q":
		m.mode = "form"
		m.cursor = 0
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = min(max(0, len(files)-1), m.cursor+1)
	case "space", " ":
		if len(files) > 0 {
			m.files[files[m.cursor]] = !m.files[files[m.cursor]]
		}
	case "/":
		return m.prompt("filter", m.fileFilter)
	case "enter":
		return m.selectWeights()
	}
	return nil
}

func (m *Editor) selectWeights() tea.Cmd {
	var files []string
	for _, file := range m.repository.Files {
		if m.files[file] {
			files = append(files, file)
		}
	}
	if len(files) == 0 {
		m.notice = "Select at least one file with Space."
		return nil
	}
	u := cloneUser(m.draft.file)
	var conversion []byte
	format := m.currentWeights().Format
	switch m.draft.base(m.view, m.memory).Config().Kind {
	case "llama-server/v1", "splash/v1", "splash/v2", "crispembed/v1":
		format = "gguf"
	case "rapid-mlx/v1":
		format = "mlx-safetensors"
	case "vllm-metal/v1":
		format = "safetensors"
	}
	o := options{root: m.options.Root, format: format}
	if m.needsModelRelationship() {
		switch m.form[2].value {
		case "same model, another build":
			o.sameModelAs = m.view.Document.Presets[u.Origin].Artifact
		case "different model: convert to copy":
			lock, _, err := m.draft.compile(m.options.Root, m.view, m.memory)
			if err != nil {
				m.notice = "Cannot convert: " + err.Error()
				return nil
			}
			conversion = mustDraftBytes(m.draft.file)
			u = catalog.CopyOf(lock, lock.Records.Presets[m.draft.id].Presentation.DisplayName)
		}
	}
	// Fetch only the commit displayed in the picker, even if the branch moves.
	repository := strings.TrimSpace(m.form[0].value) + "@" + m.repository.Commit
	view := m.view
	cmd := m.job("material", func(ctx context.Context) editorJob {
		wid, record, warning, err := m.command.weightsRecord(ctx, repository, files, o)
		if err == nil {
			err = applyWeights(&u, view, wid, record)
		}
		if err == nil && len(record.Files) != len(files) {
			err = errors.New("metadata did not describe every selected file")
		}
		if warning != "" {
			warning += " · "
		}
		warning += "Weights changed in the draft; review the template, context and speculation before saving."
		if conversion != nil {
			warning = "Converted to copy in the draft. " + warning
		}
		return editorJob{file: u, err: err, warning: warning}
	})
	// Retain the undo point before the read. A failed read has not converted
	// anything; Undo still restores the same authoring draft.
	if conversion != nil {
		m.beforeConversion = conversion
	}
	return cmd
}

func (m *Editor) formExplanation() string {
	if m.formKind == "template" {
		switch m.form[0].value {
		case "builtin":
			return "Use the chat template included in the model."
		case "local":
			return "Choose a template file in presets.d."
		default:
			return "Choose a chat template from Hugging Face."
		}
	}
	return "Choose a Hugging Face repository, then select weights."
}

func (m *Editor) sourceDetails() {
	a := m.currentWeights()
	m.report = []string{"Preset ID: " + m.draft.id}
	if m.draft.file.Fork() {
		m.report = append(m.report, "Origin: "+m.draft.file.Origin)
	}
	if limit := m.draft.base(m.view, m.memory).ContextLimitTokens; limit > 0 {
		m.report = append(m.report, fmt.Sprintf("Model context ceiling: %d tokens", limit))
	}
	m.report = append(m.report, "", "Weights", "Format: "+a.Format)
	m.report = append(m.report, materialDetails(a.Sources, a.Files, a.License)...)
	m.report = append(m.report, "", "Template")
	id, template := m.currentTemplate()
	if id == catalog.BuiltinTemplate {
		m.report = append(m.report, "From model")
	} else {
		m.report = append(m.report, materialDetails(template.Sources, template.Files, template.License)...)
		if template.Transform != "" {
			m.report = append(m.report, "Transform: "+template.Transform)
		}
	}
	m.reportTitle, m.mode, m.returnMode = "Source details", "report", "editor"
	m.viewport.GotoTop()
}

// Keep a known source's preparation rule with that source. A new source never
// inherits an unrelated patch, and editing another setting never drops a patch.
func (m *Editor) templateTransform(repo, file string) string {
	matches := func(t catalog.Template) bool {
		if len(t.Files) != 1 || t.Files[0].Path != file {
			return false
		}
		for _, source := range t.Sources {
			if repo == "" && source.Hub == catalog.HubLocal || repo != "" && source.Hub == catalog.HubHuggingFace && source.Repo == repo {
				return true
			}
		}
		return false
	}
	_, current := m.currentTemplate()
	if matches(current) {
		return current.Transform
	}
	return ""
}

func materialDetails(sources []catalog.Source, files []catalog.File, license string) []string {
	var lines []string
	for _, source := range sources {
		if source.Hub == catalog.HubLocal {
			lines = append(lines, "Source: presets.d")
		} else {
			lines = append(lines, "Repository: "+source.Repo, "Pinned revision: "+source.Revision)
		}
	}
	lines = append(lines, "License: "+materialLicense("", license))
	for _, file := range files {
		lines = append(lines, "File: "+file.Path)
		if file.SHA256 != "" {
			lines = append(lines, fmt.Sprintf("Size: %d bytes", file.Bytes), "SHA-256: "+file.SHA256)
		}
	}
	return lines
}
