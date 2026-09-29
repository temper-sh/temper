package catalog

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter/upstreamrelease"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
)

// Versions belongs to a preset (model/features on its engine target), or to
// the runtime router. Tested is evidence, never an inferred compatibility floor.
type Versions struct {
	MinimumRequired string `yaml:"minimum_required,omitempty" json:"minimum_required,omitempty"`
	RequiredSource  string `yaml:"required_source,omitempty" json:"required_source,omitempty"`
	MinimumTested   string `yaml:"minimum_tested,omitempty" json:"minimum_tested,omitempty"`
	TestedEvidence  string `yaml:"tested_evidence,omitempty" json:"tested_evidence,omitempty"`
}

func (v *Versions) validate() error {
	if v == nil {
		return nil
	}
	if (v.MinimumRequired == "") != (strings.TrimSpace(v.RequiredSource) == "") || (v.MinimumTested == "") != (strings.TrimSpace(v.TestedEvidence) == "") {
		return errors.New("required and tested versions each need their own source or evidence reference")
	}
	for _, value := range []string{v.MinimumRequired, v.MinimumTested} {
		if value == "" {
			continue
		}
		if _, err := upstreamrelease.CompareVersions(value, value); err != nil {
			return err
		}
	}
	return nil
}

func (v *Versions) require(selected string) error {
	if v == nil || v.MinimumRequired == "" {
		return nil
	}
	order, err := upstreamrelease.CompareVersions(selected, v.MinimumRequired)
	if err != nil {
		return err
	}
	if order < 0 {
		return fmt.Errorf("release %s is below minimum required %s (%s)", selected, v.MinimumRequired, v.RequiredSource)
	}
	return nil
}

func (s Supply) validateSource() error {
	if s.Python != nil {
		_, _, err := s.pythonInputs()
		return err
	}
	if !idPattern.MatchString(s.Package) || s.Source == nil {
		return errors.New("supply requires package, target and source; installer selection and units are derived")
	}
	if err := s.Source.Validate(); err != nil {
		return err
	}
	if err := s.Versions.validate(); err != nil {
		return err
	}
	if s.Release == nil {
		return nil
	} // Moving sources are resolved before compilation.
	r := s.Release
	if _, err := upstreamrelease.CompareVersions(r.Version, r.Version); err != nil {
		return err
	}
	if !revisionPattern.MatchString(r.Revision) {
		return errors.New("resolved release needs its exact commit")
	}
	a := r.Artifact
	locator, err := url.Parse(a.Locator)
	if err != nil || locator.Scheme != "https" || locator.Host == "" || locator.User != nil || locator.Fragment != "" || !hashPattern.MatchString(a.SHA256) || a.Size <= 0 || a.UnpackedSize <= 0 || a.InstalledEntries <= 0 || a.Format != "tar.gz" || (a.ArchiveRoot != "." && !safePath(a.ArchiveRoot)) {
		return errors.New("resolved release needs an exact HTTPS tar.gz, checksum, sizes, entry count and safe root")
	}
	return nil
}

func (s Supply) installerInputs() (softwarelock.Selection, map[string]softwarelock.Unit, error) {
	if s.Python != nil {
		return s.pythonInputs()
	}
	if s.Release == nil {
		return softwarelock.Selection{}, nil, fmt.Errorf("software %q is unresolved; compile with --software latest or tested", s.Package)
	}
	if err := s.Versions.require(s.Release.Version); err != nil {
		return softwarelock.Selection{}, nil, err
	}
	id := "upstream-release:" + s.Package
	selection := softwarelock.Selection{Provenance: softwarelock.ProvenanceExecution, Method: "release-artifact", Adapter: "upstream-release", RecipeRevision: "release-archive/v1", RootUnit: id}
	units := map[string]softwarelock.Unit{id: {Adapter: "upstream-release", Scope: s.Package, NativeName: s.Package, Version: s.Release.Version, Revision: s.Release.Revision, Dependencies: []string{}, Artifacts: []software.Artifact{s.Release.Artifact}}}
	return selection, units, nil
}

// ValidateTestedSoftware checks the selected preset's explicit tested fallback
// and required floor. It does not read upstream metadata or prove availability.
func ValidateTestedSoftware(d Document, id string) error {
	if err := d.Validate(); err != nil {
		return err
	}
	p, ok := d.Presets[id]
	if !ok {
		return fmt.Errorf("unknown preset %q", id)
	}
	if _, err := testedVersion(d.Runtime.Router, d.Runtime.Router.Versions); err != nil {
		return err
	}
	_, err := testedVersion(d.Engines[p.Engine].Supply, p.EngineVersions)
	return err
}

func testedVersion(supply Supply, rule *Versions) (string, error) {
	if rule == nil || rule.MinimumTested == "" {
		return "", fmt.Errorf("software %q has no tested version for this preset", supply.Package)
	}
	if err := rule.require(rule.MinimumTested); err != nil {
		return "", err
	}
	return rule.MinimumTested, nil
}

// ResolveSoftware reads only the selected preset's sources. The caller chooses
// recorded inputs, latest available releases, or an explicit tested fallback; a
// failed latest lookup never silently changes that choice.
func ResolveSoftware(ctx context.Context, d Document, id string, choice string, reader upstreamrelease.ArtifactReader) (Document, error) {
	if err := d.Validate(); err != nil {
		return Document{}, err
	}
	if choice != "recorded" && choice != "latest" && choice != "tested" {
		return Document{}, errors.New("software choice must be recorded, latest or tested")
	}
	p, ok := d.Presets[id]
	if !ok {
		return Document{}, fmt.Errorf("unknown preset %q", id)
	}
	if choice == "recorded" {
		return d, nil
	}
	d = canonicalDocument(d)
	resolve := func(supply Supply, rule *Versions) (Supply, error) {
		if supply.Python != nil {
			return Supply{}, fmt.Errorf("software %q is an exact Python closure; use recorded software and resolve a new closure explicitly", supply.Package)
		}
		requested := "latest"
		if choice == "tested" {
			var err error
			requested, err = testedVersion(supply, rule)
			if err != nil {
				return Supply{}, err
			}
		}
		if supply.Release == nil || supply.Release.Version != requested {
			release, err := upstreamrelease.Discover(ctx, reader, *supply.Source, requested)
			if err != nil {
				return Supply{}, fmt.Errorf("resolve %s: %w", supply.Package, err)
			}
			supply.Release = &release
		}
		if err := rule.require(supply.Release.Version); err != nil {
			return Supply{}, err
		}
		return supply, nil
	}
	var err error
	d.Runtime.Router, err = resolve(d.Runtime.Router, d.Runtime.Router.Versions)
	if err != nil {
		return Document{}, err
	}
	e := d.Engines[p.Engine]
	e.Supply, err = resolve(e.Supply, p.EngineVersions)
	if err != nil {
		return Document{}, err
	}
	d.Engines[p.Engine] = e
	return d, nil
}
