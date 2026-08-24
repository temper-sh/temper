// Package softwarecmd owns the public C11 command edge for exact software
// installation, read-only audit, and provenance-guided removal.
package softwarecmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
	checkverb "github.com/temper-sh/temper/internal/software/check"
	installverb "github.com/temper-sh/temper/internal/software/install"
	"github.com/temper-sh/temper/internal/software/installplan"
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
	target, err := c.detectTarget(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "temper software install: detect host target: %v\n", err)
		return 1
	}
	invocationID, err := c.newInvocationID()
	if err != nil {
		fmt.Fprintf(stderr, "temper software install: create invocation identity: %v\n", err)
		return 1
	}
	result, err := installverb.Run(ctx, installverb.Options{
		LockPath: *lockPath, Root: *root, Installation: *installation, HostTarget: target,
		RequiredReceiptPaths: append([]string(nil), requiredReceipts...), DryRun: *dryRun, InvocationID: invocationID,
	}, c.adapters)
	if err != nil {
		fmt.Fprintf(stderr, "temper software install: %v\n", err)
		return 1
	}
	renderInstall(stdout, result)
	return 0
}

func (c Command) runCheck(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("temper software check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "Temper control root (required; never inferred from the live stack)")
	installation := flags.String("installation", "", "stable installation identity")
	lockPath := flags.String("lock", "software.lock.yaml", "path to the exact software lock")
	var requiredReceipts pathList
	flags.Var(&requiredReceipts, "require-receipt", "canonical required base receipt (repeatable)")
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
	target, err := c.detectTarget(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "temper software check: detect host target: %v\n", err)
		return 1
	}
	result, err := checkverb.Run(ctx, checkverb.Options{
		LockPath: *lockPath, Root: *root, Installation: *installation, HostTarget: target,
		RequiredReceiptPaths: append([]string(nil), requiredReceipts...),
	}, c.adapters)
	if err != nil {
		fmt.Fprintf(stderr, "temper software check: %v\n", err)
		return 1
	}
	renderCheck(stdout, result)
	if !result.Exact() {
		return 1
	}
	return 0
}

func (c Command) runRemove(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("temper software remove", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "Temper control root (required; never inferred from the live stack)")
	installation := flags.String("installation", "", "stable installation identity")
	lockPath := flags.String("lock", "software.lock.yaml", "path to the exact software lock")
	dryRun := flags.Bool("dry-run", false, "inspect and plan without writing or invoking a remover")
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
	target, err := c.detectTarget(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "temper software remove: detect host target: %v\n", err)
		return 1
	}
	invocationID, err := c.newInvocationID()
	if err != nil {
		fmt.Fprintf(stderr, "temper software remove: create invocation identity: %v\n", err)
		return 1
	}
	result, err := removeverb.Run(ctx, removeverb.Options{
		LockPath: *lockPath, Root: *root, Installation: *installation, HostTarget: target,
		DryRun: *dryRun, InvocationID: invocationID,
	}, c.adapters)
	if err != nil {
		fmt.Fprintf(stderr, "temper software remove: %v\n", err)
		return 1
	}
	renderRemove(stdout, result)
	return 0
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

func renderInstall(writer io.Writer, result installverb.Result) {
	status := changeStatus(result.Changed, result.DryRun)
	fmt.Fprintf(writer, "RESULT software-install %s installation=%s packages=%d units=%d effects=%d claims=%d\n",
		status, result.Installation, result.Packages, result.Units, result.Effects, result.Claims)
	for _, row := range result.PackageRows {
		fmt.Fprintf(writer, "PACKAGE %s method=%s adapter=%s root-unit=%s\n", row.ID, row.Method, row.Adapter, row.RootUnit)
	}
	for _, group := range result.Groups {
		effect := "unchanged"
		if group.ChangesProvider() {
			effect = "install"
			if group.EffectModel == installplan.EffectIsolated {
				effect = "publish-isolated"
			}
		}
		fmt.Fprintf(writer, "EFFECT %s %s units=%d\n", group.ID, effect, len(group.Units))
		for _, unit := range group.Units {
			claim := string(unit.ClaimAction)
			if claim == "" {
				claim = "none"
			}
			fmt.Fprintf(writer, "UNIT %s %s ownership=%s claim=%s\n", unit.ID, unit.Action, unit.Ownership, claim)
		}
	}
}

func renderCheck(writer io.Writer, result checkverb.Result) {
	status := "findings"
	if result.Exact() {
		status = "exact"
	}
	receipt := valueOrNone(result.ReceiptSHA256)
	fmt.Fprintf(writer, "RESULT software-check %s installation=%s packages=%d units=%d requirements=%d problems=%d receipt=%s\n",
		status, result.Installation, result.Packages, len(result.Units), len(result.Requirements), result.ProblemCount(), receipt)
	for _, requirement := range result.Requirements {
		fmt.Fprintf(writer, "REQUIREMENT %s %s installation=%s receipt=%s\n",
			requirement.SoftwareLockDigest, requirement.Status, valueOrNone(requirement.Installation), valueOrNone(requirement.ReceiptSHA256))
	}
	for _, unit := range result.Units {
		fmt.Fprintf(writer, "UNIT %s %s adapter=%s scope=%s location=%s ownership=%s claim=%s\n",
			unit.ID, unit.Status, unit.Adapter, unit.Scope, valueOrNone(unit.Location), unit.Ownership, valueOrNone(unit.Claim))
	}
	for _, finding := range result.Findings {
		fmt.Fprintf(writer, "PROBLEM code=%s unit=%s requirement=%s detail=%q\n",
			finding.Code, valueOrNone(finding.Unit), valueOrNone(finding.Requirement), finding.Detail)
	}
}

func renderRemove(writer io.Writer, result removeverb.Result) {
	status := changeStatus(result.Changed, result.DryRun)
	fmt.Fprintf(writer, "RESULT software-remove %s installation=%s packages=%d units=%d effects=%d claims=%d\n",
		status, result.Installation, result.Packages, result.Units, result.Effects, result.Claims)
	for _, group := range result.Groups {
		effect := "preserve"
		if group.ChangesProvider() {
			effect = "remove"
		}
		fmt.Fprintf(writer, "EFFECT %s %s units=%d\n", group.ID, effect, len(group.Units))
		for _, unit := range group.Units {
			claim := "none"
			if unit.SharedClaim != "" {
				claim = "release"
			}
			fmt.Fprintf(writer, "UNIT %s %s ownership=%s claim=%s\n", unit.ID, unit.Action, unit.Ownership, claim)
		}
	}
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

func valueOrNone(value string) string {
	if value == "" {
		return "none"
	}
	return value
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
