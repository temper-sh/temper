package hfcache

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
)

// RemoveFile deletes one explicitly named file from cached revisions. The
// official client owns snapshot/blob reference accounting; no shell is involved.
// The caller protects every currently selected use of this repository/file pair.
func RemoveFile(ctx context.Context, cache string, entry Entry, out io.Writer) error {
	path, args, err := removeCommand(cache, entry, exec.LookPath)
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, path, args...)
	command.Env = append(os.Environ(), "HF_HUB_DISABLE_TELEMETRY=1", "HF_HUB_DISABLE_UPDATE_CHECK=1")
	command.Stdout, command.Stderr = out, out
	if err := runDownload(command); err != nil {
		return fmt.Errorf("remove shared HF file %s/%s: %w", entry.Repo, entry.Name, err)
	}
	return nil
}

func removeCommand(cache string, entry Entry, lookPath func(string) (string, error)) (string, []string, error) {
	if !filepath.IsAbs(cache) {
		return "", nil, fmt.Errorf("HF removal requires an absolute reviewed cache path")
	}
	if err := entry.validate(); err != nil {
		return "", nil, err
	}
	path, err := FindClient(lookPath)
	if err != nil {
		return "", nil, err
	}
	uri := (&url.URL{Scheme: "hf", Host: "models", Path: "/" + entry.Repo + "/" + entry.Name}).String()
	return path, []string{"cache", "rm", uri, "--cache-dir", cache, "--yes"}, nil
}
