package install_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/catalog"
	installverb "github.com/temper-sh/temper/internal/software/install"
	"github.com/temper-sh/temper/internal/software/installplan"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/receiptstore"
	"github.com/temper-sh/temper/internal/software/rootstate"
	"github.com/temper-sh/temper/internal/software/statestore"
)

func TestRunPreparesInstallsReceiptsFinalizesAndIsSecondRunClean(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "temper-root")
	desired := installationLock(t, "uv", "python-environment", "probe")
	lockPath := writeLock(t, parent, "software.lock.yaml", desired)
	fake := newFakeAdapter("uv", "python-environment", installplan.EffectIsolated)
	family := installationFamily(t, fake)
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)

	result, err := installverb.Run(context.Background(), installverb.Options{
		LockPath: lockPath, Root: root, Installation: "probe", InvocationID: "run-1", LeaseDuration: time.Minute, Now: func() time.Time { return now },
	}, family)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Changed || result.Effects != 1 || result.ReceiptSHA256 == "" || fake.installCalls != 1 {
		t.Fatalf("Run() = %#v, install calls = %d", result, fake.installCalls)
	}
	state, err := statestore.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Exists() || len(state.Document.Operations) != 0 {
		t.Fatalf("final state = %#v", state.Document)
	}
	receipted, err := receiptstore.Read(root, "probe")
	if err != nil {
		t.Fatal(err)
	}
	if !receipted.Exists() || receipted.Document.Units["uv:probe:tool"].Ownership != installplan.OwnershipTemperAdded {
		t.Fatalf("receipt = %#v", receipted.Document)
	}
	beforeReceipt := append([]byte(nil), receipted.Data...)
	beforeState := append([]byte(nil), state.Data...)

	now = now.Add(time.Second)
	second, err := installverb.Run(context.Background(), installverb.Options{
		LockPath: lockPath, Root: root, Installation: "probe", InvocationID: "run-2", LeaseDuration: time.Minute, Now: func() time.Time { return now },
	}, family)
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if second.Changed || fake.installCalls != 1 {
		t.Fatalf("second Run() = %#v, install calls = %d", second, fake.installCalls)
	}
	afterReceipt, _ := receiptstore.Read(root, "probe")
	afterState, _ := statestore.Read(root)
	if string(afterReceipt.Data) != string(beforeReceipt) || string(afterState.Data) != string(beforeState) {
		t.Fatal("clean second run rewrote receipt or root state")
	}
}

func TestRunDryRunInspectsAndPlansWithoutAnyMutation(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "temper-root")
	desired := installationLock(t, "uv", "python-environment", "probe")
	lockPath := writeLock(t, parent, "software.lock.yaml", desired)
	fake := newFakeAdapter("uv", "python-environment", installplan.EffectIsolated)

	result, err := installverb.Run(context.Background(), installverb.Options{
		LockPath: lockPath, Root: root, Installation: "probe", InvocationID: "dry-run", DryRun: true,
	}, installationFamily(t, fake))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Changed || !result.DryRun || result.Effects != 1 || fake.installCalls != 0 {
		t.Fatalf("dry Run() = %#v, install calls = %d", result, fake.installCalls)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("dry run created root: %v", err)
	}
}

func TestRunVerifiesAndRecordsARequiredBaseReceiptBeforePlanning(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "temper-root")
	base := installationLock(t, "uv", "python-environment", "base")
	baseLockPath := writeLock(t, parent, "base-software.lock.yaml", base)
	fake := newFakeAdapter("uv", "python-environment", installplan.EffectIsolated)
	family := installationFamily(t, fake)
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)

	if _, err := installverb.Run(context.Background(), installverb.Options{
		LockPath: baseLockPath, Root: root, Installation: "field-kit-base", InvocationID: "base-run",
		LeaseDuration: time.Minute, Now: func() time.Time { return now },
	}, family); err != nil {
		t.Fatalf("install base: %v", err)
	}
	baseReceipt, err := receiptstore.Read(root, "field-kit-base")
	if err != nil || !baseReceipt.Exists() {
		t.Fatalf("read base receipt: exists = %v, error = %v", baseReceipt.Exists(), err)
	}
	baseDigest, err := base.SemanticDigest()
	if err != nil {
		t.Fatal(err)
	}
	experiment := installationLock(t, "uv", "python-environment", "experiment")
	experiment.Requires = []softwarelock.InstallationRequirement{{SoftwareLockDigest: baseDigest}}
	experimentLockPath := writeLock(t, parent, "experiment-software.lock.yaml", experiment)

	now = now.Add(time.Second)
	if _, err := installverb.Run(context.Background(), installverb.Options{
		LockPath: experimentLockPath, Root: root, Installation: "experiment-a", InvocationID: "experiment-run",
		RequiredReceiptPaths: []string{baseReceipt.Path()}, LeaseDuration: time.Minute, Now: func() time.Time { return now },
	}, family); err != nil {
		t.Fatalf("install experiment with verified base: %v", err)
	}
	experimentReceipt, err := receiptstore.Read(root, "experiment-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(experimentReceipt.Document.Requirements) != 1 || experimentReceipt.Document.Requirements[0].Installation != "field-kit-base" {
		t.Fatalf("recorded requirements = %#v", experimentReceipt.Document.Requirements)
	}

	for _, locked := range base.Units {
		key := fake.providerKey(installplan.Installation{ID: "field-kit-base", Root: root}, locked)
		drifted := fake.observed[key]
		drifted.Version = "9.9.9"
		fake.observed[key] = drifted
	}
	stateBefore, err := statestore.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	installCallsBefore := fake.installCalls
	_, err = installverb.Run(context.Background(), installverb.Options{
		LockPath: experimentLockPath, Root: root, Installation: "experiment-b", InvocationID: "drift-run",
		RequiredReceiptPaths: []string{baseReceipt.Path()}, LeaseDuration: time.Minute, Now: func() time.Time { return now.Add(time.Second) },
	}, family)
	if err == nil || !strings.Contains(err.Error(), "provider drift") {
		t.Fatalf("drifting base error = %v, want provider drift refusal", err)
	}
	stateAfter, readErr := statestore.Read(root)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(stateAfter.Data) != string(stateBefore.Data) || fake.installCalls != installCallsBefore {
		t.Fatal("base drift refusal mutated state or invoked an installer")
	}
	if refusedReceipt, _ := receiptstore.Read(root, "experiment-b"); refusedReceipt.Exists() {
		t.Fatal("base drift refusal published an experiment receipt")
	}
}

func TestRunReconcilesUnknownCompletedEffectWithoutRepeatingIt(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "temper-root")
	desired := installationLock(t, "uv", "python-environment", "probe")
	lockPath := writeLock(t, parent, "software.lock.yaml", desired)
	fake := newFakeAdapter("uv", "python-environment", installplan.EffectIsolated)
	fake.failAfterApplyOnce = true
	family := installationFamily(t, fake)
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)

	_, err := installverb.Run(context.Background(), installverb.Options{
		LockPath: lockPath, Root: root, Installation: "probe", InvocationID: "run-1", LeaseDuration: time.Minute, Now: func() time.Time { return now },
	}, family)
	if err == nil || !strings.Contains(err.Error(), "outcome unknown") {
		t.Fatalf("first Run() error = %v, want unknown outcome", err)
	}
	prepared, err := statestore.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.Document.Operations) != 1 || fake.installCalls != 1 {
		t.Fatalf("prepared state = %#v, install calls = %d", prepared.Document, fake.installCalls)
	}
	if receipt, _ := receiptstore.Read(root, "probe"); receipt.Exists() {
		t.Fatal("unknown outcome published a receipt")
	}

	_, err = installverb.Run(context.Background(), installverb.Options{
		LockPath: lockPath, Root: root, Installation: "probe", InvocationID: "run-live", LeaseDuration: time.Minute, Now: func() time.Time { return now.Add(30 * time.Second) },
	}, family)
	if !errors.Is(err, rootstateBusySentinel()) {
		// The error is wrapped with operation context; the helper preserves a
		// black-box dependency on errors.Is rather than string-only matching.
		t.Fatalf("live recovery error = %v, want busy refusal", err)
	}

	now = now.Add(2 * time.Minute)
	result, err := installverb.Run(context.Background(), installverb.Options{
		LockPath: lockPath, Root: root, Installation: "probe", InvocationID: "run-2", LeaseDuration: time.Minute, Now: func() time.Time { return now },
	}, family)
	if err != nil {
		t.Fatalf("recovery Run() error = %v", err)
	}
	if !result.Changed || result.Effects != 0 || fake.installCalls != 1 {
		t.Fatalf("recovery Run() = %#v, install calls = %d", result, fake.installCalls)
	}
	finalState, _ := statestore.Read(root)
	finalReceipt, _ := receiptstore.Read(root, "probe")
	if len(finalState.Document.Operations) != 0 || !finalReceipt.Exists() {
		t.Fatalf("recovery state = %#v receipt exists = %v", finalState.Document, finalReceipt.Exists())
	}
}

func TestRunSharesAnExactProviderUnitThroughActiveClaimsWithoutReinstall(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "temper-root")
	desired := installationLock(t, "homebrew", "system-package", "system")
	lockPath := writeLock(t, parent, "software.lock.yaml", desired)
	fake := newFakeAdapter("homebrew", "system-package", installplan.EffectShared)
	family := installationFamily(t, fake)
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)

	for index, installationID := range []string{"field-kit-base", "experiment-b"} {
		invocation := fmt.Sprintf("run-%d", index+1)
		_, err := installverb.Run(context.Background(), installverb.Options{
			LockPath: lockPath, Root: root, Installation: installationID, InvocationID: invocation,
			LeaseDuration: time.Minute, Now: func() time.Time { return now.Add(time.Duration(index) * time.Second) },
		}, family)
		if err != nil {
			t.Fatalf("install %s: %v", installationID, err)
		}
	}
	if fake.installCalls != 1 {
		t.Fatalf("shared install calls = %d, want 1", fake.installCalls)
	}
	state, err := statestore.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	key := installplan.SharedUnitKey("homebrew", "system", "tool")
	claims := state.Document.SharedUnits[key].Claims
	if len(claims) != 2 || claims["field-kit-base"].Status != installplan.ClaimActive || claims["experiment-b"].Status != installplan.ClaimActive {
		t.Fatalf("shared claims = %#v", claims)
	}
}

func TestRunPreservesAndClaimsAnExactPreExistingSharedUnit(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "temper-root")
	desired := installationLock(t, "homebrew", "system-package", "system")
	lockPath := writeLock(t, parent, "software.lock.yaml", desired)
	fake := newFakeAdapter("homebrew", "system-package", installplan.EffectShared)
	installation := installplan.Installation{ID: "field-kit-base", Root: root}
	for _, locked := range desired.Units {
		location := filepath.Join("/opt/fake", locked.NativeName, locked.Version)
		fake.observed[fake.providerKey(installation, locked)] = exactObservation(locked, location)
	}

	result, err := installverb.Run(context.Background(), installverb.Options{
		LockPath: lockPath, Root: root, Installation: installation.ID, InvocationID: "pre-existing-run",
		LeaseDuration: time.Minute, Now: func() time.Time { return time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC) },
	}, installationFamily(t, fake))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Changed || result.Effects != 0 || result.Claims != 1 || fake.installCalls != 0 {
		t.Fatalf("Run() = %#v, install calls = %d", result, fake.installCalls)
	}
	receipted, err := receiptstore.Read(root, installation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if receipted.Document.Units["homebrew:system:tool"].Ownership != installplan.OwnershipPreExisting {
		t.Fatalf("receipt unit = %#v", receipted.Document.Units["homebrew:system:tool"])
	}
	state, err := statestore.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	key := installplan.SharedUnitKey("homebrew", "system", "tool")
	if state.Document.SharedUnits[key].Acquisition != installplan.OwnershipPreExisting {
		t.Fatalf("shared state = %#v", state.Document.SharedUnits[key])
	}
}

// rootstateBusySentinel keeps the test package black-box while still proving
// errors.Is behavior through the exported refusal identity.
func rootstateBusySentinel() error {
	return rootstate.ErrOperationBusy
}

type fakeAdapter struct {
	descriptor         adapter.Descriptor
	observed           map[string]installplan.ObservedUnit
	installCalls       int
	failAfterApplyOnce bool
}

func newFakeAdapter(id, method string, model installplan.EffectModel) *fakeAdapter {
	return &fakeAdapter{
		descriptor: adapter.Descriptor{
			ID: id, Method: method, Protocol: catalog.AdapterProtocolV1, EffectModel: string(model),
			Targets: []software.Target{{OS: "darwin", Arch: "arm64"}},
		},
		observed: map[string]installplan.ObservedUnit{},
	}
}

func (f *fakeAdapter) Descriptor() adapter.Descriptor { return f.descriptor }

func (f *fakeAdapter) Inspect(_ context.Context, request adapter.InspectRequest) (map[string]installplan.ObservedUnit, error) {
	result := make(map[string]installplan.ObservedUnit, len(request.Units))
	for unitID, locked := range request.Units {
		key := f.providerKey(request.Installation, locked)
		if actual, ok := f.observed[key]; ok {
			result[unitID] = cloneObserved(actual)
			continue
		}
		location := filepath.Join(installplan.InstallationRoot(request.Installation), "environment", locked.NativeName)
		if f.descriptor.EffectModel == string(installplan.EffectShared) {
			location = filepath.Join("/opt/fake", locked.NativeName, locked.Version)
		}
		result[unitID] = installplan.ObservedUnit{InstallLocation: location}
	}
	return result, nil
}

func (f *fakeAdapter) Install(_ context.Context, request adapter.InstallRequest) error {
	f.installCalls++
	for _, planned := range request.Group.Units {
		if planned.Action == installplan.ActionPreserve {
			continue
		}
		locked := request.Units[planned.ID]
		f.observed[f.providerKey(request.Installation, locked)] = installplan.ObservedUnit{
			Present: true, Adapter: locked.Adapter, Scope: locked.Scope, NativeName: locked.NativeName,
			Version: locked.Version, Revision: locked.Revision,
			Dependencies: append([]string(nil), locked.Dependencies...), Artifacts: append([]software.Artifact(nil), locked.Artifacts...),
			Location: planned.Location, InstallLocation: planned.Location,
		}
	}
	if f.failAfterApplyOnce {
		f.failAfterApplyOnce = false
		return errors.New("provider outcome unknown")
	}
	return nil
}

func (f *fakeAdapter) Remove(_ context.Context, request adapter.RemoveRequest) error {
	for _, planned := range request.Group.Units {
		if !planned.Execute {
			continue
		}
		delete(f.observed, f.providerKey(request.Installation, request.Units[planned.ID]))
	}
	return nil
}

func (f *fakeAdapter) providerKey(installation installplan.Installation, unit softwarelock.Unit) string {
	if f.descriptor.EffectModel == string(installplan.EffectShared) {
		return installplan.SharedUnitKey(unit.Adapter, unit.Scope, unit.NativeName)
	}
	return installation.ID + ":" + unit.Adapter + ":" + unit.Scope + ":" + unit.NativeName
}

func installationFamily(t *testing.T, members ...adapter.InstallationAdapter) adapter.InstallationFamily {
	t.Helper()
	family, err := adapter.NewInstallationFamily(members...)
	if err != nil {
		t.Fatal(err)
	}
	return family
}

func installationLock(t *testing.T, adapterID, method, scope string) softwarelock.Document {
	t.Helper()
	unitID := adapterID + ":" + scope + ":tool"
	document := softwarelock.Document{
		Schema: softwarelock.SchemaV1,
		Provenance: softwarelock.Provenance{Experiment: &softwarelock.ExperimentIdentity{
			Schema: "field-kit-experiment/v1", ID: "install-fixture", DefinitionSHA256: strings.Repeat("a", 64),
		}},
		Target:   software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6"},
		Resolved: "2026-08-23",
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
		t.Fatalf("fixture lock invalid: %v", err)
	}
	return document
}

func writeLock(t *testing.T, directory, name string, document softwarelock.Document) string {
	t.Helper()
	data, err := softwarelock.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func cloneObserved(unit installplan.ObservedUnit) installplan.ObservedUnit {
	unit.Dependencies = append([]string(nil), unit.Dependencies...)
	unit.Artifacts = append([]software.Artifact(nil), unit.Artifacts...)
	return unit
}

func exactObservation(locked softwarelock.Unit, location string) installplan.ObservedUnit {
	return installplan.ObservedUnit{
		Present: true, Adapter: locked.Adapter, Scope: locked.Scope, NativeName: locked.NativeName,
		Version: locked.Version, Revision: locked.Revision,
		Dependencies: append([]string(nil), locked.Dependencies...), Artifacts: append([]software.Artifact(nil), locked.Artifacts...),
		Location: location, InstallLocation: location,
	}
}
