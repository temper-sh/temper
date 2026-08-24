package homebrew

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

const maxCommandDiagnosticBytes = 8 * 1024

var controlledEnvironment = []string{
	"HOMEBREW_NO_ANALYTICS=1",
	"HOMEBREW_NO_AUTO_UPDATE=1",
	"HOMEBREW_NO_ASK=1",
	"HOMEBREW_NO_COLOR=1",
	"HOMEBREW_NO_EMOJI=1",
	"HOMEBREW_NO_ENV_HINTS=1",
	"HOMEBREW_NO_GITHUB_API=1",
	"LC_ALL=C",
}

var controlledEnvironmentKeys = func() map[string]bool {
	keys := make(map[string]bool, len(controlledEnvironment)+1)
	for _, value := range controlledEnvironment {
		keys[environmentKey(value)] = true
	}
	// This Homebrew variable overrides HOMEBREW_NO_AUTO_UPDATE for API data.
	// It must be absent, rather than replaced with another nonempty value.
	keys["HOMEBREW_FORCE_API_AUTO_UPDATE"] = true
	return keys
}()

// ProcessRunner is the production non-shell process edge for Homebrew reads.
// The composition root supplies its one environment snapshot; this runner
// removes caller overrides that could enable updates or analytics.
type ProcessRunner struct {
	environment []string
}

func NewProcessRunner(environment []string) *ProcessRunner {
	return &ProcessRunner{environment: homebrewEnvironment(environment)}
}

func (r *ProcessRunner) Run(ctx context.Context, command Command) (Output, error) {
	if err := ctx.Err(); err != nil {
		return Output{}, err
	}
	if command.Executable == "" || !filepath.IsAbs(command.Executable) {
		return Output{}, errors.New("Homebrew process executable must be an absolute path")
	}

	process := exec.CommandContext(ctx, command.Executable, command.Args...)
	process.Env = append([]string(nil), r.environment...)
	var stdout, stderr bytes.Buffer
	process.Stdout = &stdout
	process.Stderr = &stderr
	err := process.Run()
	output := Output{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if err == nil {
		return output, nil
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return output, contextErr
	}
	detail := commandDiagnostic(stderr.Bytes())
	if detail == "" {
		return output, fmt.Errorf("run Homebrew command: %w", err)
	}
	return output, fmt.Errorf("run Homebrew command: %w: %s", err, detail)
}

func homebrewEnvironment(environment []string) []string {
	result := make([]string, 0, len(environment)+len(controlledEnvironment))
	for _, value := range environment {
		if !controlledEnvironmentKeys[environmentKey(value)] {
			result = append(result, value)
		}
	}
	return append(result, controlledEnvironment...)
}

func environmentKey(value string) string {
	if index := strings.IndexByte(value, '='); index >= 0 {
		return value[:index]
	}
	return value
}

func commandDiagnostic(data []byte) string {
	if len(data) > maxCommandDiagnosticBytes {
		data = data[:maxCommandDiagnosticBytes]
	}
	return strings.TrimSpace(string(data))
}
