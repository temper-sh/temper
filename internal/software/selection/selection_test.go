package selection_test

import (
	"strings"
	"testing"
	"time"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/catalog"
	"github.com/temper-sh/temper/internal/software/selection"
)

var testTarget = software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6"}

func TestLatestSelectsNewestStableAboveFloorAndExcludesKnownBad(t *testing.T) {
	supply := selectionCatalog(t)
	request := selection.Request{Package: "llama-swap", Method: "system-package", Candidates: []software.Candidate{
		singleCandidate("homebrew:system:llama-swap", "system", "llama-swap", "0.9.0", "rev/090", false),
		singleCandidate("homebrew:system:llama-swap", "system", "llama-swap", "1.3.0", "rev/130", false),
		singleCandidate("homebrew:system:llama-swap", "system", "llama-swap", "1.4.0", "rev/140", false),
		singleCandidate("homebrew:system:llama-swap", "system", "llama-swap", "2.0.0-rc.1", "rev/200rc1", false),
	}}

	locked, err := selection.Resolve(supply, testTarget, fixedTime(), nil, []selection.Request{request})
	if err != nil {
		t.Fatal(err)
	}
	root := locked.Units[locked.Selections["llama-swap"].RootUnit]
	if root.Version != "1.3.0" {
		t.Errorf("selected version = %q, want 1.3.0", root.Version)
	}
}

func TestPEP440ExactRootSelectsClosureWithConstrainedDependency(t *testing.T) {
	supply := selectionCatalog(t)
	request := selection.Request{Package: "rapid-mlx", Method: "python-environment", Candidates: []software.Candidate{
		rapidCandidate("0.1.5", "0.25.0"),
		rapidCandidate("0.1.5", "0.24.2"),
	}}

	locked, err := selection.Resolve(supply, testTarget, fixedTime(), nil, []selection.Request{request})
	if err != nil {
		t.Fatal(err)
	}
	if got := locked.Units["uv:rapid-mlx:mlx"].Version; got != "0.24.2" {
		t.Errorf("selected mlx version = %q, want 0.24.2", got)
	}
}

func TestDependencyRecipeExclusionsApplyInsideClosure(t *testing.T) {
	supply := selectionCatalog(t)
	mlx := supply.Document.Packages["mlx"]
	mlxRecipe := mlx.Recipes["uv"]
	mlxRecipe.Exclude = []string{"0.24.3"}
	mlx.Recipes["uv"] = mlxRecipe
	supply.Document.Packages["mlx"] = mlx
	if err := supply.Document.Validate(); err != nil {
		t.Fatal(err)
	}

	locked, err := selection.Resolve(supply, testTarget, fixedTime(), nil, []selection.Request{{
		Package: "rapid-mlx", Method: "python-environment", Candidates: []software.Candidate{
			rapidCandidate("0.1.5", "0.24.3"),
			rapidCandidate("0.1.5", "0.24.2"),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := locked.Units["uv:rapid-mlx:mlx"].Version; got != "0.24.2" {
		t.Errorf("selected dependency version = %q, want non-excluded 0.24.2", got)
	}
}

func TestSelectionAppliesTransitiveCatalogConstraints(t *testing.T) {
	supply := selectionCatalog(t)
	tested := []catalog.Tested{{
		RootVersion: "4.2", ClosureDigest: strings.Repeat("9", 64), Target: testTarget, Evidence: "fixture",
	}}
	supply.Document.Packages["typing-extensions"] = catalog.Package{
		Description: "typing support",
		Recipes: map[string]catalog.Recipe{"uv": {
			Method: "python-environment", RecipeRevision: "typing/v1",
			Source:        catalog.Source{Kind: "python-index", Index: "https://example.invalid/simple", Distribution: "typing-extensions"},
			VersionScheme: "pep440", Selection: catalog.Selection{Policy: "range", Constraint: ">=4,<5"}, Tested: tested,
		}},
	}
	mlx := supply.Document.Packages["mlx"]
	mlxRecipe := mlx.Recipes["uv"]
	mlxRecipe.Dependencies = []catalog.Dependency{{Package: "typing-extensions", Constraint: ">=4,<5"}}
	mlx.Recipes["uv"] = mlxRecipe
	supply.Document.Packages["mlx"] = mlx
	if err := supply.Document.Validate(); err != nil {
		t.Fatal(err)
	}

	bad := rapidCandidate("0.1.5", "0.24.2")
	bad = withTypingDependency(bad, "5.0")
	good := rapidCandidate("0.1.5", "0.24.2")
	good = withTypingDependency(good, "4.2")
	locked, err := selection.Resolve(supply, testTarget, fixedTime(), nil, []selection.Request{{
		Package: "rapid-mlx", Method: "python-environment", Candidates: []software.Candidate{bad, good},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := locked.Units["uv:rapid-mlx:typing-extensions"].Version; got != "4.2" {
		t.Errorf("selected transitive version = %q, want 4.2", got)
	}
}

func TestOpaqueLatestAndGitRevisionUseProviderIdentityNotInventedOrdering(t *testing.T) {
	tests := []struct {
		name       string
		scheme     string
		policy     catalog.Selection
		candidates []software.Candidate
		want       string
	}{
		{
			name: "opaque provider current", scheme: "opaque", policy: catalog.Selection{Policy: "latest"},
			candidates: []software.Candidate{
				singleCandidate("homebrew:system:llama-swap", "system", "llama-swap", "zebra", "rev/old", false),
				singleCandidate("homebrew:system:llama-swap", "system", "llama-swap", "apple", "rev/new", true),
			}, want: "apple",
		},
		{
			name: "git exact revision", scheme: "git-revision", policy: catalog.Selection{Policy: "revision", Revision: "git/abc123"},
			candidates: []software.Candidate{
				singleCandidate("homebrew:system:llama-swap", "system", "llama-swap", "nightly-new", "git/def456", false),
				singleCandidate("homebrew:system:llama-swap", "system", "llama-swap", "nightly-old", "git/abc123", false),
			}, want: "nightly-old",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			supply := selectionCatalog(t)
			pkg := supply.Document.Packages["llama-swap"]
			recipe := pkg.Recipes["homebrew"]
			recipe.VersionScheme = tt.scheme
			recipe.Selection = tt.policy
			pkg.Recipes["homebrew"] = recipe
			supply.Document.Packages["llama-swap"] = pkg
			if err := supply.Document.Validate(); err != nil {
				t.Fatal(err)
			}

			locked, err := selection.Resolve(supply, testTarget, fixedTime(), nil, []selection.Request{{
				Package: "llama-swap", Method: "system-package", Candidates: tt.candidates,
			}})
			if err != nil {
				t.Fatal(err)
			}
			root := locked.Units[locked.Selections["llama-swap"].RootUnit]
			if root.Version != tt.want {
				t.Errorf("selected version = %q, want %q", root.Version, tt.want)
			}
		})
	}
}

func TestSelectionRefusesMalformedAndAmbiguousCandidates(t *testing.T) {
	supply := selectionCatalog(t)
	cycle := singleCandidate("homebrew:system:llama-swap", "system", "llama-swap", "1.3.0", "rev/one", false)
	root := cycle.Units[cycle.RootUnit]
	root.Dependencies = []string{cycle.RootUnit}
	cycle.Units[cycle.RootUnit] = root

	_, err := selection.Resolve(supply, testTarget, fixedTime(), nil, []selection.Request{{
		Package: "llama-swap", Method: "system-package", Candidates: []software.Candidate{cycle},
	}})
	if err == nil || !strings.Contains(err.Error(), "dependency cycle") {
		t.Fatalf("cycle error = %v, want refusal", err)
	}

	_, err = selection.Resolve(supply, testTarget, fixedTime(), nil, []selection.Request{{
		Package: "llama-swap", Method: "system-package", Candidates: []software.Candidate{
			singleCandidate("homebrew:system:llama-swap", "system", "llama-swap", "1.3.0", "rev/one", false),
			singleCandidate("homebrew:system:llama-swap", "system", "llama-swap", "1.3.0", "rev/two", false),
		},
	}})
	if err == nil || !strings.Contains(err.Error(), "ambiguous distinct candidates") {
		t.Fatalf("ambiguity error = %v, want refusal", err)
	}
}

func TestLowerVersionAmbiguityDoesNotBlockUniqueHighestCandidate(t *testing.T) {
	supply := selectionCatalog(t)
	locked, err := selection.Resolve(supply, testTarget, fixedTime(), nil, []selection.Request{{
		Package: "llama-swap", Method: "system-package", Candidates: []software.Candidate{
			singleCandidate("homebrew:system:llama-swap", "system", "llama-swap", "1.2.0", "rev/one", false),
			singleCandidate("homebrew:system:llama-swap", "system", "llama-swap", "1.2.0", "rev/two", false),
			singleCandidate("homebrew:system:llama-swap", "system", "llama-swap", "1.3.0", "rev/three", false),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	root := locked.Units[locked.Selections["llama-swap"].RootUnit]
	if root.Version != "1.3.0" {
		t.Fatalf("selected version = %q, want 1.3.0", root.Version)
	}
}

func selectionCatalog(t *testing.T) catalog.Snapshot {
	t.Helper()
	tested := func() []catalog.Tested {
		return []catalog.Tested{{RootVersion: "1.0.0", ClosureDigest: strings.Repeat("a", 64), Target: testTarget, Evidence: "fixture"}}
	}
	document := catalog.Document{
		Schema: catalog.SchemaV1, Sequence: 7, PublishedAt: "2026-08-20T08:00:00Z",
		Methods: map[string]catalog.Method{
			"system-package": {Description: "system"}, "python-environment": {Description: "python"},
		},
		Adapters: map[string]catalog.Adapter{
			"homebrew": {Method: "system-package", Protocol: catalog.AdapterProtocolV1, EffectModel: "shared"},
			"uv":       {Method: "python-environment", Protocol: catalog.AdapterProtocolV1, EffectModel: "isolated"},
		},
		TargetBindings: []catalog.TargetBinding{
			{Method: "system-package", Target: software.Target{OS: "darwin", Arch: "arm64"}, Adapter: "homebrew"},
			{Method: "python-environment", Target: software.Target{OS: "darwin", Arch: "arm64"}, Adapter: "uv"},
		},
		Packages: map[string]catalog.Package{
			"llama-swap": {Description: "router", Recipes: map[string]catalog.Recipe{
				"homebrew": {
					Method: "system-package", RecipeRevision: "llama/v1",
					Source:        catalog.Source{Kind: "homebrew-formula", Tap: "temper/tap", Formula: "llama-swap"},
					VersionScheme: "semver", Selection: catalog.Selection{Policy: "latest", MinimumCompatible: "1.0.0"},
					Exclude: []string{"1.4.0"}, Tested: tested(),
				},
			}},
			"rapid-mlx": {Description: "server", Recipes: map[string]catalog.Recipe{
				"uv": {
					Method: "python-environment", RecipeRevision: "rapid/v1",
					Source:        catalog.Source{Kind: "python-index", Index: "https://example.invalid/simple", Distribution: "rapid-mlx"},
					VersionScheme: "pep440", Selection: catalog.Selection{Policy: "exact", Exact: "0.1.5"},
					Dependencies: []catalog.Dependency{{Package: "mlx", Constraint: ">=0.24,<0.25"}}, Tested: tested(),
				},
			}},
			"mlx": {Description: "runtime", Recipes: map[string]catalog.Recipe{
				"uv": {
					Method: "python-environment", RecipeRevision: "mlx/v1",
					Source:        catalog.Source{Kind: "python-index", Index: "https://example.invalid/simple", Distribution: "mlx"},
					VersionScheme: "pep440", Selection: catalog.Selection{Policy: "range", Constraint: ">=0.24,<0.25"}, Tested: tested(),
				},
			}},
		},
	}
	if err := document.Validate(); err != nil {
		t.Fatal(err)
	}
	return catalog.Snapshot{Document: document, SHA256: strings.Repeat("c", 64)}
}

func singleCandidate(unitID, scope, nativeName, version, revision string, current bool) software.Candidate {
	return software.Candidate{RootUnit: unitID, Current: current, Units: map[string]software.ResolvedUnit{
		unitID: {
			Scope: scope, NativeName: nativeName, Version: version, Revision: revision,
			Artifacts: []software.Artifact{{Locator: "https://example.invalid/" + nativeName, SHA256: strings.Repeat("d", 64)}},
		},
	}}
}

func rapidCandidate(rapidVersion, mlxVersion string) software.Candidate {
	return software.Candidate{RootUnit: "uv:rapid-mlx:rapid-mlx", Units: map[string]software.ResolvedUnit{
		"uv:rapid-mlx:rapid-mlx": {
			Scope: "rapid-mlx", NativeName: "rapid-mlx", Version: rapidVersion,
			Dependencies: []string{"uv:rapid-mlx:mlx"},
			Artifacts:    []software.Artifact{{Locator: "https://example.invalid/rapid.whl", SHA256: strings.Repeat("e", 64)}},
		},
		"uv:rapid-mlx:mlx": {
			Scope: "rapid-mlx", NativeName: "mlx", Version: mlxVersion,
			Artifacts: []software.Artifact{{Locator: "https://example.invalid/mlx.whl", SHA256: strings.Repeat("f", 64)}},
		},
	}}
}

func withTypingDependency(candidate software.Candidate, typingVersion string) software.Candidate {
	mlx := candidate.Units["uv:rapid-mlx:mlx"]
	mlx.Dependencies = []string{"uv:rapid-mlx:typing-extensions"}
	candidate.Units["uv:rapid-mlx:mlx"] = mlx
	candidate.Units["uv:rapid-mlx:typing-extensions"] = software.ResolvedUnit{
		Scope: "rapid-mlx", NativeName: "typing-extensions", Version: typingVersion,
		Artifacts: []software.Artifact{{Locator: "https://example.invalid/typing.whl", SHA256: strings.Repeat("8", 64)}},
	}
	return candidate
}

func fixedTime() time.Time {
	return time.Date(2026, 8, 20, 22, 0, 0, 0, time.FixedZone("CEST", 2*60*60))
}
