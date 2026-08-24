package resolve_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/catalog"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	softwareresolve "github.com/temper-sh/temper/internal/software/resolve"
	"github.com/temper-sh/temper/internal/software/testedstatus"
)

var target = software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6"}

type fakeResolver struct {
	candidates []software.Candidate
	byPackage  map[string][]software.Candidate
	err        error
	calls      int
	onRead     func()
}

func (f *fakeResolver) Descriptor() adapter.Descriptor {
	return adapter.Descriptor{
		ID: "homebrew", Method: "system-package", Protocol: catalog.AdapterProtocolV1,
		EffectModel: "shared", Targets: []software.Target{{OS: "darwin", Arch: "arm64"}},
	}
}

func (f *fakeResolver) Candidates(_ context.Context, request adapter.ResolveRequest) ([]software.Candidate, error) {
	f.calls++
	if f.onRead != nil {
		f.onRead()
	}
	if f.byPackage != nil {
		return f.byPackage[request.Package], f.err
	}
	return f.candidates, f.err
}

func TestRunWritesLockAndSecondRunPreservesExactBytes(t *testing.T) {
	directory := t.TempDir()
	lockPath := filepath.Join(directory, "software.lock.yaml")
	resolver := &fakeResolver{candidates: []software.Candidate{candidate("1.3.0", "rev/130")}}
	family := resolverFamily(t, resolver)
	options := softwareresolve.Options{
		LockPath: lockPath, Target: target,
		Requests: []softwareresolve.Request{{Package: "llama-swap", Method: "system-package"}},
		Now:      func() time.Time { return time.Date(2026, 8, 20, 22, 0, 0, 0, time.UTC) },
	}

	result, err := softwareresolve.Run(context.Background(), options, supply(t), family)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || len(result.Entries) != 1 || result.Entries[0].Version != "1.3.0" {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Statuses) != 1 || result.Statuses[0].Package != "llama-swap" || result.Statuses[0].Status != testedstatus.PolicyEligibleUntested {
		t.Fatalf("tested statuses = %#v", result.Statuses)
	}
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := softwarelock.Parse(before)
	if err != nil {
		t.Fatal(err)
	}
	if locked.Resolved != "2026-08-20" {
		t.Errorf("resolved = %q", locked.Resolved)
	}

	options.Now = func() time.Time { return time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC) }
	second, err := softwareresolve.Run(context.Background(), options, supply(t), adapter.ResolverFamily{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed {
		t.Fatalf("second result = %#v", second)
	}
	if !reflect.DeepEqual(second.Statuses, result.Statuses) {
		t.Fatalf("second tested statuses = %#v, want %#v", second.Statuses, result.Statuses)
	}
	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("second run changed software lock bytes or resolved date")
	}
	if resolver.calls != 1 {
		t.Errorf("provider reads = %d, want one", resolver.calls)
	}
}

func TestDryRunDoesNotCreateOrStageSoftwareLock(t *testing.T) {
	directory := t.TempDir()
	lockPath := filepath.Join(directory, "software.lock.yaml")
	resolver := &fakeResolver{candidates: []software.Candidate{candidate("1.3.0", "rev/130")}}
	result, err := softwareresolve.Run(context.Background(), softwareresolve.Options{
		LockPath: lockPath, Target: target, DryRun: true,
		Requests: []softwareresolve.Request{{Package: "llama-swap", Method: "system-package"}},
	}, supply(t), resolverFamily(t, resolver))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || !result.DryRun {
		t.Fatalf("result = %#v", result)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("dry-run created files: %v", entries)
	}
}

func TestRunPreservesExistingSelectionWhileFillingMissingOne(t *testing.T) {
	directory := t.TempDir()
	lockPath := filepath.Join(directory, "software.lock.yaml")
	snapshot := supply(t)
	snapshot.Document.Packages["llama-cpp"] = catalog.Package{
		Description: "engine", Recipes: map[string]catalog.Recipe{"homebrew": {
			Method: "system-package", RecipeRevision: "llama-cpp/v1",
			Source:        catalog.Source{Kind: "homebrew-formula", Tap: "temper/tap", Formula: "llama.cpp"},
			VersionScheme: "semver", Selection: catalog.Selection{Policy: "latest", MinimumCompatible: "1.0.0"},
			Tested: []catalog.Tested{{
				RootVersion: "1.1.0", ClosureDigest: strings.Repeat("7", 64), Target: target, Evidence: "fixture",
			}},
		}},
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	existing := lockDocument(snapshot, "1.2.0", "rev/120")
	existingBytes, err := softwarelock.Marshal(existing)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, existingBytes, 0o640); err != nil {
		t.Fatal(err)
	}
	newCandidate := singlePackageCandidate("homebrew:system:llama-cpp", "llama.cpp", "1.1.0", "rev/cpp110")
	resolver := &fakeResolver{byPackage: map[string][]software.Candidate{"llama-cpp": {newCandidate}}}
	result, err := softwareresolve.Run(context.Background(), softwareresolve.Options{
		LockPath: lockPath, Target: target,
		Requests: []softwareresolve.Request{
			{Package: "llama-swap", Method: "system-package"},
			{Package: "llama-cpp", Method: "system-package"},
		},
	}, snapshot, resolverFamily(t, resolver))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || resolver.calls != 1 {
		t.Fatalf("result = %#v, provider reads = %d", result, resolver.calls)
	}
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := softwarelock.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(locked.Selections["llama-swap"], existing.Selections["llama-swap"]) ||
		!reflect.DeepEqual(locked.Units["homebrew:system:llama-swap"], existing.Units["homebrew:system:llama-swap"]) {
		t.Fatal("fill-missing resolution moved the existing selection")
	}
	if locked.Units["homebrew:system:llama-cpp"].Version != "1.1.0" {
		t.Fatal("missing selection was not added")
	}
	info, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Errorf("lock mode = %o, want preserved 640", info.Mode().Perm())
	}
}

func TestProviderFailureLeavesAbsentSoftwareLockAbsent(t *testing.T) {
	directory := t.TempDir()
	lockPath := filepath.Join(directory, "software.lock.yaml")
	resolver := &fakeResolver{err: errors.New("provider offline")}
	_, err := softwareresolve.Run(context.Background(), softwareresolve.Options{
		LockPath: lockPath, Target: target,
		Requests: []softwareresolve.Request{{Package: "llama-swap", Method: "system-package"}},
	}, supply(t), resolverFamily(t, resolver))
	if err == nil || !strings.Contains(err.Error(), "provider offline") {
		t.Fatalf("Run() error = %v", err)
	}
	if _, statErr := os.Lstat(lockPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("lock exists after provider failure: %v", statErr)
	}
}

func TestAllAdapterBindingsArePreflightedBeforeProviderReads(t *testing.T) {
	directory := t.TempDir()
	snapshot := supply(t)
	snapshot.Document.Methods["python-environment"] = catalog.Method{Description: "python"}
	snapshot.Document.Adapters["uv"] = catalog.Adapter{
		Method: "python-environment", Protocol: catalog.AdapterProtocolV1, EffectModel: "isolated",
	}
	snapshot.Document.TargetBindings = append(snapshot.Document.TargetBindings, catalog.TargetBinding{
		Method: "python-environment", Target: software.Target{OS: "darwin", Arch: "arm64"}, Adapter: "uv",
	})
	copyPackage := snapshot.Document.Packages["llama-swap"]
	snapshot.Document.Packages["z-bad"] = copyPackage
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	resolver := &fakeResolver{candidates: []software.Candidate{candidate("1.3.0", "rev/130")}}
	_, err := softwareresolve.Run(context.Background(), softwareresolve.Options{
		LockPath: filepath.Join(directory, "software.lock.yaml"), Target: target,
		Requests: []softwareresolve.Request{
			{Package: "llama-swap", Method: "system-package"},
			{Package: "z-bad", Method: "python-environment"},
		},
	}, snapshot, resolverFamily(t, resolver))
	if err == nil || !strings.Contains(err.Error(), `adapter "uv" is not compiled`) {
		t.Fatalf("Run() error = %v, want unbuilt adapter refusal", err)
	}
	if resolver.calls != 0 {
		t.Fatalf("provider reads = %d, want none before complete preflight", resolver.calls)
	}
}

func TestConcurrentSoftwareLockWriterIsNotOverwritten(t *testing.T) {
	directory := t.TempDir()
	lockPath := filepath.Join(directory, "software.lock.yaml")
	snapshot := supply(t)
	concurrent, err := softwarelock.Marshal(lockDocument(snapshot, "1.2.0", "rev/120"))
	if err != nil {
		t.Fatal(err)
	}
	resolver := &fakeResolver{candidates: []software.Candidate{candidate("1.3.0", "rev/130")}}
	resolver.onRead = func() {
		if err := os.WriteFile(lockPath, concurrent, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, err = softwareresolve.Run(context.Background(), softwareresolve.Options{
		LockPath: lockPath, Target: target,
		Requests: []softwareresolve.Request{{Package: "llama-swap", Method: "system-package"}},
	}, snapshot, resolverFamily(t, resolver))
	if err == nil || !strings.Contains(err.Error(), "changed concurrently") {
		t.Fatalf("Run() error = %v, want concurrent writer refusal", err)
	}
	got, readErr := os.ReadFile(lockPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(concurrent) {
		t.Fatal("concurrent software lock was overwritten")
	}
}

func resolverFamily(t *testing.T, resolver adapter.CandidateResolver) adapter.ResolverFamily {
	t.Helper()
	family, err := adapter.NewResolverFamily(resolver)
	if err != nil {
		t.Fatal(err)
	}
	return family
}

func supply(t *testing.T) catalog.Snapshot {
	t.Helper()
	document := catalog.Document{
		Schema: catalog.SchemaV1, Sequence: 8, PublishedAt: "2026-08-20T09:00:00Z",
		Methods: map[string]catalog.Method{"system-package": {Description: "system"}},
		Adapters: map[string]catalog.Adapter{"homebrew": {
			Method: "system-package", Protocol: catalog.AdapterProtocolV1, EffectModel: "shared",
		}},
		TargetBindings: []catalog.TargetBinding{{
			Method: "system-package", Target: software.Target{OS: "darwin", Arch: "arm64"}, Adapter: "homebrew",
		}},
		Packages: map[string]catalog.Package{"llama-swap": {
			Description: "router", Recipes: map[string]catalog.Recipe{"homebrew": {
				Method: "system-package", RecipeRevision: "llama/v1",
				Source:        catalog.Source{Kind: "homebrew-formula", Tap: "temper/tap", Formula: "llama-swap"},
				VersionScheme: "semver", Selection: catalog.Selection{Policy: "latest", MinimumCompatible: "1.0.0"},
				Tested: []catalog.Tested{{
					RootVersion: "1.3.0", ClosureDigest: strings.Repeat("a", 64), Target: target, Evidence: "fixture",
				}},
			}},
		}},
	}
	if err := document.Validate(); err != nil {
		t.Fatal(err)
	}
	return catalog.Snapshot{Document: document, SHA256: strings.Repeat("c", 64)}
}

func candidate(version, revision string) software.Candidate {
	return singlePackageCandidate("homebrew:system:llama-swap", "llama-swap", version, revision)
}

func singlePackageCandidate(unitID, nativeName, version, revision string) software.Candidate {
	return software.Candidate{RootUnit: unitID, Units: map[string]software.ResolvedUnit{
		unitID: {
			Scope: "system", NativeName: nativeName, Version: version, Revision: revision,
			Artifacts: []software.Artifact{{Locator: "https://example.invalid/" + nativeName + ".tar.gz", SHA256: strings.Repeat("b", 64)}},
		},
	}}
}

func lockDocument(snapshot catalog.Snapshot, version, revision string) softwarelock.Document {
	return softwarelock.Document{
		Schema:     softwarelock.SchemaV1,
		Provenance: softwarelock.Provenance{Catalog: &softwarelock.CatalogIdentity{Schema: snapshot.Document.Schema, Sequence: snapshot.Document.Sequence, SHA256: snapshot.SHA256}},
		Target:     target, Resolved: "2026-08-19",
		Selections: map[string]softwarelock.Selection{"llama-swap": {
			Provenance: softwarelock.ProvenanceCatalog,
			Method:     "system-package", Adapter: "homebrew", RecipeRevision: "llama/v1", RootUnit: "homebrew:system:llama-swap",
		}},
		Units: map[string]softwarelock.Unit{"homebrew:system:llama-swap": {
			Adapter: "homebrew", Scope: "system", NativeName: "llama-swap", Version: version, Revision: revision,
			Dependencies: []string{}, Artifacts: []softwarelock.Artifact{{Locator: "https://example.invalid/llama.tar.gz", SHA256: strings.Repeat("b", 64)}},
		}},
	}
}
