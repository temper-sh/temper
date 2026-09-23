package hfcache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Download invokes the official HF client. An existing hf takes precedence;
// otherwise uv provisions huggingface-hub in its own isolated tool cache.
// Only an explicit preparation calls this effect; previews never run a tool.
func Download(ctx context.Context, e Entry, cacheRoot string, diagnostics io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, args, err := downloadCommand(e, cacheRoot, exec.LookPath)
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, path, args...)
	command.Env = append(os.Environ(), "HF_HUB_DISABLE_TELEMETRY=1", "HF_HUB_DISABLE_UPDATE_CHECK=1")
	// The cache lookup supplies the result. Do not parse progress or path output.
	command.Stdout = io.Discard
	command.Stderr = diagnostics
	if err := runDownload(command); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("HF download failed (shared cache retained): %w", err)
	}
	return nil
}

func downloadCommand(e Entry, cacheRoot string, lookPath func(string) (string, error)) (string, []string, error) {
	if err := e.validate(); err != nil {
		return "", nil, err
	}
	if !filepath.IsAbs(cacheRoot) {
		return "", nil, errors.New("HF download cache must be an absolute path")
	}
	args := []string{"download", "--revision", e.Revision, "--cache-dir", cacheRoot, "--quiet", "--", e.Repo, e.Name}
	if path, err := lookPath("hf"); err == nil {
		return path, args, nil
	} else if !errors.Is(err, exec.ErrNotFound) {
		return "", nil, fmt.Errorf("find hf: %w", err)
	}
	path, err := lookPath("uv")
	if err != nil {
		return "", nil, errors.New("model download needs hf or uv on PATH; install uv, then run: uv tool install huggingface-hub")
	}
	return path, append([]string{"tool", "run", "--no-config", "--from", "huggingface-hub", "hf"}, args...), nil
}
