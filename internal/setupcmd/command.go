// Package setupcmd orchestrates reads, review, one configuration commit, and
// optional exact-lock preparation. The TUI performs no product effects.
package setupcmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/catalog/distribution"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/software/adapter/upstreamrelease"
	"github.com/temper-sh/temper/internal/software/catalogsource"
	"github.com/temper-sh/temper/internal/software/catalogtrust"
)

type Dispatch func(context.Context, []string, io.Writer, io.Writer) int
type CatalogReader func(context.Context, string, string) (catalog.Document, string, error)
type SoftwareResolver func(context.Context, catalog.Document, string, string) (catalog.Document, error)

type Command struct {
	Detect    func(context.Context) (machine.Facts, error)
	Disk      func(string) (int64, error)
	Home      func() (string, error)
	CacheRoot func() (string, error)
	Catalog   CatalogReader
	Resolve   SoftwareResolver
	Dispatch  Dispatch
}

func New(detect func(context.Context) (machine.Facts, error), dispatch Dispatch) Command {
	return Command{Detect: detect, Disk: setup.FreeDisk, Home: os.UserHomeDir, Catalog: readCatalog, Resolve: resolveSoftware, Dispatch: dispatch}
}

type stringsFlag []string

func (s *stringsFlag) String() string         { return strings.Join(*s, ",") }
func (s *stringsFlag) Set(value string) error { *s = append(*s, value); return nil }

func readCatalog(ctx context.Context, root, path string) (catalog.Document, string, error) {
	if path != "" {
		info, err := os.Lstat(path)
		if err != nil {
			return catalog.Document{}, "", err
		}
		if !info.Mode().IsRegular() || info.Size() > catalogsource.MaxCatalogBytes {
			return catalog.Document{}, "", errors.New("authoring catalog must be a bounded regular file")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return catalog.Document{}, "", err
		}
		d, err := catalog.Parse(raw)
		return d, "", err
	}
	trust, err := catalogtrust.Production()
	if err != nil {
		return catalog.Document{}, "", err
	}
	snapshot, err := distribution.Read(root, trust)
	if errors.Is(err, distribution.ErrNoCatalog) {
		source, sourceErr := catalogsource.NewProductionHTTPS(&http.Client{Timeout: 30 * time.Second})
		if sourceErr != nil {
			return catalog.Document{}, "", sourceErr
		}
		bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		snapshot, err = distribution.Preview(bounded, root, trust, source)
	}
	if err != nil {
		return catalog.Document{}, "", err
	}
	return snapshot.Document, snapshot.SHA256, nil
}

func resolveSoftware(ctx context.Context, d catalog.Document, id string, choice string) (catalog.Document, error) {
	reader, err := upstreamrelease.NewHTTPReader(&http.Client{Timeout: 5 * time.Minute})
	if err != nil {
		return catalog.Document{}, err
	}
	return catalog.ResolveSoftware(ctx, d, id, choice, reader)
}

func fail(out io.Writer, err error) int { fmt.Fprintf(out, "temper init: %v\n", err); return 1 }
func quote(value string) string         { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
