package uv

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/installplan"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/removeplan"
)

func TestInstallationRoundTripRepairsAtomicallyAndRemovesOnlyItsScope(t *testing.T) {
	runtime := runtimeArchiveFixture(t, []runtimeTarEntry{
		{name: "python/bin/python3", body: "managed-python", mode: 0o755},
		{name: "python/bin/python", link: "python3", kind: tar.TypeSymlink},
		{name: "python/lib/runtime", body: "runtime", mode: 0o644},
	})
	wheel := []byte("locked wheel bytes")
	wheelLocator := "https://files.pythonhosted.org/packages/rapid_mlx-0.13.3-py3-none-any.whl"
	reader := &uvMemoryReader{content: map[string][]byte{runtime.locator: runtime.data, wheelLocator: wheel}}
	installer := &recordingEnvironmentInstaller{}
	member, err := NewInstallationAdapter(reader, installer)
	if err != nil {
		t.Fatal(err)
	}
	installation := installplan.Installation{ID: "experiment", Root: t.TempDir()}
	target := software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "26.0"}
	units := uvLockedUnits(runtime, wheelLocator, wheel)
	location := environmentLocation(installation, "rapid-mlx")
	add := uvInstallGroup(units, location, installplan.ActionAdd)

	if err := member.Install(context.Background(), adapter.InstallRequest{Target: target, Installation: installation, Group: add, Units: units}); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if reader.opens != 2 || installer.calls != 1 {
		t.Fatalf("reads/installs = %d/%d, want 2/1", reader.opens, installer.calls)
	}
	command := filepath.Join(location, "bin", "rapid-mlx")
	if info, err := os.Stat(command); err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("installed command = %v, %v", info, err)
	}
	observed := inspectUVGroup(t, member, target, installation, units)
	for id, unit := range units {
		if !installplan.MatchesLock(unit, observed[id]) || observed[id].Location != location {
			t.Fatalf("observation %q = %#v", id, observed[id])
		}
	}

	preserve := uvInstallGroup(units, location, installplan.ActionPreserve)
	if err := member.Install(context.Background(), adapter.InstallRequest{Target: target, Installation: installation, Group: preserve, Units: units}); err != nil {
		t.Fatal(err)
	}
	if reader.opens != 2 || installer.calls != 1 {
		t.Fatalf("preserve performed effects: reads/installs = %d/%d", reader.opens, installer.calls)
	}

	if err := os.WriteFile(command, []byte("drift"), 0o755); err != nil {
		t.Fatal(err)
	}
	drifted := inspectUVGroup(t, member, target, installation, units)
	if !drifted["uv:rapid-mlx:rapid-mlx"].Present || installplan.MatchesLock(units["uv:rapid-mlx:rapid-mlx"], drifted["uv:rapid-mlx:rapid-mlx"]) {
		t.Fatalf("drift was not reported: %#v", drifted)
	}
	replace := uvInstallGroup(units, location, installplan.ActionReplace)
	if err := member.Install(context.Background(), adapter.InstallRequest{Target: target, Installation: installation, Group: replace, Units: units}); err != nil {
		t.Fatal(err)
	}
	if repaired := inspectUVGroup(t, member, target, installation, units); !installplan.MatchesLock(units["uv:rapid-mlx:rapid-mlx"], repaired["uv:rapid-mlx:rapid-mlx"]) {
		t.Fatalf("repair is not exact: %#v", repaired)
	}

	sibling := filepath.Join(installplan.InstallationRoot(installation), adapterID, "other", "keep")
	if err := os.MkdirAll(filepath.Dir(sibling), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sibling, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	remove := uvRemoveGroup(units, location)
	request := adapter.RemoveRequest{Target: target, Installation: installation, Group: remove, Units: units}
	if err := member.Remove(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if err := member.Remove(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Fatalf("remove touched sibling: %v", err)
	}
	for id, unit := range inspectUVGroup(t, member, target, installation, units) {
		if unit.Present || unit.InstallLocation != location {
			t.Fatalf("removed observation %q = %#v", id, unit)
		}
	}
}

func TestFailedReplacementLeavesCurrentEnvironmentUnchanged(t *testing.T) {
	runtime := runtimeArchiveFixture(t, []runtimeTarEntry{{name: "python/bin/python3", body: "python", mode: 0o755}})
	wheel := []byte("wheel")
	wheelLocator := "https://files.pythonhosted.org/packages/rapid_mlx-0.13.3-py3-none-any.whl"
	reader := &uvMemoryReader{content: map[string][]byte{runtime.locator: runtime.data, wheelLocator: wheel}}
	installer := &recordingEnvironmentInstaller{}
	member, _ := NewInstallationAdapter(reader, installer)
	installation := installplan.Installation{ID: "experiment", Root: t.TempDir()}
	target := software.Target{OS: "darwin", Arch: "arm64"}
	units := uvLockedUnits(runtime, wheelLocator, wheel)
	location := environmentLocation(installation, "rapid-mlx")
	if err := member.Install(context.Background(), adapter.InstallRequest{Target: target, Installation: installation, Group: uvInstallGroup(units, location, installplan.ActionAdd), Units: units}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(location, "bin", "rapid-mlx"))
	if err != nil {
		t.Fatal(err)
	}
	installer.failure = errors.New("injected installer failure")
	err = member.Install(context.Background(), adapter.InstallRequest{Target: target, Installation: installation, Group: uvInstallGroup(units, location, installplan.ActionReplace), Units: units})
	if err == nil || !strings.Contains(err.Error(), "injected installer failure") {
		t.Fatalf("Install() error = %v", err)
	}
	after, err := os.ReadFile(filepath.Join(location, "bin", "rapid-mlx"))
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("current environment changed: %q, %v", after, err)
	}
	if observed := inspectUVGroup(t, member, target, installation, units); !installplan.MatchesLock(units["uv:rapid-mlx:rapid-mlx"], observed["uv:rapid-mlx:rapid-mlx"]) {
		t.Fatalf("old environment is no longer exact: %#v", observed)
	}
}

func TestInstallationRejectsUnsafeRuntimeArchiveAndWrongWheelHash(t *testing.T) {
	unsafe := runtimeArchiveFixture(t, []runtimeTarEntry{{name: "python/../escape", body: "bad", mode: 0o644}})
	wheel := []byte("wheel")
	wheelLocator := "https://files.pythonhosted.org/packages/rapid_mlx-0.13.3-py3-none-any.whl"
	tests := []struct {
		name    string
		runtime runtimeArchive
		content []byte
		want    string
	}{
		{name: "unsafe runtime", runtime: unsafe, content: wheel, want: "unsafe"},
		{name: "wheel hash", runtime: runtimeArchiveFixture(t, []runtimeTarEntry{{name: "python/bin/python3", body: "python", mode: 0o755}}), content: []byte("different"), want: "artifact"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := &uvMemoryReader{content: map[string][]byte{test.runtime.locator: test.runtime.data, wheelLocator: test.content}}
			member, _ := NewInstallationAdapter(reader, &recordingEnvironmentInstaller{})
			installation := installplan.Installation{ID: "experiment", Root: t.TempDir()}
			units := uvLockedUnits(test.runtime, wheelLocator, wheel)
			location := environmentLocation(installation, "rapid-mlx")
			err := member.Install(context.Background(), adapter.InstallRequest{
				Target: software.Target{OS: "darwin", Arch: "arm64"}, Installation: installation,
				Group: uvInstallGroup(units, location, installplan.ActionAdd), Units: units,
			})
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), test.want) {
				t.Fatalf("Install() error = %v, want %q", err, test.want)
			}
			if _, err := os.Lstat(filepath.Join(uvGroupRoot(installation, "rapid-mlx"), "current")); !os.IsNotExist(err) {
				t.Fatalf("failed install published current: %v", err)
			}
		})
	}
}

func TestPipInstallerUsesOnlyTheLockedManagedEnvironment(t *testing.T) {
	generation := t.TempDir()
	environment := filepath.Join(generation, "environment")
	wheelhouse := filepath.Join(generation, ".temper", "artifacts")
	if err := os.MkdirAll(filepath.Join(environment, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(wheelhouse, 0o755); err != nil {
		t.Fatal(err)
	}
	python := filepath.Join(environment, "bin", "python3")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > pip-invocation\nenv | sort >> pip-invocation\n"
	if err := os.WriteFile(python, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	requirements := filepath.Join(generation, ".temper", "requirements.txt")
	if err := os.WriteFile(requirements, []byte("tool==1 --hash=sha256:"+strings.Repeat("a", 64)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PIP_INDEX_URL", "https://private.invalid")
	t.Setenv("PYTHONPATH", "/private")
	if err := (PipInstaller{}).Install(context.Background(), EnvironmentInstallRequest{
		PythonPath: python, EnvironmentPath: environment, WheelhousePath: wheelhouse, RequirementsPath: requirements,
	}); err != nil {
		t.Fatal(err)
	}
	record, err := os.ReadFile(filepath.Join(environment, "pip-invocation"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(record)
	for _, wanted := range []string{"--no-index", "--no-deps", "--require-hashes", "PIP_CONFIG_FILE=/dev/null", "PYTHONNOUSERSITE=1"} {
		if !strings.Contains(text, wanted) {
			t.Errorf("invocation omits %q:\n%s", wanted, text)
		}
	}
	for _, forbidden := range []string{"private.invalid", "PYTHONPATH=/private"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("invocation retains %q:\n%s", forbidden, text)
		}
	}
}

func TestInspectHandlesIndependentUVScopesInOneLock(t *testing.T) {
	runtime := runtimeArchiveFixture(t, []runtimeTarEntry{{name: "python/bin/python3", body: "python", mode: 0o755}})
	wheel := []byte("wheel")
	wheelLocator := "https://files.pythonhosted.org/packages/rapid_mlx-0.13.3-py3-none-any.whl"
	first := uvLockedUnits(runtime, wheelLocator, wheel)
	second := map[string]softwarelock.Unit{}
	for id, unit := range first {
		unit.Scope = "other-engine"
		unit.Dependencies = append([]string(nil), unit.Dependencies...)
		newID := strings.Replace(id, "uv:rapid-mlx:", "uv:other-engine:", 1)
		for index := range unit.Dependencies {
			unit.Dependencies[index] = strings.Replace(unit.Dependencies[index], "uv:rapid-mlx:", "uv:other-engine:", 1)
		}
		second[newID] = unit
	}
	all := map[string]softwarelock.Unit{}
	for id, unit := range first {
		all[id] = unit
	}
	for id, unit := range second {
		all[id] = unit
	}
	member, _ := NewInstallationAdapter(&uvMemoryReader{}, &recordingEnvironmentInstaller{})
	installation := installplan.Installation{ID: "experiment", Root: t.TempDir()}
	observed, err := member.Inspect(context.Background(), adapter.InspectRequest{
		Target: software.Target{OS: "darwin", Arch: "arm64"}, Installation: installation, Units: all,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(observed) != len(all) {
		t.Fatalf("Inspect() returned %d units, want %d", len(observed), len(all))
	}
	for id, unit := range observed {
		if unit.Present || unit.InstallLocation == "" {
			t.Fatalf("absent observation %q = %#v", id, unit)
		}
	}
}

type runtimeTarEntry struct {
	name string
	body string
	mode int64
	kind byte
	link string
}

type runtimeArchive struct {
	data    []byte
	locator string
	hash    string
}

func runtimeArchiveFixture(t *testing.T, entries []runtimeTarEntry) runtimeArchive {
	t.Helper()
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		kind := entry.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		header := &tar.Header{Name: entry.name, Mode: entry.mode, Typeflag: kind, Linkname: entry.link}
		if kind == tar.TypeReg {
			header.Size = int64(len(entry.body))
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if kind == tar.TypeReg {
			if _, err := tarWriter.Write([]byte(entry.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	data := output.Bytes()
	hash := sha256.Sum256(data)
	return runtimeArchive{
		data: data, hash: hex.EncodeToString(hash[:]),
		locator: "https://github.com/astral-sh/python-build-standalone/releases/download/20260814/cpython-3.12.14%2B20260814-aarch64-apple-darwin-install_only_stripped.tar.gz",
	}
}

func uvLockedUnits(runtime runtimeArchive, wheelLocator string, wheel []byte) map[string]softwarelock.Unit {
	wheelHash := sha256.Sum256(wheel)
	runtimeID := "uv:rapid-mlx:cpython"
	return map[string]softwarelock.Unit{
		runtimeID: {
			Adapter: adapterID, Scope: "rapid-mlx", NativeName: "cpython", Version: "3.12.14", Revision: "python-build-standalone:20260814",
			Dependencies: []string{}, Artifacts: []software.Artifact{{Locator: runtime.locator, SHA256: runtime.hash, Size: int64(len(runtime.data))}},
		},
		"uv:rapid-mlx:rapid-mlx": {
			Adapter: adapterID, Scope: "rapid-mlx", NativeName: "rapid-mlx", Version: "0.13.3",
			Dependencies: []string{runtimeID}, Artifacts: []software.Artifact{{Locator: wheelLocator, SHA256: hex.EncodeToString(wheelHash[:]), Size: int64(len(wheel))}},
		},
	}
}

func uvInstallGroup(units map[string]softwarelock.Unit, location string, action installplan.Action) installplan.Group {
	ids := []string{"uv:rapid-mlx:cpython", "uv:rapid-mlx:rapid-mlx"}
	planned := make([]installplan.Unit, 0, len(ids))
	for _, id := range ids {
		planned = append(planned, installplan.Unit{ID: id, Action: action, Ownership: installplan.OwnershipTemperAdded, Location: location})
	}
	return installplan.Group{ID: adapterID + ":rapid-mlx", Adapter: adapterID, Scope: "rapid-mlx", EffectModel: installplan.EffectIsolated, Units: planned}
}

func uvRemoveGroup(units map[string]softwarelock.Unit, location string) removeplan.Group {
	ids := []string{"uv:rapid-mlx:rapid-mlx", "uv:rapid-mlx:cpython"}
	planned := make([]removeplan.Unit, 0, len(ids))
	for _, id := range ids {
		planned = append(planned, removeplan.Unit{ID: id, Action: removeplan.ActionRemove, Execute: true, Ownership: installplan.OwnershipTemperAdded, Location: location})
	}
	return removeplan.Group{ID: adapterID + ":rapid-mlx", Adapter: adapterID, Scope: "rapid-mlx", EffectModel: installplan.EffectIsolated, Units: planned}
}

func inspectUVGroup(t *testing.T, member *InstallationAdapter, target software.Target, installation installplan.Installation, units map[string]softwarelock.Unit) map[string]installplan.ObservedUnit {
	t.Helper()
	observed, err := member.Inspect(context.Background(), adapter.InspectRequest{Target: target, Installation: installation, Units: units})
	if err != nil {
		t.Fatal(err)
	}
	return observed
}

type uvMemoryReader struct {
	content map[string][]byte
	opens   int
}

func (r *uvMemoryReader) Open(_ context.Context, locator string) (io.ReadCloser, error) {
	r.opens++
	data, ok := r.content[locator]
	if !ok {
		return nil, errors.New("missing fixture artifact")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

type recordingEnvironmentInstaller struct {
	calls   int
	failure error
	last    EnvironmentInstallRequest
}

func (i *recordingEnvironmentInstaller) Install(_ context.Context, request EnvironmentInstallRequest) error {
	i.calls++
	i.last = request
	if i.failure != nil {
		return i.failure
	}
	if !reflect.DeepEqual(filepath.Dir(request.PythonPath), filepath.Join(request.EnvironmentPath, "bin")) {
		return errors.New("python is outside environment bin")
	}
	return os.WriteFile(filepath.Join(request.EnvironmentPath, "bin", "rapid-mlx"), []byte("#!/bin/sh\n"), 0o755)
}
