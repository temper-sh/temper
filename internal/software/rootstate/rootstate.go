// Package rootstate owns the canonical root-wide authority for prepared
// software operations and shared-provider claims.
package rootstate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/installplan"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"gopkg.in/yaml.v3"
)

const SchemaV1 = "temper-software-state/v1"

var (
	idPattern     = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	unitIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._:-][a-z0-9]+)*$`)
	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

var ErrOperationBusy = errors.New("software installation operation is held by a live invocation")

type Document struct {
	Schema      string                `yaml:"schema"`
	Root        string                `yaml:"root"`
	Generation  uint64                `yaml:"generation"`
	Operations  map[string]Operation  `yaml:"operations"`
	SharedUnits map[string]SharedUnit `yaml:"shared_units"`
}

type Operation struct {
	Kind               string                    `yaml:"kind"`
	SoftwareLockDigest string                    `yaml:"software_lock_digest"`
	Target             software.Target           `yaml:"target"`
	PlanDigest         string                    `yaml:"plan_digest"`
	StartedAt          string                    `yaml:"started_at"`
	ClaimedBy          string                    `yaml:"claimed_by"`
	LeaseExpiresAt     string                    `yaml:"lease_expires_at"`
	Fence              uint64                    `yaml:"fence"`
	Groups             map[string]OperationGroup `yaml:"groups"`
}

type OperationGroup struct {
	Adapter     string                   `yaml:"adapter"`
	Scope       string                   `yaml:"scope"`
	EffectModel installplan.EffectModel  `yaml:"effect_model"`
	Units       map[string]OperationUnit `yaml:"units"`
}

type OperationUnit struct {
	Before          installplan.Before    `yaml:"before"`
	OwnershipAfter  installplan.Ownership `yaml:"ownership_after,omitempty"`
	OwnershipBefore installplan.Ownership `yaml:"ownership_before,omitempty"`
	Location        string                `yaml:"location,omitempty"`
	RemoveProvider  bool                  `yaml:"remove_provider,omitempty"`
	RetireShared    bool                  `yaml:"retire_shared,omitempty"`
	SharedClaim     string                `yaml:"shared_claim,omitempty"`
}

type SharedUnit struct {
	Adapter      string                             `yaml:"adapter"`
	Scope        string                             `yaml:"scope"`
	NativeName   string                             `yaml:"native_name"`
	Version      string                             `yaml:"version"`
	Revision     string                             `yaml:"revision,omitempty"`
	Dependencies []string                           `yaml:"dependencies"`
	Artifacts    []software.Artifact                `yaml:"artifacts,omitempty"`
	Location     string                             `yaml:"location"`
	Acquisition  installplan.Ownership              `yaml:"acquisition"`
	Lifecycle    installplan.SharedLifecycle        `yaml:"lifecycle"`
	Claims       map[string]installplan.SharedClaim `yaml:"claims"`
}

type Lease struct {
	InvocationID string
	Now          time.Time
	Duration     time.Duration
}

type ValidationError struct{ Problems []string }

func (e *ValidationError) Error() string {
	return "software root state invalid: " + strings.Join(e.Problems, "; ")
}

func Parse(data []byte) (Document, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var document Document
	if err := decoder.Decode(&document); err != nil {
		return Document{}, fmt.Errorf("decode software root state: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Document{}, errors.New("decode software root state: multiple YAML documents are not allowed")
		}
		return Document{}, fmt.Errorf("decode software root state: %w", err)
	}
	canonical, err := Marshal(document)
	if err != nil {
		return Document{}, err
	}
	if !bytes.Equal(data, canonical) {
		return Document{}, errors.New("software root state bytes are not canonical")
	}
	return document, nil
}

func Marshal(document Document) ([]byte, error) {
	if err := document.Validate(); err != nil {
		return nil, err
	}
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(document); err != nil {
		return nil, fmt.Errorf("encode software root state: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("close software root state encoder: %w", err)
	}
	return output.Bytes(), nil
}

func (d Document) Validate() error {
	var problems []string
	problem := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if d.Schema != SchemaV1 {
		problem("schema is %q, want %q", d.Schema, SchemaV1)
	}
	if err := validateAbsolutePath(d.Root); err != nil {
		problem("root: %v", err)
	}
	if d.Generation == 0 {
		problem("generation must be greater than zero")
	}
	if d.Operations == nil {
		problem("operations map is required")
	}
	if d.SharedUnits == nil {
		problem("shared_units map is required")
	}
	for _, installationID := range sortedKeys(d.Operations) {
		operation := d.Operations[installationID]
		if !idPattern.MatchString(installationID) {
			problem("operation installation %q is not a lowercase stable id", installationID)
		}
		if operation.Kind != "install" && operation.Kind != "remove" {
			problem("operation %q kind is %q, want install or remove", installationID, operation.Kind)
		}
		if !sha256Pattern.MatchString(operation.SoftwareLockDigest) || !sha256Pattern.MatchString(operation.PlanDigest) {
			problem("operation %q lock and plan digests must be lowercase SHA-256 values", installationID)
		}
		if err := operation.Target.Validate(); err != nil {
			problem("operation %q target: %v", installationID, err)
		}
		if err := validateCanonicalInstant(operation.StartedAt); err != nil {
			problem("operation %q started_at: %v", installationID, err)
		}
		if err := validateCanonicalInstant(operation.LeaseExpiresAt); err != nil {
			problem("operation %q lease_expires_at: %v", installationID, err)
		}
		started, startedErr := time.Parse(time.RFC3339Nano, operation.StartedAt)
		expires, expiresErr := time.Parse(time.RFC3339Nano, operation.LeaseExpiresAt)
		if startedErr == nil && expiresErr == nil && !expires.After(started) {
			problem("operation %q lease must expire after it started", installationID)
		}
		if !validInvocationID(operation.ClaimedBy) {
			problem("operation %q claimed_by is not an opaque invocation id", installationID)
		}
		if operation.Fence == 0 {
			problem("operation %q fence must be greater than zero", installationID)
		}
		if len(operation.Groups) == 0 {
			problem("operation %q groups must not be empty", installationID)
		}
		for _, groupID := range sortedKeys(operation.Groups) {
			group := operation.Groups[groupID]
			if !idPattern.MatchString(group.Adapter) || !idPattern.MatchString(group.Scope) || groupID != group.Adapter+":"+group.Scope {
				problem("operation %q group %q has an invalid adapter/scope identity", installationID, groupID)
			}
			if group.EffectModel != installplan.EffectShared && group.EffectModel != installplan.EffectIsolated {
				problem("operation %q group %q has invalid effect_model %q", installationID, groupID, group.EffectModel)
			}
			if len(group.Units) == 0 {
				problem("operation %q group %q units must not be empty", installationID, groupID)
			}
			for _, unitID := range sortedKeys(group.Units) {
				unit := group.Units[unitID]
				if !unitIDPattern.MatchString(unitID) {
					problem("operation %q group %q unit id %q is invalid", installationID, groupID, unitID)
				}
				if unit.Before != installplan.BeforeAbsent && unit.Before != installplan.BeforeExact && unit.Before != installplan.BeforeNonExact {
					problem("operation %q unit %q has invalid before %q", installationID, unitID, unit.Before)
				}
				if operation.Kind == "install" {
					if unit.OwnershipAfter != installplan.OwnershipTemperAdded && unit.OwnershipAfter != installplan.OwnershipPreExisting {
						problem("operation %q unit %q has invalid ownership_after %q", installationID, unitID, unit.OwnershipAfter)
					}
					if unit.OwnershipBefore != "" || unit.Location != "" || unit.RemoveProvider || unit.RetireShared {
						problem("install operation %q unit %q carries removal-only fields", installationID, unitID)
					}
				} else {
					if unit.Before == installplan.BeforeNonExact {
						problem("remove operation %q unit %q cannot have non-exact pre-state", installationID, unitID)
					}
					if unit.OwnershipBefore != installplan.OwnershipTemperAdded && unit.OwnershipBefore != installplan.OwnershipPreExisting {
						problem("remove operation %q unit %q has invalid ownership_before %q", installationID, unitID, unit.OwnershipBefore)
					}
					if unit.OwnershipAfter != "" {
						problem("remove operation %q unit %q carries ownership_after", installationID, unitID)
					}
					if err := validateAbsolutePath(unit.Location); err != nil {
						problem("remove operation %q unit %q location: %v", installationID, unitID, err)
					}
					if unit.RemoveProvider && unit.OwnershipBefore != installplan.OwnershipTemperAdded {
						problem("remove operation %q unit %q cannot remove pre-existing software", installationID, unitID)
					}
				}
				if group.EffectModel == installplan.EffectShared {
					if !sha256Pattern.MatchString(unit.SharedClaim) {
						problem("operation %q shared unit %q requires a shared_claim", installationID, unitID)
					}
					if unit.Before == installplan.BeforeNonExact {
						problem("operation %q shared unit %q cannot have non-exact pre-state", installationID, unitID)
					}
					if operation.Kind == "remove" && unit.RetireShared && unit.OwnershipBefore != installplan.OwnershipTemperAdded {
						problem("remove operation %q shared unit %q cannot retire pre-existing software", installationID, unitID)
					}
					if operation.Kind == "remove" && unit.RemoveProvider != unit.RetireShared {
						problem("remove operation %q shared unit %q must remove exactly the retiring generation", installationID, unitID)
					}
					if operation.Kind == "remove" && unit.RetireShared {
						shared, ok := d.SharedUnits[unit.SharedClaim]
						if !ok || shared.Lifecycle != installplan.SharedRetiring || len(shared.Claims) != 0 {
							problem("remove operation %q shared unit %q has no exact retiring authority", installationID, unitID)
						}
					}
					if operation.Kind == "remove" {
						if shared, ok := d.SharedUnits[unit.SharedClaim]; ok {
							if _, stillClaimed := shared.Claims[installationID]; stillClaimed {
								problem("remove operation %q shared unit %q still has its released claim", installationID, unitID)
							}
						}
					}
				} else if unit.SharedClaim != "" {
					problem("operation %q isolated unit %q carries a shared_claim", installationID, unitID)
				} else if unit.RetireShared {
					problem("operation %q isolated unit %q cannot retire shared authority", installationID, unitID)
				}
			}
			if operation.Kind == "remove" && group.EffectModel == installplan.EffectIsolated {
				removeGroup := false
				for _, unit := range group.Units {
					removeGroup = removeGroup || unit.RemoveProvider
				}
				if removeGroup {
					for unitID, unit := range group.Units {
						if !unit.RemoveProvider || unit.OwnershipBefore != installplan.OwnershipTemperAdded {
							problem("remove operation %q isolated group %q is not wholly Temper-added and removable at unit %q", installationID, groupID, unitID)
						}
					}
				}
			}
		}
		if operation.PlanDigest != operationIntentDigest(installationID, operation) {
			problem("operation %q plan_digest does not match its immutable intent", installationID)
		}
	}
	for _, sharedKey := range sortedKeys(d.SharedUnits) {
		unit := d.SharedUnits[sharedKey]
		if sharedKey != installplan.SharedUnitKey(unit.Adapter, unit.Scope, unit.NativeName) {
			problem("shared unit %q key differs from its provider identity", sharedKey)
		}
		if !idPattern.MatchString(unit.Adapter) || !idPattern.MatchString(unit.Scope) || strings.TrimSpace(unit.NativeName) == "" || strings.TrimSpace(unit.Version) == "" {
			problem("shared unit %q has an incomplete provider identity", sharedKey)
		}
		if !strictlySorted(unit.Dependencies) {
			problem("shared unit %q dependencies must be unique and sorted", sharedKey)
		}
		if !artifactsCanonical(unit.Artifacts) {
			problem("shared unit %q artifacts are not canonical", sharedKey)
		}
		if len(unit.Artifacts) == 0 && unit.Revision == "" {
			problem("shared unit %q requires an exact revision or hashed artifact", sharedKey)
		}
		if err := validateAbsolutePath(unit.Location); err != nil {
			problem("shared unit %q location: %v", sharedKey, err)
		}
		if unit.Acquisition != installplan.OwnershipTemperAdded && unit.Acquisition != installplan.OwnershipPreExisting {
			problem("shared unit %q has invalid acquisition %q", sharedKey, unit.Acquisition)
		}
		if unit.Lifecycle != installplan.SharedActive && unit.Lifecycle != installplan.SharedRetiring {
			problem("shared unit %q has invalid lifecycle %q", sharedKey, unit.Lifecycle)
		}
		if unit.Lifecycle == installplan.SharedActive && len(unit.Claims) == 0 {
			problem("active shared unit %q must have at least one claim", sharedKey)
		}
		if unit.Lifecycle == installplan.SharedRetiring {
			if unit.Acquisition != installplan.OwnershipTemperAdded {
				problem("retiring shared unit %q must be Temper-added", sharedKey)
			}
			if len(unit.Claims) != 0 {
				problem("retiring shared unit %q must have no claims", sharedKey)
			}
			if !operationRetiresShared(d.Operations, sharedKey) {
				problem("retiring shared unit %q has no matching remove operation", sharedKey)
			}
		}
		for _, installationID := range sortedKeys(unit.Claims) {
			claim := unit.Claims[installationID]
			if !idPattern.MatchString(installationID) || !sha256Pattern.MatchString(claim.SoftwareLockDigest) || !unitIDPattern.MatchString(claim.UnitID) {
				problem("shared unit %q claim %q has an invalid identity", sharedKey, installationID)
			}
			if claim.Status != installplan.ClaimPrepared && claim.Status != installplan.ClaimActive {
				problem("shared unit %q claim %q has invalid status %q", sharedKey, installationID, claim.Status)
			}
		}
	}
	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

// Projection returns the planner-facing current state for one installation.
func (d Document) Projection(installation installplan.Installation) (installplan.State, error) {
	if err := d.Validate(); err != nil {
		return installplan.State{}, err
	}
	if d.Root != installation.Root {
		return installplan.State{}, errors.New("software root state belongs to another root")
	}
	state := installplan.State{Shared: installplan.SharedState{Root: d.Root, Units: make(map[string]installplan.SharedUnit, len(d.SharedUnits))}}
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
	if operation, ok := d.Operations[installation.ID]; ok {
		if operation.Kind == "remove" {
			state.Removing = true
			return state, nil
		}
		units := map[string]installplan.PreparedUnit{}
		for _, group := range operation.Groups {
			for unitID, unit := range group.Units {
				units[unitID] = installplan.PreparedUnit{Before: unit.Before, OwnershipAfter: unit.OwnershipAfter, SharedClaim: unit.SharedClaim}
			}
		}
		state.Prepared = &installplan.Prepared{
			LockDigest: operation.SoftwareLockDigest, Target: operation.Target,
			Installation: installation, Units: units,
		}
	}
	return state, nil
}

// Prepare records immutable operation intent and provisional claims, or
// reclaims one expired operation with a higher fence after the caller has
// already inspected and replanned reality.
func Prepare(current *Document, desired softwarelock.Document, plan installplan.Plan, observed installplan.Observation, lease Lease) (Document, bool, uint64, error) {
	if err := desired.Validate(); err != nil {
		return Document{}, false, 0, err
	}
	if err := validateLease(lease); err != nil {
		return Document{}, false, 0, err
	}
	if plan.Target != desired.Target || observed.Target != desired.Target || plan.Installation.Root != observed.Root {
		return Document{}, false, 0, errors.New("prepared software inputs disagree on target or root")
	}
	digest, err := desired.SemanticDigest()
	if err != nil {
		return Document{}, false, 0, err
	}
	if plan.LockDigest != digest {
		return Document{}, false, 0, errors.New("software installation plan belongs to another lock")
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
		if operation.Kind != "install" {
			return Document{}, false, 0, errors.New("installation has a prepared software removal")
		}
		if operation.SoftwareLockDigest != plan.LockDigest || operation.Target != plan.Target {
			return Document{}, false, 0, errors.New("prepared software operation belongs to another lock or target")
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
	if !NeedsOperation(plan) {
		return next, false, 0, nil
	}
	operation, err := operationFromPlan(desired, plan, lease)
	if err != nil {
		return Document{}, false, 0, err
	}
	for _, group := range plan.Groups {
		if group.EffectModel != installplan.EffectShared {
			continue
		}
		for _, planned := range group.Units {
			if planned.ClaimAction != installplan.ClaimAdd {
				continue
			}
			locked := desired.Units[planned.ID]
			key := planned.SharedClaim
			shared, exists := next.SharedUnits[key]
			if !exists {
				if err := validateAbsolutePath(planned.Location); err != nil {
					return Document{}, false, 0, fmt.Errorf("prepare shared unit %q: adapter did not supply an absolute install location", planned.ID)
				}
				dependencies := append([]string(nil), locked.Dependencies...)
				sort.Strings(dependencies)
				artifacts := append([]software.Artifact(nil), locked.Artifacts...)
				sortArtifacts(artifacts)
				shared = SharedUnit{
					Adapter: locked.Adapter, Scope: locked.Scope, NativeName: locked.NativeName,
					Version: locked.Version, Revision: locked.Revision,
					Dependencies: dependencies, Artifacts: artifacts, Location: planned.Location,
					Acquisition: planned.Ownership, Lifecycle: installplan.SharedActive, Claims: map[string]installplan.SharedClaim{},
				}
			} else if shared.Lifecycle != installplan.SharedActive || !sameSharedIdentity(shared, locked) || shared.Location != planned.Location || shared.Acquisition != planned.Ownership {
				return Document{}, false, 0, fmt.Errorf("shared unit %q conflicts with root-state identity", planned.ID)
			}
			if _, claimed := shared.Claims[plan.Installation.ID]; claimed {
				return Document{}, false, 0, fmt.Errorf("shared unit %q already has a claim for installation %q", planned.ID, plan.Installation.ID)
			}
			shared.Claims[plan.Installation.ID] = installplan.SharedClaim{
				SoftwareLockDigest: plan.LockDigest, UnitID: planned.ID, Status: installplan.ClaimPrepared,
			}
			next.SharedUnits[key] = shared
		}
	}
	next.Generation++
	next.Operations[plan.Installation.ID] = operation
	if err := next.Validate(); err != nil {
		return Document{}, false, 0, err
	}
	return next, true, operation.Fence, nil
}

func NeedsOperation(plan installplan.Plan) bool {
	if plan.EffectCount() > 0 {
		return true
	}
	for _, group := range plan.Groups {
		for _, unit := range group.Units {
			if unit.ClaimAction == installplan.ClaimAdd {
				return true
			}
		}
	}
	return false
}

// AssertFence is called immediately before each provider effect and state
// completion. A stale or expired holder loses without mutating.
func (d Document) AssertFence(installationID, invocationID string, fence uint64, now time.Time) error {
	if err := d.Validate(); err != nil {
		return err
	}
	operation, ok := d.Operations[installationID]
	if !ok || operation.ClaimedBy != invocationID || operation.Fence != fence {
		return errors.New("software installation operation fence is no longer held")
	}
	expires, err := time.Parse(time.RFC3339Nano, operation.LeaseExpiresAt)
	if err != nil {
		return err
	}
	if !expires.After(now) {
		return errors.New("software installation operation lease expired")
	}
	return nil
}

func Finalize(current Document, installationID, invocationID string, fence uint64, now time.Time) (Document, error) {
	if err := current.AssertFence(installationID, invocationID, fence, now); err != nil {
		return Document{}, err
	}
	next := cloneDocument(current)
	operation := next.Operations[installationID]
	if operation.Kind != "install" {
		return Document{}, errors.New("prepared software operation is not an install")
	}
	for _, group := range operation.Groups {
		if group.EffectModel != installplan.EffectShared {
			continue
		}
		for _, unit := range group.Units {
			shared, ok := next.SharedUnits[unit.SharedClaim]
			if !ok {
				return Document{}, errors.New("prepared shared claim disappeared before finalization")
			}
			claim, ok := shared.Claims[installationID]
			if !ok || claim.SoftwareLockDigest != operation.SoftwareLockDigest {
				return Document{}, errors.New("prepared shared claim differs before finalization")
			}
			claim.Status = installplan.ClaimActive
			shared.Claims[installationID] = claim
			next.SharedUnits[unit.SharedClaim] = shared
		}
	}
	delete(next.Operations, installationID)
	next.Generation++
	if err := next.Validate(); err != nil {
		return Document{}, err
	}
	return next, nil
}

func operationFromPlan(desired softwarelock.Document, plan installplan.Plan, lease Lease) (Operation, error) {
	groups := make(map[string]OperationGroup, len(plan.Groups))
	for _, group := range plan.Groups {
		units := make(map[string]OperationUnit, len(group.Units))
		for _, planned := range group.Units {
			before := installplan.BeforeExact
			switch planned.Action {
			case installplan.ActionAdd:
				before = installplan.BeforeAbsent
			case installplan.ActionReplace:
				before = installplan.BeforeNonExact
			case installplan.ActionPreserve:
			default:
				return Operation{}, fmt.Errorf("installation plan unit %q has invalid action %q", planned.ID, planned.Action)
			}
			if _, ok := desired.Units[planned.ID]; !ok {
				return Operation{}, fmt.Errorf("installation plan references unknown unit %q", planned.ID)
			}
			units[planned.ID] = OperationUnit{Before: before, OwnershipAfter: planned.Ownership, SharedClaim: planned.SharedClaim}
		}
		groups[group.ID] = OperationGroup{Adapter: group.Adapter, Scope: group.Scope, EffectModel: group.EffectModel, Units: units}
	}
	operation := Operation{
		Kind: "install", SoftwareLockDigest: plan.LockDigest, Target: plan.Target,
		StartedAt: lease.Now.UTC().Format(time.RFC3339Nano), ClaimedBy: lease.InvocationID,
		LeaseExpiresAt: lease.Now.Add(lease.Duration).UTC().Format(time.RFC3339Nano), Fence: 1, Groups: groups,
	}
	operation.PlanDigest = operationIntentDigest(plan.Installation.ID, operation)
	return operation, nil
}

func operationIntentDigest(installationID string, operation Operation) string {
	intent := struct {
		Installation       string                    `json:"installation"`
		Kind               string                    `json:"kind"`
		SoftwareLockDigest string                    `json:"software_lock_digest"`
		Target             software.Target           `json:"target"`
		Groups             map[string]OperationGroup `json:"groups"`
	}{installationID, operation.Kind, operation.SoftwareLockDigest, operation.Target, operation.Groups}
	encoded, err := json.Marshal(intent)
	if err != nil {
		panic(fmt.Sprintf("encode software operation intent: %v", err))
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func operationRetiresShared(operations map[string]Operation, sharedKey string) bool {
	count := 0
	for _, operation := range operations {
		if operation.Kind != "remove" {
			continue
		}
		for _, group := range operation.Groups {
			for _, unit := range group.Units {
				if unit.SharedClaim == sharedKey && unit.RetireShared {
					count++
				}
			}
		}
	}
	return count == 1
}

func validateLease(lease Lease) error {
	if !validInvocationID(lease.InvocationID) {
		return errors.New("software invocation id is required and must contain no whitespace or control characters")
	}
	if lease.Now.IsZero() || lease.Duration <= 0 {
		return errors.New("software operation lease requires a current instant and positive duration")
	}
	return nil
}

func validInvocationID(value string) bool {
	return value != "" && len(value) <= 200 && !strings.ContainsAny(value, "\r\n\t \x00")
}

func sameSharedIdentity(shared SharedUnit, locked softwarelock.Unit) bool {
	return shared.Adapter == locked.Adapter && shared.Scope == locked.Scope && shared.NativeName == locked.NativeName &&
		shared.Version == locked.Version && shared.Revision == locked.Revision &&
		sameStrings(shared.Dependencies, locked.Dependencies) && sameArtifacts(shared.Artifacts, locked.Artifacts)
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

func cloneDocument(document Document) Document {
	clone := document
	clone.Operations = make(map[string]Operation, len(document.Operations))
	for installationID, operation := range document.Operations {
		operation.Groups = cloneGroups(operation.Groups)
		clone.Operations[installationID] = operation
	}
	clone.SharedUnits = make(map[string]SharedUnit, len(document.SharedUnits))
	for key, unit := range document.SharedUnits {
		unit.Dependencies = append([]string(nil), unit.Dependencies...)
		unit.Artifacts = append([]software.Artifact(nil), unit.Artifacts...)
		claims := make(map[string]installplan.SharedClaim, len(unit.Claims))
		for installationID, claim := range unit.Claims {
			claims[installationID] = claim
		}
		unit.Claims = claims
		clone.SharedUnits[key] = unit
	}
	return clone
}

func cloneGroups(groups map[string]OperationGroup) map[string]OperationGroup {
	clone := make(map[string]OperationGroup, len(groups))
	for groupID, group := range groups {
		units := make(map[string]OperationUnit, len(group.Units))
		for unitID, unit := range group.Units {
			units[unitID] = unit
		}
		group.Units = units
		clone[groupID] = group
	}
	return clone
}

func validateAbsolutePath(path string) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Dir(path) == path {
		return fmt.Errorf("path %q must be absolute, clean, and narrower than a filesystem root", path)
	}
	return nil
}

func validateCanonicalInstant(value string) error {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.Location() != time.UTC || parsed.Format(time.RFC3339Nano) != value {
		return errors.New("must use canonical RFC 3339 UTC form")
	}
	return nil
}

func strictlySorted(values []string) bool {
	for index := 1; index < len(values); index++ {
		if values[index] <= values[index-1] {
			return false
		}
	}
	return true
}

func artifactsCanonical(values []software.Artifact) bool {
	for index, artifact := range values {
		if strings.TrimSpace(artifact.Locator) == "" || !sha256Pattern.MatchString(artifact.SHA256) || artifact.Size < 0 || artifact.UnpackedSize < 0 || artifact.InstalledEntries < 0 || ((artifact.Format == "") != (artifact.ArchiveRoot == "")) || (artifact.Format == "" && (artifact.UnpackedSize != 0 || artifact.InstalledEntries != 0)) {
			return false
		}
		if index > 0 {
			previous := values[index-1]
			if artifact.Locator < previous.Locator || (artifact.Locator == previous.Locator && artifact.SHA256 <= previous.SHA256) {
				return false
			}
		}
	}
	return true
}

func sortArtifacts(values []software.Artifact) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].Locator == values[j].Locator {
			return values[i].SHA256 < values[j].SHA256
		}
		return values[i].Locator < values[j].Locator
	})
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
