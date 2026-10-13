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
	"github.com/temper-sh/temper/internal/render/engine"
	"gopkg.in/yaml.v3"
)

const SchemaV1 = "temper-manifest/v1"

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
	CrispEmbed   *engine.CrispEmbedTuning `yaml:"crispembed,omitempty"`
	Splash       *SplashTuning            `yaml:"splash,omitempty"`
	Draft        *Model                   `yaml:"draft,omitempty"`
	Projector    *Model                   `yaml:"projector,omitempty"`
	DisplayName  string                   `yaml:"display_name"`
	Model        Model                    `yaml:"model"`
	Engine       string                   `yaml:"engine"`
	Interface    string                   `yaml:"interface,omitempty"`
	Modalities   []string                 `yaml:"modalities,omitempty"`
	Window       int                      `yaml:"window"`
	MaxTokens    int                      `yaml:"max_tokens,omitempty"`
	Thinking     string                   `yaml:"thinking,omitempty"`
	Speculation  *Speculation             `yaml:"speculation,omitempty"`
	ChatTemplate string                   `yaml:"chat_template,omitempty"`
	Llama        *LlamaTuning             `yaml:"llama,omitempty"`
	RapidMLX     *RapidMLXTuning          `yaml:"rapid_mlx,omitempty"`
	VLLMMetal    *VLLMMetalTuning         `yaml:"vllm_metal,omitempty"`
	Sampling     *engine.SamplingDefaults `yaml:"sampling,omitempty"`
}

type SplashTuning struct {
	engine.SplashConfig `yaml:",inline"`
	SoftwareSHA256      string `yaml:"software_sha256"`
}

type Model struct {
	Repo   string   `yaml:"repo"`
	Format string   `yaml:"format,omitempty"`
	Files  []string `yaml:"files,omitempty"`
}

type Speculation struct {
	Method    string `yaml:"method"`
	MaxTokens int    `yaml:"max_tokens,omitempty"`
}

type LlamaTuning struct {
	KV                 string                      `yaml:"kv,omitempty"`
	Parallel           int                         `yaml:"parallel"`
	FlashAttention     string                      `yaml:"flash_attention"`
	Batch              int                         `yaml:"batch"`
	UBatch             int                         `yaml:"ubatch"`
	ContextCheckpoints *int                        `yaml:"context_checkpoints,omitempty"`
	PromptCacheRAMMiB  *int                        `yaml:"prompt_cache_ram_mib,omitempty"`
	Controls           *engine.LlamaServerControls `yaml:"controls,omitempty"`
}

type RapidMLXTuning struct {
	ToolCallParser        string  `yaml:"tool_call_parser,omitempty"`
	PrefillStepSize       int     `yaml:"prefill_step_size,omitempty"`
	RequestTimeoutSeconds int     `yaml:"request_timeout_seconds,omitempty"`
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

type VLLMMetalTuning struct {
	LanguageModelOnly    bool    `yaml:"language_model_only,omitempty"`
	ChunkedPrefill       bool    `yaml:"chunked_prefill,omitempty"`
	BlockSize            int     `yaml:"block_size,omitempty"`
	ToolCallParser       string  `yaml:"tool_call_parser,omitempty"`
	ReasoningParser      string  `yaml:"reasoning_parser,omitempty"`
	MaxNumSeqs           int     `yaml:"max_num_seqs"`
	MaxNumBatchedTokens  int     `yaml:"max_num_batched_tokens"`
	GPUMemoryUtilization float64 `yaml:"gpu_memory_utilization"`
	KVCacheDType         string  `yaml:"kv_cache_dtype"`
	PrefixCache          string  `yaml:"prefix_cache"`
}

type Mode struct {
	Foreground         string  `yaml:"foreground"`
	ExternalForeground bool    `yaml:"external_foreground,omitempty"`
	Members            Members `yaml:"members"`
}

type Members struct {
	Resident []Member `yaml:"resident"`
	OnDemand []Member `yaml:"on_demand"`
}

type Member struct {
	Layout  string `yaml:"layout"`
	TTL     *int   `yaml:"ttl"`
	NGL     *int   `yaml:"ngl"`
	Preload bool   `yaml:"preload"`
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
	if d.Schema != SchemaV1 {
		return &ValidationError{Problems: []string{fmt.Sprintf("schema is %q, want %q", d.Schema, SchemaV1)}}
	}
	var problems []string
	for _, id := range sortedKeys(d.Layouts) {
		layout := d.Layouts[id]
		if layout.Sampling != nil {
			if layout.Engine != engine.LlamaServer && layout.Engine != engine.Splash {
				problems = append(problems, fmt.Sprintf("layout %q explicit sampling requires llama-server or Splash", id))
			} else if err := layout.Sampling.Validate(); err != nil {
				problems = append(problems, fmt.Sprintf("layout %q: %v", id, err))
			}
		}
		if layout.Llama != nil && layout.Llama.Controls != nil {
			if err := layout.Llama.Controls.Validate(); err != nil {
				problems = append(problems, fmt.Sprintf("layout %q: %v", id, err))
			}
		}
	}
	if len(problems) != 0 {
		return &ValidationError{Problems: problems}
	}
	return d.validateFields()
}

func (d Document) validateFields() error {
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
		if layout.Interface != "chat-completions" && layout.Interface != "reranking" {
			problem("layout %q interface %q must be chat-completions or reranking", id, layout.Interface)
		}
		if layout.Window <= 0 {
			problem("layout %q window must be greater than zero", id)
		}
		if !equalStringSlices(layout.Modalities, []string{"text"}) && !equalStringSlices(layout.Modalities, []string{"text", "image"}) {
			problem("layout %q modalities must be [text] or [text, image]", id)
		}
		if p := layout.Projector; p != nil {
			if layout.Engine != "llama-server" || layout.Interface != "chat-completions" || !equalStringSlices(layout.Modalities, []string{"text", "image"}) || !repoPattern.MatchString(p.Repo) || p.Format != "gguf" || len(p.Files) != 1 || !safeRelativePath(p.Files[0]) || !strings.HasSuffix(p.Files[0], ".gguf") {
				problem("layout %q projector requires one exact GGUF and llama-server image input", id)
			}
		} else if layout.Engine == "llama-server" && equalStringSlices(layout.Modalities, []string{"text", "image"}) {
			problem("layout %q llama-server image input requires a projector", id)
		}

		variants := 0
		for _, selected := range []bool{layout.Llama != nil, layout.RapidMLX != nil, layout.VLLMMetal != nil, layout.Splash != nil, layout.CrispEmbed != nil} {
			if selected {
				variants++
			}
		}
		if variants != 1 {
			problem("layout %q must declare exactly one engine tuning block", id)
		}
		matches := layout.Engine == "llama-server" && layout.Llama != nil ||
			layout.Engine == "rapid-mlx" && layout.RapidMLX != nil ||
			layout.Engine == "vllm-metal" && layout.VLLMMetal != nil ||
			layout.Engine == "splash" && layout.Splash != nil ||
			layout.Engine == "crispembed" && layout.CrispEmbed != nil
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
			if layout.MaxTokens != 0 || layout.Thinking != "" || layout.ChatTemplate != "" || layout.Sampling != nil {
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
			if layout.Engine != "llama-server" && layout.Engine != "splash" {
				problem("layout %q engine %q does not support Temper chat-template patches", id, layout.Engine)
			}
			if _, ok := d.Patches[layout.ChatTemplate]; !ok {
				problem("layout %q chat_template references unknown patch %q", id, layout.ChatTemplate)
			}
		}

		if layout.Engine != "splash" && layout.Engine != "llama-server" && (layout.Draft != nil || layout.Speculation != nil && (layout.Speculation.Method == "dflash" || layout.Speculation.Method == "dflash2")) {
			problem("layout %q: external draft requires llama-server or Splash", id)
		}
		validateEngineTuning(id, layout, problem)
	}

	for _, id := range sortedKeys(d.Modes) {
		validateMode(d, id, d.Modes[id], problem)
	}

	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

func validateEngineTuning(id string, layout Layout, problem func(string, ...any)) {
	switch layout.Engine {
	case "crispembed":
		if layout.CrispEmbed == nil {
			return
		}
		if layout.Model.Format != "gguf" || layout.Interface != "reranking" || !equalStringSlices(layout.Modalities, []string{"text"}) {
			problem("layout %q CrispEmbed requires a GGUF text reranker", id)
		}
		if err := layout.CrispEmbed.Validate(); err != nil {
			problem("layout %q: %v", id, err)
		}
	case "splash":
		if layout.Splash == nil {
			return
		}
		if err := layout.Splash.SplashConfig.Validate(); err != nil {
			problem("layout %q: %v", id, err)
		}
		if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(layout.Splash.SoftwareSHA256) {
			problem("layout %q Splash requires exact software_sha256", id)
		}
		if layout.Model.Format != "gguf" || layout.Interface != "chat-completions" || !equalStringSlices(layout.Modalities, []string{"text"}) {
			problem("layout %q Splash requires text GGUF chat completions", id)
		}
		if layout.Draft == nil || !repoPattern.MatchString(layout.Draft.Repo) || layout.Draft.Format != "safetensors" || !equalStringSlices(layout.Draft.Files, []string{"config.json", "model.safetensors"}) {
			problem("layout %q Splash requires a complete DFlash2 draft", id)
		}
		if layout.Speculation == nil || layout.Speculation.Method != "dflash2" || layout.Speculation.MaxTokens != 0 {
			problem("layout %q Splash requires engine-owned DFlash2 speculation", id)
		}
	case "llama-server":
		if layout.Llama == nil {
			return
		}
		tuning := layout.Llama
		if layout.Draft != nil {
			draft := layout.Draft
			if !repoPattern.MatchString(draft.Repo) || draft.Format != "gguf" || len(draft.Files) != 1 || !safeRelativePath(draft.Files[0]) || !strings.HasSuffix(draft.Files[0], ".gguf") {
				problem("layout %q llama-server requires one complete GGUF draft", id)
			}
			if layout.Speculation == nil || (layout.Speculation.Method != "mtp" && layout.Speculation.Method != "dflash" && layout.Speculation.Method != "dflash2") {
				problem("layout %q GGUF draft requires MTP or DFlash speculation", id)
			}
		}
		if layout.Speculation != nil && (layout.Speculation.Method == "dflash" || layout.Speculation.Method == "dflash2") {
			if layout.Draft == nil || layout.Speculation.MaxTokens < 1 || layout.Speculation.MaxTokens > 15 {
				problem("layout %q llama-server DFlash requires a draft and 1–15 proposed tokens", id)
			}
		}
		if layout.Model.Format != "gguf" {
			problem("layout %q llama-server requires model.format gguf", id)
		}
		if !equalStringSlices(layout.Modalities, []string{"text"}) && !(layout.Projector != nil && equalStringSlices(layout.Modalities, []string{"text", "image"})) {
			problem("layout %q llama-server image input requires its projector", id)
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
		if layout.Interface == "chat-completions" && tuning.KV != "q4" && tuning.KV != "q8" && tuning.KV != "f16" {
			problem("layout %q llama.kv %q must be q4, q8 or f16 for chat completions", id, tuning.KV)
		}
		if layout.Interface == "reranking" && tuning.KV != "" {
			problem("layout %q reranking interface cannot declare llama.kv", id)
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
		problem("layout %q engine %q is not supported by manifest v1", id, layout.Engine)
	}
}

func validateManifestSpeculation(id, field, method string, tokens int, interfaceName string, problem func(string, ...any)) {
	switch method {
	case "none":
		if tokens != 0 {
			problem("layout %q %s none method requires zero max_tokens", id, field)
		}
	case "dflash", "dflash2":
		if interfaceName != "chat-completions" || tokens < 0 || tokens > 15 {
			problem("layout %q %s DFlash tokens must be 0–15 for chat completions; the engine validates zero", id, field)
		}
	case "mtp":
		if interfaceName != "chat-completions" || tokens <= 0 || tokens > 16 {
			problem("layout %q %s MTP tokens must be between 1 and 16 for chat completions", id, field)
		}
	default:
		problem("layout %q %s method %q must be none, mtp, dflash or dflash2", id, field, method)
	}
}

func validateMode(d Document, id string, mode Mode, problem func(string, ...any)) {
	if !validID(id) {
		problem("mode id %q is not a lowercase stable id", id)
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
			if member.Preload && placement != "resident" {
				problem("%s preload is allowed only on a resident member", location)
			}
		}
	}
	validateMembers("resident", mode.Members.Resident)
	validateMembers("on_demand", mode.Members.OnDemand)

	if mode.ExternalForeground {
		if mode.Foreground != "" {
			problem("mode %q external foreground cannot name a local foreground layout", id)
		}
		if len(seenLayouts) == 0 {
			problem("mode %q with external foreground needs a helper member", id)
		}
		return
	}
	if mode.Foreground == "none" {
		if len(seenLayouts) != 0 {
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

// TechnicalInterface returns the declared engine API.
func (l Layout) TechnicalInterface() string { return l.Interface }
func (l Layout) InputModalities() []string  { return append([]string(nil), l.Modalities...) }
func (l Layout) ModelFiles() []string       { return append([]string(nil), l.Model.Files...) }
func (l Layout) ModelFormat() string        { return l.Model.Format }
func (l Layout) KVCache() string {
	if l.Splash != nil {
		return l.Splash.KVCache
	}
	if l.Llama != nil {
		return l.Llama.KV
	}
	return ""
}
func (l Layout) SpeculationContract() (string, int) {
	if l.Speculation != nil {
		return l.Speculation.Method, l.Speculation.MaxTokens
	}
	return "none", 0
}

// ForegroundLayout returns the exact foreground member selected by a mode.
func (d Document) ForegroundLayout(mode Mode) string {
	if mode.Foreground == "none" || mode.ExternalForeground {
		return ""
	}
	return mode.Foreground
}

// QualificationRole maps the engine interface to the update gate's task names.
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
