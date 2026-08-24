package qualification

import "fmt"

// ValidateProfileStatusTransition verifies one immutable lineage edge. The
// supplied digest must be the already-verified canonical bytes of previous;
// this pure function neither discovers nor resolves history.
func ValidateProfileStatusTransition(previous, current ProfileEnvelope, previousSHA256 string) error {
	var problems []string
	problem := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	if !sha256Pattern.MatchString(previousSHA256) {
		problem("previous sha256 must be 64 lowercase hexadecimal characters")
	}
	if previous.Schema != current.Schema || previous.ID != current.ID {
		problem("status transition must remain in one schema and id lineage")
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
	if !isProfileStatus(previous.Status) {
		problem("previous status %q is not supported", previous.Status)
	}
	if !isProfileStatus(current.Status) {
		problem("current status %q is not supported", current.Status)
	} else if isProfileStatus(previous.Status) && !legalProfileStatusTransition(previous.Status, current.Status) {
		problem("status transition %s -> %s is not allowed", previous.Status, current.Status)
	}
	if previous.Promotion == current.Promotion {
		problem("a new profile revision must have a distinct promotion identity")
	}

	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

func isProfileStatus(status string) bool {
	switch status {
	case ProfileStatusWatch, ProfileStatusLab, ProfileStatusQualified, ProfileStatusRejected, ProfileStatusRetired:
		return true
	default:
		return false
	}
}

func legalProfileStatusTransition(previous, current string) bool {
	if previous == current {
		return true
	}
	switch previous {
	case ProfileStatusWatch:
		return current == ProfileStatusLab || current == ProfileStatusRejected || current == ProfileStatusRetired
	case ProfileStatusLab:
		return current == ProfileStatusQualified || current == ProfileStatusRejected || current == ProfileStatusRetired
	case ProfileStatusQualified:
		return current == ProfileStatusLab || current == ProfileStatusRejected || current == ProfileStatusRetired
	case ProfileStatusRejected, ProfileStatusRetired:
		return current == ProfileStatusLab
	default:
		return false
	}
}
