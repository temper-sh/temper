// Package catalog compiles explicit local catalog choices into portable,
// self-contained execution locks. Compilation is pure; software discovery is
// a separate read through ResolveSoftware.
package catalog

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/temper-sh/temper/internal/render/engine"
	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter/upstreamrelease"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"gopkg.in/yaml.v3"
)

const Schema = "temper-catalog/v2"
const SelectionSchema = "temper-selection/v2"
const LockSchema = "temper-execution-lock/v2"

// V1 is retained only for already-issued Field Kit inputs. New authored
// catalogs use sources and resolved releases, without installer bookkeeping.
const legacySchema = "temper-catalog/v1"
const legacySelectionSchema = "temper-selection/v1"
const legacyLockSchema = "temper-execution-lock/v1"

var idPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)
var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Document struct {
	Schema    string              `yaml:"schema" json:"schema"`
	Date      string              `yaml:"date" json:"date"`
	Runtime   Runtime             `yaml:"runtime" json:"runtime"`
	Artifacts map[string]Artifact `yaml:"artifacts" json:"artifacts"`
	Patches   map[string]Patch    `yaml:"patches" json:"patches"`
	Engines   map[string]Engine   `yaml:"engines" json:"engines"`
	Layouts   map[string]Layout   `yaml:"layouts" json:"layouts"`
	Profiles  map[string]Profile  `yaml:"profiles" json:"profiles"`
}

type Runtime struct {
	Router Supply `yaml:"router" json:"router"`
}

type File struct {
	Path   string `yaml:"path" json:"path"`
	Bytes  int64  `yaml:"bytes" json:"bytes"`
	SHA256 string `yaml:"sha256" json:"sha256"`
}

type Artifact struct {
	Repo     string `yaml:"repo" json:"repo"`
	Revision string `yaml:"revision" json:"revision"`
	Format   string `yaml:"format" json:"format"`
	Files    []File `yaml:"files" json:"files"`
	License  string `yaml:"license" json:"license"`
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
	Package  string                        `yaml:"package" json:"package"`
	Target   software.Target               `yaml:"target" json:"target"`
	Source   *upstreamrelease.GitHubSource `yaml:"source,omitempty" json:"source,omitempty"`
	Release  *upstreamrelease.Release      `yaml:"release,omitempty" json:"release,omitempty"`
	Versions *Versions                     `yaml:"versions,omitempty" json:"versions,omitempty"`
	// Legacy V1 compatibility; forbidden in V2 authoring.
	Selection *softwarelock.Selection      `yaml:"selection,omitempty" json:"selection,omitempty"`
	Units     map[string]softwarelock.Unit `yaml:"units,omitempty" json:"units,omitempty"`
}

type Engine struct {
	Family     string   `yaml:"family" json:"family"`
	Adapter    string   `yaml:"adapter" json:"adapter"`
	Supply     Supply   `yaml:"supply" json:"supply"`
	Interfaces []string `yaml:"interfaces" json:"interfaces"`
	Modalities []string `yaml:"modalities" json:"modalities"`
}

type RequestDefaults struct {
	MaxOutputTokens int                     `yaml:"max_output_tokens" json:"max_output_tokens"`
	Reasoning       string                  `yaml:"reasoning" json:"reasoning"`
	Sampling        engine.SamplingDefaults `yaml:"sampling" json:"sampling"`
}

type Speculation struct {
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

type Layout struct {
	DisplayName         string          `yaml:"display_name" json:"display_name"`
	Artifact            string          `yaml:"artifact" json:"artifact"`
	Patches             []string        `yaml:"patches" json:"patches"`
	Engine              string          `yaml:"engine" json:"engine"`
	Interface           string          `yaml:"interface" json:"interface"`
	Modalities          []string        `yaml:"modalities" json:"modalities"`
	ContextWindowTokens int             `yaml:"context_window_tokens" json:"context_window_tokens"`
	RequestDefaults     RequestDefaults `yaml:"request_defaults" json:"request_defaults"`
	Speculation         Speculation     `yaml:"speculation" json:"speculation"`
	EngineConfig        LlamaConfig     `yaml:"engine_config" json:"engine_config"`
	EngineVersions      *Versions       `yaml:"engine_versions,omitempty" json:"engine_versions,omitempty"`
}

type Binding struct {
	ID             string `yaml:"id,omitempty" json:"id,omitempty"` // V1 only; layout is the binding identity.
	Layout         string `yaml:"layout" json:"layout"`
	Route          string `yaml:"route" json:"route"`
	Residency      string `yaml:"residency" json:"residency"`
	IdleTTLSeconds int    `yaml:"idle_ttl_seconds" json:"idle_ttl_seconds"`
	Preload        bool   `yaml:"preload" json:"preload"`
}

type Profile struct {
	Bindings             []Binding `yaml:"bindings" json:"bindings"`
	GPUMemoryUtilization float64   `yaml:"gpu_memory_utilization" json:"gpu_memory_utilization"`
}

type Selection struct {
	Schema       string   `yaml:"schema" json:"schema"`
	Profile      string   `yaml:"profile" json:"profile"`
	Tools        []string `yaml:"tools,omitempty" json:"tools,omitempty"`               // V1 only.
	Integrations []string `yaml:"integrations,omitempty" json:"integrations,omitempty"` // V1 only.
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

func ParseSelection(data []byte) (Selection, error) {
	var s Selection
	if err := decode(data, &s); err != nil {
		return Selection{}, err
	}
	if err := s.Validate(); err != nil {
		return Selection{}, err
	}
	return s, nil
}

func (s Selection) Validate() error {
	if (s.Schema != SelectionSchema && s.Schema != legacySelectionSchema) || !idPattern.MatchString(s.Profile) {
		return errors.New("selection requires a supported schema and a stable profile id")
	}
	if len(s.Tools) != 0 || len(s.Integrations) != 0 {
		return errors.New("this catalog slice has no supported optional tools or integrations")
	}
	if s.Schema == SelectionSchema && (s.Tools != nil || s.Integrations != nil) {
		return errors.New("v2 selection contains only schema and profile")
	}
	return nil
}

func (d Document) Validate() error {
	if d.Schema != Schema && d.Schema != legacySchema {
		return fmt.Errorf("catalog schema must be %s", Schema)
	}
	if _, err := time.Parse("2006-01-02", d.Date); err != nil {
		return errors.New("catalog date must be YYYY-MM-DD")
	}
	if len(d.Artifacts) == 0 || len(d.Engines) == 0 || len(d.Layouts) == 0 || len(d.Profiles) == 0 {
		return errors.New("catalog requires artifacts, engines, layouts and profiles")
	}
	if d.Runtime.Router.Package != "llama-swap" {
		return errors.New("runtime router must supply llama-swap")
	}
	if err := d.Runtime.Router.validate(d.Date, d.Schema == legacySchema); err != nil {
		return fmt.Errorf("router: %w", err)
	}
	for id, a := range d.Artifacts {
		if err := validateMaterial(id, a.Repo, a.Revision, a.Files, a.License); err != nil {
			return fmt.Errorf("artifact: %w", err)
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
			// lock closure. A selected layout must still resolve its own artifact.
			if !idPattern.MatchString(a) || seen[a] {
				return fmt.Errorf("patch %q has invalid or repeated artifact %q", id, a)
			}
			seen[a] = true
		}
	}
	for id, e := range d.Engines {
		if !idPattern.MatchString(id) || e.Family != engine.LlamaServer || e.Adapter != "llama-server/v2" {
			return fmt.Errorf("engine %q requires the supported llama-server/v2 adapter and a complete release closure", id)
		}
		if e.Supply.Package != "llama-cpp" || !slices.Equal(e.Interfaces, []string{engine.InterfaceChatCompletions}) || !slices.Equal(e.Modalities, []string{"text"}) {
			return fmt.Errorf("engine %q requires the text chat llama-cpp closure", id)
		}
		if e.Supply.Versions != nil {
			return fmt.Errorf("engine %q version requirements belong to the consuming layout", id)
		}
		if err := e.Supply.validate(d.Date, d.Schema == legacySchema); err != nil {
			return fmt.Errorf("engine %q: %w", id, err)
		}
	}
	for id, l := range d.Layouts {
		if !idPattern.MatchString(id) || strings.TrimSpace(l.DisplayName) == "" {
			return fmt.Errorf("layout %q needs a stable id and display name", id)
		}
		a, ok := d.Artifacts[l.Artifact]
		if !ok {
			return fmt.Errorf("layout %q references unknown artifact %q", id, l.Artifact)
		}
		e, ok := d.Engines[l.Engine]
		if !ok {
			return fmt.Errorf("layout %q references unknown engine %q", id, l.Engine)
		}
		if l.EngineConfig.Kind != e.Adapter {
			return fmt.Errorf("layout %q engine config does not match its adapter", id)
		}
		if d.Schema == legacySchema && l.EngineVersions != nil {
			return fmt.Errorf("layout %q: v1 cannot carry version policy", id)
		}
		if err := l.EngineVersions.validate(); err != nil {
			return fmt.Errorf("layout %q: %w", id, err)
		}
		if len(l.Patches) > 1 {
			return fmt.Errorf("layout %q supports one template patch", id)
		}
		for _, p := range l.Patches {
			patch, ok := d.Patches[p]
			if !ok || !slices.Contains(patch.CompatibleArtifacts, l.Artifact) {
				return fmt.Errorf("layout %q patch %q is absent or incompatible", id, p)
			}
		}
		if l.Interface != engine.InterfaceChatCompletions || !slices.Equal(l.Modalities, []string{"text"}) {
			return fmt.Errorf("layout %q requires text chat completions", id)
		}
		if l.Speculation.Method == "none" {
			if l.Speculation.Source != "none" || l.Speculation.MaxDraftTokens != 0 {
				return fmt.Errorf("layout %q none speculation cannot carry a draft source", id)
			}
		} else if l.Speculation.Method != "mtp" || l.Speculation.Source != "embedded" {
			return fmt.Errorf("layout %q requires none or embedded mtp speculation", id)
		}
		if _, err := engine.Build(l.request(id, a, "/model.gguf", "/template.jinja")); err != nil {
			return fmt.Errorf("layout %q: %w", id, err)
		}
	}
	for id, p := range d.Profiles {
		if !idPattern.MatchString(id) || len(p.Bindings) == 0 {
			return fmt.Errorf("profile %q requires a stable id and bindings", id)
		}
		if !(p.GPUMemoryUtilization > 0 && p.GPUMemoryUtilization <= 1) {
			return fmt.Errorf("profile %q memory utilization must be in (0, 1]", id)
		}
		seenBindings, seenLayouts := map[string]bool{}, map[string]bool{}
		defaults := 0
		var engineID string
		for _, b := range p.Bindings {
			l, ok := d.Layouts[b.Layout]
			if !ok || seenLayouts[b.Layout] {
				return fmt.Errorf("profile %q has an unknown layout or duplicate binding", id)
			}
			if (d.Schema == legacySchema && (!idPattern.MatchString(b.ID) || seenBindings[b.ID])) || (d.Schema == Schema && b.ID != "") {
				return fmt.Errorf("profile %q: binding IDs belong only to v1; v2 uses the layout identity", id)
			}
			seenBindings[b.ID] = true
			seenLayouts[b.Layout] = true
			if b.Route == "default" {
				defaults++
			} else if b.Route != "available" {
				return fmt.Errorf("profile %q route must be default or available", id)
			}
			if b.Residency != "resident" && b.Residency != "on-demand" {
				return fmt.Errorf("profile %q has invalid residency", id)
			}
			if b.IdleTTLSeconds < 0 || b.Preload && b.Residency != "resident" {
				return fmt.Errorf("profile %q has invalid TTL or preload", id)
			}
			if engineID != "" && engineID != l.Engine {
				return fmt.Errorf("profile %q selects conflicting llama-cpp closures", id)
			}
			engineID = l.Engine
		}
		if defaults != 1 {
			return fmt.Errorf("profile %q requires exactly one default route", id)
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

func (s Supply) validate(date string, legacy bool) error {
	if s.Target.OS != "darwin" || s.Target.Arch != "arm64" || s.Target.Distribution != "" || s.Target.DistributionVersion != "" {
		return errors.New("release compatibility must be darwin/arm64; observed OS versions belong to machine evidence")
	}
	if !legacy {
		return s.validateSource()
	}
	if s.Source != nil || s.Release != nil || s.Versions != nil {
		return errors.New("v1 supply cannot carry v2 source fields")
	}
	if !idPattern.MatchString(s.Package) || s.Selection == nil || s.Selection.Provenance != softwarelock.ProvenanceExperiment || s.Selection.Method != "release-artifact" || s.Selection.Adapter != "upstream-release" {
		return errors.New("local catalog supply requires an exact experiment-provenance release-artifact closure")
	}
	if len(s.Units) != 1 {
		return errors.New("release supply requires one complete archive closure")
	}
	u, ok := s.Units[s.Selection.RootUnit]
	if !ok || u.Adapter != "upstream-release" || u.Scope != s.Package || len(u.Dependencies) != 0 || len(u.Artifacts) != 1 || !revisionPattern.MatchString(u.Revision) {
		return errors.New("release supply is incomplete or crosses an installation scope")
	}
	a := u.Artifacts[0]
	locator, err := url.Parse(a.Locator)
	if err != nil || locator.Scheme != "https" || locator.Host == "" || locator.User != nil || locator.Fragment != "" || a.Size <= 0 || a.UnpackedSize <= 0 || a.InstalledEntries <= 0 || a.Format != "tar.gz" || (a.ArchiveRoot != "." && !safePath(a.ArchiveRoot)) {
		return errors.New("release supply needs an exact HTTPS tar.gz with expanded size, entry count and archive root")
	}
	d := softwarelock.Document{Schema: softwarelock.SchemaV1, Target: s.Target, Resolved: date,
		Provenance: softwarelock.Provenance{Experiment: &softwarelock.ExperimentIdentity{Schema: Schema, ID: "local-catalog", DefinitionSHA256: strings.Repeat("0", 64)}},
		Selections: map[string]softwarelock.Selection{s.Package: *s.Selection}, Units: s.Units}
	return d.Validate()
}

func (l Layout) request(id string, a Artifact, modelPath, templatePath string) engine.Request {
	c := l.EngineConfig
	if len(l.Patches) == 0 {
		templatePath = ""
	}
	return engine.Request{Engine: engine.LlamaServer, LayoutID: id, ModelPath: modelPath, ArtifactFormat: a.Format, KVCache: c.KVCache,
		Interface: l.Interface, Modalities: l.Modalities, Window: l.ContextWindowTokens, MaxTokens: l.RequestDefaults.MaxOutputTokens,
		Thinking: l.RequestDefaults.Reasoning, Speculation: l.Speculation.Method, SpeculativeTokens: l.Speculation.MaxDraftTokens,
		ChatTemplatePath: templatePath, NGL: &c.GPULayers, Sampling: &l.RequestDefaults.Sampling,
		LlamaServer: &engine.LlamaServerTuning{Parallel: c.Parallel, FlashAttention: c.FlashAttention, Batch: c.BatchTokens, UBatch: c.MicrobatchTokens,
			ContextCheckpoints: &c.ContextCheckpoints, PromptCacheRAMMiB: &c.PromptCacheRAMMiB, Controls: &c.Controls}}
}
