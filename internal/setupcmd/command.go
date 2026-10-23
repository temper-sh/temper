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
	"os/exec"
	"strings"
	"time"

	"github.com/temper-sh/temper/internal/buildinfo"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/catalog/distribution"
	"github.com/temper-sh/temper/internal/cleanup"
	"github.com/temper-sh/temper/internal/harness"
	"github.com/temper-sh/temper/internal/hfcache"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/managed"
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
	Input     io.Reader // Standalone layout editor; New supplies stdin.
	Detect    func(context.Context) (machine.Facts, error)
	Disk      func(string) (int64, error)
	Home      func() (string, error)
	CacheRoot func() (string, error)
	// Downloader reports whether the user's model download client is
	// available; nil skips the check.
	Downloader func() error
	Catalog    CatalogReader
	// StoredCatalog reads only the local catalog; it never downloads or stores one.
	StoredCatalog func(string) (catalog.View, error)
	// Newest downloads the newest catalog for temper update.
	Newest  NewestCatalog
	Resolve SoftwareResolver
	Effects preset.Effects
	// Start activates a saved layout after an install; nil uses the managed
	// router. Tests replace it so they never reach launchd.
	Start      func(ctx context.Context, root, layout string) (*managed.Job, *managed.Status, error)
	Harnesses  func() ([]harness.Option, error)
	Controller func(string, *managed.Job, managed.State) managed.Controller
	// Remover releases software installations for temper cleanup.
	Remover cleanup.Remover
}

func New(detect func(context.Context) (machine.Facts, error), effects preset.Effects) Command {
	return Command{Input: os.Stdin, Downloader: hfcache.CheckClient, Detect: detect, Disk: setup.FreeDisk, Home: os.UserHomeDir, Catalog: readCatalog, StoredCatalog: readStoredCatalog, Newest: newestCatalog, Resolve: newSoftwareResolver(), Effects: effects, Harnesses: func() ([]harness.Option, error) {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		return harness.Discover(home, os.Getenv, exec.LookPath)
	}}
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
	// Temper uses the catalog it last downloaded; only temper update and
	// temper catalog update download a newer one.
	view, err := readStoredCatalog(root)
	if errors.Is(err, distribution.ErrNoCatalog) {
		return newestCatalog(ctx, root, !store)
	}
	return view, err
}

func readStoredCatalog(root string) (catalog.View, error) {
	trust, err := catalogtrust.Production()
	if err != nil {
		return catalog.View{}, err
	}
	snapshot, err := distribution.Read(root, trust)
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

func (c Command) start(ctx context.Context, root, layout string) (*managed.Job, *LayoutStatus, error) {
	if c.Start != nil {
		before, status, err := c.Start(ctx, root, layout)
		if status == nil {
			return before, nil, err
		}
		return before, &LayoutStatus{Status: *status}, err
	}
	return c.activate(ctx, root, layout, "", false)
}

// layoutToStart picks the layout an install starts: the one the root runs
// now or remembers, else "default", else the only saved layout. Empty means none.
func layoutToStart(root string, configuration setup.Configuration) string {
	if state, err := (managed.FileStore{Root: root}).Read(); err == nil {
		if selection := state.Selection(); selection != nil {
			if _, ok := configuration.Layouts[selection.Layout]; ok {
				return selection.Layout
			}
			return ""
		}
		if job := knownJob(state); job != nil {
			if _, ok := configuration.Layouts[job.Layout]; ok {
				return job.Layout
			}
		}
	}
	if l, ok := configuration.Layouts["default"]; ok && len(l.Presets) > 0 {
		return "default"
	}
	if len(configuration.Layouts) == 1 {
		for id, l := range configuration.Layouts {
			if len(l.Presets) > 0 {
				return id
			}
		}
	}
	return ""
}

// startAfterInstall starts the layout to use after preparing material and
// reports it. It returns false when the layout did not start.
func (c Command) startAfterInstall(ctx context.Context, root string, configuration setup.Configuration, jsonOutput bool, out io.Writer, result map[string]any) bool {
	id := layoutToStart(root, configuration)
	if id == "" {
		if ids, _ := savedLayouts(root, ""); len(ids) > 0 && !jsonOutput {
			fmt.Fprintf(out, "Start a layout with: temper layout activate %s\n", strings.Join(ids, " | "))
		}
		return true
	}
	before, status, err := c.start(ctx, root, id)
	started := map[string]any{"id": id}
	if status != nil {
		started["status"] = status
	}
	if err != nil {
		started["error"] = err.Error()
	}
	result["layout"] = started
	if !jsonOutput {
		if status != nil {
			c.renderLayoutResult(out, root, "activate", id, false, before, *status)
		}
		if err != nil {
			fmt.Fprintf(out, "Layout %s activation incomplete: %v\nRetry with: temper layout activate %s\n", id, err, id)
		}
	}
	return err == nil
}

// restartAfterUpdate restarts the running layout so it uses the update. A
// stopped root stays stopped. It returns false when the restart failed.
func (c Command) restartAfterUpdate(ctx context.Context, root string, configuration setup.Configuration, jsonOutput bool, out io.Writer, result map[string]any) bool {
	state, err := (managed.FileStore{Root: root}).Read()
	job := knownJob(state)
	if err != nil || job == nil {
		return true
	}
	if _, ok := configuration.Layouts[job.Layout]; !ok {
		return true
	}
	before, status, err := c.start(ctx, root, job.Layout)
	restarted := map[string]any{"id": job.Layout}
	if status != nil {
		restarted["status"] = status
	}
	if err != nil {
		restarted["error"] = err.Error()
	}
	result["layout"] = restarted
	if !jsonOutput {
		if status != nil {
			c.renderLayoutResult(out, root, "restart", job.Layout, false, before, *status)
		}
		if err != nil {
			fmt.Fprintf(out, "Layout %s update activation incomplete: %v\nRetry with: temper layout activate %s\n", job.Layout, err, job.Layout)
		}
	}
	return err == nil
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
