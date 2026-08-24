// Package removeplan computes provenance-guided software removal plans. It is
// pure: adapters inspect before this boundary and execute only the complete
// validated groups returned from it.
package removeplan

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/installplan"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/receipt"
)

var installationIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)

type Action string

const (
	ActionPreserve Action = "preserve"
	ActionRemove   Action = "remove"
)

type Unit struct {
	ID             string
	Action         Action
	Execute        bool
	Ownership      installplan.Ownership
	Location       string
	SharedClaim    string
	RetireShared   bool
	RequirePresent bool
}

type Group struct {
	ID          string
	Adapter     string
	Scope       string
	EffectModel installplan.EffectModel
	Units       []Unit
}

func (g Group) ChangesProvider() bool {
	for _, unit := range g.Units {
		if unit.Execute {
			return true
		}
	}
	return false
}

type PreparedUnit struct {
	Before         installplan.Before
	Ownership      installplan.Ownership
	Location       string
	RemoveProvider bool
	RetireShared   bool
	SharedClaim    string
}

type PreparedGroup struct {
	ID          string
	Adapter     string
	Scope       string
	EffectModel installplan.EffectModel
	Units       map[string]PreparedUnit
}

type Prepared struct {
	LockDigest   string
	Target       software.Target
	Installation installplan.Installation
	Groups       map[string]PreparedGroup
}

type State struct {
	Shared   installplan.SharedState
	Prepared *Prepared
}

type Plan struct {
	LockDigest    string
	Target        software.Target
	Installation  installplan.Installation
	Groups        []Group
	ReceiptRemove bool
	Recovering    bool
}

func (p Plan) EffectCount() int {
	count := 0
	for _, group := range p.Groups {
		if group.ChangesProvider() {
			count++
		}
	}
	return count
}

func (p Plan) ClaimReleaseCount() int {
	count := 0
	for _, group := range p.Groups {
		for _, unit := range group.Units {
			if unit.SharedClaim != "" {
				count++
			}
		}
	}
	return count
}

func (p Plan) Changed() bool { return p.ReceiptRemove || p.Recovering }

// Build derives a complete removal plan. A nil receipt is valid only for
// recovery from prepared intent or for an already-clean second run.
func Build(desired softwarelock.Document, installation installplan.Installation, effectModels map[string]installplan.EffectModel, observed *installplan.Observation, previous *receipt.Document, state State) (Plan, error) {
	if err := desired.Validate(); err != nil {
		return Plan{}, err
	}
	if err := validateInstallation(installation); err != nil {
		return Plan{}, err
	}
	digest, err := desired.SemanticDigest()
	if err != nil {
		return Plan{}, err
	}
	if state.Shared.Root != "" && state.Shared.Root != installation.Root {
		return Plan{}, errors.New("root-wide shared-claim state belongs to another root")
	}
	if previous != nil {
		if err := previous.ValidateAgainst(desired, installation); err != nil {
			return Plan{}, err
		}
	}
	if state.Prepared != nil {
		return buildPrepared(desired, installation, digest, effectModels, observed, state)
	}
	if previous == nil {
		if claimantUnit(state.Shared, installation.ID) != "" {
			return Plan{}, fmt.Errorf("installation %q has a shared claim but no receipt or prepared removal", installation.ID)
		}
		return Plan{LockDigest: digest, Target: desired.Target, Installation: installation}, nil
	}
	if observed == nil {
		return Plan{}, errors.New("software removal requires provider observation")
	}
	if err := validateObservation(desired, installation, *observed); err != nil {
		return Plan{}, err
	}
	models, err := validateEffectModels(desired, effectModels)
	if err != nil {
		return Plan{}, err
	}

	groups := groupUnits(desired, models)
	sharedKeys := map[string]string{}
	for unitID, unit := range desired.Units {
		if models[unit.Adapter] != installplan.EffectShared {
			continue
		}
		key := installplan.SharedUnitKey(unit.Adapter, unit.Scope, unit.NativeName)
		if other, ok := sharedKeys[key]; ok {
			return Plan{}, fmt.Errorf("shared removal units %q and %q have the same provider identity", other, unitID)
		}
		sharedKeys[key] = unitID
	}
	plan := Plan{LockDigest: digest, Target: desired.Target, Installation: installation, ReceiptRemove: true}
	for _, groupID := range sortedKeys(groups) {
		group := groups[groupID]
		ordered := removalOrder(desired, group.ids)
		planned := Group{ID: groupID, Adapter: group.adapter, Scope: group.scope, EffectModel: group.model}
		removeIsolated := group.model == installplan.EffectIsolated
		if removeIsolated {
			for _, unitID := range ordered {
				if previous.Units[unitID].Ownership != installplan.OwnershipTemperAdded {
					removeIsolated = false
					break
				}
			}
		}
		for _, unitID := range ordered {
			locked := desired.Units[unitID]
			recorded := previous.Units[unitID]
			actual := observed.Units[unitID]
			if actual.Present && actual.Location != recorded.Location {
				return Plan{}, fmt.Errorf("software removal unit %q location differs from its receipt", unitID)
			}
			unit := Unit{ID: unitID, Action: ActionPreserve, Ownership: recorded.Ownership, Location: recorded.Location, SharedClaim: recorded.SharedClaim}
			if group.model == installplan.EffectIsolated {
				if removeIsolated {
					unit.Action = ActionRemove
					unit.Execute = actual.Present
				}
				planned.Units = append(planned.Units, unit)
				continue
			}
			shared, ok := state.Shared.Units[recorded.SharedClaim]
			if !ok || shared.Lifecycle != installplan.SharedActive || !matchesShared(shared, locked, recorded) {
				return Plan{}, fmt.Errorf("shared removal unit %q disagrees with active root-state authority", unitID)
			}
			claim, ok := shared.Claims[installation.ID]
			if !ok || claim.Status != installplan.ClaimActive || claim.SoftwareLockDigest != digest || claim.UnitID != unitID {
				return Plan{}, fmt.Errorf("shared removal unit %q has no matching active claim", unitID)
			}
			if len(shared.Claims) > 1 {
				if !actual.Present {
					return Plan{}, fmt.Errorf("shared removal unit %q is missing while other claims remain", unitID)
				}
				unit.RequirePresent = true
			} else if shared.Acquisition == installplan.OwnershipTemperAdded {
				unit.Action = ActionRemove
				unit.Execute = actual.Present
				unit.RetireShared = true
			}
			planned.Units = append(planned.Units, unit)
		}
		plan.Groups = append(plan.Groups, planned)
	}
	return plan, nil
}

func buildPrepared(desired softwarelock.Document, installation installplan.Installation, digest string, effectModels map[string]installplan.EffectModel, observed *installplan.Observation, state State) (Plan, error) {
	prepared := state.Prepared
	if prepared.LockDigest != digest || prepared.Target != desired.Target || prepared.Installation != installation {
		return Plan{}, errors.New("prepared removal belongs to another lock, target, root, or installation")
	}
	if observed == nil {
		return Plan{}, errors.New("prepared software removal requires provider observation")
	}
	if err := validateObservation(desired, installation, *observed); err != nil {
		return Plan{}, err
	}
	models, err := validateEffectModels(desired, effectModels)
	if err != nil {
		return Plan{}, err
	}
	seen := map[string]bool{}
	plan := Plan{LockDigest: digest, Target: desired.Target, Installation: installation, ReceiptRemove: true, Recovering: true}
	for _, groupID := range sortedKeys(prepared.Groups) {
		intent := prepared.Groups[groupID]
		if intent.ID != groupID || intent.Adapter+":"+intent.Scope != groupID || models[intent.Adapter] != intent.EffectModel {
			return Plan{}, fmt.Errorf("prepared removal group %q disagrees with compiled adapter semantics", groupID)
		}
		group := Group{ID: groupID, Adapter: intent.Adapter, Scope: intent.Scope, EffectModel: intent.EffectModel}
		ids := make([]string, 0, len(intent.Units))
		for unitID := range intent.Units {
			ids = append(ids, unitID)
		}
		for _, unitID := range removalOrder(desired, ids) {
			locked, ok := desired.Units[unitID]
			if !ok || locked.Adapter != intent.Adapter || locked.Scope != intent.Scope || seen[unitID] {
				return Plan{}, fmt.Errorf("prepared removal contains invalid unit %q", unitID)
			}
			seen[unitID] = true
			stored := intent.Units[unitID]
			actual := observed.Units[unitID]
			if actual.Present && actual.Location != stored.Location {
				return Plan{}, fmt.Errorf("prepared software removal unit %q location drifted", unitID)
			}
			unit := Unit{
				ID: unitID, Action: ActionPreserve, Ownership: stored.Ownership, Location: stored.Location,
				SharedClaim: stored.SharedClaim, RetireShared: stored.RetireShared,
			}
			if stored.RemoveProvider {
				unit.Action = ActionRemove
				unit.Execute = actual.Present
			}
			if stored.SharedClaim != "" && !stored.RetireShared {
				if shared, ok := state.Shared.Units[stored.SharedClaim]; ok && len(shared.Claims) > 0 {
					unit.RequirePresent = true
				}
			}
			if stored.RetireShared {
				shared, ok := state.Shared.Units[stored.SharedClaim]
				if !ok || shared.Lifecycle != installplan.SharedRetiring || len(shared.Claims) != 0 {
					return Plan{}, fmt.Errorf("prepared removal unit %q lost its retiring shared authority", unitID)
				}
			}
			if unit.RequirePresent && !actual.Present {
				return Plan{}, fmt.Errorf("prepared shared removal unit %q is missing while other claims remain", unitID)
			}
			group.Units = append(group.Units, unit)
		}
		plan.Groups = append(plan.Groups, group)
	}
	if len(seen) != len(desired.Units) {
		return Plan{}, errors.New("prepared removal does not cover the complete software lock")
	}
	return plan, nil
}

// VerifyPostState proves that every prepared provider action reached its
// absolute postcondition before the receipt and retiring authority are removed.
func VerifyPostState(desired softwarelock.Document, plan Plan, observed installplan.Observation) error {
	if err := validateObservation(desired, plan.Installation, observed); err != nil {
		return err
	}
	for _, group := range plan.Groups {
		for _, unit := range group.Units {
			actual := observed.Units[unit.ID]
			switch {
			case unit.Action == ActionRemove && actual.Present:
				return fmt.Errorf("removed software unit %q is still present", unit.ID)
			case unit.Action == ActionPreserve && unit.RequirePresent && !actual.Present:
				return fmt.Errorf("preserved shared software unit %q disappeared", unit.ID)
			case actual.Present && (!installplan.MatchesLock(desired.Units[unit.ID], actual) || actual.Location != unit.Location):
				return fmt.Errorf("software removal unit %q drifted during provider effects", unit.ID)
			}
		}
	}
	return nil
}

type pendingGroup struct {
	adapter string
	scope   string
	model   installplan.EffectModel
	ids     []string
}

func groupUnits(desired softwarelock.Document, models map[string]installplan.EffectModel) map[string]*pendingGroup {
	groups := map[string]*pendingGroup{}
	for unitID, unit := range desired.Units {
		id := unit.Adapter + ":" + unit.Scope
		if groups[id] == nil {
			groups[id] = &pendingGroup{adapter: unit.Adapter, scope: unit.Scope, model: models[unit.Adapter]}
		}
		groups[id].ids = append(groups[id].ids, unitID)
	}
	return groups
}

func validateEffectModels(desired softwarelock.Document, models map[string]installplan.EffectModel) (map[string]installplan.EffectModel, error) {
	for _, unit := range desired.Units {
		model, ok := models[unit.Adapter]
		if !ok || (model != installplan.EffectShared && model != installplan.EffectIsolated) {
			return nil, fmt.Errorf("adapter %q has no valid compiled effect model", unit.Adapter)
		}
	}
	return models, nil
}

func validateObservation(desired softwarelock.Document, installation installplan.Installation, observed installplan.Observation) error {
	if observed.Target != desired.Target || observed.Root != installation.Root {
		return errors.New("software removal observation differs from lock target or requested root")
	}
	for unitID, locked := range desired.Units {
		actual, ok := observed.Units[unitID]
		if !ok {
			return fmt.Errorf("software removal observation omits unit %q", unitID)
		}
		if actual.Present {
			if !installplan.MatchesLock(locked, actual) {
				return fmt.Errorf("software removal unit %q has a non-exact provider identity", unitID)
			}
			if !absoluteClean(actual.Location) {
				return fmt.Errorf("software removal unit %q has an invalid provider location", unitID)
			}
		} else if actual.Adapter != "" || actual.Scope != "" || actual.NativeName != "" || actual.Version != "" || actual.Revision != "" || len(actual.Dependencies) != 0 || len(actual.Artifacts) != 0 || actual.Location != "" {
			return fmt.Errorf("absent software removal observation %q carries provider identity", unitID)
		}
	}
	if len(observed.Units) != len(desired.Units) {
		return errors.New("software removal observation contains unexpected units")
	}
	return nil
}

func validateInstallation(installation installplan.Installation) error {
	if !installationIDPattern.MatchString(installation.ID) || !absoluteClean(installation.Root) {
		return errors.New("software removal requires a lowercase installation id and absolute clean root")
	}
	return nil
}

func absoluteClean(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path && filepath.Dir(path) != path
}

func matchesShared(shared installplan.SharedUnit, locked softwarelock.Unit, recorded receipt.Unit) bool {
	observed := installplan.ObservedUnit{
		Present: true, Adapter: shared.Adapter, Scope: shared.Scope, NativeName: shared.NativeName,
		Version: shared.Version, Revision: shared.Revision, Dependencies: shared.Dependencies,
		Artifacts: shared.Artifacts, Location: shared.Location,
	}
	return installplan.MatchesLock(locked, observed) && shared.Location == recorded.Location && shared.Acquisition == recorded.Ownership
}

func claimantUnit(shared installplan.SharedState, installationID string) string {
	for _, key := range sortedKeys(shared.Units) {
		if claim, ok := shared.Units[key].Claims[installationID]; ok {
			return claim.UnitID
		}
	}
	return ""
}

// removalOrder is reverse topological order: dependants are removed before
// the dependencies they reference.
func removalOrder(desired softwarelock.Document, ids []string) []string {
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	seen := map[string]bool{}
	ordered := make([]string, 0, len(ids))
	var visit func(string)
	visit = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		dependencies := append([]string(nil), desired.Units[id].Dependencies...)
		sort.Strings(dependencies)
		for _, dependency := range dependencies {
			if wanted[dependency] {
				visit(dependency)
			}
		}
		ordered = append(ordered, id)
	}
	canonical := append([]string(nil), ids...)
	sort.Strings(canonical)
	for _, id := range canonical {
		visit(id)
	}
	for left, right := 0, len(ordered)-1; left < right; left, right = left+1, right-1 {
		ordered[left], ordered[right] = ordered[right], ordered[left]
	}
	return ordered
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
