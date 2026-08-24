package qualification_test

import (
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/qualification"
)

func TestValidateProfileStatusTransitionAcceptsLegalImmutableEdges(t *testing.T) {
	tests := []struct {
		previous string
		current  string
	}{
		{previous: qualification.ProfileStatusWatch, current: qualification.ProfileStatusWatch},
		{previous: qualification.ProfileStatusWatch, current: qualification.ProfileStatusLab},
		{previous: qualification.ProfileStatusWatch, current: qualification.ProfileStatusRejected},
		{previous: qualification.ProfileStatusWatch, current: qualification.ProfileStatusRetired},
		{previous: qualification.ProfileStatusLab, current: qualification.ProfileStatusLab},
		{previous: qualification.ProfileStatusLab, current: qualification.ProfileStatusQualified},
		{previous: qualification.ProfileStatusLab, current: qualification.ProfileStatusRejected},
		{previous: qualification.ProfileStatusLab, current: qualification.ProfileStatusRetired},
		{previous: qualification.ProfileStatusQualified, current: qualification.ProfileStatusQualified},
		{previous: qualification.ProfileStatusQualified, current: qualification.ProfileStatusLab},
		{previous: qualification.ProfileStatusQualified, current: qualification.ProfileStatusRejected},
		{previous: qualification.ProfileStatusQualified, current: qualification.ProfileStatusRetired},
		{previous: qualification.ProfileStatusRejected, current: qualification.ProfileStatusRejected},
		{previous: qualification.ProfileStatusRejected, current: qualification.ProfileStatusLab},
		{previous: qualification.ProfileStatusRetired, current: qualification.ProfileStatusRetired},
		{previous: qualification.ProfileStatusRetired, current: qualification.ProfileStatusLab},
	}

	for _, tt := range tests {
		t.Run(tt.previous+"_to_"+tt.current, func(t *testing.T) {
			previous, current, digest := statusTransitionFixtures(t, tt.previous, tt.current)
			if err := qualification.ValidateProfileStatusTransition(previous, current, digest); err != nil {
				t.Fatalf("ValidateProfileStatusTransition() error = %v", err)
			}
		})
	}
}

func TestValidateProfileStatusTransitionRefusesIllegalEdges(t *testing.T) {
	tests := []struct {
		previous string
		current  string
	}{
		{previous: qualification.ProfileStatusWatch, current: qualification.ProfileStatusQualified},
		{previous: qualification.ProfileStatusLab, current: qualification.ProfileStatusWatch},
		{previous: qualification.ProfileStatusQualified, current: qualification.ProfileStatusWatch},
		{previous: qualification.ProfileStatusRejected, current: qualification.ProfileStatusWatch},
		{previous: qualification.ProfileStatusRejected, current: qualification.ProfileStatusQualified},
		{previous: qualification.ProfileStatusRejected, current: qualification.ProfileStatusRetired},
		{previous: qualification.ProfileStatusRetired, current: qualification.ProfileStatusWatch},
		{previous: qualification.ProfileStatusRetired, current: qualification.ProfileStatusQualified},
		{previous: qualification.ProfileStatusRetired, current: qualification.ProfileStatusRejected},
	}

	for _, tt := range tests {
		t.Run(tt.previous+"_to_"+tt.current, func(t *testing.T) {
			previous, current, digest := statusTransitionFixtures(t, tt.previous, tt.current)
			err := qualification.ValidateProfileStatusTransition(previous, current, digest)
			if err == nil || !strings.Contains(err.Error(), "is not allowed") {
				t.Fatalf("ValidateProfileStatusTransition() error = %v, want illegal transition", err)
			}
		})
	}
}

func TestValidateProfileStatusTransitionRefusesBrokenLineage(t *testing.T) {
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
			current.Revision = 3
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
			previous, current, digest := statusTransitionFixtures(t, qualification.ProfileStatusLab, qualification.ProfileStatusRejected)
			tt.mutate(&previous, &current, &digest)
			err := qualification.ValidateProfileStatusTransition(previous, current, digest)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateProfileStatusTransition() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func statusTransitionFixtures(t *testing.T, previousStatus, currentStatus string) (qualification.ProfileEnvelope, qualification.ProfileEnvelope, string) {
	t.Helper()
	previous := parseModelArtifactFixture(t).ProfileEnvelope
	previous.Status = previousStatus
	digest := strings.Repeat("a", 64)
	current := previous
	current.Revision = 2
	current.Status = currentStatus
	current.Supersedes = &qualification.Reference{
		Schema: previous.Schema, ID: previous.ID, Revision: previous.Revision, SHA256: digest,
	}
	current.Promotion = qualification.PromotionReference{
		Schema: qualification.ProductPromotionSchemaV1, ID: "next-promotion", Revision: 1, SHA256: strings.Repeat("b", 64),
	}
	return previous, current, digest
}
