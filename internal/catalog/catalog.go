// Package catalog reads Temper's preset catalog and compiles explicit choices
// into portable, self-contained execution locks. Reading and compilation are
// pure; software discovery is a separate read through ResolveSoftware.
//
// A catalog has two zones. Execution records (router, artifacts, templates,
// engines, presets) are strict: a record this Temper cannot fully read is
// unavailable, never guessed. Each record's `presentation` and the top-level
// `presentation` are tolerant: unknown keys are ignored and invalid values
// fall back. Containers (the top level and `runtime`) ignore unknown keys.
// Parse is the publisher's strict form; Read is every client's form.
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

	"github.com/temper-sh/temper/internal/modelmemory"
	templatepatch "github.com/temper-sh/temper/internal/patch"
	"github.com/temper-sh/temper/internal/render/engine"
	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter/upstreamrelease"
	"gopkg.in/yaml.v3"
)

const (
	Schema     = "temper-catalog/v1"
	LockSchema = "temper-execution-lock/v1"
	// Platform is the one platform this Temper reads catalogs for.
	Platform = "apple-silicon"
	// BuiltinTemplate selects the chat template embedded in the model file.
	BuiltinTemplate = "builtin"
)

var idPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)
var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Document struct {
	Schema       string              `yaml:"schema" json:"schema"`
	Date         string              `yaml:"date" json:"date"`
	Platform     string              `yaml:"platform" json:"platform"`
	Runtime      Runtime             `yaml:"runtime" json:"runtime"`
	Artifacts    map[string]Artifact `yaml:"artifacts" json:"artifacts"`
	Templates    map[string]Template `yaml:"templates,omitempty" json:"templates,omitempty"`
	Engines      map[string]Engine   `yaml:"engines" json:"engines"`
	Presets      map[string]Preset   `yaml:"presets" json:"presets"`
	Presentation *Presentation       `yaml:"presentation,omitempty" json:"presentation,omitempty"`
}

type Runtime struct {
	Router Router `yaml:"router" json:"router"`
}

// Router is the one llama-swap release every layout of this catalog runs.
type Router struct {
	Supply          `yaml:",inline"`
	MinimumTemper   string             `yaml:"minimum_temper,omitempty" json:"minimum_temper,omitempty"`
	ExcludeVersions []VersionExclusion `yaml:"exclude_versions,omitempty" json:"exclude_versions,omitempty"`
}

type File struct {
	Path   string `yaml:"path" json:"path"`
	Bytes  int64  `yaml:"bytes" json:"bytes"`
	SHA256 string `yaml:"sha256" json:"sha256"`
}

// Artifact is exact model material. Its identity is its format and files;
// every source serves the same paths with the same bytes.
type Artifact struct {
	Sources       []Source               `yaml:"sources" json:"sources"`
	Format        string                 `yaml:"format" json:"format"`
	Files         []File                 `yaml:"files" json:"files"`
	License       string                 `yaml:"license" json:"license"`
	ContextMemory *modelmemory.Attention `yaml:"context_memory,omitempty" json:"context_memory,omitempty"`
	Presentation  ArtifactPresentation   `yaml:"presentation,omitempty" json:"presentation,omitempty"`
}

// HuggingFace is the source this Temper downloads from.
func (a Artifact) HuggingFace() Source { return huggingFace(a.Sources) }

// Template is one chat template file, optionally rewritten by a closed,
// versioned Temper transform.
type Template struct {
	Transform           string               `yaml:"transform,omitempty" json:"transform,omitempty"`
	Sources             []Source             `yaml:"sources" json:"sources"`
	Files               []File               `yaml:"files" json:"files"`
	CompatibleArtifacts []string             `yaml:"compatible_artifacts" json:"compatible_artifacts"`
	License             string               `yaml:"license" json:"license"`
	Presentation        TemplatePresentation `yaml:"presentation,omitempty" json:"presentation,omitempty"`
}

// HuggingFace is the source this Temper downloads the template from.
func (t Template) HuggingFace() Source { return huggingFace(t.Sources) }

func (t Template) source() string {
	hf := t.HuggingFace()
	source := "hf://" + hf.Repo + "@" + hf.Revision + "/" + t.Files[0].Path
	if t.local() {
		source = "local:" + t.Files[0].Path
	}
	if t.Transform != "" {
		source += "?transform=" + t.Transform
	}
	return source
}

// Supply separates an upstream source from its resolved release. Complete
// archives include the executable and its libraries. Installer rows are derived.
type Supply struct {
	Python  *PythonSupply                 `yaml:"python,omitempty" json:"python,omitempty"`
	Package string                        `yaml:"package" json:"package"`
	Target  software.Target               `yaml:"target" json:"target"`
	Source  *upstreamrelease.GitHubSource `yaml:"source,omitempty" json:"source,omitempty"`
	Release *upstreamrelease.Release      `yaml:"release,omitempty" json:"release,omitempty"`
}

// Engine is one maintained engine line; its release changes inside the record.
type Engine struct {
	Adapter         string             `yaml:"adapter" json:"adapter"`
	MinimumTemper   string             `yaml:"minimum_temper,omitempty" json:"minimum_temper,omitempty"`
	ExcludeVersions []VersionExclusion `yaml:"exclude_versions,omitempty" json:"exclude_versions,omitempty"`
	Interfaces      []string           `yaml:"interfaces" json:"interfaces"`
	Modalities      []string           `yaml:"modalities" json:"modalities"`
	Supply          Supply             `yaml:"supply" json:"supply"`
	Presentation    EnginePresentation `yaml:"presentation,omitempty" json:"presentation,omitempty"`
}

// Family is the engine kind: its adapter name without the version.
func (e Engine) Family() string {
	family, _, _ := strings.Cut(e.Adapter, "/")
	return family
}

type RequestDefaults struct {
	// Zero leaves generation bounded by the remaining context, without a separate output cap.
	MaxOutputTokens       int                     `yaml:"max_output_tokens,omitempty" json:"max_output_tokens,omitempty"`
	Reasoning             string                  `yaml:"reasoning" json:"reasoning"`
	ReasoningBudgetTokens *int                    `yaml:"reasoning_budget_tokens,omitempty" json:"reasoning_budget_tokens,omitempty"`
	Sampling              engine.SamplingDefaults `yaml:"sampling" json:"sampling"`
}

// empty reports that no generation default is set. A 0.1.0 lock writes every
// field, so its zero seed counts as no seed.
func (r RequestDefaults) empty() bool {
	// Legacy locks serialized zero-valued numeric sampling even for rerankers.
	for _, field := range []**float64{&r.Sampling.Temperature, &r.Sampling.TopP, &r.Sampling.MinP, &r.Sampling.RepeatPenalty, &r.Sampling.PresencePenalty} {
		if *field != nil && **field == 0 {
			*field = nil
		}
	}
	if r.Sampling.TopK != nil && *r.Sampling.TopK == 0 {
		r.Sampling.TopK = nil
	}
	if r.Sampling.Seed != nil && *r.Sampling.Seed == 0 {
		r.Sampling.Seed = nil
	}
	return r == RequestDefaults{}
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
	ContextCheckpoints *int                       `yaml:"context_checkpoints,omitempty" json:"context_checkpoints,omitempty"`
	PromptCacheRAMMiB  *int                       `yaml:"prompt_cache_ram_mib,omitempty" json:"prompt_cache_ram_mib,omitempty"`
	GPULayers          int                        `yaml:"gpu_layers" json:"gpu_layers"`
	Controls           engine.LlamaServerControls `yaml:"controls" json:"controls"`
}

// Preset is one exact execution: artifact, engine, template and settings.
// Machine-dependent settings live only in its profiles.
type Preset struct {
	ReasoningCapability   string             `yaml:"reasoning_capability,omitempty" json:"reasoning_capability,omitempty"`
	Artifact              string             `yaml:"artifact" json:"artifact"`
	ProjectorArtifact     string             `yaml:"projector_artifact,omitempty" json:"projector_artifact,omitempty"`
	Template              string             `yaml:"template,omitempty" json:"template,omitempty"`
	Engine                string             `yaml:"engine" json:"engine"`
	Compute               ComputeDevice      `yaml:"compute" json:"compute"`
	ComputeOptions        []ComputeDevice    `yaml:"compute_options" json:"compute_options"`
	Interface             string             `yaml:"interface" json:"interface"`
	Modalities            []string           `yaml:"modalities" json:"modalities"`
	ContextLimitTokens    int                `yaml:"context_limit_tokens,omitempty" json:"context_limit_tokens,omitempty"`
	RequestDefaults       RequestDefaults    `yaml:"request_defaults,omitempty" json:"request_defaults,omitempty"`
	Speculation           Speculation        `yaml:"speculation,omitempty" json:"speculation,omitempty"`
	ExcludeEngineVersions []VersionExclusion `yaml:"exclude_engine_versions,omitempty" json:"exclude_engine_versions,omitempty"`
	Profiles              []Profile          `yaml:"profiles" json:"profiles"`
	Presentation          PresetPresentation `yaml:"presentation,omitempty" json:"presentation,omitempty"`
}

// Profile is one complete machine-dependent setting of a preset, chosen by
// the Mac's memory. A lock keeps only the selected profile.
type Profile struct {
	MinMemoryGiB        int                `yaml:"min_memory_gib" json:"min_memory_gib"`
	ContextWindowTokens int                `yaml:"context_window_tokens" json:"context_window_tokens"`
	EngineConfig        EngineConfig       `yaml:"engine_config" json:"engine_config"`
	Memory              *MemoryMeasurement `yaml:"memory,omitempty" json:"memory,omitempty"`
	// unreadable says why this Temper cannot use the profile; never serialized.
	unreadable string
}

func (p *Profile) UnmarshalYAML(node *yaml.Node) error {
	var value struct {
		MinMemoryGiB        int                `yaml:"min_memory_gib"`
		ContextWindowTokens int                `yaml:"context_window_tokens"`
		EngineConfig        EngineConfig       `yaml:"engine_config"`
		Memory              *MemoryMeasurement `yaml:"memory,omitempty"`
	}
	*p = Profile{}
	if err := decodeNode(node, &value); err != nil {
		p.unreadable = err.Error()
		return nil
	}
	p.MinMemoryGiB, p.ContextWindowTokens, p.EngineConfig = value.MinMemoryGiB, value.ContextWindowTokens, value.EngineConfig
	p.Memory = value.Memory
	return nil
}

// Window is the context window of the preset's first profile, which in a
// lock is the selected one.
func (p Preset) Window() int { return p.profile().ContextWindowTokens }

// Config is the engine configuration of the preset's first profile, which in
// a lock is the selected one.
func (p Preset) Config() EngineConfig { return p.profile().EngineConfig }

func (p Preset) profile() Profile {
	if len(p.Profiles) == 0 {
		return Profile{}
	}
	return p.Profiles[0]
}

// SelectProfile returns the profile with the highest memory floor the Mac
// meets; below the smallest floor it returns the smallest profile.
func (p Preset) SelectProfile(memoryGiB int) Profile {
	selected := p.profile()
	for _, profile := range p.Profiles {
		if profile.MinMemoryGiB <= memoryGiB {
			selected = profile
		}
	}
	return selected
}

// TemplateID is the selected template, or BuiltinTemplate.
func (p Preset) TemplateID() string {
	if p.Template == "" {
		return BuiltinTemplate
	}
	return p.Template
}

// patched reports whether the preset replaces the model's embedded template.
func (p Preset) patched() bool { return p.TemplateID() != BuiltinTemplate }

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

// decodeNode decodes one record strictly: an unknown field is an error.
func decodeNode(node *yaml.Node, into any) error {
	raw, err := yaml.Marshal(node)
	if err != nil {
		return err
	}
	return decode(raw, into)
}

// Parse is the publisher's strict reader: every record and presentation value
// must be understood and valid.
func Parse(data []byte) (Document, error) {
	var d Document
	if err := decode(data, &d); err != nil {
		return Document{}, err
	}
	d.normalize()
	if err := d.Validate(); err != nil {
		return Document{}, err
	}
	return d, nil
}

// normalize applies the defaults of optional execution fields.
func (d *Document) normalize() {
	for id, p := range d.Presets {
		if p.Speculation == (Speculation{}) {
			p.Speculation = Speculation{Method: "none", Source: "none"}
		}
		d.Presets[id] = p
	}
}

// Validate is the publisher's complete check: every execution record, every
// presentation value and the editorial rules.
func (d Document) Validate() error {
	if err := d.validateRecords(); err != nil {
		return err
	}
	for id, t := range d.Templates {
		if t.local() {
			return fmt.Errorf("template %q: a catalog cannot have a local template file", id)
		}
	}
	return d.validatePresentation()
}

// validateRecords checks the execution zone only: header, router and every
// record with its references. Locks and filtered client views use it.
func (d Document) validateRecords() error {
	if d.Schema != Schema {
		return fmt.Errorf("catalog schema must be %s", Schema)
	}
	if _, err := time.Parse("2006-01-02", d.Date); err != nil {
		return errors.New("catalog date must be YYYY-MM-DD")
	}
	if d.Platform != Platform {
		return fmt.Errorf("catalog platform must be %s", Platform)
	}
	if len(d.Artifacts) == 0 || len(d.Engines) == 0 || len(d.Presets) == 0 {
		return errors.New("catalog requires artifacts, engines and presets")
	}
	if err := validateRouter(d.Runtime.Router); err != nil {
		return fmt.Errorf("router: %w", err)
	}
	for id, a := range d.Artifacts {
		if err := validateArtifact(id, a); err != nil {
			return fmt.Errorf("artifact %q: %w", id, err)
		}
	}
	for id, t := range d.Templates {
		if err := validateTemplate(id, t); err != nil {
			return fmt.Errorf("template %q: %w", id, err)
		}
	}
	for id, e := range d.Engines {
		if err := validateEngine(id, e); err != nil {
			return fmt.Errorf("engine %q: %w", id, err)
		}
	}
	for id, p := range d.Presets {
		if err := d.validatePreset(id, p); err != nil {
			return fmt.Errorf("preset %q: %w", id, err)
		}
	}
	return nil
}

func validateRouter(r Router) error {
	if r.Package != "llama-swap" || r.Python != nil {
		return errors.New("runtime router must supply llama-swap from a release")
	}
	if err := r.Supply.validate(); err != nil {
		return err
	}
	if err := validateMinimumTemper(r.MinimumTemper); err != nil {
		return err
	}
	if err := validateExclusions(r.ExcludeVersions); err != nil {
		return err
	}
	return allowedRelease(r.Release, r.ExcludeVersions)
}

func validateArtifact(id string, a Artifact) error {
	if err := validateMaterial(id, a.Sources, a.Files, a.License, false); err != nil {
		return err
	}
	if a.ContextMemory != nil {
		if err := a.ContextMemory.Validate(); err != nil {
			return err
		}
	}
	if a.Format == "safetensors" && len(a.Files) == 2 && a.Files[0].Path == "config.json" && a.Files[1].Path == "model.safetensors" {
		return nil
	}
	if a.Format == "safetensors" && len(a.Files) == 1 && strings.HasSuffix(a.Files[0].Path, ".safetensors") {
		return nil // A selected MTP sidecar; target snapshots are checked below.
	}
	if a.Format == "mlx-safetensors" || a.Format == "safetensors" {
		if completeSnapshot(a.Files) {
			return nil
		}
		return errors.New("requires config, tokenizer and safetensors weights")
	}
	if a.Format != "gguf" || len(a.Files) != 1 || !strings.HasSuffix(a.Files[0].Path, ".gguf") {
		return errors.New("this executable slice requires one complete GGUF")
	}
	return nil
}

func completeSnapshot(files []File) bool {
	config, weights, tokenizer := false, false, false
	for _, f := range files {
		config = config || f.Path == "config.json"
		weights = weights || strings.HasSuffix(f.Path, ".safetensors")
		tokenizer = tokenizer || f.Path == "tokenizer.json"
	}
	return config && weights && tokenizer
}

func validateTemplate(id string, t Template) error {
	if err := validateMaterial(id, t.Sources, t.Files, t.License, true); err != nil {
		return err
	}
	if len(t.Files) != 1 || len(t.CompatibleArtifacts) == 0 {
		return errors.New("requires one template file and explicit compatible artifacts")
	}
	if _, err := templatepatch.ParseSource(t.source()); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, a := range t.CompatibleArtifacts {
		// Compatibility may name artifacts outside a lock closure or unknown
		// to this Temper; a selected preset must still resolve its own artifact.
		if !idPattern.MatchString(a) || seen[a] {
			return fmt.Errorf("invalid or repeated artifact %q", a)
		}
		seen[a] = true
	}
	return nil
}

// adapters is the closed set of engine adapters this Temper runs, with the
// package each installs, its interfaces and its modalities.
var adapters = map[string]struct {
	pkg        string
	python     bool
	interfaces []string
	modalities []string
}{
	"llama-server/v1": {"llama-cpp", false, []string{engine.InterfaceChatCompletions, engine.InterfaceReranking}, []string{"text", "image"}},
	"splash/v1":       {"splash", false, []string{engine.InterfaceChatCompletions}, []string{"text"}},
	"splash/v2":       {"splash", false, []string{engine.InterfaceChatCompletions}, []string{"text"}},
	"crispembed/v1":   {"crispembed", false, []string{engine.InterfaceReranking}, []string{"text"}},
	"kapitsa/v1":      {"kapitsa", false, []string{engine.InterfaceChatCompletions}, []string{"text"}},
	"rapid-mlx/v1":    {"rapid-mlx", true, []string{engine.InterfaceChatCompletions}, []string{"text"}},
	"vllm-metal/v1":   {"vllm-metal", true, []string{engine.InterfaceChatCompletions}, []string{"text"}},
}

func validateEngine(id string, e Engine) error {
	adapter, ok := adapters[e.Adapter]
	if !idPattern.MatchString(id) || !ok || e.Supply.Package != adapter.pkg || (e.Supply.Python != nil) != adapter.python {
		return errors.New("requires a supported engine adapter, its package and a complete release closure")
	}
	for _, pair := range []struct{ values, supported []string }{{e.Interfaces, adapter.interfaces}, {e.Modalities, adapter.modalities}} {
		seen := map[string]bool{}
		if len(pair.values) == 0 {
			return errors.New("interfaces and modalities must not be empty")
		}
		for _, value := range pair.values {
			if seen[value] || !slices.Contains(pair.supported, value) {
				return fmt.Errorf("unsupported or repeated interface/modality %q", value)
			}
			seen[value] = true
		}
	}
	if !slices.Contains(e.Modalities, "text") {
		return errors.New("engine modalities must include text")
	}
	if err := e.Supply.validate(); err != nil {
		return err
	}
	if err := validateMinimumTemper(e.MinimumTemper); err != nil {
		return err
	}
	if err := validateExclusions(e.ExcludeVersions); err != nil {
		return err
	}
	return allowedRelease(e.Supply.Release, e.ExcludeVersions)
}

// knownCapabilities keeps only the interfaces and modalities this Temper's
// adapter knows. A capability nothing uses is not a dependency.
func (e Engine) knownCapabilities() Engine {
	adapter, ok := adapters[e.Adapter]
	if !ok {
		return e
	}
	keep := func(values, supported []string) []string {
		var result []string
		for _, value := range values {
			if slices.Contains(supported, value) && !slices.Contains(result, value) {
				result = append(result, value)
			}
		}
		return result
	}
	e.Interfaces = keep(e.Interfaces, adapter.interfaces)
	e.Modalities = keep(e.Modalities, adapter.modalities)
	return e
}

func (d Document) validatePreset(id string, p Preset) error {
	switch p.ReasoningCapability {
	case "": // Older records have not declared capability.
	case "switchable":
		if p.Interface != engine.InterfaceChatCompletions {
			return errors.New("reasoning capability requires a chat preset")
		}
	case "always":
		if p.RequestDefaults.Reasoning != "on" {
			return errors.New("always-thinking models require an on default")
		}
	case "none":
		if p.RequestDefaults.Reasoning != "off" {
			return errors.New("non-thinking models require an off default")
		}
	default:
		return errors.New("reasoning_capability must be switchable, always or none")
	}

	if !idPattern.MatchString(id) {
		return errors.New("needs a stable id")
	}
	a, ok := d.Artifacts[p.Artifact]
	if !ok {
		return fmt.Errorf("references unknown artifact %q", p.Artifact)
	}
	e, ok := d.Engines[p.Engine]
	if !ok {
		return fmt.Errorf("references unknown engine %q", p.Engine)
	}
	if len(p.Profiles) == 0 {
		return errors.New("requires at least one profile")
	}
	for i, profile := range p.Profiles {
		if profile.unreadable != "" {
			return fmt.Errorf("profile %d: %s", i, profile.unreadable)
		}
		if profile.MinMemoryGiB <= 0 || i > 0 && profile.MinMemoryGiB <= p.Profiles[i-1].MinMemoryGiB {
			return errors.New("profile min_memory_gib values must be positive and strictly increasing")
		}
		if _, err := profile.EngineConfig.value(); err != nil {
			return err
		}
		if profile.EngineConfig.Kind != e.Adapter {
			return errors.New("engine config does not match its adapter")
		}
		if profile.Memory != nil {
			if err := profile.Memory.validate(); err != nil {
				return fmt.Errorf("profile %d: %w", i, err)
			}
		}
	}
	if err := p.validateContext(); err != nil {
		return err
	}
	if err := p.validateCompute(); err != nil {
		return err
	}
	if err := validateExclusions(p.ExcludeEngineVersions); err != nil {
		return err
	}
	if err := allowedRelease(e.Supply.Release, p.ExcludeEngineVersions); err != nil {
		return err
	}
	if p.patched() {
		t, ok := d.Templates[p.Template]
		if !ok || !slices.Contains(t.CompatibleArtifacts, p.Artifact) {
			return fmt.Errorf("template %q is absent or incompatible", p.Template)
		}
	}
	if !slices.Contains(e.Interfaces, p.Interface) {
		return errors.New("interface is not offered by its engine")
	}
	for _, modality := range p.Modalities {
		if !slices.Contains(e.Modalities, modality) {
			return fmt.Errorf("modality %q is not offered by its engine", modality)
		}
	}
	if p.ProjectorArtifact != "" {
		projector, ok := d.Artifacts[p.ProjectorArtifact]
		if !ok || projector.Format != "gguf" || e.Family() != engine.LlamaServer || p.Interface != engine.InterfaceChatCompletions || !slices.Equal(p.Modalities, []string{"text", "image"}) {
			return errors.New("requires an exact GGUF projector for llama-server image input")
		}
	}
	if p.Interface == engine.InterfaceReranking && !p.RequestDefaults.empty() {
		return errors.New("reranking cannot declare generation defaults")
	}
	if (e.Family() == engine.RapidMLX || e.Family() == engine.VLLMMetal || e.Family() == engine.Kapitsa) && !completeSnapshot(a.Files) {
		return errors.New("target snapshot requires config, tokenizer and safetensors weights")
	}
	if e.Family() == engine.Splash || e.Family() == engine.Kapitsa {
		draft, ok := d.Artifacts[p.Speculation.DraftArtifact]
		if !ok || draft.Format != "safetensors" || p.Speculation.Method != "dflash2" || p.Speculation.Source != "artifact" || p.Speculation.MaxDraftTokens != 0 {
			return errors.New("requires a complete DFlash2 sidecar for this engine")
		}
		if e.Family() == engine.Kapitsa {
			for _, file := range append(append([]File{}, a.Files...), draft.Files...) {
				for _, part := range strings.Split(file.Path, "/") {
					if strings.HasPrefix(part, "._") {
						return errors.New("Kapitsa model files cannot include AppleDouble sidecars")
					}
				}
			}
		}
	} else if p.Speculation.Source == "artifact" {
		draft, ok := d.Artifacts[p.Speculation.DraftArtifact]
		if e.Family() == engine.RapidMLX {
			if !ok || draft.Format != "safetensors" || len(draft.Files) != 1 || !strings.HasSuffix(draft.Files[0].Path, ".safetensors") || p.Speculation.Method != "mtp" {
				return errors.New("Rapid MLX MTP requires one exact MLX safetensors sidecar")
			}
		} else if !ok || e.Family() != engine.LlamaServer || draft.Format != "gguf" || (p.Speculation.Method != "mtp" && p.Speculation.Method != "dflash" && p.Speculation.Method != "dflash2") {
			return errors.New("requires an exact GGUF draft for llama-server MTP or DFlash")
		}
	} else if p.Speculation.DraftArtifact != "" {
		return errors.New("draft artifact requires artifact speculation source")
	} else if p.Speculation.Method == "none" {
		if p.Speculation.Source != "none" || p.Speculation.MaxDraftTokens != 0 {
			return errors.New("none speculation cannot carry a draft source")
		}
	} else if p.Speculation.Method != "mtp" || p.Speculation.Source != "embedded" {
		return errors.New("requires none or embedded mtp speculation")
	}
	for _, profile := range p.Profiles {
		if _, err := engine.Build(p.request(id, profile, a, "/model.gguf", "/template.jinja")); err != nil {
			return err
		}
	}
	return nil
}

func validateMaterial(id string, sources []Source, files []File, license string, local bool) error {
	if !idPattern.MatchString(id) || strings.TrimSpace(license) == "" || len(files) == 0 {
		return errors.New("needs a stable id, files and a license")
	}
	if !plainText(license) {
		return errors.New("license must be plain text")
	}
	if err := validateSources(sources, local); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, f := range files {
		if !safePath(f.Path) || f.Bytes <= 0 || !hashPattern.MatchString(f.SHA256) || seen[f.Path] {
			return errors.New("has an unsafe, duplicate or incomplete file")
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

func (l Preset) request(id string, profile Profile, a Artifact, modelPath, templatePath string) engine.Request {
	c := profile.EngineConfig
	ngl := l.gpuOffloadLayers(c)
	if !l.patched() {
		templatePath = ""
	}
	r := engine.Request{Engine: engine.LlamaServer, LayoutID: id, ModelPath: modelPath, ArtifactFormat: a.Format, KVCache: c.KVCache,
		Interface: l.Interface, Modalities: l.Modalities, Window: profile.ContextWindowTokens, MaxTokens: l.RequestDefaults.MaxOutputTokens,
		Thinking: l.RequestDefaults.Reasoning, ReasoningBudgetTokens: l.RequestDefaults.ReasoningBudgetTokens,
		Speculation: l.Speculation.Method, SpeculativeTokens: l.Speculation.MaxDraftTokens,
		ChatTemplatePath: templatePath, NGL: &ngl, Sampling: &l.RequestDefaults.Sampling,
		LlamaServer: &engine.LlamaServerTuning{Parallel: c.Parallel, FlashAttention: c.FlashAttention, Batch: c.BatchTokens, UBatch: c.MicrobatchTokens,
			ContextCheckpoints: c.ContextCheckpoints, PromptCacheRAMMiB: c.PromptCacheRAMMiB, Controls: &c.Controls}}
	if l.ProjectorArtifact != "" {
		r.ProjectorPath = "/projector.gguf"
	}
	if l.Interface == engine.InterfaceReranking {
		r.Sampling = nil
	}
	if c.Kapitsa != nil {
		r.Engine, r.LlamaServer, r.NGL, r.KVCache = engine.Kapitsa, nil, nil, ""
		r.Kapitsa = &engine.KapitsaTuning{KapitsaConfig: c.Kapitsa.KapitsaConfig, StatePath: "/state"}
		r.DraftModelPath = "/draft"
	} else if c.CrispEmbed != nil {
		r.Engine, r.LlamaServer, r.NGL, r.Sampling = engine.CrispEmbed, nil, nil, nil
		r.KVCache = ""
		r.CrispEmbed = &c.CrispEmbed.CrispEmbedTuning
	} else if c.Splash != nil {
		r.Engine, r.LlamaServer, r.NGL = engine.Splash, nil, nil
		r.KVCache = c.Splash.KVCache
		r.Splash = &engine.SplashTuning{SplashConfig: c.Splash.SplashConfig, Adapter: c.Kind, ModelID: a.HuggingFace().Repo, AssemblyPath: "/prepared", StatePath: "/state"}
	} else if c.Python != nil {
		r.LlamaServer, r.NGL, r.Sampling = nil, nil, nil
		r.KVCache = ""
		if t := c.Python.RapidMLX; t != nil {
			r.Engine = engine.RapidMLX
			if l.Speculation.DraftArtifact != "" {
				r.DraftModelPath = "/draft/model.safetensors"
			}
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
