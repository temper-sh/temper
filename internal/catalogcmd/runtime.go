package catalogcmd

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/temper-sh/temper/internal/apply"
	"github.com/temper-sh/temper/internal/artifactset"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/clihelp"
	"github.com/temper-sh/temper/internal/datadir"
	"github.com/temper-sh/temper/internal/fetch"
	"github.com/temper-sh/temper/internal/fieldkitbinding"
	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/probecmd"
	"github.com/temper-sh/temper/internal/render"
	"github.com/temper-sh/temper/internal/render/engine"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/receiptstore"
	"github.com/temper-sh/temper/internal/splash"
)

// Software installs, audits and removes one exact software lock under a
// named installation, writing its report lines to out.
type Software interface {
	Install(ctx context.Context, root, installation string, lock softwarelock.Document, out io.Writer) error
	Check(ctx context.Context, root, installation string, lock softwarelock.Document, out io.Writer) error
	Remove(ctx context.Context, root, installation string, lock softwarelock.Document, out io.Writer) error
}

// Effects are the machine primitives execution operations compose. The
// caller supplies one lock; its projections never leave this process.
type Effects struct {
	Software Software
	Fetch    fetch.Func
	Serve    func(ctx context.Context, options probecmd.Options, supervision probecmd.Supervision, stdout, stderr io.Writer) error
	Facts    func(context.Context) (machine.Facts, error)
	Binary   func() ([]byte, error)
}

func Runtime(ctx context.Context, args []string, stdout, stderr io.Writer, effects Effects) int {
	if len(args) == 0 || isHelp(args[0]) {
		if len(args) == 0 {
			runtimeUsage(stderr)
			return 2
		}
		runtimeUsage(stdout)
		return 0
	}
	operation := args[0]
	if operation == "configure" {
		return configureExecution(ctx, args[1:], stdout, stderr)
	}
	switch operation {
	case "inspect", "prepare", "render", "serve", "remove", "paths":
	default:
		return failed(stderr, fmt.Errorf("unknown execution operation %q", operation))
	}
	f := flag.NewFlagSet("temper execution "+operation, flag.ContinueOnError)
	f.SetOutput(stderr)
	lockPath := f.String("lock", "", "exact execution lock")
	root := f.String("root", "", "explicit Temper root")
	installation := f.String("installation", "", "exact installation ID")
	generation := f.String("generation", "", "rendered generation for serving")
	listen := f.String("listen", "127.0.0.1:8080", "loopback listener")
	statusFile := f.String("status-file", "", "new supervision status path")
	dry := f.Bool("dry-run", false, "validate without effects")
	jsonOutput := f.Bool("json", false, "print one JSON document for scripts")
	f.Usage = clihelp.Usage(stderr, clihelp.Help{Summary: map[string]string{
		"inspect": "Show which preset an execution lock runs.",
		"prepare": "Install the lock's software and model files into an installation.",
		"render":  "Check installed files and write the lock's engine settings.",
		"paths":   "Show the installed model and Python paths of an installation.",
		"serve":   "Serve one prepared installation in the foreground until stopped.",
		"remove":  "Remove an installation's software.",
	}[operation], Usage: []string{"temper execution " + operation + " --lock FILE [options]"}}, f)
	if err := f.Parse(args[1:]); err != nil {
		return parseFailure(err)
	}
	if f.NArg() != 0 || *lockPath == "" {
		return failed(stderr, errors.New("--lock is required"))
	}
	if *root != "" {
		// prepare and remove write through software install and model fetch,
		// which ensure the marker before their first change.
		if err := datadir.Open(*root, !*dry && (operation == "render" || operation == "serve")); err != nil {
			return failed(stderr, err)
		}
	}
	info, err := os.Lstat(*lockPath)
	if err != nil {
		return failed(stderr, err)
	}
	if !info.Mode().IsRegular() || info.Size() > 4<<20 {
		return failed(stderr, errors.New("execution lock must be a regular file no larger than 4 MiB"))
	}
	raw, err := os.ReadFile(*lockPath)
	if err != nil {
		return failed(stderr, err)
	}
	l, err := catalog.ParseLock(raw)
	if err != nil {
		return failed(stderr, err)
	}
	p, err := l.Projections()
	if err != nil {
		return failed(stderr, err)
	}
	identity := map[string]any{"schema": "temper-execution/v1", "preset": l.Preset, "presets": sortedKeys(l.Records.Presets), "request_defaults": p.RequestDefaults,
		"compute": l.Records.Presets[l.Preset].Compute, "compute_options": l.Records.Presets[l.Preset].ComputeOptions}
	if operation == "inspect" {
		if *jsonOutput {
			return encode(stdout, stderr, identity)
		}
		fmt.Fprintf(stdout, "Execution lock for %s (%s)\n", l.Preset, strings.ToUpper(string(l.Records.Presets[l.Preset].Compute)))
		fmt.Fprintf(stdout, "Presets: %s\n", strings.Join(sortedKeys(l.Records.Presets), ", "))
		return 0
	}
	if *root == "" || *installation == "" {
		return failed(stderr, errors.New("--root and --installation are required"))
	}
	resolved, err := datadir.Resolve(*root)
	if err != nil {
		return failed(stderr, err)
	}
	if !regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`).MatchString(*installation) {
		return failed(stderr, errors.New("invalid installation ID"))
	}
	if operation == "serve" && (!regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(*generation) || *statusFile == "") {
		return failed(stderr, errors.New("serve requires --generation and a new --status-file"))
	}
	if operation == "serve" {
		if len(l.Records.Presets) != 1 {
			return failed(stderr, errors.New("supervised serving currently requires exactly one preset"))
		}
		supervision := probecmd.Supervision{StatusPath: *statusFile, Root: resolved, Installation: *installation, Generation: *generation, Listen: *listen}
		if err := supervision.Validate(); err != nil {
			return failed(stderr, err)
		}
		bundle, err := render.Build(render.Inputs{Manifest: p.Manifest, Lock: p.Artifacts, Mode: l.Preset, Root: resolved, StartPort: datadir.StartPort(resolved)})
		if err != nil {
			return failed(stderr, err)
		}
		if bundle.Digest() != *generation {
			return failed(stderr, errors.New("generation differs from the supplied execution lock and root"))
		}
		if !*dry {
			for _, artifact := range bundle.Artifacts {
				path := filepath.Join(resolved, "rendered", "generations", *generation, artifact.Path)
				info, err := os.Lstat(path)
				if err != nil || !info.Mode().IsRegular() {
					return failed(stderr, fmt.Errorf("rendered artifact is absent or unsafe: %s", artifact.Path))
				}
				actual, err := os.ReadFile(path)
				if err != nil {
					return failed(stderr, err)
				}
				if !bytes.Equal(actual, artifact.Data) {
					return failed(stderr, fmt.Errorf("rendered artifact differs from execution lock: %s", artifact.Path))
				}
			}
		}
	}
	if *dry {
		if *jsonOutput {
			return encode(stdout, stderr, map[string]any{"schema": "temper-execution-plan/v1", "operation": operation, "execution": identity, "root": resolved, "installation": *installation, "dry_run": true})
		}
		fmt.Fprintf(stdout, "Dry run: would %s %s in %s (installation %s). Nothing changed.\n", operation, l.Preset, resolved, *installation)
		return 0
	}
	if err := ctx.Err(); err != nil {
		return failed(stderr, err)
	}
	if operation == "prepare" || operation == "serve" {
		for _, layout := range p.Manifest.Layouts {
			if layout.Splash != nil {
				facts, err := effects.Facts(ctx)
				if err != nil {
					return failed(stderr, err)
				}
				if err := engine.SplashCompatibility(facts.Chip, facts.Target.DistributionVersion); err != nil {
					return failed(stderr, err)
				}
				break
			}
		}
	}
	if operation == "paths" {
		models, interpreters := map[string]string{}, map[string]string{}
		for id, layout := range p.Manifest.Layouts {
			set, err := artifactset.New(resolved, id, layout, p.Artifacts.Entries[id], p.Manifest.Patches)
			if err != nil {
				return failed(stderr, err)
			}
			if err := set.Verify(); err != nil {
				return failed(stderr, err)
			}
			models[id] = set.ModelPath()
		}
		for id, selection := range p.Software.Selections {
			if selection.Adapter != "python" {
				continue
			}
			executable, err := probecmd.InstalledExecutable(resolved, *installation, p.Software, id, "bin/python3")
			if err != nil {
				return failed(stderr, err)
			}
			interpreters[id] = executable
		}
		if *jsonOutput {
			return encode(stdout, stderr, map[string]any{"schema": "temper-execution-paths/v1", "execution": identity, "models": models, "python": interpreters})
		}
		for _, id := range sortedKeys(models) {
			fmt.Fprintf(stdout, "Model %s: %s\n", id, models[id])
		}
		for _, id := range sortedKeys(interpreters) {
			fmt.Fprintf(stdout, "Python for %s: %s\n", id, interpreters[id])
		}
		return 0
	}
	if operation == "remove" {
		if err := effects.Software.Remove(ctx, resolved, *installation, p.Software, io.Discard); err != nil {
			return failed(stderr, err)
		}
		if *jsonOutput {
			return encode(stdout, stderr, map[string]any{"schema": "temper-execution-removal/v1", "execution": identity, "root": resolved, "installation": *installation})
		}
		fmt.Fprintf(stdout, "Removed installation %s from %s.\n", *installation, resolved)
		return 0
	}
	if operation == "serve" {
		return serve(ctx, effects, resolved, *installation, *generation, *listen, *statusFile, p, stdout, stderr)
	}
	// step keeps a primitive's report off stdout, which carries only the result.
	step := func(name string, effect func(io.Writer) error) error {
		var report bytes.Buffer
		if err := effect(&report); err != nil {
			_, _ = stderr.Write(report.Bytes())
			return fmt.Errorf("execution %s failed at %s: %w", operation, name, err)
		}
		return nil
	}
	if operation == "prepare" {
		if err := step("software install", func(out io.Writer) error {
			return effects.Software.Install(ctx, resolved, *installation, p.Software, out)
		}); err != nil {
			return failed(stderr, err)
		}
		for _, layout := range sortedKeys(l.Records.Presets) {
			if err := step("fetch "+layout, func(io.Writer) error {
				return effects.Fetch(ctx, resolved, layout, p.Manifest, p.Artifacts)
			}); err != nil {
				return failed(stderr, err)
			}
		}
	}
	if err := step("software check", func(out io.Writer) error {
		return effects.Software.Check(ctx, resolved, *installation, p.Software, out)
	}); err != nil {
		return failed(stderr, err)
	}
	for _, id := range sortedKeys(p.Manifest.Layouts) {
		layout := p.Manifest.Layouts[id]
		if layout.Splash == nil {
			continue
		}
		material, err := splash.New(resolved, id, layout, p.Artifacts.Entries[id], p.Manifest.Patches)
		if err != nil {
			return failed(stderr, err)
		}
		if operation == "prepare" {
			python, err := probecmd.InstalledExecutable(resolved, *installation, p.Software, engine.Splash, engine.SplashPython)
			if err != nil {
				return failed(stderr, err)
			}
			if err := material.Prepare(ctx, python, splash.Derive); err != nil {
				return failed(stderr, err)
			}
		} else if err := material.Verify(); err != nil {
			return failed(stderr, err)
		}
	}
	applied, err := apply.Run(ctx, apply.Options{Manifest: p.Manifest, Lock: p.Artifacts, Root: resolved, Mode: l.Preset})
	if err != nil {
		return failed(stderr, fmt.Errorf("execution %s failed at render: %w", operation, err))
	}
	if err := verifyContent(ctx, resolved, p.Manifest, p.Artifacts); err != nil {
		return failed(stderr, fmt.Errorf("execution %s failed at content check: %w", operation, err))
	}
	binding, err := bind(ctx, effects, resolved, *installation, p.Software, p.Artifacts, applied.Generation)
	if err != nil {
		return failed(stderr, fmt.Errorf("execution %s failed at binding: %w", operation, err))
	}
	if *jsonOutput {
		return encode(stdout, stderr, map[string]any{"schema": "temper-execution-material/v1", "execution": identity, "generation": applied.Generation, "binding": string(binding)})
	}
	verb := map[string]string{"prepare": "Prepared", "render": "Rendered"}[operation]
	fmt.Fprintf(stdout, "%s %s in %s (installation %s, generation %s).\n", verb, l.Preset, resolved, *installation, applied.Generation)
	return 0
}

func serve(ctx context.Context, effects Effects, root, installation, generation, listen, statusFile string, p catalog.Projections, stdout, stderr io.Writer) int {
	for _, id := range sortedKeys(p.Manifest.Layouts) {
		layout := p.Manifest.Layouts[id]
		if layout.Splash == nil {
			continue
		}
		material, err := splash.New(root, id, layout, p.Artifacts.Entries[id], p.Manifest.Patches)
		if err != nil {
			return failed(stderr, err)
		}
		if err := material.Verify(); err != nil {
			return failed(stderr, fmt.Errorf("Splash assembly: %w; run execution prepare", err))
		}
		if err := material.PrepareState(); err != nil {
			return failed(stderr, err)
		}
	}
	options := probecmd.Options{Root: root, Installation: installation, SoftwareLock: p.Software, Generation: generation, Listen: listen}
	supervision := probecmd.Supervision{StatusPath: statusFile, Root: root, Installation: installation, Generation: generation, Listen: listen}
	if err := effects.Serve(ctx, options, supervision, stdout, stderr); err != nil {
		return failed(stderr, err)
	}
	return 0
}

// verifyContent hashes every selected model file and Splash assembly; a
// receipt alone is not enough evidence for a prepared or rendered execution.
func verifyContent(ctx context.Context, root string, document manifest.Document, locked lockfile.Document) error {
	for _, id := range sortedKeys(document.Layouts) {
		layout := document.Layouts[id]
		set, err := artifactset.New(root, id, layout, locked.Entries[id], document.Patches)
		if err != nil {
			return err
		}
		if _, err := set.InspectContent(ctx); err != nil {
			return fmt.Errorf("layout %q: %w", id, err)
		}
		if layout.Splash != nil {
			material, err := splash.New(root, id, layout, locked.Entries[id], document.Patches)
			if err != nil {
				return err
			}
			if err := material.Verify(); err != nil {
				return fmt.Errorf("layout %q Splash assembly: %w", id, err)
			}
		}
	}
	return nil
}

// bind builds the Field Kit binding for one prepared installation and generation.
func bind(ctx context.Context, effects Effects, root, installation string, software softwarelock.Document, artifacts lockfile.Document, generation string) ([]byte, error) {
	binary, err := effects.Binary()
	if err != nil {
		return nil, fmt.Errorf("read executing Temper binary: %w", err)
	}
	facts, err := effects.Facts(ctx)
	if err != nil {
		return nil, fmt.Errorf("detect machine facts: %w", err)
	}
	manifestLock, err := lockfile.Marshal(artifacts)
	if err != nil {
		return nil, err
	}
	receipted, err := receiptstore.Read(root, installation)
	if err != nil {
		return nil, fmt.Errorf("read installation %q receipt: %w", installation, err)
	}
	if !receipted.Exists() {
		return nil, fmt.Errorf("installation %q has no receipt below the explicit root", installation)
	}
	document, err := fieldkitbinding.Build(fieldkitbinding.Inputs{
		TemperBinary: binary, Machine: facts, ManifestLock: manifestLock, RenderedGeneration: generation,
		Installations: []fieldkitbinding.InstallationInput{{Lock: software, ReceiptData: receipted.Data}},
	})
	if err != nil {
		return nil, err
	}
	return fieldkitbinding.Marshal(document)
}
