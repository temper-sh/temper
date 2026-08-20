// Package policy applies one catalog recipe graph to an exact provider-neutral
// closure. Both resolution and later lock validation use this implementation.
package policy

import (
	"fmt"
	"sort"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/catalog"
	"github.com/temper-sh/temper/internal/software/version"
)

// ClosureEligible checks root policy, exclusions, and every direct/transitive
// catalog dependency constraint. current is required while resolving an
// opaque latest candidate; nil means validation of an already resolved lock,
// where the historical provider-current observation cannot be reconstructed.
func ClosureEligible(supply catalog.Document, adapterID, packageID, rootUnit string, units map[string]software.ResolvedUnit, current *bool) (bool, error) {
	root, ok := units[rootUnit]
	if !ok {
		return false, fmt.Errorf("root unit %q is missing", rootUnit)
	}
	recipe := supply.Packages[packageID].Recipes[adapterID]
	matched, err := Matches(recipe, root.Version, root.Revision, current)
	if err != nil || !matched {
		return matched, err
	}
	return dependenciesEligible(supply, adapterID, packageID, rootUnit, units)
}

// Matches applies one recipe's root policy to an exact version identity.
func Matches(recipe catalog.Recipe, candidateVersion, candidateRevision string, current *bool) (bool, error) {
	scheme := recipe.VersionScheme
	selection := recipe.Selection
	switch scheme {
	case "opaque":
		if stringExcluded(candidateVersion, recipe.Exclude) {
			return false, nil
		}
		switch selection.Policy {
		case "exact":
			return candidateVersion == selection.Exact, nil
		case "latest":
			if current == nil {
				return true, nil
			}
			return *current, nil
		}
	case "git-revision":
		if stringExcluded(candidateVersion, recipe.Exclude) || stringExcluded(candidateRevision, recipe.Exclude) {
			return false, nil
		}
		return candidateRevision == selection.Revision, nil
	case "semver", "pep440":
		if err := version.Validate(scheme, candidateVersion); err != nil {
			return false, err
		}
		for _, excluded := range recipe.Exclude {
			order, err := version.Compare(scheme, candidateVersion, excluded)
			if err != nil {
				return false, fmt.Errorf("excluded version %q: %w", excluded, err)
			}
			if order == 0 {
				return false, nil
			}
		}
		allowPrerelease := selection.Policy == "exact"
		if !allowPrerelease && selection.MinimumCompatible != "" {
			minimumIsPrerelease, err := version.IsPrerelease(scheme, selection.MinimumCompatible)
			if err != nil {
				return false, fmt.Errorf("minimum_compatible: %w", err)
			}
			allowPrerelease = minimumIsPrerelease
		}
		if !allowPrerelease && selection.Constraint != "" {
			var err error
			allowPrerelease, err = version.ConstraintAllowsPrerelease(scheme, selection.Constraint)
			if err != nil {
				return false, err
			}
		}
		if !allowPrerelease {
			prerelease, err := version.IsPrerelease(scheme, candidateVersion)
			if err != nil {
				return false, err
			}
			if prerelease {
				return false, nil
			}
		}
		if selection.MinimumCompatible != "" {
			matched, err := version.Satisfies(scheme, candidateVersion, ">="+selection.MinimumCompatible)
			if err != nil || !matched {
				return matched, err
			}
		}
		switch selection.Policy {
		case "latest":
			return true, nil
		case "range":
			return version.Satisfies(scheme, candidateVersion, selection.Constraint)
		case "exact":
			order, err := version.Compare(scheme, candidateVersion, selection.Exact)
			return order == 0, err
		}
	}
	return false, fmt.Errorf("unsupported %s/%s selection", scheme, selection.Policy)
}

func dependenciesEligible(supply catalog.Document, adapterID, packageID, unitID string, units map[string]software.ResolvedUnit) (bool, error) {
	recipe := supply.Packages[packageID].Recipes[adapterID]
	for _, dependency := range recipe.Dependencies {
		dependencyRecipe := supply.Packages[dependency.Package].Recipes[adapterID]
		descendants := reachableFrom(units, units[unitID].Dependencies)
		var matchingIDs []string
		for descendantID := range descendants {
			if units[descendantID].NativeName == dependencyRecipe.Source.NativeName() {
				matchingIDs = append(matchingIDs, descendantID)
			}
		}
		sort.Strings(matchingIDs)
		if len(matchingIDs) != 1 {
			return false, fmt.Errorf("catalog dependency %q has %d matching units below %q, want exactly one", dependency.Package, len(matchingIDs), unitID)
		}
		dependencyUnit := units[matchingIDs[0]]
		policyMatched, err := Matches(dependencyRecipe, dependencyUnit.Version, dependencyUnit.Revision, nil)
		if err != nil {
			return false, fmt.Errorf("catalog dependency %q policy: %w", dependency.Package, err)
		}
		if !policyMatched {
			return false, nil
		}
		matched, err := version.Satisfies(dependencyRecipe.VersionScheme, dependencyUnit.Version, dependency.Constraint)
		if err != nil {
			return false, fmt.Errorf("catalog dependency %q constraint: %w", dependency.Package, err)
		}
		if !matched {
			return false, nil
		}
		matched, err = dependenciesEligible(supply, adapterID, dependency.Package, matchingIDs[0], units)
		if err != nil || !matched {
			return matched, err
		}
	}
	return true, nil
}

func reachableFrom(units map[string]software.ResolvedUnit, roots []string) map[string]bool {
	reachable := map[string]bool{}
	var walk func(string)
	walk = func(unitID string) {
		if reachable[unitID] {
			return
		}
		reachable[unitID] = true
		for _, dependency := range units[unitID].Dependencies {
			walk(dependency)
		}
	}
	for _, root := range roots {
		walk(root)
	}
	return reachable
}

func stringExcluded(value string, excluded []string) bool {
	for _, item := range excluded {
		if value == item {
			return true
		}
	}
	return false
}
