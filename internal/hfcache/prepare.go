package hfcache

import (
	"context"
	"errors"
)

// DownloadFunc delegates cache population to the official HF client.
// It must download only the named file at the exact revision into cacheRoot.
type DownloadFunc func(ctx context.Context, entry Entry, cacheRoot string) error

// Ensure reuses a cache candidate or asks HF to populate its cache. Temper
// never writes HF blobs, snapshots, locks or partials. The caller must verify
// the candidate's bytes before accepting it into an installation.
func (c Cache) Ensure(ctx context.Context, e Entry, download DownloadFunc) (File, error) {
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	if file, ok, err := c.Inspect(e); err != nil || ok {
		return file, err
	}
	if c.Root == "" {
		return File{}, errors.New("HF cache root is required")
	}
	if download == nil {
		return File{}, errors.New("uncached model requires the HF downloader")
	}
	if err := download(ctx, e, c.Root); err != nil {
		return File{}, err
	}
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	file, ok, err := c.Inspect(e)
	if err == nil && !ok {
		err = errors.New("HF download completed without the exact cached file; retry preparation")
	}
	return file, err
}
