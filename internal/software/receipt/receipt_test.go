package receipt_test

import (
	"strings"
	"testing"
	"time"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/installplan"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/receipt"
)

const receiptRoot = "/tmp/temper-receipt-test"

func TestReceiptReuseDependsOnSoftwareRatherThanExperimentDefinition(t *testing.T) {
	desired := receiptLock(t)
	plan, observed := receiptPlan(t, desired)
	installed, err := receipt.Build(desired, plan, observed, time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	desired.Provenance.Experiment.DefinitionSHA256 = strings.Repeat("c", 64)
	if err := installed.ValidateAgainst(desired, plan.Installation); err != nil {
		t.Fatalf("unchanged software rejected after an experiment change: %v", err)
	}
	desired.Provenance = softwarelock.Provenance{Execution: &softwarelock.ExecutionIdentity{Schema: "temper-execution-lock/v2", Profile: "another-profile", SHA256: strings.Repeat("e", 64)}}
	selection := desired.Selections["tool"]
	selection.Provenance = softwarelock.ProvenanceExecution
	desired.Selections["tool"] = selection
	if err := installed.ValidateAgainst(desired, plan.Installation); err != nil {
		t.Fatalf("unchanged software rejected from another source: %v", err)
	}
	for _, change := range []struct {
		name  string
		apply func(*softwarelock.Document)
	}{
		{"version", func(d *softwarelock.Document) {
			u := d.Units["uv:probe:tool"]
			u.Version = "2.0.0"
			d.Units["uv:probe:tool"] = u
		}},
		{"artifact bytes", func(d *softwarelock.Document) {
			u := d.Units["uv:probe:tool"]
			u.Artifacts[0].SHA256 = strings.Repeat("d", 64)
		}},
		{"target", func(d *softwarelock.Document) { d.Target.DistributionVersion = "26.0" }},
	} {
		t.Run(change.name, func(t *testing.T) {
			changed := receiptLock(t)
			change.apply(&changed)
			if err := installed.ValidateAgainst(changed, plan.Installation); err == nil {
				t.Fatal("changed installed software was accepted")
			}
		})
	}
}

func TestBuildRoundTripsCanonicalObservedHistory(t *testing.T) {
	desired := receiptLock(t)
	plan, observed := receiptPlan(t, desired)
	wantTime := time.Date(2026, 8, 23, 9, 10, 11, 0, time.UTC)

	document, err := receipt.Build(desired, plan, observed, wantTime)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	data, err := receipt.Marshal(document)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	parsed, err := receipt.Parse(data)
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v", err)
	}
	if err := parsed.ValidateAgainst(desired, plan.Installation); err != nil {
		t.Fatalf("ValidateAgainst() error = %v", err)
	}
	if parsed.ObservedAt != "2026-08-23T09:10:11Z" || len(receipt.Digest(data)) != 64 {
		t.Fatalf("receipt identity = observed %q digest %q", parsed.ObservedAt, receipt.Digest(data))
	}
	previous := parsed.Previous()
	if previous.LockDigest != plan.LockDigest || previous.Units["uv:probe:tool"].Ownership != installplan.OwnershipTemperAdded {
		t.Fatalf("Previous() = %#v", previous)
	}
}

func TestParseRefusesAlternateBytesAndUnknownFields(t *testing.T) {
	desired := receiptLock(t)
	plan, observed := receiptPlan(t, desired)
	document, err := receipt.Build(desired, plan, observed, time.Date(2026, 8, 23, 9, 10, 11, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	data, err := receipt.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}

	for _, input := range [][]byte{
		append(append([]byte(nil), data...), []byte("# same values, different bytes\n")...),
		[]byte(strings.Replace(string(data), "observed_at:", "unexpected: value\nobserved_at:", 1)),
	} {
		if _, err := receipt.Parse(input); err == nil {
			t.Fatalf("Parse() accepted noncanonical or unknown input:\n%s", input)
		}
	}
}

func TestVerifyObservationRefusesProviderDrift(t *testing.T) {
	desired := receiptLock(t)
	plan, observed := receiptPlan(t, desired)
	document, err := receipt.Build(desired, plan, observed, time.Date(2026, 8, 23, 9, 10, 11, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	drifted := observed.Units["uv:probe:tool"]
	drifted.Version = "2.0.0"
	observed.Units["uv:probe:tool"] = drifted
	if err := document.VerifyObservation(observed); err == nil || !strings.Contains(err.Error(), "provider drift") {
		t.Fatalf("VerifyObservation() error = %v, want drift refusal", err)
	}
}

func receiptLock(t *testing.T) softwarelock.Document {
	t.Helper()
	document := softwarelock.Document{
		Schema: softwarelock.SchemaV1,
		Provenance: softwarelock.Provenance{Experiment: &softwarelock.ExperimentIdentity{
			Schema: "field-kit-experiment/v1", ID: "receipt-fixture", DefinitionSHA256: strings.Repeat("a", 64),
		}},
		Target:   software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6"},
		Resolved: "2026-08-23",
		Selections: map[string]softwarelock.Selection{
			"tool": {Provenance: softwarelock.ProvenanceExperiment, Method: "python-environment", Adapter: "uv", RecipeRevision: "tool/v1", RootUnit: "uv:probe:tool"},
		},
		Units: map[string]softwarelock.Unit{
			"uv:probe:tool": {
				Adapter: "uv", Scope: "probe", NativeName: "tool", Version: "1.0.0",
				Dependencies: []string{}, Artifacts: []software.Artifact{{Locator: "https://example.invalid/tool.whl", SHA256: strings.Repeat("b", 64)}},
			},
		},
	}
	if err := document.Validate(); err != nil {
		t.Fatalf("fixture lock invalid: %v", err)
	}
	return document
}

func receiptPlan(t *testing.T, desired softwarelock.Document) (installplan.Plan, installplan.Observation) {
	t.Helper()
	location := receiptRoot + "/software/installations/probe/environment/tool"
	before := installplan.Observation{Target: desired.Target, Root: receiptRoot, Units: map[string]installplan.ObservedUnit{
		"uv:probe:tool": {InstallLocation: location},
	}}
	observed := installplan.Observation{Target: desired.Target, Root: receiptRoot, Units: map[string]installplan.ObservedUnit{
		"uv:probe:tool": {
			Present: true, Adapter: "uv", Scope: "probe", NativeName: "tool", Version: "1.0.0", Dependencies: []string{},
			Artifacts: []software.Artifact{{Locator: "https://example.invalid/tool.whl", SHA256: strings.Repeat("b", 64)}}, Location: location,
		},
	}}
	plan, err := installplan.Build(desired, installplan.Installation{ID: "probe", Root: receiptRoot}, map[string]installplan.EffectModel{"uv": installplan.EffectIsolated}, before, installplan.State{})
	if err != nil {
		t.Fatalf("Build plan: %v", err)
	}
	return plan, observed
}
