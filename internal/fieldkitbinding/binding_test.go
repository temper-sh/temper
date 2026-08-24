package fieldkitbinding_test

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/fieldkitbinding"
	manifestlock "github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/installplan"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/receipt"
)

func TestBuildBindsExactInputsAndRecursivelyExplicitRequirements(t *testing.T) {
	foundation := makeInstallation(t, "foundation", nil)
	base := makeInstallation(t, "field-kit-base", []installationFixture{foundation})
	experiment := makeInstallation(t, "runtime-smoke", []installationFixture{base})
	manifestData := manifestLockData(t)

	document, err := fieldkitbinding.Build(fieldkitbinding.Inputs{
		TemperBinary:       []byte("temper-binary"),
		Machine:            machineFacts(),
		ManifestLock:       manifestData,
		RenderedGeneration: strings.Repeat("9", 64),
		Installations: []fieldkitbinding.InstallationInput{
			{Lock: foundation.Lock, ReceiptData: foundation.ReceiptData},
			{Lock: base.Lock, ReceiptData: base.ReceiptData},
			{Lock: experiment.Lock, ReceiptData: experiment.ReceiptData},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if document.Schema != fieldkitbinding.SchemaV1 {
		t.Fatalf("schema = %q", document.Schema)
	}
	if document.TemperBinary.SHA256 != digest([]byte("temper-binary")) || document.TemperBinary.OS != "darwin" || document.TemperBinary.Arch != "arm64" {
		t.Fatalf("temper binary = %#v", document.TemperBinary)
	}
	if document.ManifestLock.Schema != manifestlock.SchemaV1 || document.ManifestLock.SHA256 != digest(manifestData) {
		t.Fatalf("manifest lock = %#v", document.ManifestLock)
	}
	if len(document.Installations) != 3 || document.Installations[0].Installation != "foundation" || document.Installations[2].Installation != "runtime-smoke" {
		t.Fatalf("installations = %#v", document.Installations)
	}
	requiredBase := document.Installations[2].Requirements
	if len(requiredBase) != 1 || requiredBase[0].Installation != "field-kit-base" {
		t.Fatalf("experiment requirements = %#v", requiredBase)
	}
	if len(requiredBase[0].Requirements) != 1 || requiredBase[0].Requirements[0].Installation != "foundation" {
		t.Fatalf("recursive requirements = %#v", requiredBase[0].Requirements)
	}

	data, err := fieldkitbinding.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := fieldkitbinding.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parsed, document) {
		t.Fatalf("round trip changed document\n got: %#v\nwant: %#v", parsed, document)
	}
	if fieldkitbinding.Digest(data) == "" {
		t.Fatal("canonical binding has no digest")
	}
}

func TestBuildRequiresDependenciesEarlierInTheOrderedSet(t *testing.T) {
	base := makeInstallation(t, "field-kit-base", nil)
	experiment := makeInstallation(t, "runtime-smoke", []installationFixture{base})

	_, err := fieldkitbinding.Build(validInputs(t, []installationFixture{experiment, base}))
	if err == nil || !strings.Contains(err.Error(), "earlier installation") {
		t.Fatalf("Build() error = %v", err)
	}
}

func TestBuildRefusesAReceiptRequirementThatDoesNotIdentifyTheSuppliedBase(t *testing.T) {
	base := makeInstallation(t, "field-kit-base", nil)
	experiment := makeInstallation(t, "runtime-smoke", []installationFixture{base})
	document, err := receipt.Parse(experiment.ReceiptData)
	if err != nil {
		t.Fatal(err)
	}
	document.Requirements[0].ReceiptSHA256 = strings.Repeat("f", 64)
	experiment.ReceiptData, err = receipt.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}

	_, err = fieldkitbinding.Build(validInputs(t, []installationFixture{base, experiment}))
	if err == nil || !strings.Contains(err.Error(), "required receipt identity") {
		t.Fatalf("Build() error = %v", err)
	}
}

func TestBuildRefusesNoncanonicalReceiptBytes(t *testing.T) {
	base := makeInstallation(t, "field-kit-base", nil)
	base.ReceiptData = append(base.ReceiptData, '\n')

	_, err := fieldkitbinding.Build(validInputs(t, []installationFixture{base}))
	if err == nil || !strings.Contains(err.Error(), "canonical") {
		t.Fatalf("Build() error = %v", err)
	}
}

func TestValidateRequiresTheFullRecursiveIdentity(t *testing.T) {
	foundation := makeInstallation(t, "foundation", nil)
	base := makeInstallation(t, "field-kit-base", []installationFixture{foundation})
	experiment := makeInstallation(t, "runtime-smoke", []installationFixture{base})
	document, err := fieldkitbinding.Build(validInputs(t, []installationFixture{foundation, base, experiment}))
	if err != nil {
		t.Fatal(err)
	}
	document.Installations[2].Requirements[0].Requirements = nil

	err = document.Validate()
	if err == nil || !strings.Contains(err.Error(), "complete earlier identity") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestParseRefusesAlternateBytesForTheSameDocument(t *testing.T) {
	base := makeInstallation(t, "field-kit-base", nil)
	document, err := fieldkitbinding.Build(validInputs(t, []installationFixture{base}))
	if err != nil {
		t.Fatal(err)
	}
	data, err := fieldkitbinding.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')

	_, err = fieldkitbinding.Parse(data)
	if err == nil || !strings.Contains(err.Error(), "canonical") {
		t.Fatalf("Parse() error = %v", err)
	}
}

type installationFixture struct {
	Lock        softwarelock.Document
	LockDigest  string
	ReceiptData []byte
	ReceiptHash string
	ID          string
}

func makeInstallation(t *testing.T, id string, requirements []installationFixture) installationFixture {
	t.Helper()
	target := machineFacts().Target
	requires := make([]softwarelock.InstallationRequirement, len(requirements))
	receiptRequirements := make([]receipt.Requirement, len(requirements))
	for index, required := range requirements {
		requires[index] = softwarelock.InstallationRequirement{SoftwareLockDigest: required.LockDigest}
		receiptRequirements[index] = receipt.Requirement{
			SoftwareLockDigest: required.LockDigest,
			Installation:       required.ID,
			ReceiptSHA256:      required.ReceiptHash,
		}
	}
	unitID := "uv:environment:" + id
	locked := softwarelock.Document{
		Schema: softwarelock.SchemaV1,
		Provenance: softwarelock.Provenance{Experiment: &softwarelock.ExperimentIdentity{
			Schema: "field-kit-experiment/v1", ID: id, DefinitionSHA256: strings.Repeat("a", 64),
		}},
		Requires: requires,
		Target:   target,
		Resolved: "2026-08-24",
		Selections: map[string]softwarelock.Selection{
			id: {Provenance: softwarelock.ProvenanceExperiment, Method: "python-environment", Adapter: "uv", RecipeRevision: "v1", RootUnit: unitID},
		},
		Units: map[string]softwarelock.Unit{
			unitID: {Adapter: "uv", Scope: "environment", NativeName: id, Version: "1.0.0", Revision: "v1", Dependencies: []string{}},
		},
	}
	lockDigest, err := locked.SemanticDigest()
	if err != nil {
		t.Fatal(err)
	}
	root := "/tmp/temper-field-kit-binding"
	receipted := receipt.Document{
		Schema: receipt.SchemaV1, Installation: id, SoftwareLockDigest: lockDigest,
		Target: target, Root: root, ObservedAt: "2026-08-24T10:00:00Z",
		Requirements: receiptRequirements,
		Selections: map[string]receipt.Selection{
			id: {Provenance: softwarelock.ProvenanceExperiment, Method: "python-environment", Adapter: "uv", RecipeRevision: "v1", RootUnit: unitID},
		},
		Units: map[string]receipt.Unit{
			unitID: {
				Adapter: "uv", Scope: "environment", NativeName: id, Version: "1.0.0", Revision: "v1", Dependencies: []string{},
				Location: root + "/software/installations/" + id + "/environment", Ownership: installplan.OwnershipTemperAdded,
			},
		},
	}
	receiptData, err := receipt.Marshal(receipted)
	if err != nil {
		t.Fatal(err)
	}
	return installationFixture{
		Lock: locked, LockDigest: lockDigest, ReceiptData: receiptData,
		ReceiptHash: receipt.Digest(receiptData), ID: id,
	}
}

func validInputs(t *testing.T, installations []installationFixture) fieldkitbinding.Inputs {
	t.Helper()
	inputs := fieldkitbinding.Inputs{
		TemperBinary:       []byte("temper-binary"),
		Machine:            machineFacts(),
		ManifestLock:       manifestLockData(t),
		RenderedGeneration: strings.Repeat("9", 64),
		Installations:      make([]fieldkitbinding.InstallationInput, len(installations)),
	}
	for index, installation := range installations {
		inputs.Installations[index] = fieldkitbinding.InstallationInput{Lock: installation.Lock, ReceiptData: installation.ReceiptData}
	}
	return inputs
}

func machineFacts() machine.Facts {
	return machine.Facts{
		Schema:        machine.FactsSchemaV1,
		Target:        software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6"},
		HardwareModel: "Mac17,3", Chip: "Apple M5", OSBuild: "24G90",
		PhysicalMemoryBytes: 34359738368, MetalDeviceMemoryMiB: 26542,
		MetalDeviceMemorySource: machine.MetalDeviceSourcePredicted,
		WiredLimitMiB:           24576, WiredLimitSource: budget.WiredSourceLive,
	}
}

func manifestLockData(t *testing.T) []byte {
	t.Helper()
	data, err := manifestlock.Marshal(manifestlock.Document{
		Schema: manifestlock.SchemaV1,
		Entries: map[string]manifestlock.Entry{
			"coder": {
				Repo: "example/coder", Revision: strings.Repeat("b", 40),
				Files:    []manifestlock.File{{Name: "coder.gguf", SHA256: strings.Repeat("c", 64)}},
				Resolved: "2026-08-24",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
