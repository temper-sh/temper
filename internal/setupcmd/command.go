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

	"github.com/temper-sh/temper/internal/buildinfo"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/catalog/distribution"
	"github.com/temper-sh/temper/internal/hfcache"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/preset"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/software/adapter/upstreamrelease"
	"github.com/temper-sh/temper/internal/software/catalogsource"
	"github.com/temper-sh/temper/internal/software/catalogtrust"
)

// CatalogReader returns the catalog view setup uses: an explicit file, or the
// stored catalog. With store, a root without one stores the verified catalog
// it downloads; otherwise the download is only previewed.
type CatalogReader func(ctx context.Context, root, path string, store bool) (catalog.View, error)

// SoftwareResolver resolves one preset's software for a choice. Latest falls
// back per item to the catalog's stable release and reports it.
type SoftwareResolver func(context.Context, catalog.Document, string, string) (catalog.Document, []catalog.Resolution, error)

type Command struct {
	// Name is the verb this command serves (init, configure, layout,
	// update); it names flag sets and error messages.
	Name      string
	Detect    func(context.Context) (machine.Facts, error)
	Disk      func(string) (int64, error)
	Home      func() (string, error)
	CacheRoot func() (string, error)
	// Downloader reports whether the user's model download client is
	// available; nil skips the check.
	Downloader func() error
	Catalog    CatalogReader
	// Newest downloads the newest catalog for temper update.
	Newest  NewestCatalog
	Resolve SoftwareResolver
	Effects preset.Effects
}

func New(detect func(context.Context) (machine.Facts, error), effects preset.Effects) Command {
	return Command{Downloader: hfcache.CheckClient, Detect: detect, Disk: setup.FreeDisk, Home: os.UserHomeDir, Catalog: readCatalog, Newest: newestCatalog, Resolve: newSoftwareResolver(), Effects: effects}
}

type stringsFlag []string

func (s *stringsFlag) String() string         { return strings.Join(*s, ",") }
func (s *stringsFlag) Set(value string) error { *s = append(*s, value); return nil }

func readCatalog(ctx context.Context, root, path string, store bool) (catalog.View, error) {
	if path != "" {
		info, err := os.Lstat(path)
		if err != nil {
			return catalog.View{}, err
		}
		if !info.Mode().IsRegular() || info.Size() > catalogsource.MaxCatalogBytes {
			return catalog.View{}, errors.New("authoring catalog must be a bounded regular file")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return catalog.View{}, err
		}
		return catalog.Read(raw, buildinfo.Version)
	}
	trust, err := catalogtrust.Production()
	if err != nil {
		return catalog.View{}, err
	}
	// Temper uses the catalog it last downloaded; only temper update and
	// temper catalog update download a newer one.
	snapshot, err := distribution.Read(root, trust)
	if errors.Is(err, distribution.ErrNoCatalog) {
		return newestCatalog(ctx, root, !store)
	}
	if err != nil {
		return catalog.View{}, err
	}
	return catalog.View{Document: snapshot.Document, Unavailable: snapshot.Unavailable}, nil
}

// newSoftwareResolver looks each upstream source up at most once per command,
// so every preset of one run gets the same router release.
func newSoftwareResolver() SoftwareResolver {
	latest := &catalog.LatestReleases{}
	return func(ctx context.Context, d catalog.Document, id string, choice string) (catalog.Document, []catalog.Resolution, error) {
		if choice == catalog.SoftwareLatest && latest.Reader == nil {
			reader, err := upstreamrelease.NewHTTPReader(&http.Client{Timeout: 5 * time.Minute})
			if err != nil {
				return catalog.Document{}, nil, err
			}
			latest.Reader = reader
		}
		return catalog.ResolveSoftware(ctx, d, id, choice, latest, true)
	}
}

func (c Command) verb() string {
	if c.Name == "" {
		return "configure"
	}
	return c.Name
}

func (c Command) fail(out io.Writer, err error) int {
	fmt.Fprintf(out, "temper %s: %v\n", c.verb(), err)
	return 1
}
