package qualification

import (
	"fmt"
	"sort"
)

// ModeProfile composes exact runtimes, tools, placements, and harness
// integrations into one witnessed world. It describes no user selection or
// preferred member.
type ModeProfile struct {
	ProfileEnvelope `yaml:",inline"`
	Spec            ModeSpec `yaml:"spec"`
}

type ModeSpec struct {
	Foreground   string            `yaml:"foreground"`
	Bindings     []ModeBinding     `yaml:"bindings"`
	Tools        []ModeTool        `yaml:"tools"`
	Harnesses    []ModeHarness     `yaml:"harnesses"`
	RoleBindings map[string]string `yaml:"role_bindings"`
	WallModel    ModeWallModel     `yaml:"wall_model"`
}

type ModeBinding struct {
	ID             string    `yaml:"id"`
	Role           string    `yaml:"role"`
	RuntimeProfile Reference `yaml:"runtime_profile"`
	Placement      string    `yaml:"placement"`
	NGL            ModeNGL   `yaml:"ngl"`
	TTLSeconds     uint64    `yaml:"ttl_seconds"`
	Preload        bool      `yaml:"preload"`
}

type ModeNGL struct {
	State  string  `yaml:"state"`
	Layers *uint64 `yaml:"layers,omitempty"`
}

type ModeTool struct {
	Profile Reference `yaml:"profile"`
	Active  bool      `yaml:"active"`
}

type ModeHarness struct {
	ID                   string   `yaml:"id"`
	IntegrationRevision  string   `yaml:"integration_revision"`
	RequiredCapabilities []string `yaml:"required_capabilities"`
}

type ModeWallModel struct {
	Result               string  `yaml:"result"`
	PredictedResidentMiB *uint64 `yaml:"predicted_resident_mib,omitempty"`
	Witness              string  `yaml:"witness,omitempty"`
	Reason               string  `yaml:"reason,omitempty"`
}

// ParseModeProfile accepts only canonical mode-profile YAML.
func ParseModeProfile(data []byte) (ModeProfile, error) {
	return decodeCanonicalProfile(data, "mode profile", MarshalModeProfile)
}

// MarshalModeProfile validates a mode profile and returns canonical YAML.
func MarshalModeProfile(profile ModeProfile) ([]byte, error) {
	return encodeCanonicalProfile(profile, "mode profile", ModeProfile.Validate)
}

// Validate enforces the v1 common envelope and closed mode composition.
func (p ModeProfile) Validate() error {
	var problems []string
	problem := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	validateProfileEnvelope(p.ProfileEnvelope, ModeSchemaV1, problem)
	validateModeEvidenceScopes(p, problem)
	validateModeSpec(p.Spec, p.ProfileEnvelope, problem)
	validateQualifiedMode(p, problem)

	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

func validateModeSpec(spec ModeSpec, envelope ProfileEnvelope, problem func(string, ...any)) {
	if spec.Foreground != "harness" && spec.Foreground != "local" && spec.Foreground != "none" {
		problem("spec.foreground %q must be harness, local, or none", spec.Foreground)
	}
	if len(envelope.Applicability.Foregrounds) != 1 || envelope.Applicability.Foregrounds[0] != spec.Foreground {
		problem("applicability.foregrounds must contain exactly spec.foreground %q", spec.Foreground)
	}

	bindings := validateModeBindings(spec.Bindings, problem)
	toolReferences := validateModeTools(spec.Tools, problem)
	harnessIDs := validateModeHarnesses(spec.Harnesses, problem)
	if !equalStrings(harnessIDs, envelope.Applicability.Harnesses) {
		problem("applicability.harnesses must exactly match spec.harnesses ids")
	}
	validateModeRoleBindings(spec.RoleBindings, bindings, envelope.Roles, problem)
	validateModeDependencies(envelope.Dependencies, spec.Bindings, toolReferences, problem)
	validateModeWallModel(spec.WallModel, envelope.Evidence, problem)
	validateModeForeground(spec, bindings, envelope.DataBoundary, problem)
}

func validateModeBindings(bindings []ModeBinding, problem func(string, ...any)) map[string]ModeBinding {
	result := map[string]ModeBinding{}
	previous := ""
	seenRuntimes := map[string]bool{}
	for index, binding := range bindings {
		location := fmt.Sprintf("spec.bindings[%d]", index)
		validateStableID(location+".id", binding.ID, problem)
		if index > 0 && binding.ID <= previous {
			problem("spec.bindings must be unique and sorted by id")
		}
		previous = binding.ID
		result[binding.ID] = binding
		validateStableID(location+".role", binding.Role, problem)
		validateRuntimeReference(location+".runtime_profile", binding.RuntimeProfile, ModelRuntimeSchemaV1, problem)
		runtimeIdentity := referenceExactIdentity(binding.RuntimeProfile)
		if seenRuntimes[runtimeIdentity] {
			problem("spec.bindings repeats runtime profile %s@%d", binding.RuntimeProfile.ID, binding.RuntimeProfile.Revision)
		}
		seenRuntimes[runtimeIdentity] = true
		if binding.Placement != "on-demand" && binding.Placement != "resident" {
			problem("%s.placement %q must be on-demand or resident", location, binding.Placement)
		}
		validateModeNGL(location+".ngl", binding.NGL, problem)
		if binding.Preload && binding.Placement != "resident" {
			problem("%s.preload is allowed only for resident placement", location)
		}
	}
	return result
}

func validateModeNGL(location string, ngl ModeNGL, problem func(string, ...any)) {
	switch ngl.State {
	case "engine-default":
		if ngl.Layers != nil {
			problem("%s.layers must be absent when state is engine-default", location)
		}
	case "explicit":
		if ngl.Layers == nil {
			problem("%s.layers is required when state is explicit", location)
		}
	default:
		problem("%s.state %q must be engine-default or explicit", location, ngl.State)
	}
}

func validateModeTools(tools []ModeTool, problem func(string, ...any)) []Reference {
	previous := ""
	result := make([]Reference, 0, len(tools))
	for index, tool := range tools {
		location := fmt.Sprintf("spec.tools[%d].profile", index)
		validateRuntimeReference(location, tool.Profile, ToolSchemaV1, problem)
		exactIdentity := referenceExactIdentity(tool.Profile)
		if index > 0 && exactIdentity <= previous {
			problem("spec.tools must be unique and sorted by exact profile identity")
		}
		previous = exactIdentity
		result = append(result, tool.Profile)
	}
	return result
}

func validateModeHarnesses(harnesses []ModeHarness, problem func(string, ...any)) []string {
	previous := ""
	result := make([]string, 0, len(harnesses))
	for index, harness := range harnesses {
		location := fmt.Sprintf("spec.harnesses[%d]", index)
		validateStableID(location+".id", harness.ID, problem)
		if index > 0 && harness.ID <= previous {
			problem("spec.harnesses must be unique and sorted by id")
		}
		previous = harness.ID
		result = append(result, harness.ID)
		if !exactRevisionIDPattern.MatchString(harness.IntegrationRevision) {
			problem("%s.integration_revision %q is not an exact revision", location, harness.IntegrationRevision)
		}
		validateSortedStableIDs(location+".required_capabilities", harness.RequiredCapabilities, true, problem)
	}
	return result
}

func validateModeRoleBindings(roleBindings map[string]string, bindings map[string]ModeBinding, roles []string, problem func(string, ...any)) {
	keys := make([]string, 0, len(roleBindings))
	for role := range roleBindings {
		keys = append(keys, role)
	}
	sort.Strings(keys)
	if !equalStrings(keys, roles) {
		problem("spec.role_bindings keys must exactly match roles")
	}
	for _, role := range keys {
		validateStableID("spec.role_bindings role", role, problem)
		bindingID := roleBindings[role]
		binding, ok := bindings[bindingID]
		if !ok {
			problem("spec.role_bindings[%q] references unknown binding %q", role, bindingID)
		} else if binding.Role != role {
			problem("spec.role_bindings[%q] references binding %q with role %q", role, bindingID, binding.Role)
		}
	}
}

func validateModeDependencies(dependencies []ProfileDependency, bindings []ModeBinding, toolReferences []Reference, problem func(string, ...any)) {
	want := make([]ProfileDependency, 0, len(bindings)+len(toolReferences))
	seenRuntimes := map[string]bool{}
	for _, binding := range bindings {
		identity := referenceExactIdentity(binding.RuntimeProfile)
		if !seenRuntimes[identity] {
			want = append(want, ProfileDependency{Relationship: "runtime", Profile: binding.RuntimeProfile})
			seenRuntimes[identity] = true
		}
	}
	for _, reference := range toolReferences {
		want = append(want, ProfileDependency{Relationship: "tool", Profile: reference})
	}
	sort.Slice(want, func(left, right int) bool {
		return want[left].Relationship+"\x00"+referenceExactIdentity(want[left].Profile) < want[right].Relationship+"\x00"+referenceExactIdentity(want[right].Profile)
	})
	if len(dependencies) != len(want) {
		problem("mode dependencies must exactly contain every runtime and tool profile")
		return
	}
	for index := range want {
		if dependencies[index] != want[index] {
			problem("dependencies[%d] must exactly match the mode runtime/tool closure", index)
		}
	}
}

func validateModeWallModel(wall ModeWallModel, evidence []ProfileEvidence, problem func(string, ...any)) {
	switch wall.Result {
	case "fit", "does-not-fit":
		if wall.PredictedResidentMiB == nil {
			problem("spec.wall_model.predicted_resident_mib is required for %s", wall.Result)
		}
		if !stableIDPattern.MatchString(wall.Witness) {
			problem("spec.wall_model.witness %q is not a lowercase stable id", wall.Witness)
		} else if !evidenceHasID(evidence, wall.Witness) {
			problem("spec.wall_model.witness references unknown evidence id %q", wall.Witness)
		}
		if wall.Reason != "" {
			problem("spec.wall_model.reason must be absent for %s", wall.Result)
		}
	case "not-applicable", "unmeasured":
		validateLine("spec.wall_model.reason", wall.Reason, problem)
		if wall.PredictedResidentMiB != nil || wall.Witness != "" {
			problem("spec.wall_model prediction and witness must be absent for %s", wall.Result)
		}
	default:
		problem("spec.wall_model.result %q must be fit, does-not-fit, not-applicable, or unmeasured", wall.Result)
	}
}

func validateModeForeground(spec ModeSpec, bindings map[string]ModeBinding, boundary ProfileDataBoundary, problem func(string, ...any)) {
	switch spec.Foreground {
	case "local":
		bindingID, ok := spec.RoleBindings["coder"]
		if !ok {
			problem("local foreground requires a coder role binding")
		} else if binding, exists := bindings[bindingID]; !exists || binding.Placement != "resident" {
			problem("local foreground coder binding must be resident")
		}
	case "harness":
		if len(spec.Harnesses) == 0 {
			problem("harness foreground requires at least one harness")
		}
	case "none":
		if len(spec.Bindings) != 0 || len(spec.Tools) != 0 || len(spec.Harnesses) != 0 || len(spec.RoleBindings) != 0 {
			problem("none foreground mode must have no bindings, tools, harnesses, or role bindings")
		}
		if spec.WallModel.Result != "not-applicable" {
			problem("none foreground mode wall_model must be not-applicable")
		}
		if boundary.Inference != "not-applicable" || len(boundary.Network) != 0 || len(boundary.Reads) != 0 || len(boundary.Writes) != 0 {
			problem("none foreground mode data_boundary must be not-applicable and empty")
		}
	}
}

func validateModeEvidenceScopes(profile ModeProfile, problem func(string, ...any)) {
	for index, evidence := range profile.Evidence {
		if !scopeReferenceEqualsReference(evidence.Scope.ModeProfile, Reference{
			Schema: profile.Schema, ID: profile.ID, Revision: profile.Revision,
		}) {
			problem("evidence[%d].scope.mode_profile must identify the containing mode", index)
		}
	}
}

func evidenceHasID(evidence []ProfileEvidence, id string) bool {
	for _, item := range evidence {
		if item.ID == id {
			return true
		}
	}
	return false
}
