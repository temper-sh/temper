// Package upstream defines the narrow read boundary used to resolve and fetch
// immutable artifacts. Concrete network clients live outside this package.
package upstream

import (
	"context"
	"io"
)

type FilePin struct {
	Revision string
	SHA256   string
}

type SnapshotFilePin struct {
	Name   string
	SHA256 string
}

type SnapshotPin struct {
	Revision string
	Files    []SnapshotFilePin
}

type Reader interface {
	Resolve(ctx context.Context, repo, file string) (FilePin, error)
	Open(ctx context.Context, repo, revision, file string) (io.ReadCloser, error)
}

// SnapshotReader is the optional batch boundary for pinning several files at
// one repository revision. Readers which implement it prevent moving-main
// races and may hash small non-LFS files at the selected immutable revision.
type SnapshotReader interface {
	ResolveSnapshot(ctx context.Context, repo string, files []string) (SnapshotPin, error)
}
