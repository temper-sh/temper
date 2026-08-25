package qualification_test

import (
	"testing"

	"github.com/temper-sh/temper/internal/qualification"
)

const qualifiedEvidenceID = "profile-identity-witness"

func qualifyEnvelope(t *testing.T, envelope *qualification.ProfileEnvelope, lifecycle string, scope qualification.ProfileEvidenceScope) {
	t.Helper()
	envelope.QualificationStatus = qualification.QualificationStatusQualified
	envelope.QualificationReason = "Fake evidence completely exercises the qualified profile contract"
	envelope.LifecycleStatus = lifecycle
	envelope.LifecycleReason = "Fake lifecycle exercises catalog closure without making a product claim"
	key, err := qualification.EvidenceScopeKey(scope)
	if err != nil {
		t.Fatal(err)
	}
	scope.Key = key
	envelope.Evidence = []qualification.ProfileEvidence{{
		ID: qualifiedEvidenceID,
		Source: qualification.ProfileEvidenceSource{
			Kind:              "product-promotion",
			MaterialReference: qualification.MaterialReference(envelope.Promotion),
		},
		Claims: []string{"profile-qualification"},
		Scope:  scope,
	}}
}

func qualifiedArtifactFixture(t *testing.T, lifecycle string) (qualification.ModelArtifactProfile, []byte) {
	t.Helper()
	profile := parseModelArtifactFixture(t)
	qualifyEnvelope(t, &profile.ProfileEnvelope, lifecycle, qualification.ProfileEvidenceScope{
		ArtifactProfile: selfScopeReference(profile.ProfileEnvelope),
		CoResidents:     []qualification.ProfileCoResident{},
		Harnesses:       []qualification.ProfileHarnessWitness{},
		Conditions:      notApplicableEvidenceConditions(),
	})
	data, err := qualification.MarshalModelArtifactProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	return profile, data
}

func qualifiedEngineFixture(t *testing.T, lifecycle string) (qualification.EngineProfile, []byte) {
	t.Helper()
	profile := parseEngineFixture(t)
	qualifyEnvelope(t, &profile.ProfileEnvelope, lifecycle, qualification.ProfileEvidenceScope{
		EngineProfile: selfScopeReference(profile.ProfileEnvelope),
		CoResidents:   []qualification.ProfileCoResident{},
		Harnesses:     []qualification.ProfileHarnessWitness{},
		Conditions:    notApplicableEvidenceConditions(),
	})
	data, err := qualification.MarshalEngineProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	return profile, data
}

func qualifiedToolFixture(t *testing.T, lifecycle string) (qualification.ToolProfile, []byte) {
	t.Helper()
	profile := parseToolFixture(t)
	harnesses := make([]qualification.ProfileHarnessWitness, 0, len(profile.Spec.Transports))
	for _, transport := range profile.Spec.Transports {
		harnesses = append(harnesses, qualification.ProfileHarnessWitness{
			ID: transport.Harness, IntegrationRevision: transport.IntegrationRevision,
		})
	}
	qualifyEnvelope(t, &profile.ProfileEnvelope, lifecycle, qualification.ProfileEvidenceScope{
		ToolProfile: selfScopeReference(profile.ProfileEnvelope),
		CoResidents: []qualification.ProfileCoResident{},
		Harnesses:   harnesses,
		Conditions:  notApplicableEvidenceConditions(),
	})
	data, err := qualification.MarshalToolProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	return profile, data
}

func qualifiedRuntimeFixture(t *testing.T, lifecycle string, artifact, engine qualification.Reference, bucket qualification.Reference) (qualification.ModelRuntimeProfile, []byte) {
	t.Helper()
	profile := parseModelRuntimeFixture(t)
	profile.Spec.ArtifactProfile = artifact
	profile.Spec.EngineProfile = engine
	profile.Dependencies = []qualification.ProfileDependency{
		{Relationship: "artifact", Profile: artifact},
		{Relationship: "engine", Profile: engine},
	}
	profile.Applicability.MachineBuckets = []qualification.Reference{bucket}
	qualifyEnvelope(t, &profile.ProfileEnvelope, lifecycle, qualification.ProfileEvidenceScope{
		ArtifactProfile: exactScopeReference(artifact),
		EngineProfile:   exactScopeReference(engine),
		RuntimeProfile:  selfScopeReference(profile.ProfileEnvelope),
		MachineBucket:   &bucket,
		Mode:            "local",
		CoResidents:     []qualification.ProfileCoResident{},
		Harnesses:       []qualification.ProfileHarnessWitness{},
		Conditions: qualification.ProfileEvidenceConditions{
			OSBuild:          qualification.EvidenceStringCondition{State: "observed", Value: "fake-os-build"},
			WiredLimitMiB:    qualification.EvidenceIntegerCondition{State: "observed", Value: 24576},
			WiredLimitSource: qualification.EvidenceStringCondition{State: "observed", Value: "fake-source"},
			Power:            qualification.EvidenceStringCondition{State: "unmeasured"},
			Thermal:          qualification.EvidenceStringCondition{State: "unmeasured"},
			Load:             qualification.EvidenceStringCondition{State: "unmeasured"},
		},
	})
	profile.Spec.Performance.TaskSuccess = qualification.PerformanceAxis{
		State: "measured",
		Observations: []qualification.PerformanceObservation{{
			Metric: "first-attempt-task-success",
			Value: qualification.PerformanceValue{
				Kind:            qualification.PerformanceValueSuccessFraction,
				SuccessFraction: &qualification.PerformanceSuccessFraction{Successes: 2, Attempts: 2},
			},
			Definition: "Two fake first attempts completed their declared task",
			Witness:    qualifiedEvidenceID,
		}},
	}
	profile.Spec.Performance.Regressions = qualification.PerformanceAxis{
		State: "measured",
		Observations: []qualification.PerformanceObservation{
			integerObservation("known-bad-tasks", 1, "One known-bad fake task remained bounded"),
			integerObservation("new-regressions", 0, "No new fake regression was observed"),
			integerObservation("retained-good-tasks", 2, "Two fake known-good tasks remained successful"),
		},
	}
	data, err := qualification.MarshalModelRuntimeProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	return profile, data
}

func qualifiedModeFixture(t *testing.T, lifecycle string, runtime, tool qualification.Reference) (qualification.ModeProfile, []byte) {
	t.Helper()
	profile := parseModeFixture(t)
	profile.Spec.Bindings[0].RuntimeProfile = runtime
	profile.Spec.Tools[0].Profile = tool
	profile.Dependencies = []qualification.ProfileDependency{
		{Relationship: "runtime", Profile: runtime},
		{Relationship: "tool", Profile: tool},
	}
	residentMiB := uint64(16384)
	profile.Spec.WallModel = qualification.ModeWallModel{
		Result: "fit", PredictedResidentMiB: &residentMiB, Witness: qualifiedEvidenceID,
	}
	harnesses := []qualification.ProfileHarnessWitness{{ID: "pi", IntegrationRevision: "temper-pi-tools/v1"}}
	qualifyEnvelope(t, &profile.ProfileEnvelope, lifecycle, qualification.ProfileEvidenceScope{
		ModeProfile: selfScopeReference(profile.ProfileEnvelope),
		CoResidents: []qualification.ProfileCoResident{},
		Harnesses:   harnesses,
		Conditions:  notApplicableEvidenceConditions(),
	})
	data, err := qualification.MarshalModeProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	return profile, data
}

func qualifiedActivityFixture(t *testing.T, lifecycle string, mode qualification.Reference) (qualification.ActivityProfile, []byte) {
	t.Helper()
	profile := parseActivityFixture(t)
	profile.Spec.ModeProfile = mode
	profile.Dependencies = []qualification.ProfileDependency{{Relationship: "mode", Profile: mode}}
	qualifyEnvelope(t, &profile.ProfileEnvelope, lifecycle, qualification.ProfileEvidenceScope{
		ActivityProfile: selfScopeReference(profile.ProfileEnvelope),
		CoResidents:     []qualification.ProfileCoResident{},
		Harnesses: []qualification.ProfileHarnessWitness{{
			ID: "pi", IntegrationRevision: "temper-pi-tools/v1",
		}},
		Conditions: notApplicableEvidenceConditions(),
	})
	data, err := qualification.MarshalActivityProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	return profile, data
}

func profileReference(envelope qualification.ProfileEnvelope, data []byte) qualification.Reference {
	return qualification.Reference{
		Schema: envelope.Schema, ID: envelope.ID, Revision: envelope.Revision, SHA256: qualification.Digest(data),
	}
}

func selfScopeReference(envelope qualification.ProfileEnvelope) *qualification.ScopeReference {
	return &qualification.ScopeReference{Schema: envelope.Schema, ID: envelope.ID, Revision: envelope.Revision}
}

func exactScopeReference(reference qualification.Reference) *qualification.ScopeReference {
	return &qualification.ScopeReference{
		Schema: reference.Schema, ID: reference.ID, Revision: reference.Revision, SHA256: reference.SHA256,
	}
}

func notApplicableEvidenceConditions() qualification.ProfileEvidenceConditions {
	return qualification.ProfileEvidenceConditions{
		OSBuild:          qualification.EvidenceStringCondition{State: "not-applicable"},
		WiredLimitMiB:    qualification.EvidenceIntegerCondition{State: "not-applicable"},
		WiredLimitSource: qualification.EvidenceStringCondition{State: "not-applicable"},
		Power:            qualification.EvidenceStringCondition{State: "not-applicable"},
		Thermal:          qualification.EvidenceStringCondition{State: "not-applicable"},
		Load:             qualification.EvidenceStringCondition{State: "not-applicable"},
	}
}

func integerObservation(metric string, value uint64, definition string) qualification.PerformanceObservation {
	return qualification.PerformanceObservation{
		Metric: metric,
		Value: qualification.PerformanceValue{
			Kind:    qualification.PerformanceValueInteger,
			Integer: &value,
		},
		Definition: definition,
		Witness:    qualifiedEvidenceID,
	}
}

func qualifiedCatalogBundle(t *testing.T, lifecycle string) (qualification.CatalogIndex, map[string][]byte) {
	t.Helper()
	index := parseCatalogFixture(t)
	bucket := index.MachineBuckets[0].Document
	artifact, artifactData := qualifiedArtifactFixture(t, lifecycle)
	engine, engineData := qualifiedEngineFixture(t, lifecycle)
	tool, toolData := qualifiedToolFixture(t, lifecycle)
	runtime, runtimeData := qualifiedRuntimeFixture(
		t,
		lifecycle,
		profileReference(artifact.ProfileEnvelope, artifactData),
		profileReference(engine.ProfileEnvelope, engineData),
		bucket,
	)
	mode, modeData := qualifiedModeFixture(
		t,
		lifecycle,
		profileReference(runtime.ProfileEnvelope, runtimeData),
		profileReference(tool.ProfileEnvelope, toolData),
	)
	activity, activityData := qualifiedActivityFixture(
		t,
		lifecycle,
		profileReference(mode.ProfileEnvelope, modeData),
	)

	index.Profiles = []qualification.IndexedDocument{
		indexedProfile(activity.ProfileEnvelope, activityData, "activity"),
		indexedProfile(engine.ProfileEnvelope, engineData, "engine"),
		indexedProfile(mode.ProfileEnvelope, modeData, "mode"),
		indexedProfile(artifact.ProfileEnvelope, artifactData, "model-artifact"),
		indexedProfile(runtime.ProfileEnvelope, runtimeData, "model-runtime"),
		indexedProfile(tool.ProfileEnvelope, toolData, "tool"),
	}
	files := map[string][]byte{exampleBucketPath: readMachineBucketFixture(t)}
	for indexPosition, data := range [][]byte{activityData, engineData, modeData, artifactData, runtimeData, toolData} {
		files[index.Profiles[indexPosition].Path] = data
	}
	return index, files
}

func indexedProfile(envelope qualification.ProfileEnvelope, data []byte, kind string) qualification.IndexedDocument {
	return qualification.IndexedDocument{
		Document: profileReference(envelope, data),
		Path:     "profiles/" + kind + "/" + envelope.ID + "/1.yaml",
	}
}
