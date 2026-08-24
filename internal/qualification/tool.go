package qualification

import "fmt"

// ToolProfile pins one tool core and every model-visible transport,
// permission, backend-role, and loud-failure boundary. It is eligibility data,
// never consent to expose or execute the tool.
type ToolProfile struct {
	ProfileEnvelope `yaml:",inline"`
	Spec            ToolSpec `yaml:"spec"`
}

type ToolSpec struct {
	Core             ToolCore             `yaml:"core"`
	Transports       []ToolTransport      `yaml:"transports"`
	Permissions      ToolPermissions      `yaml:"permissions"`
	Backend          ToolBackend          `yaml:"backend"`
	FailureSemantics ToolFailureSemantics `yaml:"failure_semantics"`
}

type ToolCore struct {
	Source            ToolSource `yaml:"source"`
	InterfaceRevision string     `yaml:"interface_revision"`
}

type ToolSource struct {
	Kind       string `yaml:"kind"`
	Repository string `yaml:"repository"`
	Revision   string `yaml:"revision"`
	SHA256     string `yaml:"sha256"`
}

type ToolTransport struct {
	Harness             string                    `yaml:"harness"`
	IntegrationRevision string                    `yaml:"integration_revision"`
	Protocol            string                    `yaml:"protocol"`
	RequestSchema       string                    `yaml:"request_schema"`
	ResultSchema        string                    `yaml:"result_schema"`
	DescriptionSHA256   string                    `yaml:"description_sha256"`
	Deviations          []ToolAffordanceDeviation `yaml:"affordance_deviations"`
}

type ToolAffordanceDeviation struct {
	ID       string   `yaml:"id"`
	Summary  string   `yaml:"summary"`
	Effect   string   `yaml:"effect"`
	Evidence []string `yaml:"evidence"`
}

type ToolPermissions struct {
	Reads    []string `yaml:"reads"`
	Writes   []string `yaml:"writes"`
	Executes []string `yaml:"executes"`
	Network  []string `yaml:"network"`
}

type ToolBackend struct {
	RequiredRoles []string `yaml:"required_roles"`
	OptionalRoles []string `yaml:"optional_roles"`
}

type ToolFailureSemantics struct {
	InvalidInput       string `yaml:"invalid_input"`
	PermissionDenied   string `yaml:"permission_denied"`
	BackendUnavailable string `yaml:"backend_unavailable"`
	PartialEffect      string `yaml:"partial_effect"`
}

// ParseToolProfile accepts only canonical tool-profile YAML.
func ParseToolProfile(data []byte) (ToolProfile, error) {
	return decodeCanonicalProfile(data, "tool profile", MarshalToolProfile)
}

// MarshalToolProfile validates a tool profile and returns canonical YAML.
func MarshalToolProfile(profile ToolProfile) ([]byte, error) {
	return encodeCanonicalProfile(profile, "tool profile", ToolProfile.Validate)
}

// Validate enforces the v1 common envelope and closed tool body.
func (p ToolProfile) Validate() error {
	var problems []string
	problem := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	validateProfileEnvelope(p.ProfileEnvelope, ToolSchemaV1, problem)
	if len(p.Dependencies) != 0 {
		problem("tool dependencies must be empty")
	}
	validateToolEvidenceScopes(p, problem)
	validateToolSpec(p.Spec, p.ProfileEnvelope, problem)

	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

func validateToolEvidenceScopes(profile ToolProfile, problem func(string, ...any)) {
	for index, evidence := range profile.Evidence {
		if !scopeReferenceEqualsReference(evidence.Scope.ToolProfile, Reference{
			Schema: profile.Schema, ID: profile.ID, Revision: profile.Revision,
		}) {
			problem("evidence[%d].scope.tool_profile must identify the containing tool", index)
		}
	}
}

func validateToolSpec(spec ToolSpec, envelope ProfileEnvelope, problem func(string, ...any)) {
	validateToolCore(spec.Core, problem)
	transportHarnesses := validateToolTransports(spec.Transports, envelope.Evidence, problem)
	if !equalStrings(transportHarnesses, envelope.Applicability.Harnesses) {
		problem("applicability.harnesses must exactly match spec.transports harnesses")
	}
	validateToolPermissions(spec.Permissions, envelope.DataBoundary, problem)
	validateToolBackend(spec.Backend, problem)
	validateToolFailureSemantics(spec.FailureSemantics, problem)
}

func validateToolCore(core ToolCore, problem func(string, ...any)) {
	if core.Source.Kind != "github" && core.Source.Kind != "upstream-release" {
		problem("spec.core.source.kind %q must be github or upstream-release", core.Source.Kind)
	}
	if !repositoryPattern.MatchString(core.Source.Repository) {
		problem("spec.core.source.repository %q must be owner/name", core.Source.Repository)
	}
	if !commitSHA1Pattern.MatchString(core.Source.Revision) {
		problem("spec.core.source.revision must be an exact 40-character lowercase commit hash")
	}
	if !sha256Pattern.MatchString(core.Source.SHA256) {
		problem("spec.core.source.sha256 must be 64 lowercase hexadecimal characters")
	}
	if !exactRevisionIDPattern.MatchString(core.InterfaceRevision) {
		problem("spec.core.interface_revision %q is not an exact revision", core.InterfaceRevision)
	}
}

func validateToolTransports(transports []ToolTransport, evidence []ProfileEvidence, problem func(string, ...any)) []string {
	if len(transports) == 0 {
		problem("spec.transports must not be empty")
	}
	evidenceIDs := map[string]bool{}
	for _, item := range evidence {
		evidenceIDs[item.ID] = true
	}
	previous := ""
	seenHarnesses := map[string]bool{}
	harnesses := make([]string, 0, len(transports))
	for index, transport := range transports {
		location := fmt.Sprintf("spec.transports[%d]", index)
		validateStableID(location+".harness", transport.Harness, problem)
		if seenHarnesses[transport.Harness] {
			problem("spec.transports repeats harness %q", transport.Harness)
		}
		seenHarnesses[transport.Harness] = true
		harnesses = append(harnesses, transport.Harness)
		if !exactRevisionIDPattern.MatchString(transport.IntegrationRevision) {
			problem("%s.integration_revision %q is not an exact revision", location, transport.IntegrationRevision)
		}
		if !exactRevisionIDPattern.MatchString(transport.Protocol) {
			problem("%s.protocol %q is not an exact revision", location, transport.Protocol)
		}
		if !schemaIDPattern.MatchString(transport.RequestSchema) {
			problem("%s.request_schema %q is not a versioned schema id", location, transport.RequestSchema)
		}
		if !schemaIDPattern.MatchString(transport.ResultSchema) {
			problem("%s.result_schema %q is not a versioned schema id", location, transport.ResultSchema)
		}
		if !sha256Pattern.MatchString(transport.DescriptionSHA256) {
			problem("%s.description_sha256 must be 64 lowercase hexadecimal characters", location)
		}
		validateToolDeviations(location+".affordance_deviations", transport.Deviations, evidenceIDs, problem)
		exactIdentity := transport.Harness + "\x00" + transport.IntegrationRevision
		if index > 0 && exactIdentity <= previous {
			problem("spec.transports must be unique and sorted by harness and integration revision")
		}
		previous = exactIdentity
	}
	return harnesses
}

func validateToolDeviations(location string, deviations []ToolAffordanceDeviation, evidenceIDs map[string]bool, problem func(string, ...any)) {
	previous := ""
	for index, deviation := range deviations {
		itemLocation := fmt.Sprintf("%s[%d]", location, index)
		validateStableID(itemLocation+".id", deviation.ID, problem)
		if index > 0 && deviation.ID <= previous {
			problem("%s must be unique and sorted by id", location)
		}
		previous = deviation.ID
		validateLine(itemLocation+".summary", deviation.Summary, problem)
		validateLine(itemLocation+".effect", deviation.Effect, problem)
		validateSortedStableIDs(itemLocation+".evidence", deviation.Evidence, false, problem)
		for _, evidenceID := range deviation.Evidence {
			if !evidenceIDs[evidenceID] {
				problem("%s.evidence references unknown evidence id %q", itemLocation, evidenceID)
			}
		}
	}
}

func validateToolPermissions(permissions ToolPermissions, boundary ProfileDataBoundary, problem func(string, ...any)) {
	validateSortedStableIDs("spec.permissions.reads", permissions.Reads, true, problem)
	validateSortedStableIDs("spec.permissions.writes", permissions.Writes, true, problem)
	validateSortedStableIDs("spec.permissions.executes", permissions.Executes, true, problem)
	validateSortedStableIDs("spec.permissions.network", permissions.Network, true, problem)
	if !equalStrings(permissions.Reads, boundary.Reads) {
		problem("spec.permissions.reads must exactly match data_boundary.reads")
	}
	if !equalStrings(permissions.Writes, boundary.Writes) {
		problem("spec.permissions.writes must exactly match data_boundary.writes")
	}
	networkPurposes := make([]string, 0, len(boundary.Network))
	for _, use := range boundary.Network {
		if !contains(networkPurposes, use.Purpose) {
			networkPurposes = append(networkPurposes, use.Purpose)
		}
	}
	if !equalStrings(permissions.Network, networkPurposes) {
		problem("spec.permissions.network must exactly match data_boundary.network purposes")
	}
}

func validateToolBackend(backend ToolBackend, problem func(string, ...any)) {
	validateSortedStableIDs("spec.backend.required_roles", backend.RequiredRoles, true, problem)
	validateSortedStableIDs("spec.backend.optional_roles", backend.OptionalRoles, true, problem)
	for _, role := range backend.RequiredRoles {
		if contains(backend.OptionalRoles, role) {
			problem("spec.backend role %q cannot be both required and optional", role)
		}
	}
}

func validateToolFailureSemantics(semantics ToolFailureSemantics, problem func(string, ...any)) {
	if semantics.InvalidInput != "refuse" {
		problem("spec.failure_semantics.invalid_input must be refuse")
	}
	if semantics.PermissionDenied != "refuse" {
		problem("spec.failure_semantics.permission_denied must be refuse")
	}
	if semantics.BackendUnavailable != "propagate-error" && semantics.BackendUnavailable != "refuse" {
		problem("spec.failure_semantics.backend_unavailable must be propagate-error or refuse")
	}
	if semantics.PartialEffect != "not-applicable" && semantics.PartialEffect != "report-partial" {
		problem("spec.failure_semantics.partial_effect must be not-applicable or report-partial")
	}
}

func equalStrings(left, right []string) bool {
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
