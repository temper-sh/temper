package presetcmd

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/userpreset"
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
		m.form = append(m.form, formField{label: "Model relationship", choices: []string{"same model, another build", "different model: convert to copy"}})
	}
	m.mode, m.cursor, m.controlKey = "form", 0, ""
}

func (m *Editor) openTemplate() {
	m.formKind = "template"
	m.selectedTemplate = nil
	m.form = []formField{
		{label: "Source", value: "builtin", choices: []string{"builtin", "local", "huggingface"}},
		{label: "Repository"}, {label: "Revision", value: "main"}, {label: "File"},
	}
	id, current := m.currentTemplate()
	if id != catalog.BuiltinTemplate && len(current.Files) > 0 {
		m.form[3].value = current.Files[0].Path
		if len(current.Sources) > 0 && current.Sources[0].Hub == catalog.HubLocal {
			m.form[0].value = "local"
			if content, ok := m.draft.templates[current.Files[0].Path]; ok {
				m.selectedTemplate = &userpreset.TemplateFile{Name: current.Files[0].Path, ManagedName: current.Files[0].Path, Content: content}
			}
		} else {
			m.form[0].value = "huggingface"
			m.form[1].value = current.HuggingFace().Repo
			m.form[2].value = current.HuggingFace().Revision
		}
	}
	m.mode, m.cursor, m.controlKey = "form", 0, ""
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
	if key == "esc" || key == "q" {
		m.mode, m.cursor, m.controlKey = "editor", 0, ""
		m.selectedTemplate = nil
	}
	return nil
}

// Source forms accept a repository name or its HTTPS repository page. Branches
// and files remain separate explicit controls.
func repositoryName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "://") {
		u, err := url.Parse(value)
		if err != nil || u.Scheme != "https" || u.Host != "huggingface.co" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return "", errors.New("Use a Hugging Face repository URL or owner/name.")
		}
		value = strings.Trim(u.Path, "/")
	}
	parts := strings.Split(value, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(value, " \t\r\n@?#") {
		return "", errors.New("Repository must be owner/name or https://huggingface.co/owner/name.")
	}
	return value, nil
}

func (m *Editor) submitForm() tea.Cmd {
	if m.formKind == "weights" {
		repo, err := repositoryName(m.form[0].value)
		if err != nil {
			m.notice = err.Error()
			return nil
		}
		m.form[0].value = repo
		revision := strings.TrimSpace(m.form[1].value)
		if repo == "" || revision == "" {
			m.notice = "Enter a repository and revision."
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
			m.notice = "Choose a template file with Browse…."
			return nil
		}
	case "huggingface":
		repo, err := repositoryName(m.form[1].value)
		if err != nil {
			m.notice = err.Error()
			return nil
		}
		m.form[1].value = repo
		o.transform = m.templateTransform(m.form[1].value, m.form[3].value)
		if m.form[1].value == "" || m.form[2].value == "" || m.form[3].value == "" {
			m.notice = "Enter the repository, revision and template file."
			return nil
		}
		values = append(values, m.form[1].value+"@"+m.form[2].value, m.form[3].value)
	}
	selected := m.selectedTemplate
	if m.form[0].value != "local" {
		selected = nil
	}
	builtin := m.form[0].value == "builtin"
	view := m.view
	return m.job("material", func(ctx context.Context) editorJob {
		tid, record, _, err := m.command.templateRecord(ctx, id, values, o)
		if err == nil {
			err = applyTemplate(&u, view, tid, record, builtin)
		}
		return editorJob{file: u, template: selected, err: err, warning: "Template changed in the draft. Review and Save when ready."}
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
		m.mode, m.cursor, m.controlKey = "form", 0, ""
	case "space", " ", "enter", "left", "right":
		if m.cursor < len(files) {
			if key == "space" || key == " " || key == "enter" {
				m.files[files[m.cursor]] = !m.files[files[m.cursor]]
			}
		} else if m.needsModelRelationship() {
			if m.form[2].value == m.form[2].choices[0] {
				m.form[2].value = m.form[2].choices[1]
			} else {
				m.form[2].value = m.form[2].choices[0]
			}
		}
	case "/":
		return m.prompt("filter", m.fileFilter)
	}
	return nil
}

func (m *Editor) selectWeights() tea.Cmd {
	if m.needsModelRelationship() && m.form[2].value == "" {
		m.notice = "Choose whether these weights are the same model or a different model."
		m.cursor = len(m.visibleFiles())
		return nil
	}
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
	case "rapid-mlx/v1", "kapitsa/v1":
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
			if m.formErrors() {
				m.notice = "Correct invalid draft inputs before converting to a copy."
				return nil
			}
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
			return "Use a chat template saved on this computer."
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
