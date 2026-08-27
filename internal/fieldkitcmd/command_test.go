package fieldkitcmd_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/fieldkitcmd"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/software"
)

func TestBindRequiresEveryExplicitInputBeforeReadingTheHost(t *testing.T) {
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
	exit := command.Run(context.Background(), []string{"bind", "--root", "/tmp/explicit"}, &stdout, &stderr)
	if exit != 2 || called || stdout.Len() != 0 || !strings.Contains(stderr.String(), "at least one --installation") {
		t.Fatalf("exit = %d, called = %v, stdout = %q, stderr = %q", exit, called, stdout.String(), stderr.String())
	}
}

func TestBaselineInspectUsesEmbeddedContentAndDetectedFacts(t *testing.T) {
	command, err := fieldkitcmd.New(
		func(context.Context) (machine.Facts, error) {
			return machine.Facts{
				Schema:        machine.FactsSchemaV1,
				Target:        software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "26.0"},
				HardwareModel: "Mac17,3", Chip: "Apple M5", OSBuild: "25A1",
				PhysicalMemoryBytes: 34359738368, MetalDeviceMemoryMiB: 26542,
				MetalDeviceMemorySource: machine.MetalDeviceSourcePredicted,
				WiredLimitMiB:           24576, WiredLimitSource: budget.WiredSourceLive,
			}, nil
		},
		func() ([]byte, error) { return nil, errors.New("binary should not be read") },
	)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	exit := command.Run(context.Background(), []string{"baseline", "inspect"}, &stdout, &stderr)
	if exit != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "BASELINE applicable qwen38-dynamic-q4xl@3") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}

func TestBindRefusesDuplicateInstallationFlags(t *testing.T) {
	command, err := fieldkitcmd.New(
		func(context.Context) (machine.Facts, error) { return machine.Facts{}, nil },
		func() ([]byte, error) { return []byte("temper"), nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	exit := command.Run(context.Background(), []string{
		"bind", "--root", "/tmp/explicit", "--manifest-lock", "manifest.lock.yaml",
		"--generation", strings.Repeat("a", 64),
		"--installation", "base=base.lock.yaml", "--installation", "base=other.lock.yaml",
	}, &stdout, &stderr)
	if exit != 2 || !strings.Contains(stderr.String(), "duplicate installation") {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
}
