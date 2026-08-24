package testedstatus_test

import (
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/catalog"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/testedstatus"
)

func TestCompareDerivesSignedCatalogStatusWithoutChangingTheLock(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*softwarelock.Document, *catalog.Snapshot)
		want   testedstatus.Status
	}{
		{
			name: "exact tested tuple under its target selector",
			mutate: func(locked *softwarelock.Document, supply *catalog.Snapshot) {
				digest, err := locked.ClosureDigest("llama-cpp")
				if err != nil {
					t.Fatal(err)
				}
				recipe := supply.Document.Packages["llama-cpp"].Recipes["homebrew"]
				recipe.Tested[0].ClosureDigest = digest
				recipe.Tested[0].Target.DistributionVersion = ""
				supply.Document.Packages["llama-cpp"].Recipes["homebrew"] = recipe
			},
			want: testedstatus.ExactTested,
		},
		{
			name: "eligible closure without exact evidence",
			want: testedstatus.PolicyEligibleUntested,
		},
		{
			name: "excluded root",
			mutate: func(_ *softwarelock.Document, supply *catalog.Snapshot) {
				recipe := supply.Document.Packages["llama-cpp"].Recipes["homebrew"]
				recipe.Exclude = []string{"1.2.3"}
				supply.Document.Packages["llama-cpp"].Recipes["homebrew"] = recipe
			},
			want: testedstatus.KnownBad,
		},
		{
			name: "excluded transitive dependency",
			mutate: func(_ *softwarelock.Document, supply *catalog.Snapshot) {
				recipe := supply.Document.Packages["libfoo"].Recipes["homebrew"]
				recipe.Exclude = []string{"4.5.6"}
				supply.Document.Packages["libfoo"].Recipes["homebrew"] = recipe
			},
			want: testedstatus.KnownBad,
		},
		{
			name: "version below current floor",
			mutate: func(locked *softwarelock.Document, _ *catalog.Snapshot) {
				unit := locked.Units["homebrew:system:llama.cpp"]
				unit.Version = "0.9.0"
				locked.Units["homebrew:system:llama.cpp"] = unit
			},
			want: testedstatus.OutsidePolicy,
		},
		{
			name: "old recipe revision remains eligible but untested",
			mutate: func(_ *softwarelock.Document, supply *catalog.Snapshot) {
				recipe := supply.Document.Packages["llama-cpp"].Recipes["homebrew"]
				recipe.RecipeRevision = "llama-cpp/v2"
				supply.Document.Packages["llama-cpp"].Recipes["homebrew"] = recipe
			},
			want: testedstatus.PolicyEligibleUntested,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			locked, supply := fixture(t)
			before, err := softwarelock.Marshal(locked)
			if err != nil {
				t.Fatal(err)
			}
			if test.mutate != nil {
				test.mutate(&locked, &supply)
			}

			entries, err := testedstatus.Compare(locked, supply)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Status != test.want {
				t.Fatalf("entries = %#v, want status %q", entries, test.want)
			}
			if entries[0].Package != "llama-cpp" || entries[0].RootVersion != locked.Units["homebrew:system:llama.cpp"].Version {
				t.Fatalf("entry identity = %#v", entries[0])
			}
			if test.want == testedstatus.ExactTested && entries[0].Evidence != "results/llama-cpp-1.2.3" {
				t.Fatalf("evidence = %q", entries[0].Evidence)
			}
			after, err := softwarelock.Marshal(locked)
			if err != nil {
				t.Fatal(err)
			}
			if test.name != "version below current floor" && string(after) != string(before) {
				t.Fatal("comparison changed the software lock")
			}
		})
	}
}

func TestCompareSortsSelectionsByLogicalPackage(t *testing.T) {
	locked, supply := fixture(t)
	locked.Selections["libfoo"] = softwarelock.Selection{
		Provenance: softwarelock.ProvenanceCatalog,
		Method:     "system-package", Adapter: "homebrew", RecipeRevision: "libfoo/v1", RootUnit: "homebrew:system:libfoo",
	}

	entries, err := testedstatus.Compare(locked, supply)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Package != "libfoo" || entries[1].Package != "llama-cpp" {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestCompareRejectsMalformedInputs(t *testing.T) {
	locked, supply := fixture(t)
	locked.Resolved = "today"

	_, err := testedstatus.Compare(locked, supply)
	if err == nil || !strings.Contains(err.Error(), "resolved") {
		t.Fatalf("error = %v", err)
	}
}

func fixture(t *testing.T) (softwarelock.Document, catalog.Snapshot) {
	t.Helper()
	target := software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6"}
	supply := catalog.Snapshot{
		SHA256: strings.Repeat("f", 64),
		Document: catalog.Document{
			Schema: "temper-software-supply/v1", Sequence: 7, PublishedAt: "2026-08-20T10:00:00Z",
			Methods: map[string]catalog.Method{"system-package": {Description: "target system package manager"}},
			Adapters: map[string]catalog.Adapter{"homebrew": {
				Method: "system-package", Protocol: "temper-installer-adapter/v1", EffectModel: "shared",
			}},
			TargetBindings: []catalog.TargetBinding{{
				Method: "system-package", Target: software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos"}, Adapter: "homebrew",
			}},
			Packages: map[string]catalog.Package{
				"llama-cpp": {
					Description: "primary runtime",
					Recipes: map[string]catalog.Recipe{"homebrew": {
						Method: "system-package", RecipeRevision: "llama-cpp/v1",
						Source:        catalog.Source{Kind: "homebrew-formula", Tap: "homebrew/core", Formula: "llama.cpp"},
						VersionScheme: "semver", Selection: catalog.Selection{Policy: "latest", MinimumCompatible: "1.0.0"},
						Dependencies: []catalog.Dependency{{Package: "libfoo", Constraint: ">=4.0.0,<5.0.0"}},
						Tested: []catalog.Tested{{
							RootVersion: "1.2.3", ClosureDigest: strings.Repeat("a", 64), Target: target,
							Evidence: "results/llama-cpp-1.2.3",
						}},
					}},
				},
				"libfoo": {
					Description: "runtime dependency",
					Recipes: map[string]catalog.Recipe{"homebrew": {
						Method: "system-package", RecipeRevision: "libfoo/v1",
						Source:        catalog.Source{Kind: "homebrew-formula", Tap: "homebrew/core", Formula: "libfoo"},
						VersionScheme: "semver", Selection: catalog.Selection{Policy: "latest", MinimumCompatible: "4.0.0"},
						Tested: []catalog.Tested{{
							RootVersion: "4.5.6", ClosureDigest: strings.Repeat("b", 64), Target: target,
							Evidence: "results/libfoo-4.5.6",
						}},
					}},
				},
			},
		},
	}
	locked := softwarelock.Document{
		Schema:     "temper-software-lock/v1",
		Provenance: softwarelock.Provenance{Catalog: &softwarelock.CatalogIdentity{Schema: "temper-software-supply/v1", Sequence: 7, SHA256: strings.Repeat("f", 64)}},
		Target:     target, Resolved: "2026-08-20",
		Selections: map[string]softwarelock.Selection{
			"llama-cpp": {Provenance: softwarelock.ProvenanceCatalog, Method: "system-package", Adapter: "homebrew", RecipeRevision: "llama-cpp/v1", RootUnit: "homebrew:system:llama.cpp"},
		},
		Units: map[string]softwarelock.Unit{
			"homebrew:system:llama.cpp": {
				Adapter: "homebrew", Scope: "system", NativeName: "llama.cpp", Version: "1.2.3", Revision: "formula:1",
				Dependencies: []string{"homebrew:system:libfoo"},
			},
			"homebrew:system:libfoo": {
				Adapter: "homebrew", Scope: "system", NativeName: "libfoo", Version: "4.5.6", Revision: "formula:1",
				Dependencies: []string{},
			},
		},
	}
	if err := locked.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := supply.Validate(); err != nil {
		t.Fatal(err)
	}
	return locked, supply
}
