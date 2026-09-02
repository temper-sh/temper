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

const maxPipDiagnosticBytes = 64 << 10

// PipInstaller uses pip bundled inside the exact managed-Python archive. It
// consumes only the pre-hashed local wheelhouse and requirements document;
// no ambient uv, pip, Python, index, cache, or user site participates.
type PipInstaller struct{}

func (PipInstaller) Install(ctx context.Context, request EnvironmentInstallRequest) error {
	if err := validateEnvironmentInstallRequest(request); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	command := exec.CommandContext(ctx, request.PythonPath,
		"-m", "pip", "install",
		"--disable-pip-version-check",
		"--no-index",
		"--no-deps",
		"--no-compile",
		"--no-cache-dir",
		"--no-input",
		"--only-binary", ":all:",
		"--require-hashes",
		"--find-links", request.WheelhousePath,
		"--requirement", request.RequirementsPath,
	)
	command.Dir = request.EnvironmentPath
	command.Env = []string{
		"HOME=" + filepath.Join(request.EnvironmentPath, ".temper-home"),
		"LC_ALL=C",
		"NO_COLOR=1",
		"PATH=" + filepath.Join(request.EnvironmentPath, "bin") + ":/usr/bin:/bin:/usr/sbin:/sbin",
		"PIP_CONFIG_FILE=/dev/null",
		"PIP_DISABLE_PIP_VERSION_CHECK=1",
		"PIP_NO_INDEX=1",
		"PYTHONDONTWRITEBYTECODE=1",
		"PYTHONNOUSERSITE=1",
	}
	var diagnostic boundedInstallBuffer
	command.Stdout = &diagnostic
	command.Stderr = &diagnostic
	err := command.Run()
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	if diagnostic.overflow {
		return fmt.Errorf("run managed Python pip: output exceeds %d bytes", maxPipDiagnosticBytes)
	}
	if err == nil {
		return nil
	}
	detail := strings.TrimSpace(diagnostic.String())
	if detail == "" {
		return fmt.Errorf("run managed Python pip: %w", err)
	}
	return fmt.Errorf("run managed Python pip: %w: %s", err, detail)
}

func validateEnvironmentInstallRequest(request EnvironmentInstallRequest) error {
	paths := []struct{ name, value string }{
		{name: "Python path", value: request.PythonPath},
		{name: "environment path", value: request.EnvironmentPath},
		{name: "wheelhouse path", value: request.WheelhousePath},
		{name: "requirements path", value: request.RequirementsPath},
	}
	for _, item := range paths {
		name, value := item.name, item.value
		if value == "" || !filepath.IsAbs(value) || filepath.Clean(value) != value {
			return fmt.Errorf("uv %s must be an absolute clean path", strings.ToLower(name))
		}
	}
	if !strictlyBelowUV(request.EnvironmentPath, request.PythonPath) || !strictlyBelowUV(filepath.Dir(request.EnvironmentPath), request.WheelhousePath) || !strictlyBelowUV(filepath.Dir(request.EnvironmentPath), request.RequirementsPath) {
		return errors.New("uv environment installer paths do not share one staged generation")
	}
	if !regularUVExecutable(request.PythonPath) || !realUVDirectory(request.WheelhousePath) || !regularUVFile(request.RequirementsPath) {
		return errors.New("uv environment installer inputs are absent or have unsafe shape")
	}
	return nil
}

type boundedInstallBuffer struct {
	data     bytes.Buffer
	overflow bool
}

func (b *boundedInstallBuffer) Write(value []byte) (int, error) {
	remaining := maxPipDiagnosticBytes - b.data.Len()
	if remaining > 0 {
		keep := min(remaining, len(value))
		_, _ = b.data.Write(value[:keep])
	}
	if len(value) > remaining {
		b.overflow = true
	}
	return len(value), nil
}

func (b *boundedInstallBuffer) String() string { return b.data.String() }
