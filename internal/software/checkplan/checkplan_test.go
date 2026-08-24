package checkplan_test

import (
	"strings"
	"testing"
	"time"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/checkplan"
	"github.com/temper-sh/temper/internal/software/installplan"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/receipt"
	"github.com/temper-sh/temper/internal/software/rootstate"
)

const checkRoot = "/tmp/temper-software-check-test"

func TestAnalyzeReportsAnExactIsolatedInstallation(t *testing.T) {
	facts := installedFactsFor(t, installplan.EffectIsolated, nil)

	result, err := checkplan.Analyze(facts.input())
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if !result.Exact() || result.ProblemCount() != 0 || len(result.Units) != 1 || result.Units[0].Status != checkplan.UnitExact {
		t.Fatalf("Analyze() = %#v", result)
	}
	if result.ReceiptSHA256 == "" || result.Units[0].Ownership != string(installplan.OwnershipTemperAdded) {
		t.Fatalf("exact identities = receipt %q, unit %#v", result.ReceiptSHA256, result.Units[0])
	}
}

func TestAnalyzeClassifiesTheFirstFailedUnitLayer(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*checkplan.Input)
		wantStatus checkplan.UnitStatus
		wantCode   checkplan.Code
	}{
		{
			name: "provider missing",
			mutate: func(input *checkplan.Input) {
				input.Observed.Units["uv:probe:tool"] = installplan.ObservedUnit{InstallLocation: installplan.InstallationRoot(input.Installation) + "/environment/tool"}
			},
			wantStatus: checkplan.UnitMissing,
			wantCode:   checkplan.CodeProviderMissing,
		},
		{
			name: "provider identity drifted",
			mutate: func(input *checkplan.Input) {
				unit := input.Observed.Units["uv:probe:tool"]
				unit.Version = "9.9.9"
				input.Observed.Units["uv:probe:tool"] = unit
			},
			wantStatus: checkplan.UnitDrifted,
			wantCode:   checkplan.CodeProviderDrift,
		},
		{
			name: "isolated provider escaped the installation",
			mutate: func(input *checkplan.Input) {
				unit := input.Observed.Units["uv:probe:tool"]
				unit.Location = "/opt/unowned/tool"
				unit.InstallLocation = unit.Location
				input.Observed.Units["uv:probe:tool"] = unit
			},
			wantStatus: checkplan.UnitDrifted,
			wantCode:   checkplan.CodeProviderDrift,
		},
		{
			name: "receipt missing",
			mutate: func(input *checkplan.Input) {
				input.Receipt = nil
			},
			wantStatus: checkplan.UnitUnreceipted,
			wantCode:   checkplan.CodeReceiptMissing,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			facts := installedFactsFor(t, installplan.EffectIsolated, nil)
			input := facts.input()
			test.mutate(&input)

			result, err := checkplan.Analyze(input)
			if err != nil {
				t.Fatalf("Analyze() error = %v", err)
			}
			if result.Exact() || result.Units[0].Status != test.wantStatus || !hasFinding(result, test.wantCode, "uv:probe:tool") {
				t.Fatalf("Analyze() = %#v", result)
			}
		})
	}
}

func TestAnalyzeDistinguishesMissingAndDriftingSharedClaims(t *testing.T) {
	t.Run("missing root state is unclaimed", func(t *testing.T) {
		facts := installedFactsFor(t, installplan.EffectShared, nil)
		input := facts.input()
		input.State = nil

		result, err := checkplan.Analyze(input)
		if err != nil {
			t.Fatal(err)
		}
		if result.Units[0].Status != checkplan.UnitUnclaimed || !hasFinding(result, checkplan.CodeClaimMissing, "homebrew:system:tool") {
			t.Fatalf("Analyze() = %#v", result)
		}
	})

	t.Run("claimed identity disagreement is drift", func(t *testing.T) {
		facts := installedFactsFor(t, installplan.EffectShared, nil)
		input := facts.input()
		key := installplan.SharedUnitKey("homebrew", "system", "tool")
		shared := input.State.SharedUnits[key]
		shared.Version = "9.9.9"
		input.State.SharedUnits[key] = shared

		result, err := checkplan.Analyze(input)
		if err != nil {
			t.Fatal(err)
		}
		if result.Units[0].Status != checkplan.UnitDrifted || !hasFinding(result, checkplan.CodeClaimDrift, "homebrew:system:tool") {
			t.Fatalf("Analyze() = %#v", result)
		}
	})

	t.Run("prepared claim remains visible and is never finalized", func(t *testing.T) {
		facts := installedFactsFor(t, installplan.EffectShared, nil)
		input := facts.input()
		input.State = &facts.prepared

		result, err := checkplan.Analyze(input)
		if err != nil {
			t.Fatal(err)
		}
		if result.Units[0].Status != checkplan.UnitUnclaimed || !hasFinding(result, checkplan.CodeOperationPrepared, "") || !hasFinding(result, checkplan.CodeClaimMissing, "homebrew:system:tool") {
			t.Fatalf("Analyze() = %#v", result)
		}
	})
}

func TestAnalyzeAuditsRequiredReceiptStateAndTheReceiptBinding(t *testing.T) {
	baseDigest := strings.Repeat("c", 64)
	receiptDigest := strings.Repeat("d", 64)
	requirement := installplan.SatisfiedRequirement{
		SoftwareLockDigest: baseDigest, InstallationID: "field-kit-base", ReceiptSHA256: receiptDigest,
	}
	facts := installedFactsFor(t, installplan.EffectIsolated, []installplan.SatisfiedRequirement{requirement})
	exact := checkplan.RequirementObservation{
		SoftwareLockDigest: baseDigest, Installation: "field-kit-base", ReceiptSHA256: receiptDigest, Status: checkplan.RequirementExact,
	}

	input := facts.input()
	input.Requirements = []checkplan.RequirementObservation{exact}
	result, err := checkplan.Analyze(input)
	if err != nil || !result.Exact() || result.Requirements[0].Status != checkplan.RequirementExact {
		t.Fatalf("exact Analyze() = %#v, %v", result, err)
	}

	input = facts.input()
	input.Requirements = nil
	result, err = checkplan.Analyze(input)
	if err != nil {
		t.Fatal(err)
	}
	if !hasFinding(result, checkplan.CodeRequiredReceiptMissing, "") || result.Requirements[0].Status != checkplan.RequirementMissing {
		t.Fatalf("missing requirement Analyze() = %#v", result)
	}

	input = facts.input()
	exact.ReceiptSHA256 = strings.Repeat("e", 64)
	input.Requirements = []checkplan.RequirementObservation{exact}
	result, err = checkplan.Analyze(input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Units[0].Status != checkplan.UnitDrifted || !hasFinding(result, checkplan.CodeReceiptDrift, "uv:probe:tool") {
		t.Fatalf("changed base receipt Analyze() = %#v", result)
	}
}

type installedFacts struct {
	desired      softwarelock.Document
	installation installplan.Installation
	model        installplan.EffectModel
	observed     installplan.Observation
	receipt      receipt.Document
	prepared     rootstate.Document
	final        rootstate.Document
	requirements []checkplan.RequirementObservation
}

func (f installedFacts) input() checkplan.Input {
	receipted := f.receipt
	state := f.final
	return checkplan.Input{
		Desired: f.desired, Installation: f.installation,
		EffectModels: map[string]installplan.EffectModel{f.desired.Selections["tool"].Adapter: f.model},
		Observed:     f.observed, Receipt: &receipted, State: &state,
		Requirements: append([]checkplan.RequirementObservation(nil), f.requirements...),
	}
}

func installedFactsFor(t *testing.T, model installplan.EffectModel, satisfied []installplan.SatisfiedRequirement) installedFacts {
	t.Helper()
	adapterID, method, scope := "uv", "python-environment", "probe"
	installationID := "probe"
	location := checkRoot + "/software/installations/probe/environment/tool"
	if model == installplan.EffectShared {
		adapterID, method, scope = "homebrew", "system-package", "system"
		installationID = "field-kit-base"
		location = "/opt/fake/tool/1.0.0"
	}
	desired := checkLock(t, adapterID, method, scope)
	for _, requirement := range satisfied {
		desired.Requires = append(desired.Requires, softwarelock.InstallationRequirement{SoftwareLockDigest: requirement.SoftwareLockDigest})
	}
	if err := desired.Validate(); err != nil {
		t.Fatal(err)
	}
	installation := installplan.Installation{ID: installationID, Root: checkRoot}
	unitID := adapterID + ":" + scope + ":tool"
	before := installplan.Observation{Target: desired.Target, Root: checkRoot, Units: map[string]installplan.ObservedUnit{
		unitID: {InstallLocation: location},
	}}
	plannerState := installplan.State{Requirements: satisfied}
	if model == installplan.EffectShared {
		plannerState.Shared = installplan.SharedState{Root: checkRoot, Units: map[string]installplan.SharedUnit{}}
	}
	plan, err := installplan.Build(desired, installation, map[string]installplan.EffectModel{adapterID: model}, before, plannerState)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	prepared, _, fence, err := rootstate.Prepare(nil, desired, plan, before, rootstate.Lease{InvocationID: "check-fixture", Now: now, Duration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	locked := desired.Units[unitID]
	observed := installplan.Observation{Target: desired.Target, Root: checkRoot, Units: map[string]installplan.ObservedUnit{
		unitID: exactObservation(locked, location),
	}}
	receipted, err := receipt.Build(desired, plan, observed, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	final, err := rootstate.Finalize(prepared, installation.ID, "check-fixture", fence, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	requirements := make([]checkplan.RequirementObservation, 0, len(satisfied))
	for _, requirement := range satisfied {
		requirements = append(requirements, checkplan.RequirementObservation{
			SoftwareLockDigest: requirement.SoftwareLockDigest, Installation: requirement.InstallationID,
			ReceiptSHA256: requirement.ReceiptSHA256, Status: checkplan.RequirementExact,
		})
	}
	return installedFacts{
		desired: desired, installation: installation, model: model, observed: observed,
		receipt: receipted, prepared: prepared, final: final, requirements: requirements,
	}
}

func checkLock(t *testing.T, adapterID, method, scope string) softwarelock.Document {
	t.Helper()
	unitID := adapterID + ":" + scope + ":tool"
	document := softwarelock.Document{
		Schema: softwarelock.SchemaV1,
		Provenance: softwarelock.Provenance{Experiment: &softwarelock.ExperimentIdentity{
			Schema: "field-kit-experiment/v1", ID: "check-fixture", DefinitionSHA256: strings.Repeat("a", 64),
		}},
		Target:   software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6"},
		Resolved: "2026-08-24",
		Selections: map[string]softwarelock.Selection{
			"tool": {Provenance: softwarelock.ProvenanceExperiment, Method: method, Adapter: adapterID, RecipeRevision: "tool/v1", RootUnit: unitID},
		},
		Units: map[string]softwarelock.Unit{
			unitID: {
				Adapter: adapterID, Scope: scope, NativeName: "tool", Version: "1.0.0", Dependencies: []string{},
				Artifacts: []software.Artifact{{Locator: "https://example.invalid/tool", SHA256: strings.Repeat("b", 64)}},
			},
		},
	}
	if err := document.Validate(); err != nil {
		t.Fatal(err)
	}
	return document
}

func exactObservation(locked softwarelock.Unit, location string) installplan.ObservedUnit {
	return installplan.ObservedUnit{
		Present: true, Adapter: locked.Adapter, Scope: locked.Scope, NativeName: locked.NativeName,
		Version: locked.Version, Revision: locked.Revision,
		Dependencies: append([]string(nil), locked.Dependencies...), Artifacts: append([]software.Artifact(nil), locked.Artifacts...),
		Location: location, InstallLocation: location,
	}
}

func hasFinding(result checkplan.Result, code checkplan.Code, unit string) bool {
	for _, finding := range result.Findings {
		if finding.Code == code && finding.Unit == unit {
			return true
		}
	}
	return false
}
