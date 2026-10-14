// Package softwarecmd owns the public command edge for exact software
// installation, read-only audit, and provenance-guided removal. The same
// operations serve in-process callers that already hold an exact lock.
package softwarecmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/temper-sh/temper/internal/datadir"
	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
	checkverb "github.com/temper-sh/temper/internal/software/check"
	installverb "github.com/temper-sh/temper/internal/software/install"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/lockstore"
	removeverb "github.com/temper-sh/temper/internal/software/remove"
)

type TargetDetector func(context.Context) (software.Target, error)
type InvocationIDSource func() (string, error)

type Command struct {
	adapters        adapter.InstallationFamily
	detectTarget    TargetDetector
	newInvocationID InvocationIDSource
}

func New(adapters adapter.InstallationFamily, detectTarget TargetDetector, newInvocationID InvocationIDSource) (Command, error) {
	if detectTarget == nil {
		return Command{}, errors.New("software command target detector is required")
	}
	if newInvocationID == nil {
		return Command{}, errors.New("software command invocation id source is required")
	}
	return Command{adapters: adapters, detectTarget: detectTarget, newInvocationID: newInvocationID}, nil
}

func (c Command) Run(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) == 0 {
		usage(stderr)
		return 2
	}
	switch arguments[0] {
	case "help", "--help", "-h":
		usage(stdout)
		return 0
	case "install":
		return c.runInstall(ctx, arguments[1:], stdout, stderr)
	case "check":
		return c.runCheck(ctx, arguments[1:], stdout, stderr)
	case "remove":
		return c.runRemove(ctx, arguments[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "temper software: unknown verb %q\n\n", arguments[0])
		usage(stderr)
		return 2
	}
}

func (c Command) runInstall(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("temper software install", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "Temper control root (required; never inferred from the live stack)")
	installation := flags.String("installation", "", "stable installation identity")
	lockPath := flags.String("lock", "software.lock.yaml", "path to the exact software lock")
	var requiredReceipts pathList
	flags.Var(&requiredReceipts, "require-receipt", "canonical required base receipt (repeatable)")
	dryRun := flags.Bool("dry-run", false, "inspect and plan without writing or invoking an installer")
	jsonOutput := flags.Bool("json", false, "print the full report as one JSON document")
	flags.Usage = func() { installUsage(stderr) }
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if !validRequiredFlags(flags, *root, *installation, stderr, "install", installUsage) {
		return 2
	}
	lock, err := readLock(*lockPath)
	if err != nil {
		fmt.Fprintf(stderr, "temper software install: %v\n", err)
		return 1
	}
	result, err := c.install(ctx, installverb.Options{
		Lock: lock, Root: *root, Installation: *installation,
		RequiredReceiptPaths: append([]string(nil), requiredReceipts...), DryRun: *dryRun,
	})
	if err != nil {
		fmt.Fprintf(stderr, "temper software install: %v\n", err)
		return 1
	}
	if *jsonOutput {
		_ = writeJSON(stdout, newInstallReport(result))
	} else {
		renderInstall(stdout, result)
	}
	return 0
}

// Install applies one exact lock to a named installation and writes the
// command's summary.
func (c Command) Install(ctx context.Context, root, installation string, lock softwarelock.Document, out io.Writer) error {
	result, err := c.install(ctx, installverb.Options{Lock: lock, Root: root, Installation: installation})
	if err == nil {
		renderInstall(out, result)
	}
	return err
}

func (c Command) install(ctx context.Context, options installverb.Options) (installverb.Result, error) {
	if err := datadir.Open(options.Root, !options.DryRun); err != nil {
		return installverb.Result{}, err
	}
	target, err := c.detectTarget(ctx)
	if err != nil {
		return installverb.Result{}, fmt.Errorf("detect host target: %w", err)
	}
	options.HostTarget = target
	if options.InvocationID, err = c.newInvocationID(); err != nil {
		return installverb.Result{}, fmt.Errorf("create invocation identity: %w", err)
	}
	return installverb.Run(ctx, options, c.adapters)
}

func (c Command) runCheck(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("temper software check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "Temper control root (required; never inferred from the live stack)")
	installation := flags.String("installation", "", "stable installation identity")
	lockPath := flags.String("lock", "software.lock.yaml", "path to the exact software lock")
	var requiredReceipts pathList
	flags.Var(&requiredReceipts, "require-receipt", "canonical required base receipt (repeatable)")
	jsonOutput := flags.Bool("json", false, "print the full report as one JSON document")
	flags.Usage = func() { checkUsage(stderr) }
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if !validRequiredFlags(flags, *root, *installation, stderr, "check", checkUsage) {
		return 2
	}
	lock, err := readLock(*lockPath)
	if err != nil {
		fmt.Fprintf(stderr, "temper software check: %v\n", err)
		return 1
	}
	result, err := c.check(ctx, checkverb.Options{
		Lock: lock, Root: *root, Installation: *installation,
		RequiredReceiptPaths: append([]string(nil), requiredReceipts...),
	})
	if err != nil {
		fmt.Fprintf(stderr, "temper software check: %v\n", err)
		return 1
	}
	if *jsonOutput {
		_ = writeJSON(stdout, newCheckReport(result))
	} else {
		renderCheck(stdout, result)
	}
	if !result.Exact() {
		return 1
	}
	return 0
}

// ErrNotExact reports a completed audit whose findings are in its report.
var ErrNotExact = errors.New("installed software differs from its exact lock")

// Check audits one named installation against its exact lock and writes the
// command's summary. Findings return ErrNotExact.
func (c Command) Check(ctx context.Context, root, installation string, lock softwarelock.Document, out io.Writer) error {
	result, err := c.check(ctx, checkverb.Options{Lock: lock, Root: root, Installation: installation})
	if err != nil {
		return err
	}
	renderCheck(out, result)
	if !result.Exact() {
		return ErrNotExact
	}
	return nil
}

func (c Command) check(ctx context.Context, options checkverb.Options) (checkverb.Result, error) {
	if err := datadir.Open(options.Root, false); err != nil {
		return checkverb.Result{}, err
	}
	target, err := c.detectTarget(ctx)
	if err != nil {
		return checkverb.Result{}, fmt.Errorf("detect host target: %w", err)
	}
	options.HostTarget = target
	return checkverb.Run(ctx, options, c.adapters)
}

func (c Command) runRemove(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("temper software remove", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "Temper control root (required; never inferred from the live stack)")
	installation := flags.String("installation", "", "stable installation identity")
	lockPath := flags.String("lock", "software.lock.yaml", "path to the exact software lock")
	dryRun := flags.Bool("dry-run", false, "inspect and plan without writing or invoking a remover")
	jsonOutput := flags.Bool("json", false, "print the full report as one JSON document")
	flags.Usage = func() { removeUsage(stderr) }
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if !validRequiredFlags(flags, *root, *installation, stderr, "remove", removeUsage) {
		return 2
	}
	lock, err := readLock(*lockPath)
	if err != nil {
		fmt.Fprintf(stderr, "temper software remove: %v\n", err)
		return 1
	}
	result, err := c.remove(ctx, removeverb.Options{Lock: lock, Root: *root, Installation: *installation, DryRun: *dryRun})
	if err != nil {
		fmt.Fprintf(stderr, "temper software remove: %v\n", err)
		return 1
	}
	if *jsonOutput {
		_ = writeJSON(stdout, newRemoveReport(result))
	} else {
		renderRemove(stdout, result)
	}
	return 0
}

// Remove releases one named installation of an exact lock and writes the
// command's summary.
func (c Command) Remove(ctx context.Context, root, installation string, lock softwarelock.Document, out io.Writer) error {
	result, err := c.remove(ctx, removeverb.Options{Lock: lock, Root: root, Installation: installation})
	if err == nil {
		renderRemove(out, result)
	}
	return err
}

func (c Command) remove(ctx context.Context, options removeverb.Options) (removeverb.Result, error) {
	if err := datadir.Open(options.Root, !options.DryRun); err != nil {
		return removeverb.Result{}, err
	}
	target, err := c.detectTarget(ctx)
	if err != nil {
		return removeverb.Result{}, fmt.Errorf("detect host target: %w", err)
	}
	options.HostTarget = target
	if options.InvocationID, err = c.newInvocationID(); err != nil {
		return removeverb.Result{}, fmt.Errorf("create invocation identity: %w", err)
	}
	return removeverb.Run(ctx, options, c.adapters)
}

// readLock is the command edge's only file read: one regular lock file.
func readLock(path string) (softwarelock.Document, error) {
	snapshot, err := lockstore.Read(path)
	if err != nil {
		return softwarelock.Document{}, err
	}
	if !snapshot.Exists() {
		return softwarelock.Document{}, fmt.Errorf("software lock %q does not exist", path)
	}
	return snapshot.Document, nil
}

func validRequiredFlags(flags *flag.FlagSet, root, installation string, stderr io.Writer, verb string, usageFn func(io.Writer)) bool {
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "temper software %s: unexpected arguments: %s\n\n", verb, strings.Join(flags.Args(), " "))
		usageFn(stderr)
		return false
	}
	var missing []string
	if root == "" {
		missing = append(missing, "--root")
	}
	if installation == "" {
		missing = append(missing, "--installation")
	}
	if len(missing) != 0 {
		predicate := "is"
		if len(missing) > 1 {
			predicate = "are"
		}
		fmt.Fprintf(stderr, "temper software %s: %s %s required\n", verb, strings.Join(missing, " and "), predicate)
		usageFn(stderr)
		return false
	}
	return true
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

type pathList []string

func (p *pathList) String() string { return strings.Join(*p, ",") }

func (p *pathList) Set(value string) error {
	if value == "" {
		return errors.New("receipt path must not be empty")
	}
	*p = append(*p, value)
	return nil
}

func usage(writer io.Writer) {
	fmt.Fprintln(writer, "usage:")
	fmt.Fprintln(writer, "  temper software install --root PATH --installation ID [options]")
	fmt.Fprintln(writer, "  temper software check --root PATH --installation ID [options]")
	fmt.Fprintln(writer, "  temper software remove --root PATH --installation ID [options]")
}

func installUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: temper software install --root PATH --installation ID [--lock PATH] [--require-receipt PATH]... [--dry-run]")
}

func checkUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: temper software check --root PATH --installation ID [--lock PATH] [--require-receipt PATH]...")
}

func removeUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: temper software remove --root PATH --installation ID [--lock PATH] [--dry-run]")
}
