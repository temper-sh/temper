// Package installplan computes provider-neutral software installation plans.
// It is pure: adapters inspect before this boundary and execute only the
// complete validated groups returned from it.
package installplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/temper-sh/temper/internal/software"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
)

var (
	installationIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	sha256Pattern         = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type EffectModel string

const (
	EffectShared   EffectModel = "shared"
	EffectIsolated EffectModel = "isolated"
)

type Ownership string

const (
	OwnershipTemperAdded Ownership = "temper-added"
	OwnershipPreExisting Ownership = "pre-existing"
)

type Action string

const (
	ActionPreserve Action = "preserve"
	ActionAdd      Action = "add"
	ActionReplace  Action = "replace"
)

// ObservedUnit is the normalized provider state for one desired lock unit.
// An absent unit has Present false and no other fields. Provider-native values
// must be translated before entering the pure planner.
type ObservedUnit struct {
	Present      bool
	Adapter      string
	Scope        string
	NativeName   string
	Version      string
	Revision     string
	Dependencies []string
	Artifacts    []software.Artifact
	Location     string
	// InstallLocation is the adapter's absolute destination when the unit is
	// absent. It is planning input, not a claim that provider state exists.
	InstallLocation string
}

type Observation struct {
	Target software.Target
	Root   string
	Units  map[string]ObservedUnit
}

type Installation struct {
	ID   string
	Root string
}

// Previous is the planner-facing provenance projection of a canonical receipt.
// Receipt parsing and exact lock validation remain a separate persistent-schema
// boundary.
type Previous struct {
	LockDigest   string
	Target       software.Target
	Installation Installation
	Units        map[string]PreviousUnit
}

type PreviousUnit struct {
	Ownership   Ownership
	SharedClaim string
}

type Before string

const (
	BeforeAbsent   Before = "absent"
	BeforeExact    Before = "exact"
	BeforeNonExact Before = "non-exact"
)

type PreparedUnit struct {
	Before         Before
	OwnershipAfter Ownership
	SharedClaim    string
}

// Prepared is the planner-facing projection of an immutable prepared-operation
// record. It lets a restart distinguish software installed by the interrupted
// operation from software that existed before Temper began.
type Prepared struct {
	LockDigest   string
	Target       software.Target
	Installation Installation
	Units        map[string]PreparedUnit
}

type ClaimStatus string

const (
	ClaimPrepared ClaimStatus = "prepared"
	ClaimActive   ClaimStatus = "active"
)

type SharedClaim struct {
	SoftwareLockDigest string
	UnitID             string
	Status             ClaimStatus
}

type SharedLifecycle string

const (
	SharedActive   SharedLifecycle = "active"
	SharedRetiring SharedLifecycle = "retiring"
)

// SharedUnit is the root-wide current ownership fact for one provider-native
// shared unit. Receipts snapshot claims; this registry arbitrates removal.
type SharedUnit struct {
	Adapter      string
	Scope        string
	NativeName   string
	Version      string
	Revision     string
	Dependencies []string
	Artifacts    []software.Artifact
	Location     string
	Acquisition  Ownership
	Lifecycle    SharedLifecycle
	Claims       map[string]SharedClaim
}

type SharedState struct {
	Root  string
	Units map[string]SharedUnit
}

type SatisfiedRequirement struct {
	SoftwareLockDigest string
	InstallationID     string
	ReceiptSHA256      string
}

type State struct {
	Previous     *Previous
	Prepared     *Prepared
	Removing     bool
	Shared       SharedState
	Requirements []SatisfiedRequirement
}

type ClaimAction string

const (
	ClaimNone     ClaimAction = ""
	ClaimPreserve ClaimAction = "preserve"
	ClaimAdd      ClaimAction = "add"
	ClaimActivate ClaimAction = "activate"
)

type Unit struct {
	ID          string
	Action      Action
	Ownership   Ownership
	Location    string
	SharedClaim string
	ClaimAction ClaimAction
}

type Group struct {
	ID          string
	Adapter     string
	Scope       string
	EffectModel EffectModel
	Units       []Unit
}

func (g Group) ChangesProvider() bool {
	for _, unit := range g.Units {
		if unit.Action != ActionPreserve {
			return true
		}
	}
	return false
}

type Plan struct {
	LockDigest   string
	Target       software.Target
	Installation Installation
	Requirements []SatisfiedRequirement
	Groups       []Group
	ReceiptWrite bool
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

func (p Plan) Changed() bool { return p.ReceiptWrite || p.EffectCount() > 0 || p.ClaimWriteCount() > 0 }

func (p Plan) ClaimWriteCount() int {
	count := 0
	for _, group := range p.Groups {
		for _, unit := range group.Units {
			if unit.ClaimAction == ClaimAdd || unit.ClaimAction == ClaimActivate {
				count++
			}
		}
	}
	return count
}

// Digest identifies the complete provider-neutral plan. Build orders every
// slice deterministically, so canonical JSON is stable across map iteration.
func (p Plan) Digest() string {
	encoded, err := json.Marshal(p)
	if err != nil {
		panic(fmt.Sprintf("encode software installation plan: %v", err))
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// Build computes the complete installation plan without invoking a provider or
// writing state. effectModels is the compiled adapter descriptor projection;
// catalog data cannot choose effect semantics.
func Build(desired softwarelock.Document, installation Installation, effectModels map[string]EffectModel, observed Observation, state State) (Plan, error) {
	if err := desired.Validate(); err != nil {
		return Plan{}, err
	}
	if err := validateInstallation(installation); err != nil {
		return Plan{}, err
	}
	if state.Removing {
		return Plan{}, errors.New("installation has a prepared software removal")
	}
	if observed.Target != desired.Target {
		return Plan{}, errors.New("software observation target differs from lock target")
	}
	if observed.Root != installation.Root {
		return Plan{}, errors.New("software observation root differs from requested root")
	}
	if err := validateObservation(desired, observed); err != nil {
		return Plan{}, err
	}

	lockDigest, err := desired.SemanticDigest()
	if err != nil {
		return Plan{}, err
	}

	type pendingGroup struct {
		adapter string
		scope   string
		model   EffectModel
		ids     []string
	}
	groups := map[string]*pendingGroup{}
	sharedKeys := map[string]string{}
	for unitID, unit := range desired.Units {
		model, ok := effectModels[unit.Adapter]
		if !ok {
			return Plan{}, fmt.Errorf("adapter %q has no compiled effect model", unit.Adapter)
		}
		if model != EffectShared && model != EffectIsolated {
			return Plan{}, fmt.Errorf("adapter %q has invalid effect model %q", unit.Adapter, model)
		}
		if model == EffectShared {
			key := SharedUnitKey(unit.Adapter, unit.Scope, unit.NativeName)
			if other, exists := sharedKeys[key]; exists {
				return Plan{}, fmt.Errorf("shared installation units %q and %q have the same provider identity", other, unitID)
			}
			sharedKeys[key] = unitID
		}
		groupID := unit.Adapter + ":" + unit.Scope
		group := groups[groupID]
		if group == nil {
			group = &pendingGroup{adapter: unit.Adapter, scope: unit.Scope, model: model}
			groups[groupID] = group
		} else if group.model != model {
			return Plan{}, fmt.Errorf("installation group %q has conflicting effect models", groupID)
		}
		group.ids = append(group.ids, unitID)
	}
	if err := validateIsolatedLocations(desired, installation, effectModels, observed); err != nil {
		return Plan{}, err
	}
	if err := validatePrevious(desired, installation, lockDigest, effectModels, state.Previous); err != nil {
		return Plan{}, err
	}
	if err := validatePrepared(desired, installation, lockDigest, effectModels, state.Previous, state.Prepared); err != nil {
		return Plan{}, err
	}
	if err := validateSharedState(desired, installation, lockDigest, effectModels, observed, state); err != nil {
		return Plan{}, err
	}
	requirements, err := validateRequirements(desired, state.Requirements)
	if err != nil {
		return Plan{}, err
	}

	groupIDs := sortedKeys(groups)
	plan := Plan{LockDigest: lockDigest, Target: desired.Target, Installation: installation, Requirements: requirements, ReceiptWrite: state.Previous == nil}
	for _, groupID := range groupIDs {
		pending := groups[groupID]
		ordered := dependencyOrder(desired, pending.ids)
		group, changed, err := planGroup(desired, observed, installation, state, lockDigest, groupID, pending.adapter, pending.scope, pending.model, ordered)
		if err != nil {
			return Plan{}, err
		}
		if changed {
			plan.ReceiptWrite = true
		}
		plan.Groups = append(plan.Groups, group)
	}
	return plan, nil
}

func planGroup(desired softwarelock.Document, observed Observation, installation Installation, state State, lockDigest, groupID, adapter, scope string, model EffectModel, ordered []string) (Group, bool, error) {
	group, changed, err := planProviderGroup(desired, observed, state.Previous, state.Prepared, groupID, adapter, scope, model, ordered)
	if err != nil || model != EffectShared {
		return group, changed, err
	}
	if err := applySharedClaims(&group, desired, observed, installation, state, lockDigest); err != nil {
		return Group{}, false, err
	}
	return group, changed, nil
}

func planProviderGroup(desired softwarelock.Document, observed Observation, previous *Previous, prepared *Prepared, groupID, adapter, scope string, model EffectModel, ordered []string) (Group, bool, error) {
	group := Group{ID: groupID, Adapter: adapter, Scope: scope, EffectModel: model}
	exact := make(map[string]bool, len(ordered))
	present := 0
	for _, unitID := range ordered {
		actual := observed.Units[unitID]
		exact[unitID] = actual.Present && sameUnit(desired.Units[unitID], actual)
		if actual.Present {
			present++
		}
	}

	if prepared != nil {
		return planPreparedGroup(group, observed, prepared, exact, ordered)
	}

	if previous == nil {
		if model == EffectIsolated && present != 0 && present != len(ordered) {
			return Group{}, false, fmt.Errorf("installation group %q is a partial unreceipted isolated environment", groupID)
		}
		for _, unitID := range ordered {
			actual := observed.Units[unitID]
			switch {
			case exact[unitID]:
				group.Units = append(group.Units, plannedUnit(unitID, ActionPreserve, OwnershipPreExisting, actual))
			case !actual.Present:
				group.Units = append(group.Units, plannedUnit(unitID, ActionAdd, OwnershipTemperAdded, actual))
			default:
				return Group{}, false, fmt.Errorf("installation unit %q is present with an unreceipted non-exact identity", unitID)
			}
		}
		return group, group.ChangesProvider(), nil
	}

	allExact := true
	for _, unitID := range ordered {
		if !exact[unitID] {
			allExact = false
			break
		}
	}
	if allExact {
		for _, unitID := range ordered {
			group.Units = append(group.Units, plannedUnit(unitID, ActionPreserve, previous.Units[unitID].Ownership, observed.Units[unitID]))
		}
		return group, false, nil
	}

	if model == EffectIsolated {
		for _, unitID := range ordered {
			if previous.Units[unitID].Ownership == OwnershipPreExisting {
				return Group{}, false, fmt.Errorf("installation group %q cannot replace a receipted pre-existing isolated unit", groupID)
			}
		}
		for _, unitID := range ordered {
			action := ActionReplace
			if !observed.Units[unitID].Present {
				action = ActionAdd
			}
			group.Units = append(group.Units, plannedUnit(unitID, action, OwnershipTemperAdded, observed.Units[unitID]))
		}
		return group, true, nil
	}

	for _, unitID := range ordered {
		actual := observed.Units[unitID]
		switch {
		case exact[unitID]:
			group.Units = append(group.Units, plannedUnit(unitID, ActionPreserve, previous.Units[unitID].Ownership, actual))
		case !actual.Present:
			group.Units = append(group.Units, plannedUnit(unitID, ActionAdd, OwnershipTemperAdded, actual))
		default:
			return Group{}, false, fmt.Errorf("shared installation unit %q drifted from the exact lock identity", unitID)
		}
	}
	return group, group.ChangesProvider(), nil
}

func planPreparedGroup(group Group, observed Observation, prepared *Prepared, exact map[string]bool, ordered []string) (Group, bool, error) {
	if group.EffectModel == EffectShared {
		for _, unitID := range ordered {
			actual := observed.Units[unitID]
			intent := prepared.Units[unitID]
			switch {
			case exact[unitID]:
				group.Units = append(group.Units, plannedUnit(unitID, ActionPreserve, intent.OwnershipAfter, actual))
			case !actual.Present && intent.Before == BeforeAbsent:
				group.Units = append(group.Units, plannedUnit(unitID, ActionAdd, intent.OwnershipAfter, actual))
			case !actual.Present:
				return Group{}, false, fmt.Errorf("prepared shared installation unit %q disappeared after being preserved", unitID)
			default:
				return Group{}, false, fmt.Errorf("prepared shared installation unit %q has a non-exact identity", unitID)
			}
		}
		return group, group.ChangesProvider(), nil
	}

	mutable := false
	allExact := true
	for _, unitID := range ordered {
		if prepared.Units[unitID].Before != BeforeExact {
			mutable = true
		}
		if !exact[unitID] {
			allExact = false
		}
	}
	if !mutable {
		if !allExact {
			return Group{}, false, fmt.Errorf("prepared isolated installation group %q drifted after being preserved", group.ID)
		}
		for _, unitID := range ordered {
			group.Units = append(group.Units, plannedUnit(unitID, ActionPreserve, prepared.Units[unitID].OwnershipAfter, observed.Units[unitID]))
		}
		return group, false, nil
	}
	if allExact {
		for _, unitID := range ordered {
			group.Units = append(group.Units, plannedUnit(unitID, ActionPreserve, prepared.Units[unitID].OwnershipAfter, observed.Units[unitID]))
		}
		return group, false, nil
	}
	for _, unitID := range ordered {
		action := ActionReplace
		if !observed.Units[unitID].Present {
			action = ActionAdd
		}
		group.Units = append(group.Units, plannedUnit(unitID, action, prepared.Units[unitID].OwnershipAfter, observed.Units[unitID]))
	}
	return group, true, nil
}

func applySharedClaims(group *Group, desired softwarelock.Document, observed Observation, installation Installation, state State, lockDigest string) error {
	for index := range group.Units {
		planned := &group.Units[index]
		desiredUnit := desired.Units[planned.ID]
		key := SharedUnitKey(desiredUnit.Adapter, desiredUnit.Scope, desiredUnit.NativeName)
		planned.SharedClaim = key

		registered, exists := state.Shared.Units[key]
		if !exists {
			if state.Previous != nil || state.Prepared != nil {
				return fmt.Errorf("shared installation unit %q has provenance but no root-wide claim record", planned.ID)
			}
			planned.ClaimAction = ClaimAdd
			continue
		}
		planned.Ownership = registered.Acquisition
		claim, claimed := registered.Claims[installation.ID]
		if !claimed {
			if state.Previous != nil || state.Prepared != nil {
				return fmt.Errorf("shared installation unit %q has no claim for installation %q", planned.ID, installation.ID)
			}
			planned.ClaimAction = ClaimAdd
			continue
		}
		if claim.SoftwareLockDigest != lockDigest || claim.UnitID != planned.ID {
			return fmt.Errorf("shared installation unit %q claim belongs to another lock or unit", planned.ID)
		}
		switch claim.Status {
		case ClaimPrepared:
			planned.ClaimAction = ClaimActivate
		case ClaimActive:
			planned.ClaimAction = ClaimPreserve
		default:
			return fmt.Errorf("shared installation unit %q claim has invalid status %q", planned.ID, claim.Status)
		}
		if observed.Units[planned.ID].Present && observed.Units[planned.ID].Location != registered.Location {
			return fmt.Errorf("shared installation unit %q location differs from its claim record", planned.ID)
		}
	}
	return nil
}

// SharedUnitKey is the stable root-wide identity for one provider-native
// shared package. Length-safe JSON avoids delimiter collisions in names.
func SharedUnitKey(adapter, scope, nativeName string) string {
	encoded, err := json.Marshal(struct {
		Adapter    string `json:"adapter"`
		Scope      string `json:"scope"`
		NativeName string `json:"native_name"`
	}{Adapter: adapter, Scope: scope, NativeName: nativeName})
	if err != nil {
		panic(fmt.Sprintf("encode shared unit key: %v", err))
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// MatchesLock reports whether one provider observation has the exact
// provider-native identity and closure frozen by a lock unit. Presence and
// location remain separate facts for the caller to evaluate.
func MatchesLock(locked softwarelock.Unit, observed ObservedUnit) bool {
	if locked.Adapter != observed.Adapter || locked.Scope != observed.Scope || locked.NativeName != observed.NativeName || locked.Version != observed.Version || locked.Revision != observed.Revision {
		return false
	}
	if !sameStrings(locked.Dependencies, observed.Dependencies) {
		return false
	}
	return sameArtifacts(locked.Artifacts, observed.Artifacts)
}

func plannedUnit(id string, action Action, ownership Ownership, observed ObservedUnit) Unit {
	location := observed.Location
	if !observed.Present {
		location = observed.InstallLocation
	}
	return Unit{ID: id, Action: action, Ownership: ownership, Location: location}
}

func validateObservation(desired softwarelock.Document, observed Observation) error {
	if err := validateRoot(observed.Root); err != nil {
		return fmt.Errorf("software observation: %w", err)
	}
	for unitID := range desired.Units {
		actual, ok := observed.Units[unitID]
		if !ok {
			return fmt.Errorf("software observation omits unit %q", unitID)
		}
		if !actual.Present && !emptyObservedIdentity(actual) {
			return fmt.Errorf("absent software observation %q carries identity fields", unitID)
		}
		if actual.Present {
			if err := validateRoot(actual.Location); err != nil {
				return fmt.Errorf("software observation unit %q location: %w", unitID, err)
			}
			if actual.InstallLocation != "" && actual.InstallLocation != actual.Location {
				return fmt.Errorf("software observation unit %q install location differs from observed location", unitID)
			}
		} else if actual.InstallLocation != "" {
			if err := validateRoot(actual.InstallLocation); err != nil {
				return fmt.Errorf("software observation unit %q install location: %w", unitID, err)
			}
		}
	}
	for unitID := range observed.Units {
		if _, ok := desired.Units[unitID]; !ok {
			return fmt.Errorf("software observation contains unexpected unit %q", unitID)
		}
	}
	return nil
}

func validatePrevious(desired softwarelock.Document, installation Installation, lockDigest string, effectModels map[string]EffectModel, previous *Previous) error {
	if previous == nil {
		return nil
	}
	if previous.LockDigest != lockDigest {
		return errors.New("installation receipt is bound to a different software lock")
	}
	if previous.Target != desired.Target {
		return errors.New("installation receipt target differs from lock target")
	}
	if previous.Installation != installation {
		return errors.New("installation receipt identity differs from requested installation")
	}
	for unitID, desiredUnit := range desired.Units {
		provenance, ok := previous.Units[unitID]
		if !ok {
			return fmt.Errorf("installation receipt omits unit %q", unitID)
		}
		if provenance.Ownership != OwnershipTemperAdded && provenance.Ownership != OwnershipPreExisting {
			return fmt.Errorf("installation receipt unit %q has invalid ownership %q", unitID, provenance.Ownership)
		}
		if effectModels[desiredUnit.Adapter] == EffectShared {
			want := SharedUnitKey(desiredUnit.Adapter, desiredUnit.Scope, desiredUnit.NativeName)
			if provenance.SharedClaim != want {
				return fmt.Errorf("installation receipt unit %q has shared claim %q, want %q", unitID, provenance.SharedClaim, want)
			}
		} else if provenance.SharedClaim != "" {
			return fmt.Errorf("installation receipt isolated unit %q carries a shared claim", unitID)
		}
	}
	for unitID := range previous.Units {
		if _, ok := desired.Units[unitID]; !ok {
			return fmt.Errorf("installation receipt contains unexpected unit %q", unitID)
		}
	}
	return nil
}

func validatePrepared(desired softwarelock.Document, installation Installation, lockDigest string, effectModels map[string]EffectModel, previous *Previous, prepared *Prepared) error {
	if prepared == nil {
		return nil
	}
	if prepared.LockDigest != lockDigest {
		return errors.New("prepared installation is bound to a different software lock")
	}
	if prepared.Target != desired.Target {
		return errors.New("prepared installation target differs from lock target")
	}
	if prepared.Installation != installation {
		return errors.New("prepared installation identity differs from requested installation")
	}
	hasPreparedWork := false
	for unitID, desiredUnit := range desired.Units {
		intent, ok := prepared.Units[unitID]
		if !ok {
			return fmt.Errorf("prepared installation omits unit %q", unitID)
		}
		if intent.Before != BeforeAbsent && intent.Before != BeforeExact && intent.Before != BeforeNonExact {
			return fmt.Errorf("prepared installation unit %q has invalid before state %q", unitID, intent.Before)
		}
		if intent.OwnershipAfter != OwnershipTemperAdded && intent.OwnershipAfter != OwnershipPreExisting {
			return fmt.Errorf("prepared installation unit %q has invalid ownership %q", unitID, intent.OwnershipAfter)
		}
		model := effectModels[desiredUnit.Adapter]
		if intent.Before != BeforeExact || model == EffectShared {
			hasPreparedWork = true
		}
		if model == EffectShared {
			want := SharedUnitKey(desiredUnit.Adapter, desiredUnit.Scope, desiredUnit.NativeName)
			if intent.SharedClaim != want {
				return fmt.Errorf("prepared installation unit %q has shared claim %q, want %q", unitID, intent.SharedClaim, want)
			}
		} else if intent.SharedClaim != "" {
			return fmt.Errorf("prepared isolated installation unit %q carries a shared claim", unitID)
		}
		if intent.Before == BeforeNonExact && model != EffectIsolated {
			return fmt.Errorf("prepared shared installation unit %q cannot have a non-exact before state", unitID)
		}
		if previous == nil {
			if intent.Before == BeforeNonExact || (model == EffectIsolated && intent.Before == BeforeExact && intent.OwnershipAfter != OwnershipPreExisting) || (intent.Before == BeforeAbsent && intent.OwnershipAfter != OwnershipTemperAdded) {
				return fmt.Errorf("prepared fresh installation unit %q has inconsistent provenance", unitID)
			}
			continue
		}
		switch intent.Before {
		case BeforeExact:
			if intent.OwnershipAfter != previous.Units[unitID].Ownership {
				return fmt.Errorf("prepared installation unit %q changes preserved ownership", unitID)
			}
		case BeforeAbsent:
			if intent.OwnershipAfter != OwnershipTemperAdded {
				return fmt.Errorf("prepared installation unit %q does not own its repaired installation", unitID)
			}
		case BeforeNonExact:
			if previous.Units[unitID].Ownership != OwnershipTemperAdded || intent.OwnershipAfter != OwnershipTemperAdded {
				return fmt.Errorf("prepared installation unit %q cannot replace pre-existing software", unitID)
			}
		}
	}
	for unitID := range prepared.Units {
		if _, ok := desired.Units[unitID]; !ok {
			return fmt.Errorf("prepared installation contains unexpected unit %q", unitID)
		}
	}
	if !hasPreparedWork {
		return errors.New("prepared installation contains no provider or shared-claim work")
	}

	// Any mutable isolated group is published as a whole and therefore must be
	// wholly Temper-owned after the operation.
	groupMutable := map[string]bool{}
	for unitID, desiredUnit := range desired.Units {
		if effectModels[desiredUnit.Adapter] == EffectIsolated && prepared.Units[unitID].Before != BeforeExact {
			groupMutable[desiredUnit.Adapter+":"+desiredUnit.Scope] = true
		}
	}
	for unitID, desiredUnit := range desired.Units {
		if groupMutable[desiredUnit.Adapter+":"+desiredUnit.Scope] && prepared.Units[unitID].OwnershipAfter != OwnershipTemperAdded {
			return fmt.Errorf("prepared isolated installation group %q is not wholly Temper-owned", desiredUnit.Adapter+":"+desiredUnit.Scope)
		}
	}
	return nil
}

func validateSharedState(desired softwarelock.Document, installation Installation, lockDigest string, effectModels map[string]EffectModel, observed Observation, state State) error {
	hasSharedDesired := false
	for _, unit := range desired.Units {
		if effectModels[unit.Adapter] == EffectShared {
			hasSharedDesired = true
			break
		}
	}
	if hasSharedDesired || len(state.Shared.Units) > 0 {
		if state.Shared.Root != installation.Root {
			return errors.New("root-wide shared-claim state root differs from requested root")
		}
	} else if state.Shared.Root != "" && state.Shared.Root != installation.Root {
		return errors.New("root-wide shared-claim state root differs from requested root")
	}

	for key, registered := range state.Shared.Units {
		if key != SharedUnitKey(registered.Adapter, registered.Scope, registered.NativeName) {
			return fmt.Errorf("shared-claim record %q does not match its provider identity", key)
		}
		if strings.TrimSpace(registered.Adapter) == "" || strings.TrimSpace(registered.Scope) == "" || strings.TrimSpace(registered.NativeName) == "" || strings.TrimSpace(registered.Version) == "" {
			return fmt.Errorf("shared-claim record %q has an incomplete provider identity", key)
		}
		if err := validateRoot(registered.Location); err != nil {
			return fmt.Errorf("shared-claim record %q location: %w", key, err)
		}
		if registered.Acquisition != OwnershipTemperAdded && registered.Acquisition != OwnershipPreExisting {
			return fmt.Errorf("shared-claim record %q has invalid acquisition %q", key, registered.Acquisition)
		}
		if registered.Lifecycle != SharedActive && registered.Lifecycle != SharedRetiring {
			return fmt.Errorf("shared-claim record %q has invalid lifecycle %q", key, registered.Lifecycle)
		}
		if registered.Lifecycle == SharedRetiring {
			if len(registered.Claims) != 0 {
				return fmt.Errorf("retiring shared-claim record %q still has installation claims", key)
			}
			continue
		}
		if len(registered.Claims) == 0 {
			return fmt.Errorf("active shared-claim record %q has no installation claims", key)
		}
		for claimant, claim := range registered.Claims {
			if !installationIDPattern.MatchString(claimant) {
				return fmt.Errorf("shared-claim record %q has invalid installation id %q", key, claimant)
			}
			if !sha256Pattern.MatchString(claim.SoftwareLockDigest) || strings.TrimSpace(claim.UnitID) == "" {
				return fmt.Errorf("shared-claim record %q installation %q has an invalid lock or unit identity", key, claimant)
			}
			if claim.Status != ClaimPrepared && claim.Status != ClaimActive {
				return fmt.Errorf("shared-claim record %q installation %q has invalid status %q", key, claimant, claim.Status)
			}
			if claimant == installation.ID {
				desiredUnit, ok := desired.Units[claim.UnitID]
				if !ok || effectModels[desiredUnit.Adapter] != EffectShared || SharedUnitKey(desiredUnit.Adapter, desiredUnit.Scope, desiredUnit.NativeName) != key {
					return fmt.Errorf("shared-claim record %q contains an unexpected claim for installation %q", key, claimant)
				}
				if claim.SoftwareLockDigest != lockDigest {
					return fmt.Errorf("shared-claim record %q installation %q belongs to another software lock", key, claimant)
				}
				if state.Previous == nil && state.Prepared == nil {
					return fmt.Errorf("shared-claim record %q has an unreceipted, unprepared claim for installation %q", key, claimant)
				}
			}
		}
	}

	for unitID, desiredUnit := range desired.Units {
		if effectModels[desiredUnit.Adapter] != EffectShared {
			continue
		}
		key := SharedUnitKey(desiredUnit.Adapter, desiredUnit.Scope, desiredUnit.NativeName)
		registered, exists := state.Shared.Units[key]
		if !exists {
			if state.Previous != nil || state.Prepared != nil {
				return fmt.Errorf("shared installation unit %q has provenance but no root-wide claim record", unitID)
			}
			continue
		}
		if registered.Lifecycle != SharedActive {
			return fmt.Errorf("shared installation unit %q is retiring and cannot accept a claim", unitID)
		}
		registeredObserved := ObservedUnit{
			Present: true, Adapter: registered.Adapter, Scope: registered.Scope, NativeName: registered.NativeName,
			Version: registered.Version, Revision: registered.Revision,
			Dependencies: registered.Dependencies, Artifacts: registered.Artifacts, Location: registered.Location,
		}
		if !sameUnit(desiredUnit, registeredObserved) {
			return fmt.Errorf("shared installation unit %q conflicts with the root-wide claimed identity", unitID)
		}
		claim, claimed := registered.Claims[installation.ID]
		actual := observed.Units[unitID]
		switch {
		case actual.Present && sameUnit(desiredUnit, actual) && actual.Location == registered.Location:
		case !actual.Present && registered.Acquisition == OwnershipTemperAdded && claimed:
			// A claimed package Temper originally added may be repaired. The
			// active/prepared claim keeps ownership unambiguous across roots.
		default:
			return fmt.Errorf("shared installation unit %q drifted from the root-wide claim record", unitID)
		}
		if state.Previous != nil {
			if !claimed || state.Previous.Units[unitID].SharedClaim != key || state.Previous.Units[unitID].Ownership != registered.Acquisition {
				return fmt.Errorf("installation receipt unit %q disagrees with the root-wide claim record", unitID)
			}
			if state.Prepared == nil && claim.Status != ClaimActive {
				return fmt.Errorf("installation receipt unit %q has a non-active root-wide claim", unitID)
			}
		}
		if state.Prepared != nil {
			if !claimed || state.Prepared.Units[unitID].SharedClaim != key || state.Prepared.Units[unitID].OwnershipAfter != registered.Acquisition {
				return fmt.Errorf("prepared installation unit %q disagrees with the root-wide claim record", unitID)
			}
			if state.Previous == nil && claim.Status != ClaimPrepared {
				return fmt.Errorf("prepared fresh installation unit %q does not have a prepared root-wide claim", unitID)
			}
		}
	}
	return nil
}

func validateRequirements(desired softwarelock.Document, satisfied []SatisfiedRequirement) ([]SatisfiedRequirement, error) {
	wanted := make(map[string]bool, len(desired.Requires))
	for _, requirement := range desired.Requires {
		wanted[requirement.SoftwareLockDigest] = true
	}
	seen := make(map[string]bool, len(satisfied))
	canonical := append([]SatisfiedRequirement(nil), satisfied...)
	for index, requirement := range canonical {
		if !sha256Pattern.MatchString(requirement.SoftwareLockDigest) || !sha256Pattern.MatchString(requirement.ReceiptSHA256) || !installationIDPattern.MatchString(requirement.InstallationID) {
			return nil, fmt.Errorf("satisfied requirement[%d] has an invalid lock, installation, or receipt identity", index)
		}
		if seen[requirement.SoftwareLockDigest] {
			return nil, fmt.Errorf("satisfied requirements repeat software lock digest %q", requirement.SoftwareLockDigest)
		}
		seen[requirement.SoftwareLockDigest] = true
		if !wanted[requirement.SoftwareLockDigest] {
			return nil, fmt.Errorf("satisfied software lock %q is not required by this installation", requirement.SoftwareLockDigest)
		}
	}
	for digest := range wanted {
		if !seen[digest] {
			return nil, fmt.Errorf("required base software lock %q has no verified installation receipt", digest)
		}
	}
	sort.Slice(canonical, func(i, j int) bool {
		return canonical[i].SoftwareLockDigest < canonical[j].SoftwareLockDigest
	})
	return canonical, nil
}

func validateInstallation(installation Installation) error {
	if !installationIDPattern.MatchString(installation.ID) {
		return fmt.Errorf("installation id %q is not a lowercase stable id", installation.ID)
	}
	return validateRoot(installation.Root)
}

func validateIsolatedLocations(desired softwarelock.Document, installation Installation, effectModels map[string]EffectModel, observed Observation) error {
	installationRoot := InstallationRoot(installation)
	for unitID, desiredUnit := range desired.Units {
		actual := observed.Units[unitID]
		if effectModels[desiredUnit.Adapter] != EffectIsolated {
			continue
		}
		location := actual.Location
		if !actual.Present {
			location = actual.InstallLocation
		}
		if location == "" {
			continue
		}
		relative, err := filepath.Rel(installationRoot, location)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			return fmt.Errorf("isolated installation unit %q location %q is outside installation root %q", unitID, location, installationRoot)
		}
	}
	return nil
}

func InstallationRoot(installation Installation) string {
	return filepath.Join(installation.Root, "software", "installations", installation.ID)
}

func validateRoot(value string) error {
	if value == "" || !filepath.IsAbs(value) || filepath.Clean(value) != value || filepath.Dir(value) == value {
		return fmt.Errorf("path %q must be absolute, clean, and narrower than a filesystem root", value)
	}
	return nil
}

func emptyObservedIdentity(unit ObservedUnit) bool {
	return unit.Adapter == "" && unit.Scope == "" && unit.NativeName == "" && unit.Version == "" && unit.Revision == "" && len(unit.Dependencies) == 0 && len(unit.Artifacts) == 0 && unit.Location == ""
}

func sameUnit(desired softwarelock.Unit, actual ObservedUnit) bool {
	return MatchesLock(desired, actual)
}

func sameStrings(left, right []string) bool {
	left = append([]string(nil), left...)
	right = append([]string(nil), right...)
	sort.Strings(left)
	sort.Strings(right)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sameArtifacts(left, right []software.Artifact) bool {
	left = append([]software.Artifact(nil), left...)
	right = append([]software.Artifact(nil), right...)
	sortArtifacts := func(values []software.Artifact) {
		sort.Slice(values, func(i, j int) bool {
			if values[i].Locator == values[j].Locator {
				return values[i].SHA256 < values[j].SHA256
			}
			return values[i].Locator < values[j].Locator
		})
	}
	sortArtifacts(left)
	sortArtifacts(right)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func dependencyOrder(desired softwarelock.Document, ids []string) []string {
	inGroup := make(map[string]bool, len(ids))
	for _, id := range ids {
		inGroup[id] = true
	}
	ids = append([]string(nil), ids...)
	sort.Strings(ids)
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
		for _, dependencyID := range dependencies {
			if inGroup[dependencyID] {
				visit(dependencyID)
			}
		}
		ordered = append(ordered, id)
	}
	for _, id := range ids {
		visit(id)
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
