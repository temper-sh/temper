package qualification

import "fmt"

// ValidateProfileDispositionTransition verifies one immutable lineage edge.
// The supplied digest must be the already-verified canonical bytes of
// previous; this pure function neither discovers nor resolves history.
func ValidateProfileDispositionTransition(previous, current ProfileEnvelope, previousSHA256 string) error {
	var problems []string
	problem := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	if !sha256Pattern.MatchString(previousSHA256) {
		problem("previous sha256 must be 64 lowercase hexadecimal characters")
	}
	if previous.Schema != current.Schema || previous.ID != current.ID {
		problem("disposition transition must remain in one schema and id lineage")
	}
	if previous.Revision+1 != current.Revision {
		problem("current revision must immediately follow previous revision %d", previous.Revision)
	}
	wantSupersedes := Reference{
		Schema: previous.Schema, ID: previous.ID, Revision: previous.Revision, SHA256: previousSHA256,
	}
	if current.Supersedes == nil || *current.Supersedes != wantSupersedes {
		problem("current supersedes must exactly identify the previous canonical profile")
	}

	validateProfileDisposition(previous, problem)
	validateProfileDisposition(current, problem)
	if isQualificationStatus(previous.QualificationStatus) && isQualificationStatus(current.QualificationStatus) &&
		!legalQualificationTransition(previous.QualificationStatus, current.QualificationStatus) {
		problem("qualification transition %s -> %s is not allowed", previous.QualificationStatus, current.QualificationStatus)
	}
	if isLifecycleStatus(previous.LifecycleStatus) && isLifecycleStatus(current.LifecycleStatus) &&
		!legalLifecycleTransition(previous.LifecycleStatus, current.LifecycleStatus) {
		problem("lifecycle transition %s -> %s is not allowed", previous.LifecycleStatus, current.LifecycleStatus)
	}
	if previous.LifecycleStatus == LifecycleStatusRetired && current.LifecycleStatus == LifecycleStatusExperimental && current.QualificationStatus != QualificationStatusLab {
		problem("reopening RETIRED requires LAB/EXPERIMENTAL")
	}
	if previous.Promotion == current.Promotion {
		problem("a new profile revision must have a distinct promotion identity")
	}

	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

func validateProfileDisposition(envelope ProfileEnvelope, problem func(string, ...any)) {
	validateDisposition("", envelope.Revision, envelope.QualificationStatus, envelope.QualificationReason, envelope.LifecycleStatus, envelope.LifecycleReason, problem)
}

func validateDisposition(location string, revision uint64, qualificationStatus, qualificationReason, lifecycleStatus, lifecycleReason string, problem func(string, ...any)) {
	qualificationValid := isQualificationStatus(qualificationStatus)
	lifecycleValid := isLifecycleStatus(lifecycleStatus)
	if !qualificationValid {
		problem("%squalification_status %q is not supported", location, qualificationStatus)
	}
	if !lifecycleValid {
		problem("%slifecycle_status %q is not supported", location, lifecycleStatus)
	}
	validateLine(location+"qualification_reason", qualificationReason, problem)
	validateLine(location+"lifecycle_reason", lifecycleReason, problem)
	if !qualificationValid || !lifecycleValid {
		return
	}

	switch lifecycleStatus {
	case LifecycleStatusSupported, LifecycleStatusDeprecated:
		if qualificationStatus != QualificationStatusQualified {
			problem("%s lifecycle requires QUALIFIED qualification", lifecycleStatus)
		}
	case LifecycleStatusRetired:
	}
	if qualificationStatus == QualificationStatusRejected && lifecycleStatus != LifecycleStatusRetired {
		problem("REJECTED qualification requires RETIRED lifecycle")
	}
	if revision == 1 && lifecycleStatus == LifecycleStatusRetired && qualificationStatus != QualificationStatusRejected {
		problem("initial RETIRED lifecycle requires REJECTED qualification")
	}
}

func isQualificationStatus(status string) bool {
	switch status {
	case QualificationStatusWatch, QualificationStatusLab, QualificationStatusQualified, QualificationStatusRejected:
		return true
	default:
		return false
	}
}

func isLifecycleStatus(status string) bool {
	switch status {
	case LifecycleStatusExperimental, LifecycleStatusSupported, LifecycleStatusDeprecated, LifecycleStatusRetired:
		return true
	default:
		return false
	}
}

func legalQualificationTransition(previous, current string) bool {
	if previous == current {
		return true
	}
	switch previous {
	case QualificationStatusWatch:
		return current == QualificationStatusLab || current == QualificationStatusRejected
	case QualificationStatusLab:
		return current == QualificationStatusQualified || current == QualificationStatusRejected
	case QualificationStatusQualified:
		return current == QualificationStatusLab || current == QualificationStatusRejected
	case QualificationStatusRejected:
		return current == QualificationStatusLab
	default:
		return false
	}
}

func legalLifecycleTransition(previous, current string) bool {
	if previous == current {
		return true
	}
	switch previous {
	case LifecycleStatusExperimental:
		return current == LifecycleStatusSupported || current == LifecycleStatusRetired
	case LifecycleStatusSupported:
		return current == LifecycleStatusExperimental || current == LifecycleStatusDeprecated || current == LifecycleStatusRetired
	case LifecycleStatusDeprecated:
		return current == LifecycleStatusExperimental || current == LifecycleStatusSupported || current == LifecycleStatusRetired
	case LifecycleStatusRetired:
		return current == LifecycleStatusExperimental
	default:
		return false
	}
}
