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

func TestValidateRejectsIncompleteArchiveArtifactMetadata(t *testing.T) {
	document, err := softwarelock.Parse(validLock(strings.Repeat("d", 64)))
	if err != nil {
		t.Fatal(err)
	}
	unit := document.Units["homebrew:system:llama-swap"]
	unit.Artifacts[0].UnpackedSize = 123
	unit.Artifacts[0].InstalledEntries = 4
	document.Units["homebrew:system:llama-swap"] = unit

	err = document.Validate()
	if err == nil || !strings.Contains(err.Error(), "require an archive format") {
		t.Fatalf("Validate() error = %v, want incomplete archive metadata refusal", err)
	}
}

func TestSemanticDigestIncludesArtifactInstallationMetadata(t *testing.T) {
	document, err := softwarelock.Parse(validLock(strings.Repeat("d", 64)))
	if err != nil {
		t.Fatal(err)
	}
	before, err := document.SemanticDigest()
	if err != nil {
		t.Fatal(err)
	}
	unit := document.Units["homebrew:system:llama-swap"]
	unit.Artifacts[0].Size = 123
	document.Units["homebrew:system:llama-swap"] = unit
	after, err := document.SemanticDigest()
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("SemanticDigest() ignored artifact size")
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

func TestParseAcceptsADirectExperimentLockWithoutCatalogProvenance(t *testing.T) {
	input := strings.Replace(string(validLock(strings.Repeat("d", 64))), `  catalog:
    schema: temper-software-supply/v1
    sequence: 42
    sha256: `+strings.Repeat("d", 64), `  experiment:
    schema: field-kit-experiment/v1
    id: llama-cpp-pr-smoke
    definition_sha256: `+strings.Repeat("9", 64), 1)
	input = strings.ReplaceAll(input, "    provenance: catalog", "    provenance: experiment")

	document, err := softwarelock.Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if document.Provenance.Catalog != nil || document.Provenance.Experiment == nil || document.Provenance.Experiment.ID != "llama-cpp-pr-smoke" {
		t.Fatalf("experiment provenance = %#v", document.Provenance)
	}
}

func TestParseAcceptsCatalogBackedExperimentProvenance(t *testing.T) {
	catalogBytes := validCatalog()
	input := strings.Replace(string(validLock(catalog.SnapshotDigest(catalogBytes))), "requires: []", `  experiment:
    schema: labs-experiment/v1
    id: rapid-mlx-candidate
    definition_sha256: `+strings.Repeat("8", 64)+`
requires: []`, 1)
	input = strings.Replace(input, "  rapid-mlx:\n    provenance: catalog", "  rapid-mlx:\n    provenance: experiment", 1)
	input = strings.Replace(input, "    recipe_revision: rapid-mlx-uv/v1", "    recipe_revision: rapid-mlx-pr-481/v1", 1)

	document, err := softwarelock.Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if document.Provenance.Catalog == nil || document.Provenance.Experiment == nil {
		t.Fatalf("combined provenance = %#v, want catalog and experiment", document.Provenance)
	}
	supply, err := catalog.Parse(catalogBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := document.ValidateAgainst(supply, catalog.SnapshotDigest(catalogBytes)); err != nil {
		t.Fatalf("ValidateAgainst() error = %v", err)
	}
}

func TestValidateAgainstRefusesADirectExperimentLockWithoutCatalogProvenance(t *testing.T) {
	input := strings.Replace(string(validLock(strings.Repeat("d", 64))), `  catalog:
    schema: temper-software-supply/v1
    sequence: 42
    sha256: `+strings.Repeat("d", 64), `  experiment:
    schema: field-kit-experiment/v1
    id: llama-cpp-pr-smoke
    definition_sha256: `+strings.Repeat("9", 64), 1)
	input = strings.ReplaceAll(input, "    provenance: catalog", "    provenance: experiment")
	document, err := softwarelock.Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	supply, err := catalog.Parse(validCatalog())
	if err != nil {
		t.Fatal(err)
	}

	err = document.ValidateAgainst(supply, catalog.SnapshotDigest(validCatalog()))
	if err == nil || !strings.Contains(err.Error(), "no catalog provenance") {
		t.Fatalf("ValidateAgainst() error = %v, want direct-experiment refusal", err)
	}
}

func TestValidateRefusesMissingOrMalformedProvenance(t *testing.T) {
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
			name: "no provenance",
			mutate: func(document softwarelock.Document) softwarelock.Document {
				document.Provenance = softwarelock.Provenance{}
				return document
			},
			want: "provenance must contain",
		},
		{
			name: "bad experiment digest",
			mutate: func(document softwarelock.Document) softwarelock.Document {
				document.Provenance.Experiment = &softwarelock.ExperimentIdentity{Schema: "labs-experiment/v1", ID: "candidate", DefinitionSHA256: "moving"}
				return document
			},
			want: "definition_sha256",
		},
		{
			name: "selection omits provenance",
			mutate: func(document softwarelock.Document) softwarelock.Document {
				selection := document.Selections["llama-swap"]
				selection.Provenance = ""
				document.Selections["llama-swap"] = selection
				return document
			},
			want: "must be catalog or experiment",
		},
		{
			name: "selection provenance has no matching identity",
			mutate: func(document softwarelock.Document) softwarelock.Document {
				document.Selections["llama-swap"] = softwarelock.Selection{
					Provenance: softwarelock.ProvenanceExperiment,
					Method:     "system-package", Adapter: "homebrew", RecipeRevision: "llama-swap-homebrew/v1", RootUnit: "homebrew:system:llama-swap",
				}
				return document
			},
			want: "lock has no experiment identity",
		},
		{
			name: "duplicate base requirement",
			mutate: func(document softwarelock.Document) softwarelock.Document {
				digest := strings.Repeat("7", 64)
				document.Requires = []softwarelock.InstallationRequirement{{SoftwareLockDigest: digest}, {SoftwareLockDigest: digest}}
				return document
			},
			want: "requires repeats",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.mutate(cloneDocument(base)).Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want %q", err, tt.want)
			}
		})
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

func TestSemanticDigestCanonicalizesRequiredInstallationOrder(t *testing.T) {
	document, err := softwarelock.Parse(validLock(strings.Repeat("d", 64)))
	if err != nil {
		t.Fatal(err)
	}
	document.Requires = []softwarelock.InstallationRequirement{
		{SoftwareLockDigest: strings.Repeat("b", 64)},
		{SoftwareLockDigest: strings.Repeat("a", 64)},
	}
	first, err := document.SemanticDigest()
	if err != nil {
		t.Fatal(err)
	}
	document.Requires[0], document.Requires[1] = document.Requires[1], document.Requires[0]
	second, err := document.SemanticDigest()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("SemanticDigest() changed with requirement order: %s != %s", first, second)
	}
}

func TestSemanticDigestCanonicalizesAbsentAndEmptyRequirements(t *testing.T) {
	document, err := softwarelock.Parse(validLock(strings.Repeat("d", 64)))
	if err != nil {
		t.Fatal(err)
	}
	document.Requires = nil
	withoutField, err := document.SemanticDigest()
	if err != nil {
		t.Fatal(err)
	}
	document.Requires = []softwarelock.InstallationRequirement{}
	withEmptyField, err := document.SemanticDigest()
	if err != nil {
		t.Fatal(err)
	}
	if withoutField != withEmptyField {
		t.Errorf("SemanticDigest() distinguishes absent and empty requirements: %s != %s", withoutField, withEmptyField)
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
		Provenance: softwarelock.ProvenanceCatalog,
		Method:     "system-package", Adapter: "uv", RecipeRevision: "llama-swap-homebrew/v1", RootUnit: "homebrew:system:llama-swap",
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
provenance:
  catalog:
    schema: temper-software-supply/v1
    sequence: 42
    sha256: %s
requires: []
target: {os: darwin, arch: arm64, distribution: macos, distribution_version: "15.6"}
resolved: 2026-08-20
selections:
  llama-swap:
    provenance: catalog
    method: system-package
    adapter: homebrew
    recipe_revision: llama-swap-homebrew/v1
    root_unit: homebrew:system:llama-swap
  rapid-mlx:
    provenance: catalog
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
    dependencies: [uv:rapid-mlx:cpython, uv:rapid-mlx:mlx, uv:rapid-mlx:typing-extensions]
    artifacts:
      - {locator: "https://example.invalid/rapid-mlx-a.whl", sha256: cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc}
      - {locator: "https://example.invalid/rapid-mlx-b.whl", sha256: dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd}
  uv:rapid-mlx:cpython:
    adapter: uv
    scope: rapid-mlx
    native_name: cpython
    version: 3.12.11
    revision: python-build/20260820
    dependencies: []
    artifacts:
      - {locator: "https://example.invalid/cpython.tar.zst", sha256: 7777777777777777777777777777777777777777777777777777777777777777}
  uv:rapid-mlx:mlx:
    adapter: uv
    scope: rapid-mlx
    native_name: mlx
    version: 1.2.0
    dependencies: [uv:rapid-mlx:cpython]
    artifacts:
      - {locator: "https://example.invalid/mlx.whl", sha256: eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee}
  uv:rapid-mlx:typing-extensions:
    adapter: uv
    scope: rapid-mlx
    native_name: typing-extensions
    version: 4.12.0
    dependencies: [uv:rapid-mlx:cpython]
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
  cpython:
    description: uv-managed CPython runtime
    recipes:
      uv:
        method: python-environment
        recipe_revision: cpython-uv/v1
        source: {kind: python-runtime, implementation: cpython}
        version_scheme: pep440
        selection: {policy: range, constraint: ">=3.12,<3.13"}
        dependencies: []
        exclude: []
        gates: [python-smoke.v1]
        tested:
          - {root_version: 3.12.11, closure_digest: dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd, target: {os: darwin, arch: arm64}, evidence: results/cpython}
  mlx:
    description: MLX framework
    recipes:
      uv:
        method: python-environment
        recipe_revision: mlx-uv/v1
        source: {kind: python-index, index: pypi, distribution: mlx}
        version_scheme: pep440
        selection: {policy: range, constraint: ">=1,<2"}
        dependencies: [{package: cpython, constraint: ">=3.12,<3.13"}]
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
        dependencies:
          - {package: cpython, constraint: ">=3.12,<3.13"}
          - {package: mlx, constraint: ">=1.2,<1.3"}
        exclude: []
        gates: [runtime-smoke.v1]
        tested:
          - {root_version: 0.1.5, closure_digest: cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc, target: {os: darwin, arch: arm64}, evidence: results/rapid-mlx}
`)
}
