// Package checkplan computes a read-only software installation audit from
// already-parsed desired, receipt, root-state, requirement, and provider facts.
package checkplan

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/temper-sh/temper/internal/software/installplan"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/receipt"
	"github.com/temper-sh/temper/internal/software/rootstate"
)

type UnitStatus string

const (
	UnitExact        UnitStatus = "exact"
	UnitMissing      UnitStatus = "missing"
	UnitDrifted      UnitStatus = "drifted"
	UnitUnclaimed    UnitStatus = "unclaimed"
	UnitUnreceipted  UnitStatus = "unreceipted"
	OwnershipUnknown            = "unknown"
)

type RequirementStatus string

const (
	RequirementExact   RequirementStatus = "exact"
	RequirementMissing RequirementStatus = "missing"
	RequirementDrifted RequirementStatus = "drifted"
)

type Code string

const (
	CodeRequiredReceiptMissing Code = "required-receipt-missing"
	CodeRequiredReceiptDrift   Code = "required-receipt-drift"
	CodeReceiptMissing         Code = "receipt-missing"
	CodeReceiptDrift           Code = "receipt-drift"
	CodeProviderMissing        Code = "provider-missing"
	CodeProviderDrift          Code = "provider-drift"
	CodeClaimMissing           Code = "claim-missing"
	CodeClaimDrift             Code = "claim-drift"
	CodeOperationPrepared      Code = "operation-prepared"
)

type RequirementObservation struct {
	SoftwareLockDigest string
	Installation       string
	ReceiptSHA256      string
	Status             RequirementStatus
	Detail             string
}

type Input struct {
	Desired      softwarelock.Document
	Installation installplan.Installation
	EffectModels map[string]installplan.EffectModel
	Observed     installplan.Observation
	Receipt      *receipt.Document
	State        *rootstate.Document
	Requirements []RequirementObservation
}

type RequirementResult struct {
	SoftwareLockDigest string
	Installation       string
	ReceiptSHA256      string
	Status             RequirementStatus
}

type UnitResult struct {
	ID        string
	Adapter   string
	Scope     string
	Status    UnitStatus
	Location  string
	Ownership string
	Claim     string
}

type Finding struct {
	Code        Code
	Unit        string
	Requirement string
	Detail      string
}

type Result struct {
	Installation  string
	LockDigest    string
	ReceiptSHA256 string
	Packages      int
	Requirements  []RequirementResult
	Units         []UnitResult
	Findings      []Finding
}

func (r Result) Exact() bool       { return len(r.Findings) == 0 }
func (r Result) ProblemCount() int { return len(r.Findings) }

var (
	installationIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	sha256Pattern         = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Analyze compares already-read facts without performing I/O or mutating any
// input. Fatal errors mean the supplied facts cannot form a valid report;
// expected installed-state disagreement is returned as findings.
func Analyze(input Input) (Result, error) {
	if err := validateInput(input); err != nil {
		return Result{}, err
	}
	lockDigest, err := input.Desired.SemanticDigest()
	if err != nil {
		return Result{}, err
	}
	result := Result{Installation: input.Installation.ID, LockDigest: lockDigest, Packages: len(input.Desired.Selections)}

	requirements, requirementFindings := assessRequirements(input.Desired, input.Requirements)
	result.Requirements = requirements
	result.Findings = append(result.Findings, requirementFindings...)

	receiptProblem := ""
	if input.Receipt != nil {
		data, err := receipt.Marshal(*input.Receipt)
		if err != nil {
			return Result{}, err
		}
		result.ReceiptSHA256 = receipt.Digest(data)
		if err := input.Receipt.ValidateAgainst(input.Desired, input.Installation); err != nil {
			receiptProblem = err.Error()
		} else if err := verifyRecordedRequirementIdentities(*input.Receipt, requirements); err != nil {
			receiptProblem = err.Error()
		}
	}

	stateProblem := ""
	if input.State != nil && input.State.Root != input.Installation.Root {
		stateProblem = "software root state belongs to another root"
	}
	if input.State != nil {
		if operation, ok := input.State.Operations[input.Installation.ID]; ok {
			result.Findings = append(result.Findings, Finding{
				Code:   CodeOperationPrepared,
				Detail: fmt.Sprintf("prepared install operation is held by %q through %s at fence %d", operation.ClaimedBy, operation.LeaseExpiresAt, operation.Fence),
			})
		}
	}

	models := input.EffectModels
	for _, unitID := range dependencyOrder(input.Desired) {
		locked := input.Desired.Units[unitID]
		actual := input.Observed.Units[unitID]
		unit := UnitResult{
			ID: unitID, Adapter: locked.Adapter, Scope: locked.Scope,
			Location: actual.Location, Ownership: OwnershipUnknown,
		}
		if input.Receipt != nil && receiptProblem == "" {
			receipted := input.Receipt.Units[unitID]
			unit.Ownership = string(receipted.Ownership)
			unit.Claim = receipted.SharedClaim
		}

		switch {
		case !actual.Present:
			unit.Status = UnitMissing
			result.Findings = append(result.Findings, Finding{Code: CodeProviderMissing, Unit: unitID, Detail: "provider reports the locked unit absent"})
		case !installplan.MatchesLock(locked, actual):
			unit.Status = UnitDrifted
			result.Findings = append(result.Findings, Finding{Code: CodeProviderDrift, Unit: unitID, Detail: "provider identity or closure differs from the software lock"})
		case models[locked.Adapter] == installplan.EffectIsolated && !strictlyBelow(installplan.InstallationRoot(input.Installation), actual.Location):
			unit.Status = UnitDrifted
			result.Findings = append(result.Findings, Finding{Code: CodeProviderDrift, Unit: unitID, Detail: "isolated provider location is outside the named installation"})
		case input.Receipt == nil:
			unit.Status = UnitUnreceipted
			result.Findings = append(result.Findings, Finding{Code: CodeReceiptMissing, Unit: unitID, Detail: "exact provider state has no installation receipt"})
		case receiptProblem != "":
			unit.Status = UnitDrifted
			result.Findings = append(result.Findings, Finding{Code: CodeReceiptDrift, Unit: unitID, Detail: receiptProblem})
		default:
			receipted := input.Receipt.Units[unitID]
			switch {
			case actual.Location != receipted.Location:
				unit.Status = UnitDrifted
				result.Findings = append(result.Findings, Finding{Code: CodeProviderDrift, Unit: unitID, Detail: "provider location differs from the installation receipt"})
			case models[locked.Adapter] == installplan.EffectIsolated && receipted.SharedClaim != "":
				unit.Status = UnitDrifted
				result.Findings = append(result.Findings, Finding{Code: CodeReceiptDrift, Unit: unitID, Detail: "isolated receipt unit carries a shared claim"})
			case models[locked.Adapter] == installplan.EffectShared:
				unit.Status, result.Findings = assessSharedClaim(input, locked, unit, lockDigest, stateProblem, result.Findings)
			default:
				unit.Status = UnitExact
			}
		}
		result.Units = append(result.Units, unit)
	}

	result.Findings = append(result.Findings, unexpectedClaims(input)...)
	sortFindings(result.Findings)
	return result, nil
}

func validateInput(input Input) error {
	if err := input.Desired.Validate(); err != nil {
		return err
	}
	if !installationIDPattern.MatchString(input.Installation.ID) {
		return fmt.Errorf("installation id %q is not a lowercase stable id", input.Installation.ID)
	}
	if err := validateAbsolutePath(input.Installation.Root); err != nil {
		return fmt.Errorf("installation root: %w", err)
	}
	if input.Observed.Target != input.Desired.Target || input.Observed.Root != input.Installation.Root {
		return errors.New("software observation differs from the lock target or installation root")
	}
	if len(input.Observed.Units) != len(input.Desired.Units) {
		return errors.New("software observation closure differs from the software lock")
	}
	for unitID, locked := range input.Desired.Units {
		actual, ok := input.Observed.Units[unitID]
		if !ok {
			return fmt.Errorf("software observation omits unit %q", unitID)
		}
		if actual.Present {
			if err := validateAbsolutePath(actual.Location); err != nil {
				return fmt.Errorf("software observation unit %q location: %w", unitID, err)
			}
			if actual.InstallLocation != "" && actual.InstallLocation != actual.Location {
				return fmt.Errorf("software observation unit %q install location differs from observed location", unitID)
			}
		} else if actual.Adapter != "" || actual.Scope != "" || actual.NativeName != "" || actual.Version != "" || actual.Revision != "" || len(actual.Dependencies) != 0 || len(actual.Artifacts) != 0 || actual.Location != "" {
			return fmt.Errorf("absent software observation %q carries provider identity", unitID)
		} else if actual.InstallLocation != "" {
			if err := validateAbsolutePath(actual.InstallLocation); err != nil {
				return fmt.Errorf("software observation unit %q install location: %w", unitID, err)
			}
		}
		model, ok := input.EffectModels[locked.Adapter]
		if !ok || (model != installplan.EffectShared && model != installplan.EffectIsolated) {
			return fmt.Errorf("software lock adapter %q has no valid compiled effect model", locked.Adapter)
		}
	}
	if input.Receipt != nil {
		if err := input.Receipt.Validate(); err != nil {
			return err
		}
	}
	if input.State != nil {
		if err := input.State.Validate(); err != nil {
			return err
		}
	}
	for index, requirement := range input.Requirements {
		if !sha256Pattern.MatchString(requirement.SoftwareLockDigest) {
			return fmt.Errorf("requirement observation[%d] has an invalid software lock digest", index)
		}
		switch requirement.Status {
		case RequirementExact:
			if !installationIDPattern.MatchString(requirement.Installation) || !sha256Pattern.MatchString(requirement.ReceiptSHA256) {
				return fmt.Errorf("exact requirement observation[%d] has an invalid installation or receipt identity", index)
			}
		case RequirementMissing, RequirementDrifted:
		default:
			return fmt.Errorf("requirement observation[%d] has invalid status %q", index, requirement.Status)
		}
	}
	return nil
}

func assessRequirements(desired softwarelock.Document, observed []RequirementObservation) ([]RequirementResult, []Finding) {
	wanted := make(map[string]bool, len(desired.Requires))
	for _, requirement := range desired.Requires {
		wanted[requirement.SoftwareLockDigest] = true
	}
	byDigest := map[string][]RequirementObservation{}
	var findings []Finding
	for _, requirement := range observed {
		if !wanted[requirement.SoftwareLockDigest] {
			findings = append(findings, Finding{Code: CodeRequiredReceiptDrift, Requirement: requirement.SoftwareLockDigest, Detail: "supplied receipt is not required by the software lock"})
			continue
		}
		byDigest[requirement.SoftwareLockDigest] = append(byDigest[requirement.SoftwareLockDigest], requirement)
	}
	digests := make([]string, 0, len(wanted))
	for digest := range wanted {
		digests = append(digests, digest)
	}
	sort.Strings(digests)
	results := make([]RequirementResult, 0, len(digests))
	for _, digest := range digests {
		matches := byDigest[digest]
		switch {
		case len(matches) == 0:
			results = append(results, RequirementResult{SoftwareLockDigest: digest, Status: RequirementMissing})
			findings = append(findings, Finding{Code: CodeRequiredReceiptMissing, Requirement: digest, Detail: "required base software lock has no supplied receipt"})
		case len(matches) > 1:
			results = append(results, RequirementResult{SoftwareLockDigest: digest, Status: RequirementDrifted})
			findings = append(findings, Finding{Code: CodeRequiredReceiptDrift, Requirement: digest, Detail: "required base software lock has more than one supplied receipt"})
		default:
			match := matches[0]
			result := RequirementResult{
				SoftwareLockDigest: digest, Installation: match.Installation,
				ReceiptSHA256: match.ReceiptSHA256, Status: match.Status,
			}
			results = append(results, result)
			switch match.Status {
			case RequirementMissing:
				findings = append(findings, Finding{Code: CodeRequiredReceiptMissing, Requirement: digest, Detail: detailOr(match.Detail, "required base software receipt is missing")})
			case RequirementDrifted:
				findings = append(findings, Finding{Code: CodeRequiredReceiptDrift, Requirement: digest, Detail: detailOr(match.Detail, "required base software receipt or provider state drifted")})
			}
		}
	}
	return results, findings
}

func verifyRecordedRequirementIdentities(document receipt.Document, requirements []RequirementResult) error {
	current := make(map[string]RequirementResult, len(requirements))
	for _, requirement := range requirements {
		current[requirement.SoftwareLockDigest] = requirement
	}
	for _, recorded := range document.Requirements {
		requirement := current[recorded.SoftwareLockDigest]
		if requirement.Status == RequirementExact && (recorded.Installation != requirement.Installation || recorded.ReceiptSHA256 != requirement.ReceiptSHA256) {
			return fmt.Errorf("installation receipt binds another current base receipt for software lock %q", recorded.SoftwareLockDigest)
		}
	}
	return nil
}

func assessSharedClaim(input Input, locked softwarelock.Unit, unit UnitResult, lockDigest, stateProblem string, findings []Finding) (UnitStatus, []Finding) {
	unitID := unit.ID
	wantKey := installplan.SharedUnitKey(locked.Adapter, locked.Scope, locked.NativeName)
	if unit.Claim != wantKey {
		return UnitDrifted, append(findings, Finding{Code: CodeReceiptDrift, Unit: unitID, Detail: "shared receipt claim differs from the provider identity"})
	}
	if stateProblem != "" {
		return UnitDrifted, append(findings, Finding{Code: CodeClaimDrift, Unit: unitID, Detail: stateProblem})
	}
	if input.State == nil {
		return UnitUnclaimed, append(findings, Finding{Code: CodeClaimMissing, Unit: unitID, Detail: "shared receipt unit has no root-state authority"})
	}
	shared, ok := input.State.SharedUnits[wantKey]
	if !ok {
		return UnitUnclaimed, append(findings, Finding{Code: CodeClaimMissing, Unit: unitID, Detail: "shared receipt unit has no root-state record"})
	}
	registered := installplan.ObservedUnit{
		Present: true, Adapter: shared.Adapter, Scope: shared.Scope, NativeName: shared.NativeName,
		Version: shared.Version, Revision: shared.Revision,
		Dependencies: shared.Dependencies, Artifacts: shared.Artifacts, Location: shared.Location,
	}
	if !installplan.MatchesLock(locked, registered) || shared.Location != unit.Location || string(shared.Acquisition) != unit.Ownership {
		return UnitDrifted, append(findings, Finding{Code: CodeClaimDrift, Unit: unitID, Detail: "root-state shared identity, location, or acquisition differs from the receipt"})
	}
	claim, ok := shared.Claims[input.Installation.ID]
	if !ok {
		return UnitUnclaimed, append(findings, Finding{Code: CodeClaimMissing, Unit: unitID, Detail: "shared root-state record has no claim for this installation"})
	}
	if claim.SoftwareLockDigest != lockDigest || claim.UnitID != unitID {
		return UnitDrifted, append(findings, Finding{Code: CodeClaimDrift, Unit: unitID, Detail: "shared claim belongs to another lock or unit"})
	}
	if claim.Status != installplan.ClaimActive {
		return UnitUnclaimed, append(findings, Finding{Code: CodeClaimMissing, Unit: unitID, Detail: "shared claim is not active"})
	}
	return UnitExact, findings
}

func unexpectedClaims(input Input) []Finding {
	if input.State == nil || input.State.Root != input.Installation.Root {
		return nil
	}
	expected := map[string]string{}
	for unitID, locked := range input.Desired.Units {
		if input.EffectModels[locked.Adapter] == installplan.EffectShared {
			expected[installplan.SharedUnitKey(locked.Adapter, locked.Scope, locked.NativeName)] = unitID
		}
	}
	var findings []Finding
	for key, shared := range input.State.SharedUnits {
		claim, ok := shared.Claims[input.Installation.ID]
		if !ok {
			continue
		}
		if _, wanted := expected[key]; !wanted {
			findings = append(findings, Finding{Code: CodeClaimDrift, Unit: claim.UnitID, Detail: "root state contains an unexpected shared claim for this installation"})
		}
	}
	return findings
}

func dependencyOrder(desired softwarelock.Document) []string {
	ids := make([]string, 0, len(desired.Units))
	for id := range desired.Units {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	included := make(map[string]bool, len(ids))
	for _, id := range ids {
		included[id] = true
	}
	visited := map[string]bool{}
	ordered := make([]string, 0, len(ids))
	var visit func(string)
	visit = func(id string) {
		if visited[id] {
			return
		}
		visited[id] = true
		dependencies := append([]string(nil), desired.Units[id].Dependencies...)
		sort.Strings(dependencies)
		for _, dependency := range dependencies {
			if included[dependency] {
				visit(dependency)
			}
		}
		ordered = append(ordered, id)
	}
	for _, id := range ids {
		visit(id)
	}
	return ordered
}

func sortFindings(findings []Finding) {
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Requirement != findings[j].Requirement {
			return findings[i].Requirement < findings[j].Requirement
		}
		if findings[i].Unit != findings[j].Unit {
			return findings[i].Unit < findings[j].Unit
		}
		if findings[i].Code != findings[j].Code {
			return findings[i].Code < findings[j].Code
		}
		return findings[i].Detail < findings[j].Detail
	})
}

func strictlyBelow(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != "." && relative != ".." && !filepath.IsAbs(relative) && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func validateAbsolutePath(path string) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Dir(path) == path {
		return fmt.Errorf("path %q must be absolute, clean, and narrower than a filesystem root", path)
	}
	return nil
}

func detailOr(detail, fallback string) string {
	if detail != "" {
		return detail
	}
	return fallback
}
