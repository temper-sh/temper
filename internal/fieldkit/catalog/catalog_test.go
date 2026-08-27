package catalog_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/fieldkit/catalog"
)

func TestLoadVerifiesSnapshotAndReferencedFiles(t *testing.T) {
	root, catalogPath := fixture(t)
	snapshot, err := catalog.Load(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Document.Revision != 1 || len(snapshot.Entries) != 1 || snapshot.Entries[0].Package.ID != "fixed-smoke" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if err := os.WriteFile(filepath.Join(root, "experiments", "fixed-smoke@1", "prompt.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Load(catalogPath); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("tampered prompt error = %v", err)
	}
}

func TestLoadRefusesAlternateJSONAndEscapingReferences(t *testing.T) {
	root, catalogPath := fixture(t)
	data, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalogPath, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Load(catalogPath); err == nil || !strings.Contains(err.Error(), "not canonical") {
		t.Fatalf("alternate catalog error = %v", err)
	}

	_, catalogPath = fixture(t)
	data, _ = os.ReadFile(catalogPath)
	data = []byte(strings.Replace(string(data), "experiments/fixed-smoke@1/package.json", "../package.json", 1))
	if err := os.WriteFile(catalogPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Load(catalogPath); err == nil || !strings.Contains(err.Error(), "invalid identity, path, or hash") {
		t.Fatalf("escaping reference error = %v", err)
	}
	_ = root
}

func TestEvaluateSeparatesHardApplicabilityFromAdvisoryRelevance(t *testing.T) {
	promoted := promotedPackage()
	promoted.Relevance = []catalog.Signal{{
		ID: "missing-48gb-witness", Reason: "this memory class lacks a witness",
		When: catalog.Predicate{
			OS: "darwin", Arch: "arm64", Distribution: "macos",
			MinPhysicalMemoryMiB: 49152, MinWiredLimitMiB: 30000,
		},
	}}
	facts := machineFacts(65536, 45000)
	applicability, signals := catalog.Evaluate(promoted, facts)
	if !applicability.Applicable || len(signals) != 1 {
		t.Fatalf("applicability = %#v, signals = %#v", applicability, signals)
	}

	facts = machineFacts(16384, 10000)
	applicability, signals = catalog.Evaluate(promoted, facts)
	if applicability.Applicable || len(signals) != 0 || !strings.Contains(strings.Join(applicability.Reasons, " "), "physical-memory") {
		t.Fatalf("applicability = %#v, signals = %#v", applicability, signals)
	}
}

func TestPackageValidationEnforcesFixedAndAdaptiveAttemptBounds(t *testing.T) {
	fixed := promotedPackage()
	if err := fixed.Validate(); err != nil {
		t.Fatal(err)
	}
	fixed.Bounds.MaximumAttempts = 2
	if err := fixed.Validate(); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("fixed bounds error = %v", err)
	}
	adaptive := promotedPackage()
	adaptive.Kind = "bounded-adaptive"
	adaptive.Bounds.MaximumAttempts = 1
	if err := adaptive.Validate(); err == nil || !strings.Contains(err.Error(), "at least two") {
		t.Fatalf("adaptive bounds error = %v", err)
	}
}

func TestCatalogAvailabilityRetainsHistoryButAllowsOnlyOneActiveRevision(t *testing.T) {
	hash := strings.Repeat("a", 64)
	document := catalog.Document{
		Schema: catalog.CatalogSchemaV1, Revision: 2, PromotedAt: "2026-08-25T20:00:00Z",
		Experiments: []catalog.Reference{
			{ID: "experiment", Revision: 1, Availability: "active", AvailabilityReason: "first", PackagePath: "one/package.json", PackageSHA256: hash},
			{ID: "experiment", Revision: 2, Availability: "active", AvailabilityReason: "second", PackagePath: "two/package.json", PackageSHA256: hash},
		},
	}
	if err := document.Validate(); err == nil || !strings.Contains(err.Error(), "more than one active") {
		t.Fatalf("multiple active error = %v", err)
	}
	document.Experiments[0].Availability = "retired"
	document.Experiments[0].AvailabilityReason = "superseded by revision 2"
	if err := document.Validate(); err != nil {
		t.Fatal(err)
	}
	document.Experiments[0].AvailabilityReason = ""
	if err := document.Validate(); err == nil || !strings.Contains(err.Error(), "no availability reason") {
		t.Fatalf("missing reason error = %v", err)
	}
}

func fixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	packageRoot := filepath.Join(root, "experiments", "fixed-smoke@1")
	if err := os.MkdirAll(packageRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	prompt := []byte("# Fixed smoke\n")
	plan := []byte("schema: fake-plan/v1\n")
	if err := os.WriteFile(filepath.Join(packageRoot, "prompt.md"), prompt, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageRoot, "plan.yaml"), plan, 0o644); err != nil {
		t.Fatal(err)
	}
	promoted := promotedPackage()
	promoted.Prompt.SHA256 = catalog.Digest(prompt)
	promoted.Mechanics.Plan.SHA256 = catalog.Digest(plan)
	packageData := canonicalJSON(t, promoted)
	if err := os.WriteFile(filepath.Join(packageRoot, "package.json"), packageData, 0o644); err != nil {
		t.Fatal(err)
	}
	document := catalog.Document{
		Schema: catalog.CatalogSchemaV1, Revision: 1, PromotedAt: "2026-08-25T20:00:00Z",
		Experiments: []catalog.Reference{{
			ID: "fixed-smoke", Revision: 1,
			Availability: "active", AvailabilityReason: "fixture is active",
			PackagePath: "experiments/fixed-smoke@1/package.json", PackageSHA256: catalog.Digest(packageData),
		}},
	}
	catalogPath := filepath.Join(root, "catalog.json")
	if err := os.WriteFile(catalogPath, canonicalJSON(t, document), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, catalogPath
}

func promotedPackage() catalog.Package {
	hash := strings.Repeat("a", 64)
	return catalog.Package{
		Schema: catalog.PackageSchemaV1, ID: "fixed-smoke", Revision: 1,
		Origin: catalog.Origin{
			ExperimentID: "fixed-smoke", ExperimentRevision: 1, ExperimentSHA256: hash,
			PromotionID: "fixed-smoke-field-kit", PromotionRevision: 1, PromotionSHA256: hash,
		},
		Kind: "fixed", Title: "Fixed smoke", Question: "Does the fixed smoke pass?", Decision: "Whether to retain the candidate.",
		Applicability: catalog.Predicate{
			OS: "darwin", Arch: "arm64", Distribution: "macos",
			MinPhysicalMemoryMiB: 32768, MinWiredLimitMiB: 20000,
		},
		Relevance: []catalog.Signal{},
		Cost: catalog.Cost{
			FixedRuntimeMinutes: 5, SetupMinutesMin: 0, SetupMinutesMax: 1,
			MemoryPressure: "low", ServiceDisruption: "none", PaidProvider: "none",
		},
		Consent: catalog.Consent{
			Choices: []string{"run-fixed-smoke"}, Reads: []string{"machine-facts"}, Writes: []string{"isolated-report"},
			NetworkDestinations: []string{}, LocalOutput: "local-only", Cleanup: "remove-isolated-report-on-request", RenewedConsent: []string{},
		},
		Parameters: []catalog.Parameter{{ID: "arm", Kind: "fixed", Fixed: "candidate", Required: true, Values: []string{}}},
		Bounds:     catalog.Bounds{MaximumAttempts: 1, MaximumRuntimeMinutes: 5},
		StopRules:  []catalog.StopRule{{ID: "timeout", Observation: "five minutes elapsed", Action: "stop"}},
		Mechanics: catalog.Mechanics{
			TemperProtocol: "temper-field-kit-binding/v1",
			Plan:           catalog.FileIdentity{Path: "plan.yaml", SHA256: hash}, ExternalInputs: []catalog.FileIdentity{},
			Resume: "restart-the-single-attempt", Interruption: "stop-isolated-processes-and-preserve-report",
		},
		Report: catalog.Report{
			Schema: "field-kit-experiment-report/v1", RequiredConditions: []string{"machine", "temper-binding"},
			Sensitivity: "review-before-sharing", Submission: "explicit-export-only",
		},
		Prompt:       catalog.FileIdentity{Path: "prompt.md", SHA256: hash},
		Invalidation: []string{"any promoted material changes"},
	}
}

func machineFacts(memoryMiB, wiredMiB int64) catalog.MachineFacts {
	return catalog.MachineFacts{
		Schema:        catalog.MachineSchemaV1,
		Target:        catalog.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6"},
		HardwareModel: "Mac17,3", Chip: "Apple M5", OSBuild: "24G90",
		PhysicalMemoryBytes: memoryMiB * 1024 * 1024, WiredLimitMiB: wiredMiB,
		MetalDeviceMemoryMiB: memoryMiB * 81 / 100, MetalDeviceMemorySource: "predicted-metal-81-percent", WiredLimitSource: "live-sysctl",
	}
}

func canonicalJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}
