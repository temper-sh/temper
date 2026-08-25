package qualification

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ProductPromotionInputs are the exact immutable C7 materials a caller makes
// available to the pure compiler. The compiler never discovers a catalog or
// reads Labs, Results, Field Kit, or the filesystem.
type ProductPromotionInputs struct {
	PriorPackets   [][]byte
	Profiles       [][]byte
	MachineBuckets [][]byte
}

// CompileProductPromotion projects one canonical Labs C8 packet into one
// canonical C7 profile. The packet digest is computed over the exact accepted
// input bytes and becomes both profile provenance and, when selected, its
// public evidence source.
func CompileProductPromotion(packetData []byte, inputs ProductPromotionInputs) ([]byte, error) {
	packet, err := ParseProductPromotionPacket(packetData)
	if err != nil {
		return nil, err
	}
	promotion := PromotionReference{
		Schema: ProductPromotionSchemaV1,
		ID:     packet.ID, Revision: packet.Revision, SHA256: Digest(packetData),
	}
	if err := validateProductPromotionInputs(packet, promotion, inputs); err != nil {
		return nil, err
	}
	profile, err := compileProductPromotionProfile(packet, promotion)
	if err != nil {
		return nil, fmt.Errorf("compile product promotion: %w", err)
	}
	if err := refusePrivateProjection(packet, profile); err != nil {
		return nil, err
	}
	return profile, nil
}

func compileProductPromotionProfile(packet ProductPromotionPacket, promotion PromotionReference) ([]byte, error) {
	evidence, err := compileProductPromotionEvidence(packet, promotion)
	if err != nil {
		return nil, err
	}
	envelope := ProfileEnvelope{
		Schema: packet.Target.Schema, ID: packet.Target.ID, Revision: packet.Target.Revision,
		Supersedes:          packet.Target.Supersedes,
		QualificationStatus: packet.Decision.QualificationStatus, QualificationReason: packet.Decision.QualificationReason,
		LifecycleStatus: packet.Decision.LifecycleStatus, LifecycleReason: packet.Decision.LifecycleReason,
		Title: packet.Candidate.Title, Summary: packet.Candidate.Summary,
		WhatThisMeans: packet.Candidate.WhatThisMeans, Roles: packet.Candidate.Roles,
		Applicability: packet.Candidate.Applicability, Dependencies: packet.Candidate.Dependencies,
		DataBoundary: packet.Candidate.DataBoundary, KnownFailures: packet.Candidate.KnownFailures,
		InvalidationTriggers: packet.Candidate.InvalidationTriggers,
		Evidence:             evidence, Promotion: promotion,
	}

	switch spec := packet.Candidate.Spec.(type) {
	case PromotionModelArtifactSpec:
		return MarshalModelArtifactProfile(ModelArtifactProfile{ProfileEnvelope: envelope, Spec: spec.ModelArtifactSpec})
	case PromotionEngineSpec:
		return MarshalEngineProfile(EngineProfile{ProfileEnvelope: envelope, Spec: spec.EngineSpec})
	case PromotionModelRuntimeSpec:
		return MarshalModelRuntimeProfile(ModelRuntimeProfile{ProfileEnvelope: envelope, Spec: spec.ModelRuntimeSpec})
	case PromotionToolSpec:
		return MarshalToolProfile(ToolProfile{ProfileEnvelope: envelope, Spec: spec.ToolSpec})
	case PromotionModeSpec:
		return MarshalModeProfile(ModeProfile{ProfileEnvelope: envelope, Spec: spec.ModeSpec})
	case PromotionActivitySpec:
		return MarshalActivityProfile(ActivityProfile{ProfileEnvelope: envelope, Spec: spec.ActivitySpec})
	default:
		return nil, fmt.Errorf("candidate spec type %T is not supported", packet.Candidate.Spec)
	}
}

func compileProductPromotionEvidence(packet ProductPromotionPacket, promotion PromotionReference) ([]ProfileEvidence, error) {
	compiled := make([]ProfileEvidence, 0, len(packet.Evidence))
	for _, source := range packet.Evidence {
		claims := sortedAcceptedClaims(source, packet.Decision.AcceptedClaims)
		if len(claims) == 0 {
			continue
		}
		scope := source.Scope.profileScope()
		key, err := EvidenceScopeKey(scope)
		if err != nil {
			return nil, fmt.Errorf("evidence %q scope: %w", source.ID, err)
		}
		scope.Key = key

		publicSource := ProfileEvidenceSource{Kind: source.PublicSource.Kind}
		switch source.PublicSource.Kind {
		case "product-promotion":
			publicSource.MaterialReference = MaterialReference(promotion)
		case "results-record":
			publicSource.MaterialReference = MaterialReference{
				Schema: source.PublicSource.Schema, ID: source.PublicSource.ID,
				Revision: source.PublicSource.Revision, SHA256: source.PublicSource.SHA256,
			}
		default:
			return nil, fmt.Errorf("evidence %q public source kind %q is not supported", source.ID, source.PublicSource.Kind)
		}
		compiled = append(compiled, ProfileEvidence{ID: source.ID, Source: publicSource, Claims: claims, Scope: scope})
	}
	return compiled, nil
}

func validateProductPromotionInputs(packet ProductPromotionPacket, promotion PromotionReference, inputs ProductPromotionInputs) error {
	if err := validatePriorPromotionPacket(packet, inputs.PriorPackets); err != nil {
		return err
	}
	profiles, err := indexPromotionProfiles(inputs.Profiles)
	if err != nil {
		return err
	}
	buckets, err := indexPromotionBuckets(inputs.MachineBuckets)
	if err != nil {
		return err
	}

	neededProfiles := map[string]Reference{}
	qualifiedDependencies := map[string]Reference{}
	neededBuckets := map[string]Reference{}
	addProfile := func(reference Reference) {
		if reference.Schema == packet.Target.Schema && reference.ID == packet.Target.ID && reference.Revision == packet.Target.Revision && reference.SHA256 == "" {
			return
		}
		neededProfiles[referenceExactIdentity(reference)] = reference
	}
	addBucket := func(reference Reference) { neededBuckets[referenceExactIdentity(reference)] = reference }
	for _, dependency := range packet.Candidate.Dependencies {
		addProfile(dependency.Profile)
		qualifiedDependencies[referenceExactIdentity(dependency.Profile)] = dependency.Profile
	}
	if packet.Target.Supersedes != nil {
		addProfile(*packet.Target.Supersedes)
	}
	for _, reference := range packet.Candidate.Applicability.MachineBuckets {
		addBucket(reference)
	}
	for _, evidence := range packet.Evidence {
		collectPromotionScopeInputs(evidence.Scope, packet.Target, addProfile, addBucket)
	}

	for identity, reference := range neededProfiles {
		profile, ok := profiles[identity]
		if !ok {
			return fmt.Errorf("compile product promotion: required profile %s/%s@%d with sha256 %s was not supplied", reference.Schema, reference.ID, reference.Revision, reference.SHA256)
		}
		if _, isDependency := qualifiedDependencies[identity]; packet.Decision.QualificationStatus == QualificationStatusQualified && isDependency && profile.Envelope.QualificationStatus != QualificationStatusQualified {
			return fmt.Errorf("compile product promotion: QUALIFIED target requires profile %s/%s@%d to be QUALIFIED, got %s", reference.Schema, reference.ID, reference.Revision, profile.Envelope.QualificationStatus)
		}
	}
	for identity, reference := range neededBuckets {
		if _, ok := buckets[identity]; !ok {
			return fmt.Errorf("compile product promotion: required machine bucket %s@%d with sha256 %s was not supplied", reference.ID, reference.Revision, reference.SHA256)
		}
	}
	if len(profiles) != len(neededProfiles) {
		return fmt.Errorf("compile product promotion: supplied profile set contains %d unused document(s)", len(profiles)-len(neededProfiles))
	}
	if len(buckets) != len(neededBuckets) {
		return fmt.Errorf("compile product promotion: supplied machine-bucket set contains %d unused document(s)", len(buckets)-len(neededBuckets))
	}
	if packet.Target.Supersedes != nil {
		previous := profiles[referenceExactIdentity(*packet.Target.Supersedes)]
		current := ProfileEnvelope{
			Schema: packet.Target.Schema, ID: packet.Target.ID, Revision: packet.Target.Revision,
			Supersedes:          packet.Target.Supersedes,
			QualificationStatus: packet.Decision.QualificationStatus, QualificationReason: packet.Decision.QualificationReason,
			LifecycleStatus: packet.Decision.LifecycleStatus, LifecycleReason: packet.Decision.LifecycleReason,
			Promotion: promotion,
		}
		if err := ValidateProfileDispositionTransition(previous.Envelope, current, packet.Target.Supersedes.SHA256); err != nil {
			return fmt.Errorf("compile product promotion: target profile transition: %w", err)
		}
	}
	return nil
}

func validatePriorPromotionPacket(current ProductPromotionPacket, documents [][]byte) error {
	if current.Revision == 1 {
		if len(documents) != 0 {
			return fmt.Errorf("compile product promotion: initial packet must not receive prior-packet inputs")
		}
		return nil
	}
	if len(documents) != 1 {
		return fmt.Errorf("compile product promotion: packet revision %d requires exactly one prior-packet input", current.Revision)
	}
	previousData := documents[0]
	previous, err := ParseProductPromotionPacket(previousData)
	if err != nil {
		return fmt.Errorf("compile product promotion: prior packet: %w", err)
	}
	want := MaterialReference{
		Schema: previous.Schema, ID: previous.ID, Revision: previous.Revision, SHA256: Digest(previousData),
	}
	if current.Supersedes == nil || *current.Supersedes != want {
		return fmt.Errorf("compile product promotion: prior packet does not exactly match supersedes")
	}
	return nil
}

type promotionInputProfile struct {
	Reference
	Envelope ProfileEnvelope
}

func indexPromotionProfiles(documents [][]byte) (map[string]promotionInputProfile, error) {
	indexed := make(map[string]promotionInputProfile, len(documents))
	for index, data := range documents {
		profile, err := parsePromotionInputProfile(data)
		if err != nil {
			return nil, fmt.Errorf("compile product promotion: supplied profile %d: %w", index, err)
		}
		identity := referenceExactIdentity(profile.Reference)
		if _, exists := indexed[identity]; exists {
			return nil, fmt.Errorf("compile product promotion: supplied profiles repeat %s/%s@%d", profile.Schema, profile.ID, profile.Revision)
		}
		indexed[identity] = profile
	}
	return indexed, nil
}

func parsePromotionInputProfile(data []byte) (promotionInputProfile, error) {
	var header struct {
		Schema string `yaml:"schema"`
	}
	if err := decodeHeader(data, &header); err != nil {
		return promotionInputProfile{}, err
	}
	makeInput := func(envelope ProfileEnvelope) promotionInputProfile {
		return promotionInputProfile{
			Reference: Reference{Schema: envelope.Schema, ID: envelope.ID, Revision: envelope.Revision, SHA256: Digest(data)},
			Envelope:  envelope,
		}
	}
	switch header.Schema {
	case ModelArtifactSchemaV1:
		profile, err := ParseModelArtifactProfile(data)
		if err != nil {
			return promotionInputProfile{}, err
		}
		return makeInput(profile.ProfileEnvelope), nil
	case EngineSchemaV1:
		profile, err := ParseEngineProfile(data)
		if err != nil {
			return promotionInputProfile{}, err
		}
		return makeInput(profile.ProfileEnvelope), nil
	case ModelRuntimeSchemaV1:
		profile, err := ParseModelRuntimeProfile(data)
		if err != nil {
			return promotionInputProfile{}, err
		}
		return makeInput(profile.ProfileEnvelope), nil
	case ToolSchemaV1:
		profile, err := ParseToolProfile(data)
		if err != nil {
			return promotionInputProfile{}, err
		}
		return makeInput(profile.ProfileEnvelope), nil
	case ModeSchemaV1:
		profile, err := ParseModeProfile(data)
		if err != nil {
			return promotionInputProfile{}, err
		}
		return makeInput(profile.ProfileEnvelope), nil
	case ActivitySchemaV1:
		profile, err := ParseActivityProfile(data)
		if err != nil {
			return promotionInputProfile{}, err
		}
		return makeInput(profile.ProfileEnvelope), nil
	default:
		return promotionInputProfile{}, fmt.Errorf("schema %q is not a C7 profile schema", header.Schema)
	}
}

func indexPromotionBuckets(documents [][]byte) (map[string]Reference, error) {
	indexed := make(map[string]Reference, len(documents))
	for index, data := range documents {
		bucket, err := ParseMachineBucket(data)
		if err != nil {
			return nil, fmt.Errorf("compile product promotion: supplied machine bucket %d: %w", index, err)
		}
		reference := Reference{Schema: bucket.Schema, ID: bucket.ID, Revision: bucket.Revision, SHA256: Digest(data)}
		identity := referenceExactIdentity(reference)
		if _, exists := indexed[identity]; exists {
			return nil, fmt.Errorf("compile product promotion: supplied machine buckets repeat %s@%d", bucket.ID, bucket.Revision)
		}
		indexed[identity] = reference
	}
	return indexed, nil
}

func collectPromotionScopeInputs(scope ProductPromotionEvidenceScope, target ProductPromotionTarget, addProfile func(Reference), addBucket func(Reference)) {
	for _, reference := range []*ScopeReference{scope.ArtifactProfile, scope.EngineProfile, scope.RuntimeProfile, scope.ToolProfile, scope.ModeProfile, scope.ActivityProfile} {
		if reference == nil {
			continue
		}
		if reference.Schema == target.Schema && reference.ID == target.ID && reference.Revision == target.Revision && reference.SHA256 == "" {
			continue
		}
		addProfile(Reference{Schema: reference.Schema, ID: reference.ID, Revision: reference.Revision, SHA256: reference.SHA256})
	}
	if scope.MachineBucket != nil {
		addBucket(*scope.MachineBucket)
	}
	for _, resident := range scope.CoResidents {
		addProfile(resident.RuntimeProfile)
	}
}

func decodeHeader(data []byte, destination any) error {
	if err := yaml.Unmarshal(data, destination); err != nil {
		return fmt.Errorf("decode header: %w", err)
	}
	return nil
}

func refusePrivateProjection(packet ProductPromotionPacket, profile []byte) error {
	var document yaml.Node
	if err := yaml.Unmarshal(profile, &document); err != nil {
		return fmt.Errorf("compile product promotion: inspect public projection: %w", err)
	}
	for _, evidence := range packet.Evidence {
		for _, source := range evidence.Sources {
			if (source.Classification == "private" || source.Classification == "restricted") && yamlScalarContains(&document, source.Locator) {
				return fmt.Errorf("compile product promotion: private or restricted locator from source %q crossed the C7 projection", source.ID)
			}
		}
	}
	return nil
}

func yamlScalarContains(node *yaml.Node, value string) bool {
	if node.Kind == yaml.ScalarNode && strings.Contains(node.Value, value) {
		return true
	}
	for _, child := range node.Content {
		if yamlScalarContains(child, value) {
			return true
		}
	}
	return false
}
