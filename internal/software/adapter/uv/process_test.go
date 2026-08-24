package uv_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software/adapter/uv"
)

func TestProcessRunnerExecutesWithoutShellAndControlsPythonEnvironment(t *testing.T) {
	runner := uv.NewProcessRunner([]string{
		"TEMPER_UV_HELPER=1",
		"TEMPER_PRESERVED=present",
		"HTTPS_PROXY=https://proxy.example",
		"UV_INDEX=https://private.example",
		"PIP_INDEX_URL=https://private.example",
		"PYTHONPATH=/private",
		"VIRTUAL_ENV=/private/venv",
		"CONDA_PREFIX=/private/conda",
		"NO_COLOR=0",
		"FORCE_COLOR=1",
		"LC_ALL=owner-locale",
	})
	output, err := runner.Run(context.Background(), uv.Command{
		Executable: os.Args[0],
		Args:       []string{"-test.run=TestProcessRunnerHelper", "--", "inspect", "two words", "$(not-a-shell)"},
		Stdin:      []byte("requirement from stdin\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`args=["two words" "$(not-a-shell)"]`,
		"stdin=requirement from stdin",
		"TEMPER_PRESERVED=present",
		"HTTPS_PROXY=https://proxy.example",
		"UV_INDEX=",
		"PIP_INDEX_URL=",
		"PYTHONPATH=",
		"VIRTUAL_ENV=",
		"CONDA_PREFIX=",
		"UV_NO_CACHE=1",
		"UV_NO_CONFIG=1",
		"UV_NO_PROGRESS=1",
		"UV_NO_WRAP=1",
		"UV_PYTHON_DOWNLOADS=never",
		"PIP_DISABLE_PIP_VERSION_CHECK=1",
		"NO_COLOR=1",
		"FORCE_COLOR=",
		"LC_ALL=C",
	}
	got := strings.Split(strings.TrimSpace(string(output.Stdout)), "\n")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stdout lines = %#v\nwant %#v", got, want)
	}
	if len(output.Stderr) != 0 {
		t.Fatalf("stderr = %q", output.Stderr)
	}
}

func TestProcessRunnerReturnsCapturedOutputAndDiagnosticOnFailure(t *testing.T) {
	runner := uv.NewProcessRunner([]string{"TEMPER_UV_HELPER=1"})
	output, err := runner.Run(context.Background(), uv.Command{
		Executable: os.Args[0], Args: []string{"-test.run=TestProcessRunnerHelper", "--", "fail"},
	})
	if err == nil || !strings.Contains(err.Error(), "exit status 23") || !strings.Contains(err.Error(), "resolver refused request") {
		t.Fatalf("error = %v", err)
	}
	if string(output.Stdout) != "partial output\n" || string(output.Stderr) != "resolver refused request\n" {
		t.Fatalf("output = %#v", output)
	}
}

func TestProcessRunnerRefusesInvalidExecutionFactsBeforeStarting(t *testing.T) {
	runner := uv.NewProcessRunner([]string{"TEMPER_UV_HELPER=1"})
	if _, err := runner.Run(context.Background(), uv.Command{Executable: "uv"}); err == nil || !strings.Contains(err.Error(), "absolute path") {
		t.Fatalf("relative executable error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runner.Run(ctx, uv.Command{Executable: os.Args[0]}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled error = %v", err)
	}
}

func TestProcessRunnerHelper(t *testing.T) {
	if os.Getenv("TEMPER_UV_HELPER") != "1" {
		return
	}
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		fmt.Fprintln(os.Stderr, "helper command is missing")
		os.Exit(24)
	}
	switch os.Args[separator+1] {
	case "inspect":
		fmt.Printf("args=%q\n", os.Args[separator+2:])
		var input string
		fmt.Fscan(os.Stdin, &input)
		var second, third string
		fmt.Fscan(os.Stdin, &second, &third)
		fmt.Printf("stdin=%s %s %s\n", input, second, third)
		for _, key := range []string{
			"TEMPER_PRESERVED", "HTTPS_PROXY", "UV_INDEX", "PIP_INDEX_URL", "PYTHONPATH",
			"VIRTUAL_ENV", "CONDA_PREFIX", "UV_NO_CACHE", "UV_NO_CONFIG", "UV_NO_PROGRESS",
			"UV_NO_WRAP", "UV_PYTHON_DOWNLOADS", "PIP_DISABLE_PIP_VERSION_CHECK", "NO_COLOR",
			"FORCE_COLOR", "LC_ALL",
		} {
			fmt.Printf("%s=%s\n", key, os.Getenv(key))
		}
	case "fail":
		fmt.Fprintln(os.Stdout, "partial output")
		fmt.Fprintln(os.Stderr, "resolver refused request")
		os.Exit(23)
	default:
		fmt.Fprintln(os.Stderr, "unknown helper command")
		os.Exit(25)
	}
	os.Exit(0)
}
