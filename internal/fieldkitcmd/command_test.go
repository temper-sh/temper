package fieldkitcmd_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/fieldkitcmd"
	"github.com/temper-sh/temper/internal/machine"
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

func TestBaselineCommandIsNotAvailable(t *testing.T) {
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
	exit := command.Run(context.Background(), []string{"baseline", "verify"}, &stdout, &stderr)
	if exit != 2 || called || stdout.Len() != 0 || !strings.Contains(stderr.String(), `unknown command "baseline"`) {
		t.Fatalf("exit=%d called=%v stdout=%q stderr=%q", exit, called, stdout.String(), stderr.String())
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
