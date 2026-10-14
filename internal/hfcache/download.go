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

// ErrClientMissing explains the one prerequisite of model downloads: the
// user's own Hugging Face CLI. Temper never installs or provisions it.
var ErrClientMissing = errors.New("model download needs the Hugging Face CLI hf on PATH; install it (for example: brew install hf)")

// FindClient locates the user's hf without running it.
func FindClient(lookPath func(string) (string, error)) (string, error) {
	path, err := lookPath("hf")
	if errors.Is(err, exec.ErrNotFound) {
		return "", ErrClientMissing
	}
	if err != nil {
		return "", fmt.Errorf("find hf: %w", err)
	}
	return path, nil
}

// CheckClient is the read-only preview check for FindClient on PATH.
func CheckClient() error {
	_, err := FindClient(exec.LookPath)
	return err
}

// Download invokes the user's official HF client. Only an explicit
// preparation calls this effect; previews never run a tool.
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
	// The cache lookup supplies the result. Do not parse progress or path
	// output; hf shows its own download progress on the diagnostics stream.
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
	path, err := FindClient(lookPath)
	if err != nil {
		return "", nil, err
	}
	return path, []string{"download", "--revision", e.Revision, "--cache-dir", cacheRoot, "--", e.Repo, e.Name}, nil
}
