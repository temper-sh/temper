package remove_test

import (
	"context"
	"errors"
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
	removeverb "github.com/temper-sh/temper/internal/software/remove"
	"github.com/temper-sh/temper/internal/software/rootstate"
	"github.com/temper-sh/temper/internal/software/statestore"
)

func TestRunDryRunExactRemovalAndSecondRunClean(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "temper-root")
	desired := removeLock(t, "uv", "python-environment", "probe")
	lockPath := writeRemoveLock(t, parent, desired)
	fake := newRemoveAdapter("uv", "python-environment", installplan.EffectIsolated)
	family := removeFamily(t, fake)
	now := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	installOne(t, family, lockPath, root, "probe", now)
	beforeReceipt, _ := receiptstore.Read(root, "probe")
	beforeState, _ := statestore.Read(root)
	providerBefore := len(fake.observed)

	dry, err := removeverb.Run(context.Background(), removeverb.Options{
		LockPath: lockPath, Root: root, Installation: "probe", InvocationID: "remove-dry", DryRun: true,
		Now: func() time.Time { return now.Add(time.Second) },
	}, family)
	if err != nil {
		t.Fatalf("dry Run() error = %v", err)
	}
	if !dry.Changed || !dry.DryRun || dry.Effects != 1 || dry.Claims != 0 || fake.removeCalls != 0 {
		t.Fatalf("dry Run() = %#v, remove calls = %d", dry, fake.removeCalls)
	}
	afterDryReceipt, _ := receiptstore.Read(root, "probe")
	afterDryState, _ := statestore.Read(root)
	if string(afterDryReceipt.Data) != string(beforeReceipt.Data) || string(afterDryState.Data) != string(beforeState.Data) || len(fake.observed) != providerBefore {
		t.Fatal("dry removal mutated receipt, state, or provider")
	}

	result, err := removeverb.Run(context.Background(), removeverb.Options{
		LockPath: lockPath, Root: root, Installation: "probe", InvocationID: "remove-1", LeaseDuration: time.Minute,
		Now: func() time.Time { return now.Add(2 * time.Second) },
	}, family)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Changed || result.Effects != 1 || fake.removeCalls != 1 || len(fake.observed) != 0 {
		t.Fatalf("Run() = %#v, remove calls = %d, provider = %#v", result, fake.removeCalls, fake.observed)
	}
	removedReceipt, _ := receiptstore.Read(root, "probe")
	finalState, _ := statestore.Read(root)
	if removedReceipt.Exists() || len(finalState.Document.Operations) != 0 {
		t.Fatalf("receipt exists = %v, final state = %#v", removedReceipt.Exists(), finalState.Document)
	}
	finalBytes := append([]byte(nil), finalState.Data...)
	inspections := fake.inspectCalls

	second, err := removeverb.Run(context.Background(), removeverb.Options{
		LockPath: lockPath, Root: root, Installation: "probe", InvocationID: "remove-2",
		Now: func() time.Time { return now.Add(3 * time.Second) },
	}, family)
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if second.Changed || fake.removeCalls != 1 || fake.inspectCalls != inspections {
		t.Fatalf("second Run() = %#v, remove calls = %d inspections = %d", second, fake.removeCalls, fake.inspectCalls)
	}
	secondState, _ := statestore.Read(root)
	if string(secondState.Data) != string(finalBytes) {
		t.Fatal("clean second removal rewrote root state")
	}
}

func TestRunReleasesOneSharedClaimWithoutRemovalThenRetiresTheLast(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "temper-root")
	desired := removeLock(t, "homebrew", "system-package", "system")
	lockPath := writeRemoveLock(t, parent, desired)
	fake := newRemoveAdapter("homebrew", "system-package", installplan.EffectShared)
	family := removeFamily(t, fake)
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	installOne(t, family, lockPath, root, "experiment-a", now)
	installOne(t, family, lockPath, root, "experiment-b", now.Add(time.Second))
	if fake.installCalls != 1 {
		t.Fatalf("shared install calls = %d, want 1", fake.installCalls)
	}

	first, err := removeverb.Run(context.Background(), removeverb.Options{
		LockPath: lockPath, Root: root, Installation: "experiment-a", InvocationID: "remove-a", LeaseDuration: time.Minute,
		Now: func() time.Time { return now.Add(2 * time.Second) },
	}, family)
	if err != nil {
		t.Fatalf("remove first claimant: %v", err)
	}
	if first.Effects != 0 || first.Claims != 1 || fake.removeCalls != 0 || len(fake.observed) != 1 {
		t.Fatalf("first removal = %#v, remove calls = %d provider = %#v", first, fake.removeCalls, fake.observed)
	}
	state, _ := statestore.Read(root)
	key := installplan.SharedUnitKey("homebrew", "system", "tool")
	shared := state.Document.SharedUnits[key]
	if shared.Lifecycle != installplan.SharedActive || len(shared.Claims) != 1 {
		t.Fatalf("shared authority after first release = %#v", shared)
	}
	if _, ok := shared.Claims["experiment-b"]; !ok {
		t.Fatalf("remaining claims = %#v", shared.Claims)
	}

	last, err := removeverb.Run(context.Background(), removeverb.Options{
		LockPath: lockPath, Root: root, Installation: "experiment-b", InvocationID: "remove-b", LeaseDuration: time.Minute,
		Now: func() time.Time { return now.Add(3 * time.Second) },
	}, family)
	if err != nil {
		t.Fatalf("remove last claimant: %v", err)
	}
	if last.Effects != 1 || last.Claims != 1 || fake.removeCalls != 1 || len(fake.observed) != 0 {
		t.Fatalf("last removal = %#v, remove calls = %d provider = %#v", last, fake.removeCalls, fake.observed)
	}
	state, _ = statestore.Read(root)
	if len(state.Document.SharedUnits) != 0 || len(state.Document.Operations) != 0 {
		t.Fatalf("final shared state = %#v", state.Document)
	}
}

func TestRunPreservesPreExistingSharedSoftwareAndDropsTemperAuthority(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "temper-root")
	desired := removeLock(t, "homebrew", "system-package", "system")
	lockPath := writeRemoveLock(t, parent, desired)
	fake := newRemoveAdapter("homebrew", "system-package", installplan.EffectShared)
	installation := installplan.Installation{ID: "field-kit-base", Root: root}
	for _, locked := range desired.Units {
		location := filepath.Join("/opt/fake", locked.NativeName, locked.Version)
		fake.observed[fake.providerKey(installation, locked)] = exactRemoveObservation(locked, location)
	}
	family := removeFamily(t, fake)
	now := time.Date(2026, 8, 24, 11, 0, 0, 0, time.UTC)
	installOne(t, family, lockPath, root, installation.ID, now)

	result, err := removeverb.Run(context.Background(), removeverb.Options{
		LockPath: lockPath, Root: root, Installation: installation.ID, InvocationID: "remove-pre-existing", LeaseDuration: time.Minute,
		Now: func() time.Time { return now.Add(time.Second) },
	}, family)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Effects != 0 || fake.removeCalls != 0 || len(fake.observed) != 1 {
		t.Fatalf("Run() = %#v, remove calls = %d provider = %#v", result, fake.removeCalls, fake.observed)
	}
	state, _ := statestore.Read(root)
	receipted, _ := receiptstore.Read(root, installation.ID)
	if receipted.Exists() || len(state.Document.SharedUnits) != 0 {
		t.Fatalf("receipt exists = %v state = %#v", receipted.Exists(), state.Document)
	}
}

func TestRunReconcilesAnUnknownCompletedRemovalWithoutRepeatingIt(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "temper-root")
	desired := removeLock(t, "uv", "python-environment", "probe")
	lockPath := writeRemoveLock(t, parent, desired)
	fake := newRemoveAdapter("uv", "python-environment", installplan.EffectIsolated)
	family := removeFamily(t, fake)
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	installOne(t, family, lockPath, root, "probe", now)
	fake.failAfterRemoveOnce = true

	_, err := removeverb.Run(context.Background(), removeverb.Options{
		LockPath: lockPath, Root: root, Installation: "probe", InvocationID: "remove-unknown", LeaseDuration: time.Minute,
		Now: func() time.Time { return now.Add(time.Second) },
	}, family)
	if err == nil || !strings.Contains(err.Error(), "outcome unknown") {
		t.Fatalf("first Run() error = %v, want unknown outcome", err)
	}
	prepared, _ := statestore.Read(root)
	receipted, _ := receiptstore.Read(root, "probe")
	if len(prepared.Document.Operations) != 1 || !receipted.Exists() || fake.removeCalls != 1 || len(fake.observed) != 0 {
		t.Fatalf("prepared = %#v receipt = %v remove calls = %d provider = %#v", prepared.Document, receipted.Exists(), fake.removeCalls, fake.observed)
	}

	_, err = removeverb.Run(context.Background(), removeverb.Options{
		LockPath: lockPath, Root: root, Installation: "probe", InvocationID: "remove-live", LeaseDuration: time.Minute,
		Now: func() time.Time { return now.Add(30 * time.Second) },
	}, family)
	if !errors.Is(err, rootstate.ErrOperationBusy) {
		t.Fatalf("live recovery error = %v, want busy", err)
	}
	// Model a crash after the receipt deletion commit but before root-state
	// finalization. Prepared intent must be sufficient without the receipt.
	receipted, _ = receiptstore.Read(root, "probe")
	if removed, err := receipted.Remove(context.Background()); err != nil || !removed {
		t.Fatalf("simulate committed receipt removal = %v, %v", removed, err)
	}

	recovered, err := removeverb.Run(context.Background(), removeverb.Options{
		LockPath: lockPath, Root: root, Installation: "probe", InvocationID: "remove-recovery", LeaseDuration: time.Minute,
		Now: func() time.Time { return now.Add(2 * time.Minute) },
	}, family)
	if err != nil {
		t.Fatalf("recovery Run() error = %v", err)
	}
	if !recovered.Changed || recovered.Effects != 0 || fake.removeCalls != 1 {
		t.Fatalf("recovery = %#v, remove calls = %d", recovered, fake.removeCalls)
	}
	finalState, _ := statestore.Read(root)
	finalReceipt, _ := receiptstore.Read(root, "probe")
	if len(finalState.Document.Operations) != 0 || finalReceipt.Exists() {
		t.Fatalf("final state = %#v receipt exists = %v", finalState.Document, finalReceipt.Exists())
	}
}

func TestRetiringSharedGenerationRefusesANewClaimUntilRemovalFinalizes(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "temper-root")
	desired := removeLock(t, "homebrew", "system-package", "system")
	lockPath := writeRemoveLock(t, parent, desired)
	fake := newRemoveAdapter("homebrew", "system-package", installplan.EffectShared)
	family := removeFamily(t, fake)
	now := time.Date(2026, 8, 24, 13, 0, 0, 0, time.UTC)
	installOne(t, family, lockPath, root, "experiment-a", now)
	fake.failAfterRemoveOnce = true

	_, err := removeverb.Run(context.Background(), removeverb.Options{
		LockPath: lockPath, Root: root, Installation: "experiment-a", InvocationID: "remove-retiring", LeaseDuration: time.Minute,
		Now: func() time.Time { return now.Add(time.Second) },
	}, family)
	if err == nil || !strings.Contains(err.Error(), "outcome unknown") {
		t.Fatalf("remove error = %v, want unknown outcome", err)
	}
	state, _ := statestore.Read(root)
	key := installplan.SharedUnitKey("homebrew", "system", "tool")
	if state.Document.SharedUnits[key].Lifecycle != installplan.SharedRetiring {
		t.Fatalf("shared state = %#v", state.Document.SharedUnits[key])
	}

	_, err = installverb.Run(context.Background(), installverb.Options{
		LockPath: lockPath, Root: root, Installation: "experiment-b", InvocationID: "claim-retiring",
		LeaseDuration: time.Minute, Now: func() time.Time { return now.Add(2 * time.Second) },
	}, family)
	if err == nil || !strings.Contains(err.Error(), "retiring") {
		t.Fatalf("claim retiring generation error = %v", err)
	}
	if receipt, _ := receiptstore.Read(root, "experiment-b"); receipt.Exists() {
		t.Fatal("refused claimant received a receipt")
	}
}

type removeAdapter struct {
	descriptor          adapter.Descriptor
	observed            map[string]installplan.ObservedUnit
	inspectCalls        int
	installCalls        int
	removeCalls         int
	failAfterRemoveOnce bool
}

func newRemoveAdapter(id, method string, model installplan.EffectModel) *removeAdapter {
	return &removeAdapter{
		descriptor: adapter.Descriptor{
			ID: id, Method: method, Protocol: catalog.AdapterProtocolV1, EffectModel: string(model),
			Targets: []software.Target{{OS: "darwin", Arch: "arm64"}},
		},
		observed: map[string]installplan.ObservedUnit{},
	}
}

func (f *removeAdapter) Descriptor() adapter.Descriptor { return f.descriptor }

func (f *removeAdapter) Inspect(_ context.Context, request adapter.InspectRequest) (map[string]installplan.ObservedUnit, error) {
	f.inspectCalls++
	result := make(map[string]installplan.ObservedUnit, len(request.Units))
	for unitID, locked := range request.Units {
		key := f.providerKey(request.Installation, locked)
		if actual, ok := f.observed[key]; ok {
			result[unitID] = cloneRemoveObservation(actual)
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

func (f *removeAdapter) Install(_ context.Context, request adapter.InstallRequest) error {
	f.installCalls++
	for _, planned := range request.Group.Units {
		if planned.Action == installplan.ActionPreserve {
			continue
		}
		locked := request.Units[planned.ID]
		f.observed[f.providerKey(request.Installation, locked)] = exactRemoveObservation(locked, planned.Location)
	}
	return nil
}

func (f *removeAdapter) Remove(_ context.Context, request adapter.RemoveRequest) error {
	f.removeCalls++
	for _, planned := range request.Group.Units {
		if planned.Execute {
			delete(f.observed, f.providerKey(request.Installation, request.Units[planned.ID]))
		}
	}
	if f.failAfterRemoveOnce {
		f.failAfterRemoveOnce = false
		return errors.New("provider removal outcome unknown")
	}
	return nil
}

func (f *removeAdapter) providerKey(installation installplan.Installation, unit softwarelock.Unit) string {
	if f.descriptor.EffectModel == string(installplan.EffectShared) {
		return installplan.SharedUnitKey(unit.Adapter, unit.Scope, unit.NativeName)
	}
	return installation.ID + ":" + unit.Adapter + ":" + unit.Scope + ":" + unit.NativeName
}

func removeFamily(t *testing.T, members ...adapter.InstallationAdapter) adapter.InstallationFamily {
	t.Helper()
	family, err := adapter.NewInstallationFamily(members...)
	if err != nil {
		t.Fatal(err)
	}
	return family
}

func installOne(t *testing.T, family adapter.InstallationFamily, lockPath, root, installationID string, now time.Time) {
	t.Helper()
	_, err := installverb.Run(context.Background(), installverb.Options{
		LockPath: lockPath, Root: root, Installation: installationID, InvocationID: "install-" + installationID,
		LeaseDuration: time.Minute, Now: func() time.Time { return now },
	}, family)
	if err != nil {
		t.Fatalf("install %s: %v", installationID, err)
	}
}

func removeLock(t *testing.T, adapterID, method, scope string) softwarelock.Document {
	t.Helper()
	unitID := adapterID + ":" + scope + ":tool"
	document := softwarelock.Document{
		Schema: softwarelock.SchemaV1,
		Provenance: softwarelock.Provenance{Experiment: &softwarelock.ExperimentIdentity{
			Schema: "field-kit-experiment/v1", ID: "remove-fixture", DefinitionSHA256: strings.Repeat("a", 64),
		}},
		Target: software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6"}, Resolved: "2026-08-24",
		Selections: map[string]softwarelock.Selection{"tool": {
			Provenance: softwarelock.ProvenanceExperiment, Method: method, Adapter: adapterID, RecipeRevision: "tool/v1", RootUnit: unitID,
		}},
		Units: map[string]softwarelock.Unit{unitID: {
			Adapter: adapterID, Scope: scope, NativeName: "tool", Version: "1.0.0", Dependencies: []string{},
			Artifacts: []software.Artifact{{Locator: "https://example.invalid/tool", SHA256: strings.Repeat("b", 64)}},
		}},
	}
	if err := document.Validate(); err != nil {
		t.Fatalf("fixture lock invalid: %v", err)
	}
	return document
}

func writeRemoveLock(t *testing.T, directory string, document softwarelock.Document) string {
	t.Helper()
	data, err := softwarelock.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, document.Selections["tool"].Adapter+"-software.lock.yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func exactRemoveObservation(locked softwarelock.Unit, location string) installplan.ObservedUnit {
	return installplan.ObservedUnit{
		Present: true, Adapter: locked.Adapter, Scope: locked.Scope, NativeName: locked.NativeName,
		Version: locked.Version, Revision: locked.Revision,
		Dependencies: append([]string(nil), locked.Dependencies...), Artifacts: append([]software.Artifact(nil), locked.Artifacts...),
		Location: location, InstallLocation: location,
	}
}

func cloneRemoveObservation(unit installplan.ObservedUnit) installplan.ObservedUnit {
	unit.Dependencies = append([]string(nil), unit.Dependencies...)
	unit.Artifacts = append([]software.Artifact(nil), unit.Artifacts...)
	return unit
}
