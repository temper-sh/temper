package setupcmd

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/catalog/distribution"
	"github.com/temper-sh/temper/internal/cleanup"
	"github.com/temper-sh/temper/internal/clihelp"
	"github.com/temper-sh/temper/internal/managed"
	"github.com/temper-sh/temper/internal/preset"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/software/catalogsource"
	"github.com/temper-sh/temper/internal/software/catalogtrust"
)

// NewestCatalog downloads and verifies the newest catalog. Without dry it
// becomes the stored catalog; with dry nothing is stored.
type NewestCatalog func(ctx context.Context, root string, dry bool) (catalog.View, error)

func newestCatalog(ctx context.Context, root string, dry bool) (catalog.View, error) {
	trust, err := catalogtrust.Production()
	if err != nil {
		return catalog.View{}, err
	}
	source, err := catalogsource.NewProductionHTTPS(&http.Client{Timeout: 30 * time.Second})
	if err != nil {
		return catalog.View{}, err
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var snapshot distribution.Snapshot
	if dry {
		snapshot, err = distribution.Preview(bounded, root, trust, source)
	} else if _, err = distribution.Update(bounded, root, false, trust, source); err == nil {
		snapshot, err = distribution.Read(root, trust)
	}
	if err != nil {
		return catalog.View{}, clihelp.Network("download the catalog", err)
	}
	return catalog.View{Document: snapshot.Document, Unavailable: snapshot.Unavailable}, nil
}

// presetUpdate is the outcome for one saved preset.
type presetUpdate struct {
	ID      string   `json:"id"`
	Status  string   `json:"status"` // updated, unchanged or kept
	Changes []string `json:"changes,omitempty"`
	Reason  string   `json:"reason,omitempty"`
	Note    string   `json:"note,omitempty"`
}

// Update downloads the newest catalog and rebuilds every saved preset from its
// origin catalog preset, the user's choices and the software setting. With
// --software it keeps models and settings and changes only router and engine
// releases.
func (c Command) Update(ctx context.Context, args []string, out, diagnostics io.Writer) int {
	c.Name = "update"
	fail := c.fail
	f := flag.NewFlagSet("temper update", flag.ContinueOnError)
	f.SetOutput(diagnostics)
	rootArg := f.String("root", "", "`PATH` Temper folder (default ~/.temper)")
	softwareOnly := f.Bool("software", false, "keep models and settings; change only the router and engine releases")
	dry := f.Bool("dry-run", false, "show old → new; save, install and restart nothing")
	jsonOutput := f.Bool("json", false, "print one JSON document for scripts")
	f.Usage = clihelp.Usage(diagnostics, clihelp.Help{
		Summary:  "Get the newest catalog, rebuild your saved presets from it, install what\nchanged and restart the running layout. Your context and compute choices stay.",
		Usage:    []string{"temper update [--software] [--dry-run] [--json]"},
		Examples: []string{"temper update --dry-run   # only look", "temper update"},
	}, f)
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if f.NArg() != 0 {
		return fail(diagnostics, errors.New("unexpected update arguments"))
	}
	home := ""
	var err error
	if *rootArg == "" {
		if home, err = c.Home(); err != nil {
			return fail(diagnostics, err)
		}
	}
	root, err := setup.Root(*rootArg, home)
	if err != nil {
		return fail(diagnostics, err)
	}
	current, rev, err := setup.ReadConfiguration(root)
	if err != nil {
		return fail(diagnostics, err)
	}
	if rev == "" {
		return fail(diagnostics, errors.New("no saved configuration; choose presets with temper init"))
	}
	view, err := c.Newest(ctx, root, *dry)
	if err != nil {
		return fail(diagnostics, err)
	}
	facts, err := c.Detect(ctx)
	if err != nil {
		return fail(diagnostics, err)
	}
	memoryGiB := int(facts.PhysicalMemoryBytes >> 30)
	resolutions := map[string]catalog.Resolution{}
	build := func(origin string, choices setup.Choices) (catalog.Lock, error) {
		d, used, err := c.Resolve(ctx, view.Document, origin, current.Software)
		if err != nil {
			return catalog.Lock{}, err
		}
		for _, r := range used {
			resolutions[r.Package] = r
		}
		return setup.BuildPreset(d, origin, memoryGiB, choices)
	}
	next := current
	next.Presets = maps.Clone(current.Presets)
	var results []presetUpdate
	for _, id := range slices.Sorted(maps.Keys(current.Presets)) {
		saved := current.Presets[id]
		origin := saved.Lock.Preset
		result := presetUpdate{ID: id}
		var candidate catalog.Lock
		if *softwareOnly {
			candidate, err = c.rebuildSoftware(ctx, saved.Lock, view.Document, current.Software, resolutions)
			if err == nil {
				// Compare with what a full update would build; a difference means
				// this combination is not one the catalog tested.
				if full, fullErr := build(origin, saved.Choices); fullErr == nil && !catalog.SameExecution(full, candidate) {
					result.Note = "not the catalog's tested combination; run temper update for it"
				}
			}
		} else if _, ok := view.Document.Presets[origin]; !ok {
			err = errors.New(unavailableReason(view, origin, "left the catalog"))
		} else {
			candidate, err = build(origin, saved.Choices)
		}
		if err == nil {
			if assessed, assessErr := setup.Assess(candidate, facts); assessErr != nil {
				err = assessErr
			} else if len(assessed.Refusals) > 0 {
				err = fmt.Errorf("does not fit: %s", strings.Join(assessed.Refusals, "; "))
			}
		}
		if err != nil && !*softwareOnly {
			// Keep models and settings; one router per layout still needs the
			// newest software.
			result.Status, result.Reason = "kept", err.Error()
			if software, softwareErr := c.rebuildSoftware(ctx, saved.Lock, view.Document, current.Software, resolutions); softwareErr == nil {
				candidate, err = software, nil
				result.Reason += "; software updated"
			}
		}
		if err != nil {
			result.Status, result.Reason = "kept", err.Error()
			results = append(results, result)
			continue
		}
		result.Changes = setup.Changes(saved.Lock, candidate)
		if result.Status == "" {
			result.Status = "updated"
			if sameLock(saved.Lock, candidate) {
				result.Status = "unchanged"
			} else if len(result.Changes) == 0 {
				result.Changes = []string{"catalog descriptions changed"}
			}
		}
		next.Presets[id] = setup.Preset{Name: saved.Name, Choices: saved.Choices, Lock: candidate}
		results = append(results, result)
	}
	plan, err := c.preview(ctx, root, next)
	if err != nil {
		return fail(diagnostics, err)
	}
	if !*dry && !plan.CanPrepare {
		return fail(diagnostics, fmt.Errorf("cannot install the update: %s", strings.Join(plan.Refusals, "; ")))
	}
	nextRev, changed, err := setup.SaveConfiguration(ctx, root, next, rev, *dry)
	if err != nil {
		return fail(diagnostics, err)
	}
	var prepared []string
	if changed && !*dry {
		progress := diagnostics
		if *jsonOutput {
			progress = io.Discard
		}
		for _, id := range slices.Sorted(maps.Keys(next.Presets)) {
			if err := preset.Prepare(ctx, root, next.Presets[id].Lock, c.Effects, progress); err != nil {
				return fail(diagnostics, fmt.Errorf("configuration saved; preset %s preparation incomplete, resume with temper configure --resume --prepare: %w", id, err))
			}
			prepared = append(prepared, id)
		}
	}
	result := map[string]any{"schema": "temper-update-result/v1", "catalog_date": view.Document.Date,
		"presets": results, "software": sortedResolutions(resolutions), "revision": nextRev, "changed": changed, "dry_run": *dry, "prepared": prepared}
	if *jsonOutput {
		restarted := len(prepared) == 0 || c.restartAfterUpdate(ctx, root, next, true, out, result)
		_ = c.configurationOutput(out, diagnostics, result)
		if !restarted {
			return 1
		}
		return 0
	}
	fmt.Fprintf(out, "Catalog of %s\n", view.Document.Date)
	for _, r := range results {
		status := map[string]string{"updated": "updated", "unchanged": "no change", "kept": "kept as it was"}[r.Status]
		if r.Reason != "" {
			status += " (" + r.Reason + ")"
		}
		fmt.Fprintf(out, "%s: %s\n", r.ID, status)
		for _, change := range r.Changes {
			fmt.Fprintln(out, "  - "+change)
		}
		if r.Note != "" {
			fmt.Fprintln(out, "  "+r.Note)
		}
	}
	printResolutions(out, resolutions)
	switch {
	case *dry:
		fmt.Fprintln(out, "Dry run: nothing was saved, installed or restarted.")
	case len(prepared) > 0:
		fmt.Fprintf(out, "Installed the update for %s.\n", count(len(prepared), "preset", "presets"))
		if !c.restartAfterUpdate(ctx, root, next, false, out, result) {
			return 1
		}
		if state, err := (managed.FileStore{Root: root}).Read(); err == nil {
			if plan, err := cleanup.Build(root, next, state, ""); err == nil && len(plan.Remove) > 0 {
				fmt.Fprintf(out, "Old releases and caches use %s; remove them with: temper cleanup\n", setup.DownloadSize(plan.Bytes()))
			}
		}
	default:
		fmt.Fprintln(out, "Everything is up to date.")
	}
	return 0
}

// rebuildSoftware keeps a saved preset's models and settings and moves its
// router and engine to the newest catalog's releases, or to the newest
// upstream releases for the latest setting, falling back per item.
func (c Command) rebuildSoftware(ctx context.Context, saved catalog.Lock, newest catalog.Document, software string, resolutions map[string]catalog.Resolution) (catalog.Lock, error) {
	d := setup.WithNewestSoftware(saved, newest)
	resolved, used, err := c.Resolve(ctx, d, saved.Preset, software)
	if err != nil {
		return catalog.Lock{}, err
	}
	for _, r := range used {
		resolutions[r.Package] = r
	}
	return catalog.CompilePreset(resolved, saved.Preset, 0, 0, saved.Target)
}

// printChanges shows one saved preset's old → new, as temper update does.
func printChanges(out io.Writer, id string, old, next catalog.Lock) {
	if sameLock(old, next) {
		fmt.Fprintf(out, "%s: no change\n", id)
		return
	}
	fmt.Fprintf(out, "%s: updated\n", id)
	for _, change := range setup.Changes(old, next) {
		fmt.Fprintln(out, "  - "+change)
	}
}

// sameLock compares two locks as they are saved.
func sameLock(a, b catalog.Lock) bool {
	ra, errA := catalog.MarshalLock(a)
	rb, errB := catalog.MarshalLock(b)
	return errA == nil && errB == nil && bytes.Equal(ra, rb)
}

// printResolutions makes every fallback from latest to stable visible.
func printResolutions(out io.Writer, resolutions map[string]catalog.Resolution) {
	for _, r := range sortedResolutions(resolutions) {
		if r.Reason != "" {
			fmt.Fprintf(out, "%s: using the tested release %s; the latest is unavailable (%s)\n", r.Package, r.Used, r.Reason)
		} else {
			fmt.Fprintf(out, "%s: using the latest release %s\n", r.Package, r.Used)
		}
	}
}

func sortedResolutions(resolutions map[string]catalog.Resolution) []catalog.Resolution {
	result := make([]catalog.Resolution, 0, len(resolutions))
	for _, id := range slices.Sorted(maps.Keys(resolutions)) {
		result = append(result, resolutions[id])
	}
	return result
}
