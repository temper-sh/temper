package setupcmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"reflect"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
	"github.com/temper-sh/temper/internal/managed"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/setupui"
)

type LayoutEditorOptions struct {
	Root, ID string
	DryRun   bool
}

// NewLayoutEditor reads saved composition and the preset folder. The stored
// catalog resolves file dependencies; discovery never downloads or writes.
func (c Command) NewLayoutEditor(ctx context.Context, options LayoutEditorOptions) (*setupui.LayoutModel, error) {
	home := ""
	if options.Root == "" {
		var err error
		home, err = c.Home()
		if err != nil {
			return nil, err
		}
	}
	root, err := setup.Root(options.Root, home)
	if err != nil {
		return nil, err
	}
	configuration, revision, err := setup.ReadConfiguration(root)
	if err != nil {
		return nil, err
	}
	folder, err := c.readLayoutPresetFolder(ctx, root, configuration)
	if err != nil {
		return nil, err
	}
	available := configuration
	available.Presets = maps.Clone(configuration.Presets)
	for id, file := range folder.files {
		available.Presets[id] = file.preset
	}
	save := func(ctx context.Context, layouts map[string]setup.Layout) (bool, error) {
		candidate := configuration
		candidate.Layouts = layouts
		candidate.Presets = maps.Clone(configuration.Presets)
		for _, layout := range layouts {
			for _, id := range layout.Presets {
				if _, ok := candidate.Presets[id]; ok {
					continue
				}
				if err := folder.check(ctx, id); err != nil {
					return false, err
				}
				candidate.Presets[id] = folder.files[id].preset
			}
		}
		next, changed, err := setup.SaveConfiguration(ctx, root, candidate, revision, options.DryRun)
		published := !options.DryRun && (err == nil || changed)
		if published {
			revision, configuration = next, candidate
		}
		return published, err
	}
	preview := func(ctx context.Context, candidate setup.Configuration, id string) (setup.LayoutPlan, error) {
		if len(candidate.Layouts[id].Presets) == 0 {
			return setup.LayoutPlan{ID: id, Layout: candidate.Layouts[id]}, nil
		}
		if c.Detect == nil {
			return setup.LayoutPlan{}, errors.New("machine facts unavailable")
		}
		facts, err := c.Detect(ctx)
		if err != nil {
			return setup.LayoutPlan{}, err
		}
		return setup.AssessLayout(candidate, id, facts)
	}
	return setupui.NewLayoutModel(ctx, setupui.LayoutInput{Configuration: available, PresetErrors: folder.problems, ID: options.ID, DryRun: options.DryRun}, save, preview)
}

func (c Command) editLayouts(ctx context.Context, args []string, out, diagnostics io.Writer) int {
	options := LayoutEditorOptions{}
	direct := len(args) > 0 && args[0] == "edit"
	if direct {
		args = args[1:]
		if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
			options.ID, args = args[0], args[1:]
		}
	}
	f := flag.NewFlagSet("temper layout", flag.ContinueOnError)
	f.SetOutput(diagnostics)
	f.StringVar(&options.Root, "root", "", "Temper folder (default ~/.temper)")
	f.BoolVar(&options.DryRun, "dry-run", false, "review without saving or deleting layouts")
	f.Usage = func() {
		fmt.Fprintln(diagnostics, "Usage: temper layout [--root PATH] [--dry-run]\n       temper layout edit ID [options]\nCompose layouts from saved presets. Save is explicit; when the editor closes,\nthe running layout restarts with its saved changes.")
		f.PrintDefaults()
	}
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if f.NArg() != 0 || direct && options.ID == "" {
		f.Usage()
		return 2
	}
	inFile, inOK := c.Input.(*os.File)
	outFile, outOK := out.(*os.File)
	if !inOK || !outOK || !term.IsTerminal(inFile.Fd()) || !term.IsTerminal(outFile.Fd()) {
		fmt.Fprintln(diagnostics, "The layout editor needs terminal input and output. For scripts, use temper configure --show --json and temper configure --file FILE --revision REVISION.")
		return 2
	}
	home := ""
	if options.Root == "" {
		var err error
		if home, err = c.Home(); err != nil {
			return c.fail(diagnostics, err)
		}
	}
	root, err := setup.Root(options.Root, home)
	if err != nil {
		return c.fail(diagnostics, err)
	}
	options.Root = root
	before, _, err := setup.ReadConfiguration(root)
	if err != nil {
		return c.fail(diagnostics, err)
	}
	m, err := c.NewLayoutEditor(ctx, options)
	if err != nil {
		return c.fail(diagnostics, err)
	}
	_, err = tea.NewProgram(m, tea.WithInput(c.Input), tea.WithOutput(out), tea.WithContext(ctx)).Run()
	m.Close()
	if err != nil && ctx.Err() == nil {
		return c.fail(diagnostics, err)
	}
	if options.DryRun || ctx.Err() != nil {
		return 0
	}
	if !c.applyEditedLayout(ctx, root, before, out) {
		return 1
	}
	return 0
}

// applyEditedLayout restarts the running layout when the editor saved a change
// to what it serves, so saving is enough to use it. Harness files, such as
// Pi's models.json, follow that restart. A restart waits for active requests.
// A stopped root stays stopped; a deleted running layout keeps serving until
// it is stopped or another layout is activated.
func (c Command) applyEditedLayout(ctx context.Context, root string, before setup.Configuration, out io.Writer) bool {
	state, err := (managed.FileStore{Root: root}).Read()
	job := knownJob(state)
	if err != nil || job == nil {
		return true
	}
	after, _, err := setup.ReadConfiguration(root)
	if err != nil {
		fmt.Fprintf(out, "Saved layouts could not be read to apply them: %v\nApply with: temper layout activate %s\n", err, job.Layout)
		return false
	}
	layout, ok := after.Layouts[job.Layout]
	if !ok || servesSame(before, after, job.Layout) {
		return true
	}
	fmt.Fprintf(out, "Applying saved changes to the running layout %s.\n", layout.Name)
	prior, status, err := c.start(ctx, root, job.Layout, waitNotice(out, false))
	if status != nil {
		c.renderLayoutResult(out, root, "activate", job.Layout, false, prior, *status)
	}
	if err != nil {
		fmt.Fprintf(out, "Layout %s was not restarted: %v\nApply later with: temper layout activate %s\n", layout.Name, err, job.Layout)
		return false
	}
	return true
}

// servesSame reports whether a layout serves the same models with the same
// settings in two configurations. A rename alone does not unload models.
func servesSame(before, after setup.Configuration, id string) bool {
	old, next := before.Layouts[id], after.Layouts[id]
	old.Name, next.Name = "", ""
	if !reflect.DeepEqual(old, next) {
		return false
	}
	for _, preset := range after.Layouts[id].Presets {
		if !reflect.DeepEqual(before.Presets[preset], after.Presets[preset]) {
			return false
		}
	}
	return true
}
