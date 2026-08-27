// Package manifest parses and validates the user-owned Temper manifest.
package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	patchsource "github.com/temper-sh/temper/internal/patch"
	"gopkg.in/yaml.v3"
)

const SchemaV1 = "temper-manifest/v1"

var (
	idPattern   = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	repoPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)
)

type Document struct {
	Schema   string            `yaml:"schema"`
	Defaults Defaults          `yaml:"defaults"`
	Patches  map[string]Patch  `yaml:"patches"`
	Layouts  map[string]Layout `yaml:"layouts"`
	Tools    map[string]Tool   `yaml:"tools"`
	Modes    map[string]Mode   `yaml:"modes"`
}

type Defaults struct {
	TTL                  int     `yaml:"ttl"`
	GPUMemoryUtilization float64 `yaml:"gpu_memory_utilization"`
}

type Patch struct {
	Source string `yaml:"source"`
	File   string `yaml:"file"`
}

type Layout struct {
	DisplayName  string      `yaml:"display_name"`
	Model        Model       `yaml:"model"`
	Engine       string      `yaml:"engine"`
	Role         string      `yaml:"role"`
	Window       int         `yaml:"window"`
	MaxTokens    int         `yaml:"max_tokens"`
	KV           string      `yaml:"kv"`
	Thinking     string      `yaml:"thinking"`
	ChatTemplate string      `yaml:"chat_template"`
	Llama        LlamaTuning `yaml:"llama"`
}

type Model struct {
	Repo string `yaml:"repo"`
	File string `yaml:"file"`
}

type LlamaTuning struct {
	Parallel       int    `yaml:"parallel"`
	FlashAttention string `yaml:"flash_attention"`
	Batch          int    `yaml:"batch"`
	UBatch         int    `yaml:"ubatch"`
	SpecType       string `yaml:"spec_type,omitempty"`
	SpecDraftNMax  int    `yaml:"spec_draft_n_max,omitempty"`
}

type Tool struct {
	Source string   `yaml:"source"`
	Needs  []string `yaml:"needs"`
}

type Mode struct {
	Foreground string   `yaml:"foreground"`
	Tools      []string `yaml:"tools"`
	Harnesses  []string `yaml:"harnesses"`
	Members    Members  `yaml:"members"`
}

type Members struct {
	Resident []Member `yaml:"resident"`
	OnDemand []Member `yaml:"on_demand"`
}

type Member struct {
	Layout    string `yaml:"layout"`
	TTL       *int   `yaml:"ttl"`
	NGL       *int   `yaml:"ngl"`
	Preferred bool   `yaml:"preferred"`
	Preload   bool   `yaml:"preload"`
}

type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "manifest invalid: " + strings.Join(e.Problems, "; ")
}

func Parse(data []byte) (Document, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	var document Document
	if err := decoder.Decode(&document); err != nil {
		return Document{}, fmt.Errorf("decode manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Document{}, errors.New("decode manifest: multiple YAML documents are not allowed")
		}
		return Document{}, fmt.Errorf("decode manifest: %w", err)
	}
	if err := document.Validate(); err != nil {
		return Document{}, err
	}
	return document, nil
}

func (d Document) Validate() error {
	var problems []string
	problem := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	if d.Schema != SchemaV1 {
		problem("schema is %q, want %q", d.Schema, SchemaV1)
	}
	if d.Defaults.TTL < 0 {
		problem("defaults.ttl must be zero or greater")
	}
	if d.Defaults.GPUMemoryUtilization <= 0 || d.Defaults.GPUMemoryUtilization > 1 {
		problem("defaults.gpu_memory_utilization must be greater than zero and at most one")
	}
	if len(d.Layouts) == 0 {
		problem("layouts must not be empty")
	}
	if len(d.Modes) == 0 {
		problem("modes must not be empty")
	}

	for _, id := range sortedKeys(d.Patches) {
		patch := d.Patches[id]
		if !validID(id) {
			problem("patch id %q is not a lowercase stable id", id)
		}
		if patch.Source == "" {
			problem("patch %q source is required", id)
		} else if _, err := patchsource.ParseSource(patch.Source); err != nil {
			problem("patch %q source is invalid: %v", id, err)
		}
		if !safeRelativePath(patch.File) {
			problem("patch %q file %q is not a safe relative path", id, patch.File)
		}
	}

	for _, id := range sortedKeys(d.Layouts) {
		layout := d.Layouts[id]
		if !validID(id) {
			problem("layout id %q is not a lowercase stable id", id)
		}
		if layout.DisplayName == "" {
			problem("layout %q display_name is required", id)
		}
		if !repoPattern.MatchString(layout.Model.Repo) {
			problem("layout %q model.repo %q must be owner/name", id, layout.Model.Repo)
		}
		if !safeRelativePath(layout.Model.File) {
			problem("layout %q model.file %q is not a safe relative path", id, layout.Model.File)
		}
		if layout.Engine != "llama-server" {
			problem("layout %q engine %q is not supported by manifest v1", id, layout.Engine)
		}
		if layout.Role != "coder" && layout.Role != "rerank" {
			problem("layout %q role %q must be coder or rerank", id, layout.Role)
		}
		if layout.Window <= 0 {
			problem("layout %q window must be greater than zero", id)
		}
		if layout.Llama.Parallel <= 0 || layout.Llama.Batch <= 0 || layout.Llama.UBatch <= 0 {
			problem("layout %q llama parallel, batch and ubatch must be greater than zero", id)
		}
		if layout.Llama.FlashAttention != "on" && layout.Llama.FlashAttention != "off" && layout.Llama.FlashAttention != "auto" {
			problem("layout %q llama.flash_attention %q must be on, off or auto", id, layout.Llama.FlashAttention)
		}
		switch layout.Llama.SpecType {
		case "":
			if layout.Llama.SpecDraftNMax != 0 {
				problem("layout %q llama.spec_draft_n_max requires spec_type", id)
			}
		case "draft-mtp":
			if layout.Role != "coder" {
				problem("layout %q llama.spec_type draft-mtp is supported only for coder layouts", id)
			}
			if layout.Llama.SpecDraftNMax <= 0 || layout.Llama.SpecDraftNMax > 16 {
				problem("layout %q llama.spec_draft_n_max must be between 1 and 16 for draft-mtp", id)
			}
		default:
			problem("layout %q llama.spec_type %q is unsupported", id, layout.Llama.SpecType)
		}

		switch layout.Role {
		case "coder":
			if layout.MaxTokens <= 0 || layout.MaxTokens >= layout.Window {
				problem("layout %q max_tokens must be greater than zero and below window", id)
			}
			if layout.KV != "q8" && layout.KV != "f16" {
				problem("layout %q kv %q must be q8 or f16", id, layout.KV)
			}
			if layout.Thinking != "on" && layout.Thinking != "off" {
				problem("layout %q thinking %q must be on or off", id, layout.Thinking)
			}
		case "rerank":
			if layout.MaxTokens != 0 || layout.KV != "" || layout.Thinking != "" || layout.ChatTemplate != "" {
				problem("layout %q reranker cannot declare coder-only tuning", id)
			}
		}

		if layout.ChatTemplate != "" {
			if _, ok := d.Patches[layout.ChatTemplate]; !ok {
				problem("layout %q chat_template references unknown patch %q", id, layout.ChatTemplate)
			}
		}
	}

	for _, id := range sortedKeys(d.Tools) {
		tool := d.Tools[id]
		if !validID(id) {
			problem("tool id %q is not a lowercase stable id", id)
		}
		if tool.Source == "" {
			problem("tool %q source is required", id)
		}
		if len(tool.Needs) == 0 {
			problem("tool %q needs must not be empty", id)
		}
		for _, role := range tool.Needs {
			if role != "coder" && role != "rerank" {
				problem("tool %q needs unsupported role %q", id, role)
			}
		}
		if duplicate := firstDuplicate(tool.Needs); duplicate != "" {
			problem("tool %q repeats role %q", id, duplicate)
		}
	}

	for _, id := range sortedKeys(d.Modes) {
		mode := d.Modes[id]
		if !validID(id) {
			problem("mode id %q is not a lowercase stable id", id)
		}
		if mode.Foreground != "local" && mode.Foreground != "none" {
			problem("mode %q foreground %q must be local or none in manifest v1", id, mode.Foreground)
		}
		if duplicate := firstDuplicate(mode.Tools); duplicate != "" {
			problem("mode %q repeats tool %q", id, duplicate)
		}
		if duplicate := firstDuplicate(mode.Harnesses); duplicate != "" {
			problem("mode %q repeats harness %q", id, duplicate)
		}
		for _, harness := range mode.Harnesses {
			if harness != "pi" {
				problem("mode %q harness %q is not supported by manifest v1", id, harness)
			}
		}

		roles := map[string]bool{}
		seenLayouts := map[string]bool{}
		residentCoders := 0
		preferredCoders := 0
		validateMembers := func(placement string, members []Member) {
			for index, member := range members {
				location := fmt.Sprintf("mode %q members.%s[%d]", id, placement, index)
				layout, ok := d.Layouts[member.Layout]
				if !ok {
					problem("%s references unknown layout %q", location, member.Layout)
					continue
				}
				if seenLayouts[member.Layout] {
					problem("mode %q repeats layout %q", id, member.Layout)
				}
				seenLayouts[member.Layout] = true
				roles[layout.Role] = true
				if member.TTL != nil && *member.TTL < 0 {
					problem("%s ttl must be zero or greater", location)
				}
				if member.NGL != nil && *member.NGL < 0 {
					problem("%s ngl must be zero or greater", location)
				}
				if placement == "resident" && layout.Role == "coder" {
					residentCoders++
				}
				if member.Preferred {
					if placement != "resident" || layout.Role != "coder" {
						problem("%s preferred is allowed only on a resident coder", location)
					} else {
						preferredCoders++
					}
				}
				if member.Preload && placement != "resident" {
					problem("%s preload is allowed only on a resident member", location)
				}
			}
		}
		validateMembers("resident", mode.Members.Resident)
		validateMembers("on_demand", mode.Members.OnDemand)

		for _, toolID := range mode.Tools {
			tool, ok := d.Tools[toolID]
			if !ok {
				problem("mode %q references unknown tool %q", id, toolID)
				continue
			}
			for _, role := range tool.Needs {
				if !roles[role] {
					problem("mode %q tool %q needs missing role %q", id, toolID, role)
				}
			}
		}

		switch mode.Foreground {
		case "local":
			if residentCoders == 0 {
				problem("mode %q local foreground needs a resident coder", id)
			}
			if preferredCoders != 1 {
				problem("mode %q local foreground needs exactly one preferred resident coder", id)
			}
		case "none":
			if len(seenLayouts) != 0 || len(mode.Tools) != 0 || len(mode.Harnesses) != 0 {
				problem("mode %q with foreground none must be empty", id)
			}
		}
	}

	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

func (d Document) Mode(name string) (Mode, error) {
	mode, ok := d.Modes[name]
	if !ok {
		return Mode{}, fmt.Errorf("mode %q is not selected", name)
	}
	return mode, nil
}

func validID(id string) bool {
	return idPattern.MatchString(id)
}

func safeRelativePath(path string) bool {
	if path == "" || filepath.IsAbs(path) || strings.Contains(path, "\\") || strings.ContainsAny(path, "\r\n\x00") {
		return false
	}
	clean := filepath.Clean(path)
	return clean == path && clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

func firstDuplicate(values []string) string {
	seen := map[string]bool{}
	for _, value := range values {
		if seen[value] {
			return value
		}
		seen[value] = true
	}
	return ""
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
