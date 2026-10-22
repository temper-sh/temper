package presetcmd

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/render/engine"
	"github.com/temper-sh/temper/internal/userpreset"
)

// The file/command field table is a serialization surface. The editor exposes
// decisions supported by this preset, preserving every other authored value.
func (m *Editor) fieldsFor(page int) []editField {
	p := m.draft.base(m.view, m.memory)
	all := m.draft.fields(m.view, m.memory)
	values := map[string]any{}
	for _, f := range all {
		values[f.Path] = f.current
	}
	chat := p.Interface == engine.InterfaceChatCompletions
	kind := p.Config().Kind
	splash := strings.HasPrefix(kind, "splash/")
	reasoning := values["request_defaults.reasoning"] == "on"
	spec := m.currentSpeculation()
	var rows []editField
	for _, f := range all {
		if section(f.Path) != page {
			continue
		}
		switch f.Path {
		case "context_limit_tokens", "speculation.source", "speculation.draft_artifact",
			"engine_config.rapid_mlx.tool_call_parser", "engine_config.rapid_mlx.reasoning_parser",
			"engine_config.vllm_metal.tool_call_parser", "engine_config.vllm_metal.reasoning_parser",
			"engine_config.vllm_metal.block_size":
			continue
		case "compute":
			if len(p.ComputeOptions) < 2 {
				continue
			}
			f.Choices = nil
			for _, device := range p.ComputeOptions {
				f.Choices = append(f.Choices, string(device))
			}
		case "request_defaults.reasoning":
			if splash || kind == "rapid-mlx/v1" && m.reasoningParser() == "" {
				continue
			}
		case "request_defaults.reasoning_budget_tokens", "engine_config.controls.reasoning_effort", "engine_config.controls.preserve_reasoning":
			if !chat || !reasoning {
				continue
			}
		case "request_defaults.sampling.min_p", "request_defaults.sampling.repeat_penalty", "request_defaults.sampling.presence_penalty":
			if splash {
				continue
			}
		case "speculation.method":
			choices := m.speculationChoices()
			if len(choices) < 2 {
				continue
			}
			f.Choices = nil
			for _, choice := range choices {
				f.Choices = append(f.Choices, choice.label)
			}
			f.current = m.speculationLabel(spec)
			f.origin = m.speculationLabel(p.Speculation)
			f.Help = "Choose a speculation configuration available for these weights and engine."
		case "speculation.max_draft_tokens":
			if spec.Method == "none" || splash {
				continue
			}
		case "engine_config.gpu_layers":
			if values["compute"] == string(catalog.ComputeCPU) {
				continue
			}
		case "engine_config.rapid_mlx.cache_memory_mib":
			if values["engine_config.rapid_mlx.prefix_cache"] != "on" {
				continue
			}
		case "engine_config.rapid_mlx.pflash":
			if slices.Contains(p.Modalities, "image") {
				continue
			}
		case "engine_config.vllm_metal.max_num_seqs", "engine_config.vllm_metal.prefix_cache":
			if spec.Method == "mtp" {
				continue
			}
		}
		if !chat && (f.Path == "template" || strings.HasPrefix(f.Path, "request_defaults.") || strings.HasPrefix(f.Path, "speculation.")) {
			continue
		}
		rows = append(rows, f)
	}
	return rows
}

func (m *Editor) sections() ([]string, []int) {
	var labels []string
	var pages []int
	for page, label := range editorSections {
		if page == 4 || len(m.fieldsFor(page)) > 0 {
			labels, pages = append(labels, label), append(pages, page)
		}
	}
	return labels, pages
}

func (m *Editor) stepSection(delta int) tea.Cmd {
	_, pages := m.sections()
	index := max(0, slices.Index(pages, m.page))
	return m.selectSection(pages[(index+delta+len(pages))%len(pages)])
}

func (m *Editor) availablePresetID(name string) (string, error) {
	base := strings.Trim(invalidIDRun.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if base == "" {
		base = "preset"
	}
	for suffix := 1; ; suffix++ {
		id := base
		if suffix > 1 {
			id += "-" + strconv.Itoa(suffix)
		}
		if userpreset.InCatalog(m.view, id) || m.draft != nil && m.draft.id == id {
			continue
		}
		exists, err := userpreset.Exists(m.options.Root, id)
		if err != nil {
			return "", err
		}
		if !exists {
			return id, nil
		}
	}
}

func (m *Editor) initialPreset() catalog.Preset {
	u, err := catalog.ParseUserPreset(m.draft.id, m.draft.initial)
	if err == nil && u.Preset != nil {
		p := *u.Preset
		p.Profiles = []catalog.Profile{p.SelectProfile(m.memory)}
		return p
	}
	return m.draft.base(m.view, m.memory)
}

func (m *Editor) reasoningParser() string {
	initial, _ := catalog.ParseUserPreset(m.draft.id, m.draft.initial)
	for _, file := range []catalog.UserPreset{m.draft.file, initial} {
		if parser, ok := nestedValue(file.Changes, "engine_config.rapid_mlx.reasoning_parser"); ok && parser != "" {
			if value, ok := parser.(string); ok {
				return value
			}
		}
	}
	for _, p := range []catalog.Preset{m.draft.base(m.view, m.memory), m.initialPreset()} {
		if config := p.Config().Python; config != nil && config.RapidMLX != nil && config.RapidMLX.ReasoningParser != "" {
			return config.RapidMLX.ReasoningParser
		}
	}
	p := m.draft.base(m.view, m.memory)
	artifact := m.catalogArtifact()
	for _, id := range sortedKeys(m.view.Document.Presets) {
		other := m.view.Document.Presets[id]
		if artifact != "" && other.Artifact == artifact && other.Engine == p.Engine {
			if config := other.Config().Python; config != nil && config.RapidMLX != nil && config.RapidMLX.ReasoningParser != "" {
				return config.RapidMLX.ReasoningParser
			}
		}
	}
	return ""
}

func (m *Editor) catalogArtifact() string {
	id := m.draft.base(m.view, m.memory).Artifact
	if changed, ok := m.draft.file.Changes["artifact"].(string); ok {
		id = changed
	}
	if own, ok := m.draft.file.Artifacts[id]; ok {
		if own.SameModelAs != "" {
			return own.SameModelAs
		}
		known, ok := m.view.Document.Artifacts[id]
		if !ok || known.Format != own.Format || !slices.Equal(known.Files, own.Files) {
			return ""
		}
	}
	return id
}

type speculationChoice struct {
	label string
	spec  catalog.Speculation
}

func (m *Editor) currentSpeculation() catalog.Speculation {
	spec := m.draft.base(m.view, m.memory).Speculation
	if m.draft.file.Fork() {
		for path, target := range map[string]*string{"method": &spec.Method, "source": &spec.Source, "draft_artifact": &spec.DraftArtifact} {
			if value, ok := nestedValue(m.draft.file.Changes, "speculation."+path); ok {
				*target, _ = value.(string)
			}
		}
		if value, ok := nestedValue(m.draft.file.Changes, "speculation.max_draft_tokens"); ok {
			spec.MaxDraftTokens, _ = strconv.Atoi(fmt.Sprint(value))
		}
	}
	if spec.Method == "" {
		spec.Method, spec.Source = "none", "none"
	}
	return spec
}

func (m *Editor) speculationLabel(spec catalog.Speculation) string {
	if spec.Method == "" || spec.Method == "none" {
		return "Off"
	}
	label := strings.ToUpper(spec.Method)
	if spec.DraftArtifact != "" {
		a := m.view.Document.Artifacts[spec.DraftArtifact]
		if own, ok := m.draft.file.Artifacts[spec.DraftArtifact]; ok {
			a = own.Artifact
		}
		name := a.Presentation.ModelName
		if name == "" && len(a.Files) > 0 {
			name = a.Files[0].Path
		}
		if name == "" {
			name = spec.DraftArtifact
		}
		return label + " · " + name
	}
	return label + " · from model"
}

func (m *Editor) speculationChoices() []speculationChoice {
	p := m.draft.base(m.view, m.memory)
	var choices []speculationChoice
	add := func(spec catalog.Speculation) {
		if spec.Method == "" {
			spec.Method, spec.Source = "none", "none"
		}
		for _, choice := range choices {
			if choice.spec.Method == spec.Method && choice.spec.Source == spec.Source && choice.spec.DraftArtifact == spec.DraftArtifact {
				return
			}
		}
		choices = append(choices, speculationChoice{label: m.speculationLabel(spec), spec: spec})
	}
	if !strings.HasPrefix(p.Config().Kind, "splash/") {
		add(catalog.Speculation{Method: "none", Source: "none"})
	}
	add(m.currentSpeculation())
	add(m.initialPreset().Speculation)
	artifact := m.catalogArtifact()
	for _, id := range sortedKeys(m.view.Document.Presets) {
		other := m.view.Document.Presets[id]
		if artifact != "" && other.Artifact == artifact && other.Engine == p.Engine {
			add(other.Speculation)
		}
	}
	return choices
}

func (m *Editor) setEditorField(field catalog.Field, text string) error {
	draft := *m.draft
	draft.file = cloneUser(draft.file)
	if field.Path == "speculation.method" {
		var choice *speculationChoice
		for _, option := range m.speculationChoices() {
			if option.label == text {
				choice = &option
				break
			}
		}
		if choice == nil {
			return fmt.Errorf("choose %s", strings.Join(field.Choices, ", "))
		}
		if id := choice.spec.DraftArtifact; id != "" && !draft.file.Fork() {
			if _, ok := draft.file.Artifacts[id]; !ok {
				draft.file.Artifacts[id] = catalog.UserArtifact{Artifact: m.view.Document.Artifacts[id]}
			}
		}
		for path, value := range map[string]any{"method": choice.spec.Method, "source": choice.spec.Source, "draft_artifact": choice.spec.DraftArtifact, "max_draft_tokens": choice.spec.MaxDraftTokens} {
			if err := draft.setValue("speculation."+path, value, m.memory); err != nil {
				return err
			}
		}
		if draft.base(m.view, m.memory).Config().Kind == "vllm-metal/v1" && choice.spec.Method == "mtp" {
			for path, value := range map[string]any{"max_num_seqs": 1, "prefix_cache": "off"} {
				if err := draft.setValue("engine_config.vllm_metal."+path, value, m.memory); err != nil {
					return err
				}
			}
		}
	} else if err := draft.set(field, text, m.memory); err != nil {
		return err
	}
	kind := draft.base(m.view, m.memory).Config().Kind
	if field.Path == "request_defaults.reasoning" && kind == "rapid-mlx/v1" {
		parser := ""
		if text == "on" {
			parser = m.reasoningParser()
		}
		if err := draft.setValue("engine_config.rapid_mlx.reasoning_parser", parser, m.memory); err != nil {
			return err
		}
	}
	if field.Path == "engine_config.reasoning_effort" && strings.HasPrefix(kind, "splash/") {
		reasoning := "on"
		if text == "none" {
			reasoning = "off"
		}
		if err := draft.setValue("request_defaults.reasoning", reasoning, m.memory); err != nil {
			return err
		}
	}
	m.draft.file = draft.file
	return nil
}

func (m *Editor) resetEditorField(path string) error {
	paths := []string{path}
	if path == "speculation.method" {
		paths = []string{"speculation.method", "speculation.source", "speculation.draft_artifact", "speculation.max_draft_tokens"}
	}
	kind := m.draft.base(m.view, m.memory).Config().Kind
	if path == "speculation.method" && kind == "vllm-metal/v1" {
		paths = append(paths, "engine_config.vllm_metal.max_num_seqs", "engine_config.vllm_metal.prefix_cache")
	}
	if path == "request_defaults.reasoning" && kind == "rapid-mlx/v1" {
		paths = append(paths, "engine_config.rapid_mlx.reasoning_parser")
	}
	if path == "engine_config.reasoning_effort" && strings.HasPrefix(kind, "splash/") {
		paths = append(paths, "request_defaults.reasoning")
	}
	draft := *m.draft
	draft.file = cloneUser(draft.file)
	for _, path := range paths {
		if err := draft.reset(path, m.memory); err != nil {
			return err
		}
	}
	m.draft.file = draft.file
	return nil
}
