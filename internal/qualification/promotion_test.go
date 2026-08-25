package qualification_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/qualification"
)

func TestProductPromotionRoundTripsAdoptedLabsFixture(t *testing.T) {
	data := readProductPromotionFixture(t)
	packet, err := qualification.ParseProductPromotionPacket(data)
	if err != nil {
		t.Fatal(err)
	}
	if packet.Schema != qualification.ProductPromotionSchemaV1 || packet.ID != "fake-coder-artifact-lab" || packet.Revision != 1 {
		t.Fatalf("packet identity = %s/%s@%d", packet.Schema, packet.ID, packet.Revision)
	}
	if packet.Target.Schema != qualification.ModelArtifactSchemaV1 || packet.Target.ID != "fake-coder-artifact" {
		t.Fatalf("packet target = %#v", packet.Target)
	}
	if _, ok := packet.Candidate.Spec.(qualification.PromotionModelArtifactSpec); !ok {
		t.Fatalf("candidate spec type = %T", packet.Candidate.Spec)
	}

	encoded, err := qualification.MarshalProductPromotionPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, data) {
		t.Fatalf("round trip changed canonical bytes\n got:\n%s\nwant:\n%s", encoded, data)
	}
}

func TestCompileProductPromotionMatchesLabsPublicProjection(t *testing.T) {
	packet := readProductPromotionFixture(t)
	want := readProductPromotionProfileFixture(t)

	got, err := qualification.CompileProductPromotion(packet, qualification.ProductPromotionInputs{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("compiled projection differs from Labs contract\n got:\n%s\nwant:\n%s", got, want)
	}
	profile, err := qualification.ParseModelArtifactProfile(got)
	if err != nil {
		t.Fatalf("compiled profile is not canonical C7: %v", err)
	}
	if profile.Promotion.SHA256 != qualification.Digest(packet) || profile.Evidence[0].Source.SHA256 != profile.Promotion.SHA256 {
		t.Fatalf("compiled provenance = promotion %#v, evidence %#v", profile.Promotion, profile.Evidence[0].Source)
	}
	for _, forbidden := range []string{
		"fixtures/private/fake-artifact-review.json",
		"fake-private-artifact-review",
		"catalog_consideration",
		"forbidden_generalizations",
		"sanitization",
	} {
		if bytes.Contains(got, []byte(forbidden)) {
			t.Fatalf("compiled public profile contains C8-only/private value %q", forbidden)
		}
	}
}

func TestParseProductPromotionRefusesNoncanonicalOrAmbiguousYAML(t *testing.T) {
	canonical := string(readProductPromotionFixture(t))
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "unknown field", input: strings.Replace(canonical, "id: fake-coder-artifact-lab", "automatic_publication: true\nid: fake-coder-artifact-lab", 1), want: "field automatic_publication not found"},
		{name: "duplicate key", input: strings.Replace(canonical, "revision: 1\nsanitization:", "revision: 1\nrevision: 2\nsanitization:", 1), want: "mapping key \"revision\" already defined"},
		{name: "multiple documents", input: canonical + "---\nnull\n", want: "multiple YAML documents"},
		{name: "missing final newline", input: strings.TrimSuffix(canonical, "\n"), want: "not canonical"},
		{name: "noncanonical mapping order", input: "schema: temper-labs-product-promotion/v1\n" + strings.Replace(canonical, "schema: temper-labs-product-promotion/v1\n", "", 1), want: "not canonical"},
		{name: "false sanitization", input: strings.Replace(canonical, "public_candidate_reviewed: true", "public_candidate_reviewed: false", 1), want: "must be true"},
		{name: "target body mismatch", input: strings.Replace(canonical, "target:\n  id: fake-coder-artifact\n  revision: 1\n  schema: temper-qualification-model-artifact/v1", "target:\n  id: fake-coder-artifact\n  revision: 1\n  schema: temper-qualification-engine/v1", 1), want: "field declared_download_bytes not found"},
		{name: "uninjected product source identity", input: strings.Replace(canonical, "kind: product-promotion", "id: forged-packet\n      kind: product-promotion", 1), want: "must not supply its injected identity"},
		{name: "unsupported accepted claim", input: strings.Replace(canonical, "- artifact-identity\n  confounds:", "- artifact-quality\n  confounds:", 1), want: "unsupported claim"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := qualification.ParseProductPromotionPacket([]byte(tt.input))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParseProductPromotionPacket() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestCompileProductPromotionRefusesPrivateProjectionAndUnusedInputs(t *testing.T) {
	canonical := string(readProductPromotionFixture(t))
	privateCandidate := strings.Replace(
		canonical,
		"summary: Fake byte identities for exercising the C8 writer boundary",
		"summary: fixtures/private/fake-artifact-review.json",
		1,
	)
	if _, err := qualification.CompileProductPromotion([]byte(privateCandidate), qualification.ProductPromotionInputs{}); err == nil || !strings.Contains(err.Error(), "private or restricted locator") {
		t.Fatalf("private candidate CompileProductPromotion() error = %v", err)
	}

	unused := readProductPromotionProfileFixture(t)
	if _, err := qualification.CompileProductPromotion([]byte(canonical), qualification.ProductPromotionInputs{Profiles: [][]byte{unused}}); err == nil || !strings.Contains(err.Error(), "unused document") {
		t.Fatalf("unused input CompileProductPromotion() error = %v", err)
	}
	if _, err := qualification.CompileProductPromotion([]byte(canonical), qualification.ProductPromotionInputs{PriorPackets: [][]byte{[]byte(canonical)}}); err == nil || !strings.Contains(err.Error(), "initial packet") {
		t.Fatalf("unused prior packet CompileProductPromotion() error = %v", err)
	}
}

func TestCompileProductPromotionRequiresExactIndependentSupersessionChains(t *testing.T) {
	priorPacketData := readProductPromotionFixture(t)
	priorProfileData := readProductPromotionProfileFixture(t)
	current := parseProductPromotionFixture(t)
	current.Revision = 2
	current.Supersedes = &qualification.MaterialReference{
		Schema: qualification.ProductPromotionSchemaV1, ID: current.ID,
		Revision: 1, SHA256: qualification.Digest(priorPacketData),
	}
	current.Target.Revision = 2
	current.Target.Supersedes = &qualification.Reference{
		Schema: qualification.ModelArtifactSchemaV1, ID: current.Target.ID,
		Revision: 1, SHA256: qualification.Digest(priorProfileData),
	}
	evidenceScope := current.Evidence[0].Scope.ArtifactProfile
	evidenceScope.Revision = 2

	currentData, err := qualification.MarshalProductPromotionPacket(current)
	if err != nil {
		t.Fatal(err)
	}
	inputs := qualification.ProductPromotionInputs{
		PriorPackets: [][]byte{priorPacketData},
		Profiles:     [][]byte{priorProfileData},
	}
	compiled, err := qualification.CompileProductPromotion(currentData, inputs)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := qualification.ParseModelArtifactProfile(compiled)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Revision != 2 || profile.Supersedes == nil || profile.Supersedes.SHA256 != qualification.Digest(priorProfileData) {
		t.Fatalf("compiled profile lineage = revision %d, supersedes %#v", profile.Revision, profile.Supersedes)
	}
	if profile.Promotion.Revision != 2 || profile.Promotion.SHA256 != qualification.Digest(currentData) {
		t.Fatalf("compiled packet lineage = %#v", profile.Promotion)
	}

	if _, err := qualification.CompileProductPromotion(currentData, qualification.ProductPromotionInputs{Profiles: [][]byte{priorProfileData}}); err == nil || !strings.Contains(err.Error(), "exactly one prior-packet") {
		t.Fatalf("missing prior packet CompileProductPromotion() error = %v", err)
	}
	wrongPacket := current
	wrongPacket.Supersedes = &qualification.MaterialReference{
		Schema: qualification.ProductPromotionSchemaV1, ID: current.ID,
		Revision: 1, SHA256: strings.Repeat("f", 64),
	}
	wrongPacketData, err := qualification.MarshalProductPromotionPacket(wrongPacket)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qualification.CompileProductPromotion(wrongPacketData, inputs); err == nil || !strings.Contains(err.Error(), "does not exactly match") {
		t.Fatalf("wrong prior digest CompileProductPromotion() error = %v", err)
	}

	illegal := current
	illegal.Decision.Status = qualification.ProfileStatusWatch
	illegalData, err := qualification.MarshalProductPromotionPacket(illegal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qualification.CompileProductPromotion(illegalData, inputs); err == nil || !strings.Contains(err.Error(), "LAB -> WATCH") {
		t.Fatalf("illegal target transition CompileProductPromotion() error = %v", err)
	}
}

func TestCompileProductPromotionCoversEveryC7TargetKind(t *testing.T) {
	artifactData := readPromotionInputFixture(t, "model-artifact.yaml")
	engineData := readPromotionInputFixture(t, "engine.yaml")
	runtimeData := readPromotionInputFixture(t, "model-runtime.yaml")
	toolData := readPromotionInputFixture(t, "tool.yaml")
	modeData := readPromotionInputFixture(t, "mode.yaml")
	bucketData := readPromotionInputFixture(t, "machine-bucket.yaml")
	bucket, err := qualification.ParseMachineBucket(bucketData)
	if err != nil {
		t.Fatal(err)
	}
	bucketReference := qualification.Reference{
		Schema: bucket.Schema, ID: bucket.ID, Revision: bucket.Revision, SHA256: qualification.Digest(bucketData),
	}

	tests := []struct {
		name   string
		packet func(*testing.T) qualification.ProductPromotionPacket
		inputs qualification.ProductPromotionInputs
		parse  func([]byte) error
	}{
		{
			name: "model artifact",
			packet: func(t *testing.T) qualification.ProductPromotionPacket {
				profile, err := qualification.ParseModelArtifactProfile(artifactData)
				if err != nil {
					t.Fatal(err)
				}
				return promotionPacketForProfile(profile.ProfileEnvelope, qualification.PromotionModelArtifactSpec{ModelArtifactSpec: profile.Spec}, selfPromotionScope(profile.Schema, profile.ID, profile.Revision))
			},
			parse: func(data []byte) error { _, err := qualification.ParseModelArtifactProfile(data); return err },
		},
		{
			name: "engine",
			packet: func(t *testing.T) qualification.ProductPromotionPacket {
				profile, err := qualification.ParseEngineProfile(engineData)
				if err != nil {
					t.Fatal(err)
				}
				return promotionPacketForProfile(profile.ProfileEnvelope, qualification.PromotionEngineSpec{EngineSpec: profile.Spec}, selfPromotionScope(profile.Schema, profile.ID, profile.Revision))
			},
			parse: func(data []byte) error { _, err := qualification.ParseEngineProfile(data); return err },
		},
		{
			name: "model runtime",
			packet: func(t *testing.T) qualification.ProductPromotionPacket {
				profile, err := qualification.ParseModelRuntimeProfile(runtimeData)
				if err != nil {
					t.Fatal(err)
				}
				profile.Applicability.MachineBuckets = []qualification.Reference{bucketReference}
				scope := selfPromotionScope(profile.Schema, profile.ID, profile.Revision)
				scope.ArtifactProfile = scopeReference(profile.Spec.ArtifactProfile)
				scope.EngineProfile = scopeReference(profile.Spec.EngineProfile)
				scope.MachineBucket = &bucketReference
				scope.Mode = "local"
				scope.Conditions = qualification.ProfileEvidenceConditions{
					OSBuild:          qualification.EvidenceStringCondition{State: "observed", Value: "fake-os-build"},
					WiredLimitMiB:    qualification.EvidenceIntegerCondition{State: "observed", Value: 24576},
					WiredLimitSource: qualification.EvidenceStringCondition{State: "observed", Value: "fake-source"},
					Power:            qualification.EvidenceStringCondition{State: "unmeasured"},
					Thermal:          qualification.EvidenceStringCondition{State: "unmeasured"},
					Load:             qualification.EvidenceStringCondition{State: "unmeasured"},
				}
				return promotionPacketForProfile(profile.ProfileEnvelope, qualification.PromotionModelRuntimeSpec{ModelRuntimeSpec: profile.Spec}, scope)
			},
			inputs: qualification.ProductPromotionInputs{
				Profiles: [][]byte{artifactData, engineData}, MachineBuckets: [][]byte{bucketData},
			},
			parse: func(data []byte) error { _, err := qualification.ParseModelRuntimeProfile(data); return err },
		},
		{
			name: "tool",
			packet: func(t *testing.T) qualification.ProductPromotionPacket {
				profile, err := qualification.ParseToolProfile(toolData)
				if err != nil {
					t.Fatal(err)
				}
				return promotionPacketForProfile(profile.ProfileEnvelope, qualification.PromotionToolSpec{ToolSpec: profile.Spec}, selfPromotionScope(profile.Schema, profile.ID, profile.Revision))
			},
			parse: func(data []byte) error { _, err := qualification.ParseToolProfile(data); return err },
		},
		{
			name: "mode",
			packet: func(t *testing.T) qualification.ProductPromotionPacket {
				profile, err := qualification.ParseModeProfile(modeData)
				if err != nil {
					t.Fatal(err)
				}
				return promotionPacketForProfile(profile.ProfileEnvelope, qualification.PromotionModeSpec{ModeSpec: profile.Spec}, selfPromotionScope(profile.Schema, profile.ID, profile.Revision))
			},
			inputs: qualification.ProductPromotionInputs{Profiles: [][]byte{runtimeData, toolData}},
			parse:  func(data []byte) error { _, err := qualification.ParseModeProfile(data); return err },
		},
		{
			name: "activity",
			packet: func(t *testing.T) qualification.ProductPromotionPacket {
				profile, err := qualification.ParseActivityProfile(readPromotionInputFixture(t, "activity.yaml"))
				if err != nil {
					t.Fatal(err)
				}
				return promotionPacketForProfile(profile.ProfileEnvelope, qualification.PromotionActivitySpec{ActivitySpec: profile.Spec}, selfPromotionScope(profile.Schema, profile.ID, profile.Revision))
			},
			inputs: qualification.ProductPromotionInputs{Profiles: [][]byte{modeData}},
			parse:  func(data []byte) error { _, err := qualification.ParseActivityProfile(data); return err },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packetData, err := qualification.MarshalProductPromotionPacket(tt.packet(t))
			if err != nil {
				t.Fatal(err)
			}
			compiled, err := qualification.CompileProductPromotion(packetData, tt.inputs)
			if err != nil {
				t.Fatal(err)
			}
			if err := tt.parse(compiled); err != nil {
				t.Fatalf("compiled target is not canonical C7: %v", err)
			}
			if !bytes.Contains(compiled, []byte("sha256: "+qualification.Digest(packetData))) {
				t.Fatalf("compiled target does not carry exact packet digest")
			}
		})
	}
}

func promotionPacketForProfile(envelope qualification.ProfileEnvelope, spec qualification.ProductPromotionSpec, scope qualification.ProductPromotionEvidenceScope) qualification.ProductPromotionPacket {
	return qualification.ProductPromotionPacket{
		Schema: qualification.ProductPromotionSchemaV1,
		ID:     envelope.ID + "-lab", Revision: 1,
		Target: qualification.ProductPromotionTarget{
			Schema: envelope.Schema, ID: envelope.ID, Revision: envelope.Revision, Supersedes: envelope.Supersedes,
		},
		Decision: qualification.ProductPromotionDecision{
			Status: envelope.Status, StatusReason: envelope.StatusReason,
			DecidedAt: "2026-08-25T19:00:00Z", Reviewers: []string{"fake-reviewer"},
			AcceptedClaims:           []string{"profile-identity"},
			ForbiddenGeneralizations: []string{"Fake fixture carries no real qualification or recommendation claim"},
			Gates: []qualification.ProductPromotionGate{{
				ID: "profile-material-pinned", Result: "pass", Evidence: []string{"profile-identity-witness"},
				Explanation: "Fake target material is structurally complete",
			}},
		},
		Evidence: []qualification.ProductPromotionEvidence{{
			ID: "profile-identity-witness", Claims: []string{"profile-identity"},
			Sources: []qualification.ProductPromotionEvidenceSource{{
				Kind: "labs-record", Schema: "temper-labs-record/v1", ID: "fake-profile-review",
				Revision: 1, Locator: "fixtures/public/fake-profile-review.json",
				SHA256: strings.Repeat("d", 64), Classification: "public",
			}},
			PublicSource: qualification.ProductPromotionPublicSource{Kind: "product-promotion"},
			Scope:        scope,
		}},
		Candidate: qualification.ProductPromotionCandidate{
			ProductPromotionCandidateCommon: qualification.ProductPromotionCandidateCommon{
				Title: envelope.Title, Summary: envelope.Summary, WhatThisMeans: envelope.WhatThisMeans,
				Roles: envelope.Roles, Applicability: envelope.Applicability, Dependencies: envelope.Dependencies,
				DataBoundary: envelope.DataBoundary, KnownFailures: envelope.KnownFailures,
				InvalidationTriggers: envelope.InvalidationTriggers,
			},
			Spec: spec,
		},
		Sanitization: qualification.ProductPromotionSanitization{
			PublicCandidateReviewed: true,
			ExcludedClasses: []string{
				"credentials", "machine-identifying-values-outside-the-C7-bucket", "private-corpus-content",
				"prompts-not-approved-for-publication", "raw-user-content",
			},
			Redactions:        []qualification.ProductPromotionRedaction{},
			ReviewerStatement: "Fake candidate and public source contain no private material",
		},
		CatalogConsideration: qualification.ProductCatalogConsideration{
			RecommendationReview: "separate", Comparisons: []qualification.Reference{},
			Note: "Fake fixture provides no recommendation evidence",
		},
	}
}

func selfPromotionScope(schema, id string, revision uint64) qualification.ProductPromotionEvidenceScope {
	self := &qualification.ScopeReference{Schema: schema, ID: id, Revision: revision}
	scope := qualification.ProductPromotionEvidenceScope{
		CoResidents: []qualification.ProfileCoResident{}, Harnesses: []qualification.ProfileHarnessWitness{},
		Conditions: notApplicablePromotionConditions(),
	}
	switch schema {
	case qualification.ModelArtifactSchemaV1:
		scope.ArtifactProfile = self
	case qualification.EngineSchemaV1:
		scope.EngineProfile = self
	case qualification.ModelRuntimeSchemaV1:
		scope.RuntimeProfile = self
	case qualification.ToolSchemaV1:
		scope.ToolProfile = self
	case qualification.ModeSchemaV1:
		scope.ModeProfile = self
	case qualification.ActivitySchemaV1:
		scope.ActivityProfile = self
	}
	return scope
}

func notApplicablePromotionConditions() qualification.ProfileEvidenceConditions {
	return qualification.ProfileEvidenceConditions{
		OSBuild:          qualification.EvidenceStringCondition{State: "not-applicable"},
		WiredLimitMiB:    qualification.EvidenceIntegerCondition{State: "not-applicable"},
		WiredLimitSource: qualification.EvidenceStringCondition{State: "not-applicable"},
		Power:            qualification.EvidenceStringCondition{State: "not-applicable"},
		Thermal:          qualification.EvidenceStringCondition{State: "not-applicable"},
		Load:             qualification.EvidenceStringCondition{State: "not-applicable"},
	}
}

func scopeReference(reference qualification.Reference) *qualification.ScopeReference {
	return &qualification.ScopeReference{Schema: reference.Schema, ID: reference.ID, Revision: reference.Revision, SHA256: reference.SHA256}
}

func readPromotionInputFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readProductPromotionFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/product-promotion.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readProductPromotionProfileFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/product-promotion-profile.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func parseProductPromotionFixture(t *testing.T) qualification.ProductPromotionPacket {
	t.Helper()
	packet, err := qualification.ParseProductPromotionPacket(readProductPromotionFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	return packet
}
