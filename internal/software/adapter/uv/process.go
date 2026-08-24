package uv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	maxCommandOutputBytes     = 16 * 1024 * 1024
	maxCommandDiagnosticBytes = 8 * 1024
)

var uvControlledEnvironment = []string{
	"UV_NO_CACHE=1",
	"UV_NO_CONFIG=1",
	"UV_NO_PROGRESS=1",
	"UV_NO_WRAP=1",
	"UV_PYTHON_DOWNLOADS=never",
	"PIP_DISABLE_PIP_VERSION_CHECK=1",
	"NO_COLOR=1",
	"LC_ALL=C",
}

// ProcessRunner is the production non-shell edge for bounded uv metadata
// reads. It removes Python/package-manager overrides from the composition
// root's environment snapshot before adding Temper's fixed read settings.
type ProcessRunner struct {
	environment []string
}

func NewProcessRunner(environment []string) *ProcessRunner {
	return &ProcessRunner{environment: uvEnvironment(environment)}
}

func (r *ProcessRunner) Run(ctx context.Context, command Command) (Output, error) {
	if err := ctx.Err(); err != nil {
		return Output{}, err
	}
	if command.Executable == "" || !filepath.IsAbs(command.Executable) {
		return Output{}, errors.New("uv process executable must be an absolute path")
	}
	process := exec.CommandContext(ctx, command.Executable, command.Args...)
	process.Env = append([]string(nil), r.environment...)
	process.Stdin = bytes.NewReader(command.Stdin)
	stdout := newBoundedBuffer(maxCommandOutputBytes)
	stderr := newBoundedBuffer(maxCommandDiagnosticBytes)
	process.Stdout = stdout
	process.Stderr = stderr
	runErr := process.Run()
	output := Output{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if contextErr := ctx.Err(); contextErr != nil {
		return output, contextErr
	}
	if stdout.overflow {
		return output, fmt.Errorf("run uv command: output exceeds %d bytes", maxCommandOutputBytes)
	}
	if runErr == nil {
		return output, nil
	}
	detail := strings.TrimSpace(string(stderr.Bytes()))
	if detail == "" {
		return output, fmt.Errorf("run uv command: %w", runErr)
	}
	if stderr.overflow {
		detail += " [truncated]"
	}
	return output, fmt.Errorf("run uv command: %w: %s", runErr, detail)
}

func uvEnvironment(environment []string) []string {
	result := make([]string, 0, len(environment)+len(uvControlledEnvironment))
	for _, value := range environment {
		key := environmentKey(value)
		if strings.HasPrefix(key, "UV_") || strings.HasPrefix(key, "PIP_") || strings.HasPrefix(key, "PYTHON") || key == "VIRTUAL_ENV" || key == "CONDA_PREFIX" || key == "NO_COLOR" || key == "FORCE_COLOR" || key == "LC_ALL" {
			continue
		}
		result = append(result, value)
	}
	return append(result, uvControlledEnvironment...)
}

func environmentKey(value string) string {
	if index := strings.IndexByte(value, '='); index >= 0 {
		return value[:index]
	}
	return value
}

type boundedBuffer struct {
	limit    int
	data     []byte
	overflow bool
}

func newBoundedBuffer(limit int) *boundedBuffer {
	return &boundedBuffer{limit: limit, data: make([]byte, 0, min(limit, 4096))}
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	remaining := b.limit - len(b.data)
	if remaining > 0 {
		keep := min(remaining, len(data))
		b.data = append(b.data, data[:keep]...)
	}
	if len(data) > remaining {
		b.overflow = true
	}
	return len(data), nil
}

func (b *boundedBuffer) Bytes() []byte {
	return append([]byte(nil), b.data...)
}
