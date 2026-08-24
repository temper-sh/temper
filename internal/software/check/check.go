// Package check orchestrates the read-only software installation audit. It
// owns reads only; checkplan owns every comparison and finding decision.
package check

import (
	"context"
	"errors"
	"fmt"

	"github.com/temper-sh/temper/internal/datadir"
	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/checkplan"
	"github.com/temper-sh/temper/internal/software/installplan"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	softwarelockstore "github.com/temper-sh/temper/internal/software/lockstore"
	"github.com/temper-sh/temper/internal/software/receipt"
	"github.com/temper-sh/temper/internal/software/receiptstore"
	"github.com/temper-sh/temper/internal/software/rootstate"
	"github.com/temper-sh/temper/internal/software/statestore"
)

type Options struct {
	LockPath     string
	Root         string
	Installation string
	// HostTarget is the optional public-edge host binding. Generic callers may
	// leave it zero when they are executing against an injected provider.
	HostTarget           software.Target
	RequiredReceiptPaths []string
}

type Result = checkplan.Result

func Run(ctx context.Context, options Options, adapters adapter.InstallationFamily) (Result, error) {
	if options.LockPath == "" || options.Installation == "" {
		return Result{}, errors.New("software lock path and installation id are required")
	}
	if options.HostTarget != (software.Target{}) {
		if err := options.HostTarget.Validate(); err != nil {
			return Result{}, fmt.Errorf("host target: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	root, err := datadir.Resolve(options.Root)
	if err != nil {
		return Result{}, err
	}
	desiredSnapshot, err := softwarelockstore.Read(options.LockPath)
	if err != nil {
		return Result{}, fmt.Errorf("read software lock: %w", err)
	}
	if !desiredSnapshot.Exists() {
		return Result{}, fmt.Errorf("software lock %q does not exist", options.LockPath)
	}
	desired := desiredSnapshot.Document
	if options.HostTarget != (software.Target{}) && desired.Target != options.HostTarget {
		return Result{}, fmt.Errorf("software lock target %s does not match host target %s", desired.Target, options.HostTarget)
	}
	effectModels, err := adapters.EffectModels(desired)
	if err != nil {
		return Result{}, err
	}
	installation := installplan.Installation{ID: options.Installation, Root: root}
	ownSnapshot, err := receiptstore.Read(root, options.Installation)
	if err != nil {
		return Result{}, err
	}
	stateSnapshot, err := statestore.Read(root)
	if err != nil {
		return Result{}, err
	}
	var state *rootstate.Document
	if stateSnapshot.Exists() {
		state = &stateSnapshot.Document
	}
	requirements, err := observeRequirements(ctx, desired, installation, options.RequiredReceiptPaths, adapters, state)
	if err != nil {
		return Result{}, err
	}
	observed, err := adapters.Inspect(ctx, desired.Target, installation, desired.Units)
	if err != nil {
		return Result{}, err
	}
	var ownReceipt *receipt.Document
	if ownSnapshot.Exists() {
		ownReceipt = &ownSnapshot.Document
	}
	return checkplan.Analyze(checkplan.Input{
		Desired: desired, Installation: installation, EffectModels: effectModels,
		Observed: observed, Receipt: ownReceipt, State: state, Requirements: requirements,
	})
}

func observeRequirements(ctx context.Context, desired softwarelock.Document, installation installplan.Installation, paths []string, adapters adapter.InstallationFamily, state *rootstate.Document) ([]checkplan.RequirementObservation, error) {
	wanted := make(map[string]bool, len(desired.Requires))
	for _, requirement := range desired.Requires {
		wanted[requirement.SoftwareLockDigest] = true
	}
	observed := make([]checkplan.RequirementObservation, 0, len(paths))
	for _, path := range paths {
		document, data, err := receiptstore.ReadFile(path)
		if err != nil {
			return nil, err
		}
		observation := checkplan.RequirementObservation{
			SoftwareLockDigest: document.SoftwareLockDigest,
			Installation:       document.Installation,
			ReceiptSHA256:      receipt.Digest(data),
			Status:             checkplan.RequirementDrifted,
		}
		switch {
		case !wanted[document.SoftwareLockDigest]:
			observation.Detail = "supplied receipt is not required by the software lock"
		case document.Root != installation.Root:
			observation.Detail = "required software receipt belongs to another Temper root"
		case document.Target != desired.Target:
			observation.Detail = "required software receipt belongs to another target"
		case document.Installation == installation.ID:
			observation.Detail = "required software receipt belongs to the current installation"
		default:
			base := installplan.Installation{ID: document.Installation, Root: document.Root}
			provider, inspectErr := adapters.Inspect(ctx, document.Target, base, document.DesiredUnits())
			if inspectErr != nil {
				return nil, fmt.Errorf("inspect required software receipt %q: %w", path, inspectErr)
			}
			if verifyErr := document.VerifyObservation(provider); verifyErr != nil {
				observation.Detail = verifyErr.Error()
			} else if claimErr := verifyRequiredSharedClaims(document, state); claimErr != nil {
				observation.Detail = claimErr.Error()
			} else {
				observation.Status = checkplan.RequirementExact
			}
		}
		observed = append(observed, observation)
	}
	return observed, nil
}

func verifyRequiredSharedClaims(document receipt.Document, state *rootstate.Document) error {
	for unitID, unit := range document.Units {
		if unit.SharedClaim == "" {
			continue
		}
		if state == nil {
			return fmt.Errorf("shared receipt unit %q has no root-state authority", unitID)
		}
		if state.Root != document.Root {
			return fmt.Errorf("shared receipt unit %q belongs to another root-state authority", unitID)
		}
		shared, ok := state.SharedUnits[unit.SharedClaim]
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
