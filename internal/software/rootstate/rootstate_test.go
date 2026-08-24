package rootstate_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/installplan"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/rootstate"
)

const stateRoot = "/tmp/temper-state-test"

func TestPrepareRoundTripsAndFinalizeRemovesIntent(t *testing.T) {
	desired := stateLock(t, "uv", "python-environment", "probe")
	installation := installplan.Installation{ID: "probe", Root: stateRoot}
	location := installplan.InstallationRoot(installation) + "/environment/tool"
	observed := absentObservation(desired, location)
	plan := buildStatePlan(t, desired, installation, observed, installplan.State{})
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)

	prepared, changed, fence, err := rootstate.Prepare(nil, desired, plan, observed, rootstate.Lease{InvocationID: "run-1", Now: now, Duration: time.Minute})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if !changed || prepared.Generation != 1 || fence != 1 || len(prepared.Operations) != 1 {
		t.Fatalf("Prepare() = changed %v generation %d fence %d operations %d", changed, prepared.Generation, fence, len(prepared.Operations))
	}
	data, err := rootstate.Marshal(prepared)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := rootstate.Parse(data)
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v", err)
	}
	projection, err := parsed.Projection(installation)
	if err != nil || projection.Prepared == nil || projection.Prepared.Units["uv:probe:tool"].Before != installplan.BeforeAbsent {
		t.Fatalf("Projection() = %#v, %v", projection, err)
	}
	finalized, err := rootstate.Finalize(parsed, installation.ID, "run-1", fence, now.Add(30*time.Second))
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if finalized.Generation != 2 || len(finalized.Operations) != 0 {
		t.Fatalf("Finalize() generation = %d operations = %d", finalized.Generation, len(finalized.Operations))
	}
}

func TestPrepareRefusesLiveHolderAndReclaimsExpiredLeaseWithFence(t *testing.T) {
	desired := stateLock(t, "uv", "python-environment", "probe")
	installation := installplan.Installation{ID: "probe", Root: stateRoot}
	observed := absentObservation(desired, installplan.InstallationRoot(installation)+"/environment/tool")
	plan := buildStatePlan(t, desired, installation, observed, installplan.State{})
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	prepared, _, _, err := rootstate.Prepare(nil, desired, plan, observed, rootstate.Lease{InvocationID: "run-1", Now: now, Duration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	projection, err := prepared.Projection(installation)
	if err != nil {
		t.Fatal(err)
	}
	recoveryPlan := buildStatePlan(t, desired, installation, observed, projection)

	_, _, _, err = rootstate.Prepare(&prepared, desired, recoveryPlan, observed, rootstate.Lease{InvocationID: "run-2", Now: now.Add(30 * time.Second), Duration: time.Minute})
	if !errors.Is(err, rootstate.ErrOperationBusy) {
		t.Fatalf("live Prepare() error = %v, want ErrOperationBusy", err)
	}
	reclaimed, changed, fence, err := rootstate.Prepare(&prepared, desired, recoveryPlan, observed, rootstate.Lease{InvocationID: "run-2", Now: now.Add(2 * time.Minute), Duration: time.Minute})
	if err != nil {
		t.Fatalf("expired Prepare() error = %v", err)
	}
	if !changed || fence != 2 || reclaimed.Generation != 2 || reclaimed.Operations[installation.ID].ClaimedBy != "run-2" {
		t.Fatalf("reclaimed operation = %#v", reclaimed.Operations[installation.ID])
	}
}

func TestPreparedSharedClaimBecomesActiveOnlyAtFinalize(t *testing.T) {
	desired := stateLock(t, "homebrew", "system-package", "system")
	installation := installplan.Installation{ID: "field-kit-base", Root: stateRoot}
	location := "/opt/homebrew/Cellar/tool/1.0.0"
	observed := absentObservation(desired, location)
	state := installplan.State{Shared: installplan.SharedState{Root: stateRoot, Units: map[string]installplan.SharedUnit{}}}
	plan := buildStatePlan(t, desired, installation, observed, state)
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)

	prepared, _, fence, err := rootstate.Prepare(nil, desired, plan, observed, rootstate.Lease{InvocationID: "run-shared", Now: now, Duration: time.Minute})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	key := installplan.SharedUnitKey("homebrew", "system", "tool")
	if prepared.SharedUnits[key].Claims[installation.ID].Status != installplan.ClaimPrepared {
		t.Fatalf("prepared claim = %#v", prepared.SharedUnits[key].Claims[installation.ID])
	}
	finalized, err := rootstate.Finalize(prepared, installation.ID, "run-shared", fence, now.Add(10*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if finalized.SharedUnits[key].Claims[installation.ID].Status != installplan.ClaimActive {
		t.Fatalf("final claim = %#v", finalized.SharedUnits[key].Claims[installation.ID])
	}
}

func TestParseRefusesNoncanonicalBytes(t *testing.T) {
	desired := stateLock(t, "uv", "python-environment", "probe")
	installation := installplan.Installation{ID: "probe", Root: stateRoot}
	observed := absentObservation(desired, installplan.InstallationRoot(installation)+"/environment/tool")
	plan := buildStatePlan(t, desired, installation, observed, installplan.State{})
	document, _, _, err := rootstate.Prepare(nil, desired, plan, observed, rootstate.Lease{
		InvocationID: "run-1", Now: time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC), Duration: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := rootstate.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("# alternate spelling\n")...)
	if _, err := rootstate.Parse(data); err == nil || !strings.Contains(err.Error(), "not canonical") {
		t.Fatalf("Parse() error = %v, want canonical refusal", err)
	}
}

func stateLock(t *testing.T, adapterID, method, scope string) softwarelock.Document {
	t.Helper()
	document := softwarelock.Document{
		Schema: softwarelock.SchemaV1,
		Provenance: softwarelock.Provenance{Experiment: &softwarelock.ExperimentIdentity{
			Schema: "field-kit-experiment/v1", ID: "state-fixture", DefinitionSHA256: strings.Repeat("a", 64),
		}},
		Target:   software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6"},
		Resolved: "2026-08-23",
		Selections: map[string]softwarelock.Selection{
			"tool": {Provenance: softwarelock.ProvenanceExperiment, Method: method, Adapter: adapterID, RecipeRevision: "tool/v1", RootUnit: adapterID + ":" + scope + ":tool"},
		},
		Units: map[string]softwarelock.Unit{
			adapterID + ":" + scope + ":tool": {
				Adapter: adapterID, Scope: scope, NativeName: "tool", Version: "1.0.0", Dependencies: []string{},
				Artifacts: []software.Artifact{{Locator: "https://example.invalid/tool", SHA256: strings.Repeat("b", 64)}},
			},
		},
	}
	if err := document.Validate(); err != nil {
		t.Fatalf("fixture lock invalid: %v", err)
	}
	return document
}

func absentObservation(desired softwarelock.Document, location string) installplan.Observation {
	units := map[string]installplan.ObservedUnit{}
	for unitID := range desired.Units {
		units[unitID] = installplan.ObservedUnit{InstallLocation: location}
	}
	return installplan.Observation{Target: desired.Target, Root: stateRoot, Units: units}
}

func buildStatePlan(t *testing.T, desired softwarelock.Document, installation installplan.Installation, observed installplan.Observation, state installplan.State) installplan.Plan {
	t.Helper()
	model := installplan.EffectIsolated
	for _, unit := range desired.Units {
		if unit.Adapter == "homebrew" {
			model = installplan.EffectShared
		}
	}
	plan, err := installplan.Build(desired, installation, map[string]installplan.EffectModel{desired.Selections["tool"].Adapter: model}, observed, state)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	return plan
}
