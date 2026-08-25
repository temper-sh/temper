package qualification

import "fmt"

// ActivityProfile narrows the active tool set of one exact mode for one
// reviewed purpose. It cannot widen the mode's roles, permissions, harnesses,
// or data boundary.
type ActivityProfile struct {
	ProfileEnvelope `yaml:",inline"`
	Spec            ActivitySpec `yaml:"spec"`
}

type ActivitySpec struct {
	ModeProfile Reference   `yaml:"mode_profile"`
	ActiveTools []Reference `yaml:"active_tools"`
	Purpose     string      `yaml:"purpose"`
}

// ParseActivityProfile accepts only canonical activity-profile YAML.
func ParseActivityProfile(data []byte) (ActivityProfile, error) {
	return decodeCanonicalProfile(data, "activity profile", MarshalActivityProfile)
}

// MarshalActivityProfile validates an activity profile and returns canonical
// YAML.
func MarshalActivityProfile(profile ActivityProfile) ([]byte, error) {
	return encodeCanonicalProfile(profile, "activity profile", ActivityProfile.Validate)
}

// Validate enforces the v1 common envelope and closed activity body.
func (p ActivityProfile) Validate() error {
	var problems []string
	problem := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	validateProfileEnvelope(p.ProfileEnvelope, ActivitySchemaV1, problem)
	validateRuntimeReference("spec.mode_profile", p.Spec.ModeProfile, ModeSchemaV1, problem)
	validateActivityTools(p.Spec.ActiveTools, problem)
	if p.Spec.Purpose != "change" && p.Spec.Purpose != "inspect" && p.Spec.Purpose != "review" && p.Spec.Purpose != "verify" {
		problem("spec.purpose %q must be change, inspect, review, or verify", p.Spec.Purpose)
	}
	wantDependency := ProfileDependency{Relationship: "mode", Profile: p.Spec.ModeProfile}
	if len(p.Dependencies) != 1 || p.Dependencies[0] != wantDependency {
		problem("activity dependencies must contain exactly spec.mode_profile")
	}
	validateActivityEvidenceScopes(p, problem)

	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

func validateActivityTools(tools []Reference, problem func(string, ...any)) {
	previous := ""
	for index, tool := range tools {
		location := fmt.Sprintf("spec.active_tools[%d]", index)
		validateRuntimeReference(location, tool, ToolSchemaV1, problem)
		exactIdentity := referenceExactIdentity(tool)
		if index > 0 && exactIdentity <= previous {
			problem("spec.active_tools must be unique and sorted by exact profile identity")
		}
		previous = exactIdentity
	}
}

func validateActivityEvidenceScopes(profile ActivityProfile, problem func(string, ...any)) {
	for index, evidence := range profile.Evidence {
		if !scopeReferenceEqualsReference(evidence.Scope.ActivityProfile, Reference{
			Schema: profile.Schema, ID: profile.ID, Revision: profile.Revision,
		}) {
			problem("evidence[%d].scope.activity_profile must identify the containing activity", index)
		}
	}
}
