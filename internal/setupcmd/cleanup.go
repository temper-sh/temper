package setupcmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/temper-sh/temper/internal/cleanup"
	"github.com/temper-sh/temper/internal/clihelp"
	"github.com/temper-sh/temper/internal/datadir"
	"github.com/temper-sh/temper/internal/managed"
	"github.com/temper-sh/temper/internal/setup"
)

// Cleanup removes Temper's cached material that no saved preset and no
// running layout uses, and lists Hugging Face models no saved preset uses.
func (c Command) Cleanup(ctx context.Context, args []string, out, diagnostics io.Writer) int {
	c.Name = "cleanup"
	fail := c.fail
	f := flag.NewFlagSet("temper cleanup", flag.ContinueOnError)
	f.SetOutput(diagnostics)
	rootArg := f.String("root", "", "`PATH` Temper folder (default ~/.temper)")
	dry := f.Bool("dry-run", false, "show what would be removed; remove nothing")
	jsonOutput := f.Bool("json", false, "print one JSON document for scripts")
	f.Usage = clihelp.Usage(diagnostics, clihelp.Help{
		Summary: "Remove old engine releases, model links and Splash caches that no saved preset\nand no running layout uses. Hugging Face models are only listed: other tools\nmay use them, so removing them stays your choice.",
		Usage:   []string{"temper cleanup [--dry-run] [--json]"},
	}, f)
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if f.NArg() != 0 {
		return fail(diagnostics, errors.New("unexpected cleanup arguments"))
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
	if err = datadir.Check(root); err != nil {
		return fail(diagnostics, err)
	}
	cache := ""
	if c.CacheRoot != nil {
		if cache, err = c.CacheRoot(); err != nil {
			return fail(diagnostics, err)
		}
	}
	plan := func() (cleanup.Plan, error) {
		configuration, _, err := setup.ReadConfiguration(root)
		if err != nil {
			return cleanup.Plan{}, err
		}
		state, err := managed.FileStore{Root: root}.Read()
		if err != nil {
			return cleanup.Plan{}, err
		}
		return cleanup.Build(root, configuration, state, cache)
	}
	p, err := plan()
	if err != nil {
		return fail(diagnostics, err)
	}
	if !*dry && len(p.Remove) > 0 {
		if c.Remover == nil {
			return fail(diagnostics, errors.New("software removal is unavailable"))
		}
		unlock, err := setup.LockRoot(root)
		if err != nil {
			return fail(diagnostics, err)
		}
		defer unlock()
		// Plan again under the lock: an install or activation may have run.
		if p, err = plan(); err != nil {
			return fail(diagnostics, err)
		}
		if err = cleanup.Apply(ctx, root, p, c.Remover); err != nil {
			return fail(diagnostics, err)
		}
	}
	if *jsonOutput {
		return c.configurationOutput(out, diagnostics, map[string]any{"schema": "temper-cleanup-result/v1", "dry_run": *dry, "removed_bytes": p.Bytes(), "plan": p})
	}
	renderCleanup(out, p, *dry)
	return 0
}

func renderCleanup(out io.Writer, p cleanup.Plan, dry bool) {
	switch {
	case len(p.Remove) == 0:
		fmt.Fprintln(out, "Nothing to remove: every installed file is in use.")
	case dry:
		fmt.Fprintf(out, "Dry run: would remove %s, %s:\n", count(len(p.Remove), "unused item", "unused items"), setup.DownloadSize(p.Bytes()))
	default:
		fmt.Fprintf(out, "Removed %s, %s:\n", count(len(p.Remove), "unused item", "unused items"), setup.DownloadSize(p.Bytes()))
	}
	width := 0
	for _, item := range p.Remove {
		width = max(width, len(item.Label))
	}
	for _, item := range p.Remove {
		fmt.Fprintf(out, "  %-*s  %s\n", width, item.Label, setup.DownloadSize(item.Bytes))
	}
	for _, kept := range p.Kept {
		fmt.Fprintln(out, "Left in place (cannot verify): "+kept)
	}
	if len(p.UnusedRepos) == 0 {
		return
	}
	fmt.Fprintln(out, "Hugging Face models that no saved preset uses (other tools may still use them):")
	width = 0
	for _, r := range p.UnusedRepos {
		width = max(width, len(r.ID))
	}
	for _, r := range p.UnusedRepos {
		fmt.Fprintf(out, "  %-*s  %s\n", width, r.ID, setup.DownloadSize(r.Bytes))
	}
	fmt.Fprintf(out, "Remove one with: hf cache rm model/%s\n", p.UnusedRepos[0].ID)
}
