// Package catalog compiles explicit local catalog choices into portable,
// self-contained execution locks. Compilation is pure; software discovery is
// a separate read through ResolveSoftware.
package catalog

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/temper-sh/temper/internal/render/engine"
	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter/upstreamrelease"
	"gopkg.in/yaml.v3"
)

const Schema = "temper-catalog/v3"
const LockSchema = "temper-execution-lock/v3"

var idPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)
var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Document struct {
	Schema      string              `yaml:"schema" json:"schema"`
	Date        string              `yaml:"date" json:"date"`
	Runtime     Runtime             `yaml:"runtime" json:"runtime"`
	Artifacts   map[string]Artifact `yaml:"artifacts" json:"artifacts"`
	Patches     map[string]Patch    `yaml:"patches" json:"patches"`
	Engines     map[string]Engine   `yaml:"engines" json:"engines"`
	Presets     map[string]Preset   `yaml:"presets" json:"presets"`
	PresetOrder []string            `yaml:"preset_order,omitempty" json:"preset_order,omitempty"`
}

type Runtime struct {
	Router             Supply   `yaml:"router" json:"router"`
	PythonEnvironments []Supply `yaml:"python_environments,omitempty" json:"python_environments,omitempty"`
}

type File struct {
	Path   string `yaml:"path" json:"path"`
	Bytes  int64  `yaml:"bytes" json:"bytes"`
	SHA256 string `yaml:"sha256" json:"sha256"`
}

type Artifact struct {
	ModelName   string `yaml:"model_name,omitempty" json:"model_name,omitempty"`
	WeightsName string `yaml:"weights_name,omitempty" json:"weights_name,omitempty"`
	Repo        string `yaml:"repo" json:"repo"`
	Revision    string `yaml:"revision" json:"revision"`
	Format      string `yaml:"format" json:"format"`
	Files       []File `yaml:"files" json:"files"`
	License     string `yaml:"license" json:"license"`
}

type Patch struct {
	Repo                string   `yaml:"repo" json:"repo"`
	Revision            string   `yaml:"revision" json:"revision"`
	Files               []File   `yaml:"files" json:"files"`
	CompatibleArtifacts []string `yaml:"compatible_artifacts" json:"compatible_artifacts"`
	License             string   `yaml:"license" json:"license"`
}

// Supply separates an upstream source from its resolved release. Complete
// archives include the executable and its libraries. Installer rows are derived.
type Supply struct {
	Python   *PythonSupply                 `yaml:"python,omitempty" json:"python,omitempty"`
	Package  string                        `yaml:"package" json:"package"`
	Target   software.Target               `yaml:"target" json:"target"`
	Source   *upstreamrelease.GitHubSource `yaml:"source,omitempty" json:"source,omitempty"`
	Release  *upstreamrelease.Release      `yaml:"release,omitempty" json:"release,omitempty"`
	Versions *Versions                     `yaml:"versions,omitempty" json:"versions,omitempty"`
}

type Engine struct {
	DisplayName string   `yaml:"display_name,omitempty" json:"display_name,omitempty"`
	Family      string   `yaml:"family" json:"family"`
	Adapter     string   `yaml:"adapter" json:"adapter"`
	Supply      Supply   `yaml:"supply" json:"supply"`
	Interfaces  []string `yaml:"interfaces" json:"interfaces"`
	Modalities  []string `yaml:"modalities" json:"modalities"`
}

type RequestDefaults struct {
	MaxOutputTokens int                     `yaml:"max_output_tokens" json:"max_output_tokens"`
	Reasoning       string                  `yaml:"reasoning" json:"reasoning"`
	Sampling        engine.SamplingDefaults `yaml:"sampling" json:"sampling"`
}

type Speculation struct {
	DraftArtifact  string `yaml:"draft_artifact,omitempty" json:"draft_artifact,omitempty"`
	Method         string `yaml:"method" json:"method"`
	Source         string `yaml:"source" json:"source"`
	MaxDraftTokens int    `yaml:"max_draft_tokens" json:"max_draft_tokens"`
}

type LlamaConfig struct {
	Kind               string                     `yaml:"kind" json:"kind"`
	Parallel           int                        `yaml:"parallel" json:"parallel"`
	KVCache            string                     `yaml:"kv_cache" json:"kv_cache"`
	FlashAttention     string                     `yaml:"flash_attention" json:"flash_attention"`
	BatchTokens        int                        `yaml:"batch_tokens" json:"batch_tokens"`
	MicrobatchTokens   int                        `yaml:"microbatch_tokens" json:"microbatch_tokens"`
	ContextCheckpoints int                        `yaml:"context_checkpoints" json:"context_checkpoints"`
	PromptCacheRAMMiB  int                        `yaml:"prompt_cache_ram_mib" json:"prompt_cache_ram_mib"`
	GPULayers          int                        `yaml:"gpu_layers" json:"gpu_layers"`
	Controls           engine.LlamaServerControls `yaml:"controls" json:"controls"`
}

type Preset struct {
	Description         string           `yaml:"description,omitempty" json:"description,omitempty"`
	AssessmentURL       string           `yaml:"assessment_url,omitempty" json:"assessment_url,omitempty"`
	Recommended         bool             `yaml:"recommended,omitempty" json:"recommended,omitempty"`
	DisplayName         string           `yaml:"display_name" json:"display_name"`
	MemoryTier          string           `yaml:"memory_tier,omitempty" json:"memory_tier,omitempty"`
	Artifact            string           `yaml:"artifact" json:"artifact"`
	Patches             []string         `yaml:"patches" json:"patches"`
	Engine              string           `yaml:"engine" json:"engine"`
	Interface           string           `yaml:"interface" json:"interface"`
	Modalities          []string         `yaml:"modalities" json:"modalities"`
	ContextWindowTokens int              `yaml:"context_window_tokens" json:"context_window_tokens"`
	ContextLimitTokens  int              `yaml:"context_limit_tokens,omitempty" json:"context_limit_tokens,omitempty"`
	ContextFindings     []ContextFinding `yaml:"context_findings,omitempty" json:"context_findings,omitempty"`
	RequestDefaults     RequestDefaults  `yaml:"request_defaults" json:"request_defaults"`
	Speculation         Speculation      `yaml:"speculation" json:"speculation"`
	EngineConfig        EngineConfig     `yaml:"engine_config" json:"engine_config"`
	EngineVersions      *Versions        `yaml:"engine_versions,omitempty" json:"engine_versions,omitempty"`
}

func decode(data []byte, into any) error {
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(into); err != nil {
		return fmt.Errorf("decode catalog contract: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("expected exactly one catalog contract document")
	}
	return nil
}

func Parse(data []byte) (Document, error) {
	var d Document
	if err := decode(data, &d); err != nil {
		return Document{}, err
	}
	if err := d.Validate(); err != nil {
		return Document{}, err
	}
	return d, nil
}

func (d Document) Validate() error {
	if d.Schema != Schema {
		return fmt.Errorf("catalog schema must be %s", Schema)
	}
	if _, err := time.Parse("2006-01-02", d.Date); err != nil {
		return errors.New("catalog date must be YYYY-MM-DD")
	}
	if len(d.Artifacts) == 0 || len(d.Engines) == 0 || len(d.Presets) == 0 {
		return errors.New("catalog requires artifacts, engines and presets")
	}
	if err := d.validatePresentation(); err != nil {
		return err
	}
	if d.Runtime.Router.Package != "llama-swap" {
		return errors.New("runtime router must supply llama-swap")
	}
	if err := d.Runtime.Router.validate(); err != nil {
		return fmt.Errorf("router: %w", err)
	}
	seenPython := map[string]bool{}
	for _, supply := range d.Runtime.PythonEnvironments {
		if supply.Python == nil || seenPython[supply.Package] {
			return errors.New("runtime Python environments require distinct exact supplies")
		}
		if err := supply.validate(); err != nil {
			return fmt.Errorf("runtime Python environment: %w", err)
		}
		seenPython[supply.Package] = true
	}
	for id, a := range d.Artifacts {
		if err := validateMaterial(id, a.Repo, a.Revision, a.Files, a.License); err != nil {
			return fmt.Errorf("artifact: %w", err)
		}
		if a.Format == "safetensors" && len(a.Files) == 2 && a.Files[0].Path == "config.json" && a.Files[1].Path == "model.safetensors" {
			continue
		}
		if a.Format == "mlx-safetensors" || a.Format == "safetensors" {
			config, weights, tokenizer := false, false, false
			for _, f := range a.Files {
				config = config || f.Path == "config.json"
				weights = weights || strings.HasSuffix(f.Path, ".safetensors")
				tokenizer = tokenizer || f.Path == "tokenizer.json"
			}
			if config && weights && tokenizer {
				continue
			}
			return fmt.Errorf("artifact %q requires config, tokenizer and safetensors weights", id)
		}
		if a.Format != "gguf" || len(a.Files) != 1 || !strings.HasSuffix(a.Files[0].Path, ".gguf") {
			return fmt.Errorf("artifact %q: this executable slice requires one complete GGUF", id)
		}
	}
	for id, p := range d.Patches {
		if err := validateMaterial(id, p.Repo, p.Revision, p.Files, p.License); err != nil {
			return fmt.Errorf("patch: %w", err)
		}
		if len(p.Files) != 1 || len(p.CompatibleArtifacts) == 0 {
			return fmt.Errorf("patch %q requires one template and explicit compatible artifacts", id)
		}
		seen := map[string]bool{}
		for _, a := range p.CompatibleArtifacts {
			// Compatibility identifiers may name artifacts outside a selected
			// lock closure. A selected preset must still resolve its own artifact.
			if !idPattern.MatchString(a) || seen[a] {
				return fmt.Errorf("patch %q has invalid or repeated artifact %q", id, a)
			}
			seen[a] = true
		}
	}
	for id, e := range d.Engines {
		supported := e.Family == engine.LlamaServer && e.Adapter == "llama-server/v2" && e.Supply.Package == "llama-cpp" || e.Family == engine.Splash && e.Adapter == "splash/v1" && e.Supply.Package == "splash"
		supported = supported || e.Supply.Python != nil &&
			(e.Family == engine.RapidMLX && e.Adapter == "rapid-mlx/v1" && e.Supply.Package == "rapid-mlx" ||
				e.Family == engine.VLLMMetal && e.Adapter == "vllm-metal/v1" && e.Supply.Package == "vllm-metal")
		if !idPattern.MatchString(id) || !supported {
			return fmt.Errorf("engine %q requires a supported engine adapter and a complete release closure", id)
		}
		if !slices.Equal(e.Interfaces, []string{engine.InterfaceChatCompletions}) || !slices.Equal(e.Modalities, []string{"text"}) {
			return fmt.Errorf("engine %q requires a text chat release closure", id)
		}
		if e.Supply.Versions != nil {
			return fmt.Errorf("engine %q version requirements belong to the consuming preset", id)
		}
		if err := e.Supply.validate(); err != nil {
			return fmt.Errorf("engine %q: %w", id, err)
		}
	}
	for id, l := range d.Presets {
		if err := l.validateContext(); err != nil {
			return fmt.Errorf("preset %q: %w", id, err)
		}

		if !idPattern.MatchString(id) || strings.TrimSpace(l.DisplayName) == "" {
			return fmt.Errorf("preset %q needs a stable id and display name", id)
		}
		a, ok := d.Artifacts[l.Artifact]
		if !ok {
			return fmt.Errorf("preset %q references unknown artifact %q", id, l.Artifact)
		}
		e, ok := d.Engines[l.Engine]
		if !ok {
			return fmt.Errorf("preset %q references unknown engine %q", id, l.Engine)
		}
		if _, err := l.EngineConfig.value(); err != nil {
			return fmt.Errorf("preset %q: %w", id, err)
		}
		if l.EngineConfig.Kind != e.Adapter {
			return fmt.Errorf("preset %q engine config does not match its adapter", id)
		}

		if err := l.EngineVersions.validate(); err != nil {
			return fmt.Errorf("preset %q: %w", id, err)
		}
		if len(l.Patches) > 1 {
			return fmt.Errorf("preset %q supports one template patch", id)
		}
		for _, p := range l.Patches {
			patch, ok := d.Patches[p]
			if !ok || !slices.Contains(patch.CompatibleArtifacts, l.Artifact) {
				return fmt.Errorf("preset %q patch %q is absent or incompatible", id, p)
			}
		}
		if l.Interface != engine.InterfaceChatCompletions || !slices.Equal(l.Modalities, []string{"text"}) {
			return fmt.Errorf("preset %q requires text chat completions", id)
		}
		if e.Family == engine.Splash {
			draft, ok := d.Artifacts[l.Speculation.DraftArtifact]
			if !ok || draft.Format != "safetensors" || l.Speculation.Method != "dflash2" || l.Speculation.Source != "artifact" || l.Speculation.MaxDraftTokens != 0 {
				return fmt.Errorf("preset %q requires a complete DFlash2 sidecar and Splash", id)
			}
		} else if l.Speculation.Source == "artifact" {
			draft, ok := d.Artifacts[l.Speculation.DraftArtifact]
			if !ok || e.Family != engine.LlamaServer || draft.Format != "gguf" || (l.Speculation.Method != "mtp" && l.Speculation.Method != "dflash" && l.Speculation.Method != "dflash2") {
				return fmt.Errorf("preset %q requires an exact GGUF draft for llama-server MTP or DFlash", id)
			}
		} else if l.Speculation.DraftArtifact != "" {
			return fmt.Errorf("preset %q: draft artifact requires artifact speculation source", id)
		} else if l.Speculation.Method == "none" {
			if l.Speculation.Source != "none" || l.Speculation.MaxDraftTokens != 0 {
				return fmt.Errorf("preset %q none speculation cannot carry a draft source", id)
			}
		} else if l.Speculation.Method != "mtp" || l.Speculation.Source != "embedded" {
			return fmt.Errorf("preset %q requires none or embedded mtp speculation", id)
		}
		if _, err := engine.Build(l.request(id, a, "/model.gguf", "/template.jinja")); err != nil {
			return fmt.Errorf("preset %q: %w", id, err)
		}
	}

	return nil
}

func validateMaterial(id, repo, revision string, files []File, license string) error {
	if !idPattern.MatchString(id) || !repoPattern.MatchString(repo) || !revisionPattern.MatchString(revision) || strings.TrimSpace(license) == "" || len(files) == 0 {
		return fmt.Errorf("%q needs exact source, files and license", id)
	}
	seen := map[string]bool{}
	for _, f := range files {
		if !safePath(f.Path) || f.Bytes <= 0 || !hashPattern.MatchString(f.SHA256) || seen[f.Path] {
			return fmt.Errorf("%q has an unsafe, duplicate or incomplete file", id)
		}
		seen[f.Path] = true
	}
	return nil
}

func safePath(p string) bool {
	return p != "" && p != "." && p != ".." && !strings.HasPrefix(p, "../") && !strings.HasPrefix(p, "/") && !strings.ContainsAny(p, "\\\r\n\x00") && path.Clean(p) == p
}

func (s Supply) validate() error {
	if s.Target.OS != "darwin" || s.Target.Arch != "arm64" || s.Target.Distribution != "" || s.Target.DistributionVersion != "" {
		return errors.New("release compatibility must be darwin/arm64; observed OS versions belong to machine evidence")
	}
	return s.validateSource()
}

func (l Preset) request(id string, a Artifact, modelPath, templatePath string) engine.Request {
	c := l.EngineConfig
	if len(l.Patches) == 0 {
		templatePath = ""
	}
	r := engine.Request{Engine: engine.LlamaServer, LayoutID: id, ModelPath: modelPath, ArtifactFormat: a.Format, KVCache: c.KVCache,
		Interface: l.Interface, Modalities: l.Modalities, Window: l.ContextWindowTokens, MaxTokens: l.RequestDefaults.MaxOutputTokens,
		Thinking: l.RequestDefaults.Reasoning, Speculation: l.Speculation.Method, SpeculativeTokens: l.Speculation.MaxDraftTokens,
		ChatTemplatePath: templatePath, NGL: &c.GPULayers, Sampling: &l.RequestDefaults.Sampling,
		LlamaServer: &engine.LlamaServerTuning{Parallel: c.Parallel, FlashAttention: c.FlashAttention, Batch: c.BatchTokens, UBatch: c.MicrobatchTokens,
			ContextCheckpoints: &c.ContextCheckpoints, PromptCacheRAMMiB: &c.PromptCacheRAMMiB, Controls: &c.Controls}}
	if c.Splash != nil {
		r.Engine, r.LlamaServer, r.NGL = engine.Splash, nil, nil
		r.KVCache = c.Splash.KVCache
		r.Splash = &engine.SplashTuning{SplashConfig: c.Splash.SplashConfig, ModelID: a.Repo, AssemblyPath: "/prepared", StatePath: "/state"}
	} else if c.Python != nil {
		r.LlamaServer, r.NGL, r.Sampling = nil, nil, nil
		r.KVCache = ""
		if t := c.Python.RapidMLX; t != nil {
			r.Engine = engine.RapidMLX
			r.RapidMLX = &engine.RapidMLXTuning{MaxNumSeqs: t.MaxNumSeqs, MaxConcurrentRequests: t.MaxConcurrentRequests, PrefillBatchSize: t.PrefillBatchSize, CompletionBatchSize: t.CompletionBatchSize, GPUMemoryUtilization: t.GPUMemoryUtilization, PrefixCache: t.PrefixCache, CacheMemoryMiB: t.CacheMemoryMiB, KVCacheDType: t.KVCacheDType, PFlash: t.PFlash, ReasoningParser: t.ReasoningParser}
			r.RapidMLX.ToolCallParser, r.RapidMLX.PrefillStepSize, r.RapidMLX.RequestTimeoutSeconds = t.ToolCallParser, t.PrefillStepSize, t.RequestTimeoutSeconds
		}
		if t := c.Python.VLLMMetal; t != nil {
			r.Engine = engine.VLLMMetal
			r.VLLMMetal = &engine.VLLMMetalTuning{MaxNumSeqs: t.MaxNumSeqs, MaxNumBatchedTokens: t.MaxNumBatchedTokens, GPUMemoryUtilization: t.GPUMemoryUtilization, KVCacheDType: t.KVCacheDType, PrefixCache: t.PrefixCache}
			r.VLLMMetal.ToolCallParser, r.VLLMMetal.ReasoningParser = t.ToolCallParser, t.ReasoningParser
			r.VLLMMetal.LanguageModelOnly, r.VLLMMetal.ChunkedPrefill, r.VLLMMetal.BlockSize = t.LanguageModelOnly, t.ChunkedPrefill, t.BlockSize
		}
	} else if l.Speculation.DraftArtifact != "" {
		r.DraftModelPath = "/draft.gguf"
	}
	return r
}
