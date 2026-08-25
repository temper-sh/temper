package qualification_test

import (
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/qualification"
)

func TestValidateProfileDispositionTransitionAcceptsIndependentLegalEdges(t *testing.T) {
	tests := []struct {
		name                  string
		previousQualification string
		previousLifecycle     string
		currentQualification  string
		currentLifecycle      string
	}{
		{name: "unchanged lab experiment", previousQualification: qualification.QualificationStatusLab, previousLifecycle: qualification.LifecycleStatusExperimental, currentQualification: qualification.QualificationStatusLab, currentLifecycle: qualification.LifecycleStatusExperimental},
		{name: "qualifies into support", previousQualification: qualification.QualificationStatusLab, previousLifecycle: qualification.LifecycleStatusExperimental, currentQualification: qualification.QualificationStatusQualified, currentLifecycle: qualification.LifecycleStatusSupported},
		{name: "deprecates qualified support", previousQualification: qualification.QualificationStatusQualified, previousLifecycle: qualification.LifecycleStatusSupported, currentQualification: qualification.QualificationStatusQualified, currentLifecycle: qualification.LifecycleStatusDeprecated},
		{name: "reverses deprecation", previousQualification: qualification.QualificationStatusQualified, previousLifecycle: qualification.LifecycleStatusDeprecated, currentQualification: qualification.QualificationStatusQualified, currentLifecycle: qualification.LifecycleStatusSupported},
		{name: "changed material returns through lab experiment", previousQualification: qualification.QualificationStatusQualified, previousLifecycle: qualification.LifecycleStatusSupported, currentQualification: qualification.QualificationStatusLab, currentLifecycle: qualification.LifecycleStatusExperimental},
		{name: "retires without erasing qualification", previousQualification: qualification.QualificationStatusQualified, previousLifecycle: qualification.LifecycleStatusExperimental, currentQualification: qualification.QualificationStatusQualified, currentLifecycle: qualification.LifecycleStatusRetired},
		{name: "reopens retired through lab experiment", previousQualification: qualification.QualificationStatusQualified, previousLifecycle: qualification.LifecycleStatusRetired, currentQualification: qualification.QualificationStatusLab, currentLifecycle: qualification.LifecycleStatusExperimental},
		{name: "rejects and retires watch", previousQualification: qualification.QualificationStatusWatch, previousLifecycle: qualification.LifecycleStatusExperimental, currentQualification: qualification.QualificationStatusRejected, currentLifecycle: qualification.LifecycleStatusRetired},
		{name: "reconsiders rejected through lab experiment", previousQualification: qualification.QualificationStatusRejected, previousLifecycle: qualification.LifecycleStatusRetired, currentQualification: qualification.QualificationStatusLab, currentLifecycle: qualification.LifecycleStatusExperimental},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous, current, digest := dispositionTransitionFixtures(t, tt.previousQualification, tt.previousLifecycle, tt.currentQualification, tt.currentLifecycle)
			if err := qualification.ValidateProfileDispositionTransition(previous, current, digest); err != nil {
				t.Fatalf("ValidateProfileDispositionTransition() error = %v", err)
			}
		})
	}
}

func TestValidateProfileDispositionTransitionRefusesIllegalEdgesAndCombinations(t *testing.T) {
	tests := []struct {
		name                  string
		previousQualification string
		previousLifecycle     string
		currentQualification  string
		currentLifecycle      string
		want                  string
	}{
		{name: "skips lab", previousQualification: qualification.QualificationStatusWatch, previousLifecycle: qualification.LifecycleStatusExperimental, currentQualification: qualification.QualificationStatusQualified, currentLifecycle: qualification.LifecycleStatusExperimental, want: "qualification transition"},
		{name: "supports lab evidence", previousQualification: qualification.QualificationStatusLab, previousLifecycle: qualification.LifecycleStatusExperimental, currentQualification: qualification.QualificationStatusLab, currentLifecycle: qualification.LifecycleStatusSupported, want: "requires QUALIFIED"},
		{name: "returns qualified to watch", previousQualification: qualification.QualificationStatusQualified, previousLifecycle: qualification.LifecycleStatusSupported, currentQualification: qualification.QualificationStatusWatch, currentLifecycle: qualification.LifecycleStatusExperimental, want: "qualification transition"},
		{name: "deprecates an experiment", previousQualification: qualification.QualificationStatusQualified, previousLifecycle: qualification.LifecycleStatusExperimental, currentQualification: qualification.QualificationStatusQualified, currentLifecycle: qualification.LifecycleStatusDeprecated, want: "lifecycle transition"},
		{name: "reopens retired directly to support", previousQualification: qualification.QualificationStatusQualified, previousLifecycle: qualification.LifecycleStatusRetired, currentQualification: qualification.QualificationStatusQualified, currentLifecycle: qualification.LifecycleStatusSupported, want: "lifecycle transition"},
		{name: "rejected remains experimental", previousQualification: qualification.QualificationStatusLab, previousLifecycle: qualification.LifecycleStatusExperimental, currentQualification: qualification.QualificationStatusRejected, currentLifecycle: qualification.LifecycleStatusExperimental, want: "requires RETIRED"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous, current, digest := dispositionTransitionFixtures(t, tt.previousQualification, tt.previousLifecycle, tt.currentQualification, tt.currentLifecycle)
			err := qualification.ValidateProfileDispositionTransition(previous, current, digest)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateProfileDispositionTransition() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateProfileDispositionTransitionRefusesBrokenLineage(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*qualification.ProfileEnvelope, *qualification.ProfileEnvelope, *string)
		want   string
	}{
		{name: "invalid prior digest", mutate: func(_, _ *qualification.ProfileEnvelope, digest *string) { *digest = "nope" }, want: "previous sha256"},
		{name: "schema fork", mutate: func(_ *qualification.ProfileEnvelope, current *qualification.ProfileEnvelope, _ *string) {
			current.Schema = qualification.EngineSchemaV1
		}, want: "one schema and id lineage"},
		{name: "id fork", mutate: func(_ *qualification.ProfileEnvelope, current *qualification.ProfileEnvelope, _ *string) {
			current.ID = "another-profile"
		}, want: "one schema and id lineage"},
		{name: "skipped revision", mutate: func(_ *qualification.ProfileEnvelope, current *qualification.ProfileEnvelope, _ *string) {
			current.Revision++
		}, want: "immediately follow"},
		{name: "missing supersedes", mutate: func(_ *qualification.ProfileEnvelope, current *qualification.ProfileEnvelope, _ *string) {
			current.Supersedes = nil
		}, want: "supersedes must exactly identify"},
		{name: "wrong supersedes digest", mutate: func(_ *qualification.ProfileEnvelope, current *qualification.ProfileEnvelope, _ *string) {
			current.Supersedes.SHA256 = strings.Repeat("f", 64)
		}, want: "supersedes must exactly identify"},
		{name: "reused promotion", mutate: func(previous *qualification.ProfileEnvelope, current *qualification.ProfileEnvelope, _ *string) {
			current.Promotion = previous.Promotion
		}, want: "distinct promotion identity"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous, current, digest := dispositionTransitionFixtures(t, qualification.QualificationStatusLab, qualification.LifecycleStatusExperimental, qualification.QualificationStatusRejected, qualification.LifecycleStatusRetired)
			tt.mutate(&previous, &current, &digest)
			err := qualification.ValidateProfileDispositionTransition(previous, current, digest)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateProfileDispositionTransition() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func dispositionTransitionFixtures(t *testing.T, previousQualification, previousLifecycle, currentQualification, currentLifecycle string) (qualification.ProfileEnvelope, qualification.ProfileEnvelope, string) {
	t.Helper()
	previous := parseModelArtifactFixture(t).ProfileEnvelope
	previous.QualificationStatus = previousQualification
	previous.QualificationReason = "Previous evidence disposition"
	previous.LifecycleStatus = previousLifecycle
	previous.LifecycleReason = "Previous product posture"
	if previousLifecycle == qualification.LifecycleStatusRetired {
		previous.Revision = 2
	}

	digest := strings.Repeat("a", 64)
	current := previous
	current.Revision = previous.Revision + 1
	current.QualificationStatus = currentQualification
	current.QualificationReason = "Current evidence disposition"
	current.LifecycleStatus = currentLifecycle
	current.LifecycleReason = "Current product posture"
	current.Supersedes = &qualification.Reference{
		Schema: previous.Schema, ID: previous.ID, Revision: previous.Revision, SHA256: digest,
	}
	current.Promotion = qualification.PromotionReference{
		Schema: qualification.ProductPromotionSchemaV1, ID: "next-promotion", Revision: 1, SHA256: strings.Repeat("b", 64),
	}
	return previous, current, digest
}
