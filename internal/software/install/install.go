// Package install orchestrates the C6 read -> plan -> prepare -> adapter
// effect -> inspect -> receipt -> finalize protocol for one exact software
// lock and named installation.
package install

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"time"

	"github.com/temper-sh/temper/internal/datadir"
	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/installplan"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/receipt"
	"github.com/temper-sh/temper/internal/software/receiptstore"
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
	HostTarget           software.Target
	RequiredReceiptPaths []string
	DryRun               bool
	InvocationID         string
	LeaseDuration        time.Duration
	Now                  func() time.Time
}

type Package struct {
	ID       string
	Method   string
	Adapter  string
	RootUnit string
}

type Result struct {
	Changed       bool
	DryRun        bool
	Installation  string
	Packages      int
	Units         int
	Effects       int
	Claims        int
	PackageRows   []Package
	Groups        []installplan.Group
	ReceiptSHA256 string
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
	ownReceipt, err := receiptstore.Read(root, options.Installation)
	if err != nil {
		return Result{}, err
	}
	stateSnapshot, err := statestore.Read(root)
	if err != nil {
		return Result{}, err
	}
	plannerState := installplan.State{Shared: installplan.SharedState{Root: root, Units: map[string]installplan.SharedUnit{}}}
	if stateSnapshot.Exists() {
		plannerState, err = stateSnapshot.Document.Projection(installation)
		if err != nil {
			return Result{}, err
		}
	}
	if ownReceipt.Exists() {
		if err := ownReceipt.Document.ValidateAgainst(desired, installation); err != nil {
			return Result{}, err
		}
		previous := ownReceipt.Document.Previous()
		plannerState.Previous = &previous
	}
	requirements, err := readAndVerifyRequirements(ctx, desired, installation, options.RequiredReceiptPaths, adapters, stateSnapshot)
	if err != nil {
		return Result{}, err
	}
	plannerState.Requirements = requirements
	observed, err := adapters.Inspect(ctx, desired.Target, installation, desired.Units)
	if err != nil {
		return Result{}, err
	}
	plan, err := installplan.Build(desired, installation, effectModels, observed, plannerState)
	if err != nil {
		return Result{}, err
	}
	recovering := plannerState.Prepared != nil
	result := Result{
		Changed: plan.Changed() || recovering, DryRun: options.DryRun, Installation: options.Installation,
		Packages: len(desired.Selections), Units: len(desired.Units), Effects: plan.EffectCount(), Claims: plan.ClaimWriteCount(),
		PackageRows: packageRows(desired), Groups: append([]installplan.Group(nil), plan.Groups...),
	}
	if !result.Changed || options.DryRun {
		if ownReceipt.Exists() {
			result.ReceiptSHA256 = receipt.Digest(ownReceipt.Data)
		}
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
	startedAt := now().UTC()
	var currentState *rootstate.Document
	if stateSnapshot.Exists() {
		currentState = &stateSnapshot.Document
	}
	prepared, prepareChanged, fence, err := rootstate.Prepare(currentState, desired, plan, observed, rootstate.Lease{
		InvocationID: options.InvocationID, Now: startedAt, Duration: leaseDuration,
	})
	if err != nil {
		return Result{}, err
	}
	if prepareChanged {
		if err := stateSnapshot.Commit(ctx, prepared); err != nil {
			return Result{}, err
		}
	}
	for _, group := range plan.Groups {
		if !group.ChangesProvider() {
			continue
		}
		if fence == 0 {
			return Result{}, errors.New("software provider effect has no prepared operation fence")
		}
		current, err := statestore.Read(root)
		if err != nil {
			return Result{}, err
		}
		if !current.Exists() {
			return Result{}, errors.New("prepared software root state disappeared before provider effect")
		}
		if err := current.Document.AssertFence(options.Installation, options.InvocationID, fence, now().UTC()); err != nil {
			return Result{}, err
		}
		if err := adapters.Install(ctx, desired.Target, installation, group, desired.Units); err != nil {
			return Result{}, err
		}
	}
	postState, err := adapters.Inspect(ctx, desired.Target, installation, desired.Units)
	if err != nil {
		return Result{}, fmt.Errorf("inspect software post-state: %w", err)
	}
	if plan.ReceiptWrite {
		candidate, err := receipt.Build(desired, plan, postState, now().UTC())
		if err != nil {
			return Result{}, err
		}
		if err := ownReceipt.Commit(ctx, candidate); err != nil {
			return Result{}, err
		}
		data, err := receipt.Marshal(candidate)
		if err != nil {
			return Result{}, err
		}
		result.ReceiptSHA256 = receipt.Digest(data)
	} else {
		if !ownReceipt.Exists() {
			return Result{}, errors.New("software plan omitted receipt write without an existing receipt")
		}
		if err := ownReceipt.Document.VerifyObservation(postState); err != nil {
			return Result{}, err
		}
		result.ReceiptSHA256 = receipt.Digest(ownReceipt.Data)
	}
	if fence != 0 {
		finalSnapshot, err := statestore.Read(root)
		if err != nil {
			return Result{}, err
		}
		if !finalSnapshot.Exists() {
			return Result{}, errors.New("prepared software root state disappeared before finalization")
		}
		finalized, err := rootstate.Finalize(finalSnapshot.Document, options.Installation, options.InvocationID, fence, now().UTC())
		if err != nil {
			return Result{}, err
		}
		if err := finalSnapshot.Commit(ctx, finalized); err != nil {
			return Result{}, err
		}
	}
	return result, nil
}

func packageRows(desired softwarelock.Document) []Package {
	ids := make([]string, 0, len(desired.Selections))
	for id := range desired.Selections {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	rows := make([]Package, 0, len(ids))
	for _, id := range ids {
		selection := desired.Selections[id]
		rows = append(rows, Package{ID: id, Method: selection.Method, Adapter: selection.Adapter, RootUnit: selection.RootUnit})
	}
	return rows
}

func readAndVerifyRequirements(ctx context.Context, desired softwarelock.Document, installation installplan.Installation, paths []string, adapters adapter.InstallationFamily, state statestore.Snapshot) ([]installplan.SatisfiedRequirement, error) {
	wanted := make(map[string]bool, len(desired.Requires))
	for _, requirement := range desired.Requires {
		wanted[requirement.SoftwareLockDigest] = true
	}
	if len(paths) != len(wanted) {
		return nil, fmt.Errorf("software lock requires %d base receipts, got %d", len(wanted), len(paths))
	}
	seen := map[string]bool{}
	result := make([]installplan.SatisfiedRequirement, 0, len(paths))
	for _, path := range paths {
		document, data, err := receiptstore.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if !wanted[document.SoftwareLockDigest] {
			return nil, fmt.Errorf("software receipt %q is not required by this lock", path)
		}
		if seen[document.SoftwareLockDigest] {
			return nil, fmt.Errorf("required software lock %q has more than one receipt", document.SoftwareLockDigest)
		}
		seen[document.SoftwareLockDigest] = true
		if document.Root != installation.Root || document.Target != desired.Target || document.Installation == installation.ID {
			return nil, fmt.Errorf("required software receipt %q belongs to another root, target, or the current installation", path)
		}
		baseInstallation := installplan.Installation{ID: document.Installation, Root: document.Root}
		observed, err := adapters.Inspect(ctx, document.Target, baseInstallation, document.DesiredUnits())
		if err != nil {
			return nil, fmt.Errorf("inspect required software receipt %q: %w", path, err)
		}
		if err := document.VerifyObservation(observed); err != nil {
			return nil, fmt.Errorf("verify required software receipt %q: %w", path, err)
		}
		if err := verifyRequiredSharedClaims(document, state); err != nil {
			return nil, fmt.Errorf("verify required software receipt %q: %w", path, err)
		}
		result = append(result, installplan.SatisfiedRequirement{
			SoftwareLockDigest: document.SoftwareLockDigest, InstallationID: document.Installation, ReceiptSHA256: receipt.Digest(data),
		})
	}
	for digest := range wanted {
		if !seen[digest] {
			return nil, fmt.Errorf("required base software lock %q has no receipt", digest)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].SoftwareLockDigest < result[j].SoftwareLockDigest })
	return result, nil
}

func verifyRequiredSharedClaims(document receipt.Document, state statestore.Snapshot) error {
	for unitID, unit := range document.Units {
		if unit.SharedClaim == "" {
			continue
		}
		if !state.Exists() {
			return fmt.Errorf("shared receipt unit %q has no root-state authority", unitID)
		}
		if state.Document.Root != document.Root {
			return fmt.Errorf("shared receipt unit %q belongs to another root-state authority", unitID)
		}
		shared, ok := state.Document.SharedUnits[unit.SharedClaim]
		locked := softwarelock.Unit{
			Adapter: unit.Adapter, Scope: unit.Scope, NativeName: unit.NativeName,
			Version: unit.Version, Revision: unit.Revision,
			Dependencies: unit.Dependencies, Artifacts: unit.Artifacts,
		}
		registered := installplan.ObservedUnit{
			Present: true, Adapter: shared.Adapter, Scope: shared.Scope, NativeName: shared.NativeName,
			Version: shared.Version, Revision: shared.Revision,
			Dependencies: shared.Dependencies, Artifacts: shared.Artifacts, Location: shared.Location,
		}
		if !ok || !installplan.MatchesLock(locked, registered) || shared.Location != unit.Location || shared.Acquisition != unit.Ownership {
			return fmt.Errorf("shared receipt unit %q disagrees with root-state authority", unitID)
		}
		claim, ok := shared.Claims[document.Installation]
		if !ok || claim.Status != installplan.ClaimActive || claim.SoftwareLockDigest != document.SoftwareLockDigest || claim.UnitID != unitID {
			return fmt.Errorf("shared receipt unit %q has no matching active claim", unitID)
		}
	}
	return nil
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
