// Command temper resolves immutable artifact identities, manages exact
// software installations, materializes explicit layout sets, and renders
// managed config generations. It never infers or touches the live legacy
// service root.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	applyverb "github.com/temper-sh/temper/internal/apply"
	"github.com/temper-sh/temper/internal/budget"
	checkverb "github.com/temper-sh/temper/internal/check"
	fetchverb "github.com/temper-sh/temper/internal/fetch"
	"github.com/temper-sh/temper/internal/fieldkitcmd"
	"github.com/temper-sh/temper/internal/huggingface"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/probecmd"
	resolveverb "github.com/temper-sh/temper/internal/resolve"
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/adapter/upstreamrelease"
	"github.com/temper-sh/temper/internal/software/catalogsource"
	"github.com/temper-sh/temper/internal/software/catalogtrust"
	"github.com/temper-sh/temper/internal/software/catalogupdate"
	"github.com/temper-sh/temper/internal/softwarecmd"
	updateverb "github.com/temper-sh/temper/internal/update"
	"github.com/temper-sh/temper/internal/upstream"
)

// version is replaced only by the release build's validated linker flag.
var version = "0.0.0-dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	return runWithDependencies(ctx, arguments, stdout, stderr, dependencies{
		newUpstream:   newUpstreamReader,
		detectMachine: machine.Detect,
		detectFacts:   machine.DetectFacts,
		newSoftware:   newSoftwareCommand,
		newProbe: func() (probecmd.Command, error) {
			return probecmd.New(probecmd.ProcessRunner{})
		},
		newFieldKit: func() (fieldkitcmd.Command, error) {
			return fieldkitcmd.New(machine.DetectFacts, fieldkitcmd.ReadExecutingBinary)
		},
	})
}

type upstreamFactory func() (upstream.Reader, error)
type machineDetector func(context.Context) (budget.Machine, error)
type machineFactsDetector func(context.Context) (machine.Facts, error)
type softwareCommandFactory func() (softwarecmd.Command, error)
type fieldKitCommandFactory func() (fieldkitcmd.Command, error)
type probeCommandFactory func() (probecmd.Command, error)

type dependencies struct {
	newUpstream   upstreamFactory
	detectMachine machineDetector
	detectFacts   machineFactsDetector
	newSoftware   softwareCommandFactory
	newFieldKit   fieldKitCommandFactory
	newProbe      probeCommandFactory
}

func runWithUpstream(ctx context.Context, arguments []string, stdout, stderr io.Writer, newSource upstreamFactory) int {
	return runWithDependencies(ctx, arguments, stdout, stderr, dependencies{
		newUpstream:   newSource,
		detectMachine: machine.Detect,
		detectFacts:   machine.DetectFacts,
		newSoftware:   newSoftwareCommand,
		newProbe: func() (probecmd.Command, error) {
			return probecmd.New(probecmd.ProcessRunner{})
		},
		newFieldKit: func() (fieldkitcmd.Command, error) {
			return fieldkitcmd.New(machine.DetectFacts, fieldkitcmd.ReadExecutingBinary)
		},
	})
}

func runWithDependencies(ctx context.Context, arguments []string, stdout, stderr io.Writer, deps dependencies) int {
	if len(arguments) == 0 {
		usage(stderr)
		return 2
	}
	switch arguments[0] {
	case "version", "--version":
		fmt.Fprintln(stdout, "temper "+version)
		return 0
	case "help", "--help", "-h":
		usage(stdout)
		return 0
	case "apply":
		return runApply(ctx, arguments[1:], stdout, stderr)
	case "resolve":
		return runResolve(ctx, arguments[1:], stdout, stderr, deps.newUpstream)
	case "fetch":
		return runFetch(ctx, arguments[1:], stdout, stderr, deps.newUpstream)
	case "check":
		return runCheck(ctx, arguments[1:], stdout, stderr, deps.detectMachine)
	case "update":
		return runUpdate(ctx, arguments[1:], stdout, stderr, deps.newUpstream)
	case "machine":
		return runMachine(ctx, arguments[1:], stdout, stderr, deps.detectFacts)
	case "field-kit":
		if deps.newFieldKit == nil {
			fmt.Fprintln(stderr, "temper field-kit: command dependencies are unavailable")
			return 1
		}
		command, err := deps.newFieldKit()
		if err != nil {
			fmt.Fprintf(stderr, "temper field-kit: construct command: %v\n", err)
			return 1
		}
		return command.Run(ctx, arguments[1:], stdout, stderr)
	case "probe":
		if deps.newProbe == nil {
			fmt.Fprintln(stderr, "temper probe: command dependencies are unavailable")
			return 1
		}
		command, err := deps.newProbe()
		if err != nil {
			fmt.Fprintf(stderr, "temper probe: construct command: %v\n", err)
			return 1
		}
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
	data, err := machine.MarshalFacts(facts)
	if err != nil {
		fmt.Fprintf(stderr, "temper machine facts: %v\n", err)
		return 1
	}
	if _, err := stdout.Write(data); err != nil {
		fmt.Fprintf(stderr, "temper machine facts: write output: %v\n", err)
		return 1
	}
	return 0
}

func runUpdate(ctx context.Context, arguments []string, stdout, stderr io.Writer, newSource upstreamFactory) int {
	layout := ""
	if len(arguments) > 0 && !strings.HasPrefix(arguments[0], "-") {
		layout = arguments[0]
		arguments = arguments[1:]
	}
	flags := flag.NewFlagSet("temper update", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifestPath := flags.String("manifest", "manifest.yaml", "path to the user-owned manifest")
	lockPath := flags.String("lock", "manifest.lock.yaml", "path to the resolution lock")
	dryRun := flags.Bool("dry-run", false, "resolve and report without writing the lock")
	flags.Usage = func() { updateUsage(stderr) }
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "temper update: unexpected arguments: %s\n\n", strings.Join(flags.Args(), " "))
		updateUsage(stderr)
		return 2
	}
	source, err := newSource()
	if err != nil {
		fmt.Fprintf(stderr, "temper update: %v\n", err)
		return 1
	}
	result, err := updateverb.Run(ctx, updateverb.Options{
		ManifestPath: *manifestPath,
		LockPath:     *lockPath,
		Layout:       layout,
		DryRun:       *dryRun,
	}, source)
	if err != nil {
		fmt.Fprintf(stderr, "temper update: %v\n", err)
		return 1
	}
	status := changeStatus(result.Changed, result.DryRun)
	fmt.Fprintf(stdout, "RESULT update %s targets=%d changed=%d\n", status, len(result.Entries), result.ChangeCount())
	if result.All {
		fmt.Fprintf(stdout, "WARNING update-all targets=%d detail=%q\n", len(result.Entries), "re-resolved independent layout pins together")
	}
	for _, entry := range result.Entries {
		entryStatus := "unchanged"
		if entry.Changed {
			entryStatus = "changed"
		}
		fmt.Fprintf(stdout, "LOCK %s %s old-revision=%s new-revision=%s old-artifact-set=%s new-artifact-set=%s\n",
			entry.ID, entryStatus, entry.OldRevision, entry.NewRevision, entry.OldArtifactSet, entry.NewArtifactSet)
	}
	for _, entry := range result.Entries {
		for _, gate := range entry.Gates {
			fmt.Fprintf(stdout, "GATE %s %s\n", entry.ID, gate.Step)
			fmt.Fprintf(stdout, "COMMAND %s\n", gate.Command)
		}
	}
	return 0
}

func runCheck(ctx context.Context, arguments []string, stdout, stderr io.Writer, detectMachine machineDetector) int {
	flags := flag.NewFlagSet("temper check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifestPath := flags.String("manifest", "manifest.yaml", "path to the user-owned manifest")
	lockPath := flags.String("lock", "manifest.lock.yaml", "path to the resolution lock")
	root := flags.String("root", "", "Temper data root (required; never inferred from the live stack)")
	mode := flags.String("mode", "local", "mode whose artifact sets to audit")
	verify := flags.Bool("verify", false, "stream selected artifacts and verify their SHA-256")
	flags.Usage = func() { checkUsage(stderr) }
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *root == "" {
		if flags.NArg() != 0 {
			fmt.Fprintf(stderr, "temper check: unexpected arguments: %s\n\n", strings.Join(flags.Args(), " "))
		} else {
			fmt.Fprintln(stderr, "temper check: --root is required")
		}
		checkUsage(stderr)
		return 2
	}

	machineFacts, err := detectMachine(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "temper check: detect machine: %v\n", err)
		return 1
	}
	result, err := checkverb.Run(ctx, checkverb.Options{
		ManifestPath: *manifestPath,
		LockPath:     *lockPath,
		Root:         *root,
		Mode:         *mode,
		Verify:       *verify,
		Machine:      machineFacts,
	})
	if err != nil {
		fmt.Fprintf(stderr, "temper check: %v\n", err)
		return 1
	}
	status := "ok"
	if !result.OK() {
		status = "failed"
	}
	fmt.Fprintf(stdout, "RESULT check %s mode=%s verification=%s layouts=%d problems=%d\n",
		status, result.Mode, result.Verification, len(result.Layouts), len(result.Findings))
	for _, layout := range result.Layouts {
		layoutStatus := "failed"
		if layout.OK {
			layoutStatus = "ok"
		}
		artifactSet := layout.ArtifactSet
		if artifactSet == "" {
			artifactSet = "none"
		}
		fmt.Fprintf(stdout, "LAYOUT %s %s artifact-set=%s files=%d\n", layout.ID, layoutStatus, artifactSet, layout.Files)
	}
	switch result.Budget.Status {
	case budget.StatusFits, budget.StatusExceeded:
		fmt.Fprintf(stdout, "BUDGET prediction %s holder=%s physical-mib=%d device-mib=%d utilization=%s allocation-mib=%d holder-minimum-mib=%d co-tenants-mib=%d os-floor-mib=%d required-mib=%d wired-limit-mib=%d spare-mib=%d source=%s\n",
			result.Budget.Status, result.Budget.Holder, result.Budget.PhysicalMiB, result.Budget.DeviceMiB,
			strconv.FormatFloat(result.Budget.Utilization, 'f', -1, 64), result.Budget.AllocationMiB, result.Budget.HolderMinimumMiB,
			result.Budget.CoTenantsMiB, result.Budget.OSFloorMiB, result.Budget.RequiredMiB,
			result.Budget.WiredLimitMiB, result.Budget.SpareMiB, result.Budget.WiredSource)
	case budget.StatusUnavailable, budget.StatusNotApplicable:
		fmt.Fprintf(stdout, "BUDGET prediction %s reason=%q\n", result.Budget.Status, result.Budget.Reason)
	}
	for _, finding := range result.Findings {
		fmt.Fprintf(stdout, "PROBLEM code=%s layout=%s detail=%q\n", finding.Code, finding.Layout, finding.Detail)
	}
	if !result.OK() {
		return 1
	}
	return 0
}

func runResolve(ctx context.Context, arguments []string, stdout, stderr io.Writer, newSource upstreamFactory) int {
	flags := flag.NewFlagSet("temper resolve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifestPath := flags.String("manifest", "manifest.yaml", "path to the user-owned manifest")
	lockPath := flags.String("lock", "manifest.lock.yaml", "path to the resolution lock")
	dryRun := flags.Bool("dry-run", false, "resolve and report without writing the lock")
	flags.Usage = func() { resolveUsage(stderr) }
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "temper resolve: unexpected arguments: %s\n\n", strings.Join(flags.Args(), " "))
		resolveUsage(stderr)
		return 2
	}
	source, err := newSource()
	if err != nil {
		fmt.Fprintf(stderr, "temper resolve: %v\n", err)
		return 1
	}
	result, err := resolveverb.Run(ctx, resolveverb.Options{
		ManifestPath: *manifestPath,
		LockPath:     *lockPath,
		DryRun:       *dryRun,
	}, source)
	if err != nil {
		fmt.Fprintf(stderr, "temper resolve: %v\n", err)
		return 1
	}
	status := changeStatus(result.Changed, result.DryRun)
	fmt.Fprintf(stdout, "RESULT resolve %s entries=%d\n", status, len(result.Entries))
	for _, entry := range result.Entries {
		fmt.Fprintf(stdout, "LOCK %s revision=%s\n", entry.ID, entry.Revision)
	}
	return 0
}

func runFetch(ctx context.Context, arguments []string, stdout, stderr io.Writer, newSource upstreamFactory) int {
	if len(arguments) == 0 || strings.HasPrefix(arguments[0], "-") {
		fmt.Fprintln(stderr, "temper fetch: layout id is required")
		fetchUsage(stderr)
		return 2
	}
	layout := arguments[0]
	flags := flag.NewFlagSet("temper fetch", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifestPath := flags.String("manifest", "manifest.yaml", "path to the user-owned manifest")
	lockPath := flags.String("lock", "manifest.lock.yaml", "path to the resolution lock")
	root := flags.String("root", "", "Temper data root (required; never inferred from the live stack)")
	dryRun := flags.Bool("dry-run", false, "check local presence without downloading or writing")
	flags.Usage = func() { fetchUsage(stderr) }
	if err := flags.Parse(arguments[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *root == "" {
		if flags.NArg() != 0 {
			fmt.Fprintf(stderr, "temper fetch: unexpected arguments: %s\n\n", strings.Join(flags.Args(), " "))
		} else {
			fmt.Fprintln(stderr, "temper fetch: --root is required")
		}
		fetchUsage(stderr)
		return 2
	}
	source, err := newSource()
	if err != nil {
		fmt.Fprintf(stderr, "temper fetch: %v\n", err)
		return 1
	}
	result, err := fetchverb.Run(ctx, fetchverb.Options{
		ManifestPath: *manifestPath,
		LockPath:     *lockPath,
		Root:         *root,
		Layout:       layout,
		DryRun:       *dryRun,
	}, source)
	if err != nil {
		fmt.Fprintf(stderr, "temper fetch: %v\n", err)
		return 1
	}
	status := changeStatus(result.Changed, result.DryRun)
	fmt.Fprintf(stdout, "RESULT fetch %s layout=%s artifact-set=%s\n", status, result.Layout, result.ArtifactSet)
	for _, file := range result.Files {
		fmt.Fprintf(stdout, "FILE %s\n", file)
	}
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
	member, err := upstreamrelease.NewInstallationAdapter(reader)
	if err != nil {
		return softwarecmd.Command{}, err
	}
	family, err := adapter.NewInstallationFamily(member)
	if err != nil {
		return softwarecmd.Command{}, err
	}
	trust, err := catalogtrust.Production()
	if err != nil {
		return softwarecmd.Command{}, err
	}
	source, err := catalogsource.NewProductionHTTPS(&http.Client{})
	if err != nil {
		return softwarecmd.Command{}, err
	}
	capabilities, err := adapter.NewRegistry(upstreamrelease.Descriptor())
	if err != nil {
		return softwarecmd.Command{}, err
	}
	updateCatalog := func(ctx context.Context, options catalogupdate.Options) (catalogupdate.Result, error) {
		return catalogupdate.Run(ctx, options, trust, source, capabilities)
	}
	return softwarecmd.New(family, machine.DetectTarget, newSoftwareInvocationID, updateCatalog)
}

func newSoftwareInvocationID() (string, error) {
	var identity [16]byte
	if _, err := rand.Read(identity[:]); err != nil {
		return "", err
	}
	return "software-" + hex.EncodeToString(identity[:]), nil
}

func changeStatus(changed, dryRun bool) string {
	if !changed {
		return "unchanged"
	}
	if dryRun {
		return "would-change"
	}
	return "changed"
}

func runApply(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("temper apply", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifestPath := flags.String("manifest", "manifest.yaml", "path to the user-owned manifest")
	lockPath := flags.String("lock", "manifest.lock.yaml", "path to the resolution lock")
	root := flags.String("root", "", "Temper data root (required; never inferred from the live stack)")
	mode := flags.String("mode", "local", "mode to render")
	piModelsBase := flags.String("pi-models-base", "", "existing Pi models.json to merge")
	piSettingsBase := flags.String("pi-settings-base", "", "existing Pi settings.json to merge")
	dryRun := flags.Bool("dry-run", false, "verify selected sets and report without writing")
	flags.Usage = func() { applyUsage(stderr) }
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *root == "" {
		if flags.NArg() != 0 {
			fmt.Fprintf(stderr, "temper apply: unexpected arguments: %s\n\n", strings.Join(flags.Args(), " "))
		} else {
			fmt.Fprintln(stderr, "temper apply: --root is required")
		}
		applyUsage(stderr)
		return 2
	}

	result, err := applyverb.Run(ctx, applyverb.Options{
		ManifestPath:       *manifestPath,
		LockPath:           *lockPath,
		Root:               *root,
		Mode:               *mode,
		PiModelsBasePath:   *piModelsBase,
		PiSettingsBasePath: *piSettingsBase,
		DryRun:             *dryRun,
	})
	if err != nil {
		fmt.Fprintf(stderr, "temper apply: %v\n", err)
		return 1
	}
	status := changeStatus(result.Changed, result.DryRun)
	fmt.Fprintf(stdout, "RESULT apply %s mode=%s generation=%s\n", status, result.Mode, result.Generation)
	for _, artifact := range result.Artifacts {
		fmt.Fprintf(stdout, "ARTIFACT %s\n", artifact)
	}
	return 0
}

func usage(writer io.Writer) {
	fmt.Fprintf(writer, "temper %s — deterministic local-AI configuration\n\n", version)
	fmt.Fprintln(writer, "usage:")
	fmt.Fprintln(writer, "  temper apply [options]")
	fmt.Fprintln(writer, "  temper resolve [options]")
	fmt.Fprintln(writer, "  temper fetch <layout-id> --root PATH [options]")
	fmt.Fprintln(writer, "  temper check --root PATH [options]")
	fmt.Fprintln(writer, "  temper update [layout-id] [options]")
	fmt.Fprintln(writer, "  temper machine facts")
	fmt.Fprintln(writer, "  temper field-kit baseline <verify|inspect|explain|start|status|run|run-next|finish> [options]")
	fmt.Fprintln(writer, "  temper field-kit bind [options]")
	fmt.Fprintln(writer, "  temper probe serve [options]")
	fmt.Fprintln(writer, "  temper software <install|check|remove> [options]")
	fmt.Fprintln(writer, "  temper version")
	fmt.Fprintln(writer, "  temper help")
}

func machineUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: temper machine facts")
}

func applyUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: temper apply --root PATH [--manifest PATH] [--lock PATH] [--mode NAME] [--dry-run]")
}

func resolveUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: temper resolve [--manifest PATH] [--lock PATH] [--dry-run]")
}

func fetchUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: temper fetch <layout-id> --root PATH [--manifest PATH] [--lock PATH] [--dry-run]")
}

func checkUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: temper check --root PATH [--manifest PATH] [--lock PATH] [--mode NAME] [--verify]")
}

func updateUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: temper update [layout-id] [--manifest PATH] [--lock PATH] [--dry-run]")
}
