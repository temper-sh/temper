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

const (
	SchemaV1 = "temper-manifest/v1"
	SchemaV2 = "temper-manifest/v2"
)

var (
	idPattern     = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	repoPattern   = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)
	optionPattern = regexp.MustCompile(`^[a-z0-9]+(?:[_-][a-z0-9]+)*$`)
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
	DisplayName  string           `yaml:"display_name"`
	Model        Model            `yaml:"model"`
	Engine       string           `yaml:"engine"`
	Role         string           `yaml:"role,omitempty"`
	Interface    string           `yaml:"interface,omitempty"`
	Modalities   []string         `yaml:"modalities,omitempty"`
	Window       int              `yaml:"window"`
	MaxTokens    int              `yaml:"max_tokens,omitempty"`
	KV           string           `yaml:"kv,omitempty"`
	Thinking     string           `yaml:"thinking,omitempty"`
	Speculation  *Speculation     `yaml:"speculation,omitempty"`
	ChatTemplate string           `yaml:"chat_template,omitempty"`
	Llama        *LlamaTuning     `yaml:"llama,omitempty"`
	RapidMLX     *RapidMLXTuning  `yaml:"rapid_mlx,omitempty"`
	MLXVLM       *MLXVLMTuning    `yaml:"mlx_vlm,omitempty"`
	VLLMMetal    *VLLMMetalTuning `yaml:"vllm_metal,omitempty"`
}

type Model struct {
	Repo   string   `yaml:"repo"`
	File   string   `yaml:"file,omitempty"`
	Format string   `yaml:"format,omitempty"`
	Files  []string `yaml:"files,omitempty"`
}

type Speculation struct {
	Method    string `yaml:"method"`
	MaxTokens int    `yaml:"max_tokens,omitempty"`
}

type LlamaTuning struct {
	KV                 string `yaml:"kv,omitempty"`
	Parallel           int    `yaml:"parallel"`
	FlashAttention     string `yaml:"flash_attention"`
	Batch              int    `yaml:"batch"`
	UBatch             int    `yaml:"ubatch"`
	SpecType           string `yaml:"spec_type,omitempty"`
	SpecDraftNMax      int    `yaml:"spec_draft_n_max,omitempty"`
	ContextCheckpoints *int   `yaml:"context_checkpoints,omitempty"`
	PromptCacheRAMMiB  *int   `yaml:"prompt_cache_ram_mib,omitempty"`
}

type RapidMLXTuning struct {
	MaxNumSeqs            int     `yaml:"max_num_seqs"`
	MaxConcurrentRequests int     `yaml:"max_concurrent_requests"`
	PrefillBatchSize      int     `yaml:"prefill_batch_size"`
	CompletionBatchSize   int     `yaml:"completion_batch_size"`
	GPUMemoryUtilization  float64 `yaml:"gpu_memory_utilization"`
	PrefixCache           string  `yaml:"prefix_cache"`
	CacheMemoryMiB        *int    `yaml:"cache_memory_mib,omitempty"`
	KVCacheDType          string  `yaml:"kv_cache_dtype"`
	PFlash                string  `yaml:"pflash"`
	ReasoningParser       string  `yaml:"reasoning_parser,omitempty"`
}

type MLXVLMTuning struct {
	MaxNumSeqs      int      `yaml:"max_num_seqs"`
	PrefillStepSize int      `yaml:"prefill_step_size"`
	VisionCacheSize int      `yaml:"vision_cache_size"`
	KVBits          *float64 `yaml:"kv_bits,omitempty"`
	KVQuantScheme   string   `yaml:"kv_quant_scheme,omitempty"`
	KVGroupSize     int      `yaml:"kv_group_size,omitempty"`
	MaxKVSize       *int     `yaml:"max_kv_size,omitempty"`
}

type VLLMMetalTuning struct {
	MaxNumSeqs           int     `yaml:"max_num_seqs"`
	MaxNumBatchedTokens  int     `yaml:"max_num_batched_tokens"`
	GPUMemoryUtilization float64 `yaml:"gpu_memory_utilization"`
	KVCacheDType         string  `yaml:"kv_cache_dtype"`
	PrefixCache          string  `yaml:"prefix_cache"`
}

type Tool struct {
	Source string   `yaml:"source"`
	Needs  []string `yaml:"needs"`
}

type Mode struct {
	Foreground string            `yaml:"foreground"`
	Services   map[string]string `yaml:"services,omitempty"`
	Tools      []string          `yaml:"tools"`
	Harnesses  []string          `yaml:"harnesses"`
	Members    Members           `yaml:"members"`
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
	switch d.Schema {
	case SchemaV1:
		return d.validateV1()
	case SchemaV2:
		return d.validateV2()
	default:
		return &ValidationError{Problems: []string{fmt.Sprintf("schema is %q, want %q or %q", d.Schema, SchemaV1, SchemaV2)}}
	}
}

func (d Document) validateV1() error {
	var problems []string
	problem := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
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
		if layout.Model.Format != "" || len(layout.Model.Files) != 0 {
			problem("layout %q manifest v1 model cannot declare format or files", id)
		}
		if layout.Engine != "llama-server" {
			problem("layout %q engine %q is not supported by manifest v1", id, layout.Engine)
		}
		if layout.Interface != "" || len(layout.Modalities) != 0 || layout.Speculation != nil || layout.RapidMLX != nil || layout.MLXVLM != nil || layout.VLLMMetal != nil {
			problem("layout %q declares successor-only engine fields in manifest v1", id)
		}
		if layout.Role != "coder" && layout.Role != "rerank" {
			problem("layout %q role %q must be coder or rerank", id, layout.Role)
		}
		if layout.Window <= 0 {
			problem("layout %q window must be greater than zero", id)
		}
		if layout.Llama == nil {
			problem("layout %q llama tuning is required", id)
			layout.Llama = &LlamaTuning{}
		}
		if layout.Llama.KV != "" {
			problem("layout %q manifest v1 llama.kv is not supported; use layout kv", id)
		}
		if layout.Llama.Parallel <= 0 || layout.Llama.Batch <= 0 || layout.Llama.UBatch <= 0 {
			problem("layout %q llama parallel, batch and ubatch must be greater than zero", id)
		}
		if layout.Llama.FlashAttention != "on" && layout.Llama.FlashAttention != "off" && layout.Llama.FlashAttention != "auto" {
			problem("layout %q llama.flash_attention %q must be on, off or auto", id, layout.Llama.FlashAttention)
		}
		if layout.Llama.ContextCheckpoints != nil && *layout.Llama.ContextCheckpoints < 0 {
			problem("layout %q llama.context_checkpoints must be zero or greater", id)
		}
		if layout.Llama.PromptCacheRAMMiB != nil && *layout.Llama.PromptCacheRAMMiB < 0 {
			problem("layout %q llama.prompt_cache_ram_mib must be zero or greater", id)
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
		if len(mode.Services) != 0 {
			problem("mode %q manifest v1 cannot declare services", id)
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

func (d Document) validateV2() error {
	var problems []string
	problem := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
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
		if !validID(id) || id == "none" {
			problem("layout id %q is not an allowed lowercase stable id", id)
		}
		if layout.DisplayName == "" {
			problem("layout %q display_name is required", id)
		}
		if !repoPattern.MatchString(layout.Model.Repo) {
			problem("layout %q model.repo %q must be owner/name", id, layout.Model.Repo)
		}
		if layout.Model.File != "" {
			problem("layout %q manifest v2 uses model.files, not model.file", id)
		}
		if layout.Model.Format != "gguf" && layout.Model.Format != "mlx-safetensors" && layout.Model.Format != "safetensors" {
			problem("layout %q model.format %q must be gguf, mlx-safetensors or safetensors", id, layout.Model.Format)
		}
		if len(layout.Model.Files) == 0 {
			problem("layout %q model.files must not be empty", id)
		}
		for index, file := range layout.Model.Files {
			if !safeRelativePath(file) {
				problem("layout %q model.files[%d] %q is not a safe relative path", id, index, file)
			}
		}
		if duplicate := firstDuplicate(layout.Model.Files); duplicate != "" {
			problem("layout %q model.files repeats %q", id, duplicate)
		}
		if !sort.StringsAreSorted(layout.Model.Files) {
			problem("layout %q model.files must be sorted", id)
		}
		if layout.Model.Format == "gguf" && len(layout.Model.Files) != 1 {
			problem("layout %q gguf model must select exactly one file", id)
		}
		if layout.Role != "" || layout.KV != "" {
			problem("layout %q manifest v2 cannot declare legacy role or layout kv", id)
		}
		if layout.Interface != "chat-completions" && layout.Interface != "reranking" {
			problem("layout %q interface %q must be chat-completions or reranking", id, layout.Interface)
		}
		if layout.Window <= 0 {
			problem("layout %q window must be greater than zero", id)
		}
		if !equalStringSlices(layout.Modalities, []string{"text"}) && !equalStringSlices(layout.Modalities, []string{"text", "image"}) {
			problem("layout %q modalities must be [text] or [text, image]", id)
		}

		variants := 0
		for _, selected := range []bool{layout.Llama != nil, layout.RapidMLX != nil, layout.MLXVLM != nil, layout.VLLMMetal != nil} {
			if selected {
				variants++
			}
		}
		if variants != 1 {
			problem("layout %q must declare exactly one engine tuning block", id)
		}
		matches := layout.Engine == "llama-server" && layout.Llama != nil ||
			layout.Engine == "rapid-mlx" && layout.RapidMLX != nil ||
			layout.Engine == "mlx-vlm" && layout.MLXVLM != nil ||
			layout.Engine == "vllm-metal" && layout.VLLMMetal != nil
		if !matches {
			problem("layout %q engine %q does not match its tuning block", id, layout.Engine)
		}

		switch layout.Interface {
		case "chat-completions":
			if layout.MaxTokens <= 0 || layout.MaxTokens >= layout.Window {
				problem("layout %q max_tokens must be greater than zero and below window", id)
			}
			if layout.Thinking != "on" && layout.Thinking != "off" {
				problem("layout %q thinking %q must be on or off", id, layout.Thinking)
			}
		case "reranking":
			if layout.MaxTokens != 0 || layout.Thinking != "" || layout.ChatTemplate != "" {
				problem("layout %q reranking interface cannot declare chat-completion tuning", id)
			}
			if !equalStringSlices(layout.Modalities, []string{"text"}) {
				problem("layout %q reranking interface requires text-only modality", id)
			}
		}
		if layout.Speculation == nil {
			problem("layout %q speculation contract is required", id)
		} else {
			validateManifestSpeculation(id, "speculation", layout.Speculation.Method, layout.Speculation.MaxTokens, layout.Interface, problem)
		}

		if layout.ChatTemplate != "" {
			if layout.Engine != "llama-server" {
				problem("layout %q engine %q does not support Temper chat-template patches", id, layout.Engine)
			}
			if _, ok := d.Patches[layout.ChatTemplate]; !ok {
				problem("layout %q chat_template references unknown patch %q", id, layout.ChatTemplate)
			}
		}

		validateV2EngineTuning(id, layout, problem)
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
		for _, service := range tool.Needs {
			if service != "rerank" {
				problem("tool %q needs unsupported service %q", id, service)
			}
		}
		if duplicate := firstDuplicate(tool.Needs); duplicate != "" {
			problem("tool %q repeats service %q", id, duplicate)
		}
	}

	for _, id := range sortedKeys(d.Modes) {
		validateV2Mode(d, id, d.Modes[id], problem)
	}

	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

func validateV2EngineTuning(id string, layout Layout, problem func(string, ...any)) {
	switch layout.Engine {
	case "llama-server":
		if layout.Llama == nil {
			return
		}
		tuning := layout.Llama
		if layout.Model.Format != "gguf" {
			problem("layout %q llama-server requires model.format gguf", id)
		}
		if !equalStringSlices(layout.Modalities, []string{"text"}) {
			problem("layout %q llama-server requires text-only modality", id)
		}
		if tuning.Parallel <= 0 || tuning.Batch <= 0 || tuning.UBatch <= 0 {
			problem("layout %q llama parallel, batch and ubatch must be greater than zero", id)
		}
		if tuning.UBatch > tuning.Batch {
			problem("layout %q llama.ubatch must not exceed llama.batch", id)
		}
		if tuning.FlashAttention != "on" && tuning.FlashAttention != "off" && tuning.FlashAttention != "auto" {
			problem("layout %q llama.flash_attention %q must be on, off or auto", id, tuning.FlashAttention)
		}
		if tuning.ContextCheckpoints != nil && *tuning.ContextCheckpoints < 0 {
			problem("layout %q llama.context_checkpoints must be zero or greater", id)
		}
		if tuning.PromptCacheRAMMiB != nil && *tuning.PromptCacheRAMMiB < 0 {
			problem("layout %q llama.prompt_cache_ram_mib must be zero or greater", id)
		}
		if layout.Interface == "chat-completions" && tuning.KV != "q8" && tuning.KV != "f16" {
			problem("layout %q llama.kv %q must be q8 or f16 for chat completions", id, tuning.KV)
		}
		if layout.Interface == "reranking" && tuning.KV != "" {
			problem("layout %q reranking interface cannot declare llama.kv", id)
		}
		if tuning.SpecType != "" || tuning.SpecDraftNMax != 0 {
			problem("layout %q manifest v2 uses layout speculation, not llama speculation fields", id)
		}
	case "rapid-mlx":
		if layout.RapidMLX == nil {
			return
		}
		tuning := layout.RapidMLX
		if layout.Model.Format != "mlx-safetensors" || layout.Interface != "chat-completions" {
			problem("layout %q rapid-mlx requires mlx-safetensors chat completions", id)
		}
		if tuning.MaxNumSeqs <= 0 || tuning.MaxConcurrentRequests <= 0 || tuning.PrefillBatchSize <= 0 || tuning.CompletionBatchSize <= 0 {
			problem("layout %q rapid_mlx batching values must be greater than zero", id)
		}
		if tuning.MaxConcurrentRequests < tuning.MaxNumSeqs {
			problem("layout %q rapid_mlx.max_concurrent_requests must not be below max_num_seqs", id)
		}
		if tuning.GPUMemoryUtilization <= 0 || tuning.GPUMemoryUtilization > 1 {
			problem("layout %q rapid_mlx.gpu_memory_utilization must be greater than zero and at most one", id)
		}
		if tuning.PrefixCache != "on" && tuning.PrefixCache != "off" {
			problem("layout %q rapid_mlx.prefix_cache %q must be on or off", id, tuning.PrefixCache)
		}
		if tuning.CacheMemoryMiB != nil && *tuning.CacheMemoryMiB < 0 {
			problem("layout %q rapid_mlx.cache_memory_mib must be zero or greater", id)
		}
		if tuning.KVCacheDType != "bf16" && tuning.KVCacheDType != "int8" && tuning.KVCacheDType != "int4" {
			problem("layout %q rapid_mlx.kv_cache_dtype %q must be bf16, int8 or int4", id, tuning.KVCacheDType)
		}
		if tuning.PFlash != "off" && tuning.PFlash != "auto" && tuning.PFlash != "always" {
			problem("layout %q rapid_mlx.pflash %q must be off, auto or always", id, tuning.PFlash)
		}
		if equalStringSlices(layout.Modalities, []string{"text", "image"}) && tuning.PFlash != "off" {
			problem("layout %q rapid-mlx multimodal launch requires pflash off", id)
		}
		if layout.Thinking == "on" && !optionPattern.MatchString(tuning.ReasoningParser) {
			problem("layout %q rapid_mlx.reasoning_parser is required for thinking on", id)
		}
		if layout.Thinking != "on" && tuning.ReasoningParser != "" {
			problem("layout %q rapid_mlx.reasoning_parser requires thinking on", id)
		}
	case "mlx-vlm":
		if layout.MLXVLM == nil {
			return
		}
		tuning := layout.MLXVLM
		if layout.Model.Format != "mlx-safetensors" || layout.Interface != "chat-completions" {
			problem("layout %q mlx-vlm requires mlx-safetensors chat completions", id)
		}
		if tuning.MaxNumSeqs <= 0 || tuning.PrefillStepSize <= 0 || tuning.VisionCacheSize < 0 {
			problem("layout %q mlx_vlm sequence/prefill values must be positive and vision cache nonnegative", id)
		}
		if tuning.KVBits == nil {
			if tuning.KVQuantScheme != "" || tuning.KVGroupSize != 0 {
				problem("layout %q mlx_vlm KV quantization settings require kv_bits", id)
			}
		} else {
			bits := *tuning.KVBits
			if bits != 3.5 && bits != 4 && bits != 8 {
				problem("layout %q mlx_vlm.kv_bits must be 3.5, 4 or 8", id)
			}
			if tuning.KVQuantScheme != "uniform" && tuning.KVQuantScheme != "turboquant" {
				problem("layout %q mlx_vlm.kv_quant_scheme %q must be uniform or turboquant", id, tuning.KVQuantScheme)
			}
			if tuning.KVGroupSize <= 0 {
				problem("layout %q mlx_vlm.kv_group_size must be greater than zero", id)
			}
			if bits == 3.5 && tuning.KVQuantScheme != "turboquant" {
				problem("layout %q mlx_vlm 3.5-bit KV requires turboquant", id)
			}
		}
		if tuning.MaxKVSize != nil && *tuning.MaxKVSize <= 0 {
			problem("layout %q mlx_vlm.max_kv_size must be greater than zero when present", id)
		}
		if layout.Speculation != nil && layout.Speculation.Method != "none" {
			problem("layout %q mlx-vlm does not support the selected speculation contract", id)
		}
	case "vllm-metal":
		if layout.VLLMMetal == nil {
			return
		}
		tuning := layout.VLLMMetal
		if layout.Model.Format != "safetensors" || layout.Interface != "chat-completions" || !equalStringSlices(layout.Modalities, []string{"text"}) {
			problem("layout %q vllm-metal requires text-only safetensors chat completions", id)
		}
		if tuning.MaxNumSeqs <= 0 || tuning.MaxNumBatchedTokens <= 0 {
			problem("layout %q vllm_metal batching values must be greater than zero", id)
		}
		if tuning.GPUMemoryUtilization <= 0 || tuning.GPUMemoryUtilization > 1 {
			problem("layout %q vllm_metal.gpu_memory_utilization must be greater than zero and at most one", id)
		}
		if tuning.KVCacheDType != "auto" && tuning.KVCacheDType != "bfloat16" && tuning.KVCacheDType != "float16" {
			problem("layout %q vllm_metal.kv_cache_dtype %q must be auto, bfloat16 or float16", id, tuning.KVCacheDType)
		}
		if tuning.PrefixCache != "on" && tuning.PrefixCache != "off" {
			problem("layout %q vllm_metal.prefix_cache %q must be on or off", id, tuning.PrefixCache)
		}
		if layout.Speculation != nil && layout.Speculation.Method == "mtp" && tuning.MaxNumSeqs != 1 {
			problem("layout %q vllm-metal MTP requires max_num_seqs 1", id)
		}
		if layout.Speculation != nil && layout.Speculation.Method == "mtp" && tuning.PrefixCache == "on" {
			problem("layout %q vllm-metal MTP with prefix caching is refused until upstream output-corruption reports are cleared by qualification", id)
		}
	default:
		problem("layout %q engine %q is not supported by manifest v2", id, layout.Engine)
	}
}

func validateManifestSpeculation(id, field, method string, tokens int, interfaceName string, problem func(string, ...any)) {
	switch method {
	case "none":
		if tokens != 0 {
			problem("layout %q %s max_tokens requires mtp", id, field)
		}
	case "mtp":
		if interfaceName != "chat-completions" || tokens <= 0 || tokens > 16 {
			problem("layout %q %s MTP tokens must be between 1 and 16 for chat completions", id, field)
		}
	default:
		problem("layout %q %s method %q must be none or mtp", id, field, method)
	}
}

func validateV2Mode(d Document, id string, mode Mode, problem func(string, ...any)) {
	if !validID(id) {
		problem("mode id %q is not a lowercase stable id", id)
	}
	if duplicate := firstDuplicate(mode.Tools); duplicate != "" {
		problem("mode %q repeats tool %q", id, duplicate)
	}
	if duplicate := firstDuplicate(mode.Harnesses); duplicate != "" {
		problem("mode %q repeats harness %q", id, duplicate)
	}
	for _, harness := range mode.Harnesses {
		if harness != "pi" {
			problem("mode %q harness %q is not supported by manifest v2", id, harness)
		}
	}

	seenLayouts := map[string]bool{}
	resident := map[string]bool{}
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
			if placement == "resident" {
				resident[member.Layout] = true
			}
			if member.TTL != nil && *member.TTL < 0 {
				problem("%s ttl must be zero or greater", location)
			}
			if member.NGL != nil && (*member.NGL < 0 || layout.Engine != "llama-server") {
				problem("%s ngl is a nonnegative llama-server-only placement", location)
			}
			if member.Preferred {
				problem("%s preferred is removed in manifest v2; bind mode.foreground", location)
			}
			if member.Preload && placement != "resident" {
				problem("%s preload is allowed only on a resident member", location)
			}
		}
	}
	validateMembers("resident", mode.Members.Resident)
	validateMembers("on_demand", mode.Members.OnDemand)

	for _, service := range sortedKeys(mode.Services) {
		layoutID := mode.Services[service]
		if service != "rerank" {
			problem("mode %q service %q is unsupported", id, service)
		}
		layout, ok := d.Layouts[layoutID]
		if !ok || !seenLayouts[layoutID] {
			problem("mode %q service %q references non-member layout %q", id, service, layoutID)
		} else if service == "rerank" && layout.Interface != "reranking" {
			problem("mode %q service %q layout %q does not implement reranking", id, service, layoutID)
		}
	}
	for _, toolID := range mode.Tools {
		tool, ok := d.Tools[toolID]
		if !ok {
			problem("mode %q references unknown tool %q", id, toolID)
			continue
		}
		for _, service := range tool.Needs {
			if _, ok := mode.Services[service]; !ok {
				problem("mode %q tool %q needs missing service %q", id, toolID, service)
			}
		}
	}

	if mode.Foreground == "none" {
		if len(seenLayouts) != 0 || len(mode.Tools) != 0 || len(mode.Harnesses) != 0 || len(mode.Services) != 0 {
			problem("mode %q with foreground none must be empty", id)
		}
		return
	}
	foreground, ok := d.Layouts[mode.Foreground]
	if !ok {
		problem("mode %q foreground references unknown layout %q", id, mode.Foreground)
		return
	}
	if !resident[mode.Foreground] {
		problem("mode %q foreground layout %q must be resident", id, mode.Foreground)
	}
	if foreground.Interface != "chat-completions" {
		problem("mode %q foreground layout %q must implement chat completions", id, mode.Foreground)
	}
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// TechnicalInterface normalizes the retained v1 role into the successor's
// engine-neutral interface vocabulary.
func (l Layout) TechnicalInterface() string {
	if l.Interface != "" {
		return l.Interface
	}
	if l.Role == "coder" {
		return "chat-completions"
	}
	if l.Role == "rerank" {
		return "reranking"
	}
	return ""
}

func (l Layout) InputModalities() []string {
	if len(l.Modalities) != 0 {
		return append([]string(nil), l.Modalities...)
	}
	return []string{"text"}
}

func (l Layout) ModelFiles() []string {
	if len(l.Model.Files) != 0 {
		return append([]string(nil), l.Model.Files...)
	}
	if l.Model.File == "" {
		return nil
	}
	return []string{l.Model.File}
}

func (l Layout) ModelFormat() string {
	if l.Model.Format != "" {
		return l.Model.Format
	}
	return "gguf"
}

func (l Layout) KVCache() string {
	if l.Llama != nil && l.Llama.KV != "" {
		return l.Llama.KV
	}
	return l.KV
}

func (l Layout) SpeculationContract() (string, int) {
	if l.Speculation != nil {
		return l.Speculation.Method, l.Speculation.MaxTokens
	}
	if l.Llama != nil && l.Llama.SpecType == "draft-mtp" {
		return "mtp", l.Llama.SpecDraftNMax
	}
	return "none", 0
}

// ForegroundLayout returns the exact foreground member selected by a mode.
func (d Document) ForegroundLayout(mode Mode) string {
	if d.Schema == SchemaV2 {
		if mode.Foreground == "none" {
			return ""
		}
		return mode.Foreground
	}
	for _, member := range mode.Members.Resident {
		if member.Preferred {
			return member.Layout
		}
	}
	return ""
}

// QualificationRole preserves the existing update gate vocabulary while the
// qualification schema is revised independently.
func (l Layout) QualificationRole() string {
	if l.TechnicalInterface() == "chat-completions" {
		return "coder"
	}
	return "rerank"
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
