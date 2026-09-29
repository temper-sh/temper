package lockfile_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software"

	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
)

func TestPortableTargetIsExplicitAndChangesLockIdentity(t *testing.T) {
	document, err := softwarelock.Parse(validLock(strings.Repeat("d", 64)))
	if err != nil {
		t.Fatal(err)
	}
	document.Target = software.Target{OS: "darwin", Arch: "arm64"}
	host := software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "26.6.1"}
	if document.SupportsHost(host) {
		t.Fatal("old exact lock silently gained portable semantics")
	}
	exact, err := document.SemanticDigest()
	if err != nil {
		t.Fatal(err)
	}
	document.TargetMode = "compatible"
	if !document.SupportsHost(host) {
		t.Fatal("explicit portable lock rejected compatible host")
	}
	if document.SupportsHost(software.Target{OS: "linux", Arch: "arm64"}) {
		t.Fatal("portable lock accepted another OS")
	}
	if document.SupportsHost(software.Target{OS: "darwin", Arch: "amd64"}) {
		t.Fatal("portable lock accepted another architecture")
	}
	portable, err := document.SemanticDigest()
	if err != nil {
		t.Fatal(err)
	}
	if portable == exact {
		t.Fatal("target compatibility did not enter lock identity")
	}
	document.Target = host
	if err := document.Validate(); err == nil {
		t.Fatal("portable lock accepted observed host distribution fields")
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
			want: "provenance must identify",
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
			want: "must be experiment or execution",
		},
		{
			name: "selection provenance has no matching identity",
			mutate: func(document softwarelock.Document) softwarelock.Document {
				document.Selections["llama-swap"] = softwarelock.Selection{
					Provenance: softwarelock.ProvenanceExecution,
					Method:     "system-package", Adapter: "homebrew", RecipeRevision: "llama-swap-homebrew/v1", RootUnit: "homebrew:system:llama-swap",
				}
				return document
			},
			want: "no execution identity",
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
  experiment:
    schema: test-input/v1
    id: fixture
    definition_sha256: %s
requires: []
target: {os: darwin, arch: arm64, distribution: macos, distribution_version: "15.6"}
resolved: 2026-08-20
selections:
  llama-swap:
    provenance: experiment
    method: system-package
    adapter: homebrew
    recipe_revision: llama-swap-homebrew/v1
    root_unit: homebrew:system:llama-swap
  rapid-mlx:
    provenance: experiment
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
