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

type Reader interface {
	Resolve(ctx context.Context, repo, file string) (FilePin, error)
	Open(ctx context.Context, repo, revision, file string) (io.ReadCloser, error)
}
