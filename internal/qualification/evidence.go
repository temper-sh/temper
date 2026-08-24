package qualification

import "fmt"

const EvidenceScopeSchemaV1 = "temper-qualification-evidence-scope/v1"

type evidenceScopeMaterial struct {
	Schema          string                    `yaml:"schema"`
	ArtifactProfile *ScopeReference           `yaml:"artifact_profile,omitempty"`
	EngineProfile   *ScopeReference           `yaml:"engine_profile,omitempty"`
	RuntimeProfile  *ScopeReference           `yaml:"runtime_profile,omitempty"`
	MachineBucket   *Reference                `yaml:"machine_bucket,omitempty"`
	Mode            string                    `yaml:"mode,omitempty"`
	CoResidents     []ProfileCoResident       `yaml:"co_residents"`
	Harnesses       []ProfileHarnessWitness   `yaml:"harnesses"`
	Conditions      ProfileEvidenceConditions `yaml:"conditions"`
}

// EvidenceScopeKey returns the SHA-256 of the canonical v1 scope material.
// Key itself is excluded from the preimage.
func EvidenceScopeKey(scope ProfileEvidenceScope) (string, error) {
	material := evidenceScopeMaterial{
		Schema: EvidenceScopeSchemaV1, ArtifactProfile: scope.ArtifactProfile,
		EngineProfile: scope.EngineProfile, RuntimeProfile: scope.RuntimeProfile,
		MachineBucket: scope.MachineBucket, Mode: scope.Mode,
		CoResidents: scope.CoResidents, Harnesses: scope.Harnesses, Conditions: scope.Conditions,
	}
	data, err := encodeCanonicalProfile(material, "evidence scope material", func(evidenceScopeMaterial) error { return nil })
	if err != nil {
		return "", err
	}
	return Digest(data), nil
}

func validateProfileEvidence(envelope ProfileEnvelope, problem func(string, ...any)) {
	previous := ""
	for index, evidence := range envelope.Evidence {
		location := fmt.Sprintf("evidence[%d]", index)
		if !stableIDPattern.MatchString(evidence.ID) {
			problem("%s.id %q is not a lowercase stable id", location, evidence.ID)
		}
		if index > 0 && evidence.ID <= previous {
			problem("evidence must be unique and sorted by id")
		}
		previous = evidence.ID

		validateEvidenceSource(location+".source", evidence.Source, envelope.Promotion, problem)
		validateSortedStableIDs(location+".claims", evidence.Claims, false, problem)
		validateEvidenceScope(location+".scope", evidence.Scope, envelope, problem)
	}
}

func validateEvidenceSource(location string, source ProfileEvidenceSource, promotion PromotionReference, problem func(string, ...any)) {
	validateMaterialReference(location, source.MaterialReference, problem)
	switch source.Kind {
	case "product-promotion":
		if source.Schema != promotion.Schema || source.ID != promotion.ID || source.Revision != promotion.Revision || source.SHA256 != promotion.SHA256 {
			problem("%s product-promotion identity must exactly match promotion", location)
		}
	case "results-record":
		if source.Schema == "field-kit-session/v1" || source.Schema == "field-kit-runtime-profile/v1" {
			problem("%s results-record source must not identify a raw Field Kit packet", location)
		}
	default:
		problem("%s.kind %q must be product-promotion or results-record", location, source.Kind)
	}
}

func validateEvidenceScope(location string, scope ProfileEvidenceScope, envelope ProfileEnvelope, problem func(string, ...any)) {
	if !sha256Pattern.MatchString(scope.Key) {
		problem("%s.key must be 64 lowercase hexadecimal characters", location)
	}
	validateOptionalScopeReference(location+".artifact_profile", scope.ArtifactProfile, envelope, problem)
	validateOptionalScopeReference(location+".engine_profile", scope.EngineProfile, envelope, problem)
	validateOptionalScopeReference(location+".runtime_profile", scope.RuntimeProfile, envelope, problem)
	if scope.MachineBucket != nil {
		validateReference(location+".machine_bucket", *scope.MachineBucket, problem)
		if scope.MachineBucket.Schema != MachineBucketSchemaV1 {
			problem("%s.machine_bucket schema is %q, want %q", location, scope.MachineBucket.Schema, MachineBucketSchemaV1)
		}
	}
	if scope.Mode != "" && !stableIDPattern.MatchString(scope.Mode) {
		problem("%s.mode %q is not a lowercase stable id", location, scope.Mode)
	}
	validateCoResidents(location+".co_residents", scope.CoResidents, problem)
	validateHarnessWitnesses(location+".harnesses", scope.Harnesses, problem)
	validateEvidenceConditions(location+".conditions", scope.Conditions, problem)
	validateScopeShape(location, scope, envelope, problem)

	wantKey, err := EvidenceScopeKey(scope)
	if err != nil {
		problem("%s.key cannot be computed: %v", location, err)
	} else if scope.Key != wantKey {
		problem("%s.key is %q, want recomputed %q", location, scope.Key, wantKey)
	}
}

func validateOptionalScopeReference(location string, reference *ScopeReference, envelope ProfileEnvelope, problem func(string, ...any)) {
	if reference == nil {
		return
	}
	if _, ok := profileKinds[reference.Schema]; !ok {
		problem("%s.schema %q is not a profile schema", location, reference.Schema)
	}
	if !stableIDPattern.MatchString(reference.ID) {
		problem("%s.id %q is not a lowercase stable id", location, reference.ID)
	}
	if reference.Revision == 0 {
		problem("%s.revision must be greater than zero", location)
	}
	isSelf := reference.Schema == envelope.Schema && reference.ID == envelope.ID && reference.Revision == envelope.Revision
	if isSelf {
		if reference.SHA256 != "" {
			problem("%s.sha256 must be absent for the containing profile", location)
		}
	} else if !sha256Pattern.MatchString(reference.SHA256) {
		problem("%s.sha256 must be 64 lowercase hexadecimal characters for another profile", location)
	}
}

func validateCoResidents(location string, residents []ProfileCoResident, problem func(string, ...any)) {
	previous := ""
	seenProfiles := map[string]bool{}
	for index, resident := range residents {
		itemLocation := fmt.Sprintf("%s[%d]", location, index)
		validateReference(itemLocation+".runtime_profile", resident.RuntimeProfile, problem)
		if resident.RuntimeProfile.Schema != ModelRuntimeSchemaV1 {
			problem("%s.runtime_profile schema is %q, want %q", itemLocation, resident.RuntimeProfile.Schema, ModelRuntimeSchemaV1)
		}
		if resident.Placement != "on-demand" && resident.Placement != "resident" {
			problem("%s.placement %q must be on-demand or resident", itemLocation, resident.Placement)
		}
		semanticIdentity := referenceSemanticIdentity(resident.RuntimeProfile)
		if seenProfiles[semanticIdentity] {
			problem("%s repeats runtime profile %s@%d", location, resident.RuntimeProfile.ID, resident.RuntimeProfile.Revision)
		}
		seenProfiles[semanticIdentity] = true
		exactIdentity := referenceExactIdentity(resident.RuntimeProfile) + "\x00" + resident.Placement
		if index > 0 && exactIdentity <= previous {
			problem("%s must be unique and sorted by runtime profile and placement", location)
		}
		previous = exactIdentity
	}
}

func validateHarnessWitnesses(location string, harnesses []ProfileHarnessWitness, problem func(string, ...any)) {
	previous := ""
	seenIDs := map[string]bool{}
	for index, harness := range harnesses {
		itemLocation := fmt.Sprintf("%s[%d]", location, index)
		if !stableIDPattern.MatchString(harness.ID) {
			problem("%s.id %q is not a lowercase stable id", itemLocation, harness.ID)
		}
		if !exactRevisionIDPattern.MatchString(harness.IntegrationRevision) {
			problem("%s.integration_revision %q is not an exact revision", itemLocation, harness.IntegrationRevision)
		}
		if seenIDs[harness.ID] {
			problem("%s repeats harness id %q", location, harness.ID)
		}
		seenIDs[harness.ID] = true
		exactIdentity := harness.ID + "\x00" + harness.IntegrationRevision
		if index > 0 && exactIdentity <= previous {
			problem("%s must be unique and sorted by id and integration revision", location)
		}
		previous = exactIdentity
	}
}

func validateEvidenceConditions(location string, conditions ProfileEvidenceConditions, problem func(string, ...any)) {
	validateEvidenceStringCondition(location+".os_build", conditions.OSBuild, []string{"not-applicable", "observed"}, problem)
	validateEvidenceIntegerCondition(location+".wired_limit_mib", conditions.WiredLimitMiB, problem)
	validateEvidenceStringCondition(location+".wired_limit_source", conditions.WiredLimitSource, []string{"not-applicable", "observed"}, problem)
	states := []string{"not-applicable", "observed", "unmeasured"}
	validateEvidenceStringCondition(location+".power", conditions.Power, states, problem)
	validateEvidenceStringCondition(location+".thermal", conditions.Thermal, states, problem)
	validateEvidenceStringCondition(location+".load", conditions.Load, states, problem)
}

func validateEvidenceStringCondition(location string, condition EvidenceStringCondition, states []string, problem func(string, ...any)) {
	if !contains(states, condition.State) {
		problem("%s.state %q is not supported", location, condition.State)
		return
	}
	if condition.State == "observed" {
		validateLine(location+".value", condition.Value, problem)
	} else if condition.Value != "" {
		problem("%s.value must be absent when state is %s", location, condition.State)
	}
}

func validateEvidenceIntegerCondition(location string, condition EvidenceIntegerCondition, problem func(string, ...any)) {
	switch condition.State {
	case "observed":
		if condition.Value <= 0 {
			problem("%s.value must be greater than zero when observed", location)
		}
	case "not-applicable":
		if condition.Value != 0 {
			problem("%s.value must be absent when state is not-applicable", location)
		}
	default:
		problem("%s.state %q must be not-applicable or observed", location, condition.State)
	}
}

func validateScopeShape(location string, scope ProfileEvidenceScope, envelope ProfileEnvelope, problem func(string, ...any)) {
	switch envelope.Schema {
	case ModelArtifactSchemaV1:
		requireSelfScopeReference(location+".artifact_profile", scope.ArtifactProfile, envelope, problem)
		if scope.EngineProfile != nil || scope.RuntimeProfile != nil || scope.MachineBucket != nil || scope.Mode != "" || len(scope.CoResidents) != 0 || len(scope.Harnesses) != 0 {
			problem("%s model-artifact scope must contain only its artifact identity and not-applicable conditions", location)
		}
		if !conditionsAreNotApplicable(scope.Conditions) {
			problem("%s model-artifact conditions must all be not-applicable", location)
		}
	case EngineSchemaV1:
		requireSelfScopeReference(location+".engine_profile", scope.EngineProfile, envelope, problem)
		if scope.ArtifactProfile != nil || scope.RuntimeProfile != nil || scope.Mode != "" || len(scope.CoResidents) != 0 || len(scope.Harnesses) != 0 {
			problem("%s engine scope cannot contain artifact, runtime, mode, co-resident, or harness dimensions", location)
		}
	case ModelRuntimeSchemaV1:
		requireSelfScopeReference(location+".runtime_profile", scope.RuntimeProfile, envelope, problem)
		if scope.ArtifactProfile == nil || scope.EngineProfile == nil || scope.MachineBucket == nil || scope.Mode == "" {
			problem("%s model-runtime scope requires artifact, engine, runtime, machine bucket, and mode", location)
		}
		if scope.Conditions.OSBuild.State != "observed" || scope.Conditions.WiredLimitMiB.State != "observed" || scope.Conditions.WiredLimitSource.State != "observed" {
			problem("%s model-runtime scope requires observed OS build, wired limit, and wired-limit source", location)
		}
		if scope.Conditions.Power.State != "observed" && scope.Conditions.Power.State != "unmeasured" {
			problem("%s model-runtime power condition must be observed or unmeasured", location)
		}
		if scope.Conditions.Thermal.State != "observed" && scope.Conditions.Thermal.State != "unmeasured" {
			problem("%s model-runtime thermal condition must be observed or unmeasured", location)
		}
		if scope.Conditions.Load.State != "observed" && scope.Conditions.Load.State != "unmeasured" {
			problem("%s model-runtime load condition must be observed or unmeasured", location)
		}
		for index, resident := range scope.CoResidents {
			if resident.RuntimeProfile.Schema == envelope.Schema && resident.RuntimeProfile.ID == envelope.ID && resident.RuntimeProfile.Revision == envelope.Revision {
				problem("%s.co_residents[%d] cannot name the containing runtime", location, index)
			}
		}
	}
}

func requireSelfScopeReference(location string, reference *ScopeReference, envelope ProfileEnvelope, problem func(string, ...any)) {
	if reference == nil {
		problem("%s must identify the containing profile", location)
		return
	}
	if reference.Schema != envelope.Schema || reference.ID != envelope.ID || reference.Revision != envelope.Revision || reference.SHA256 != "" {
		problem("%s must identify the containing profile without sha256", location)
	}
}

func conditionsAreNotApplicable(conditions ProfileEvidenceConditions) bool {
	return conditions.OSBuild.State == "not-applicable" &&
		conditions.WiredLimitMiB.State == "not-applicable" &&
		conditions.WiredLimitSource.State == "not-applicable" &&
		conditions.Power.State == "not-applicable" &&
		conditions.Thermal.State == "not-applicable" &&
		conditions.Load.State == "not-applicable"
}

func scopeReferenceEqualsReference(scope *ScopeReference, reference Reference) bool {
	return scope != nil && scope.Schema == reference.Schema && scope.ID == reference.ID && scope.Revision == reference.Revision && scope.SHA256 == reference.SHA256
}
