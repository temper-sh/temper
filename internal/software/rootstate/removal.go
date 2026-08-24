package rootstate

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/installplan"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/removeplan"
)

// RemovalProjection returns only the immutable facts the pure removal planner
// needs. A prepared install is a refusal, never guessed to be removable.
func (d Document) RemovalProjection(installation installplan.Installation) (removeplan.State, error) {
	if err := d.Validate(); err != nil {
		return removeplan.State{}, err
	}
	if d.Root != installation.Root {
		return removeplan.State{}, errors.New("software root state belongs to another root")
	}
	state := removeplan.State{Shared: installplan.SharedState{Root: d.Root, Units: make(map[string]installplan.SharedUnit, len(d.SharedUnits))}}
	for key, unit := range d.SharedUnits {
		claims := make(map[string]installplan.SharedClaim, len(unit.Claims))
		for claimant, claim := range unit.Claims {
			claims[claimant] = claim
		}
		state.Shared.Units[key] = installplan.SharedUnit{
			Adapter: unit.Adapter, Scope: unit.Scope, NativeName: unit.NativeName,
			Version: unit.Version, Revision: unit.Revision,
			Dependencies: append([]string(nil), unit.Dependencies...), Artifacts: append([]software.Artifact(nil), unit.Artifacts...),
			Location: unit.Location, Acquisition: unit.Acquisition, Lifecycle: unit.Lifecycle, Claims: claims,
		}
	}
	operation, ok := d.Operations[installation.ID]
	if !ok {
		return state, nil
	}
	if operation.Kind != "remove" {
		return removeplan.State{}, errors.New("installation has a prepared software install")
	}
	groups := make(map[string]removeplan.PreparedGroup, len(operation.Groups))
	for groupID, group := range operation.Groups {
		units := make(map[string]removeplan.PreparedUnit, len(group.Units))
		for unitID, unit := range group.Units {
			units[unitID] = removeplan.PreparedUnit{
				Before: unit.Before, Ownership: unit.OwnershipBefore, Location: unit.Location,
				RemoveProvider: unit.RemoveProvider, RetireShared: unit.RetireShared, SharedClaim: unit.SharedClaim,
			}
		}
		groups[groupID] = removeplan.PreparedGroup{
			ID: groupID, Adapter: group.Adapter, Scope: group.Scope, EffectModel: group.EffectModel, Units: units,
		}
	}
	state.Prepared = &removeplan.Prepared{
		LockDigest: operation.SoftwareLockDigest, Target: operation.Target,
		Installation: installation, Groups: groups,
	}
	return state, nil
}

// PrepareRemoval records immutable removal intent and releases this
// installation's shared claims in the same serialized root-state commit. A
// final Temper-added generation becomes retiring before any provider deletion.
func PrepareRemoval(current *Document, desired softwarelock.Document, plan removeplan.Plan, observed installplan.Observation, lease Lease) (Document, bool, uint64, error) {
	if err := desired.Validate(); err != nil {
		return Document{}, false, 0, err
	}
	if err := validateLease(lease); err != nil {
		return Document{}, false, 0, err
	}
	if plan.Target != desired.Target || observed.Target != desired.Target || plan.Installation.Root != observed.Root {
		return Document{}, false, 0, errors.New("prepared removal inputs disagree on target or root")
	}
	digest, err := desired.SemanticDigest()
	if err != nil {
		return Document{}, false, 0, err
	}
	if plan.LockDigest != digest {
		return Document{}, false, 0, errors.New("software removal plan belongs to another lock")
	}
	var next Document
	if current == nil {
		next = Document{Schema: SchemaV1, Root: plan.Installation.Root, Operations: map[string]Operation{}, SharedUnits: map[string]SharedUnit{}}
	} else {
		if err := current.Validate(); err != nil {
			return Document{}, false, 0, err
		}
		if current.Root != plan.Installation.Root {
			return Document{}, false, 0, errors.New("software root state belongs to another root")
		}
		next = cloneDocument(*current)
	}
	if operation, exists := next.Operations[plan.Installation.ID]; exists {
		if operation.Kind != "remove" || operation.SoftwareLockDigest != plan.LockDigest || operation.Target != plan.Target {
			return Document{}, false, 0, errors.New("prepared software operation belongs to another kind, lock, or target")
		}
		if !removalPlanMatchesOperation(plan, operation) {
			return Document{}, false, 0, errors.New("software removal plan differs from immutable prepared intent")
		}
		expires, err := time.Parse(time.RFC3339Nano, operation.LeaseExpiresAt)
		if err != nil {
			return Document{}, false, 0, err
		}
		if expires.After(lease.Now) {
			return Document{}, false, 0, fmt.Errorf("%w: installation=%s claimed_by=%s", ErrOperationBusy, plan.Installation.ID, operation.ClaimedBy)
		}
		operation.ClaimedBy = lease.InvocationID
		operation.LeaseExpiresAt = lease.Now.Add(lease.Duration).UTC().Format(time.RFC3339Nano)
		operation.Fence++
		next.Generation++
		next.Operations[plan.Installation.ID] = operation
		if err := next.Validate(); err != nil {
			return Document{}, false, 0, err
		}
		return next, true, operation.Fence, nil
	}
	if !plan.ReceiptRemove || len(plan.Groups) == 0 {
		return Document{}, false, 0, errors.New("software removal plan has no receipt-backed work")
	}
	operation, err := operationFromRemovalPlan(desired, plan, observed, lease)
	if err != nil {
		return Document{}, false, 0, err
	}
	for _, group := range plan.Groups {
		if group.EffectModel != installplan.EffectShared {
			continue
		}
		for _, planned := range group.Units {
			shared, ok := next.SharedUnits[planned.SharedClaim]
			if !ok || shared.Lifecycle != installplan.SharedActive {
				return Document{}, false, 0, fmt.Errorf("shared removal unit %q lost active root-state authority", planned.ID)
			}
			locked := desired.Units[planned.ID]
			if planned.SharedClaim != installplan.SharedUnitKey(locked.Adapter, locked.Scope, locked.NativeName) ||
				!sameSharedIdentity(shared, locked) || shared.Location != planned.Location || shared.Acquisition != planned.Ownership {
				return Document{}, false, 0, fmt.Errorf("shared removal unit %q differs from exact root-state authority", planned.ID)
			}
			claim, ok := shared.Claims[plan.Installation.ID]
			if !ok || claim.Status != installplan.ClaimActive || claim.SoftwareLockDigest != plan.LockDigest || claim.UnitID != planned.ID {
				return Document{}, false, 0, fmt.Errorf("shared removal unit %q lost its active claim", planned.ID)
			}
			delete(shared.Claims, plan.Installation.ID)
			switch {
			case planned.RetireShared:
				if len(shared.Claims) != 0 || shared.Acquisition != installplan.OwnershipTemperAdded {
					return Document{}, false, 0, fmt.Errorf("shared removal unit %q is no longer the final Temper-added claim", planned.ID)
				}
				shared.Lifecycle = installplan.SharedRetiring
				shared.Claims = map[string]installplan.SharedClaim{}
				next.SharedUnits[planned.SharedClaim] = shared
			case len(shared.Claims) == 0:
				if shared.Acquisition != installplan.OwnershipPreExisting {
					return Document{}, false, 0, fmt.Errorf("shared removal unit %q would orphan Temper-added authority", planned.ID)
				}
				delete(next.SharedUnits, planned.SharedClaim)
			default:
				next.SharedUnits[planned.SharedClaim] = shared
			}
		}
	}
	next.Generation++
	next.Operations[plan.Installation.ID] = operation
	if err := next.Validate(); err != nil {
		return Document{}, false, 0, err
	}
	return next, true, operation.Fence, nil
}

// FinalizeRemoval forgets only retiring generations owned by this exact
// operation, then removes the completed intent under the lease fence.
func FinalizeRemoval(current Document, installationID, invocationID string, fence uint64, now time.Time) (Document, error) {
	if err := current.AssertFence(installationID, invocationID, fence, now); err != nil {
		return Document{}, err
	}
	next := cloneDocument(current)
	operation := next.Operations[installationID]
	if operation.Kind != "remove" {
		return Document{}, errors.New("prepared software operation is not a removal")
	}
	for _, group := range operation.Groups {
		for _, unit := range group.Units {
			if !unit.RetireShared {
				continue
			}
			shared, ok := next.SharedUnits[unit.SharedClaim]
			if !ok || shared.Lifecycle != installplan.SharedRetiring || len(shared.Claims) != 0 {
				return Document{}, errors.New("retiring shared authority disappeared or changed before finalization")
			}
			delete(next.SharedUnits, unit.SharedClaim)
		}
	}
	delete(next.Operations, installationID)
	next.Generation++
	if err := next.Validate(); err != nil {
		return Document{}, err
	}
	return next, nil
}

func operationFromRemovalPlan(desired softwarelock.Document, plan removeplan.Plan, observed installplan.Observation, lease Lease) (Operation, error) {
	groups := make(map[string]OperationGroup, len(plan.Groups))
	seen := map[string]bool{}
	for _, group := range plan.Groups {
		if group.ID != group.Adapter+":"+group.Scope || (group.EffectModel != installplan.EffectShared && group.EffectModel != installplan.EffectIsolated) {
			return Operation{}, fmt.Errorf("software removal plan group %q has invalid adapter semantics", group.ID)
		}
		if _, exists := groups[group.ID]; exists {
			return Operation{}, fmt.Errorf("software removal plan repeats group %q", group.ID)
		}
		if len(group.Units) == 0 {
			return Operation{}, fmt.Errorf("software removal plan group %q has no units", group.ID)
		}
		units := make(map[string]OperationUnit, len(group.Units))
		for _, planned := range group.Units {
			if seen[planned.ID] {
				return Operation{}, fmt.Errorf("software removal plan repeats unit %q", planned.ID)
			}
			seen[planned.ID] = true
			locked, ok := desired.Units[planned.ID]
			if !ok || locked.Adapter != group.Adapter || locked.Scope != group.Scope {
				return Operation{}, fmt.Errorf("software removal plan references invalid unit %q", planned.ID)
			}
			if planned.Action != removeplan.ActionPreserve && planned.Action != removeplan.ActionRemove {
				return Operation{}, fmt.Errorf("software removal plan unit %q has invalid action %q", planned.ID, planned.Action)
			}
			if planned.Execute && planned.Action != removeplan.ActionRemove {
				return Operation{}, fmt.Errorf("software removal plan unit %q executes a preserve action", planned.ID)
			}
			if group.EffectModel == installplan.EffectIsolated && !strictlyBelow(installplan.InstallationRoot(plan.Installation), planned.Location) {
				return Operation{}, fmt.Errorf("isolated removal unit %q location is outside its named installation", planned.ID)
			}
			if group.EffectModel == installplan.EffectShared && planned.SharedClaim != installplan.SharedUnitKey(locked.Adapter, locked.Scope, locked.NativeName) {
				return Operation{}, fmt.Errorf("shared removal unit %q has the wrong provider claim", planned.ID)
			}
			before := installplan.BeforeAbsent
			if observed.Units[planned.ID].Present {
				before = installplan.BeforeExact
			}
			units[planned.ID] = OperationUnit{
				Before: before, OwnershipBefore: planned.Ownership, Location: planned.Location,
				RemoveProvider: planned.Action == removeplan.ActionRemove, RetireShared: planned.RetireShared,
				SharedClaim: planned.SharedClaim,
			}
		}
		groups[group.ID] = OperationGroup{Adapter: group.Adapter, Scope: group.Scope, EffectModel: group.EffectModel, Units: units}
	}
	if len(seen) != len(desired.Units) {
		return Operation{}, errors.New("software removal plan does not cover the complete lock")
	}
	operation := Operation{
		Kind: "remove", SoftwareLockDigest: plan.LockDigest, Target: plan.Target,
		StartedAt: lease.Now.UTC().Format(time.RFC3339Nano), ClaimedBy: lease.InvocationID,
		LeaseExpiresAt: lease.Now.Add(lease.Duration).UTC().Format(time.RFC3339Nano), Fence: 1, Groups: groups,
	}
	operation.PlanDigest = operationIntentDigest(plan.Installation.ID, operation)
	return operation, nil
}

func removalPlanMatchesOperation(plan removeplan.Plan, operation Operation) bool {
	if len(plan.Groups) != len(operation.Groups) {
		return false
	}
	for _, group := range plan.Groups {
		stored, ok := operation.Groups[group.ID]
		if !ok || stored.Adapter != group.Adapter || stored.Scope != group.Scope || stored.EffectModel != group.EffectModel || len(stored.Units) != len(group.Units) {
			return false
		}
		for _, unit := range group.Units {
			intent, ok := stored.Units[unit.ID]
			if !ok || intent.OwnershipBefore != unit.Ownership || intent.Location != unit.Location ||
				intent.RemoveProvider != (unit.Action == removeplan.ActionRemove) || intent.RetireShared != unit.RetireShared || intent.SharedClaim != unit.SharedClaim {
				return false
			}
		}
	}
	return true
}

func strictlyBelow(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
