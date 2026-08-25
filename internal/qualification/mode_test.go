package qualification_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/qualification"
)

func TestParseModeProfileRoundTripsCanonicalFixture(t *testing.T) {
	data := readModeFixture(t)

	profile, err := qualification.ParseModeProfile(data)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Schema != qualification.ModeSchemaV1 || profile.ID != "example-local-search-mode" || profile.Revision != 1 || profile.QualificationStatus != qualification.QualificationStatusLab {
		t.Fatalf("profile identity = %#v", profile.ProfileEnvelope)
	}
	if profile.Spec.Foreground != "local" || len(profile.Spec.Bindings) != 1 || profile.Spec.RoleBindings["coder"] != "primary-coder" {
		t.Fatalf("mode spec = %#v", profile.Spec)
	}

	encoded, err := qualification.MarshalModeProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, data) {
		t.Fatalf("round trip changed canonical bytes\n got:\n%s\nwant:\n%s", encoded, data)
	}
}

func TestModeProfileValidationRefusesIncompleteWorld(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*qualification.ModeProfile)
		want   string
	}{
		{name: "unknown foreground", mutate: func(profile *qualification.ModeProfile) { profile.Spec.Foreground = "automatic" }, want: "harness, local, or none"},
		{name: "foreground applicability mismatch", mutate: func(profile *qualification.ModeProfile) { profile.Applicability.Foregrounds = []string{"harness"} }, want: "must contain exactly spec.foreground"},
		{name: "unstable binding id", mutate: func(profile *qualification.ModeProfile) { profile.Spec.Bindings[0].ID = "Primary Coder" }, want: "lowercase stable id"},
		{name: "wrong runtime schema", mutate: func(profile *qualification.ModeProfile) {
			profile.Spec.Bindings[0].RuntimeProfile.Schema = qualification.EngineSchemaV1
		}, want: "want \"temper-qualification-model-runtime/v1\""},
		{name: "unknown placement", mutate: func(profile *qualification.ModeProfile) { profile.Spec.Bindings[0].Placement = "maybe" }, want: "on-demand or resident"},
		{name: "unknown ngl state", mutate: func(profile *qualification.ModeProfile) { profile.Spec.Bindings[0].NGL.State = "all" }, want: "engine-default or explicit"},
		{name: "engine default with layers", mutate: func(profile *qualification.ModeProfile) {
			layers := uint64(0)
			profile.Spec.Bindings[0].NGL.Layers = &layers
		}, want: "layers must be absent"},
		{name: "explicit without layers", mutate: func(profile *qualification.ModeProfile) { profile.Spec.Bindings[0].NGL.State = "explicit" }, want: "layers is required"},
		{name: "preload on demand", mutate: func(profile *qualification.ModeProfile) { profile.Spec.Bindings[0].Placement = "on-demand" }, want: "preload is allowed only"},
		{name: "harness applicability mismatch", mutate: func(profile *qualification.ModeProfile) { profile.Applicability.Harnesses = nil }, want: "must exactly match spec.harnesses ids"},
		{name: "moving harness integration", mutate: func(profile *qualification.ModeProfile) {
			profile.Spec.Harnesses[0].IntegrationRevision = "latest integration"
		}, want: "integration_revision"},
		{name: "role keys mismatch", mutate: func(profile *qualification.ModeProfile) { profile.Roles = []string{"coder", "rerank"} }, want: "keys must exactly match roles"},
		{name: "unknown role binding", mutate: func(profile *qualification.ModeProfile) { profile.Spec.RoleBindings["coder"] = "missing" }, want: "references unknown binding"},
		{name: "missing dependency", mutate: func(profile *qualification.ModeProfile) { profile.Dependencies = profile.Dependencies[:1] }, want: "must exactly contain every runtime and tool"},
		{name: "dependency mismatch", mutate: func(profile *qualification.ModeProfile) {
			profile.Dependencies[0].Profile.SHA256 = strings.Repeat("f", 64)
		}, want: "must exactly match the mode runtime/tool closure"},
		{name: "unmeasured wall without reason", mutate: func(profile *qualification.ModeProfile) { profile.Spec.WallModel.Reason = "" }, want: "wall_model.reason"},
		{name: "fit wall without prediction", mutate: func(profile *qualification.ModeProfile) {
			profile.Spec.WallModel.Result = "fit"
			profile.Spec.WallModel.Reason = ""
		}, want: "predicted_resident_mib is required"},
		{name: "local coder on demand", mutate: func(profile *qualification.ModeProfile) {
			profile.Spec.Bindings[0].Placement = "on-demand"
			profile.Spec.Bindings[0].Preload = false
		}, want: "coder binding must be resident"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := parseModeFixture(t)
			tt.mutate(&profile)

			_, err := qualification.MarshalModeProfile(profile)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("MarshalModeProfile() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestModeProfileAcceptsExplicitZeroNGL(t *testing.T) {
	profile := parseModeFixture(t)
	layers := uint64(0)
	profile.Spec.Bindings[0].NGL = qualification.ModeNGL{State: "explicit", Layers: &layers}

	data, err := qualification.MarshalModeProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("layers: 0\n")) {
		t.Fatalf("canonical mode omitted explicit zero NGL:\n%s", data)
	}
}

func TestModeProfileAcceptsClosedOffWorld(t *testing.T) {
	profile := parseModeFixture(t)
	profile.Spec = qualification.ModeSpec{
		Foreground: "none", Bindings: []qualification.ModeBinding{}, Tools: []qualification.ModeTool{},
		Harnesses: []qualification.ModeHarness{}, RoleBindings: map[string]string{},
		WallModel: qualification.ModeWallModel{Result: "not-applicable", Reason: "No local world is active"},
	}
	profile.Applicability.Foregrounds = []string{"none"}
	profile.Applicability.Harnesses = nil
	profile.Dependencies = nil
	profile.Roles = nil
	profile.DataBoundary.Inference = "not-applicable"
	profile.DataBoundary.Reads = nil
	profile.DataBoundary.Writes = nil

	if _, err := qualification.MarshalModeProfile(profile); err != nil {
		t.Fatalf("MarshalModeProfile() error = %v", err)
	}
}

func TestParseModeProfileRefusesNoncanonicalOrAmbiguousYAML(t *testing.T) {
	canonical := string(readModeFixture(t))
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "unknown field", input: strings.Replace(canonical, "evidence: []", "evidence: []\nselected: true", 1), want: "field selected not found"},
		{name: "anchor", input: strings.Replace(canonical, "qualification_status: LAB", "qualification_status: &qualification LAB", 1), want: "not canonical"},
		{name: "duplicate key", input: strings.Replace(canonical, "qualification_status: LAB", "qualification_status: LAB\nqualification_status: WATCH", 1), want: "mapping key \"qualification_status\" already defined"},
		{name: "multiple documents", input: canonical + "---\nnull\n", want: "multiple YAML documents"},
		{name: "missing final newline", input: strings.TrimSuffix(canonical, "\n"), want: "not canonical"},
		{name: "noncanonical mapping order", input: "schema: temper-qualification-mode/v1\n" + strings.Replace(canonical, "schema: temper-qualification-mode/v1\n", "", 1), want: "not canonical"},
		{name: "noncanonical integer", input: strings.Replace(canonical, "revision: 1", "revision: 01", 1), want: "not canonical"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := qualification.ParseModeProfile([]byte(tt.input))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParseModeProfile() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func readModeFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/mode.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func parseModeFixture(t *testing.T) qualification.ModeProfile {
	t.Helper()
	profile, err := qualification.ParseModeProfile(readModeFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	return profile
}
