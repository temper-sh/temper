package qualification_test

import (
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/qualification"
)

func TestModelArtifactProfileAcceptsPublicEvidenceWithCanonicalSelfScope(t *testing.T) {
	profile := parseModelArtifactFixture(t)
	profile.Evidence = []qualification.ProfileEvidence{artifactEvidence(t, profile)}

	data, err := qualification.MarshalModelArtifactProfile(profile)
	if err != nil {
		t.Fatalf("MarshalModelArtifactProfile() error = %v", err)
	}
	parsed, err := qualification.ParseModelArtifactProfile(data)
	if err != nil {
		t.Fatalf("ParseModelArtifactProfile() error = %v", err)
	}
	if parsed.Evidence[0].Scope.Key != profile.Evidence[0].Scope.Key {
		t.Fatalf("parsed scope key = %q, want %q", parsed.Evidence[0].Scope.Key, profile.Evidence[0].Scope.Key)
	}
}

func TestEvidenceScopeKeyChangesWithCanonicalScopeMaterial(t *testing.T) {
	profile := parseModelArtifactFixture(t)
	evidence := artifactEvidence(t, profile)
	before := evidence.Scope.Key
	evidence.Scope.Conditions.OSBuild = qualification.EvidenceStringCondition{State: "observed", Value: "fake-build"}
	after, err := qualification.EvidenceScopeKey(evidence.Scope)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("EvidenceScopeKey() did not change with scope material")
	}
}

func TestEvidenceScopeKeyHasStableV1Preimage(t *testing.T) {
	profile := parseModelArtifactFixture(t)
	got := artifactEvidence(t, profile).Scope.Key
	const want = "a75e4a387c834d04717f4b7728a16a3ddad7c7831489fb582b4f423282ab51b4"
	if got != want {
		t.Fatalf("EvidenceScopeKey() = %q, want %q", got, want)
	}
}

func TestProfileEvidenceRefusesUnscopedOrNonpublicClaims(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*qualification.ModelArtifactProfile)
		want   string
	}{
		{name: "wrong scope key", mutate: func(profile *qualification.ModelArtifactProfile) {
			profile.Evidence[0].Scope.Key = strings.Repeat("f", 64)
		}, want: "want recomputed"},
		{name: "self digest", mutate: func(profile *qualification.ModelArtifactProfile) {
			profile.Evidence[0].Scope.ArtifactProfile.SHA256 = strings.Repeat("a", 64)
		}, want: "must be absent for the containing profile"},
		{name: "unknown source kind", mutate: func(profile *qualification.ModelArtifactProfile) { profile.Evidence[0].Source.Kind = "labs-record" }, want: "product-promotion or results-record"},
		{name: "raw source schema", mutate: func(profile *qualification.ModelArtifactProfile) {
			profile.Evidence[0].Source.Schema = "field-kit-session/v1"
		}, want: "must not identify a raw Field Kit packet"},
		{name: "empty claims", mutate: func(profile *qualification.ModelArtifactProfile) { profile.Evidence[0].Claims = nil }, want: "claims must not be empty"},
		{name: "machine-dependent artifact scope", mutate: func(profile *qualification.ModelArtifactProfile) {
			profile.Evidence[0].Scope.MachineBucket = &qualification.Reference{Schema: qualification.MachineBucketSchemaV1, ID: "fake-bucket", Revision: 1, SHA256: strings.Repeat("b", 64)}
		}, want: "must contain only its artifact identity"},
		{name: "observed artifact condition", mutate: func(profile *qualification.ModelArtifactProfile) {
			profile.Evidence[0].Scope.Conditions.OSBuild = qualification.EvidenceStringCondition{State: "observed", Value: "fake-build"}
		}, want: "conditions must all be not-applicable"},
		{name: "promotion source mismatch", mutate: func(profile *qualification.ModelArtifactProfile) {
			profile.Evidence[0].Source.Kind = "product-promotion"
		}, want: "must exactly match promotion"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := parseModelArtifactFixture(t)
			profile.Evidence = []qualification.ProfileEvidence{artifactEvidence(t, profile)}
			tt.mutate(&profile)

			_, err := qualification.MarshalModelArtifactProfile(profile)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("MarshalModelArtifactProfile() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestModelRuntimeProfileAcceptsMeasuredValueWithCompleteWitnessScope(t *testing.T) {
	profile := parseModelRuntimeFixture(t)
	evidence := runtimeEvidence(t, &profile)
	profile.Evidence = []qualification.ProfileEvidence{evidence}
	profile.Spec.Performance.Throughput = qualification.PerformanceAxis{
		State: "measured",
		Observations: []qualification.PerformanceObservation{{
			Metric: "decode-tokens-per-second", Definition: "Fake decoded tokens divided by elapsed generation seconds", Witness: evidence.ID,
			Value: qualification.PerformanceValue{Kind: qualification.PerformanceValueDecimal, Decimal: "12.5"},
		}},
	}

	data, err := qualification.MarshalModelRuntimeProfile(profile)
	if err != nil {
		t.Fatalf("MarshalModelRuntimeProfile() error = %v", err)
	}
	if _, err := qualification.ParseModelRuntimeProfile(data); err != nil {
		t.Fatalf("ParseModelRuntimeProfile() error = %v", err)
	}
}

func TestModelRuntimeEvidenceRequiresCompleteExactScope(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*qualification.ModelRuntimeProfile)
		want   string
	}{
		{name: "missing artifact", mutate: func(profile *qualification.ModelRuntimeProfile) { profile.Evidence[0].Scope.ArtifactProfile = nil }, want: "requires artifact, engine, runtime, machine bucket, and mode"},
		{name: "wrong artifact", mutate: func(profile *qualification.ModelRuntimeProfile) {
			profile.Evidence[0].Scope.ArtifactProfile.SHA256 = strings.Repeat("f", 64)
		}, want: "must exactly match spec.artifact_profile"},
		{name: "missing bucket", mutate: func(profile *qualification.ModelRuntimeProfile) { profile.Evidence[0].Scope.MachineBucket = nil }, want: "requires artifact, engine, runtime, machine bucket, and mode"},
		{name: "bucket outside applicability", mutate: func(profile *qualification.ModelRuntimeProfile) { profile.Applicability.MachineBuckets = nil }, want: "must be present in applicability.machine_buckets"},
		{name: "missing mode", mutate: func(profile *qualification.ModelRuntimeProfile) { profile.Evidence[0].Scope.Mode = "" }, want: "requires artifact, engine, runtime, machine bucket, and mode"},
		{name: "unobserved os build", mutate: func(profile *qualification.ModelRuntimeProfile) {
			profile.Evidence[0].Scope.Conditions.OSBuild = qualification.EvidenceStringCondition{State: "not-applicable"}
		}, want: "requires observed OS build"},
		{name: "invalid co-resident placement", mutate: func(profile *qualification.ModelRuntimeProfile) {
			profile.Evidence[0].Scope.CoResidents = []qualification.ProfileCoResident{{
				RuntimeProfile: qualification.Reference{Schema: qualification.ModelRuntimeSchemaV1, ID: "fake-peer", Revision: 1, SHA256: strings.Repeat("c", 64)}, Placement: "sometimes",
			}}
		}, want: "must be on-demand or resident"},
		{name: "self co-resident", mutate: func(profile *qualification.ModelRuntimeProfile) {
			profile.Evidence[0].Scope.CoResidents = []qualification.ProfileCoResident{{
				RuntimeProfile: qualification.Reference{Schema: profile.Schema, ID: profile.ID, Revision: profile.Revision, SHA256: strings.Repeat("d", 64)}, Placement: "resident",
			}}
		}, want: "cannot name the containing runtime"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := parseModelRuntimeFixture(t)
			profile.Evidence = []qualification.ProfileEvidence{runtimeEvidence(t, &profile)}
			tt.mutate(&profile)

			_, err := qualification.MarshalModelRuntimeProfile(profile)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("MarshalModelRuntimeProfile() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func artifactEvidence(t *testing.T, profile qualification.ModelArtifactProfile) qualification.ProfileEvidence {
	t.Helper()
	scope := qualification.ProfileEvidenceScope{
		ArtifactProfile: &qualification.ScopeReference{Schema: profile.Schema, ID: profile.ID, Revision: profile.Revision},
		CoResidents:     []qualification.ProfileCoResident{},
		Harnesses:       []qualification.ProfileHarnessWitness{},
		Conditions:      notApplicableConditions(),
	}
	key, err := qualification.EvidenceScopeKey(scope)
	if err != nil {
		t.Fatal(err)
	}
	scope.Key = key
	return qualification.ProfileEvidence{
		ID: "artifact-integrity-witness",
		Source: qualification.ProfileEvidenceSource{
			Kind: "results-record",
			MaterialReference: qualification.MaterialReference{
				Schema: "temper-results-record/v1", ID: "fake-artifact-result", Revision: 1, SHA256: strings.Repeat("a", 64),
			},
		},
		Claims: []string{"artifact-integrity"},
		Scope:  scope,
	}
}

func runtimeEvidence(t *testing.T, profile *qualification.ModelRuntimeProfile) qualification.ProfileEvidence {
	t.Helper()
	bucket := qualification.Reference{
		Schema: qualification.MachineBucketSchemaV1, ID: "fake-runtime-bucket", Revision: 1, SHA256: strings.Repeat("b", 64),
	}
	profile.Applicability.MachineBuckets = []qualification.Reference{bucket}
	scope := qualification.ProfileEvidenceScope{
		ArtifactProfile: &qualification.ScopeReference{
			Schema: profile.Spec.ArtifactProfile.Schema, ID: profile.Spec.ArtifactProfile.ID,
			Revision: profile.Spec.ArtifactProfile.Revision, SHA256: profile.Spec.ArtifactProfile.SHA256,
		},
		EngineProfile: &qualification.ScopeReference{
			Schema: profile.Spec.EngineProfile.Schema, ID: profile.Spec.EngineProfile.ID,
			Revision: profile.Spec.EngineProfile.Revision, SHA256: profile.Spec.EngineProfile.SHA256,
		},
		RuntimeProfile: &qualification.ScopeReference{Schema: profile.Schema, ID: profile.ID, Revision: profile.Revision},
		MachineBucket:  &bucket,
		Mode:           "fake-local",
		CoResidents:    []qualification.ProfileCoResident{},
		Harnesses:      []qualification.ProfileHarnessWitness{},
		Conditions: qualification.ProfileEvidenceConditions{
			OSBuild:          qualification.EvidenceStringCondition{State: "observed", Value: "24A123"},
			WiredLimitMiB:    qualification.EvidenceIntegerCondition{State: "observed", Value: 24576},
			WiredLimitSource: qualification.EvidenceStringCondition{State: "observed", Value: "fake-sysctl"},
			Power:            qualification.EvidenceStringCondition{State: "unmeasured"},
			Thermal:          qualification.EvidenceStringCondition{State: "unmeasured"},
			Load:             qualification.EvidenceStringCondition{State: "unmeasured"},
		},
	}
	key, err := qualification.EvidenceScopeKey(scope)
	if err != nil {
		t.Fatal(err)
	}
	scope.Key = key
	return qualification.ProfileEvidence{
		ID: "runtime-throughput-witness",
		Source: qualification.ProfileEvidenceSource{
			Kind: "results-record",
			MaterialReference: qualification.MaterialReference{
				Schema: "temper-results-record/v1", ID: "fake-runtime-result", Revision: 1, SHA256: strings.Repeat("c", 64),
			},
		},
		Claims: []string{"decode-throughput"},
		Scope:  scope,
	}
}

func notApplicableConditions() qualification.ProfileEvidenceConditions {
	return qualification.ProfileEvidenceConditions{
		OSBuild:          qualification.EvidenceStringCondition{State: "not-applicable"},
		WiredLimitMiB:    qualification.EvidenceIntegerCondition{State: "not-applicable"},
		WiredLimitSource: qualification.EvidenceStringCondition{State: "not-applicable"},
		Power:            qualification.EvidenceStringCondition{State: "not-applicable"},
		Thermal:          qualification.EvidenceStringCondition{State: "not-applicable"},
		Load:             qualification.EvidenceStringCondition{State: "not-applicable"},
	}
}
