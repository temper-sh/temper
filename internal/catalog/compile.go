package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"

	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/render"
	"github.com/temper-sh/temper/internal/software"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"gopkg.in/yaml.v3"
)

type Lock struct {
	Schema               string          `yaml:"schema" json:"schema"`
	SourceSnapshotSHA256 string          `yaml:"source_snapshot_sha256" json:"source_snapshot_sha256"`
	Preset               string          `yaml:"preset" json:"preset"`
	Target               software.Target `yaml:"target" json:"target"`
	Records              Document        `yaml:"records" json:"records"`
	ExecutionDigest      string          `yaml:"execution_digest" json:"execution_digest"`
}

// CompilePreset freezes one explicitly selected preset. Empty template and zero
// window use catalog defaults; "builtin" explicitly selects the embedded template.
// Compilation is pure and makes no publication or machine-fit claim.
func CompilePreset(d Document, id, template string, window int, target software.Target) (Lock, error) {
	if err := d.Validate(); err != nil {
		return Lock{}, err
	}
	if err := target.Validate(); err != nil {
		return Lock{}, err
	}
	if target.OS != "darwin" || target.Arch != "arm64" || target.Distribution != "" || target.DistributionVersion != "" {
		return Lock{}, errors.New("execution target is portable darwin/arm64 compatibility, not an observed OS version")
	}
	if _, ok := d.Presets[id]; !ok {
		return Lock{}, fmt.Errorf("unknown preset %q", id)
	}
	d = canonicalDocument(d)
	snapshot := digest(d)
	p := d.Presets[id]
	if window != 0 {
		if window <= p.RequestDefaults.MaxOutputTokens || window > p.ContextLimit() {
			return Lock{}, fmt.Errorf("preset %q context must be between %d and %d tokens (total input and output)", id, p.RequestDefaults.MaxOutputTokens+1, p.ContextLimit())
		}
		p.ContextWindowTokens = window
	}
	if template != "" {
		p.Patches = []string{}
		if template != "builtin" {
			patch, ok := d.Patches[template]
			if !ok || !slices.Contains(patch.CompatibleArtifacts, p.Artifact) {
				return Lock{}, fmt.Errorf("preset %q template patch %q is absent or incompatible", id, template)
			}
			p.Patches = []string{template}
		}
	}
	e := d.Engines[p.Engine]
	if release := e.Supply.Release; release != nil {
		if err := p.EngineVersions.require(release.Version); err != nil {
			return Lock{}, fmt.Errorf("preset %q: %w", id, err)
		}
	}
	if !e.Supply.Target.Matches(target) || !d.Runtime.Router.Target.Matches(target) {
		return Lock{}, errors.New("preset engine or router is incompatible with target")
	}
	selected := Document{Schema: Schema, Date: d.Date, Runtime: d.Runtime,
		Artifacts: map[string]Artifact{p.Artifact: d.Artifacts[p.Artifact]},
		Patches:   map[string]Patch{}, Engines: map[string]Engine{p.Engine: e},
		Presets: map[string]Preset{id: p}}
	if p.Speculation.DraftArtifact != "" {
		selected.Artifacts[p.Speculation.DraftArtifact] = d.Artifacts[p.Speculation.DraftArtifact]
	}
	for _, patch := range p.Patches {
		selected.Patches[patch] = d.Patches[patch]
	}
	selected = canonicalDocument(selected)
	locked := Lock{Schema: LockSchema, SourceSnapshotSHA256: snapshot, Preset: id,
		Target: target, Records: selected, ExecutionDigest: executionDigest(selected, id, target)}
	// Validate through the same typed renderer used by preparation.
	inputs, err := locked.projections()
	if err != nil {
		return Lock{}, err
	}
	if _, err = render.Build(render.Inputs{Manifest: inputs.Manifest, Lock: inputs.Artifacts, Mode: id, Root: "/temper-compile-validation"}); err != nil {
		return Lock{}, fmt.Errorf("compile runtime inputs: %w", err)
	}
	return locked, nil
}

func ParseLock(data []byte) (Lock, error) {
	var l Lock
	if err := decode(data, &l); err != nil {
		return Lock{}, err
	}
	if err := l.Validate(); err != nil {
		return Lock{}, err
	}
	return l, nil
}

func (l Lock) Validate() error {
	if l.Schema != LockSchema || !hashPattern.MatchString(l.SourceSnapshotSHA256) {
		return errors.New("execution lock requires its schema and exact source snapshot digest")
	}
	expected, err := CompilePreset(l.Records, l.Preset, "", 0, l.Target)
	if err != nil {
		return err
	}
	expected.SourceSnapshotSHA256 = l.SourceSnapshotSHA256
	if !reflect.DeepEqual(expected, l) {
		return errors.New("execution lock contains altered digests, noncanonical data, or unselected records")
	}
	return nil
}

func MarshalLock(l Lock) ([]byte, error) {
	if err := l.Validate(); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func canonicalDocument(d Document) Document {
	// Clone at the boundary: canonicalization must not mutate caller-owned maps,
	// slices or configuration pointers.
	raw, _ := json.Marshal(d)
	var result Document
	_ = json.Unmarshal(raw, &result)
	for id, a := range result.Artifacts {
		sort.Slice(a.Files, func(i, j int) bool { return a.Files[i].Path < a.Files[j].Path })
		result.Artifacts[id] = a
	}
	for id, p := range result.Patches {
		sort.Slice(p.Files, func(i, j int) bool { return p.Files[i].Path < p.Files[j].Path })
		slices.Sort(p.CompatibleArtifacts)
		result.Patches[id] = p
	}
	for id, l := range result.Presets {
		if l.Patches == nil {
			l.Patches = []string{}
		}
		result.Presets[id] = l
	}
	return result
}

func executionDigest(d Document, id string, target software.Target) string {
	router := d.Runtime.Router
	router.Source, router.Versions = nil, nil
	return digest(struct {
		Kind   string
		Target software.Target
		Router Supply
		Preset string
		Python []Supply `json:",omitempty"`
	}{"preset-execution/v1", target, router, presetExecutionDigest(d, d.Presets[id]), d.Runtime.PythonEnvironments})
}

func artifactMaterialDigest(a Artifact) string {
	return digest(struct {
		Kind   string
		Format string
		Files  []File
	}{"artifact-material/v1", a.Format, a.Files})
}

func patchMaterialDigest(p Patch) string {
	return digest(struct {
		Kind  string
		Files []File
	}{"patch-material/v1", p.Files})
}

func engineExecutionDigest(e Engine) string {
	e.DisplayName = ""
	e.Supply.Source, e.Supply.Versions = nil, nil
	return digest(e)
}

func presetExecutionDigest(d Document, l Preset) string {
	x := l
	x.DisplayName, x.EngineVersions = "", nil
	x.MemoryTier = ""
	x.Description, x.AssessmentURL, x.Recommended = "", "", false
	x.ContextLimitTokens, x.ContextFindings = 0, nil
	x.Artifact = artifactMaterialDigest(d.Artifacts[l.Artifact])
	if l.Speculation.DraftArtifact != "" {
		x.Speculation.DraftArtifact = artifactMaterialDigest(d.Artifacts[l.Speculation.DraftArtifact])
	}
	x.Engine = engineExecutionDigest(d.Engines[l.Engine])
	x.Patches = append([]string{}, l.Patches...)
	for i, patch := range l.Patches {
		x.Patches[i] = patchMaterialDigest(d.Patches[patch])
	}
	// This evidence hash has its own versioned definition. Preserve its field
	// names and kind when changing storage schemas, so measured contexts remain
	// attributable to the same execution inputs.
	return digest(struct {
		Kind   string
		Layout Preset
	}{"layout-execution/v1", x})
}

func digest(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic("validated catalog data is not JSON encodable: " + err.Error())
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Projections are derived inputs for Temper installation and rendering primitives.
// They are not independent authoring surfaces or part of portable identity.
type Projections struct {
	Manifest        manifest.Document
	Artifacts       lockfile.Document
	Software        softwarelock.Document
	RequestDefaults map[string]RequestDefaults
}

func (l Lock) Projections() (Projections, error) {
	if err := l.Validate(); err != nil {
		return Projections{}, err
	}
	return l.projections()
}

func (l Lock) projections() (Projections, error) {
	d := l.Records
	p := Projections{Manifest: manifest.Document{Schema: manifest.SchemaV2, Defaults: manifest.Defaults{TTL: 1800, GPUMemoryUtilization: .85}, Patches: map[string]manifest.Patch{}, Layouts: map[string]manifest.Layout{}, Modes: map[string]manifest.Mode{}, Tools: map[string]manifest.Tool{}},
		Artifacts: lockfile.Document{Schema: lockfile.SchemaV1, Entries: map[string]lockfile.Entry{}},
		Software: softwarelock.Document{Schema: softwarelock.SchemaV1, Target: l.Target, TargetMode: "compatible", Requires: []softwarelock.InstallationRequirement{},
			Provenance: softwarelock.Provenance{Execution: &softwarelock.ExecutionIdentity{Schema: LockSchema, Preset: l.Preset, SHA256: l.ExecutionDigest}}, Selections: map[string]softwarelock.Selection{}, Units: map[string]softwarelock.Unit{}},
		RequestDefaults: map[string]RequestDefaults{}}
	addSupply := func(s Supply) error {
		selection, units, err := s.installerInputs()
		if err != nil {
			return err
		}
		if prior, ok := p.Software.Selections[s.Package]; ok && !reflect.DeepEqual(prior, selection) {
			return fmt.Errorf("conflicting software selection %q", s.Package)
		}
		p.Software.Selections[s.Package] = selection
		for id, u := range units {
			if prior, ok := p.Software.Units[id]; ok && !reflect.DeepEqual(prior, u) {
				return fmt.Errorf("conflicting software unit %q", id)
			}
			p.Software.Units[id] = u
		}
		return nil
	}
	if err := addSupply(d.Runtime.Router); err != nil {
		return Projections{}, err
	}
	for _, supply := range d.Runtime.PythonEnvironments {
		if err := addSupply(supply); err != nil {
			return Projections{}, err
		}
	}
	for _, e := range d.Engines {
		if err := addSupply(e.Supply); err != nil {
			return Projections{}, err
		}
	}
	for id, patch := range d.Patches {
		p.Manifest.Patches[id] = manifest.Patch{Source: "hf://" + patch.Repo + "@" + patch.Revision + "/" + patch.Files[0].Path, File: patch.Files[0].Path}
	}
	for id, layout := range d.Presets {
		a := d.Artifacts[layout.Artifact]
		c := layout.EngineConfig
		m := manifest.Layout{DisplayName: layout.DisplayName, Model: manifest.Model{Repo: a.Repo, Files: []string{a.Files[0].Path}, Format: a.Format}, Engine: d.Engines[layout.Engine].Family, Interface: layout.Interface, Modalities: append([]string{}, layout.Modalities...), Window: layout.ContextWindowTokens, MaxTokens: layout.RequestDefaults.MaxOutputTokens, Thinking: layout.RequestDefaults.Reasoning,
			Speculation: &manifest.Speculation{Method: layout.Speculation.Method, MaxTokens: layout.Speculation.MaxDraftTokens}, Sampling: &layout.RequestDefaults.Sampling,
			Llama: &manifest.LlamaTuning{KV: c.KVCache, Parallel: c.Parallel, FlashAttention: c.FlashAttention, Batch: c.BatchTokens, UBatch: c.MicrobatchTokens, ContextCheckpoints: &c.ContextCheckpoints, PromptCacheRAMMiB: &c.PromptCacheRAMMiB, Controls: &c.Controls}}
		entry := lockfile.Entry{Repo: a.Repo, Revision: a.Revision, Resolved: d.Date, Files: []lockfile.File{{Name: a.Files[0].Path, SHA256: a.Files[0].SHA256}}}
		m.Model.Files, entry.Files = []string{}, []lockfile.File{}
		for _, f := range a.Files {
			m.Model.Files = append(m.Model.Files, f.Path)
			entry.Files = append(entry.Files, lockfile.File{Name: f.Path, SHA256: f.SHA256})
		}
		if c.Python != nil {
			m.Llama, m.Sampling = nil, nil
			m.RapidMLX, m.VLLMMetal = c.Python.RapidMLX, c.Python.VLLMMetal
		}
		if c.Splash != nil {
			m.Llama = nil
			m.Splash = &manifest.SplashTuning{SplashConfig: c.Splash.SplashConfig, SoftwareSHA256: d.Engines[layout.Engine].Supply.Release.Artifact.SHA256}
		}
		if layout.Speculation.DraftArtifact != "" {
			draft := d.Artifacts[layout.Speculation.DraftArtifact]
			m.Draft = &manifest.Model{Repo: draft.Repo, Format: draft.Format}
			entry.Draft = &lockfile.Draft{Repo: draft.Repo, Revision: draft.Revision}
			for _, file := range draft.Files {
				m.Draft.Files = append(m.Draft.Files, file.Path)
				entry.Draft.Files = append(entry.Draft.Files, lockfile.File{Name: file.Path, SHA256: file.SHA256})
			}
		}
		if len(layout.Patches) > 0 {
			patchID := layout.Patches[0]
			m.ChatTemplate = patchID
			entry.Patches = []lockfile.Patch{{Name: patchID, SHA256: d.Patches[patchID].Files[0].SHA256}}
		}
		p.Manifest.Layouts[id] = m
		p.Artifacts.Entries[id] = entry
		p.RequestDefaults[id] = layout.RequestDefaults
	}
	ttl := 1800
	c := d.Presets[l.Preset].EngineConfig
	member := manifest.Member{Layout: l.Preset, TTL: &ttl}
	if c.Kind == "llama-server/v2" {
		member.NGL = &c.GPULayers
	}
	p.Manifest.Modes[l.Preset] = manifest.Mode{ExternalForeground: true,
		Tools: []string{}, Harnesses: []string{}, Members: manifest.Members{OnDemand: []manifest.Member{member}}}

	if err := p.Manifest.Validate(); err != nil {
		return Projections{}, err
	}
	if err := p.Artifacts.Validate(); err != nil {
		return Projections{}, err
	}
	if err := p.Software.Validate(); err != nil {
		return Projections{}, err
	}
	return p, nil
}

// Files returns exact, inspectable inputs. Reading a lock later needs no catalog
// access, environment discovery, network, current working directory or clock.
func (p Projections) Files() (map[string][]byte, error) {
	m, err := yaml.Marshal(p.Manifest)
	if err != nil {
		return nil, err
	}
	a, err := lockfile.Marshal(p.Artifacts)
	if err != nil {
		return nil, err
	}
	s, err := softwarelock.Marshal(p.Software)
	if err != nil {
		return nil, err
	}
	r, err := json.MarshalIndent(p.RequestDefaults, "", "  ")
	if err != nil {
		return nil, err
	}
	return map[string][]byte{"manifest.yaml": m, "manifest.lock.yaml": a, "software.lock.yaml": s, "request-defaults.json": append(r, '\n')}, nil
}
