package catalog_test

import (
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/catalog"
)

func TestParseAndSelectTargetAdapter(t *testing.T) {
	document, err := catalog.Parse(validCatalog())
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	tests := []struct {
		name   string
		method string
		target software.Target
		want   string
	}{
		{
			name:   "uses Homebrew for the current macOS system method",
			method: "system-package",
			target: software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6"},
			want:   "homebrew",
		},
		{
			name:   "uses a different adapter for a future system target",
			method: "system-package",
			target: software.Target{OS: "linux", Arch: "arm64", Distribution: "ubuntu", DistributionVersion: "26.04"},
			want:   "apt",
		},
		{
			name:   "keeps uv as the explicit Python environment method",
			method: "python-environment",
			target: software.Target{OS: "darwin", Arch: "arm64"},
			want:   "uv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := document.AdapterFor(tt.method, tt.target)
			if err != nil {
				t.Fatalf("AdapterFor() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("AdapterFor() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	input := strings.Replace(string(validCatalog()), "sequence: 42", "sequence: 42\nsecret_command: brew install anything", 1)

	_, err := catalog.Parse([]byte(input))
	if err == nil || !strings.Contains(err.Error(), "field secret_command not found") {
		t.Fatalf("Parse() error = %v, want strict unknown-field refusal", err)
	}
}

func TestValidateRejectsAmbiguousTargetBindings(t *testing.T) {
	document, err := catalog.Parse(validCatalog())
	if err != nil {
		t.Fatal(err)
	}
	document.TargetBindings = append(document.TargetBindings, catalog.TargetBinding{
		Method:  "system-package",
		Target:  software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos"},
		Adapter: "homebrew",
	})

	err = document.Validate()
	if err == nil || !strings.Contains(err.Error(), "overlaps target_bindings[0]") {
		t.Fatalf("Validate() error = %v, want ambiguous target refusal", err)
	}
}

func TestValidateReportsRecipeBoundaryProblemsTogether(t *testing.T) {
	document, err := catalog.Parse(validCatalog())
	if err != nil {
		t.Fatal(err)
	}
	rapid := document.Packages["rapid-mlx"]
	recipe := rapid.Recipes["uv"]
	recipe.Method = "system-package"
	recipe.Source.Tap = "not/a-python-field"
	recipe.Dependencies = append(recipe.Dependencies, catalog.Dependency{Package: "rapid-mlx", Constraint: ">=1"})
	rapid.Recipes["uv"] = recipe
	document.Packages["rapid-mlx"] = rapid

	err = document.Validate()
	if err == nil {
		t.Fatal("Validate() succeeded, want invalid recipe")
	}
	for _, wanted := range []string{
		"does not match adapter method",
		"cannot declare Homebrew fields",
		"dependency cycle",
	} {
		if !strings.Contains(err.Error(), wanted) {
			t.Errorf("Validate() error does not contain %q: %v", wanted, err)
		}
	}
}

func TestValidateRejectsPolicyShapesThatInventVersionSemantics(t *testing.T) {
	document, err := catalog.Parse(validCatalog())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		scheme string
		policy catalog.Selection
		want   string
	}{
		{
			name:   "semver revision",
			scheme: "semver",
			policy: catalog.Selection{Policy: "revision", Revision: "abcdef"},
			want:   "semver versions do not support revision selection",
		},
		{
			name:   "opaque range",
			scheme: "opaque",
			policy: catalog.Selection{Policy: "range", Constraint: ">=newish"},
			want:   "opaque versions support only exact or provider-designated latest selection",
		},
		{
			name:   "opaque floor",
			scheme: "opaque",
			policy: catalog.Selection{Policy: "latest", MinimumCompatible: "newish"},
			want:   "opaque versions cannot declare minimum_compatible",
		},
		{
			name:   "git latest",
			scheme: "git-revision",
			policy: catalog.Selection{Policy: "latest"},
			want:   "git-revision versions require revision selection",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := document
			candidate.Packages = clonePackages(document.Packages)
			llama := candidate.Packages["llama-swap"]
			recipe := llama.Recipes["homebrew"]
			recipe.VersionScheme = tt.scheme
			recipe.Selection = tt.policy
			llama.Recipes["homebrew"] = recipe
			candidate.Packages["llama-swap"] = llama

			err := candidate.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateRejectsMalformedNativeVersionPolicy(t *testing.T) {
	document, err := catalog.Parse(validCatalog())
	if err != nil {
		t.Fatal(err)
	}
	llama := document.Packages["llama-swap"]
	recipe := llama.Recipes["homebrew"]
	recipe.Selection.MinimumCompatible = "v1"
	llama.Recipes["homebrew"] = recipe
	document.Packages["llama-swap"] = llama

	err = document.Validate()
	if err == nil || !strings.Contains(err.Error(), "strict MAJOR.MINOR.PATCH") {
		t.Fatalf("Validate() error = %v, want malformed SemVer refusal", err)
	}
}

func TestValidateRejectsEvidenceForAnotherTargetAdapter(t *testing.T) {
	document, err := catalog.Parse(validCatalog())
	if err != nil {
		t.Fatal(err)
	}
	llama := document.Packages["llama-swap"]
	recipe := llama.Recipes["homebrew"]
	recipe.Tested[0].Target = software.Target{OS: "linux", Arch: "arm64", Distribution: "ubuntu"}
	llama.Recipes["homebrew"] = recipe
	document.Packages["llama-swap"] = llama

	err = document.Validate()
	if err == nil || !strings.Contains(err.Error(), `target selects adapter "apt", not recipe adapter "homebrew"`) {
		t.Fatalf("Validate() error = %v, want evidence/adapter refusal", err)
	}
}

func TestSnapshotDigestIdentifiesExactBytes(t *testing.T) {
	first := validCatalog()
	second := append(append([]byte(nil), first...), '\n')

	if catalog.SnapshotDigest(first) == catalog.SnapshotDigest(second) {
		t.Fatal("SnapshotDigest() ignored a byte change")
	}
	if catalog.SnapshotDigest(first) != catalog.SnapshotDigest(append([]byte(nil), first...)) {
		t.Fatal("SnapshotDigest() is not deterministic")
	}
}

func TestParseSnapshotBindsDocumentToExactBytes(t *testing.T) {
	data := validCatalog()
	snapshot, err := catalog.ParseSnapshot(data)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Document.Sequence != 42 {
		t.Errorf("snapshot sequence = %d, want 42", snapshot.Document.Sequence)
	}
	if snapshot.SHA256 != catalog.SnapshotDigest(data) {
		t.Errorf("snapshot digest = %q, want exact byte digest", snapshot.SHA256)
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("snapshot Validate() error = %v", err)
	}
}

func clonePackages(values map[string]catalog.Package) map[string]catalog.Package {
	cloned := make(map[string]catalog.Package, len(values))
	for packageID, pkg := range values {
		pkg.Recipes = cloneRecipes(pkg.Recipes)
		cloned[packageID] = pkg
	}
	return cloned
}

func cloneRecipes(values map[string]catalog.Recipe) map[string]catalog.Recipe {
	cloned := make(map[string]catalog.Recipe, len(values))
	for adapterID, recipe := range values {
		cloned[adapterID] = recipe
	}
	return cloned
}

func validCatalog() []byte {
	return []byte(`schema: temper-software-supply/v1
sequence: 42
published_at: 2026-08-20T18:30:00Z
methods:
  system-package:
    description: Shared target package manager
  python-environment:
    description: Temper-owned Python environment
adapters:
  homebrew:
    method: system-package
    protocol: temper-installer-adapter/v1
    effect_model: shared
  apt:
    method: system-package
    protocol: temper-installer-adapter/v1
    effect_model: shared
  uv:
    method: python-environment
    protocol: temper-installer-adapter/v1
    effect_model: isolated
target_bindings:
  - method: system-package
    target: {os: darwin, arch: arm64}
    adapter: homebrew
  - method: system-package
    target: {os: linux, arch: arm64, distribution: ubuntu}
    adapter: apt
  - method: python-environment
    target: {os: darwin, arch: arm64}
    adapter: uv
packages:
  llama-swap:
    description: Local model router
    recipes:
      homebrew:
        method: system-package
        recipe_revision: llama-swap-homebrew/v1
        source: {kind: homebrew-formula, tap: temper-sh/tap, formula: llama-swap}
        version_scheme: semver
        selection: {policy: latest, minimum_compatible: 1.0.0}
        dependencies: []
        exclude: [1.4.0]
        gates: [router-smoke.v1]
        tested:
          - root_version: 1.3.0
            closure_digest: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
            target: {os: darwin, arch: arm64}
            evidence: results/software/llama-swap-1.3.0
  mlx:
    description: MLX framework
    recipes:
      uv:
        method: python-environment
        recipe_revision: mlx-uv/v1
        source: {kind: python-index, index: pypi, distribution: mlx}
        version_scheme: pep440
        selection: {policy: range, constraint: ">=1,<2"}
        dependencies: []
        exclude: []
        gates: [import-smoke.v1]
        tested:
          - root_version: 1.2.0
            closure_digest: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
            target: {os: darwin, arch: arm64}
            evidence: results/software/mlx-1.2.0
  rapid-mlx:
    description: MLX model runtime
    recipes:
      uv:
        method: python-environment
        recipe_revision: rapid-mlx-uv/v1
        source: {kind: python-index, index: pypi, distribution: rapid-mlx}
        version_scheme: pep440
        selection: {policy: range, constraint: ">=0.1,<0.2"}
        dependencies:
          - {package: mlx, constraint: ">=1.2,<1.3"}
        exclude: []
        gates: [runtime-smoke.v1]
        tested:
          - root_version: 0.1.5
            closure_digest: cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
            target: {os: darwin, arch: arm64}
            evidence: results/software/rapid-mlx-0.1.5
`)
}
