package lockfile_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software/catalog"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
)

func TestParseAndValidateAgainstExactCatalogSnapshot(t *testing.T) {
	catalogBytes := validCatalog()
	supply, err := catalog.Parse(catalogBytes)
	if err != nil {
		t.Fatal(err)
	}
	digest := catalog.SnapshotDigest(catalogBytes)
	document, err := softwarelock.Parse(validLock(digest))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if err := document.ValidateAgainst(supply, digest); err != nil {
		t.Fatalf("ValidateAgainst() error = %v", err)
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	input := strings.Replace(string(validLock(strings.Repeat("d", 64))), "resolved: 2026-08-20", "resolved: 2026-08-20\ninstalled: true", 1)

	_, err := softwarelock.Parse([]byte(input))
	if err == nil || !strings.Contains(err.Error(), "field installed not found") {
		t.Fatalf("Parse() error = %v, want strict installed-state refusal", err)
	}
}

func TestMarshalRoundTripPreservesSemanticDigest(t *testing.T) {
	document, err := softwarelock.Parse(validLock(strings.Repeat("d", 64)))
	if err != nil {
		t.Fatal(err)
	}
	want, err := document.SemanticDigest()
	if err != nil {
		t.Fatal(err)
	}

	encoded, err := softwarelock.Marshal(document)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	roundTrip, err := softwarelock.Parse(encoded)
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v", err)
	}
	got, err := roundTrip.SemanticDigest()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("round-trip semantic digest = %s, want %s", got, want)
	}
}

func TestSemanticDigestIsCanonicalAndIgnoresResolvedDate(t *testing.T) {
	document, err := softwarelock.Parse(validLock(strings.Repeat("d", 64)))
	if err != nil {
		t.Fatal(err)
	}
	first, err := document.SemanticDigest()
	if err != nil {
		t.Fatal(err)
	}

	document.Resolved = "2026-08-21"
	rapid := document.Units["uv:rapid-mlx:rapid-mlx"]
	rapid.Artifacts[0], rapid.Artifacts[1] = rapid.Artifacts[1], rapid.Artifacts[0]
	rapid.Dependencies[0], rapid.Dependencies[1] = rapid.Dependencies[1], rapid.Dependencies[0]
	document.Units["uv:rapid-mlx:rapid-mlx"] = rapid
	second, err := document.SemanticDigest()
	if err != nil {
		t.Fatal(err)
	}

	if first != second {
		t.Errorf("SemanticDigest() changed across date/list ordering: %s != %s", first, second)
	}
}

func TestClosureDigestExcludesUnrelatedSelections(t *testing.T) {
	document, err := softwarelock.Parse(validLock(strings.Repeat("d", 64)))
	if err != nil {
		t.Fatal(err)
	}
	before, err := document.ClosureDigest("rapid-mlx")
	if err != nil {
		t.Fatal(err)
	}

	llama := document.Units["homebrew:system:llama-swap"]
	llama.Version = "9.9.9"
	document.Units["homebrew:system:llama-swap"] = llama
	after, err := document.ClosureDigest("rapid-mlx")
	if err != nil {
		t.Fatal(err)
	}

	if before != after {
		t.Errorf("ClosureDigest() included unrelated selection: %s != %s", before, after)
	}
}

func TestValidateRejectsInvalidClosureShapes(t *testing.T) {
	base, err := softwarelock.Parse(validLock(strings.Repeat("d", 64)))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		mutate func(softwarelock.Document) softwarelock.Document
		want   string
	}{
		{
			name: "dependency cycle",
			mutate: func(document softwarelock.Document) softwarelock.Document {
				mlx := document.Units["uv:rapid-mlx:mlx"]
				mlx.Dependencies = []string{"uv:rapid-mlx:rapid-mlx"}
				document.Units["uv:rapid-mlx:mlx"] = mlx
				return document
			},
			want: "dependency cycle",
		},
		{
			name: "orphan unit",
			mutate: func(document softwarelock.Document) softwarelock.Document {
				document.Units["uv:orphan:unused"] = softwarelock.Unit{
					Adapter: "uv", Scope: "orphan", NativeName: "unused", Version: "1.0",
					Revision: "source/revision",
				}
				return document
			},
			want: "is not reachable from any selection",
		},
		{
			name: "cross-adapter dependency",
			mutate: func(document softwarelock.Document) softwarelock.Document {
				rapid := document.Units["uv:rapid-mlx:rapid-mlx"]
				rapid.Dependencies = []string{"homebrew:system:llama-swap"}
				document.Units["uv:rapid-mlx:rapid-mlx"] = rapid
				return document
			},
			want: "crosses adapter boundary",
		},
		{
			name: "unverifiable unit",
			mutate: func(document softwarelock.Document) softwarelock.Document {
				mlx := document.Units["uv:rapid-mlx:mlx"]
				mlx.Revision = ""
				mlx.Artifacts = nil
				document.Units["uv:rapid-mlx:mlx"] = mlx
				return document
			},
			want: "requires an exact revision or at least one hashed artifact",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			document := cloneDocument(base)
			document = tt.mutate(document)
			err := document.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateAgainstRefusesCatalogIdentityAndAdapterDrift(t *testing.T) {
	catalogBytes := validCatalog()
	supply, err := catalog.Parse(catalogBytes)
	if err != nil {
		t.Fatal(err)
	}
	document, err := softwarelock.Parse(validLock(strings.Repeat("d", 64)))
	if err != nil {
		t.Fatal(err)
	}
	document.Selections["llama-swap"] = softwarelock.Selection{
		Method: "system-package", Adapter: "uv", RecipeRevision: "llama-swap-homebrew/v1", RootUnit: "homebrew:system:llama-swap",
	}
	root := document.Units["homebrew:system:llama-swap"]
	root.Adapter = "uv"
	document.Units["homebrew:system:llama-swap"] = root

	err = document.ValidateAgainst(supply, catalog.SnapshotDigest(catalogBytes))
	if err == nil {
		t.Fatal("ValidateAgainst() succeeded, want catalog drift")
	}
	for _, wanted := range []string{"catalog digest mismatch", "catalog selects \"homebrew\"", "no catalog recipe for adapter \"uv\""} {
		if !strings.Contains(err.Error(), wanted) {
			t.Errorf("ValidateAgainst() error does not contain %q: %v", wanted, err)
		}
	}
}

func TestValidateAgainstRefusesLockOutsideCatalogPolicy(t *testing.T) {
	catalogBytes := validCatalog()
	supply, err := catalog.Parse(catalogBytes)
	if err != nil {
		t.Fatal(err)
	}
	base, err := softwarelock.Parse(validLock(catalog.SnapshotDigest(catalogBytes)))
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		version  string
		excluded bool
	}{{version: "0.9.0"}, {version: "1.4.0", excluded: true}} {
		t.Run(tt.version, func(t *testing.T) {
			document := cloneDocument(base)
			root := document.Units["homebrew:system:llama-swap"]
			root.Version = tt.version
			document.Units["homebrew:system:llama-swap"] = root
			if tt.excluded {
				pkg := supply.Packages["llama-swap"]
				recipe := pkg.Recipes["homebrew"]
				recipe.Exclude = []string{tt.version}
				pkg.Recipes["homebrew"] = recipe
				supply.Packages["llama-swap"] = pkg
			}

			err := document.ValidateAgainst(supply, catalog.SnapshotDigest(catalogBytes))
			if err == nil || !strings.Contains(err.Error(), "closure does not satisfy catalog policy") {
				t.Fatalf("ValidateAgainst() error = %v, want policy refusal", err)
			}
		})
	}
}

func cloneDocument(document softwarelock.Document) softwarelock.Document {
	clone := document
	clone.Selections = make(map[string]softwarelock.Selection, len(document.Selections))
	for id, selection := range document.Selections {
		clone.Selections[id] = selection
	}
	clone.Units = make(map[string]softwarelock.Unit, len(document.Units))
	for id, unit := range document.Units {
		unit.Dependencies = append([]string(nil), unit.Dependencies...)
		unit.Artifacts = append([]softwarelock.Artifact(nil), unit.Artifacts...)
		clone.Units[id] = unit
	}
	return clone
}

func validLock(catalogDigest string) []byte {
	return []byte(fmt.Sprintf(`schema: temper-software-lock/v1
catalog:
  schema: temper-software-supply/v1
  sequence: 42
  sha256: %s
target: {os: darwin, arch: arm64, distribution: macos, distribution_version: "15.6"}
resolved: 2026-08-20
selections:
  llama-swap:
    method: system-package
    adapter: homebrew
    recipe_revision: llama-swap-homebrew/v1
    root_unit: homebrew:system:llama-swap
  rapid-mlx:
    method: python-environment
    adapter: uv
    recipe_revision: rapid-mlx-uv/v1
    root_unit: uv:rapid-mlx:rapid-mlx
units:
  homebrew:system:llama-swap:
    adapter: homebrew
    scope: system
    native_name: llama-swap
    version: 1.3.0
    revision: homebrew/core/abcdef
    dependencies: []
    artifacts:
      - {locator: "https://example.invalid/llama-swap-a.tar.gz", sha256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}
      - {locator: "https://example.invalid/llama-swap-b.tar.gz", sha256: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb}
  uv:rapid-mlx:rapid-mlx:
    adapter: uv
    scope: rapid-mlx
    native_name: rapid-mlx
    version: 0.1.5
    dependencies: [uv:rapid-mlx:mlx, uv:rapid-mlx:typing-extensions]
    artifacts:
      - {locator: "https://example.invalid/rapid-mlx-a.whl", sha256: cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc}
      - {locator: "https://example.invalid/rapid-mlx-b.whl", sha256: dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd}
  uv:rapid-mlx:mlx:
    adapter: uv
    scope: rapid-mlx
    native_name: mlx
    version: 1.2.0
    dependencies: []
    artifacts:
      - {locator: "https://example.invalid/mlx.whl", sha256: eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee}
  uv:rapid-mlx:typing-extensions:
    adapter: uv
    scope: rapid-mlx
    native_name: typing-extensions
    version: 4.12.0
    dependencies: []
    artifacts:
      - {locator: "https://example.invalid/typing-extensions.whl", sha256: ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff}
`, catalogDigest))
}

func validCatalog() []byte {
	return []byte(`schema: temper-software-supply/v1
sequence: 42
published_at: 2026-08-20T18:30:00Z
methods:
  system-package: {description: Shared target package manager}
  python-environment: {description: Temper-owned Python environment}
adapters:
  homebrew: {method: system-package, protocol: temper-installer-adapter/v1, effect_model: shared}
  uv: {method: python-environment, protocol: temper-installer-adapter/v1, effect_model: isolated}
target_bindings:
  - {method: system-package, target: {os: darwin, arch: arm64}, adapter: homebrew}
  - {method: python-environment, target: {os: darwin, arch: arm64}, adapter: uv}
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
        exclude: []
        gates: [router-smoke.v1]
        tested:
          - {root_version: 1.3.0, closure_digest: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, target: {os: darwin, arch: arm64}, evidence: results/llama-swap}
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
          - {root_version: 1.2.0, closure_digest: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb, target: {os: darwin, arch: arm64}, evidence: results/mlx}
  rapid-mlx:
    description: MLX model runtime
    recipes:
      uv:
        method: python-environment
        recipe_revision: rapid-mlx-uv/v1
        source: {kind: python-index, index: pypi, distribution: rapid-mlx}
        version_scheme: pep440
        selection: {policy: range, constraint: ">=0.1,<0.2"}
        dependencies: [{package: mlx, constraint: ">=1.2,<1.3"}]
        exclude: []
        gates: [runtime-smoke.v1]
        tested:
          - {root_version: 0.1.5, closure_digest: cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc, target: {os: darwin, arch: arm64}, evidence: results/rapid-mlx}
`)
}
