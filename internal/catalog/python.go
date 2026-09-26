package catalog

import (
	"errors"
	"fmt"
	"sort"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter/uv"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
)

// PythonSupply is one resolved Python environment. No dependency
// resolution occurs while installing an execution lock.
type PythonSupply struct {
	Root     string          `yaml:"root" json:"root"`
	Runtime  PythonPackage   `yaml:"runtime" json:"runtime"`
	Packages []PythonPackage `yaml:"packages" json:"packages"`
}

type PythonPackage struct {
	Name     string            `yaml:"name" json:"name"`
	Version  string            `yaml:"version" json:"version"`
	Revision string            `yaml:"revision,omitempty" json:"revision,omitempty"`
	Artifact software.Artifact `yaml:"artifact" json:"artifact"`
}

func (s Supply) pythonInputs() (softwarelock.Selection, map[string]softwarelock.Unit, error) {
	p := s.Python
	if p == nil || s.Source != nil || s.Release != nil || s.Selection != nil || s.Units != nil || s.Versions != nil || !idPattern.MatchString(s.Package) {
		return softwarelock.Selection{}, nil, errors.New("Python supply requires one exact runtime/wheel closure and no release or legacy fields")
	}
	unitID := func(name string) string { return "uv:" + s.Package + ":" + name }
	units := map[string]softwarelock.Unit{}
	add := func(pkg PythonPackage, dependencies []string) error {
		id := unitID(pkg.Name)
		if _, exists := units[id]; exists {
			return fmt.Errorf("Python supply repeats %q", pkg.Name)
		}
		units[id] = softwarelock.Unit{Adapter: "uv", Scope: s.Package, NativeName: pkg.Name, Version: pkg.Version, Revision: pkg.Revision, Dependencies: dependencies, Artifacts: []software.Artifact{pkg.Artifact}}
		return nil
	}
	if p.Runtime.Name != "cpython" {
		return softwarelock.Selection{}, nil, errors.New("Python supply runtime must be cpython")
	}
	if err := add(p.Runtime, []string{}); err != nil {
		return softwarelock.Selection{}, nil, err
	}
	for _, wheel := range p.Packages {
		if err := add(wheel, []string{unitID("cpython")}); err != nil {
			return softwarelock.Selection{}, nil, err
		}
	}
	rootID := unitID(p.Root)
	root, exists := units[rootID]
	if !exists || p.Root == "cpython" {
		return softwarelock.Selection{}, nil, errors.New("Python supply needs a wheel root")
	}
	root.Dependencies = []string{}
	for id := range units {
		if id != rootID {
			root.Dependencies = append(root.Dependencies, id)
		}
	}
	sort.Strings(root.Dependencies)
	units[rootID] = root
	if err := uv.ValidateClosure(s.Target, units); err != nil {
		return softwarelock.Selection{}, nil, err
	}
	return softwarelock.Selection{Provenance: softwarelock.ProvenanceExecution, Method: "python-environment", Adapter: "uv", RecipeRevision: "python-wheels/v1", RootUnit: rootID}, units, nil
}
