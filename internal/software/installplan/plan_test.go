package installplan_test

import (
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/installplan"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
)

const testRoot = "/tmp/temper-install-test"

func TestBuildPlansFreshSharedUnitsWithoutTakingPreExistingOwnership(t *testing.T) {
	desired := sharedLock(t)
	observed := observeAll(desired)
	observed.Units["homebrew:system:llama-swap"] = absent()

	plan, err := installplan.Build(desired, installationAt(testRoot), models(), observed, stateFor(t, desired, nil, nil))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !plan.Changed() || !plan.ReceiptWrite || plan.EffectCount() != 1 {
		t.Fatalf("Build() = changed %v, receipt %v, effects %d; want true, true, 1", plan.Changed(), plan.ReceiptWrite, plan.EffectCount())
	}
	group := plan.Groups[0]
	assertUnit(t, group.Units[0], "homebrew:system:libomp", installplan.ActionPreserve, installplan.OwnershipPreExisting)
	assertUnit(t, group.Units[1], "homebrew:system:llama-swap", installplan.ActionAdd, installplan.OwnershipTemperAdded)
}

func TestBuildRefusesAPartialFreshIsolatedEnvironment(t *testing.T) {
	desired := isolatedLock(t)
	observed := observeAll(desired)
	observed.Units["uv:rapid-mlx:rapid-mlx"] = absent()

	_, err := installplan.Build(desired, installationAt(testRoot), models(), observed, stateFor(t, desired, nil, nil))
	if err == nil || !strings.Contains(err.Error(), "partial unreceipted isolated environment") {
		t.Fatalf("Build() error = %v, want partial isolated refusal", err)
	}
}

func TestBuildPublishesAWhollyAbsentFreshIsolatedEnvironment(t *testing.T) {
	desired := isolatedLock(t)
	observed := ObservationFor(desired, map[string]installplan.ObservedUnit{
		"uv:rapid-mlx:cpython":   absent(),
		"uv:rapid-mlx:rapid-mlx": absent(),
	})

	plan, err := installplan.Build(desired, installationAt(testRoot), models(), observed, stateFor(t, desired, nil, nil))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.EffectCount() != 1 || !plan.ReceiptWrite {
		t.Fatalf("Build() effects = %d, receipt = %v; want 1, true", plan.EffectCount(), plan.ReceiptWrite)
	}
	for _, unit := range plan.Groups[0].Units {
		if unit.Action != installplan.ActionAdd || unit.Ownership != installplan.OwnershipTemperAdded {
			t.Errorf("fresh isolated unit = %#v, want added with Temper ownership", unit)
		}
	}
}

func TestBuildIsCleanForAnExactReceiptedSecondRun(t *testing.T) {
	desired := isolatedLock(t)
	previous := previousFor(t, desired, map[string]installplan.Ownership{
		"uv:rapid-mlx:cpython":   installplan.OwnershipTemperAdded,
		"uv:rapid-mlx:rapid-mlx": installplan.OwnershipTemperAdded,
	})

	plan, err := installplan.Build(desired, installationAt(testRoot), models(), observeAll(desired), stateFor(t, desired, &previous, nil))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.Changed() || plan.ReceiptWrite || plan.EffectCount() != 0 {
		t.Fatalf("clean Build() = changed %v, receipt %v, effects %d", plan.Changed(), plan.ReceiptWrite, plan.EffectCount())
	}
	for _, unit := range plan.Groups[0].Units {
		if unit.Action != installplan.ActionPreserve || unit.Ownership != installplan.OwnershipTemperAdded {
			t.Errorf("clean unit = %#v, want preserved Temper ownership", unit)
		}
	}
}

func TestBuildRepublishesAWhollyTemperOwnedIsolatedGroup(t *testing.T) {
	desired := isolatedLock(t)
	observed := observeAll(desired)
	drifted := observed.Units["uv:rapid-mlx:cpython"]
	drifted.Version = "3.13.1"
	observed.Units["uv:rapid-mlx:cpython"] = drifted
	previous := previousFor(t, desired, map[string]installplan.Ownership{
		"uv:rapid-mlx:cpython":   installplan.OwnershipTemperAdded,
		"uv:rapid-mlx:rapid-mlx": installplan.OwnershipTemperAdded,
	})

	plan, err := installplan.Build(desired, installationAt(testRoot), models(), observed, stateFor(t, desired, &previous, nil))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.EffectCount() != 1 || !plan.ReceiptWrite {
		t.Fatalf("Build() effects = %d, receipt = %v; want 1, true", plan.EffectCount(), plan.ReceiptWrite)
	}
	for _, unit := range plan.Groups[0].Units {
		if unit.Action != installplan.ActionReplace || unit.Ownership != installplan.OwnershipTemperAdded {
			t.Errorf("isolated republish unit = %#v, want replace with Temper ownership", unit)
		}
	}
}

func TestBuildRefusesSharedDriftEvenWhenPreviouslyAdded(t *testing.T) {
	desired := sharedLock(t)
	observed := observeAll(desired)
	drifted := observed.Units["homebrew:system:llama-swap"]
	drifted.Version = "2.0.0"
	observed.Units["homebrew:system:llama-swap"] = drifted
	previous := previousFor(t, desired, map[string]installplan.Ownership{
		"homebrew:system:libomp":     installplan.OwnershipTemperAdded,
		"homebrew:system:llama-swap": installplan.OwnershipTemperAdded,
	})

	_, err := installplan.Build(desired, installationAt(testRoot), models(), observed, stateFor(t, desired, &previous, nil))
	if err == nil || !strings.Contains(err.Error(), "shared installation unit") {
		t.Fatalf("Build() error = %v, want shared drift refusal", err)
	}
}

func TestBuildRefusesToReplaceAnyPreExistingIsolatedUnit(t *testing.T) {
	desired := isolatedLock(t)
	observed := observeAll(desired)
	observed.Units["uv:rapid-mlx:rapid-mlx"] = absent()
	previous := previousFor(t, desired, map[string]installplan.Ownership{
		"uv:rapid-mlx:cpython":   installplan.OwnershipPreExisting,
		"uv:rapid-mlx:rapid-mlx": installplan.OwnershipTemperAdded,
	})

	_, err := installplan.Build(desired, installationAt(testRoot), models(), observed, stateFor(t, desired, &previous, nil))
	if err == nil || !strings.Contains(err.Error(), "cannot replace a receipted pre-existing isolated unit") {
		t.Fatalf("Build() error = %v, want pre-existing isolated refusal", err)
	}
}

func TestBuildOrdersDependenciesBeforeDependants(t *testing.T) {
	desired := sharedLock(t)
	observed := ObservationFor(desired, map[string]installplan.ObservedUnit{
		"homebrew:system:libomp":     absent(),
		"homebrew:system:llama-swap": absent(),
	})

	plan, err := installplan.Build(desired, installationAt(testRoot), models(), observed, stateFor(t, desired, nil, nil))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	got := []string{plan.Groups[0].Units[0].ID, plan.Groups[0].Units[1].ID}
	want := []string{"homebrew:system:libomp", "homebrew:system:llama-swap"}
	if got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("unit order = %v, want %v", got, want)
	}
}

func TestBuildRecoversOwnershipAfterAFreshSharedEffectCompleted(t *testing.T) {
	desired := sharedLock(t)
	prepared := preparedFor(t, desired, map[string]installplan.PreparedUnit{
		"homebrew:system:libomp": {
			Before: installplan.BeforeExact, OwnershipAfter: installplan.OwnershipPreExisting,
		},
		"homebrew:system:llama-swap": {
			Before: installplan.BeforeAbsent, OwnershipAfter: installplan.OwnershipTemperAdded,
		},
	})

	plan, err := installplan.Build(desired, installationAt(testRoot), models(), observeAll(desired), stateFor(t, desired, nil, &prepared))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !plan.ReceiptWrite || plan.EffectCount() != 0 {
		t.Fatalf("recovered Build() receipt = %v, effects = %d; want true, 0", plan.ReceiptWrite, plan.EffectCount())
	}
	assertUnit(t, plan.Groups[0].Units[0], "homebrew:system:libomp", installplan.ActionPreserve, installplan.OwnershipPreExisting)
	assertUnit(t, plan.Groups[0].Units[1], "homebrew:system:llama-swap", installplan.ActionPreserve, installplan.OwnershipTemperAdded)
}

func TestBuildContinuesAnUnstartedPreparedSharedEffect(t *testing.T) {
	desired := sharedLock(t)
	observed := observeAll(desired)
	observed.Units["homebrew:system:llama-swap"] = absent()
	prepared := preparedFor(t, desired, map[string]installplan.PreparedUnit{
		"homebrew:system:libomp": {
			Before: installplan.BeforeExact, OwnershipAfter: installplan.OwnershipPreExisting,
		},
		"homebrew:system:llama-swap": {
			Before: installplan.BeforeAbsent, OwnershipAfter: installplan.OwnershipTemperAdded,
		},
	})

	plan, err := installplan.Build(desired, installationAt(testRoot), models(), observed, stateFor(t, desired, nil, &prepared))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.EffectCount() != 1 {
		t.Fatalf("recovered Build() effects = %d, want 1", plan.EffectCount())
	}
	assertUnit(t, plan.Groups[0].Units[1], "homebrew:system:llama-swap", installplan.ActionAdd, installplan.OwnershipTemperAdded)
}

func TestBuildRefusesWhenPreparedSharedStateLosesAPreservedUnit(t *testing.T) {
	desired := sharedLock(t)
	observed := observeAll(desired)
	observed.Units["homebrew:system:libomp"] = absent()
	prepared := preparedFor(t, desired, map[string]installplan.PreparedUnit{
		"homebrew:system:libomp": {
			Before: installplan.BeforeExact, OwnershipAfter: installplan.OwnershipPreExisting,
		},
		"homebrew:system:llama-swap": {
			Before: installplan.BeforeAbsent, OwnershipAfter: installplan.OwnershipTemperAdded,
		},
	})

	_, err := installplan.Build(desired, installationAt(testRoot), models(), observed, stateFor(t, desired, nil, &prepared))
	if err == nil || !strings.Contains(err.Error(), "drifted from the root-wide claim record") {
		t.Fatalf("Build() error = %v, want preserved-unit disappearance refusal", err)
	}
}

func TestBuildRepublishesAPartialPreparedIsolatedEnvironment(t *testing.T) {
	desired := isolatedLock(t)
	observed := observeAll(desired)
	observed.Units["uv:rapid-mlx:rapid-mlx"] = absent()
	prepared := preparedFor(t, desired, map[string]installplan.PreparedUnit{
		"uv:rapid-mlx:cpython": {
			Before: installplan.BeforeAbsent, OwnershipAfter: installplan.OwnershipTemperAdded,
		},
		"uv:rapid-mlx:rapid-mlx": {
			Before: installplan.BeforeAbsent, OwnershipAfter: installplan.OwnershipTemperAdded,
		},
	})

	plan, err := installplan.Build(desired, installationAt(testRoot), models(), observed, stateFor(t, desired, nil, &prepared))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.EffectCount() != 1 {
		t.Fatalf("recovered Build() effects = %d, want 1", plan.EffectCount())
	}
	assertUnit(t, plan.Groups[0].Units[0], "uv:rapid-mlx:cpython", installplan.ActionReplace, installplan.OwnershipTemperAdded)
	assertUnit(t, plan.Groups[0].Units[1], "uv:rapid-mlx:rapid-mlx", installplan.ActionAdd, installplan.OwnershipTemperAdded)
}

func TestBuildAddsAClaimWithoutReinstallingSharedSoftwareUsedByAnotherInstallation(t *testing.T) {
	desired := sharedLock(t)
	shared := claimedSharedState(t, desired, "field-kit-base", installplan.ClaimActive, installplan.OwnershipTemperAdded)
	installation := installplan.Installation{ID: "experiment-b", Root: testRoot}

	plan, err := installplan.Build(desired, installation, models(), observeAll(desired), installplan.State{Shared: shared})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.EffectCount() != 0 || plan.ClaimWriteCount() != 2 || !plan.ReceiptWrite {
		t.Fatalf("shared reuse = effects %d, claim writes %d, receipt %v; want 0, 2, true", plan.EffectCount(), plan.ClaimWriteCount(), plan.ReceiptWrite)
	}
	for _, unit := range plan.Groups[0].Units {
		if unit.Action != installplan.ActionPreserve || unit.ClaimAction != installplan.ClaimAdd || unit.Ownership != installplan.OwnershipTemperAdded {
			t.Errorf("shared reuse unit = %#v, want preserved provider plus new claim", unit)
		}
	}
}

func TestBuildIsCleanWhenASecondInstallationAlreadyHasActiveSharedClaims(t *testing.T) {
	desired := sharedLock(t)
	installation := installplan.Installation{ID: "experiment-b", Root: testRoot}
	previous := previousFor(t, desired, map[string]installplan.Ownership{
		"homebrew:system:libomp":     installplan.OwnershipTemperAdded,
		"homebrew:system:llama-swap": installplan.OwnershipTemperAdded,
	})
	previous.Installation = installation
	shared := claimedSharedState(t, desired, "field-kit-base", installplan.ClaimActive, installplan.OwnershipTemperAdded)
	addSharedClaims(t, desired, &shared, "experiment-b", installplan.ClaimActive)

	plan, err := installplan.Build(desired, installation, models(), observeAll(desired), installplan.State{Previous: &previous, Shared: shared})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.Changed() || plan.EffectCount() != 0 || plan.ClaimWriteCount() != 0 {
		t.Fatalf("second installation rerun = changed %v, effects %d, claim writes %d", plan.Changed(), plan.EffectCount(), plan.ClaimWriteCount())
	}
	for _, unit := range plan.Groups[0].Units {
		if unit.ClaimAction != installplan.ClaimPreserve {
			t.Errorf("active shared claim action = %q, want preserve", unit.ClaimAction)
		}
	}
}

func TestBuildFinalizesPreparedClaimsWithoutRewritingAnExistingReceipt(t *testing.T) {
	desired := sharedLock(t)
	previous := previousFor(t, desired, map[string]installplan.Ownership{
		"homebrew:system:libomp":     installplan.OwnershipPreExisting,
		"homebrew:system:llama-swap": installplan.OwnershipTemperAdded,
	})
	prepared := preparedFor(t, desired, map[string]installplan.PreparedUnit{
		"homebrew:system:libomp": {
			Before: installplan.BeforeExact, OwnershipAfter: installplan.OwnershipPreExisting,
		},
		"homebrew:system:llama-swap": {
			Before: installplan.BeforeAbsent, OwnershipAfter: installplan.OwnershipTemperAdded,
		},
	})
	state := stateFor(t, desired, nil, &prepared)
	state.Previous = &previous

	plan, err := installplan.Build(desired, installationAt(testRoot), models(), observeAll(desired), state)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.ReceiptWrite || plan.EffectCount() != 0 || plan.ClaimWriteCount() != 2 {
		t.Fatalf("finalization = receipt %v, effects %d, claim writes %d; want false, 0, 2", plan.ReceiptWrite, plan.EffectCount(), plan.ClaimWriteCount())
	}
	for _, unit := range plan.Groups[0].Units {
		if unit.ClaimAction != installplan.ClaimActivate {
			t.Errorf("prepared claim action = %q, want activate", unit.ClaimAction)
		}
	}
}

func TestBuildRequiresVerifiedBaseReceiptsDeclaredByAnExperimentLock(t *testing.T) {
	desired := isolatedLock(t)
	baseDigest := strings.Repeat("1", 64)
	receiptDigest := strings.Repeat("2", 64)
	desired.Requires = []softwarelock.InstallationRequirement{{SoftwareLockDigest: baseDigest}}
	observed := ObservationFor(desired, map[string]installplan.ObservedUnit{
		"uv:rapid-mlx:cpython":   absent(),
		"uv:rapid-mlx:rapid-mlx": absent(),
	})

	_, err := installplan.Build(desired, installationAt(testRoot), models(), observed, installplan.State{})
	if err == nil || !strings.Contains(err.Error(), "has no verified installation receipt") {
		t.Fatalf("Build() error = %v, want missing base receipt refusal", err)
	}

	state := installplan.State{Requirements: []installplan.SatisfiedRequirement{{SoftwareLockDigest: baseDigest, InstallationID: "field-kit-base", ReceiptSHA256: receiptDigest}}}
	plan, err := installplan.Build(desired, installationAt(testRoot), models(), observed, state)
	if err != nil {
		t.Fatalf("Build() with base receipt error = %v", err)
	}
	if len(plan.Requirements) != 1 || plan.Requirements[0].ReceiptSHA256 != receiptDigest {
		t.Fatalf("plan requirements = %#v", plan.Requirements)
	}
}

func TestBuildRefusesAnIsolatedUnitOutsideItsNamedInstallation(t *testing.T) {
	desired := isolatedLock(t)
	observed := observeAll(desired)
	cpython := observed.Units["uv:rapid-mlx:cpython"]
	cpython.Location = "/tmp/a-different-experiment/cpython"
	observed.Units["uv:rapid-mlx:cpython"] = cpython

	_, err := installplan.Build(desired, installationAt(testRoot), models(), observed, installplan.State{})
	if err == nil || !strings.Contains(err.Error(), "outside installation root") {
		t.Fatalf("Build() error = %v, want isolated-root refusal", err)
	}
}

func TestBuildConsumesADirectExperimentLockWithoutReadingACatalog(t *testing.T) {
	desired := isolatedLock(t)
	desired.Provenance = softwarelock.Provenance{Experiment: &softwarelock.ExperimentIdentity{
		Schema: "labs-experiment/v1", ID: "rapid-mlx-candidate", DefinitionSHA256: strings.Repeat("3", 64),
	}}
	selection := desired.Selections["rapid-mlx"]
	selection.Provenance = softwarelock.ProvenanceExperiment
	desired.Selections["rapid-mlx"] = selection
	observed := ObservationFor(desired, map[string]installplan.ObservedUnit{
		"uv:rapid-mlx:cpython":   absent(),
		"uv:rapid-mlx:rapid-mlx": absent(),
	})

	plan, err := installplan.Build(desired, installationAt(testRoot), models(), observed, installplan.State{})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.EffectCount() != 1 {
		t.Fatalf("direct experiment Build() effects = %d, want 1", plan.EffectCount())
	}
}

func TestBuildRefusesIncompleteOrMismatchedEvidence(t *testing.T) {
	desired := sharedLock(t)

	tests := []struct {
		name     string
		root     string
		observed installplan.Observation
		previous *installplan.Previous
		want     string
	}{
		{
			name: "observation omits a unit",
			root: testRoot,
			observed: ObservationFor(desired, map[string]installplan.ObservedUnit{
				"homebrew:system:llama-swap": observe(desired.Units["homebrew:system:llama-swap"]),
			}),
			want: "omits unit",
		},
		{
			name:     "observation has the wrong root",
			root:     testRoot,
			observed: observationAt(desired, "/tmp/another-root"),
			want:     "observation root differs",
		},
		{
			name:     "receipt belongs to another lock",
			root:     testRoot,
			observed: observeAll(desired),
			previous: &installplan.Previous{LockDigest: strings.Repeat("f", 64), Target: desired.Target, Installation: installationAt(testRoot), Units: map[string]installplan.PreviousUnit{}},
			want:     "different software lock",
		},
		{
			name:     "filesystem root is not an installation root",
			root:     "/",
			observed: observeAll(desired),
			want:     "narrower than a filesystem root",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := installplan.Build(desired, installationAt(tt.root), models(), tt.observed, stateFor(t, desired, tt.previous, nil))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Build() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func assertUnit(t *testing.T, got installplan.Unit, id string, action installplan.Action, ownership installplan.Ownership) {
	t.Helper()
	if got.ID != id || got.Action != action || got.Ownership != ownership {
		t.Errorf("unit = %#v, want id=%q action=%q ownership=%q", got, id, action, ownership)
	}
}

func models() map[string]installplan.EffectModel {
	return map[string]installplan.EffectModel{
		"homebrew": installplan.EffectShared,
		"uv":       installplan.EffectIsolated,
	}
}

func sharedLock(t *testing.T) softwarelock.Document {
	t.Helper()
	document := baseLock()
	document.Selections["llama-swap"] = softwarelock.Selection{
		Provenance: softwarelock.ProvenanceCatalog,
		Method:     "system-package", Adapter: "homebrew", RecipeRevision: "llama-swap/v1", RootUnit: "homebrew:system:llama-swap",
	}
	document.Units["homebrew:system:libomp"] = unit("homebrew", "system", "libomp", "19.1.0", nil, "b")
	document.Units["homebrew:system:llama-swap"] = unit("homebrew", "system", "llama-swap", "1.4.0", []string{"homebrew:system:libomp"}, "a")
	requireValid(t, document)
	return document
}

func isolatedLock(t *testing.T) softwarelock.Document {
	t.Helper()
	document := baseLock()
	document.Selections["rapid-mlx"] = softwarelock.Selection{
		Provenance: softwarelock.ProvenanceCatalog,
		Method:     "python-environment", Adapter: "uv", RecipeRevision: "rapid-mlx/v1", RootUnit: "uv:rapid-mlx:rapid-mlx",
	}
	document.Units["uv:rapid-mlx:cpython"] = unit("uv", "rapid-mlx", "cpython", "3.12.9", nil, "c")
	document.Units["uv:rapid-mlx:rapid-mlx"] = unit("uv", "rapid-mlx", "rapid-mlx", "0.7.0", []string{"uv:rapid-mlx:cpython"}, "d")
	requireValid(t, document)
	return document
}

func baseLock() softwarelock.Document {
	return softwarelock.Document{
		Schema: softwarelock.SchemaV1,
		Provenance: softwarelock.Provenance{Catalog: &softwarelock.CatalogIdentity{
			Schema: "temper-software-supply/v1", Sequence: 1, SHA256: strings.Repeat("e", 64),
		}},
		Target:     software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6"},
		Resolved:   "2026-08-20",
		Selections: map[string]softwarelock.Selection{},
		Units:      map[string]softwarelock.Unit{},
	}
}

func unit(adapter, scope, name, version string, dependencies []string, hashCharacter string) softwarelock.Unit {
	return softwarelock.Unit{
		Adapter: adapter, Scope: scope, NativeName: name, Version: version,
		Dependencies: dependencies,
		Artifacts:    []software.Artifact{{Locator: "https://example.invalid/" + name, SHA256: strings.Repeat(hashCharacter, 64)}},
	}
}

func requireValid(t *testing.T, document softwarelock.Document) {
	t.Helper()
	if err := document.Validate(); err != nil {
		t.Fatalf("fixture lock invalid: %v", err)
	}
}

func observeAll(desired softwarelock.Document) installplan.Observation {
	units := make(map[string]installplan.ObservedUnit, len(desired.Units))
	for id, desiredUnit := range desired.Units {
		units[id] = observe(desiredUnit)
	}
	return ObservationFor(desired, units)
}

func observationAt(desired softwarelock.Document, root string) installplan.Observation {
	observed := observeAll(desired)
	observed.Root = root
	return observed
}

func ObservationFor(desired softwarelock.Document, units map[string]installplan.ObservedUnit) installplan.Observation {
	return installplan.Observation{Target: desired.Target, Root: testRoot, Units: units}
}

func observe(unit softwarelock.Unit) installplan.ObservedUnit {
	location := "/opt/homebrew/Cellar/" + unit.NativeName + "/" + unit.Version
	if unit.Adapter == "uv" {
		location = testRoot + "/software/installations/field-kit-base/environment/" + unit.NativeName
	}
	return installplan.ObservedUnit{
		Present: true, Adapter: unit.Adapter, Scope: unit.Scope, NativeName: unit.NativeName,
		Version: unit.Version, Revision: unit.Revision,
		Dependencies: append([]string(nil), unit.Dependencies...), Artifacts: append([]software.Artifact(nil), unit.Artifacts...),
		Location: location,
	}
}

func absent() installplan.ObservedUnit { return installplan.ObservedUnit{} }

func previousFor(t *testing.T, desired softwarelock.Document, units map[string]installplan.Ownership) installplan.Previous {
	t.Helper()
	digest, err := desired.SemanticDigest()
	if err != nil {
		t.Fatal(err)
	}
	provenance := make(map[string]installplan.PreviousUnit, len(units))
	for unitID, ownership := range units {
		locked := desired.Units[unitID]
		sharedClaim := ""
		if locked.Adapter == "homebrew" {
			sharedClaim = installplan.SharedUnitKey(locked.Adapter, locked.Scope, locked.NativeName)
		}
		provenance[unitID] = installplan.PreviousUnit{Ownership: ownership, SharedClaim: sharedClaim}
	}
	return installplan.Previous{LockDigest: digest, Target: desired.Target, Installation: installationAt(testRoot), Units: provenance}
}

func preparedFor(t *testing.T, desired softwarelock.Document, units map[string]installplan.PreparedUnit) installplan.Prepared {
	t.Helper()
	digest, err := desired.SemanticDigest()
	if err != nil {
		t.Fatal(err)
	}
	for unitID, intent := range units {
		locked := desired.Units[unitID]
		if locked.Adapter == "homebrew" {
			intent.SharedClaim = installplan.SharedUnitKey(locked.Adapter, locked.Scope, locked.NativeName)
			units[unitID] = intent
		}
	}
	return installplan.Prepared{LockDigest: digest, Target: desired.Target, Installation: installationAt(testRoot), Units: units}
}

func installationAt(root string) installplan.Installation {
	return installplan.Installation{ID: "field-kit-base", Root: root}
}

func stateFor(t *testing.T, desired softwarelock.Document, previous *installplan.Previous, prepared *installplan.Prepared) installplan.State {
	t.Helper()
	state := installplan.State{Previous: previous, Prepared: prepared}
	hasShared := false
	for _, unit := range desired.Units {
		if unit.Adapter == "homebrew" {
			hasShared = true
			break
		}
	}
	if !hasShared {
		return state
	}
	state.Shared = installplan.SharedState{Root: testRoot, Units: map[string]installplan.SharedUnit{}}
	if previous == nil && prepared == nil {
		return state
	}
	digest, err := desired.SemanticDigest()
	if err != nil {
		t.Fatal(err)
	}
	for unitID, locked := range desired.Units {
		if locked.Adapter != "homebrew" {
			continue
		}
		ownership := installplan.OwnershipTemperAdded
		status := installplan.ClaimPrepared
		if previous != nil {
			ownership = previous.Units[unitID].Ownership
			status = installplan.ClaimActive
		} else if prepared != nil {
			ownership = prepared.Units[unitID].OwnershipAfter
		}
		actual := observe(locked)
		key := installplan.SharedUnitKey(locked.Adapter, locked.Scope, locked.NativeName)
		state.Shared.Units[key] = installplan.SharedUnit{
			Adapter: locked.Adapter, Scope: locked.Scope, NativeName: locked.NativeName,
			Version: locked.Version, Revision: locked.Revision,
			Dependencies: append([]string(nil), locked.Dependencies...), Artifacts: append([]software.Artifact(nil), locked.Artifacts...),
			Location: actual.Location, Acquisition: ownership, Lifecycle: installplan.SharedActive,
			Claims: map[string]installplan.SharedClaim{
				"field-kit-base": {SoftwareLockDigest: digest, UnitID: unitID, Status: status},
			},
		}
	}
	return state
}

func claimedSharedState(t *testing.T, desired softwarelock.Document, installationID string, status installplan.ClaimStatus, acquisition installplan.Ownership) installplan.SharedState {
	t.Helper()
	state := installplan.SharedState{Root: testRoot, Units: map[string]installplan.SharedUnit{}}
	digest, err := desired.SemanticDigest()
	if err != nil {
		t.Fatal(err)
	}
	for unitID, locked := range desired.Units {
		if locked.Adapter != "homebrew" {
			continue
		}
		actual := observe(locked)
		key := installplan.SharedUnitKey(locked.Adapter, locked.Scope, locked.NativeName)
		state.Units[key] = installplan.SharedUnit{
			Adapter: locked.Adapter, Scope: locked.Scope, NativeName: locked.NativeName,
			Version: locked.Version, Revision: locked.Revision,
			Dependencies: append([]string(nil), locked.Dependencies...), Artifacts: append([]software.Artifact(nil), locked.Artifacts...),
			Location: actual.Location, Acquisition: acquisition, Lifecycle: installplan.SharedActive,
			Claims: map[string]installplan.SharedClaim{
				installationID: {SoftwareLockDigest: digest, UnitID: unitID, Status: status},
			},
		}
	}
	return state
}

func addSharedClaims(t *testing.T, desired softwarelock.Document, state *installplan.SharedState, installationID string, status installplan.ClaimStatus) {
	t.Helper()
	digest, err := desired.SemanticDigest()
	if err != nil {
		t.Fatal(err)
	}
	for unitID, locked := range desired.Units {
		if locked.Adapter != "homebrew" {
			continue
		}
		key := installplan.SharedUnitKey(locked.Adapter, locked.Scope, locked.NativeName)
		registered := state.Units[key]
		registered.Claims[installationID] = installplan.SharedClaim{SoftwareLockDigest: digest, UnitID: unitID, Status: status}
		state.Units[key] = registered
	}
}
