package uv

import (
	"archive/tar"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/installplan"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
)

func TestSourceIdentityAndBuildDependenciesAreClosed(t *testing.T) {
	unit := softwarelock.Unit{NativeName: "mlx-lm", Version: "0.32.0", Revision: strings.Repeat("a", 40)}
	artifact := software.Artifact{Locator: "https://codeload.github.com/ml-explore/mlx-lm/tar.gz/" + unit.Revision,
		SHA256: strings.Repeat("b", 64), Size: 100, Format: "tar.gz", ArchiveRoot: "mlx-lm-" + unit.Revision, UnpackedSize: 200, InstalledEntries: 4}
	if err := validateLockedWheel(unit, "3.12.13", artifact); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*software.Artifact){
		func(a *software.Artifact) { a.Locator = "https://codeload.github.com/ml-explore/mlx-lm/tar.gz/main" },
		func(a *software.Artifact) { a.ArchiveRoot = "../../outside" },
		func(a *software.Artifact) { a.Size = 65 << 20 },
		func(a *software.Artifact) { a.UnpackedSize = 257 << 20 },
		func(a *software.Artifact) { a.Locator += "?download=1" },
	} {
		changed := artifact
		change(&changed)
		if err := validateLockedWheel(unit, "3.12.13", changed); err == nil {
			t.Fatal("unbounded or moving source was accepted")
		}
	}
	for _, name := range []string{"vllm-0.30.0+cpu-cp312-cp312-macosx_11_0_arm64.whl", "vllm_metal-0.30.0-cp312-cp312-macosx_15_0_arm64.whl"} {
		pkg, version, repo := "vllm", "0.30.0+cpu", "vllm"
		if strings.HasPrefix(name, "vllm_metal") {
			pkg, version, repo = "vllm-metal", "0.30.0", "vllm-metal"
		}
		wheel := software.Artifact{Locator: "https://github.com/vllm-project/" + repo + "/releases/download/v0.30.0/" + name, SHA256: strings.Repeat("b", 64), Size: 100}
		if err := validateLockedWheel(softwarelock.Unit{NativeName: pkg, Version: version}, "3.12.13", wheel); err != nil {
			t.Fatal(err)
		}
		wheel.Locator = strings.Replace(wheel.Locator, "vllm-project/", "unrelated/", 1)
		if err := validateLockedWheel(softwarelock.Unit{NativeName: pkg, Version: version}, "3.12.13", wheel); err == nil {
			t.Fatal("unapproved release host path admitted")
		}
	}
	if !pythonABICompatible("py37", "none", 3, 12) || pythonABICompatible("py313", "none", 3, 12) {
		t.Fatal("generic Python wheel version floor is wrong")
	}
}

func TestStandalonePythonSymlinkStaysInsideItsGeneration(t *testing.T) {
	runtime := runtimeArchiveFixture(t, []runtimeTarEntry{{name: "python/bin/python3.12", body: "managed-python", mode: 0755},
		{name: "python/bin/python3", link: "python3.12", kind: tar.TypeSymlink}})
	wheel := []byte("wheel")
	locator := "https://files.pythonhosted.org/packages/rapid_mlx-0.13.3-py3-none-any.whl"
	installer := &recordingEnvironmentInstaller{}
	member, _ := NewInstallationAdapter(&uvMemoryReader{content: map[string][]byte{runtime.locator: runtime.data, locator: wheel}}, installer)
	installation := installplan.Installation{ID: "fixture", Root: t.TempDir()}
	units := uvLockedUnits(runtime, locator, wheel)
	location := environmentLocation(installation, "rapid-mlx")
	if err := member.Install(context.Background(), adapter.InstallRequest{Target: software.Target{OS: "darwin", Arch: "arm64"}, Installation: installation,
		Group: uvInstallGroup(units, location, installplan.ActionAdd), Units: units}); err != nil {
		t.Fatal(err)
	}
	if filepath.Base(installer.last.PythonPath) != "python3.12" {
		t.Fatal("interpreter was not resolved inside the generation")
	}
}

func TestPipSourceBuildHasNoIndexOrDependencyResolution(t *testing.T) {
	root := t.TempDir()
	environment := filepath.Join(root, "environment")
	wheelhouse := filepath.Join(root, "wheels")
	source := filepath.Join(root, "source")
	for _, p := range []string{filepath.Join(environment, "bin"), wheelhouse, source} {
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
	}
	python := filepath.Join(environment, "bin", "python3")
	requirements := filepath.Join(root, "requirements.txt")
	os.WriteFile(python, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" >> invocation\n"), 0755)
	os.WriteFile(requirements, []byte("wheel==0.45.1\n"), 0600)
	request := EnvironmentInstallRequest{PythonPath: python, EnvironmentPath: environment, WheelhousePath: wheelhouse, RequirementsPath: requirements, SourceDirectories: []string{source}}
	if err := (PipInstaller{}).Install(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	record, _ := os.ReadFile(filepath.Join(environment, "invocation"))
	text := string(record)
	for _, flag := range []string{"--no-index", "--no-deps", "--no-build-isolation", source, "check"} {
		if !strings.Contains(text, flag) {
			t.Fatalf("missing %s", flag)
		}
	}
	request.SourceDirectories = []string{t.TempDir()}
	if err := (PipInstaller{}).Install(context.Background(), request); err == nil {
		t.Fatal("source outside staging admitted")
	}
}
