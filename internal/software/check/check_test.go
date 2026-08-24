package check_test

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
	checkverb "github.com/temper-sh/temper/internal/software/check"
	"github.com/temper-sh/temper/internal/software/checkplan"
	installverb "github.com/temper-sh/temper/internal/software/install"
	"github.com/temper-sh/temper/internal/software/installplan"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/receiptstore"
	"github.com/temper-sh/temper/internal/software/statestore"
)

func TestRunAuditsAnInstalledEnvironmentWithoutChangingAnyState(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "temper-root")
	desired := checkLock(t)
	lockPath := writeCheckLock(t, parent, desired)
	fake := newCheckAdapter()
	family, err := adapter.NewInstallationFamily(fake)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	if _, err := installverb.Run(context.Background(), installverb.Options{
		LockPath: lockPath, Root: root, Installation: "probe", InvocationID: "install-run",
		LeaseDuration: time.Minute, Now: func() time.Time { return now },
	}, family); err != nil {
		t.Fatalf("install fixture: %v", err)
	}
	beforeReceipt, err := receiptstore.Read(root, "probe")
	if err != nil {
		t.Fatal(err)
	}
	beforeState, err := statestore.Read(root)
	if err != nil {
		t.Fatal(err)
	}

	result, err := checkverb.Run(context.Background(), checkverb.Options{
		LockPath: lockPath, Root: root, Installation: "probe",
	}, family)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Exact() || len(result.Units) != 1 || result.Units[0].Status != checkplan.UnitExact || fake.installCalls != 1 {
		t.Fatalf("Run() = %#v, install calls = %d", result, fake.installCalls)
	}
	afterReceipt, _ := receiptstore.Read(root, "probe")
	afterState, _ := statestore.Read(root)
	if string(afterReceipt.Data) != string(beforeReceipt.Data) || string(afterState.Data) != string(beforeState.Data) {
		t.Fatal("software check changed receipt or root-state bytes")
	}
}

func TestRunReturnsProviderDriftAsACompletedFinding(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "temper-root")
	desired := checkLock(t)
	lockPath := writeCheckLock(t, parent, desired)
	fake := newCheckAdapter()
	family, err := adapter.NewInstallationFamily(fake)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := installverb.Run(context.Background(), installverb.Options{
		LockPath: lockPath, Root: root, Installation: "probe", InvocationID: "install-run",
		LeaseDuration: time.Minute, Now: func() time.Time { return time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC) },
	}, family); err != nil {
		t.Fatalf("install fixture: %v", err)
	}
	for key, unit := range fake.observed {
		unit.Version = "9.9.9"
		fake.observed[key] = unit
	}

	result, err := checkverb.Run(context.Background(), checkverb.Options{
		LockPath: lockPath, Root: root, Installation: "probe",
	}, family)
	if err != nil {
		t.Fatalf("Run() error = %v, want completed drift report", err)
	}
	if result.Exact() || result.Units[0].Status != checkplan.UnitDrifted || result.Findings[0].Code != checkplan.CodeProviderDrift {
		t.Fatalf("Run() = %#v", result)
	}
}

func TestRunOnAnAbsentRootReportsMissingWithoutCreatingIt(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "temper-root")
	lockPath := writeCheckLock(t, parent, checkLock(t))
	fake := newCheckAdapter()
	family, err := adapter.NewInstallationFamily(fake)
	if err != nil {
		t.Fatal(err)
	}

	result, err := checkverb.Run(context.Background(), checkverb.Options{
		LockPath: lockPath, Root: root, Installation: "probe",
	}, family)
	if err != nil {
		t.Fatal(err)
	}
	if result.Units[0].Status != checkplan.UnitMissing || result.Findings[0].Code != checkplan.CodeProviderMissing {
		t.Fatalf("Run() = %#v", result)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("read-only check created a root: %v", err)
	}
}

func TestRunReportsRequiredBaseProviderDriftWithoutRepairingIt(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "temper-root")
	fake := newCheckAdapter()
	family, err := adapter.NewInstallationFamily(fake)
	if err != nil {
		t.Fatal(err)
	}
	base := checkLockFor(t, "base")
	baseLockPath := writeCheckLock(t, parent, base)
	if _, err := installverb.Run(context.Background(), installverb.Options{
		LockPath: baseLockPath, Root: root, Installation: "field-kit-base", InvocationID: "base-run",
		LeaseDuration: time.Minute, Now: func() time.Time { return time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC) },
	}, family); err != nil {
		t.Fatalf("install base: %v", err)
	}
	baseReceipt, err := receiptstore.Read(root, "field-kit-base")
	if err != nil {
		t.Fatal(err)
	}
	baseDigest, err := base.SemanticDigest()
	if err != nil {
		t.Fatal(err)
	}
	experiment := checkLockFor(t, "experiment")
	experiment.Requires = []softwarelock.InstallationRequirement{{SoftwareLockDigest: baseDigest}}
	experimentLockPath := writeCheckLock(t, parent, experiment)

	exact, err := checkverb.Run(context.Background(), checkverb.Options{
		LockPath: experimentLockPath, Root: root, Installation: "experiment-a",
		RequiredReceiptPaths: []string{baseReceipt.Path()},
	}, family)
	if err != nil {
		t.Fatal(err)
	}
	if len(exact.Requirements) != 1 || exact.Requirements[0].Status != checkplan.RequirementExact {
		t.Fatalf("exact base requirement = %#v", exact.Requirements)
	}

	for key, unit := range fake.observed {
		if strings.HasPrefix(key, "field-kit-base:") {
			unit.Version = "9.9.9"
			fake.observed[key] = unit
		}
	}
	drifted, err := checkverb.Run(context.Background(), checkverb.Options{
		LockPath: experimentLockPath, Root: root, Installation: "experiment-a",
		RequiredReceiptPaths: []string{baseReceipt.Path()},
	}, family)
	if err != nil {
		t.Fatalf("Run() error = %v, want completed requirement report", err)
	}
	if drifted.Requirements[0].Status != checkplan.RequirementDrifted || !hasCheckFinding(drifted, checkplan.CodeRequiredReceiptDrift) {
		t.Fatalf("drifted base requirement = %#v", drifted)
	}
}

func TestRunPropagatesAProviderReadFailureWithoutAReport(t *testing.T) {
	parent := t.TempDir()
	lockPath := writeCheckLock(t, parent, checkLock(t))
	fake := newCheckAdapter()
	fake.inspectErr = errors.New("provider unavailable")
	family, err := adapter.NewInstallationFamily(fake)
	if err != nil {
		t.Fatal(err)
	}

	_, err = checkverb.Run(context.Background(), checkverb.Options{
		LockPath: lockPath, Root: filepath.Join(parent, "temper-root"), Installation: "probe",
	}, family)
	if err == nil || !strings.Contains(err.Error(), "provider unavailable") {
		t.Fatalf("Run() error = %v", err)
	}
}

type checkAdapter struct {
	descriptor   adapter.Descriptor
	observed     map[string]installplan.ObservedUnit
	installCalls int
	inspectErr   error
}

func newCheckAdapter() *checkAdapter {
	return &checkAdapter{
		descriptor: adapter.Descriptor{
			ID: "uv", Method: "python-environment", Protocol: catalog.AdapterProtocolV1, EffectModel: string(installplan.EffectIsolated),
			Targets: []software.Target{{OS: "darwin", Arch: "arm64"}},
		},
		observed: map[string]installplan.ObservedUnit{},
	}
}

func (f *checkAdapter) Descriptor() adapter.Descriptor { return f.descriptor }

func (f *checkAdapter) Inspect(_ context.Context, request adapter.InspectRequest) (map[string]installplan.ObservedUnit, error) {
	if f.inspectErr != nil {
		return nil, f.inspectErr
	}
	result := make(map[string]installplan.ObservedUnit, len(request.Units))
	for unitID, locked := range request.Units {
		key := request.Installation.ID + ":" + locked.Adapter + ":" + locked.Scope + ":" + locked.NativeName
		if actual, ok := f.observed[key]; ok {
			result[unitID] = cloneCheckObservation(actual)
			continue
		}
		result[unitID] = installplan.ObservedUnit{
			InstallLocation: filepath.Join(installplan.InstallationRoot(request.Installation), "environment", locked.NativeName),
		}
	}
	return result, nil
}

func (f *checkAdapter) Install(_ context.Context, request adapter.InstallRequest) error {
	f.installCalls++
	for _, planned := range request.Group.Units {
		locked := request.Units[planned.ID]
		key := request.Installation.ID + ":" + locked.Adapter + ":" + locked.Scope + ":" + locked.NativeName
		f.observed[key] = installplan.ObservedUnit{
			Present: true, Adapter: locked.Adapter, Scope: locked.Scope, NativeName: locked.NativeName,
			Version: locked.Version, Revision: locked.Revision,
			Dependencies: append([]string(nil), locked.Dependencies...), Artifacts: append([]software.Artifact(nil), locked.Artifacts...),
			Location: planned.Location, InstallLocation: planned.Location,
		}
	}
	return nil
}

func (f *checkAdapter) Remove(context.Context, adapter.RemoveRequest) error {
	return errors.New("check adapter removal must not be called")
}

func checkLock(t *testing.T) softwarelock.Document {
	t.Helper()
	return checkLockFor(t, "probe")
}

func checkLockFor(t *testing.T, scope string) softwarelock.Document {
	t.Helper()
	unitID := "uv:" + scope + ":tool"
	document := softwarelock.Document{
		Schema: softwarelock.SchemaV1,
		Provenance: softwarelock.Provenance{Experiment: &softwarelock.ExperimentIdentity{
			Schema: "field-kit-experiment/v1", ID: "check-fixture", DefinitionSHA256: strings.Repeat("a", 64),
		}},
		Target:   software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6"},
		Resolved: "2026-08-24",
		Selections: map[string]softwarelock.Selection{
			"tool": {Provenance: softwarelock.ProvenanceExperiment, Method: "python-environment", Adapter: "uv", RecipeRevision: "tool/v1", RootUnit: unitID},
		},
		Units: map[string]softwarelock.Unit{
			unitID: {
				Adapter: "uv", Scope: scope, NativeName: "tool", Version: "1.0.0", Dependencies: []string{},
				Artifacts: []software.Artifact{{Locator: "https://example.invalid/tool", SHA256: strings.Repeat("b", 64)}},
			},
		},
	}
	if err := document.Validate(); err != nil {
		t.Fatal(err)
	}
	return document
}

func writeCheckLock(t *testing.T, directory string, document softwarelock.Document) string {
	t.Helper()
	data, err := softwarelock.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "software.lock.yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func cloneCheckObservation(unit installplan.ObservedUnit) installplan.ObservedUnit {
	unit.Dependencies = append([]string(nil), unit.Dependencies...)
	unit.Artifacts = append([]software.Artifact(nil), unit.Artifacts...)
	return unit
}

func hasCheckFinding(result checkplan.Result, code checkplan.Code) bool {
	for _, finding := range result.Findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}
