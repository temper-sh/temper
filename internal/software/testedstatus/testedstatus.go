// Package testedstatus derives signed-catalog evidence status for exact
// software-lock selections. It is a pure comparison: no status is persisted in
// the lock, receipt, or machine state.
package testedstatus

import (
	"fmt"
	"sort"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/catalog"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/policy"
)

type Status string

const (
	ExactTested            Status = "exact-tested"
	PolicyEligibleUntested Status = "policy-eligible-untested"
	KnownBad               Status = "known-bad"
	OutsidePolicy          Status = "outside-policy"
)

type Entry struct {
	Package        string
	Method         string
	Adapter        string
	RecipeRevision string
	RootVersion    string
	ClosureDigest  string
	Status         Status
	Evidence       string
}

// Compare classifies every selected root against the supplied catalog
// snapshot. The snapshot may be the historical catalog named by the lock or a
// newer active catalog; catalog identity drift is therefore data to compare,
// not an input error.
func Compare(locked softwarelock.Document, supply catalog.Snapshot) ([]Entry, error) {
	if err := locked.Validate(); err != nil {
		return nil, err
	}
	if err := supply.Validate(); err != nil {
		return nil, err
	}

	units := resolvedUnits(locked.Units)
	packages := make([]string, 0, len(locked.Selections))
	for packageID := range locked.Selections {
		packages = append(packages, packageID)
	}
	sort.Strings(packages)

	entries := make([]Entry, 0, len(packages))
	for _, packageID := range packages {
		selection := locked.Selections[packageID]
		root := locked.Units[selection.RootUnit]
		closureDigest, err := locked.ClosureDigest(packageID)
		if err != nil {
			return nil, err
		}
		entry := Entry{
			Package: packageID, Method: selection.Method, Adapter: selection.Adapter,
			RecipeRevision: selection.RecipeRevision, RootVersion: root.Version,
			ClosureDigest: closureDigest, Status: OutsidePolicy,
		}

		pkg, ok := supply.Document.Packages[packageID]
		if !ok {
			entries = append(entries, entry)
			continue
		}
		recipe, ok := pkg.Recipes[selection.Adapter]
		if !ok || recipe.Method != selection.Method || root.NativeName != recipe.Source.NativeName() {
			entries = append(entries, entry)
			continue
		}
		adapterID, err := supply.Document.AdapterFor(selection.Method, locked.Target)
		if err != nil || adapterID != selection.Adapter {
			entries = append(entries, entry)
			continue
		}

		excluded, err := policy.ClosureExcluded(supply.Document, selection.Adapter, packageID, selection.RootUnit, units)
		if err == nil && excluded {
			entry.Status = KnownBad
			entries = append(entries, entry)
			continue
		}
		if err != nil {
			entries = append(entries, entry)
			continue
		}

		if selection.RecipeRevision == recipe.RecipeRevision {
			for _, tested := range recipe.Tested {
				if tested.Target.Matches(locked.Target) && tested.RootVersion == root.Version && tested.ClosureDigest == closureDigest {
					entry.Status = ExactTested
					entry.Evidence = tested.Evidence
					break
				}
			}
		}
		if entry.Status == ExactTested {
			entries = append(entries, entry)
			continue
		}

		eligible, err := policy.ClosureEligible(supply.Document, selection.Adapter, packageID, selection.RootUnit, units, nil)
		if err == nil && eligible {
			entry.Status = PolicyEligibleUntested
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func resolvedUnits(units map[string]softwarelock.Unit) map[string]software.ResolvedUnit {
	resolved := make(map[string]software.ResolvedUnit, len(units))
	for id, unit := range units {
		resolved[id] = software.ResolvedUnit{
			Scope: unit.Scope, NativeName: unit.NativeName, Version: unit.Version,
			Revision: unit.Revision, Dependencies: append([]string(nil), unit.Dependencies...),
			Artifacts: append([]software.Artifact(nil), unit.Artifacts...),
		}
	}
	return resolved
}

func (s Status) Validate() error {
	switch s {
	case ExactTested, PolicyEligibleUntested, KnownBad, OutsidePolicy:
		return nil
	default:
		return fmt.Errorf("unknown software tested status %q", s)
	}
}
