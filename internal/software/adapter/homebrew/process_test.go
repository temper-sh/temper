package homebrew_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software/adapter/homebrew"
)

func TestProcessRunnerExecutesArgumentsWithoutAShellAndControlsHomebrewEnvironment(t *testing.T) {
	runner := homebrew.NewProcessRunner([]string{
		"TEMPER_HOMEBREW_HELPER=1",
		"TEMPER_PRESERVED=present",
		"HOMEBREW_NO_ANALYTICS=0",
		"HOMEBREW_NO_AUTO_UPDATE=0",
		"HOMEBREW_FORCE_API_AUTO_UPDATE=1",
		"LC_ALL=owner-locale",
	})

	output, err := runner.Run(context.Background(), homebrew.Command{
		Executable: os.Args[0],
		Args:       []string{"-test.run=TestProcessRunnerHelper", "--", "inspect", "two words", "$(not-a-shell)"},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantLines := []string{
		`args=["two words" "$(not-a-shell)"]`,
		"TEMPER_PRESERVED=present",
		"HOMEBREW_NO_ANALYTICS=1",
		"HOMEBREW_NO_AUTO_UPDATE=1",
		"HOMEBREW_NO_ASK=1",
		"HOMEBREW_NO_COLOR=1",
		"HOMEBREW_NO_EMOJI=1",
		"HOMEBREW_NO_ENV_HINTS=1",
		"HOMEBREW_NO_GITHUB_API=1",
		"HOMEBREW_FORCE_API_AUTO_UPDATE=",
		"LC_ALL=C",
	}
	gotLines := strings.Split(strings.TrimSpace(string(output.Stdout)), "\n")
	if !reflect.DeepEqual(gotLines, wantLines) {
		t.Fatalf("stdout lines = %#v\nwant %#v", gotLines, wantLines)
	}
	if len(output.Stderr) != 0 {
		t.Fatalf("stderr = %q", output.Stderr)
	}
}

func TestProcessRunnerReturnsCapturedOutputAndBoundedDiagnosticOnFailure(t *testing.T) {
	runner := homebrew.NewProcessRunner([]string{"TEMPER_HOMEBREW_HELPER=1"})

	output, err := runner.Run(context.Background(), homebrew.Command{
		Executable: os.Args[0], Args: []string{"-test.run=TestProcessRunnerHelper", "--", "fail"},
	})
	if err == nil || !strings.Contains(err.Error(), "exit status 23") || !strings.Contains(err.Error(), "provider refused request") {
		t.Fatalf("error = %v", err)
	}
	if string(output.Stdout) != "partial output\n" || string(output.Stderr) != "provider refused request\n" {
		t.Fatalf("output = %#v", output)
	}
}

func TestProcessRunnerRefusesInvalidExecutionFactsBeforeStartingAProcess(t *testing.T) {
	runner := homebrew.NewProcessRunner([]string{"TEMPER_HOMEBREW_HELPER=1"})

	_, err := runner.Run(context.Background(), homebrew.Command{Executable: "brew"})
	if err == nil || !strings.Contains(err.Error(), "absolute path") {
		t.Fatalf("error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = runner.Run(ctx, homebrew.Command{Executable: os.Args[0]})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestProcessRunnerHelper(t *testing.T) {
	if os.Getenv("TEMPER_HOMEBREW_HELPER") != "1" {
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
	command := os.Args[separator+1]
	switch command {
	case "inspect":
		fmt.Printf("args=%q\n", os.Args[separator+2:])
		for _, key := range []string{
			"TEMPER_PRESERVED",
			"HOMEBREW_NO_ANALYTICS",
			"HOMEBREW_NO_AUTO_UPDATE",
			"HOMEBREW_NO_ASK",
			"HOMEBREW_NO_COLOR",
			"HOMEBREW_NO_EMOJI",
			"HOMEBREW_NO_ENV_HINTS",
			"HOMEBREW_NO_GITHUB_API",
			"HOMEBREW_FORCE_API_AUTO_UPDATE",
			"LC_ALL",
		} {
			fmt.Printf("%s=%s\n", key, os.Getenv(key))
		}
	case "fail":
		fmt.Fprintln(os.Stdout, "partial output")
		fmt.Fprintln(os.Stderr, "provider refused request")
		os.Exit(23)
	default:
		fmt.Fprintf(os.Stderr, "unknown helper command %q\n", command)
		os.Exit(25)
	}
	os.Exit(0)
}
