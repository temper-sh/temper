package qualification_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/qualification"
)

func TestParseToolProfileRoundTripsCanonicalFixture(t *testing.T) {
	data := readToolFixture(t)

	profile, err := qualification.ParseToolProfile(data)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Schema != qualification.ToolSchemaV1 || profile.ID != "example-project-search" || profile.Revision != 1 || profile.QualificationStatus != qualification.QualificationStatusLab {
		t.Fatalf("profile identity = %#v", profile.ProfileEnvelope)
	}
	if profile.Spec.Core.InterfaceRevision != "project-search/v1" || len(profile.Spec.Transports) != 1 || profile.Spec.Transports[0].Harness != "pi" {
		t.Fatalf("tool spec = %#v", profile.Spec)
	}

	encoded, err := qualification.MarshalToolProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, data) {
		t.Fatalf("round trip changed canonical bytes\n got:\n%s\nwant:\n%s", encoded, data)
	}
}

func TestToolProfileValidationRefusesIncompleteCoreOrTransport(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*qualification.ToolProfile)
		want   string
	}{
		{name: "unknown source kind", mutate: func(profile *qualification.ToolProfile) { profile.Spec.Core.Source.Kind = "moving-repository" }, want: "github or upstream-release"},
		{name: "unscoped repository", mutate: func(profile *qualification.ToolProfile) { profile.Spec.Core.Source.Repository = "project-search" }, want: "must be owner/name"},
		{name: "moving revision", mutate: func(profile *qualification.ToolProfile) { profile.Spec.Core.Source.Revision = "main" }, want: "40-character lowercase commit hash"},
		{name: "invalid source digest", mutate: func(profile *qualification.ToolProfile) { profile.Spec.Core.Source.SHA256 = "nope" }, want: "source.sha256"},
		{name: "moving interface", mutate: func(profile *qualification.ToolProfile) {
			profile.Spec.Core.InterfaceRevision = "Project Search latest"
		}, want: "interface_revision"},
		{name: "no transports", mutate: func(profile *qualification.ToolProfile) {
			profile.Spec.Transports = nil
			profile.Applicability.Harnesses = nil
		}, want: "transports must not be empty"},
		{name: "unstable harness", mutate: func(profile *qualification.ToolProfile) { profile.Spec.Transports[0].Harness = "Pi Harness" }, want: "lowercase stable id"},
		{name: "moving integration", mutate: func(profile *qualification.ToolProfile) {
			profile.Spec.Transports[0].IntegrationRevision = "latest integration"
		}, want: "integration_revision"},
		{name: "moving protocol", mutate: func(profile *qualification.ToolProfile) { profile.Spec.Transports[0].Protocol = "MCP latest" }, want: "protocol"},
		{name: "invalid request schema", mutate: func(profile *qualification.ToolProfile) { profile.Spec.Transports[0].RequestSchema = "request" }, want: "request_schema"},
		{name: "invalid result schema", mutate: func(profile *qualification.ToolProfile) { profile.Spec.Transports[0].ResultSchema = "result" }, want: "result_schema"},
		{name: "invalid description digest", mutate: func(profile *qualification.ToolProfile) { profile.Spec.Transports[0].DescriptionSHA256 = "nope" }, want: "description_sha256"},
		{name: "applicability mismatch", mutate: func(profile *qualification.ToolProfile) { profile.Applicability.Harnesses = nil }, want: "must exactly match spec.transports harnesses"},
		{name: "C7 dependency", mutate: func(profile *qualification.ToolProfile) {
			profile.Dependencies = []qualification.ProfileDependency{{
				Relationship: "runtime", Profile: qualification.Reference{Schema: qualification.ModelRuntimeSchemaV1, ID: "example-runtime", Revision: 1, SHA256: strings.Repeat("a", 64)},
			}}
		}, want: "tool dependencies must be empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := parseToolFixture(t)
			tt.mutate(&profile)

			_, err := qualification.MarshalToolProfile(profile)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("MarshalToolProfile() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestToolProfileValidationRefusesPermissionOrFailureAmbiguity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*qualification.ToolProfile)
		want   string
	}{
		{name: "reads disagree", mutate: func(profile *qualification.ToolProfile) { profile.Spec.Permissions.Reads = nil }, want: "permissions.reads must exactly match"},
		{name: "writes disagree", mutate: func(profile *qualification.ToolProfile) {
			profile.Spec.Permissions.Writes = []string{"generated-files"}
		}, want: "permissions.writes must exactly match"},
		{name: "network disagrees", mutate: func(profile *qualification.ToolProfile) {
			profile.Spec.Permissions.Network = []string{"provider-inference"}
		}, want: "permissions.network must exactly match"},
		{name: "unsorted executes", mutate: func(profile *qualification.ToolProfile) {
			profile.Spec.Permissions.Executes = []string{"write-command", "read-command"}
		}, want: "executes must be unique and sorted"},
		{name: "overlapping backend role", mutate: func(profile *qualification.ToolProfile) { profile.Spec.Backend.RequiredRoles = []string{"rerank"} }, want: "cannot be both required and optional"},
		{name: "silent invalid input", mutate: func(profile *qualification.ToolProfile) { profile.Spec.FailureSemantics.InvalidInput = "ignore" }, want: "invalid_input must be refuse"},
		{name: "silent permission denial", mutate: func(profile *qualification.ToolProfile) { profile.Spec.FailureSemantics.PermissionDenied = "ignore" }, want: "permission_denied must be refuse"},
		{name: "silent backend failure", mutate: func(profile *qualification.ToolProfile) { profile.Spec.FailureSemantics.BackendUnavailable = "ignore" }, want: "backend_unavailable"},
		{name: "silent partial effect", mutate: func(profile *qualification.ToolProfile) { profile.Spec.FailureSemantics.PartialEffect = "success" }, want: "partial_effect"},
		{name: "unwitnessed deviation", mutate: func(profile *qualification.ToolProfile) {
			profile.Spec.Transports[0].Deviations = []qualification.ToolAffordanceDeviation{{
				ID: "fake-deviation", Summary: "Fake model-visible mismatch", Effect: "Fake tool call refuses", Evidence: []string{"missing-witness"},
			}}
		}, want: "unknown evidence id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := parseToolFixture(t)
			tt.mutate(&profile)

			_, err := qualification.MarshalToolProfile(profile)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("MarshalToolProfile() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestQualifiedToolRequiresExactHarnessWitness(t *testing.T) {
	profile, _ := qualifiedToolFixture(t, qualification.LifecycleStatusExperimental)
	profile.Evidence[0].Scope.Harnesses[0].IntegrationRevision = "temper-pi-tools/v2"
	key, err := qualification.EvidenceScopeKey(profile.Evidence[0].Scope)
	if err != nil {
		t.Fatal(err)
	}
	profile.Evidence[0].Scope.Key = key

	_, err = qualification.MarshalToolProfile(profile)
	if err == nil || !strings.Contains(err.Error(), "has no exact evidence witness") {
		t.Fatalf("MarshalToolProfile() error = %v, want exact harness-witness refusal", err)
	}
}

func TestParseToolProfileRefusesNoncanonicalOrAmbiguousYAML(t *testing.T) {
	canonical := string(readToolFixture(t))
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
		{name: "noncanonical mapping order", input: "schema: temper-qualification-tool/v1\n" + strings.Replace(canonical, "schema: temper-qualification-tool/v1\n", "", 1), want: "not canonical"},
		{name: "noncanonical integer", input: strings.Replace(canonical, "revision: 1", "revision: 01", 1), want: "not canonical"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := qualification.ParseToolProfile([]byte(tt.input))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParseToolProfile() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func readToolFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/tool.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func parseToolFixture(t *testing.T) qualification.ToolProfile {
	t.Helper()
	profile, err := qualification.ParseToolProfile(readToolFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	return profile
}
