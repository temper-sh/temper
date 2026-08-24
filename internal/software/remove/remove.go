// Package remove orchestrates provenance-guided software removal through
// read -> pure plan -> prepared authority -> provider effect -> verification ->
// receipt release -> state finalization.
package remove

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/temper-sh/temper/internal/datadir"
	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/installplan"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/receipt"
	"github.com/temper-sh/temper/internal/software/receiptstore"
	"github.com/temper-sh/temper/internal/software/removeplan"
	"github.com/temper-sh/temper/internal/software/rootstate"
	"github.com/temper-sh/temper/internal/software/statestore"
)

const defaultLeaseDuration = 15 * time.Minute

type Options struct {
	LockPath     string
	Root         string
	Installation string
	// HostTarget is the optional public-edge host binding. Generic callers may
	// leave it zero when they are executing against an injected provider.
	HostTarget    software.Target
	DryRun        bool
	InvocationID  string
	LeaseDuration time.Duration
	Now           func() time.Time
}

type Result struct {
	Changed      bool
	DryRun       bool
	Installation string
	Packages     int
	Units        int
	Effects      int
	Claims       int
	Groups       []removeplan.Group
}

func Run(ctx context.Context, options Options, adapters adapter.InstallationFamily) (Result, error) {
	if options.LockPath == "" || options.Installation == "" || options.InvocationID == "" {
		return Result{}, errors.New("software lock path, installation id, and invocation id are required")
	}
	if options.HostTarget != (software.Target{}) {
		if err := options.HostTarget.Validate(); err != nil {
			return Result{}, fmt.Errorf("host target: %w", err)
		}
	}
	root, err := datadir.Resolve(options.Root)
	if err != nil {
		return Result{}, err
	}
	installation := installplan.Installation{ID: options.Installation, Root: root}
	lockData, err := readRegularFile(options.LockPath)
	if err != nil {
		return Result{}, fmt.Errorf("read software lock: %w", err)
	}
	desired, err := softwarelock.Parse(lockData)
	if err != nil {
		return Result{}, err
	}
	if options.HostTarget != (software.Target{}) && desired.Target != options.HostTarget {
		return Result{}, fmt.Errorf("software lock target %s does not match host target %s", desired.Target, options.HostTarget)
	}
	effectModels, err := adapters.EffectModels(desired)
	if err != nil {
		return Result{}, err
	}
	receiptSnapshot, err := receiptstore.Read(root, installation.ID)
	if err != nil {
		return Result{}, err
	}
	stateSnapshot, err := statestore.Read(root)
	if err != nil {
		return Result{}, err
	}
	plannerState := removeplan.State{Shared: installplan.SharedState{Root: root, Units: map[string]installplan.SharedUnit{}}}
	if stateSnapshot.Exists() {
		plannerState, err = stateSnapshot.Document.RemovalProjection(installation)
		if err != nil {
			return Result{}, err
		}
	}
	var previous *receipt.Document
	if receiptSnapshot.Exists() {
		if err := receiptSnapshot.Document.ValidateAgainst(desired, installation); err != nil {
			return Result{}, err
		}
		document := receiptSnapshot.Document
		previous = &document
	}
	var observed *installplan.Observation
	if previous != nil || plannerState.Prepared != nil {
		providerState, err := adapters.Inspect(ctx, desired.Target, installation, desired.Units)
		if err != nil {
			return Result{}, err
		}
		observed = &providerState
	}
	plan, err := removeplan.Build(desired, installation, effectModels, observed, previous, plannerState)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		Changed: plan.Changed(), DryRun: options.DryRun, Installation: installation.ID,
		Packages: len(desired.Selections), Units: len(desired.Units), Effects: plan.EffectCount(), Claims: plan.ClaimReleaseCount(),
		Groups: append([]removeplan.Group(nil), plan.Groups...),
	}
	if !result.Changed || options.DryRun {
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	leaseDuration := options.LeaseDuration
	if leaseDuration == 0 {
		leaseDuration = defaultLeaseDuration
	}
	var currentState *rootstate.Document
	if stateSnapshot.Exists() {
		currentState = &stateSnapshot.Document
	}
	prepared, changed, fence, err := rootstate.PrepareRemoval(currentState, desired, plan, *observed, rootstate.Lease{
		InvocationID: options.InvocationID, Now: now().UTC(), Duration: leaseDuration,
	})
	if err != nil {
		return Result{}, err
	}
	if !changed || fence == 0 {
		return Result{}, errors.New("software removal did not record prepared authority")
	}
	if err := stateSnapshot.Commit(ctx, prepared); err != nil {
		return Result{}, err
	}
	for _, group := range plan.Groups {
		if !group.ChangesProvider() {
			continue
		}
		current, err := statestore.Read(root)
		if err != nil {
			return Result{}, err
		}
		if !current.Exists() {
			return Result{}, errors.New("prepared software root state disappeared before provider removal")
		}
		if err := current.Document.AssertFence(installation.ID, options.InvocationID, fence, now().UTC()); err != nil {
			return Result{}, err
		}
		if err := adapters.Remove(ctx, desired.Target, installation, group, desired.Units); err != nil {
			return Result{}, err
		}
	}
	postState, err := adapters.Inspect(ctx, desired.Target, installation, desired.Units)
	if err != nil {
		return Result{}, fmt.Errorf("inspect software removal post-state: %w", err)
	}
	if err := removeplan.VerifyPostState(desired, plan, postState); err != nil {
		return Result{}, err
	}
	if _, err := receiptSnapshot.Remove(ctx); err != nil {
		return Result{}, err
	}
	finalSnapshot, err := statestore.Read(root)
	if err != nil {
		return Result{}, err
	}
	if !finalSnapshot.Exists() {
		return Result{}, errors.New("prepared software root state disappeared before removal finalization")
	}
	finalized, err := rootstate.FinalizeRemoval(finalSnapshot.Document, installation.ID, options.InvocationID, fence, now().UTC())
	if err != nil {
		return Result{}, err
	}
	if err := finalSnapshot.Commit(ctx, finalized); err != nil {
		return Result{}, err
	}
	return result, nil
}

func readRegularFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("expected a regular file, not a directory or symlink")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fs.ErrInvalid
	}
	return data, nil
}
