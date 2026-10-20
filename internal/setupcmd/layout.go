package setupcmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/clihelp"
	"github.com/temper-sh/temper/internal/datadir"
	"github.com/temper-sh/temper/internal/harness"
	"github.com/temper-sh/temper/internal/managed"
	"github.com/temper-sh/temper/internal/preset"
	"github.com/temper-sh/temper/internal/probecmd"
	"github.com/temper-sh/temper/internal/setup"
)

type LayoutStatus struct {
	managed.Status
	Integrations []harness.Result `json:"integrations,omitempty"`
}

func (c Command) Layout(ctx context.Context, args []string, out, diagnostics io.Writer) int {
	fail := c.fail
	if len(args) == 0 {
		layoutUsage(diagnostics)
		return 2
	}
	if helpFlag(args[0]) {
		layoutUsage(out)
		return 0
	}
	operation := args[0]
	args = args[1:]
	id := ""
	if operation == "activate" {
		if len(args) > 0 && helpFlag(args[0]) {
			layoutUsage(out)
			return 0
		}
		if len(args) == 0 || strings.HasPrefix(args[0], "-") {
			return fail(diagnostics, errors.New("activate requires a saved layout ID"))
		}
		id = args[0]
		args = args[1:]
	} else if operation != "status" && operation != "stop" {
		fmt.Fprintf(diagnostics, "temper layout: unknown operation %q\n", operation)
		layoutUsage(diagnostics)
		return 2
	}
	f := flag.NewFlagSet("temper layout "+operation, flag.ContinueOnError)
	f.SetOutput(diagnostics)
	rootArg := f.String("root", "", "`PATH` Temper folder (default ~/.temper)")
	listen := f.String("listen", "", "`127.0.0.1:PORT` address to serve on (default: the running layout's, else 127.0.0.1:8080)")
	dry := f.Bool("dry-run", false, "show what would happen; start or stop nothing")
	jsonOutput := f.Bool("json", false, "print one JSON document for scripts")
	f.Usage = clihelp.Usage(diagnostics, layoutHelp(operation), f)
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if f.NArg() != 0 {
		return fail(diagnostics, errors.New("unexpected layout arguments"))
	}
	if *listen != "" {
		host, port, err := net.SplitHostPort(*listen)
		n, e := strconv.Atoi(port)
		if err != nil || e != nil || host != "127.0.0.1" || n < 1024 || n > 65535 {
			return fail(diagnostics, errors.New("listen requires 127.0.0.1 and a port between 1024 and 65535"))
		}
	}
	var err error
	home := ""
	if *rootArg == "" {
		home, err = c.Home()
		if err != nil {
			return fail(diagnostics, err)
		}
	}
	root, err := setup.Root(*rootArg, home)
	if err != nil {
		return fail(diagnostics, err)
	}
	if _, err = setup.ExistingDirectory(root); err != nil {
		return fail(diagnostics, err)
	}
	if err = datadir.Check(root); err != nil {
		return fail(diagnostics, err)
	}
	var before *managed.Job
	var status *LayoutStatus
	switch operation {
	case "activate":
		before, status, err = c.activate(ctx, root, id, *listen, *dry)
	case "status":
		before, status, err = c.layoutStatus(ctx, root)
	default:
		before, status, err = c.stopLayout(ctx, root, *dry)
	}
	if status != nil {
		if *jsonOutput {
			_ = writeJSON(out, status)
		} else {
			c.renderLayoutResult(out, root, operation, id, *dry, before, *status)
		}
	}
	if err != nil {
		return fail(diagnostics, err)
	}
	return 0
}

// renderLayoutResult prints one layout command's outcome in plain words.
func (c Command) renderLayoutResult(out io.Writer, root, operation, id string, dry bool, before *managed.Job, status LayoutStatus) {
	layout := id
	if job := knownJob(status.State); layout == "" && job != nil {
		layout = job.Layout
	}
	saved, defaultPreset := savedLayouts(root, layout)
	renderLayout(out, layoutReport{Operation: operation, Dry: dry, Before: before, Status: status.Status, Default: defaultPreset, Saved: saved})
	for _, result := range status.Integrations {
		fmt.Fprintf(out, "Harness %s: %s · %s\n", strings.Join(result.Harnesses, ", "), result.Status, result.Path)
		if result.Error != "" {
			fmt.Fprintln(out, "  "+result.Error)
		}
		if !dry && (result.Status == "updated" || result.Status == "unchanged") {
			for _, id := range result.Harnesses {
				switch id {
				case "pi":
					fmt.Fprintln(out, "Open /model in Pi to refresh.")
				case "pig":
					fmt.Fprintln(out, "Restart PiG if its picker has not refreshed.")
				case "hermes":
					fmt.Fprintln(out, "Start a new Hermes session and choose temper in its model picker.")
				}
			}
		}
	}
}

// knownJob is the job a root's activation state names: current, else desired.
func knownJob(state managed.State) *managed.Job {
	if state.Current != nil {
		return state.Current
	}
	return state.Desired
}

// controllerFor addresses the router a root runs now, or the job it wants.
func controllerFor(root string, job *managed.Job, state managed.State) managed.Launchd {
	controller := managed.Launchd{Root: root, Label: job.Label, Listen: job.Listen}
	if state.Current != nil {
		controller.Label, controller.Listen = state.Current.Label, state.Current.Listen
	}
	return controller
}

func (c Command) controller(root string, job *managed.Job, state managed.State) managed.Controller {
	if c.Controller != nil {
		return c.Controller(root, job, state)
	}
	return controllerFor(root, job, state)
}

// activate renders one saved layout and reconciles the managed router to it.
// Without listen it keeps the running layout's listener, else 127.0.0.1:8080.
// before is the job known before the change; a refusal before reconciling
// returns no status.
func (c Command) activate(ctx context.Context, root, id, listen string, dry bool) (*managed.Job, *LayoutStatus, error) {
	store := managed.FileStore{Root: root}
	state, err := store.Read()
	if err != nil {
		return nil, nil, err
	}
	before := knownJob(state)
	if listen == "" {
		listen = "127.0.0.1:8080"
		if state.Current != nil {
			listen = state.Current.Listen
		}
	}
	configuration, configurationRevision, err := setup.ReadConfiguration(root)
	if err != nil {
		return before, nil, err
	}
	if _, ok := configuration.Layouts[id]; !ok {
		return before, nil, fmt.Errorf("unknown saved layout %q", id)
	}
	var included []harness.Preset
	for _, id := range configuration.Layouts[id].Presets {
		p := configuration.Presets[id]
		included = append(included, harness.Preset{ID: id, Name: p.Name, Lock: p.Lock})
	}
	models, err := harness.Models(included)
	if err != nil {
		return before, nil, err
	}
	integrations, err := harness.Prepare(root, configuration.Integrations, listen, models)
	if err != nil {
		return before, nil, fmt.Errorf("harness preflight: %w", err)
	}
	facts, err := c.Detect(ctx)
	if err != nil {
		return before, nil, err
	}
	free, err := c.Disk(root)
	if err != nil {
		return before, nil, err
	}
	// Activation assesses only this layout: unrelated installed alternatives
	// do not become a simultaneous memory requirement or activation gate.
	selected := setup.EmptyConfiguration()
	selected.Layouts[id] = configuration.Layouts[id]
	for _, p := range selected.Layouts[id].Presets {
		selected.Presets[p] = configuration.Presets[p]
	}
	plan, err := setup.BuildConfiguration(root, facts, free, selected, setup.Material{})
	if err != nil {
		return before, nil, err
	}
	for _, p := range plan.Presets {
		if len(p.Refusals) > 0 {
			return before, nil, fmt.Errorf("preset %s cannot activate: %v", p.Preset, p.Refusals)
		}
	}
	if len(plan.Layouts[0].Refusals) > 0 {
		return before, nil, fmt.Errorf("layout cannot activate: %v", plan.Layouts[0].Refusals)
	}
	resolve := func(lock catalog.Lock, pkg, relative string) (string, error) {
		return preset.Executable(root, lock, pkg, relative)
	}
	launcher, err := managed.ReadLauncher(root)
	if err != nil {
		return before, nil, err
	}
	rendered, err := managed.Render(root, listen, selected, plan.Layouts[0], resolve, launcher)
	if err != nil {
		return before, nil, fmt.Errorf("prepared inputs unavailable; run configure --resume --prepare: %w", err)
	}
	desired := &rendered.Job
	if !dry {
		for _, p := range selected.Presets {
			if err = preset.Verify(ctx, root, p.Lock, c.Effects.Software); err != nil {
				return before, nil, err
			}
		}
	}
	controller := c.controller(root, desired, state)
	if state.Current != nil && desired.Listen != state.Current.Listen {
		return before, nil, errors.New("stop the current layout before changing its listener")
	}
	if !dry {
		if err = datadir.Ensure(root); err != nil {
			return before, nil, err
		}
		unlock, err := setup.LockRoot(root)
		if err != nil {
			return before, nil, err
		}
		defer unlock()
		if state, err = store.Read(); err != nil {
			return before, nil, err
		}
		if state.Current != nil {
			controller = c.controller(root, desired, state)
			if desired.Listen != state.Current.Listen {
				return before, nil, errors.New("active listener changed during preflight; stop it before changing listeners")
			}
		}
		if _, revision, err := setup.ReadConfiguration(root); err != nil {
			return before, nil, err
		} else if revision != configurationRevision {
			return before, nil, errors.New("configuration changed during activation preflight; review and retry")
		}
		if state.Current == nil {
			// Nothing of this root runs, so a listener on its engine ports
			// belongs to another program or another root.
			if err = probecmd.EnginePortsFree(datadir.StartPort(root), len(desired.Presets)); err != nil {
				return before, nil, err
			}
		}
		if err = managed.Publish(ctx, rendered); err != nil {
			return before, nil, err
		}
		for _, p := range selected.Layouts[id].Presets {
			if err = preset.PrepareState(root, selected.Presets[p].Lock); err != nil {
				return before, nil, err
			}
		}
	}
	result, err := reconcileLayout(ctx, store, controller, desired, dry, integrations)
	if !dry && result.Observed.Ready && result.State.Current != nil && result.State.Current.Generation == desired.Generation {
		// Only this job's launcher copy is in use now, including when a harness
		// synchronization failed after the service became ready.
		_ = managed.PruneLaunchers(root, desired.LauncherSHA256)
	}
	return before, &result, err
}

func reconcileLayout(ctx context.Context, store managed.Store, controller managed.Controller, desired *managed.Job, dry bool, integrations harness.Plan) (LayoutStatus, error) {
	status, err := managed.Reconcile(ctx, store, controller, desired, dry)
	result := LayoutStatus{Status: status, Integrations: integrations.Results()}
	if err == nil && !dry {
		result.Integrations, err = integrations.Publish(ctx)
		if err != nil {
			err = fmt.Errorf("layout is running; harness synchronization failed: %w", err)
		}
	} else if err != nil {
		for i := range result.Integrations {
			result.Integrations[i].Status = "not-applied"
		}
	}
	return result, err
}

func (c Command) layoutStatus(ctx context.Context, root string) (*managed.Job, *LayoutStatus, error) {
	store := managed.FileStore{Root: root}
	state, err := store.Read()
	if err != nil {
		return nil, nil, err
	}
	job := knownJob(state)
	if job == nil {
		return nil, &LayoutStatus{Status: managed.Status{State: state}}, nil
	}
	status, err := managed.Inspect(ctx, store, c.controller(root, job, state))
	return job, &LayoutStatus{Status: status}, err
}

func (c Command) stopLayout(ctx context.Context, root string, dry bool) (*managed.Job, *LayoutStatus, error) {
	store := managed.FileStore{Root: root}
	state, err := store.Read()
	if err != nil {
		return nil, nil, err
	}
	job := knownJob(state)
	if job == nil {
		return nil, &LayoutStatus{Status: managed.Status{State: state}}, nil
	}
	controller := c.controller(root, job, state)
	if !dry {
		if err = datadir.Ensure(root); err != nil {
			return job, nil, err
		}
		unlock, err := setup.LockRoot(root)
		if err != nil {
			return job, nil, err
		}
		defer unlock()
		if state, err = store.Read(); err != nil {
			return job, nil, err
		}
		if state.Current != nil {
			controller = c.controller(root, job, state)
		}
	}
	status, err := managed.Reconcile(ctx, store, controller, nil, dry)
	return job, &LayoutStatus{Status: status}, err
}

func helpFlag(arg string) bool { return arg == "help" || arg == "--help" || arg == "-h" }

func layoutHelp(operation string) clihelp.Help {
	switch operation {
	case "activate":
		return clihelp.Help{Summary: "Start a saved layout, or restart it with your latest changes. It keeps running\nafter the command exits, until you stop it or restart your Mac.",
			Usage: []string{"temper layout activate ID [--listen 127.0.0.1:PORT] [--dry-run] [--json]"}, Examples: []string{"temper layout activate default"}}
	case "status":
		return clihelp.Help{Summary: "Show which layout runs, where, and its model IDs.", Usage: []string{"temper layout status [--json]"}}
	}
	return clihelp.Help{Summary: "Stop the running layout and unload its models.", Usage: []string{"temper layout stop [--dry-run] [--json]"}}
}

func layoutUsage(w io.Writer) {
	fmt.Fprintln(w, `usage:
  temper layout activate ID [--root PATH] [--listen 127.0.0.1:PORT] [--dry-run] [--json]
  temper layout status [--root PATH] [--json]
  temper layout stop [--root PATH] [--dry-run] [--json]`)
}
