package setupcmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/managed"
	"github.com/temper-sh/temper/internal/preset"
	"github.com/temper-sh/temper/internal/setup"
)

func (c Command) Layout(ctx context.Context, args []string, out, diagnostics io.Writer) int {
	if len(args) == 0 {
		return fail(diagnostics, errors.New("layout requires activate ID, status or stop"))
	}
	operation := args[0]
	args = args[1:]
	id := ""
	if operation == "activate" {
		if len(args) == 0 {
			return fail(diagnostics, errors.New("activate requires a saved layout ID"))
		}
		id = args[0]
		args = args[1:]
	} else if operation != "status" && operation != "stop" {
		return fail(diagnostics, fmt.Errorf("unknown layout operation %q", operation))
	}
	f := flag.NewFlagSet("temper layout "+operation, flag.ContinueOnError)
	f.SetOutput(diagnostics)
	rootArg := f.String("root", "", "configuration root (default ~/.temper)")
	listen := f.String("listen", "127.0.0.1:8080", "loopback endpoint for an explicitly managed router")
	dry := f.Bool("dry-run", false, "inspect proposed transition without effects")
	_ = f.Bool("json", false, "emit structured status (the default)")
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if f.NArg() != 0 {
		return fail(diagnostics, errors.New("unexpected layout arguments"))
	}
	host, port, err := net.SplitHostPort(*listen)
	n, e := strconv.Atoi(port)
	if err != nil || e != nil || host != "127.0.0.1" || n < 1024 || n > 65535 {
		return fail(diagnostics, errors.New("listen requires 127.0.0.1 and a port between 1024 and 65535"))
	}
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
	store := managed.FileStore{Root: root}
	state, err := store.Read()
	if err != nil {
		return fail(diagnostics, err)
	}
	var rendered managed.Rendered
	var desired *managed.Job
	var configurationRevision string
	selected := setup.EmptyConfiguration()
	if operation == "activate" {
		configuration, revision, err := setup.ReadConfiguration(root)
		if err != nil {
			return fail(diagnostics, err)
		}
		configurationRevision = revision
		if _, ok := configuration.Layouts[id]; !ok {
			return fail(diagnostics, fmt.Errorf("unknown saved layout %q", id))
		}
		facts, err := c.Detect(ctx)
		if err != nil {
			return fail(diagnostics, err)
		}
		free, err := c.Disk(root)
		if err != nil {
			return fail(diagnostics, err)
		}
		// Activation assesses only this layout: unrelated installed alternatives
		// do not become a simultaneous memory requirement or activation gate.
		selected.Layouts[id] = configuration.Layouts[id]
		for _, p := range selected.Layouts[id].Presets {
			selected.Presets[p] = configuration.Presets[p]
		}
		plan, err := setup.BuildConfiguration(root, facts, free, selected, setup.Material{})
		if err != nil {
			return fail(diagnostics, err)
		}
		for _, p := range plan.Modes {
			if len(p.Refusals) > 0 {
				return fail(diagnostics, fmt.Errorf("preset %s cannot activate: %v", p.Profile, p.Refusals))
			}
		}
		if len(plan.Layouts[0].Refusals) > 0 {
			return fail(diagnostics, fmt.Errorf("layout cannot activate: %v", plan.Layouts[0].Refusals))
		}
		resolve := func(lock catalog.Lock, pkg, relative string) (string, error) {
			return preset.Executable(root, lock, pkg, relative)
		}
		launcher, err := managed.ReadLauncher()
		if err != nil {
			return fail(diagnostics, err)
		}
		rendered, err = managed.Render(root, *listen, selected, plan.Layouts[0], resolve, launcher)
		if err != nil {
			return fail(diagnostics, fmt.Errorf("prepared inputs unavailable; run configure --resume --prepare: %w", err))
		}
		desired = &rendered.Job
		if !*dry {
			for _, p := range selected.Presets {
				if err = preset.Verify(ctx, root, p.Lock, preset.Dispatch(c.Dispatch)); err != nil {
					return fail(diagnostics, err)
				}
			}
		}
	}
	job := desired
	if job == nil {
		job = state.Current
		if job == nil {
			job = state.Desired
		}
	}
	if job == nil {
		if operation == "status" || operation == "stop" {
			return configurationOutput(out, diagnostics, managed.Status{State: state})
		}
		return fail(diagnostics, errors.New("no managed job"))
	}
	controller := managed.Launchd{Root: root, Label: job.Label, Listen: job.Listen}
	if state.Current != nil {
		controller.Label = state.Current.Label
		controller.Listen = state.Current.Listen
	}
	if desired != nil && state.Current != nil && desired.Listen != state.Current.Listen {
		return fail(diagnostics, errors.New("stop the current layout before changing its listener"))
	}
	if operation == "status" {
		status, err := managed.Inspect(ctx, store, controller)
		_ = configurationOutput(out, diagnostics, status)
		if err != nil {
			return fail(diagnostics, err)
		}
		return 0
	}
	if !*dry {
		if err = os.MkdirAll(root, 0700); err != nil {
			return fail(diagnostics, err)
		}
		unlock, err := setup.LockRoot(root)
		if err != nil {
			return fail(diagnostics, err)
		}
		defer unlock()
		state, err = store.Read()
		if err != nil {
			return fail(diagnostics, err)
		}
		if state.Current != nil {
			controller.Label, controller.Listen = state.Current.Label, state.Current.Listen
			if desired != nil && desired.Listen != state.Current.Listen {
				return fail(diagnostics, errors.New("active listener changed during preflight; stop it before changing listeners"))
			}
		}
		if desired != nil {
			_, revision, err := setup.ReadConfiguration(root)
			if err != nil {
				return fail(diagnostics, err)
			}
			if revision != configurationRevision {
				return fail(diagnostics, errors.New("configuration changed during activation preflight; review and retry"))
			}
			if err = managed.Publish(ctx, rendered); err != nil {
				return fail(diagnostics, err)
			}
			for _, p := range selected.Layouts[id].Presets {
				if err = preset.PrepareState(root, selected.Presets[p].Lock); err != nil {
					return fail(diagnostics, err)
				}
			}
		}
	}
	status, err := managed.Reconcile(ctx, store, controller, desired, *dry)
	_ = configurationOutput(out, diagnostics, status)
	if err != nil {
		return fail(diagnostics, err)
	}
	return 0
}
