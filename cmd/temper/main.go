// Command temper configures presets from the signed catalog, installs their
// exact software and model files, and runs managed layouts. It never infers or
// touches the live legacy service root.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/temper-sh/temper/internal/buildinfo"
	"github.com/temper-sh/temper/internal/catalogcmd"
	"github.com/temper-sh/temper/internal/fetch"
	"github.com/temper-sh/temper/internal/hfcache"
	"github.com/temper-sh/temper/internal/huggingface"
	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/managed"
	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/preset"
	"github.com/temper-sh/temper/internal/presetcmd"
	"github.com/temper-sh/temper/internal/probecmd"
	"github.com/temper-sh/temper/internal/setupcmd"
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/adapter/python"
	"github.com/temper-sh/temper/internal/software/adapter/upstreamrelease"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/softwarecmd"
	"github.com/temper-sh/temper/internal/upstream"
)

// version is replaced only by the release build's validated linker flag.
var version = "0.0.0-dev"

func main() {
	// Catalog and lock readers compare minimum_temper with this version.
	buildinfo.Version = version
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	return runWithDependencies(ctx, arguments, stdout, stderr, dependencies{
		newUpstream: newUpstreamReader,
		cacheRoot:   hfcache.DefaultRoot,
		downloadModel: func(ctx context.Context, e hfcache.Entry, cache string) error {
			return hfcache.Download(ctx, e, cache, stderr)
		},
		detectFacts: machine.DetectFacts,
		newSoftware: newSoftwareCommand,
		serve: func(ctx context.Context, options probecmd.Options, supervision probecmd.Supervision, stdout, stderr io.Writer) error {
			return probecmd.Serve(ctx, probecmd.ProcessRunner{}, options, supervision, stdout, stderr)
		},
		readBinary: readExecutingBinary,
	})
}

type upstreamFactory func() (upstream.Reader, error)
type machineFactsDetector func(context.Context) (machine.Facts, error)
type softwareCommandFactory func() (softwarecmd.Command, error)

type dependencies struct {
	newUpstream   upstreamFactory
	cacheRoot     func() (string, error)
	downloadModel hfcache.DownloadFunc
	detectFacts   machineFactsDetector
	newSoftware   softwareCommandFactory
	serve         func(context.Context, probecmd.Options, probecmd.Supervision, io.Writer, io.Writer) error
	readBinary    func() ([]byte, error)
}

// softwareOperations constructs the software command on each use, so commands that
// never install or audit software need none of its dependencies.
type softwareOperations struct{ factory softwareCommandFactory }

func (s softwareOperations) command() (softwarecmd.Command, error) {
	if s.factory == nil {
		return softwarecmd.Command{}, errors.New("software command dependencies are unavailable")
	}
	return s.factory()
}

func (s softwareOperations) Install(ctx context.Context, root, installation string, lock softwarelock.Document, out io.Writer) error {
	command, err := s.command()
	if err != nil {
		return err
	}
	return command.Install(ctx, root, installation, lock, out)
}

func (s softwareOperations) Check(ctx context.Context, root, installation string, lock softwarelock.Document, out io.Writer) error {
	command, err := s.command()
	if err != nil {
		return err
	}
	return command.Check(ctx, root, installation, lock, out)
}

func (s softwareOperations) Remove(ctx context.Context, root, installation string, lock softwarelock.Document, out io.Writer) error {
	command, err := s.command()
	if err != nil {
		return err
	}
	return command.Remove(ctx, root, installation, lock, out)
}

// fetchLayout binds model fetching to the user's HF client and shared cache.
func (d dependencies) fetchLayout(ctx context.Context, root, layout string, document manifest.Document, locked lockfile.Document) error {
	if d.newUpstream == nil {
		return errors.New("upstream reader is unavailable")
	}
	source, err := d.newUpstream()
	if err != nil {
		return err
	}
	cache := ""
	if d.cacheRoot != nil {
		if cache, err = d.cacheRoot(); err != nil {
			return fmt.Errorf("HF cache: %w", err)
		}
	}
	_, err = fetch.Run(ctx, fetch.Options{Root: root, Layout: layout, Manifest: document, Lock: locked, HFCache: cache, DownloadModel: d.downloadModel}, source)
	return err
}

func (d dependencies) presetEffects() preset.Effects {
	return preset.Effects{Software: softwareOperations{d.newSoftware}, Fetch: d.fetchLayout}
}

func runWithDependencies(ctx context.Context, arguments []string, stdout, stderr io.Writer, deps dependencies) int {
	if len(arguments) == 0 {
		usage(stderr)
		return 2
	}
	switch arguments[0] {
	case "internal-managed-exec":
		if err := managed.ExecLaunch(ctx, arguments[1:]); err != nil {
			fmt.Fprintln(stderr, "managed engine launch:", err)
			return 1
		}
		return 0
	case "version", "--version":
		fmt.Fprintln(stdout, "temper "+version)
		return 0
	case "help", "--help", "-h":
		usage(stdout)
		return 0
	case "init", "configure", "update":
		command := setupcmd.New(deps.detectFacts, deps.presetEffects())
		command.Name, command.CacheRoot = arguments[0], deps.cacheRoot
		if arguments[0] == "update" {
			return command.Update(ctx, arguments[1:], stdout, stderr)
		}
		return command.Configure(ctx, arguments[1:], os.Stdin, stdout, stderr)
	case "catalog", "execution":
		if arguments[0] == "execution" {
			return catalogcmd.Runtime(ctx, arguments[1:], stdout, stderr, catalogcmd.Effects{
				Software: softwareOperations{deps.newSoftware}, Fetch: deps.fetchLayout, Serve: deps.serve,
				Facts: deps.detectFacts, Binary: deps.readBinary,
			})
		}
		return catalogcmd.Run(ctx, arguments, stdout, stderr)
	case "cleanup":
		command := setupcmd.New(deps.detectFacts, deps.presetEffects())
		command.CacheRoot, command.Remover = deps.cacheRoot, softwareOperations{deps.newSoftware}
		return command.Cleanup(ctx, arguments[1:], stdout, stderr)
	case "layout":
		command := setupcmd.New(deps.detectFacts, deps.presetEffects())
		command.Name = "layout"
		return command.Layout(ctx, arguments[1:], stdout, stderr)
	case "machine":
		return runMachine(ctx, arguments[1:], stdout, stderr, deps.detectFacts)
	case "preset":
		hub, err := huggingface.New(huggingface.Config{Token: os.Getenv("HF_TOKEN"), HTTPClient: &http.Client{Timeout: 5 * time.Minute}})
		if err != nil {
			fmt.Fprintf(stderr, "temper preset: %v\n", err)
			return 1
		}
		command := presetcmd.New(deps.detectFacts, hub)
		planner := setupcmd.New(deps.detectFacts, deps.presetEffects())
		planner.CacheRoot = deps.cacheRoot
		command.Plan = planner.Preview
		return command.Run(ctx, arguments[1:], stdout, stderr)
	case "software":
		if deps.newSoftware == nil {
			fmt.Fprintln(stderr, "temper software: command dependencies are unavailable")
			return 1
		}
		command, err := deps.newSoftware()
		if err != nil {
			fmt.Fprintf(stderr, "temper software: construct command: %v\n", err)
			return 1
		}
		return command.Run(ctx, arguments[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "temper: unknown verb %q\n\n", arguments[0])
		usage(stderr)
		return 2
	}
}

func runMachine(ctx context.Context, arguments []string, stdout, stderr io.Writer, detectFacts machineFactsDetector) int {
	if n := len(arguments); n > 0 && (arguments[n-1] == "--help" || arguments[n-1] == "-h" || arguments[0] == "help") {
		machineUsage(stdout)
		return 0
	}
	jsonOutput := len(arguments) == 2 && arguments[1] == "--json"
	if jsonOutput {
		arguments = arguments[:1]
	}
	if len(arguments) != 1 || arguments[0] != "facts" {
		if len(arguments) > 0 {
			fmt.Fprintf(stderr, "temper machine: unknown command %q\n\n", strings.Join(arguments, " "))
		}
		machineUsage(stderr)
		return 2
	}
	if detectFacts == nil {
		fmt.Fprintln(stderr, "temper machine facts: detector is unavailable")
		return 1
	}
	facts, err := detectFacts(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "temper machine facts: %v\n", err)
		return 1
	}
	if err := facts.Validate(); err != nil {
		fmt.Fprintf(stderr, "temper machine facts: %v\n", err)
		return 1
	}
	if jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(facts); err != nil {
			fmt.Fprintf(stderr, "temper machine facts: write output: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(stdout, "%s · %s · %d GiB memory\n", facts.HardwareModel, facts.Chip, facts.PhysicalMemoryBytes>>30)
	fmt.Fprintf(stdout, "macOS %s (build %s)\n", facts.Target.DistributionVersion, facts.OSBuild)
	fmt.Fprintf(stdout, "Metal can use %d MiB (%s); wired memory limit %d MiB (%s)\n", facts.MetalDeviceMemoryMiB, facts.MetalDeviceMemorySource, facts.WiredLimitMiB, facts.WiredLimitSource)
	return 0
}

func newUpstreamReader() (upstream.Reader, error) {
	return huggingface.New(huggingface.Config{Token: os.Getenv("HF_TOKEN")})
}

func newSoftwareCommand() (softwarecmd.Command, error) {
	reader, err := upstreamrelease.NewHTTPReader(&http.Client{})
	if err != nil {
		return softwarecmd.Command{}, err
	}
	releaseMember, err := upstreamrelease.NewInstallationAdapter(reader)
	if err != nil {
		return softwarecmd.Command{}, err
	}
	pythonMember, err := python.NewInstallationAdapter(reader, python.PipInstaller{})
	if err != nil {
		return softwarecmd.Command{}, err
	}
	family, err := adapter.NewInstallationFamily(releaseMember, pythonMember)
	if err != nil {
		return softwarecmd.Command{}, err
	}
	return softwarecmd.New(family, machine.DetectTarget, newSoftwareInvocationID)
}

func newSoftwareInvocationID() (string, error) {
	var identity [16]byte
	if _, err := rand.Read(identity[:]); err != nil {
		return "", err
	}
	return "software-" + hex.EncodeToString(identity[:]), nil
}

func usage(writer io.Writer) {
	fmt.Fprintf(writer, "temper %s — run local AI models on your Mac\n\n", version)
	fmt.Fprintln(writer, `Everyday commands:
  temper init                 choose presets, install them and start the default layout
  temper layout status        show what runs, where, and its model IDs
  temper layout activate ID   start a saved layout, for example after a restart
  temper layout stop          stop the running layout
  temper update               get the newest presets and software, install and restart
  temper configure            change your presets and layouts
  temper preset               create and edit your own presets
  temper cleanup              remove old engine releases and caches nothing uses
  temper version              print the version

Tools for scripts and experiments:
  temper catalog update|inspect|rollback|compile|describe
  temper preset edit|fork|copy|check|fields|weights|template|plan
  temper execution configure|inspect|prepare|render|paths|serve|remove
  temper software install|check|remove
  temper machine facts

Add --help to any command for its options, and --json for one JSON document.
Commands use ~/.temper unless you pass --root PATH.`)
}

func machineUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: temper machine facts [--json]")
}

// readExecutingBinary reads this binary for the Field Kit binding identity.
func readExecutingBinary() ([]byte, error) {
	path, err := os.Executable()
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&fs.ModeSymlink != 0 {
		return nil, errors.New("expected a regular file without symlink indirection")
	}
	return os.ReadFile(path)
}
