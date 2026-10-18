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

	"github.com/temper-sh/temper/internal/buildinfo"
	"github.com/temper-sh/temper/internal/datadir"
	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/render"
	"github.com/temper-sh/temper/internal/software"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
)

// Lock is one self-contained compiled preset: the exact records it selects.
// It stores no fingerprint; SameExecution compares locks inside one Temper.
type Lock struct {
	Schema  string          `yaml:"schema" json:"schema"`
	Preset  string          `yaml:"preset" json:"preset"`
	Target  software.Target `yaml:"target" json:"target"`
	Records Document        `yaml:"records" json:"records"`
}

// CompilePreset freezes one explicitly selected preset with the profile a Mac
// of memoryGiB would use; zero selects the smallest profile. A nonzero window
// replaces the profile's window within the preset's bounds. Compilation is
// pure and makes no publication or machine-fit claim.
func CompilePreset(d Document, id string, memoryGiB, window int, target software.Target) (Lock, error) {
	if err := d.validateRecords(); err != nil {
		return Lock{}, err
	}
	if err := validateTarget(target); err != nil {
		return Lock{}, err
	}
	if _, ok := d.Presets[id]; !ok {
		return Lock{}, fmt.Errorf("unknown preset %q", id)
	}
	d = canonicalDocument(d)
	p := d.Presets[id]
	profile := p.SelectProfile(memoryGiB)
	if window != 0 {
		limit := p.ContextLimitTokens
		if limit == 0 {
			limit = profile.ContextWindowTokens
		}
		if window <= 0 || window > limit {
			return Lock{}, fmt.Errorf("preset %q context must be between 1 and %d tokens (total input and output)", id, limit)
		}
		profile.ContextWindowTokens = window
	}
	p.Profiles = []Profile{profile}
	e := d.Engines[p.Engine]
	selected := Document{Schema: Schema, Date: d.Date, Platform: d.Platform, Runtime: d.Runtime,
		Artifacts: map[string]Artifact{p.Artifact: d.Artifacts[p.Artifact]},
		Templates: map[string]Template{}, Engines: map[string]Engine{p.Engine: e},
		Presets: map[string]Preset{id: p}}
	if p.Speculation.DraftArtifact != "" {
		selected.Artifacts[p.Speculation.DraftArtifact] = d.Artifacts[p.Speculation.DraftArtifact]
	}
	if p.ProjectorArtifact != "" {
		selected.Artifacts[p.ProjectorArtifact] = d.Artifacts[p.ProjectorArtifact]
	}
	if p.patched() {
		selected.Templates[p.Template] = d.Templates[p.Template]
	}
	selected = canonicalDocument(selected)
	locked := Lock{Schema: LockSchema, Preset: id, Target: target, Records: selected}
	if err := locked.Validate(); err != nil {
		return Lock{}, err
	}
	// Validate through the same typed renderer used by preparation.
	inputs, err := locked.projections()
	if err != nil {
		return Lock{}, err
	}
	if _, err = render.Build(render.Inputs{Manifest: inputs.Manifest, Lock: inputs.Artifacts, Mode: id, Root: "/temper-compile-validation", StartPort: datadir.DefaultStartPort}); err != nil {
		return Lock{}, fmt.Errorf("compile runtime inputs: %w", err)
	}
	return locked, nil
}

func ParseLock(data []byte) (Lock, error) {
	var l Lock
	if err := decode(data, &l); err != nil {
		return Lock{}, err
	}
	l.Records.normalize()
	if err := l.Validate(); err != nil {
		return Lock{}, err
	}
	return l, nil
}

// Validate checks structure only: the schema, a portable target, valid
// records, resolved software and exactly the closure its preset selects. It
// never recompiles, so a later Temper reads an earlier lock unchanged.
func (l Lock) Validate() error {
	if l.Schema != LockSchema {
		return fmt.Errorf("execution lock schema is %q, want %q", l.Schema, LockSchema)
	}
	if err := validateTarget(l.Target); err != nil {
		return err
	}
	if err := l.Records.validateRecords(); err != nil {
		return err
	}
	d := l.Records
	p, ok := d.Presets[l.Preset]
	if !ok || len(d.Presets) != 1 || len(p.Profiles) != 1 {
		return fmt.Errorf("execution lock must contain exactly its preset %q with one profile", l.Preset)
	}
	artifacts := map[string]bool{p.Artifact: true}
	if p.Speculation.DraftArtifact != "" {
		artifacts[p.Speculation.DraftArtifact] = true
	}
	if p.ProjectorArtifact != "" {
		artifacts[p.ProjectorArtifact] = true
	}
	templates := map[string]bool{}
	if p.patched() {
		templates[p.Template] = true
	}
	if !sameKeys(d.Artifacts, artifacts) || !sameKeys(d.Templates, templates) || !sameKeys(d.Engines, map[string]bool{p.Engine: true}) || d.Presentation != nil {
		return errors.New("execution lock records must be exactly its preset's closure")
	}
	e := d.Engines[p.Engine]
	if e.Supply.Release == nil && e.Supply.Python == nil || d.Runtime.Router.Release == nil {
		return errors.New("execution lock requires resolved software")
	}
	for _, minimum := range []string{d.Runtime.Router.MinimumTemper, e.MinimumTemper} {
		reason, err := needsTemper(minimum, buildinfo.Version)
		if err != nil {
			return err
		}
		if reason != "" {
			return fmt.Errorf("execution lock %s", reason)
		}
	}
	if err := allowedRelease(e.Supply.Release, e.ExcludeVersions, p.ExcludeEngineVersions); err != nil {
		return fmt.Errorf("preset %q engine: %w", l.Preset, err)
	}
	if !e.Supply.Target.Matches(l.Target) || !d.Runtime.Router.Target.Matches(l.Target) {
		return errors.New("preset engine or router is incompatible with target")
	}
	return nil
}

func validateTarget(target software.Target) error {
	if err := target.Validate(); err != nil {
		return err
	}
	if target.OS != "darwin" || target.Arch != "arm64" || target.Distribution != "" || target.DistributionVersion != "" {
		return errors.New("execution target is portable darwin/arm64 compatibility, not an observed OS version")
	}
	return nil
}

func sameKeys[V any](records map[string]V, want map[string]bool) bool {
	if len(records) != len(want) {
		return false
	}
	for id := range records {
		if !want[id] {
			return false
		}
	}
	return true
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
	for id, t := range result.Templates {
		sort.Slice(t.Files, func(i, j int) bool { return t.Files[i].Path < t.Files[j].Path })
		slices.Sort(t.CompatibleArtifacts)
		result.Templates[id] = t
	}
	return result
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
	p := Projections{Manifest: manifest.Document{Schema: manifest.SchemaV1, Defaults: manifest.Defaults{TTL: 1800, GPUMemoryUtilization: .85}, Patches: map[string]manifest.Patch{}, Layouts: map[string]manifest.Layout{}, Modes: map[string]manifest.Mode{}},
		Artifacts: lockfile.Document{Schema: lockfile.SchemaV1, Entries: map[string]lockfile.Entry{}},
		Software: softwarelock.Document{Schema: softwarelock.SchemaV1, Target: l.Target, Requires: []softwarelock.InstallationRequirement{},
			Provenance: softwarelock.Provenance{Execution: &softwarelock.ExecutionIdentity{Schema: LockSchema, Preset: l.Preset}}, Selections: map[string]softwarelock.Selection{}, Units: map[string]softwarelock.Unit{}},
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
	if err := addSupply(d.Runtime.Router.Supply); err != nil {
		return Projections{}, err
	}
	for _, e := range d.Engines {
		if err := addSupply(e.Supply); err != nil {
			return Projections{}, err
		}
	}
	for id, t := range d.Templates {
		p.Manifest.Patches[id] = manifest.Patch{Source: t.source(), File: t.Files[0].Path}
	}
	for id, layout := range d.Presets {
		a := d.Artifacts[layout.Artifact]
		hf := a.HuggingFace()
		c := layout.Config()
		name := layout.Presentation.DisplayName
		if name == "" {
			name = id
		}
		m := manifest.Layout{DisplayName: name, Model: manifest.Model{Repo: hf.Repo, Files: []string{a.Files[0].Path}, Format: a.Format}, Engine: d.Engines[layout.Engine].Family(), Interface: layout.Interface, Modalities: append([]string{}, layout.Modalities...), Window: layout.Window(), MaxTokens: layout.RequestDefaults.MaxOutputTokens, Thinking: layout.RequestDefaults.Reasoning,
			Speculation: &manifest.Speculation{Method: layout.Speculation.Method, MaxTokens: layout.Speculation.MaxDraftTokens}, Sampling: &layout.RequestDefaults.Sampling,
			Llama: &manifest.LlamaTuning{KV: c.KVCache, Parallel: c.Parallel, FlashAttention: c.FlashAttention, Batch: c.BatchTokens, UBatch: c.MicrobatchTokens, ContextCheckpoints: &c.ContextCheckpoints, PromptCacheRAMMiB: &c.PromptCacheRAMMiB, Controls: &c.Controls}}
		entry := lockfile.Entry{Repo: hf.Repo, Revision: hf.Revision, Resolved: d.Date, Files: []lockfile.File{{Name: a.Files[0].Path, SHA256: a.Files[0].SHA256}}}
		m.Model.Files, entry.Files = []string{}, []lockfile.File{}
		for _, f := range a.Files {
			m.Model.Files = append(m.Model.Files, f.Path)
			entry.Files = append(entry.Files, lockfile.File{Name: f.Path, SHA256: f.SHA256})
		}
		if c.Python != nil {
			m.Llama, m.Sampling = nil, nil
			m.RapidMLX, m.VLLMMetal = c.Python.RapidMLX, c.Python.VLLMMetal
		}
		if c.CrispEmbed != nil {
			m.Llama, m.Sampling = nil, nil
			m.CrispEmbed = &c.CrispEmbed.CrispEmbedTuning
		}
		if layout.Interface == "reranking" {
			m.Sampling = nil
		}
		if layout.ProjectorArtifact != "" {
			projector := d.Artifacts[layout.ProjectorArtifact]
			source := projector.HuggingFace()
			m.Projector = &manifest.Model{Repo: source.Repo, Format: projector.Format, Files: []string{projector.Files[0].Path}}
			entry.Projector = &lockfile.Model{Repo: source.Repo, Revision: source.Revision, Files: []lockfile.File{{Name: projector.Files[0].Path, SHA256: projector.Files[0].SHA256}}}
		}
		if c.Splash != nil {
			m.Llama = nil
			m.Splash = &manifest.SplashTuning{SplashConfig: c.Splash.SplashConfig, SoftwareSHA256: d.Engines[layout.Engine].Supply.Release.Artifact.SHA256}
			if c.Kind != "splash/v1" {
				m.Splash.Adapter = c.Kind
			}
		}
		if layout.Speculation.DraftArtifact != "" {
			draft := d.Artifacts[layout.Speculation.DraftArtifact]
			source := draft.HuggingFace()
			m.Draft = &manifest.Model{Repo: source.Repo, Format: draft.Format}
			entry.Draft = &lockfile.Model{Repo: source.Repo, Revision: source.Revision}
			for _, file := range draft.Files {
				m.Draft.Files = append(m.Draft.Files, file.Path)
				entry.Draft.Files = append(entry.Draft.Files, lockfile.File{Name: file.Path, SHA256: file.SHA256})
			}
		}
		if layout.patched() {
			m.ChatTemplate = layout.Template
			entry.Patches = []lockfile.Patch{{Name: layout.Template, SHA256: d.Templates[layout.Template].Files[0].SHA256}}
		}
		p.Manifest.Layouts[id] = m
		p.Artifacts.Entries[id] = entry
		p.RequestDefaults[id] = layout.RequestDefaults
	}
	ttl := 1800
	selected := d.Presets[l.Preset]
	member := manifest.Member{Layout: l.Preset, TTL: &ttl}
	if selected.Config().Kind == "llama-server/v1" {
		ngl := selected.gpuOffloadLayers(selected.Config())
		member.NGL = &ngl
	}
	p.Manifest.Modes[l.Preset] = manifest.Mode{ExternalForeground: true,
		Members: manifest.Members{OnDemand: []manifest.Member{member}}}

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
