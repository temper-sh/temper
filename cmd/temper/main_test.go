package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"

	"github.com/temper-sh/temper/internal/softwarecmd"
)

func TestRunInitHelpDoesNotInspectMachine(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := runWithDependencies(context.Background(), []string{"init", "--help"}, &stdout, &stderr, dependencies{
		detectFacts: func(context.Context) (machine.Facts, error) {
			t.Fatal("help must not inspect the machine")
			return machine.Facts{}, nil
		},
	})
	if exit != 0 || !strings.Contains(stderr.String(), "Usage of temper init:") || !strings.Contains(stderr.String(), "default ~/.temper") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}

func TestEveryHelpRequestSucceedsWithoutEffects(t *testing.T) {
	for _, args := range [][]string{
		{"--help"}, {"configure", "--help"}, {"update", "--help"},
		{"layout", "--help"}, {"layout", "activate", "--help"}, {"layout", "status", "--help"}, {"layout", "stop", "--help"},
		{"catalog", "--help"}, {"catalog", "update", "--help"}, {"catalog", "inspect", "--help"}, {"catalog", "rollback", "--help"},
		{"catalog", "compile", "--help"}, {"catalog", "describe", "--help"},
		{"execution", "--help"}, {"execution", "configure", "--help"}, {"execution", "inspect", "--help"}, {"execution", "serve", "--help"},
		{"machine", "--help"}, {"machine", "facts", "--help"},
	} {
		var stdout, stderr bytes.Buffer
		exit := runWithDependencies(context.Background(), args, &stdout, &stderr, dependencies{
			detectFacts: func(context.Context) (machine.Facts, error) {
				t.Fatal("help must not inspect the machine")
				return machine.Facts{}, nil
			},
		})
		if exit != 0 || stdout.Len()+stderr.Len() == 0 {
			t.Errorf("%v: exit=%d stdout=%q stderr=%q", args, exit, stdout.String(), stderr.String())
		}
	}
	var stdout bytes.Buffer
	runWithDependencies(context.Background(), []string{"help"}, &stdout, io.Discard, dependencies{})
	if everyday, tools := strings.Index(stdout.String(), "temper update"), strings.Index(stdout.String(), "temper execution"); everyday < 0 || tools < everyday {
		t.Fatalf("usage does not list everyday commands first:\n%s", stdout.String())
	}
}

func TestRunInitScriptedPreviewLeavesRootAbsent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "setup")
	var stdout, stderr bytes.Buffer
	exit := runWithDependencies(context.Background(), []string{"init", "--root", root,
		"--catalog", "../../internal/testfixture/testdata/catalog.json", "--preset", "qwen3.5-4b-q4km-off", "--context", "qwen3.5-4b-q4km-off=16384", "--dry-run", "--json"}, &stdout, &stderr, dependencies{
		detectFacts: func(context.Context) (machine.Facts, error) {
			return machine.Facts{
				Schema:        machine.FactsSchemaV1,
				Target:        software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "26.6"},
				HardwareModel: "MacTest,1", Chip: "Apple test chip", OSBuild: "25G1",
				PhysicalMemoryBytes: 16 << 30, MetalDeviceMemoryMiB: 10649,
				MetalDeviceMemorySource: machine.MetalDeviceSourceLive,
				WiredLimitMiB:           10649, WiredLimitSource: budget.WiredSourceMetal,
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

func TestRunMachineFactsPrintsCanonicalReadOnlyDocument(t *testing.T) {
	want := machine.Facts{
		Schema: machine.FactsSchemaV1,
		Target: software.Target{
			OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6",
		},
		HardwareModel: "Mac17,3", Chip: "Apple M5", OSBuild: "24G90",
		PhysicalMemoryBytes:     34359738368,
		MetalDeviceMemoryMiB:    24576,
		MetalDeviceMemorySource: machine.MetalDeviceSourceLive,
		WiredLimitMiB:           24576, WiredLimitSource: budget.WiredSourceMetal,
	}
	var stdout, stderr bytes.Buffer
	exit := runWithDependencies(context.Background(), []string{"machine", "facts", "--json"}, &stdout, &stderr, dependencies{
		detectFacts: func(context.Context) (machine.Facts, error) { return want, nil },
	})
	if exit != 0 || stderr.Len() != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	var got machine.Facts
	err := json.Unmarshal(stdout.Bytes(), &got)
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

func TestRunRefusesRemovedLegacyVerbs(t *testing.T) {
	for _, verb := range []string{"apply", "resolve", "fetch", "check", "field-kit", "probe"} {
		var stdout, stderr bytes.Buffer
		exit := runWithDependencies(context.Background(), []string{verb, "--help"}, &stdout, &stderr, dependencies{})
		if exit != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "unknown verb") {
			t.Fatalf("%s: exit = %d, stdout = %q, stderr = %q", verb, exit, stdout.String(), stderr.String())
		}
	}
}
