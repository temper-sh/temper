package removeplan_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/installplan"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/receipt"
	"github.com/temper-sh/temper/internal/software/removeplan"
)

const removeRoot = "/tmp/temper-remove-plan"

func TestBuildRemovesAWhollyTemperOwnedIsolatedGroupInReverseDependencyOrder(t *testing.T) {
	desired := removalLock(t, "uv", "probe", true)
	installation := installplan.Installation{ID: "probe", Root: removeRoot}
	previous := removalReceipt(t, desired, installation, installplan.OwnershipTemperAdded)
	observed := removalObservation(desired, previous)

	plan, err := removeplan.Build(desired, installation, map[string]installplan.EffectModel{"uv": installplan.EffectIsolated}, &observed, &previous, removeplan.State{})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !plan.Changed() || plan.EffectCount() != 1 || len(plan.Groups) != 1 {
		t.Fatalf("Build() = %#v", plan)
	}
	units := plan.Groups[0].Units
	if len(units) != 2 || units[0].ID != "uv:probe:tool" || units[1].ID != "uv:probe:runtime" {
		t.Fatalf("removal order = %#v", units)
	}
	for _, unit := range units {
		if unit.Action != removeplan.ActionRemove || !unit.Execute {
			t.Fatalf("planned unit = %#v", unit)
		}
	}
}

func TestBuildPreservesAnAtomicIsolatedGroupContainingPreExistingSoftware(t *testing.T) {
	desired := removalLock(t, "uv", "probe", true)
	installation := installplan.Installation{ID: "probe", Root: removeRoot}
	previous := removalReceipt(t, desired, installation, installplan.OwnershipTemperAdded)
	preExisting := previous.Units["uv:probe:runtime"]
	preExisting.Ownership = installplan.OwnershipPreExisting
	previous.Units["uv:probe:runtime"] = preExisting
	observed := removalObservation(desired, previous)

	plan, err := removeplan.Build(desired, installation, map[string]installplan.EffectModel{"uv": installplan.EffectIsolated}, &observed, &previous, removeplan.State{})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.EffectCount() != 0 {
		t.Fatalf("effect count = %d, want 0", plan.EffectCount())
	}
	for _, unit := range plan.Groups[0].Units {
		if unit.Action != removeplan.ActionPreserve || unit.Execute {
			t.Fatalf("planned unit = %#v", unit)
		}
	}
}

func TestBuildSharedReleasePreservesForAnotherClaimThenRetiresTheLastTemperAddedGeneration(t *testing.T) {
	desired := removalLock(t, "homebrew", "system", false)
	installation := installplan.Installation{ID: "experiment-a", Root: removeRoot}
	previous := removalReceipt(t, desired, installation, installplan.OwnershipTemperAdded)
	observed := removalObservation(desired, previous)
	digest, _ := desired.SemanticDigest()
	unitID := "homebrew:system:tool"
	locked := desired.Units[unitID]
	key := installplan.SharedUnitKey(locked.Adapter, locked.Scope, locked.NativeName)
	shared := sharedRemovalState(desired, previous, installation.ID)
	unit := shared.Shared.Units[key]
	unit.Claims["experiment-b"] = installplan.SharedClaim{SoftwareLockDigest: digest, UnitID: unitID, Status: installplan.ClaimPrepared}
	shared.Shared.Units[key] = unit

	plan, err := removeplan.Build(desired, installation, map[string]installplan.EffectModel{"homebrew": installplan.EffectShared}, &observed, &previous, shared)
	if err != nil {
		t.Fatalf("Build() with another claim error = %v", err)
	}
	planned := plan.Groups[0].Units[0]
	if planned.Action != removeplan.ActionPreserve || !planned.RequirePresent || planned.RetireShared {
		t.Fatalf("non-final release = %#v", planned)
	}

	delete(unit.Claims, "experiment-b")
	shared.Shared.Units[key] = unit
	plan, err = removeplan.Build(desired, installation, map[string]installplan.EffectModel{"homebrew": installplan.EffectShared}, &observed, &previous, shared)
	if err != nil {
		t.Fatalf("Build() final release error = %v", err)
	}
	planned = plan.Groups[0].Units[0]
	if planned.Action != removeplan.ActionRemove || !planned.Execute || !planned.RetireShared {
		t.Fatalf("final release = %#v", planned)
	}
}

func TestBuildRefusesProviderLocationDriftBeforeRemoval(t *testing.T) {
	desired := removalLock(t, "uv", "probe", false)
	installation := installplan.Installation{ID: "probe", Root: removeRoot}
	previous := removalReceipt(t, desired, installation, installplan.OwnershipTemperAdded)
	observed := removalObservation(desired, previous)
	unitID := "uv:probe:tool"
	drifted := observed.Units[unitID]
	drifted.Location = filepath.Join(removeRoot, "software", "installations", "probe", "other")
	observed.Units[unitID] = drifted

	_, err := removeplan.Build(desired, installation, map[string]installplan.EffectModel{"uv": installplan.EffectIsolated}, &observed, &previous, removeplan.State{})
	if err == nil || !strings.Contains(err.Error(), "location differs") {
		t.Fatalf("Build() error = %v, want location drift refusal", err)
	}
}

func TestBuildRecoversPreparedRemovalWithoutAReceiptOrRepeatedEffect(t *testing.T) {
	desired := removalLock(t, "uv", "probe", false)
	installation := installplan.Installation{ID: "probe", Root: removeRoot}
	digest, _ := desired.SemanticDigest()
	unitID := "uv:probe:tool"
	location := filepath.Join(installplan.InstallationRoot(installation), "environment", "tool")
	observed := installplan.Observation{Target: desired.Target, Root: removeRoot, Units: map[string]installplan.ObservedUnit{unitID: {}}}
	state := removeplan.State{Prepared: &removeplan.Prepared{
		LockDigest: digest, Target: desired.Target, Installation: installation,
		Groups: map[string]removeplan.PreparedGroup{"uv:probe": {
			ID: "uv:probe", Adapter: "uv", Scope: "probe", EffectModel: installplan.EffectIsolated,
			Units: map[string]removeplan.PreparedUnit{unitID: {
				Before: installplan.BeforeExact, Ownership: installplan.OwnershipTemperAdded,
				Location: location, RemoveProvider: true,
			}},
		}},
	}}

	plan, err := removeplan.Build(desired, installation, map[string]installplan.EffectModel{"uv": installplan.EffectIsolated}, &observed, nil, state)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !plan.Recovering || !plan.Changed() || plan.EffectCount() != 0 || !plan.ReceiptRemove {
		t.Fatalf("recovery plan = %#v", plan)
	}
}

func removalLock(t *testing.T, adapter, scope string, withDependency bool) softwarelock.Document {
	t.Helper()
	rootID := adapter + ":" + scope + ":tool"
	units := map[string]softwarelock.Unit{
		rootID: {
			Adapter: adapter, Scope: scope, NativeName: "tool", Version: "1.0.0", Dependencies: []string{},
			Artifacts: []software.Artifact{{Locator: "https://example.invalid/tool", SHA256: strings.Repeat("b", 64)}},
		},
	}
	if withDependency {
		dependencyID := adapter + ":" + scope + ":runtime"
		units[dependencyID] = softwarelock.Unit{
			Adapter: adapter, Scope: scope, NativeName: "runtime", Version: "2.0.0", Dependencies: []string{},
			Artifacts: []software.Artifact{{Locator: "https://example.invalid/runtime", SHA256: strings.Repeat("c", 64)}},
		}
		root := units[rootID]
		root.Dependencies = []string{dependencyID}
		units[rootID] = root
	}
	document := softwarelock.Document{
		Schema: softwarelock.SchemaV1,
		Provenance: softwarelock.Provenance{Experiment: &softwarelock.ExperimentIdentity{
			Schema: "field-kit-experiment/v1", ID: "remove-plan", DefinitionSHA256: strings.Repeat("a", 64),
		}},
		Target: software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6"}, Resolved: "2026-08-24",
		Selections: map[string]softwarelock.Selection{"tool": {
			Provenance: softwarelock.ProvenanceExperiment, Method: "test-method", Adapter: adapter, RecipeRevision: "remove/v1", RootUnit: rootID,
		}},
		Units: units,
	}
	if err := document.Validate(); err != nil {
		t.Fatalf("fixture lock invalid: %v", err)
	}
	return document
}

func removalReceipt(t *testing.T, desired softwarelock.Document, installation installplan.Installation, ownership installplan.Ownership) receipt.Document {
	t.Helper()
	digest, _ := desired.SemanticDigest()
	document := receipt.Document{
		Schema: receipt.SchemaV1, Installation: installation.ID, SoftwareLockDigest: digest,
		Target: desired.Target, Root: installation.Root, ObservedAt: "2026-08-24T08:00:00Z", Requirements: []receipt.Requirement{},
		Selections: map[string]receipt.Selection{}, Units: map[string]receipt.Unit{},
	}
	for packageID, selection := range desired.Selections {
		document.Selections[packageID] = receipt.Selection{
			Provenance: selection.Provenance, Method: selection.Method, Adapter: selection.Adapter,
			RecipeRevision: selection.RecipeRevision, RootUnit: selection.RootUnit,
		}
	}
	for unitID, locked := range desired.Units {
		location := filepath.Join(installplan.InstallationRoot(installation), "environment", locked.NativeName)
		claim := ""
		if locked.Adapter == "homebrew" {
			location = filepath.Join("/opt/fake", locked.NativeName, locked.Version)
			claim = installplan.SharedUnitKey(locked.Adapter, locked.Scope, locked.NativeName)
		}
		document.Units[unitID] = receipt.Unit{
			Adapter: locked.Adapter, Scope: locked.Scope, NativeName: locked.NativeName,
			Version: locked.Version, Revision: locked.Revision,
			Dependencies: append([]string(nil), locked.Dependencies...), Artifacts: append([]software.Artifact(nil), locked.Artifacts...),
			Location: location, Ownership: ownership, SharedClaim: claim,
		}
	}
	if err := document.ValidateAgainst(desired, installation); err != nil {
		t.Fatalf("fixture receipt invalid: %v", err)
	}
	return document
}

func removalObservation(desired softwarelock.Document, previous receipt.Document) installplan.Observation {
	units := map[string]installplan.ObservedUnit{}
	for unitID, locked := range desired.Units {
		units[unitID] = installplan.ObservedUnit{
			Present: true, Adapter: locked.Adapter, Scope: locked.Scope, NativeName: locked.NativeName,
			Version: locked.Version, Revision: locked.Revision,
			Dependencies: append([]string(nil), locked.Dependencies...), Artifacts: append([]software.Artifact(nil), locked.Artifacts...),
			Location: previous.Units[unitID].Location, InstallLocation: previous.Units[unitID].Location,
		}
	}
	return installplan.Observation{Target: desired.Target, Root: previous.Root, Units: units}
}

func sharedRemovalState(desired softwarelock.Document, previous receipt.Document, installationID string) removeplan.State {
	digest, _ := desired.SemanticDigest()
	state := removeplan.State{Shared: installplan.SharedState{Root: previous.Root, Units: map[string]installplan.SharedUnit{}}}
	for unitID, locked := range desired.Units {
		key := previous.Units[unitID].SharedClaim
		state.Shared.Units[key] = installplan.SharedUnit{
			Adapter: locked.Adapter, Scope: locked.Scope, NativeName: locked.NativeName,
			Version: locked.Version, Revision: locked.Revision,
			Dependencies: append([]string(nil), locked.Dependencies...), Artifacts: append([]software.Artifact(nil), locked.Artifacts...),
			Location: previous.Units[unitID].Location, Acquisition: previous.Units[unitID].Ownership, Lifecycle: installplan.SharedActive,
			Claims: map[string]installplan.SharedClaim{installationID: {
				SoftwareLockDigest: digest, UnitID: unitID, Status: installplan.ClaimActive,
			}},
		}
	}
	return state
}
