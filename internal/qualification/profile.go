package qualification

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const ProductPromotionSchemaV1 = "temper-labs-product-promotion/v1"

const (
	ProfileStatusWatch     = "WATCH"
	ProfileStatusLab       = "LAB"
	ProfileStatusQualified = "QUALIFIED"
	ProfileStatusRejected  = "REJECTED"
	ProfileStatusRetired   = "RETIRED"
)

var (
	schemaIDPattern        = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*/v[1-9][0-9]*$`)
	exactRevisionIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._/-][a-z0-9]+)*$`)
)

// ProfileEnvelope is the common immutable history and evidence boundary shared
// by all six C7 profile documents.
type ProfileEnvelope struct {
	Schema               string                       `yaml:"schema"`
	ID                   string                       `yaml:"id"`
	Revision             uint64                       `yaml:"revision"`
	Supersedes           *Reference                   `yaml:"supersedes,omitempty"`
	Status               string                       `yaml:"status"`
	StatusReason         string                       `yaml:"status_reason"`
	Title                string                       `yaml:"title"`
	Summary              string                       `yaml:"summary"`
	WhatThisMeans        string                       `yaml:"what_this_means"`
	Roles                []string                     `yaml:"roles"`
	Applicability        ProfileApplicability         `yaml:"applicability"`
	Dependencies         []ProfileDependency          `yaml:"dependencies"`
	DataBoundary         ProfileDataBoundary          `yaml:"data_boundary"`
	KnownFailures        []ProfileKnownFailure        `yaml:"known_failures"`
	InvalidationTriggers []ProfileInvalidationTrigger `yaml:"invalidation_triggers"`
	Evidence             []ProfileEvidence            `yaml:"evidence"`
	Promotion            PromotionReference           `yaml:"promotion"`
}

type ProfileApplicability struct {
	MachineBuckets []Reference `yaml:"machine_buckets"`
	Foregrounds    []string    `yaml:"foregrounds"`
	Harnesses      []string    `yaml:"harnesses"`
	Explanation    string      `yaml:"explanation"`
}

type ProfileDependency struct {
	Relationship string    `yaml:"relationship"`
	Profile      Reference `yaml:"profile"`
}

type ProfileDataBoundary struct {
	Inference      string              `yaml:"inference"`
	Credentials    string              `yaml:"credentials"`
	Network        []ProfileNetworkUse `yaml:"network"`
	Reads          []string            `yaml:"reads"`
	Writes         []string            `yaml:"writes"`
	Telemetry      string              `yaml:"telemetry"`
	EvidenceExport string              `yaml:"evidence_export"`
}

type ProfileNetworkUse struct {
	Purpose     string `yaml:"purpose"`
	Destination string `yaml:"destination"`
	Timing      string `yaml:"timing"`
}

type ProfileKnownFailure struct {
	ID       string   `yaml:"id"`
	Summary  string   `yaml:"summary"`
	Effect   string   `yaml:"effect"`
	Evidence []string `yaml:"evidence"`
}

type ProfileInvalidationTrigger struct {
	ID          string `yaml:"id"`
	Condition   string `yaml:"condition"`
	Consequence string `yaml:"consequence"`
}

// ProfileEvidence is the typed public evidence inventory. Raw Labs and Field
// Kit locators have no representation on this side of the C8 boundary.
type ProfileEvidence struct {
	ID     string                `yaml:"id"`
	Source ProfileEvidenceSource `yaml:"source"`
	Claims []string              `yaml:"claims"`
	Scope  ProfileEvidenceScope  `yaml:"scope"`
}

type ProfileEvidenceSource struct {
	Kind              string `yaml:"kind"`
	MaterialReference `yaml:",inline"`
}

// MaterialReference identifies immutable bytes outside C7 without treating
// their schema as a qualification-document schema.
type MaterialReference struct {
	Schema   string `yaml:"schema"`
	ID       string `yaml:"id"`
	Revision uint64 `yaml:"revision"`
	SHA256   string `yaml:"sha256"`
}

// ScopeReference permits the current profile's semantic identity to omit its
// self-digest while dependencies retain their exact material digest.
type ScopeReference struct {
	Schema   string `yaml:"schema"`
	ID       string `yaml:"id"`
	Revision uint64 `yaml:"revision"`
	SHA256   string `yaml:"sha256,omitempty"`
}

type ProfileEvidenceScope struct {
	Key             string                    `yaml:"key"`
	ArtifactProfile *ScopeReference           `yaml:"artifact_profile,omitempty"`
	EngineProfile   *ScopeReference           `yaml:"engine_profile,omitempty"`
	RuntimeProfile  *ScopeReference           `yaml:"runtime_profile,omitempty"`
	MachineBucket   *Reference                `yaml:"machine_bucket,omitempty"`
	Mode            string                    `yaml:"mode,omitempty"`
	CoResidents     []ProfileCoResident       `yaml:"co_residents"`
	Harnesses       []ProfileHarnessWitness   `yaml:"harnesses"`
	Conditions      ProfileEvidenceConditions `yaml:"conditions"`
}

type ProfileCoResident struct {
	RuntimeProfile Reference `yaml:"runtime_profile"`
	Placement      string    `yaml:"placement"`
}

type ProfileHarnessWitness struct {
	ID                  string `yaml:"id"`
	IntegrationRevision string `yaml:"integration_revision"`
}

type ProfileEvidenceConditions struct {
	OSBuild          EvidenceStringCondition  `yaml:"os_build"`
	WiredLimitMiB    EvidenceIntegerCondition `yaml:"wired_limit_mib"`
	WiredLimitSource EvidenceStringCondition  `yaml:"wired_limit_source"`
	Power            EvidenceStringCondition  `yaml:"power"`
	Thermal          EvidenceStringCondition  `yaml:"thermal"`
	Load             EvidenceStringCondition  `yaml:"load"`
}

type EvidenceStringCondition struct {
	State string `yaml:"state"`
	Value string `yaml:"value,omitempty"`
}

type EvidenceIntegerCondition struct {
	State string `yaml:"state"`
	Value int64  `yaml:"value,omitempty"`
}

type PromotionReference struct {
	Schema   string `yaml:"schema"`
	ID       string `yaml:"id"`
	Revision uint64 `yaml:"revision"`
	SHA256   string `yaml:"sha256"`
}

func decodeCanonicalProfile[T any](data []byte, name string, marshal func(T) ([]byte, error)) (T, error) {
	var zero T
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	var document T
	if err := decoder.Decode(&document); err != nil {
		return zero, fmt.Errorf("decode qualification %s: %w", name, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return zero, fmt.Errorf("decode qualification %s: multiple YAML documents are not allowed", name)
		}
		return zero, fmt.Errorf("decode qualification %s: %w", name, err)
	}

	canonical, err := marshal(document)
	if err != nil {
		return zero, err
	}
	if !bytes.Equal(data, canonical) {
		return zero, fmt.Errorf("qualification %s bytes are not canonical", name)
	}
	return document, nil
}

func encodeCanonicalProfile[T any](document T, name string, validate func(T) error) ([]byte, error) {
	if err := validate(document); err != nil {
		return nil, err
	}

	var root yaml.Node
	if err := root.Encode(document); err != nil {
		return nil, fmt.Errorf("encode qualification %s: %w", name, err)
	}
	sortMappingKeys(&root)

	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(&root); err != nil {
		return nil, fmt.Errorf("encode qualification %s: %w", name, err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("close qualification %s encoder: %w", name, err)
	}
	return output.Bytes(), nil
}

func validateProfileEnvelope(envelope ProfileEnvelope, schema string, problem func(string, ...any)) {
	if envelope.Schema != schema {
		problem("schema is %q, want %q", envelope.Schema, schema)
	}
	if !stableIDPattern.MatchString(envelope.ID) {
		problem("id %q is not a lowercase stable id", envelope.ID)
	}
	if envelope.Revision == 0 {
		problem("revision must be greater than zero")
	}
	if envelope.Revision == 1 && envelope.Supersedes != nil {
		problem("initial revision must not supersede another profile")
	}
	if envelope.Revision > 1 && envelope.Supersedes == nil {
		problem("revision %d must supersede revision %d", envelope.Revision, envelope.Revision-1)
	}
	if envelope.Supersedes != nil {
		validateReference("supersedes", *envelope.Supersedes, problem)
		if envelope.Supersedes.Schema != schema || envelope.Supersedes.ID != envelope.ID {
			problem("supersedes must have the current profile schema and id")
		}
		if envelope.Supersedes.Revision+1 != envelope.Revision {
			problem("supersedes revision must immediately precede revision %d", envelope.Revision)
		}
	}

	if !isProfileStatus(envelope.Status) {
		problem("status %q is not supported", envelope.Status)
	}
	if envelope.Revision == 1 && envelope.Status == ProfileStatusRetired {
		problem("initial revision cannot be RETIRED")
	}
	validateLine("status_reason", envelope.StatusReason, problem)
	validateLine("title", envelope.Title, problem)
	validateLine("summary", envelope.Summary, problem)
	validateLine("what_this_means", envelope.WhatThisMeans, problem)
	validateSortedStableIDs("roles", envelope.Roles, false, problem)
	validateApplicability(envelope.Applicability, problem)
	validateProfileDependencies(envelope.Dependencies, problem)
	validateDataBoundary(envelope.DataBoundary, problem)
	validateKnownFailures(envelope.KnownFailures, envelope.Evidence, problem)
	validateInvalidationTriggers(envelope.InvalidationTriggers, problem)
	validateProfileEvidence(envelope, problem)
	if envelope.Status == ProfileStatusQualified {
		problem("QUALIFIED profiles require implemented qualification-gate and dependency-status validation")
	}
	validatePromotionReference(envelope.Promotion, problem)
}

func validateApplicability(applicability ProfileApplicability, problem func(string, ...any)) {
	validateReferenceSet("applicability.machine_buckets", applicability.MachineBuckets, MachineBucketSchemaV1, problem)
	if len(applicability.Foregrounds) == 0 {
		problem("applicability.foregrounds must not be empty")
	}
	previous := ""
	for index, foreground := range applicability.Foregrounds {
		location := fmt.Sprintf("applicability.foregrounds[%d]", index)
		if foreground != "harness" && foreground != "local" && foreground != "none" {
			problem("%s %q must be harness, local, or none", location, foreground)
		}
		if index > 0 && foreground <= previous {
			problem("applicability.foregrounds must be unique and sorted")
		}
		previous = foreground
	}
	if contains(applicability.Foregrounds, "none") && len(applicability.Foregrounds) != 1 {
		problem("applicability.foregrounds none cannot be combined with another foreground")
	}
	validateSortedStableIDs("applicability.harnesses", applicability.Harnesses, true, problem)
	validateLine("applicability.explanation", applicability.Explanation, problem)
}

func validateProfileDependencies(dependencies []ProfileDependency, problem func(string, ...any)) {
	previous := ""
	semanticIdentities := map[string]bool{}
	for index, dependency := range dependencies {
		location := fmt.Sprintf("dependencies[%d]", index)
		if !stableIDPattern.MatchString(dependency.Relationship) {
			problem("%s.relationship %q is not a lowercase stable id", location, dependency.Relationship)
		}
		validateReference(location+".profile", dependency.Profile, problem)
		if dependency.Profile.Schema == MachineBucketSchemaV1 {
			problem("%s.profile must reference a profile schema", location)
		}

		semanticIdentity := dependency.Relationship + "\x00" + referenceSemanticIdentity(dependency.Profile)
		if semanticIdentities[semanticIdentity] {
			problem("dependencies repeats relationship/profile identity %s/%s@%d", dependency.Relationship, dependency.Profile.ID, dependency.Profile.Revision)
		}
		semanticIdentities[semanticIdentity] = true
		exactIdentity := dependency.Relationship + "\x00" + referenceExactIdentity(dependency.Profile)
		if previous != "" && exactIdentity <= previous {
			problem("dependencies must be unique and sorted by relationship and exact profile identity")
		}
		previous = exactIdentity
	}
}

func validateDataBoundary(boundary ProfileDataBoundary, problem func(string, ...any)) {
	if boundary.Inference != "local" && boundary.Inference != "harness-owned-remote" && boundary.Inference != "not-applicable" {
		problem("data_boundary.inference %q is not supported", boundary.Inference)
	}
	if boundary.Credentials != "none" && boundary.Credentials != "harness-owned" {
		problem("data_boundary.credentials %q is not supported", boundary.Credentials)
	}
	if boundary.Inference == "local" && boundary.Credentials != "none" {
		problem("local inference cannot require harness-owned credentials")
	}
	if boundary.Inference == "harness-owned-remote" && boundary.Credentials != "harness-owned" {
		problem("harness-owned-remote inference requires harness-owned credentials")
	}
	if boundary.Inference == "not-applicable" && boundary.Credentials != "none" {
		problem("not-applicable inference cannot require credentials")
	}

	previousNetwork := ""
	for index, use := range boundary.Network {
		location := fmt.Sprintf("data_boundary.network[%d]", index)
		if use.Purpose != "artifact-download" && use.Purpose != "evidence-export" && use.Purpose != "provider-inference" {
			problem("%s.purpose %q is not supported", location, use.Purpose)
		}
		validateLine(location+".destination", use.Destination, problem)
		if use.Timing != "explicit-export" && use.Timing != "install-only" && use.Timing != "request-time" {
			problem("%s.timing %q is not supported", location, use.Timing)
		}
		exactIdentity := strings.Join([]string{use.Purpose, use.Destination, use.Timing}, "\x00")
		if previousNetwork != "" && exactIdentity <= previousNetwork {
			problem("data_boundary.network must be unique and sorted")
		}
		previousNetwork = exactIdentity
	}
	validateSortedStableIDs("data_boundary.reads", boundary.Reads, true, problem)
	validateSortedStableIDs("data_boundary.writes", boundary.Writes, true, problem)
	if boundary.Telemetry != "none" {
		problem("data_boundary.telemetry must be none")
	}
	if boundary.EvidenceExport != "explicit-user-action" {
		problem("data_boundary.evidence_export must be explicit-user-action")
	}
}

func validateKnownFailures(failures []ProfileKnownFailure, evidence []ProfileEvidence, problem func(string, ...any)) {
	evidenceIDs := map[string]bool{}
	for _, item := range evidence {
		evidenceIDs[item.ID] = true
	}
	previous := ""
	for index, failure := range failures {
		location := fmt.Sprintf("known_failures[%d]", index)
		if !stableIDPattern.MatchString(failure.ID) {
			problem("%s.id %q is not a lowercase stable id", location, failure.ID)
		}
		if index > 0 && failure.ID <= previous {
			problem("known_failures must be unique and sorted by id")
		}
		previous = failure.ID
		validateLine(location+".summary", failure.Summary, problem)
		validateLine(location+".effect", failure.Effect, problem)
		validateSortedStableIDs(location+".evidence", failure.Evidence, false, problem)
		for _, evidenceID := range failure.Evidence {
			if !evidenceIDs[evidenceID] {
				problem("%s.evidence references unknown evidence id %q", location, evidenceID)
			}
		}
	}
}

func validateInvalidationTriggers(triggers []ProfileInvalidationTrigger, problem func(string, ...any)) {
	if len(triggers) == 0 {
		problem("invalidation_triggers must not be empty")
	}
	previous := ""
	for index, trigger := range triggers {
		location := fmt.Sprintf("invalidation_triggers[%d]", index)
		if !stableIDPattern.MatchString(trigger.ID) {
			problem("%s.id %q is not a lowercase stable id", location, trigger.ID)
		}
		if index > 0 && trigger.ID <= previous {
			problem("invalidation_triggers must be unique and sorted by id")
		}
		previous = trigger.ID
		validateLine(location+".condition", trigger.Condition, problem)
		if trigger.Consequence != "re-review-applicability" && trigger.Consequence != "reject" && trigger.Consequence != "retire" && trigger.Consequence != "return-to-lab" {
			problem("%s.consequence %q is not supported", location, trigger.Consequence)
		}
	}
}

func validatePromotionReference(reference PromotionReference, problem func(string, ...any)) {
	if reference.Schema != ProductPromotionSchemaV1 {
		problem("promotion.schema is %q, want %q", reference.Schema, ProductPromotionSchemaV1)
	}
	if !stableIDPattern.MatchString(reference.ID) {
		problem("promotion.id %q is not a lowercase stable id", reference.ID)
	}
	if reference.Revision == 0 {
		problem("promotion.revision must be greater than zero")
	}
	if !sha256Pattern.MatchString(reference.SHA256) {
		problem("promotion.sha256 must be 64 lowercase hexadecimal characters")
	}
}

func validateSortedStableIDs(location string, values []string, allowEmpty bool, problem func(string, ...any)) {
	if !allowEmpty && len(values) == 0 {
		problem("%s must not be empty", location)
		return
	}
	previous := ""
	for index, value := range values {
		if !stableIDPattern.MatchString(value) {
			problem("%s[%d] %q is not a lowercase stable id", location, index, value)
		}
		if index > 0 && value <= previous {
			problem("%s must be unique and sorted", location)
		}
		previous = value
	}
}

func validateMaterialReference(location string, reference MaterialReference, problem func(string, ...any)) {
	if !schemaIDPattern.MatchString(reference.Schema) {
		problem("%s.schema %q is not a versioned schema id", location, reference.Schema)
	}
	if !stableIDPattern.MatchString(reference.ID) {
		problem("%s.id %q is not a lowercase stable id", location, reference.ID)
	}
	if reference.Revision == 0 {
		problem("%s.revision must be greater than zero", location)
	}
	if !sha256Pattern.MatchString(reference.SHA256) {
		problem("%s.sha256 must be 64 lowercase hexadecimal characters", location)
	}
}
