// Package selection applies catalog policy to provider-neutral candidates. It
// is pure: provider reads and lock-file effects live in adjacent packages.
package selection

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/catalog"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/policy"
	"github.com/temper-sh/temper/internal/software/version"
)

type Request struct {
	Package    string
	Method     string
	Candidates []software.Candidate
}

// Resolve adds requested selections to an optional existing lock. Existing
// selections never move; callers must explicitly run a future update verb to
// reconsider them.
func Resolve(supply catalog.Snapshot, target software.Target, resolved time.Time, existing *softwarelock.Document, requests []Request) (softwarelock.Document, error) {
	if err := supply.Validate(); err != nil {
		return softwarelock.Document{}, err
	}
	if len(requests) == 0 {
		return softwarelock.Document{}, fmt.Errorf("at least one software selection request is required")
	}
	document := softwarelock.Document{
		Schema: softwarelock.SchemaV1,
		Provenance: softwarelock.Provenance{
			Catalog: &softwarelock.CatalogIdentity{
				Schema: supply.Document.Schema, Sequence: supply.Document.Sequence, SHA256: supply.SHA256,
			},
		},
		Target: target, Resolved: resolved.Format("2006-01-02"),
		Selections: map[string]softwarelock.Selection{}, Units: map[string]softwarelock.Unit{},
	}
	if existing != nil {
		if err := existing.ValidateAgainst(supply.Document, supply.SHA256); err != nil {
			return softwarelock.Document{}, fmt.Errorf("existing software lock: %w", err)
		}
		if existing.Target != target {
			return softwarelock.Document{}, fmt.Errorf("existing software lock target differs from requested target")
		}
		document = cloneDocument(*existing)
		document.Resolved = resolved.Format("2006-01-02")
	}

	requests = append([]Request(nil), requests...)
	sort.Slice(requests, func(i, j int) bool { return requests[i].Package < requests[j].Package })
	seen := map[string]bool{}
	for _, request := range requests {
		if seen[request.Package] {
			return softwarelock.Document{}, fmt.Errorf("package %q requested more than once", request.Package)
		}
		seen[request.Package] = true
		if _, exists := document.Selections[request.Package]; exists {
			return softwarelock.Document{}, fmt.Errorf("package %q is already selected; resolution only fills missing selections", request.Package)
		}
		pkg, ok := supply.Document.Packages[request.Package]
		if !ok {
			return softwarelock.Document{}, fmt.Errorf("catalog package %q does not exist", request.Package)
		}
		adapterID, err := supply.Document.AdapterFor(request.Method, target)
		if err != nil {
			return softwarelock.Document{}, fmt.Errorf("package %q: %w", request.Package, err)
		}
		recipe, ok := pkg.Recipes[adapterID]
		if !ok || recipe.Method != request.Method {
			return softwarelock.Document{}, fmt.Errorf("package %q has no %q recipe for catalog-selected adapter %q", request.Package, request.Method, adapterID)
		}
		candidate, err := choose(supply.Document, target, request.Package, adapterID, recipe, request.Candidates)
		if err != nil {
			return softwarelock.Document{}, err
		}
		selection := softwarelock.Selection{
			Provenance: softwarelock.ProvenanceCatalog,
			Method:     request.Method, Adapter: adapterID, RecipeRevision: recipe.RecipeRevision, RootUnit: candidate.RootUnit,
		}
		document.Selections[request.Package] = selection
		for unitID, resolvedUnit := range candidate.Units {
			unit := lockedUnit(adapterID, resolvedUnit)
			if previous, exists := document.Units[unitID]; exists && !reflect.DeepEqual(canonicalUnit(previous), canonicalUnit(unit)) {
				return softwarelock.Document{}, fmt.Errorf("package %q unit %q conflicts with another selected closure", request.Package, unitID)
			}
			document.Units[unitID] = unit
		}
	}
	if err := document.ValidateAgainst(supply.Document, supply.SHA256); err != nil {
		return softwarelock.Document{}, err
	}
	return document, nil
}

func choose(supply catalog.Document, target software.Target, packageID, adapterID string, recipe catalog.Recipe, candidates []software.Candidate) (software.Candidate, error) {
	if len(candidates) == 0 {
		return software.Candidate{}, fmt.Errorf("package %q adapter %q returned no candidates", packageID, adapterID)
	}
	eligible := make([]software.Candidate, 0, len(candidates))
	for index, candidate := range candidates {
		policyMatched, err := validateCandidate(supply, target, packageID, adapterID, recipe, candidate)
		if err != nil {
			return software.Candidate{}, fmt.Errorf("package %q candidate[%d]: %w", packageID, index, err)
		}
		if !policyMatched {
			continue
		}
		eligible = append(eligible, canonicalCandidate(candidate))
	}
	if len(eligible) == 0 {
		return software.Candidate{}, fmt.Errorf("package %q has no candidate satisfying catalog policy", packageID)
	}

	best := eligible[0]
	for _, candidate := range eligible[1:] {
		order, err := compareCandidates(recipe, candidate, best)
		if err != nil {
			return software.Candidate{}, fmt.Errorf("package %q: %w", packageID, err)
		}
		if order > 0 {
			best = candidate
		}
	}
	for _, candidate := range eligible {
		order, err := compareCandidates(recipe, candidate, best)
		if err != nil {
			return software.Candidate{}, fmt.Errorf("package %q: %w", packageID, err)
		}
		if order == 0 && !sameCandidate(candidate, best) {
			return software.Candidate{}, fmt.Errorf("package %q has ambiguous distinct candidates at the selected version", packageID)
		}
	}
	return best, nil
}

func validateCandidate(supply catalog.Document, target software.Target, packageID, adapterID string, recipe catalog.Recipe, candidate software.Candidate) (bool, error) {
	root, ok := candidate.Units[candidate.RootUnit]
	if !ok || candidate.RootUnit == "" {
		return false, fmt.Errorf("root unit %q is missing", candidate.RootUnit)
	}
	if root.NativeName != recipe.Source.NativeName() {
		return false, fmt.Errorf("root native name is %q, recipe requires %q", root.NativeName, recipe.Source.NativeName())
	}
	probe := softwarelock.Document{
		Schema: softwarelock.SchemaV1,
		Provenance: softwarelock.Provenance{Catalog: &softwarelock.CatalogIdentity{
			Schema: supply.Schema, Sequence: supply.Sequence, SHA256: strings.Repeat("0", 64),
		}},
		Target: target, Resolved: "2000-01-01",
		Selections: map[string]softwarelock.Selection{
			packageID: {Provenance: softwarelock.ProvenanceCatalog, Method: recipe.Method, Adapter: adapterID, RecipeRevision: recipe.RecipeRevision, RootUnit: candidate.RootUnit},
		},
		Units: make(map[string]softwarelock.Unit, len(candidate.Units)),
	}
	for unitID, resolvedUnit := range candidate.Units {
		probe.Units[unitID] = lockedUnit(adapterID, resolvedUnit)
	}
	if err := probe.Validate(); err != nil {
		return false, err
	}

	current := candidate.Current
	return policy.ClosureEligible(supply, adapterID, packageID, candidate.RootUnit, candidate.Units, &current)
}

func compareCandidates(recipe catalog.Recipe, left, right software.Candidate) (int, error) {
	leftRoot := left.Units[left.RootUnit]
	rightRoot := right.Units[right.RootUnit]
	switch recipe.VersionScheme {
	case "semver", "pep440":
		return version.Compare(recipe.VersionScheme, leftRoot.Version, rightRoot.Version)
	case "opaque", "git-revision":
		return 0, nil
	default:
		return 0, fmt.Errorf("version scheme %q cannot compare candidates", recipe.VersionScheme)
	}
}

func lockedUnit(adapterID string, unit software.ResolvedUnit) softwarelock.Unit {
	return softwarelock.Unit{
		Adapter: adapterID, Scope: unit.Scope, NativeName: unit.NativeName,
		Version: unit.Version, Revision: unit.Revision,
		Dependencies: append([]string(nil), unit.Dependencies...),
		Artifacts:    append([]software.Artifact(nil), unit.Artifacts...),
	}
}

func canonicalCandidate(candidate software.Candidate) software.Candidate {
	result := software.Candidate{RootUnit: candidate.RootUnit, Current: candidate.Current, Units: make(map[string]software.ResolvedUnit, len(candidate.Units))}
	for unitID, unit := range candidate.Units {
		unit.Dependencies = append([]string(nil), unit.Dependencies...)
		sort.Strings(unit.Dependencies)
		unit.Artifacts = append([]software.Artifact(nil), unit.Artifacts...)
		sort.Slice(unit.Artifacts, func(i, j int) bool {
			if unit.Artifacts[i].Locator == unit.Artifacts[j].Locator {
				return unit.Artifacts[i].SHA256 < unit.Artifacts[j].SHA256
			}
			return unit.Artifacts[i].Locator < unit.Artifacts[j].Locator
		})
		result.Units[unitID] = unit
	}
	return result
}

func sameCandidate(left, right software.Candidate) bool {
	left.Current = false
	right.Current = false
	return reflect.DeepEqual(left, right)
}

func canonicalUnit(unit softwarelock.Unit) softwarelock.Unit {
	unit.Dependencies = append([]string(nil), unit.Dependencies...)
	sort.Strings(unit.Dependencies)
	unit.Artifacts = append([]software.Artifact(nil), unit.Artifacts...)
	sort.Slice(unit.Artifacts, func(i, j int) bool {
		if unit.Artifacts[i].Locator == unit.Artifacts[j].Locator {
			return unit.Artifacts[i].SHA256 < unit.Artifacts[j].SHA256
		}
		return unit.Artifacts[i].Locator < unit.Artifacts[j].Locator
	})
	return unit
}

func cloneDocument(document softwarelock.Document) softwarelock.Document {
	clone := document
	clone.Selections = make(map[string]softwarelock.Selection, len(document.Selections))
	for packageID, selection := range document.Selections {
		clone.Selections[packageID] = selection
	}
	clone.Units = make(map[string]softwarelock.Unit, len(document.Units))
	for unitID, unit := range document.Units {
		clone.Units[unitID] = canonicalUnit(unit)
	}
	return clone
}
