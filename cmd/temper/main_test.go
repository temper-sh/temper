package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/fieldkitcmd"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/probecmd"
	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"

	"github.com/temper-sh/temper/internal/softwarecmd"
	"github.com/temper-sh/temper/internal/testfixture"
	"github.com/temper-sh/temper/internal/upstream"
)

func TestRunInitHelpDoesNotInspectMachine(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := runWithDependencies(context.Background(), []string{"init", "--help"}, &stdout, &stderr, dependencies{
		detectFacts: func(context.Context) (machine.Facts, error) {
			t.Fatal("help must not inspect the machine")
			return machine.Facts{}, nil
		},
	})
	if exit != 0 || !strings.Contains(stderr.String(), "default ~/.temper") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}

func TestRunInitScriptedPreviewLeavesRootAbsent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "setup")
	var stdout, stderr bytes.Buffer
	exit := runWithDependencies(context.Background(), []string{"init", "--root", root,
		"--catalog", "../../catalog/guided-setup.json", "--preset", "qwen3.5-4b-q4km-off", "--context", "qwen3.5-4b-q4km-off=16384", "--dry-run", "--json"}, &stdout, &stderr, dependencies{
		detectFacts: func(context.Context) (machine.Facts, error) {
			return machine.Facts{
				Schema:        machine.FactsSchemaV1,
				Target:        software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "26.6"},
				HardwareModel: "MacTest,1", Chip: "Apple test chip", OSBuild: "25G1",
				PhysicalMemoryBytes: 16 << 30, MetalDeviceMemoryMiB: 13271,
				MetalDeviceMemorySource: machine.MetalDeviceSourcePredicted,
				WiredLimitMiB:           10649, WiredLimitSource: budget.WiredSourcePredicted,
			}, nil
		},
	})
	if exit != 0 || !strings.Contains(stdout.String(), `"schema": "temper-configuration-result/v1"`) || !strings.Contains(stdout.String(), `"dry_run": true`) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry run changed setup root: %v", err)
	}
}

func TestRunDispatchesFieldKitHelpWithoutReadingMachineOrBinary(t *testing.T) {
	called := false
	command, err := fieldkitcmd.New(
		func(context.Context) (machine.Facts, error) {
			called = true
			return machine.Facts{}, errors.New("unexpected facts read")
		},
		func() ([]byte, error) {
			called = true
			return nil, errors.New("unexpected binary read")
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	exit := runWithDependencies(context.Background(), []string{"field-kit", "help"}, &stdout, &stderr, dependencies{
		newFieldKit: func() (fieldkitcmd.Command, error) { return command, nil },
	})
	if exit != 0 || called || stderr.Len() != 0 || !strings.Contains(stdout.String(), "temper field-kit bind") || strings.Contains(stdout.String(), "baseline") {
		t.Fatalf("exit = %d, called = %v, stdout = %q, stderr = %q", exit, called, stdout.String(), stderr.String())
	}
}

type commandRunnerStub struct{}

func (commandRunnerStub) Run(context.Context, probecmd.Invocation, io.Writer, io.Writer) error {
	return errors.New("help must not launch a process")
}

func TestRunDispatchesProbeHelpWithoutLaunchingAProcess(t *testing.T) {
	command, err := probecmd.New(commandRunnerStub{})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	exit := runWithDependencies(context.Background(), []string{"probe", "help"}, &stdout, &stderr, dependencies{
		newProbe: func() (probecmd.Command, error) { return command, nil },
	})
	if exit != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "temper probe serve") {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", exit, stdout.String(), stderr.String())
	}
}

func TestRunMachineFactsPrintsCanonicalReadOnlyDocument(t *testing.T) {
	want := machine.Facts{
		Schema: machine.FactsSchemaV1,
		Target: software.Target{
			OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6",
		},
		HardwareModel: "Mac17,3", Chip: "Apple M5", OSBuild: "24G90",
		PhysicalMemoryBytes:     34359738368,
		MetalDeviceMemoryMiB:    26542,
		MetalDeviceMemorySource: machine.MetalDeviceSourcePredicted,
		WiredLimitMiB:           24576, WiredLimitSource: budget.WiredSourceLive,
	}
	var stdout, stderr bytes.Buffer
	exit := runWithDependencies(context.Background(), []string{"machine", "facts"}, &stdout, &stderr, dependencies{
		detectFacts: func(context.Context) (machine.Facts, error) { return want, nil },
	})
	if exit != 0 || stderr.Len() != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	got, err := machine.ParseFacts(stdout.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("facts = %#v, want %#v", got, want)
	}
}

func TestRunMachineFactsRefusesUnknownSubcommandWithoutDetecting(t *testing.T) {
	called := false
	var stdout, stderr bytes.Buffer
	exit := runWithDependencies(context.Background(), []string{"machine", "guess"}, &stdout, &stderr, dependencies{
		detectFacts: func(context.Context) (machine.Facts, error) {
			called = true
			return machine.Facts{}, nil
		},
	})
	if exit != 2 || called || !strings.Contains(stderr.String(), "usage: temper machine facts") {
		t.Fatalf("exit = %d, called = %v, stderr = %q", exit, called, stderr.String())
	}
}

func TestRunDispatchesTheSoftwareCommand(t *testing.T) {
	family, err := adapter.NewInstallationFamily()
	if err != nil {
		t.Fatal(err)
	}
	command, err := softwarecmd.New(family, func(context.Context) (software.Target, error) {
		return software.Target{}, errors.New("software help must not detect the host")
	}, func() (string, error) {
		return "software-help", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	exit := runWithDependencies(context.Background(), []string{"software", "help"}, &stdout, &stderr, dependencies{
		newSoftware: func() (softwarecmd.Command, error) { return command, nil },
	})
	if exit != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "temper software install") {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", exit, stdout.String(), stderr.String())
	}
}

func TestRunApplyReportsDryRunWithoutWriting(t *testing.T) {
	workspace := t.TempDir()
	manifestPath := filepath.Join(workspace, "manifest.yaml")
	lockPath := filepath.Join(workspace, "manifest.lock.yaml")
	root := filepath.Join(workspace, "root")
	if err := os.WriteFile(manifestPath, []byte(cliManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte(cliLock), 0o644); err != nil {
		t.Fatal(err)
	}
	materializeCLICoder(t, root, manifestPath, lockPath)
	var stdout, stderr bytes.Buffer
	exit := run(context.Background(), []string{
		"apply", "--manifest", manifestPath, "--lock", lockPath,
		"--root", root, "--mode", "local", "--dry-run",
	}, &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %s", exit, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "RESULT apply would-change mode=local generation=") {
		t.Fatalf("unexpected stdout:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "ARTIFACT llama-swap/config.yaml") {
		t.Fatalf("stdout lacks artifact list:\n%s", stdout.String())
	}
	if _, err := os.Lstat(filepath.Join(root, "rendered")); !os.IsNotExist(err) {
		t.Fatalf("dry run created rendered output: %v", err)
	}
}

func materializeCLICoder(t *testing.T, root, manifestPath, lockPath string) {
	t.Helper()
	testfixture.MaterializeLayout(t, root, manifestPath, lockPath, "coder", map[string][]byte{
		"model/coder.gguf": []byte("weights"),
	})
}

func TestRunApplyRequiresExplicitRoot(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := run(context.Background(), []string{"apply", "--dry-run"}, &stdout, &stderr)
	if exit != 2 || !strings.Contains(stderr.String(), "--root is required") {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
}

func TestRunResolveReportsCompleteLockAsUnchanged(t *testing.T) {
	workspace := t.TempDir()
	manifestPath := filepath.Join(workspace, "manifest.yaml")
	lockPath := filepath.Join(workspace, "manifest.lock.yaml")
	if err := os.WriteFile(manifestPath, []byte(cliManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte(cliLock), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	exit := run(context.Background(), []string{"resolve", "--manifest", manifestPath, "--lock", lockPath}, &stdout, &stderr)
	if exit != 0 || stdout.String() != "RESULT resolve unchanged entries=0\n" {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", exit, stdout.String(), stderr.String())
	}
}

type cliUpdateSource struct{}

func (cliUpdateSource) Resolve(context.Context, string, string) (upstream.FilePin, error) {
	return upstream.FilePin{
		Revision: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		SHA256:   "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	}, nil
}

func (cliUpdateSource) Open(context.Context, string, string, string) (io.ReadCloser, error) {
	return nil, errors.New("unexpected patch read")
}

func TestRunUpdateMovesOneRowAndPrintsCommandsWithoutRunningThem(t *testing.T) {
	workspace := t.TempDir()
	manifestPath := filepath.Join(workspace, "manifest.yaml")
	lockPath := filepath.Join(workspace, "manifest.lock.yaml")
	if err := os.WriteFile(manifestPath, []byte(cliManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte(cliLock), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	exit := runWithUpstream(context.Background(), []string{
		"update", "coder", "--manifest", manifestPath, "--lock", lockPath,
	}, &stdout, &stderr, func() (upstream.Reader, error) { return cliUpdateSource{}, nil })
	if exit != 0 || stderr.Len() != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "RESULT update changed targets=1 changed=1\nLOCK coder changed old-revision=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa new-revision=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb ") {
		t.Fatalf("unexpected stdout:\n%s", stdout.String())
	}
	for _, want := range []string{
		"GATE coder plain-completion\nCOMMAND curl ",
		"GATE coder streaming-tool-call\nCOMMAND curl ",
		"delta.tool_calls",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout lacks %q:\n%s", want, stdout.String())
		}
	}
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "revision: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb") || !strings.Contains(string(data), "sha256: cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc") {
		t.Fatalf("lock was not updated:\n%s", data)
	}
}

func TestRunBareUpdatePrintsTheAllLayoutsRiskWarning(t *testing.T) {
	workspace := t.TempDir()
	manifestPath := filepath.Join(workspace, "manifest.yaml")
	lockPath := filepath.Join(workspace, "manifest.lock.yaml")
	if err := os.WriteFile(manifestPath, []byte(cliManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte(cliLock), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	exit := runWithUpstream(context.Background(), []string{
		"update", "--manifest", manifestPath, "--lock", lockPath, "--dry-run",
	}, &stdout, &stderr, func() (upstream.Reader, error) { return cliUpdateSource{}, nil })
	if exit != 0 || stderr.Len() != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	want := "RESULT update would-change targets=1 changed=1\n" +
		"WARNING update-all targets=1 detail=\"re-resolved independent layout pins together\"\n"
	if !strings.HasPrefix(stdout.String(), want) {
		t.Fatalf("unexpected stdout:\n%s", stdout.String())
	}
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != cliLock {
		t.Fatal("bare dry-run changed the lock")
	}
}

func TestRunFetchDryRunReportsOneExplicitLayoutWithoutWriting(t *testing.T) {
	workspace := t.TempDir()
	manifestPath := filepath.Join(workspace, "manifest.yaml")
	lockPath := filepath.Join(workspace, "manifest.lock.yaml")
	root := filepath.Join(workspace, "root")
	if err := os.WriteFile(manifestPath, []byte(cliManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte(cliLock), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	exit := run(context.Background(), []string{
		"fetch", "coder", "--manifest", manifestPath, "--lock", lockPath, "--root", root, "--dry-run",
	}, &stdout, &stderr)
	if exit != 0 || !strings.HasPrefix(stdout.String(), "RESULT fetch would-change layout=coder artifact-set=") {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", exit, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "/model/coder.gguf\n") || !strings.Contains(stdout.String(), "/receipt.json\n") {
		t.Fatalf("stdout lacks materialized files: %s", stdout.String())
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("dry run touched root: %v", err)
	}
}

func TestRunFetchUsesOfficialDownloaderAndSharedCache(t *testing.T) {
	directory := t.TempDir()
	manifest, lock := filepath.Join(directory, "manifest.yaml"), filepath.Join(directory, "manifest.lock.yaml")
	for path, data := range map[string]string{manifest: cliManifest, lock: cliLock} {
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cache := filepath.Join(directory, "hub")
	snapshot := filepath.Join(cache, "models--org--Coder", "snapshots", strings.Repeat("a", 40))
	hf := filepath.Join(directory, "hf")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$TEMPER_TEST_ARGS\"\n/bin/mkdir -p \"$TEMPER_TEST_SNAPSHOT\"\nprintf weights > \"$TEMPER_TEST_SNAPSHOT/coder.gguf\"\n"
	if err := os.WriteFile(hf, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(directory, "hf-args")
	t.Setenv("PATH", directory)
	t.Setenv("HF_HUB_CACHE", cache)
	t.Setenv("TEMPER_TEST_ARGS", argsFile)
	t.Setenv("TEMPER_TEST_SNAPSHOT", snapshot)
	for _, installation := range []string{"first", "second"} {
		var out, diagnostics bytes.Buffer
		root := filepath.Join(directory, installation)
		code := run(context.Background(), []string{"fetch", "coder", "--manifest", manifest, "--lock", lock, "--root", root}, &out, &diagnostics)
		if code != 0 || !strings.Contains(out.String(), "RESULT fetch changed") {
			t.Fatalf("%s fetch: %d %s %s", installation, code, out.String(), diagnostics.String())
		}
		if installation == "first" {
			data, err := os.ReadFile(argsFile)
			if err != nil || string(data) != "download\n--revision\n"+strings.Repeat("a", 40)+"\n--cache-dir\n"+cache+"\n--quiet\n--\norg/Coder\ncoder.gguf\n" {
				t.Fatalf("wrong official downloader invocation: %q %v", data, err)
			}
			if err := os.Remove(hf); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestRunCheckReportsFullVerificationSuccess(t *testing.T) {
	workspace := t.TempDir()
	manifestPath := filepath.Join(workspace, "manifest.yaml")
	lockPath := filepath.Join(workspace, "manifest.lock.yaml")
	root := filepath.Join(workspace, "root")
	if err := os.WriteFile(manifestPath, []byte(cliManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte(cliLock), 0o644); err != nil {
		t.Fatal(err)
	}
	materializeCLICoder(t, root, manifestPath, lockPath)

	var stdout, stderr bytes.Buffer
	exit := runWithTestMachine(context.Background(), []string{
		"check", "--manifest", manifestPath, "--lock", lockPath,
		"--root", root, "--mode", "local", "--verify",
	}, &stdout, &stderr)
	if exit != 0 || stderr.Len() != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "RESULT check ok mode=local verification=sha256 layouts=1 problems=0\n") {
		t.Fatalf("unexpected stdout:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "LAYOUT coder ok artifact-set=") || !strings.Contains(stdout.String(), " files=1\n") {
		t.Fatalf("stdout lacks verified layout:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "BUDGET prediction fits holder=coder physical-mib=32768 device-mib=26542 utilization=0.85 allocation-mib=22560 holder-minimum-mib=1 co-tenants-mib=0 os-floor-mib=1024 required-mib=23584 wired-limit-mib=24576 spare-mib=992 source=live-sysctl\n") {
		t.Fatalf("stdout lacks budget prediction:\n%s", stdout.String())
	}
}

func TestRunCheckReportsFindingsOnStdoutAndExitOne(t *testing.T) {
	workspace := t.TempDir()
	manifestPath := filepath.Join(workspace, "manifest.yaml")
	lockPath := filepath.Join(workspace, "manifest.lock.yaml")
	root := filepath.Join(workspace, "absent-root")
	if err := os.WriteFile(manifestPath, []byte(cliManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte(cliLock), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	exit := runWithTestMachine(context.Background(), []string{
		"check", "--manifest", manifestPath, "--lock", lockPath, "--root", root,
	}, &stdout, &stderr)
	if exit != 1 || stderr.Len() != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "RESULT check failed mode=local verification=receipt layouts=1 problems=1\n") {
		t.Fatalf("unexpected stdout:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "PROBLEM code=artifact-not-materialized layout=coder") {
		t.Fatalf("stdout lacks actionable finding:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "BUDGET prediction unavailable reason=\"GPU-resident artifact \\\"coder\\\" did not pass admission\"\n") {
		t.Fatalf("stdout lacks unavailable budget:\n%s", stdout.String())
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("check touched root: %v", err)
	}
}

func runWithTestMachine(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	return runWithDependencies(ctx, arguments, stdout, stderr, dependencies{
		newUpstream: newUpstreamReader,
		detectMachine: func(context.Context) (budget.Machine, error) {
			return budget.Machine{
				PhysicalMiB: 32768, DeviceMiB: 26542, WiredLimitMiB: 24576,
				WiredSource: budget.WiredSourceLive,
			}, nil
		},
	})
}

const cliManifest = `schema: temper-manifest/v2
defaults: {ttl: 1800, gpu_memory_utilization: 0.85}
layouts:
  coder:
    display_name: Coder
    model: {repo: org/Coder, format: gguf, files: [coder.gguf]}
    engine: llama-server
    interface: chat-completions
    modalities: [text]
    speculation: {method: none}
    window: 8192
    max_tokens: 2048
    thinking: off
    llama: {kv: q8, parallel: 1, flash_attention: on, batch: 512, ubatch: 512}
modes:
  local:
    foreground: coder
    members:
      resident: [{layout: coder}]
`

const cliLock = `schema: temper-lock/v1
entries:
  coder:
    repo: org/Coder
    revision: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    files:
      - {name: coder.gguf, sha256: 9a129038d9a00aed0cf6a7ea059ca50a813449061ab87848cf1a13eafdf33b2c}
    resolved: 2026-08-19
`
