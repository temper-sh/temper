// Package catalog parses and validates independently published software-supply
// snapshots. It contains policy and evidence, never installed state.
package catalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/version"
	"gopkg.in/yaml.v3"
)

const (
	SchemaV1          = "temper-software-supply/v1"
	AdapterProtocolV1 = "temper-installer-adapter/v1"
)

var (
	idPattern       = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	revisionPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._/-][a-z0-9]+)*$`)
	sha256Pattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Document struct {
	Schema         string             `yaml:"schema"`
	Sequence       uint64             `yaml:"sequence"`
	PublishedAt    string             `yaml:"published_at"`
	Methods        map[string]Method  `yaml:"methods"`
	Adapters       map[string]Adapter `yaml:"adapters"`
	TargetBindings []TargetBinding    `yaml:"target_bindings"`
	Packages       map[string]Package `yaml:"packages"`
}

type Method struct {
	Description string `yaml:"description"`
}

type Adapter struct {
	Method      string `yaml:"method"`
	Protocol    string `yaml:"protocol"`
	EffectModel string `yaml:"effect_model"`
}

type TargetBinding struct {
	Method  string          `yaml:"method"`
	Target  software.Target `yaml:"target"`
	Adapter string          `yaml:"adapter"`
}

type Package struct {
	Description string            `yaml:"description"`
	Recipes     map[string]Recipe `yaml:"recipes"`
}

type Recipe struct {
	Method         string       `yaml:"method"`
	RecipeRevision string       `yaml:"recipe_revision"`
	Source         Source       `yaml:"source"`
	VersionScheme  string       `yaml:"version_scheme"`
	Selection      Selection    `yaml:"selection"`
	Dependencies   []Dependency `yaml:"dependencies"`
	Exclude        []string     `yaml:"exclude"`
	Gates          []string     `yaml:"gates"`
	Tested         []Tested     `yaml:"tested"`
}

type Source struct {
	Kind         string `yaml:"kind"`
	Tap          string `yaml:"tap,omitempty"`
	Formula      string `yaml:"formula,omitempty"`
	Index        string `yaml:"index,omitempty"`
	Distribution string `yaml:"distribution,omitempty"`
}

func (s Source) NativeName() string {
	switch s.Kind {
	case "homebrew-formula":
		return s.Formula
	case "python-index":
		return s.Distribution
	default:
		return ""
	}
}

type Selection struct {
	Policy            string `yaml:"policy"`
	MinimumCompatible string `yaml:"minimum_compatible,omitempty"`
	Constraint        string `yaml:"constraint,omitempty"`
	Exact             string `yaml:"exact,omitempty"`
	Revision          string `yaml:"revision,omitempty"`
}

type Dependency struct {
	Package    string `yaml:"package"`
	Constraint string `yaml:"constraint"`
}

type Tested struct {
	RootVersion   string          `yaml:"root_version"`
	ClosureDigest string          `yaml:"closure_digest"`
	Target        software.Target `yaml:"target"`
	Evidence      string          `yaml:"evidence"`
}

type ValidationError struct {
	Problems []string
}

// Snapshot keeps a validated catalog document bound to the digest of the
// exact published bytes from which it was parsed.
type Snapshot struct {
	Document Document
	SHA256   string
}

func (e *ValidationError) Error() string {
	return "software catalog invalid: " + strings.Join(e.Problems, "; ")
}

func Parse(data []byte) (Document, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	var document Document
	if err := decoder.Decode(&document); err != nil {
		return Document{}, fmt.Errorf("decode software catalog: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Document{}, errors.New("decode software catalog: multiple YAML documents are not allowed")
		}
		return Document{}, fmt.Errorf("decode software catalog: %w", err)
	}
	if err := document.Validate(); err != nil {
		return Document{}, err
	}
	return document, nil
}

func ParseSnapshot(data []byte) (Snapshot, error) {
	document, err := Parse(data)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Document: document, SHA256: SnapshotDigest(data)}, nil
}

func (s Snapshot) Validate() error {
	if err := s.Document.Validate(); err != nil {
		return err
	}
	if !sha256Pattern.MatchString(s.SHA256) {
		return errors.New("software catalog snapshot sha256 must be 64 lowercase hexadecimal characters")
	}
	return nil
}

func SnapshotDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (d Document) Validate() error {
	var problems []string
	problem := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	if d.Schema != SchemaV1 {
		problem("schema is %q, want %q", d.Schema, SchemaV1)
	}
	if d.Sequence == 0 {
		problem("sequence must be greater than zero")
	}
	if _, err := time.Parse(time.RFC3339, d.PublishedAt); err != nil {
		problem("published_at %q must be RFC 3339", d.PublishedAt)
	}
	if len(d.Methods) == 0 {
		problem("methods must not be empty")
	}
	if len(d.Adapters) == 0 {
		problem("adapters must not be empty")
	}
	if len(d.TargetBindings) == 0 {
		problem("target_bindings must not be empty")
	}
	if len(d.Packages) == 0 {
		problem("packages must not be empty")
	}

	for _, id := range sortedKeys(d.Methods) {
		method := d.Methods[id]
		if !idPattern.MatchString(id) {
			problem("method id %q is not a lowercase stable id", id)
		}
		if strings.TrimSpace(method.Description) == "" {
			problem("method %q description is required", id)
		}
	}

	for _, id := range sortedKeys(d.Adapters) {
		adapter := d.Adapters[id]
		if !idPattern.MatchString(id) {
			problem("adapter id %q is not a lowercase stable id", id)
		}
		if _, ok := d.Methods[adapter.Method]; !ok {
			problem("adapter %q references unknown method %q", id, adapter.Method)
		}
		if adapter.Protocol != AdapterProtocolV1 {
			problem("adapter %q protocol is %q, want %q", id, adapter.Protocol, AdapterProtocolV1)
		}
		if adapter.EffectModel != "shared" && adapter.EffectModel != "isolated" {
			problem("adapter %q effect_model %q must be shared or isolated", id, adapter.EffectModel)
		}
	}

	for index, binding := range d.TargetBindings {
		location := fmt.Sprintf("target_bindings[%d]", index)
		if _, ok := d.Methods[binding.Method]; !ok {
			problem("%s references unknown method %q", location, binding.Method)
		}
		if err := binding.Target.Validate(); err != nil {
			problem("%s target: %v", location, err)
		}
		adapter, ok := d.Adapters[binding.Adapter]
		if !ok {
			problem("%s references unknown adapter %q", location, binding.Adapter)
		} else if adapter.Method != binding.Method {
			problem("%s method %q does not match adapter %q method %q", location, binding.Method, binding.Adapter, adapter.Method)
		}
		for prior := 0; prior < index; prior++ {
			other := d.TargetBindings[prior]
			if binding.Method == other.Method && binding.Target.Overlaps(other.Target) {
				problem("%s overlaps target_bindings[%d] for method %q", location, prior, binding.Method)
			}
		}
	}

	for _, packageID := range sortedKeys(d.Packages) {
		pkg := d.Packages[packageID]
		if !idPattern.MatchString(packageID) {
			problem("package id %q is not a lowercase stable id", packageID)
		}
		if strings.TrimSpace(pkg.Description) == "" {
			problem("package %q description is required", packageID)
		}
		if len(pkg.Recipes) == 0 {
			problem("package %q recipes must not be empty", packageID)
		}
		for _, adapterID := range sortedKeys(pkg.Recipes) {
			recipe := pkg.Recipes[adapterID]
			location := fmt.Sprintf("package %q recipe %q", packageID, adapterID)
			adapter, ok := d.Adapters[adapterID]
			if !ok {
				problem("%s references unknown adapter", location)
			} else if recipe.Method != adapter.Method {
				problem("%s method %q does not match adapter method %q", location, recipe.Method, adapter.Method)
			}
			if !revisionPattern.MatchString(recipe.RecipeRevision) {
				problem("%s recipe_revision %q is not a stable revision", location, recipe.RecipeRevision)
			}
			validateSource(location, adapterID, recipe.Source, problem)
			validateSelection(location, recipe.VersionScheme, recipe.Selection, problem)
			validatePolicyVersions(location, recipe, problem)
			if duplicate := firstDuplicate(recipe.Exclude); duplicate != "" {
				problem("%s repeats excluded version %q", location, duplicate)
			}
			for index, excluded := range recipe.Exclude {
				if strings.TrimSpace(excluded) == "" {
					problem("%s exclude[%d] must not be empty", location, index)
				}
			}
			if duplicate := firstDuplicate(recipe.Gates); duplicate != "" {
				problem("%s repeats gate %q", location, duplicate)
			}
			for index, gate := range recipe.Gates {
				if !idPattern.MatchString(gate) {
					problem("%s gates[%d] %q is not a lowercase stable id", location, index, gate)
				}
			}
			if len(recipe.Tested) == 0 {
				problem("%s tested must not be empty", location)
			}
			for index, tested := range recipe.Tested {
				testedLocation := fmt.Sprintf("%s tested[%d]", location, index)
				if strings.TrimSpace(tested.RootVersion) == "" {
					problem("%s root_version is required", testedLocation)
				}
				if !sha256Pattern.MatchString(tested.ClosureDigest) {
					problem("%s closure_digest must be 64 lowercase hexadecimal characters", testedLocation)
				}
				if err := tested.Target.Validate(); err != nil {
					problem("%s target: %v", testedLocation, err)
				} else if testedAdapter, err := d.AdapterFor(recipe.Method, tested.Target); err != nil {
					problem("%s target adapter: %v", testedLocation, err)
				} else if testedAdapter != adapterID {
					problem("%s target selects adapter %q, not recipe adapter %q", testedLocation, testedAdapter, adapterID)
				}
				if strings.TrimSpace(tested.Evidence) == "" {
					problem("%s evidence is required", testedLocation)
				}
			}
			for index, dependency := range recipe.Dependencies {
				dependencyLocation := fmt.Sprintf("%s dependencies[%d]", location, index)
				dependencyPackage, ok := d.Packages[dependency.Package]
				if !ok {
					problem("%s references unknown package %q", dependencyLocation, dependency.Package)
				} else if _, ok := dependencyPackage.Recipes[adapterID]; !ok {
					problem("%s package %q has no recipe for adapter %q", dependencyLocation, dependency.Package, adapterID)
				}
				if strings.TrimSpace(dependency.Constraint) == "" {
					problem("%s constraint is required", dependencyLocation)
				} else if ok {
					dependencyRecipe, recipeOK := dependencyPackage.Recipes[adapterID]
					if recipeOK {
						if _, err := version.ConstraintAllowsPrerelease(dependencyRecipe.VersionScheme, dependency.Constraint); err != nil {
							problem("%s constraint: %v", dependencyLocation, err)
						}
					}
				}
			}
			if duplicate := duplicateDependency(recipe.Dependencies); duplicate != "" {
				problem("%s repeats dependency %q", location, duplicate)
			}
		}
	}

	for _, cycle := range dependencyCycles(d) {
		problem("dependency cycle: %s", strings.Join(cycle, " -> "))
	}

	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

func (d Document) AdapterFor(method string, target software.Target) (string, error) {
	if err := target.Validate(); err != nil {
		return "", fmt.Errorf("target invalid: %w", err)
	}
	var match string
	for _, binding := range d.TargetBindings {
		if binding.Method != method || !binding.Target.Matches(target) {
			continue
		}
		if match != "" {
			return "", fmt.Errorf("method %q has ambiguous adapters %q and %q for target", method, match, binding.Adapter)
		}
		match = binding.Adapter
	}
	if match == "" {
		return "", fmt.Errorf("method %q has no adapter for target", method)
	}
	return match, nil
}

func validateSource(location, adapterID string, source Source, problem func(string, ...any)) {
	switch source.Kind {
	case "homebrew-formula":
		if adapterID != "homebrew" {
			problem("%s source kind %q requires adapter homebrew", location, source.Kind)
		}
		if source.Tap == "" || source.Formula == "" {
			problem("%s homebrew source requires tap and formula", location)
		}
		if source.Index != "" || source.Distribution != "" {
			problem("%s homebrew source cannot declare Python index fields", location)
		}
	case "python-index":
		if adapterID != "uv" {
			problem("%s source kind %q requires adapter uv", location, source.Kind)
		}
		if source.Index == "" || source.Distribution == "" {
			problem("%s Python source requires index and distribution", location)
		}
		if source.Tap != "" || source.Formula != "" {
			problem("%s Python source cannot declare Homebrew fields", location)
		}
	default:
		problem("%s source kind %q is not supported", location, source.Kind)
	}
}

func validateSelection(location, scheme string, selection Selection, problem func(string, ...any)) {
	switch scheme {
	case "semver", "pep440":
		if selection.Policy == "revision" {
			problem("%s %s versions do not support revision selection", location, scheme)
		}
	case "git-revision":
		if selection.Policy != "revision" {
			problem("%s git-revision versions require revision selection", location)
		}
	case "opaque":
		if selection.Policy != "exact" && selection.Policy != "latest" {
			problem("%s opaque versions support only exact or provider-designated latest selection", location)
		}
		if selection.MinimumCompatible != "" {
			problem("%s opaque versions cannot declare minimum_compatible", location)
		}
	default:
		problem("%s version_scheme %q is not supported", location, scheme)
	}
	switch selection.Policy {
	case "latest":
		if selection.Constraint != "" || selection.Exact != "" || selection.Revision != "" {
			problem("%s latest selection cannot declare constraint, exact, or revision", location)
		}
	case "range":
		if selection.Constraint == "" || selection.Exact != "" || selection.Revision != "" {
			problem("%s range selection requires only constraint", location)
		}
	case "exact":
		if selection.Exact == "" || selection.Constraint != "" || selection.Revision != "" || selection.MinimumCompatible != "" {
			problem("%s exact selection requires only exact", location)
		}
	case "revision":
		if selection.Revision == "" || selection.Constraint != "" || selection.Exact != "" || selection.MinimumCompatible != "" {
			problem("%s revision selection requires only revision", location)
		}
	default:
		problem("%s selection policy %q is not supported", location, selection.Policy)
	}
}

func validatePolicyVersions(location string, recipe Recipe, problem func(string, ...any)) {
	if recipe.VersionScheme != "semver" && recipe.VersionScheme != "pep440" {
		return
	}
	validateValue := func(field, value string) {
		if value == "" {
			return
		}
		if err := version.Validate(recipe.VersionScheme, value); err != nil {
			problem("%s %s: %v", location, field, err)
		}
	}
	validateValue("selection.minimum_compatible", recipe.Selection.MinimumCompatible)
	validateValue("selection.exact", recipe.Selection.Exact)
	if recipe.Selection.Constraint != "" {
		if _, err := version.ConstraintAllowsPrerelease(recipe.VersionScheme, recipe.Selection.Constraint); err != nil {
			problem("%s selection.constraint: %v", location, err)
		}
	}
	for index, excluded := range recipe.Exclude {
		validateValue(fmt.Sprintf("exclude[%d]", index), excluded)
	}
	for index, tested := range recipe.Tested {
		validateValue(fmt.Sprintf("tested[%d].root_version", index), tested.RootVersion)
	}
}

func dependencyCycles(document Document) [][]string {
	type state uint8
	const (
		visiting state = 1
		visited  state = 2
	)
	states := map[string]state{}
	var stack []string
	var cycles [][]string

	var visit func(string, string)
	visit = func(packageID, adapterID string) {
		key := packageID + "@" + adapterID
		switch states[key] {
		case visited:
			return
		case visiting:
			start := 0
			for index, item := range stack {
				if item == key {
					start = index
					break
				}
			}
			cycle := append([]string(nil), stack[start:]...)
			cycle = append(cycle, key)
			cycles = append(cycles, cycle)
			return
		}
		states[key] = visiting
		stack = append(stack, key)
		pkg, ok := document.Packages[packageID]
		if ok {
			if recipe, ok := pkg.Recipes[adapterID]; ok {
				dependencies := append([]Dependency(nil), recipe.Dependencies...)
				sort.Slice(dependencies, func(i, j int) bool { return dependencies[i].Package < dependencies[j].Package })
				for _, dependency := range dependencies {
					visit(dependency.Package, adapterID)
				}
			}
		}
		stack = stack[:len(stack)-1]
		states[key] = visited
	}

	for _, packageID := range sortedKeys(document.Packages) {
		for _, adapterID := range sortedKeys(document.Packages[packageID].Recipes) {
			visit(packageID, adapterID)
		}
	}
	return cycles
}

func duplicateDependency(values []Dependency) string {
	seen := map[string]bool{}
	for _, value := range values {
		if seen[value.Package] {
			return value.Package
		}
		seen[value.Package] = true
	}
	return ""
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
