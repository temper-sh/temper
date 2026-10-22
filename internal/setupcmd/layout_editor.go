package setupcmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/setupui"
)

type LayoutEditorOptions struct {
	Root, ID string
	DryRun   bool
}

// NewLayoutEditor reads saved configuration only. Neither catalog state nor
// authoring files can introduce a preset into this composition flow.
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
	save := func(ctx context.Context, layouts map[string]setup.Layout) (bool, error) {
		candidate := configuration
		candidate.Layouts = layouts
		next, changed, err := setup.SaveConfiguration(ctx, root, candidate, revision, options.DryRun)
		published := !options.DryRun && (err == nil || changed)
		if published {
			revision = next
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
	return setupui.NewLayoutModel(ctx, setupui.LayoutInput{Configuration: configuration, ID: options.ID, DryRun: options.DryRun}, save, preview)
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
		fmt.Fprintln(diagnostics, "Usage: temper layout [--root PATH] [--dry-run]\n       temper layout edit ID [options]\nCompose layouts from selected presets. Save is explicit; activation is separate.")
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
	m, err := c.NewLayoutEditor(ctx, options)
	if err != nil {
		return c.fail(diagnostics, err)
	}
	defer m.Close()
	_, err = tea.NewProgram(m, tea.WithInput(c.Input), tea.WithOutput(out), tea.WithContext(ctx)).Run()
	if err != nil && ctx.Err() == nil {
		return c.fail(diagnostics, err)
	}
	return 0
}
