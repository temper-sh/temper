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

type Digests struct {
	Records   map[string]string `yaml:"records" json:"records"`
	Materials map[string]string `yaml:"materials" json:"materials"`
	Engines   map[string]string `yaml:"engines" json:"engines"`
	Layouts   map[string]string `yaml:"layouts" json:"layouts"`
	Profile   string            `yaml:"profile" json:"profile"`
}

type Lock struct {
	Schema               string          `yaml:"schema" json:"schema"`
	SourceSnapshotSHA256 string          `yaml:"source_snapshot_sha256" json:"source_snapshot_sha256"`
	Selection            Selection       `yaml:"selection" json:"selection"`
	Target               software.Target `yaml:"target" json:"target"`
	Records              Document        `yaml:"records" json:"records"`
	Digests              Digests         `yaml:"digests" json:"digests"`
}

// Compile accepts an explicitly supplied local snapshot. It makes no signature,
// publication, qualification or machine-fit claim and never rewrites selection.
func Compile(d Document, s Selection, target software.Target) (Lock, error) {
	if err := d.Validate(); err != nil {
		return Lock{}, err
	}
	if err := s.Validate(); err != nil {
		return Lock{}, err
	}
	if err := target.Validate(); err != nil {
		return Lock{}, err
	}
	if target.OS != "darwin" || target.Arch != "arm64" || target.Distribution != "" || target.DistributionVersion != "" {
		return Lock{}, errors.New("execution target is portable darwin/arm64 compatibility, not an observed OS version")
	}
	profile, ok := d.Profiles[s.Profile]
	if !ok {
		return Lock{}, fmt.Errorf("unknown selected profile %q", s.Profile)
	}
	d = canonicalDocument(d)
	snapshot := digest(d)
	s.Tools = []string{}
	s.Integrations = []string{}
	selected := Document{Schema: Schema, Date: d.Date, Runtime: d.Runtime, Artifacts: map[string]Artifact{}, Patches: map[string]Patch{}, Engines: map[string]Engine{}, Layouts: map[string]Layout{}, Profiles: map[string]Profile{s.Profile: profile}}
	for _, b := range profile.Bindings {
		l := d.Layouts[b.Layout]
		selected.Layouts[b.Layout] = l
		selected.Artifacts[l.Artifact] = d.Artifacts[l.Artifact]
		selected.Engines[l.Engine] = d.Engines[l.Engine]
		if !d.Engines[l.Engine].Supply.Target.Matches(target) {
			return Lock{}, fmt.Errorf("layout %q engine is incompatible with target", b.Layout)
		}
		for _, id := range l.Patches {
			selected.Patches[id] = d.Patches[id]
		}
	}
	if !d.Runtime.Router.Target.Matches(target) {
		return Lock{}, errors.New("router is incompatible with target")
	}
	selected = canonicalDocument(selected)
	locked := Lock{Schema: LockSchema, SourceSnapshotSHA256: snapshot, Selection: s, Target: target, Records: selected}
	locked.Digests = deriveDigests(selected, s, target)
	// Prove the exact selected graph reaches the current typed renderer before
	// returning an executable lock. No caller-supplied shell or local path enters it.
	projections, err := locked.projections()
	if err != nil {
		return Lock{}, err
	}
	if _, err = render.Build(render.Inputs{Manifest: projections.Manifest, Lock: projections.Artifacts, Mode: s.Profile, Root: "/temper-compile-validation"}); err != nil {
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
	expected, err := Compile(l.Records, l.Selection, l.Target)
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
	// slices, selection, or configuration pointers.
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
	for id, l := range result.Layouts {
		if l.Patches == nil {
			l.Patches = []string{}
		}
		result.Layouts[id] = l
	}
	return result
}

func deriveDigests(d Document, s Selection, target software.Target) Digests {
	digests := Digests{Records: map[string]string{}, Materials: map[string]string{}, Engines: map[string]string{}, Layouts: map[string]string{}}
	for id, a := range d.Artifacts {
		digests.Records["artifact/"+id] = digest(a)
		digests.Materials["artifact/"+id] = digest(struct {
			Kind   string
			Format string
			Files  []File
		}{"artifact-material/v1", a.Format, a.Files})
	}
	for id, p := range d.Patches {
		digests.Records["patch/"+id] = digest(p)
		digests.Materials["patch/"+id] = digest(struct {
			Kind  string
			Files []File
		}{"patch-material/v1", p.Files})
	}
	for id, e := range d.Engines {
		digests.Records["engine/"+id] = digest(e)
		digests.Engines[id] = digest(e)
	}
	for id, l := range d.Layouts {
		digests.Records["layout/"+id] = digest(l)
		x := l
		x.DisplayName = ""
		x.Artifact = digests.Materials["artifact/"+l.Artifact]
		x.Engine = digests.Engines[l.Engine]
		x.Patches = append([]string{}, l.Patches...)
		for i, p := range l.Patches {
			x.Patches[i] = digests.Materials["patch/"+p]
		}
		digests.Layouts[id] = digest(struct {
			Kind   string
			Layout Layout
		}{"layout-execution/v1", x})
	}
	p := d.Profiles[s.Profile]
	digests.Records["profile/"+s.Profile] = digest(p)
	p.Bindings = append([]Binding{}, p.Bindings...)
	for i, b := range p.Bindings {
		p.Bindings[i].Layout = digests.Layouts[b.Layout]
	}
	digests.Profile = digest(struct {
		Kind    string
		Target  software.Target
		Router  Supply
		Profile Profile
	}{"profile-execution/v1", target, d.Runtime.Router, p})
	return digests
}

func digest(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic("validated catalog data is not JSON encodable: " + err.Error())
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Projections are derived compatibility inputs for existing Temper primitives.
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
	profile := d.Profiles[l.Selection.Profile]
	p := Projections{Manifest: manifest.Document{Schema: manifest.SchemaV2, Defaults: manifest.Defaults{TTL: 1800, GPUMemoryUtilization: profile.GPUMemoryUtilization}, Patches: map[string]manifest.Patch{}, Layouts: map[string]manifest.Layout{}, Modes: map[string]manifest.Mode{}, Tools: map[string]manifest.Tool{}},
		Artifacts: lockfile.Document{Schema: lockfile.SchemaV1, Entries: map[string]lockfile.Entry{}},
		Software: softwarelock.Document{Schema: softwarelock.SchemaV1, Target: l.Target, TargetMode: "compatible", Resolved: d.Date, Requires: []softwarelock.InstallationRequirement{},
			Provenance: softwarelock.Provenance{Experiment: &softwarelock.ExperimentIdentity{Schema: LockSchema, ID: l.Selection.Profile, DefinitionSHA256: l.Digests.Profile}}, Selections: map[string]softwarelock.Selection{}, Units: map[string]softwarelock.Unit{}},
		RequestDefaults: map[string]RequestDefaults{}}
	addSupply := func(s Supply) error {
		if prior, ok := p.Software.Selections[s.Package]; ok && !reflect.DeepEqual(prior, s.Selection) {
			return fmt.Errorf("conflicting software selection %q", s.Package)
		}
		p.Software.Selections[s.Package] = s.Selection
		for id, u := range s.Units {
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
	for _, e := range d.Engines {
		if err := addSupply(e.Supply); err != nil {
			return Projections{}, err
		}
	}
	for id, patch := range d.Patches {
		p.Manifest.Patches[id] = manifest.Patch{Source: "hf://" + patch.Repo + "@" + patch.Revision + "/" + patch.Files[0].Path, File: patch.Files[0].Path}
	}
	for id, layout := range d.Layouts {
		a := d.Artifacts[layout.Artifact]
		c := layout.EngineConfig
		m := manifest.Layout{DisplayName: layout.DisplayName, Model: manifest.Model{Repo: a.Repo, Files: []string{a.Files[0].Path}, Format: a.Format}, Engine: d.Engines[layout.Engine].Family, Interface: layout.Interface, Modalities: append([]string{}, layout.Modalities...), Window: layout.ContextWindowTokens, MaxTokens: layout.RequestDefaults.MaxOutputTokens, Thinking: layout.RequestDefaults.Reasoning,
			Speculation: &manifest.Speculation{Method: layout.Speculation.Method, MaxTokens: layout.Speculation.MaxDraftTokens}, Sampling: &layout.RequestDefaults.Sampling,
			Llama: &manifest.LlamaTuning{KV: c.KVCache, Parallel: c.Parallel, FlashAttention: c.FlashAttention, Batch: c.BatchTokens, UBatch: c.MicrobatchTokens, ContextCheckpoints: &c.ContextCheckpoints, PromptCacheRAMMiB: &c.PromptCacheRAMMiB, Controls: &c.Controls}}
		entry := lockfile.Entry{Repo: a.Repo, Revision: a.Revision, Resolved: d.Date, Files: []lockfile.File{{Name: a.Files[0].Path, SHA256: a.Files[0].SHA256}}}
		if len(layout.Patches) > 0 {
			patchID := layout.Patches[0]
			m.ChatTemplate = patchID
			entry.Patches = []lockfile.Patch{{Name: patchID, SHA256: d.Patches[patchID].Files[0].SHA256}}
		}
		p.Manifest.Layouts[id] = m
		p.Artifacts.Entries[id] = entry
		p.RequestDefaults[id] = layout.RequestDefaults
	}
	mode := manifest.Mode{Tools: []string{}, Harnesses: []string{}}
	for _, b := range profile.Bindings {
		c := d.Layouts[b.Layout].EngineConfig
		member := manifest.Member{Layout: b.Layout, TTL: &b.IdleTTLSeconds, NGL: &c.GPULayers, Preload: b.Preload}
		if b.Route == "default" {
			mode.Foreground = b.Layout
		}
		if b.Residency == "resident" {
			mode.Members.Resident = append(mode.Members.Resident, member)
		} else {
			mode.Members.OnDemand = append(mode.Members.OnDemand, member)
		}
	}
	p.Manifest.Modes[l.Selection.Profile] = mode
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
