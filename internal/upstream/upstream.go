// Package upstream defines the narrow read boundary used to fetch immutable
// artifacts. Concrete network clients live outside this package.
package upstream

import (
	"context"
	"io"
)

type Reader interface {
	Open(ctx context.Context, repo, revision, file string) (io.ReadCloser, error)
}
