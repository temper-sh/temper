package qualification

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

// ProductPromotionPacket is the immutable Labs review input for one exact C7
// profile revision. Candidate.Spec is one member of the closed
// ProductPromotionSpec family.
type ProductPromotionPacket struct {
	Schema               string                       `yaml:"schema"`
	ID                   string                       `yaml:"id"`
	Revision             uint64                       `yaml:"revision"`
	Supersedes           *MaterialReference           `yaml:"supersedes,omitempty"`
	Target               ProductPromotionTarget       `yaml:"target"`
	Decision             ProductPromotionDecision     `yaml:"decision"`
	Evidence             []ProductPromotionEvidence   `yaml:"evidence"`
	Candidate            ProductPromotionCandidate    `yaml:"candidate"`
	Sanitization         ProductPromotionSanitization `yaml:"sanitization"`
	CatalogConsideration ProductCatalogConsideration  `yaml:"catalog_consideration"`
}

type ProductPromotionTarget struct {
	Schema     string     `yaml:"schema"`
	ID         string     `yaml:"id"`
	Revision   uint64     `yaml:"revision"`
	Supersedes *Reference `yaml:"supersedes,omitempty"`
}

type ProductPromotionDecision struct {
	QualificationStatus      string                     `yaml:"qualification_status"`
	QualificationReason      string                     `yaml:"qualification_reason"`
	LifecycleStatus          string                     `yaml:"lifecycle_status"`
	LifecycleReason          string                     `yaml:"lifecycle_reason"`
	DecidedAt                string                     `yaml:"decided_at"`
	Reviewers                []string                   `yaml:"reviewers"`
	AcceptedClaims           []string                   `yaml:"accepted_claims"`
	ForbiddenGeneralizations []string                   `yaml:"forbidden_generalizations"`
	Confounds                []ProductPromotionConfound `yaml:"confounds"`
	Gates                    []ProductPromotionGate     `yaml:"gates"`
}

type ProductPromotionConfound struct {
	ID          string `yaml:"id"`
	Effect      string `yaml:"effect"`
	Disposition string `yaml:"disposition"`
}

type ProductPromotionGate struct {
	ID          string   `yaml:"id"`
	Result      string   `yaml:"result"`
	Evidence    []string `yaml:"evidence"`
	Explanation string   `yaml:"explanation"`
}

type ProductPromotionEvidence struct {
	ID           string                           `yaml:"id"`
	Claims       []string                         `yaml:"claims"`
	Sources      []ProductPromotionEvidenceSource `yaml:"sources"`
	PublicSource ProductPromotionPublicSource     `yaml:"public_source"`
	Scope        ProductPromotionEvidenceScope    `yaml:"scope"`
}

type ProductPromotionEvidenceSource struct {
	Kind           string `yaml:"kind"`
	Schema         string `yaml:"schema"`
	ID             string `yaml:"id"`
	Revision       uint64 `yaml:"revision"`
	Locator        string `yaml:"locator"`
	SHA256         string `yaml:"sha256"`
	Classification string `yaml:"classification"`
}

type ProductPromotionPublicSource struct {
	Kind     string `yaml:"kind"`
	Schema   string `yaml:"schema,omitempty"`
	ID       string `yaml:"id,omitempty"`
	Revision uint64 `yaml:"revision,omitempty"`
	SHA256   string `yaml:"sha256,omitempty"`
}

// ProductPromotionEvidenceScope carries C7 scope material without its derived
// key. Temper computes the key while compiling the public profile.
type ProductPromotionEvidenceScope struct {
	ArtifactProfile *ScopeReference           `yaml:"artifact_profile,omitempty"`
	EngineProfile   *ScopeReference           `yaml:"engine_profile,omitempty"`
	RuntimeProfile  *ScopeReference           `yaml:"runtime_profile,omitempty"`
	ToolProfile     *ScopeReference           `yaml:"tool_profile,omitempty"`
	ModeProfile     *ScopeReference           `yaml:"mode_profile,omitempty"`
	ActivityProfile *ScopeReference           `yaml:"activity_profile,omitempty"`
	MachineBucket   *Reference                `yaml:"machine_bucket,omitempty"`
	Mode            string                    `yaml:"mode,omitempty"`
	CoResidents     []ProfileCoResident       `yaml:"co_residents"`
	Harnesses       []ProfileHarnessWitness   `yaml:"harnesses"`
	Conditions      ProfileEvidenceConditions `yaml:"conditions"`
}

type ProductPromotionCandidate struct {
	ProductPromotionCandidateCommon `yaml:",inline"`
	Spec                            ProductPromotionSpec `yaml:"-"`
}

type ProductPromotionCandidateCommon struct {
	Title                string                       `yaml:"title"`
	Summary              string                       `yaml:"summary"`
	WhatThisMeans        string                       `yaml:"what_this_means"`
	Roles                []string                     `yaml:"roles"`
	Applicability        ProfileApplicability         `yaml:"applicability"`
	Dependencies         []ProfileDependency          `yaml:"dependencies"`
	DataBoundary         ProfileDataBoundary          `yaml:"data_boundary"`
	KnownFailures        []ProfileKnownFailure        `yaml:"known_failures"`
	InvalidationTriggers []ProfileInvalidationTrigger `yaml:"invalidation_triggers"`
}

// ProductPromotionSpec is a closed family. It prevents a generic untyped C8
// payload from bypassing the target C7 schema.
type ProductPromotionSpec interface {
	productPromotionSchema() string
}

type PromotionModelArtifactSpec struct{ ModelArtifactSpec }
type PromotionEngineSpec struct{ EngineSpec }
type PromotionModelRuntimeSpec struct{ ModelRuntimeSpec }
type PromotionToolSpec struct{ ToolSpec }
type PromotionModeSpec struct{ ModeSpec }
type PromotionActivitySpec struct{ ActivitySpec }

func (PromotionModelArtifactSpec) productPromotionSchema() string { return ModelArtifactSchemaV1 }
func (PromotionEngineSpec) productPromotionSchema() string        { return EngineSchemaV1 }
func (PromotionModelRuntimeSpec) productPromotionSchema() string  { return ModelRuntimeSchemaV1 }
func (PromotionToolSpec) productPromotionSchema() string          { return ToolSchemaV1 }
func (PromotionModeSpec) productPromotionSchema() string          { return ModeSchemaV1 }
func (PromotionActivitySpec) productPromotionSchema() string      { return ActivitySchemaV1 }

type ProductPromotionSanitization struct {
	PublicCandidateReviewed bool                        `yaml:"public_candidate_reviewed"`
	ExcludedClasses         []string                    `yaml:"excluded_classes"`
	Redactions              []ProductPromotionRedaction `yaml:"redactions"`
	ReviewerStatement       string                      `yaml:"reviewer_statement"`
}

type ProductPromotionRedaction struct {
	Source    string `yaml:"source"`
	Class     string `yaml:"class"`
	Treatment string `yaml:"treatment"`
}

type ProductCatalogConsideration struct {
	RecommendationReview string      `yaml:"recommendation_review"`
	Comparisons          []Reference `yaml:"comparisons"`
	Note                 string      `yaml:"note"`
}

type promotionHeader struct {
	Target ProductPromotionTarget `yaml:"target"`
}

type productPromotionDocument[T any] struct {
	Schema               string                               `yaml:"schema"`
	ID                   string                               `yaml:"id"`
	Revision             uint64                               `yaml:"revision"`
	Supersedes           *MaterialReference                   `yaml:"supersedes,omitempty"`
	Target               ProductPromotionTarget               `yaml:"target"`
	Decision             ProductPromotionDecision             `yaml:"decision"`
	Evidence             []ProductPromotionEvidence           `yaml:"evidence"`
	Candidate            productPromotionCandidateDocument[T] `yaml:"candidate"`
	Sanitization         ProductPromotionSanitization         `yaml:"sanitization"`
	CatalogConsideration ProductCatalogConsideration          `yaml:"catalog_consideration"`
}

type productPromotionCandidateDocument[T any] struct {
	ProductPromotionCandidateCommon `yaml:",inline"`
	Spec                            T `yaml:"spec"`
}

// ParseProductPromotionPacket accepts only a canonical, closed C8 packet.
func ParseProductPromotionPacket(data []byte) (ProductPromotionPacket, error) {
	var header promotionHeader
	if err := yaml.Unmarshal(data, &header); err != nil {
		return ProductPromotionPacket{}, fmt.Errorf("decode product promotion header: %w", err)
	}

	switch header.Target.Schema {
	case ModelArtifactSchemaV1:
		return decodeProductPromotion[ModelArtifactSpec](data, func(spec ModelArtifactSpec) ProductPromotionSpec {
			return PromotionModelArtifactSpec{ModelArtifactSpec: spec}
		})
	case EngineSchemaV1:
		return decodeProductPromotion[EngineSpec](data, func(spec EngineSpec) ProductPromotionSpec {
			return PromotionEngineSpec{EngineSpec: spec}
		})
	case ModelRuntimeSchemaV1:
		return decodeProductPromotion[ModelRuntimeSpec](data, func(spec ModelRuntimeSpec) ProductPromotionSpec {
			return PromotionModelRuntimeSpec{ModelRuntimeSpec: spec}
		})
	case ToolSchemaV1:
		return decodeProductPromotion[ToolSpec](data, func(spec ToolSpec) ProductPromotionSpec {
			return PromotionToolSpec{ToolSpec: spec}
		})
	case ModeSchemaV1:
		return decodeProductPromotion[ModeSpec](data, func(spec ModeSpec) ProductPromotionSpec {
			return PromotionModeSpec{ModeSpec: spec}
		})
	case ActivitySchemaV1:
		return decodeProductPromotion[ActivitySpec](data, func(spec ActivitySpec) ProductPromotionSpec {
			return PromotionActivitySpec{ActivitySpec: spec}
		})
	default:
		return ProductPromotionPacket{}, fmt.Errorf("decode product promotion: target schema %q is not a C7 profile schema", header.Target.Schema)
	}
}

// MarshalProductPromotionPacket validates a packet and returns canonical YAML.
func MarshalProductPromotionPacket(packet ProductPromotionPacket) ([]byte, error) {
	if err := packet.Validate(); err != nil {
		return nil, err
	}

	switch spec := packet.Candidate.Spec.(type) {
	case PromotionModelArtifactSpec:
		return encodeProductPromotion(productPromotionDocumentFor(packet, spec.ModelArtifactSpec))
	case PromotionEngineSpec:
		return encodeProductPromotion(productPromotionDocumentFor(packet, spec.EngineSpec))
	case PromotionModelRuntimeSpec:
		return encodeProductPromotion(productPromotionDocumentFor(packet, spec.ModelRuntimeSpec))
	case PromotionToolSpec:
		return encodeProductPromotion(productPromotionDocumentFor(packet, spec.ToolSpec))
	case PromotionModeSpec:
		return encodeProductPromotion(productPromotionDocumentFor(packet, spec.ModeSpec))
	case PromotionActivitySpec:
		return encodeProductPromotion(productPromotionDocumentFor(packet, spec.ActivitySpec))
	default:
		return nil, fmt.Errorf("marshal product promotion: candidate spec type %T is not supported", packet.Candidate.Spec)
	}
}

// Validate enforces the Labs writer envelope and the target's closed C7 body.
func (p ProductPromotionPacket) Validate() error {
	var problems []string
	problem := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	validateProductPromotionIdentity(p, problem)
	validateProductPromotionTarget(p.Target, problem)
	validateProductPromotionDecision(p.Target, p.Decision, problem)
	validateProductPromotionEvidence(p, problem)
	validateProductPromotionSanitization(p, problem)
	validateProductCatalogConsideration(p.CatalogConsideration, problem)
	if p.Candidate.Spec == nil {
		problem("candidate.spec must be one typed C7 body")
	} else if p.Candidate.Spec.productPromotionSchema() != p.Target.Schema {
		problem("candidate.spec schema is %q, want target schema %q", p.Candidate.Spec.productPromotionSchema(), p.Target.Schema)
	}
	validateQualifiedPromotionPacket(p, problem)

	if len(problems) == 0 {
		promotion := PromotionReference{Schema: ProductPromotionSchemaV1, ID: p.ID, Revision: p.Revision, SHA256: zeroSHA256}
		if _, err := compileProductPromotionProfile(p, promotion); err != nil {
			problem("candidate profile: %v", err)
		}
	}
	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

const zeroSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"

func decodeProductPromotion[T any](data []byte, wrap func(T) ProductPromotionSpec) (ProductPromotionPacket, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var document productPromotionDocument[T]
	if err := decoder.Decode(&document); err != nil {
		return ProductPromotionPacket{}, fmt.Errorf("decode product promotion: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return ProductPromotionPacket{}, errors.New("decode product promotion: multiple YAML documents are not allowed")
		}
		return ProductPromotionPacket{}, fmt.Errorf("decode product promotion: %w", err)
	}

	packet := ProductPromotionPacket{
		Schema: document.Schema, ID: document.ID, Revision: document.Revision,
		Supersedes: document.Supersedes, Target: document.Target, Decision: document.Decision,
		Evidence: document.Evidence,
		Candidate: ProductPromotionCandidate{
			ProductPromotionCandidateCommon: document.Candidate.ProductPromotionCandidateCommon,
			Spec:                            wrap(document.Candidate.Spec),
		},
		Sanitization: document.Sanitization, CatalogConsideration: document.CatalogConsideration,
	}
	canonical, err := MarshalProductPromotionPacket(packet)
	if err != nil {
		return ProductPromotionPacket{}, err
	}
	if !bytes.Equal(data, canonical) {
		return ProductPromotionPacket{}, errors.New("product promotion bytes are not canonical")
	}
	return packet, nil
}

func productPromotionDocumentFor[T any](packet ProductPromotionPacket, spec T) productPromotionDocument[T] {
	return productPromotionDocument[T]{
		Schema: packet.Schema, ID: packet.ID, Revision: packet.Revision,
		Supersedes: packet.Supersedes, Target: packet.Target, Decision: packet.Decision,
		Evidence: packet.Evidence,
		Candidate: productPromotionCandidateDocument[T]{
			ProductPromotionCandidateCommon: packet.Candidate.ProductPromotionCandidateCommon,
			Spec:                            spec,
		},
		Sanitization: packet.Sanitization, CatalogConsideration: packet.CatalogConsideration,
	}
}

func encodeProductPromotion[T any](document productPromotionDocument[T]) ([]byte, error) {
	var root yaml.Node
	if err := root.Encode(document); err != nil {
		return nil, fmt.Errorf("encode product promotion: %w", err)
	}
	sortMappingKeys(&root)
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(&root); err != nil {
		return nil, fmt.Errorf("encode product promotion: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("close product promotion encoder: %w", err)
	}
	return output.Bytes(), nil
}

func validateProductPromotionIdentity(packet ProductPromotionPacket, problem func(string, ...any)) {
	if packet.Schema != ProductPromotionSchemaV1 {
		problem("schema is %q, want %q", packet.Schema, ProductPromotionSchemaV1)
	}
	if !stableIDPattern.MatchString(packet.ID) {
		problem("id %q is not a lowercase stable id", packet.ID)
	}
	if packet.Revision == 0 {
		problem("revision must be greater than zero")
	}
	if packet.Revision == 1 && packet.Supersedes != nil {
		problem("initial packet revision must not supersede another packet")
	}
	if packet.Revision > 1 && packet.Supersedes == nil {
		problem("packet revision %d must supersede revision %d", packet.Revision, packet.Revision-1)
	}
	if packet.Supersedes != nil {
		validateMaterialReference("supersedes", *packet.Supersedes, problem)
		if packet.Supersedes.Schema != ProductPromotionSchemaV1 || packet.Supersedes.ID != packet.ID || packet.Supersedes.Revision+1 != packet.Revision {
			problem("supersedes must exactly identify the immediately preceding packet revision in this lineage")
		}
	}
}

func validateProductPromotionTarget(target ProductPromotionTarget, problem func(string, ...any)) {
	if _, ok := profileKinds[target.Schema]; !ok {
		problem("target.schema %q is not a C7 profile schema", target.Schema)
	}
	if !stableIDPattern.MatchString(target.ID) {
		problem("target.id %q is not a lowercase stable id", target.ID)
	}
	if target.Revision == 0 {
		problem("target.revision must be greater than zero")
	}
	if target.Revision == 1 && target.Supersedes != nil {
		problem("initial target revision must not supersede another profile")
	}
	if target.Revision > 1 && target.Supersedes == nil {
		problem("target revision %d must supersede revision %d", target.Revision, target.Revision-1)
	}
	if target.Supersedes != nil {
		validateReference("target.supersedes", *target.Supersedes, problem)
		if target.Supersedes.Schema != target.Schema || target.Supersedes.ID != target.ID || target.Supersedes.Revision+1 != target.Revision {
			problem("target.supersedes must exactly identify the immediately preceding target revision")
		}
	}
}

func validateProductPromotionDecision(target ProductPromotionTarget, decision ProductPromotionDecision, problem func(string, ...any)) {
	validateDisposition("decision.", target.Revision, decision.QualificationStatus, decision.QualificationReason, decision.LifecycleStatus, decision.LifecycleReason, problem)
	if _, err := time.Parse(time.RFC3339, decision.DecidedAt); err != nil {
		problem("decision.decided_at %q must be RFC 3339", decision.DecidedAt)
	}
	validateSortedStableIDs("decision.reviewers", decision.Reviewers, false, problem)
	validateSortedStableIDs("decision.accepted_claims", decision.AcceptedClaims, decision.QualificationStatus == QualificationStatusWatch, problem)
	validateSortedLines("decision.forbidden_generalizations", decision.ForbiddenGeneralizations, problem)

	previous := ""
	for index, confound := range decision.Confounds {
		location := fmt.Sprintf("decision.confounds[%d]", index)
		if !stableIDPattern.MatchString(confound.ID) {
			problem("%s.id %q is not a lowercase stable id", location, confound.ID)
		}
		if index > 0 && confound.ID <= previous {
			problem("decision.confounds must be unique and sorted by id")
		}
		previous = confound.ID
		validateLine(location+".effect", confound.Effect, problem)
		if confound.Disposition != "bounded" && confound.Disposition != "invalidates-claim" && confound.Disposition != "unresolved" {
			problem("%s.disposition %q is not supported", location, confound.Disposition)
		}
	}

	previous = ""
	for index, gate := range decision.Gates {
		location := fmt.Sprintf("decision.gates[%d]", index)
		if !stableIDPattern.MatchString(gate.ID) {
			problem("%s.id %q is not a lowercase stable id", location, gate.ID)
		}
		if index > 0 && gate.ID <= previous {
			problem("decision.gates must be unique and sorted by id")
		}
		previous = gate.ID
		if gate.Result != "fail" && gate.Result != "not-applicable" && gate.Result != "not-run" && gate.Result != "pass" {
			problem("%s.result %q is not supported", location, gate.Result)
		}
		validateSortedStableIDs(location+".evidence", gate.Evidence, gate.Result == "not-applicable" || gate.Result == "not-run", problem)
		validateLine(location+".explanation", gate.Explanation, problem)
	}
}

func validateProductPromotionEvidence(packet ProductPromotionPacket, problem func(string, ...any)) {
	evidenceIDs := map[string]bool{}
	claimSupport := map[string]bool{}
	previous := ""
	for index, evidence := range packet.Evidence {
		location := fmt.Sprintf("evidence[%d]", index)
		if !stableIDPattern.MatchString(evidence.ID) {
			problem("%s.id %q is not a lowercase stable id", location, evidence.ID)
		}
		if index > 0 && evidence.ID <= previous {
			problem("evidence must be unique and sorted by id")
		}
		previous = evidence.ID
		evidenceIDs[evidence.ID] = true
		validateSortedStableIDs(location+".claims", evidence.Claims, false, problem)
		for _, claim := range evidence.Claims {
			claimSupport[claim] = true
		}
		validateProductPromotionSources(location, evidence, problem)
		validateProductPromotionScope(location+".scope", evidence.Scope, packet.Target, problem)
	}
	for _, claim := range packet.Decision.AcceptedClaims {
		if !claimSupport[claim] {
			problem("decision.accepted_claims contains unsupported claim %q", claim)
		}
	}
	for gateIndex, gate := range packet.Decision.Gates {
		for _, evidenceID := range gate.Evidence {
			if !evidenceIDs[evidenceID] {
				problem("decision.gates[%d].evidence references unknown evidence id %q", gateIndex, evidenceID)
			}
		}
	}
}

func validateProductPromotionSources(location string, evidence ProductPromotionEvidence, problem func(string, ...any)) {
	if len(evidence.Sources) == 0 {
		problem("%s.sources must not be empty", location)
	}
	previous := ""
	for index, source := range evidence.Sources {
		sourceLocation := fmt.Sprintf("%s.sources[%d]", location, index)
		if source.Kind != "field-kit-runtime-profile" && source.Kind != "field-kit-session" && source.Kind != "labs-record" && source.Kind != "results-record" && source.Kind != "upstream-record" {
			problem("%s.kind %q is not supported", sourceLocation, source.Kind)
		}
		validateMaterialReference(sourceLocation, MaterialReference{Schema: source.Schema, ID: source.ID, Revision: source.Revision, SHA256: source.SHA256}, problem)
		validateLine(sourceLocation+".locator", source.Locator, problem)
		if source.Classification != "private" && source.Classification != "public" && source.Classification != "restricted" {
			problem("%s.classification %q is not supported", sourceLocation, source.Classification)
		}
		exactIdentity := source.ID + "\x00" + source.Kind + "\x00" + source.Schema + "\x00" + fmt.Sprintf("%020d", source.Revision) + "\x00" + source.SHA256
		if index > 0 && exactIdentity <= previous {
			problem("%s.sources must be unique and sorted by source identity", location)
		}
		previous = exactIdentity
	}

	public := evidence.PublicSource
	switch public.Kind {
	case "product-promotion":
		if public.Schema != "" || public.ID != "" || public.Revision != 0 || public.SHA256 != "" {
			problem("%s.public_source product-promotion must not supply its injected identity", location)
		}
	case "results-record":
		validateMaterialReference(location+".public_source", MaterialReference{Schema: public.Schema, ID: public.ID, Revision: public.Revision, SHA256: public.SHA256}, problem)
		if public.Schema == "field-kit-session/v1" || public.Schema == "field-kit-runtime-profile/v1" {
			problem("%s.public_source must not expose a raw Field Kit packet", location)
		}
	default:
		problem("%s.public_source.kind %q must be product-promotion or results-record", location, public.Kind)
	}
}

func validateProductPromotionScope(location string, source ProductPromotionEvidenceScope, target ProductPromotionTarget, problem func(string, ...any)) {
	scope := source.profileScope()
	key, err := EvidenceScopeKey(scope)
	if err != nil {
		problem("%s cannot compute scope key: %v", location, err)
		return
	}
	scope.Key = key
	envelope := ProfileEnvelope{Schema: target.Schema, ID: target.ID, Revision: target.Revision}
	validateOptionalScopeReference(location+".artifact_profile", scope.ArtifactProfile, envelope, problem)
	validateOptionalScopeReference(location+".engine_profile", scope.EngineProfile, envelope, problem)
	validateOptionalScopeReference(location+".runtime_profile", scope.RuntimeProfile, envelope, problem)
	validateOptionalScopeReference(location+".tool_profile", scope.ToolProfile, envelope, problem)
	validateOptionalScopeReference(location+".mode_profile", scope.ModeProfile, envelope, problem)
	validateOptionalScopeReference(location+".activity_profile", scope.ActivityProfile, envelope, problem)
	if scope.MachineBucket != nil {
		validateRuntimeReference(location+".machine_bucket", *scope.MachineBucket, MachineBucketSchemaV1, problem)
	}
	if scope.Mode != "" && !stableIDPattern.MatchString(scope.Mode) {
		problem("%s.mode %q is not a lowercase stable id", location, scope.Mode)
	}
	validateCoResidents(location+".co_residents", scope.CoResidents, problem)
	validateHarnessWitnesses(location+".harnesses", scope.Harnesses, problem)
	validateEvidenceConditions(location+".conditions", scope.Conditions, problem)
	validateScopeShape(location, scope, envelope, problem)
}

func (s ProductPromotionEvidenceScope) profileScope() ProfileEvidenceScope {
	return ProfileEvidenceScope{
		ArtifactProfile: s.ArtifactProfile, EngineProfile: s.EngineProfile,
		RuntimeProfile: s.RuntimeProfile, ToolProfile: s.ToolProfile,
		ModeProfile: s.ModeProfile, ActivityProfile: s.ActivityProfile,
		MachineBucket: s.MachineBucket, Mode: s.Mode, CoResidents: s.CoResidents,
		Harnesses: s.Harnesses, Conditions: s.Conditions,
	}
}

func validateProductPromotionSanitization(packet ProductPromotionPacket, problem func(string, ...any)) {
	sanitization := packet.Sanitization
	if !sanitization.PublicCandidateReviewed {
		problem("sanitization.public_candidate_reviewed must be true")
	}
	wantClasses := []string{"credentials", "machine-identifying-values-outside-the-C7-bucket", "private-corpus-content", "prompts-not-approved-for-publication", "raw-user-content"}
	if !equalStrings(sanitization.ExcludedClasses, wantClasses) {
		problem("sanitization.excluded_classes must contain the complete canonical exclusion set")
	}
	validateLine("sanitization.reviewer_statement", sanitization.ReviewerStatement, problem)
	sourceIDs := map[string]bool{}
	for _, evidence := range packet.Evidence {
		for _, source := range evidence.Sources {
			sourceIDs[source.ID] = true
		}
	}
	previous := ""
	for index, redaction := range sanitization.Redactions {
		location := fmt.Sprintf("sanitization.redactions[%d]", index)
		if !sourceIDs[redaction.Source] {
			problem("%s.source references unknown evidence source %q", location, redaction.Source)
		}
		if !contains(wantClasses, redaction.Class) {
			problem("%s.class %q is not in excluded_classes", location, redaction.Class)
		}
		if redaction.Treatment != "aggregated" && redaction.Treatment != "omitted" && redaction.Treatment != "replaced-by-public-record" {
			problem("%s.treatment %q is not supported", location, redaction.Treatment)
		}
		exactIdentity := redaction.Source + "\x00" + redaction.Class
		if index > 0 && exactIdentity <= previous {
			problem("sanitization.redactions must be unique and sorted by source and class")
		}
		previous = exactIdentity
	}
}

func validateProductCatalogConsideration(consideration ProductCatalogConsideration, problem func(string, ...any)) {
	if consideration.RecommendationReview != "separate" {
		problem("catalog_consideration.recommendation_review must be separate")
	}
	validateReferenceSet("catalog_consideration.comparisons", consideration.Comparisons, ModelRuntimeSchemaV1, problem)
	validateLine("catalog_consideration.note", consideration.Note, problem)
}

func sortedAcceptedClaims(evidence ProductPromotionEvidence, accepted []string) []string {
	result := make([]string, 0, len(evidence.Claims))
	for _, claim := range evidence.Claims {
		if contains(accepted, claim) {
			result = append(result, claim)
		}
	}
	sort.Strings(result)
	return result
}
