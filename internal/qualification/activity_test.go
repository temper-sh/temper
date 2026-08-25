package qualification_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/qualification"
)

func TestParseActivityProfileRoundTripsCanonicalFixture(t *testing.T) {
	data := readActivityFixture(t)

	profile, err := qualification.ParseActivityProfile(data)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Schema != qualification.ActivitySchemaV1 || profile.ID != "example-inspect-activity" || profile.Revision != 1 || profile.Status != qualification.ProfileStatusLab {
		t.Fatalf("profile identity = %#v", profile.ProfileEnvelope)
	}
	if profile.Spec.Purpose != "inspect" || len(profile.Spec.ActiveTools) != 0 || profile.Spec.ModeProfile.ID != "example-local-search-mode" {
		t.Fatalf("activity spec = %#v", profile.Spec)
	}

	encoded, err := qualification.MarshalActivityProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, data) {
		t.Fatalf("round trip changed canonical bytes\n got:\n%s\nwant:\n%s", encoded, data)
	}
}

func TestActivityProfileValidationRefusesWideningBody(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*qualification.ActivityProfile)
		want   string
	}{
		{name: "wrong mode schema", mutate: func(profile *qualification.ActivityProfile) {
			profile.Spec.ModeProfile.Schema = qualification.ToolSchemaV1
		}, want: "want \"temper-qualification-mode/v1\""},
		{name: "unknown purpose", mutate: func(profile *qualification.ActivityProfile) { profile.Spec.Purpose = "administer" }, want: "change, inspect, review, or verify"},
		{name: "wrong active tool schema", mutate: func(profile *qualification.ActivityProfile) {
			profile.Spec.ActiveTools = []qualification.Reference{{Schema: qualification.EngineSchemaV1, ID: "fake-engine", Revision: 1, SHA256: strings.Repeat("a", 64)}}
		}, want: "want \"temper-qualification-tool/v1\""},
		{name: "duplicate active tool", mutate: func(profile *qualification.ActivityProfile) {
			tool := qualification.Reference{Schema: qualification.ToolSchemaV1, ID: "fake-tool", Revision: 1, SHA256: strings.Repeat("a", 64)}
			profile.Spec.ActiveTools = []qualification.Reference{tool, tool}
		}, want: "unique and sorted"},
		{name: "missing mode dependency", mutate: func(profile *qualification.ActivityProfile) { profile.Dependencies = nil }, want: "exactly spec.mode_profile"},
		{name: "mode dependency mismatch", mutate: func(profile *qualification.ActivityProfile) {
			profile.Dependencies[0].Profile.SHA256 = strings.Repeat("f", 64)
		}, want: "exactly spec.mode_profile"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := parseActivityFixture(t)
			tt.mutate(&profile)

			_, err := qualification.MarshalActivityProfile(profile)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("MarshalActivityProfile() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestActivityProfileEvidenceRequiresCanonicalSelfScope(t *testing.T) {
	profile := parseActivityFixture(t)
	scope := qualification.ProfileEvidenceScope{
		ActivityProfile: &qualification.ScopeReference{Schema: profile.Schema, ID: profile.ID, Revision: profile.Revision},
		CoResidents:     []qualification.ProfileCoResident{},
		Harnesses:       []qualification.ProfileHarnessWitness{},
		Conditions:      notApplicableConditions(),
	}
	key, err := qualification.EvidenceScopeKey(scope)
	if err != nil {
		t.Fatal(err)
	}
	scope.Key = key
	profile.Evidence = []qualification.ProfileEvidence{{
		ID: "activity-boundary-witness",
		Source: qualification.ProfileEvidenceSource{
			Kind: "results-record",
			MaterialReference: qualification.MaterialReference{
				Schema: "temper-results-record/v1", ID: "fake-activity-result", Revision: 1, SHA256: strings.Repeat("a", 64),
			},
		},
		Claims: []string{"activity-boundary"},
		Scope:  scope,
	}}

	if _, err := qualification.MarshalActivityProfile(profile); err != nil {
		t.Fatalf("MarshalActivityProfile() error = %v", err)
	}
	profile.Evidence[0].Scope.ActivityProfile.ID = "another-activity"
	key, err = qualification.EvidenceScopeKey(profile.Evidence[0].Scope)
	if err != nil {
		t.Fatal(err)
	}
	profile.Evidence[0].Scope.Key = key
	if _, err := qualification.MarshalActivityProfile(profile); err == nil || !strings.Contains(err.Error(), "must identify the containing activity") {
		t.Fatalf("MarshalActivityProfile() error = %v, want self-scope refusal", err)
	}
}

func TestParseActivityProfileRefusesNoncanonicalOrAmbiguousYAML(t *testing.T) {
	canonical := string(readActivityFixture(t))
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "unknown field", input: strings.Replace(canonical, "evidence: []", "evidence: []\nselected: true", 1), want: "field selected not found"},
		{name: "anchor", input: strings.Replace(canonical, "status: LAB", "status: &status LAB", 1), want: "not canonical"},
		{name: "duplicate key", input: strings.Replace(canonical, "status: LAB", "status: LAB\nstatus: WATCH", 1), want: "mapping key \"status\" already defined"},
		{name: "multiple documents", input: canonical + "---\nnull\n", want: "multiple YAML documents"},
		{name: "missing final newline", input: strings.TrimSuffix(canonical, "\n"), want: "not canonical"},
		{name: "noncanonical mapping order", input: "schema: temper-qualification-activity/v1\n" + strings.Replace(canonical, "schema: temper-qualification-activity/v1\n", "", 1), want: "not canonical"},
		{name: "noncanonical integer", input: strings.Replace(canonical, "revision: 1", "revision: 01", 1), want: "not canonical"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := qualification.ParseActivityProfile([]byte(tt.input))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParseActivityProfile() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func readActivityFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/activity.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func parseActivityFixture(t *testing.T) qualification.ActivityProfile {
	t.Helper()
	profile, err := qualification.ParseActivityProfile(readActivityFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	return profile
}
