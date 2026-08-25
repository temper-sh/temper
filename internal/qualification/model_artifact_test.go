package qualification_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/qualification"
)

func TestParseModelArtifactProfileRoundTripsCanonicalFixture(t *testing.T) {
	data := readModelArtifactFixture(t)

	profile, err := qualification.ParseModelArtifactProfile(data)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Schema != qualification.ModelArtifactSchemaV1 || profile.ID != "example-coder-artifact" || profile.Revision != 1 || profile.QualificationStatus != qualification.QualificationStatusLab {
		t.Fatalf("profile identity = %#v", profile.ProfileEnvelope)
	}
	if profile.Spec.DeclaredDownloadBytes != 1472 || len(profile.Spec.Files) != 4 || profile.Spec.Quantization.TensorAllocation[0].TensorClass != "default" {
		t.Fatalf("artifact spec = %#v", profile.Spec)
	}

	encoded, err := qualification.MarshalModelArtifactProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, data) {
		t.Fatalf("round trip changed canonical bytes\n got:\n%s\nwant:\n%s", encoded, data)
	}
}

func TestModelArtifactProfileAcceptsImmediateExactSupersession(t *testing.T) {
	profile := parseModelArtifactFixture(t)
	profile.Revision = 2
	profile.Supersedes = &qualification.Reference{
		Schema: qualification.ModelArtifactSchemaV1, ID: profile.ID, Revision: 1, SHA256: strings.Repeat("a", 64),
	}

	if _, err := qualification.MarshalModelArtifactProfile(profile); err != nil {
		t.Fatalf("MarshalModelArtifactProfile() error = %v", err)
	}
}

func TestModelArtifactProfileAcceptsExactCalibrationProvenance(t *testing.T) {
	profile := parseModelArtifactFixture(t)
	profile.Spec.Quantization.Calibration = qualification.ArtifactCalibration{
		State: "referenced",
		Source: &qualification.MaterialReference{
			Schema: "example-calibration-record/v1", ID: "example-calibration", Revision: 1, SHA256: strings.Repeat("a", 64),
		},
	}

	if _, err := qualification.MarshalModelArtifactProfile(profile); err != nil {
		t.Fatalf("MarshalModelArtifactProfile() error = %v", err)
	}
}

func TestModelArtifactProfileValidationRefusesInvalidEnvelope(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*qualification.ModelArtifactProfile)
		want   string
	}{
		{name: "unknown schema", mutate: func(profile *qualification.ModelArtifactProfile) { profile.Schema = "unknown/v1" }, want: "schema is"},
		{name: "unstable id", mutate: func(profile *qualification.ModelArtifactProfile) { profile.ID = "Example_Profile" }, want: "lowercase stable id"},
		{name: "zero revision", mutate: func(profile *qualification.ModelArtifactProfile) { profile.Revision = 0 }, want: "revision must be greater"},
		{name: "initial supersession", mutate: func(profile *qualification.ModelArtifactProfile) {
			profile.Supersedes = &qualification.Reference{Schema: profile.Schema, ID: profile.ID, Revision: 1, SHA256: strings.Repeat("a", 64)}
		}, want: "initial revision must not supersede"},
		{name: "missing supersession", mutate: func(profile *qualification.ModelArtifactProfile) { profile.Revision = 2 }, want: "must supersede revision 1"},
		{name: "skipped supersession", mutate: func(profile *qualification.ModelArtifactProfile) {
			profile.Revision = 3
			profile.Supersedes = &qualification.Reference{Schema: profile.Schema, ID: profile.ID, Revision: 1, SHA256: strings.Repeat("a", 64)}
		}, want: "immediately precede"},
		{name: "unknown qualification status", mutate: func(profile *qualification.ModelArtifactProfile) { profile.QualificationStatus = "CURRENT" }, want: "qualification_status \"CURRENT\" is not supported"},
		{name: "missing qualification reason", mutate: func(profile *qualification.ModelArtifactProfile) { profile.QualificationReason = "" }, want: "qualification_reason must be nonempty"},
		{name: "unknown lifecycle status", mutate: func(profile *qualification.ModelArtifactProfile) { profile.LifecycleStatus = "CURRENT" }, want: "lifecycle_status \"CURRENT\" is not supported"},
		{name: "missing lifecycle reason", mutate: func(profile *qualification.ModelArtifactProfile) { profile.LifecycleReason = "" }, want: "lifecycle_reason must be nonempty"},
		{name: "initial retirement", mutate: func(profile *qualification.ModelArtifactProfile) {
			profile.LifecycleStatus = qualification.LifecycleStatusRetired
		}, want: "initial RETIRED lifecycle requires REJECTED"},
		{name: "supported lab evidence", mutate: func(profile *qualification.ModelArtifactProfile) {
			profile.LifecycleStatus = qualification.LifecycleStatusSupported
		}, want: "SUPPORTED lifecycle requires QUALIFIED"},
		{name: "rejected experiment", mutate: func(profile *qualification.ModelArtifactProfile) {
			profile.QualificationStatus = qualification.QualificationStatusRejected
		}, want: "REJECTED qualification requires RETIRED"},
		{name: "qualified without evidence", mutate: func(profile *qualification.ModelArtifactProfile) {
			profile.QualificationStatus = qualification.QualificationStatusQualified
		}, want: "require implemented qualification-gate"},
		{name: "incomplete evidence", mutate: func(profile *qualification.ModelArtifactProfile) {
			profile.Evidence = []qualification.ProfileEvidence{{ID: "example-evidence"}}
		}, want: "source.kind"},
		{name: "empty roles", mutate: func(profile *qualification.ModelArtifactProfile) { profile.Roles = nil }, want: "roles must not be empty"},
		{name: "unsorted roles", mutate: func(profile *qualification.ModelArtifactProfile) { profile.Roles = []string{"rerank", "coder"} }, want: "roles must be unique and sorted"},
		{name: "empty foregrounds", mutate: func(profile *qualification.ModelArtifactProfile) { profile.Applicability.Foregrounds = nil }, want: "foregrounds must not be empty"},
		{name: "none mixed with local", mutate: func(profile *qualification.ModelArtifactProfile) {
			profile.Applicability.Foregrounds = []string{"local", "none"}
		}, want: "none cannot be combined"},
		{name: "remote inference without harness credentials", mutate: func(profile *qualification.ModelArtifactProfile) {
			profile.DataBoundary.Inference = "harness-owned-remote"
		}, want: "requires harness-owned credentials"},
		{name: "telemetry", mutate: func(profile *qualification.ModelArtifactProfile) { profile.DataBoundary.Telemetry = "anonymous" }, want: "telemetry must be none"},
		{name: "implicit evidence export", mutate: func(profile *qualification.ModelArtifactProfile) { profile.DataBoundary.EvidenceExport = "automatic" }, want: "explicit-user-action"},
		{name: "empty invalidation triggers", mutate: func(profile *qualification.ModelArtifactProfile) { profile.InvalidationTriggers = nil }, want: "invalidation_triggers must not be empty"},
		{name: "wrong promotion schema", mutate: func(profile *qualification.ModelArtifactProfile) { profile.Promotion.Schema = "field-kit-session/v1" }, want: "promotion.schema"},
		{name: "profile dependency", mutate: func(profile *qualification.ModelArtifactProfile) {
			profile.Dependencies = []qualification.ProfileDependency{{
				Relationship: "engine", Profile: qualification.Reference{Schema: qualification.EngineSchemaV1, ID: "example-engine", Revision: 1, SHA256: strings.Repeat("a", 64)},
			}}
		}, want: "model artifact dependencies must be empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := parseModelArtifactFixture(t)
			tt.mutate(&profile)

			_, err := qualification.MarshalModelArtifactProfile(profile)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("MarshalModelArtifactProfile() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestModelArtifactProfileValidationRefusesIncompleteArtifactIdentity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*qualification.ModelArtifactProfile)
		want   string
	}{
		{name: "unknown source", mutate: func(profile *qualification.ModelArtifactProfile) { profile.Spec.Source.Kind = "moving-model-selector" }, want: "source.kind"},
		{name: "unscoped repository", mutate: func(profile *qualification.ModelArtifactProfile) { profile.Spec.Source.Repository = "model" }, want: "must be owner/name"},
		{name: "moving revision", mutate: func(profile *qualification.ModelArtifactProfile) { profile.Spec.Source.Revision = "main" }, want: "40-character lowercase commit hash"},
		{name: "empty files", mutate: func(profile *qualification.ModelArtifactProfile) { profile.Spec.Files = nil }, want: "spec.files must not be empty"},
		{name: "unsorted files", mutate: func(profile *qualification.ModelArtifactProfile) {
			profile.Spec.Files[0], profile.Spec.Files[1] = profile.Spec.Files[1], profile.Spec.Files[0]
		}, want: "unique and sorted by path"},
		{name: "unsafe file path", mutate: func(profile *qualification.ModelArtifactProfile) { profile.Spec.Files[0].Path = "../model.gguf" }, want: "safe canonical relative path"},
		{name: "invalid file digest", mutate: func(profile *qualification.ModelArtifactProfile) { profile.Spec.Files[0].SHA256 = "nope" }, want: "64 lowercase hexadecimal"},
		{name: "empty file", mutate: func(profile *qualification.ModelArtifactProfile) { profile.Spec.Files[0].Size = 0 }, want: "size must be greater"},
		{name: "unknown file purpose", mutate: func(profile *qualification.ModelArtifactProfile) { profile.Spec.Files[0].Purpose = "maybe-model" }, want: "purpose \"maybe-model\" is not supported"},
		{name: "no weights file", mutate: func(profile *qualification.ModelArtifactProfile) { profile.Spec.Files[0].Purpose = "other" }, want: "at least one weights file"},
		{name: "wrong download sum", mutate: func(profile *qualification.ModelArtifactProfile) { profile.Spec.DeclaredDownloadBytes++ }, want: "want selected-file sum"},
		{name: "unstable model family", mutate: func(profile *qualification.ModelArtifactProfile) { profile.Spec.ModelFamily = "Example Family" }, want: "model_family"},
		{name: "unknown format", mutate: func(profile *qualification.ModelArtifactProfile) { profile.Spec.Format = "mystery" }, want: "format \"mystery\" is not supported"},
		{name: "missing default tensor allocation", mutate: func(profile *qualification.ModelArtifactProfile) {
			profile.Spec.Quantization.TensorAllocation = profile.Spec.Quantization.TensorAllocation[1:]
		}, want: "include a default tensor class"},
		{name: "unsorted tensor allocation", mutate: func(profile *qualification.ModelArtifactProfile) {
			profile.Spec.Quantization.TensorAllocation[0], profile.Spec.Quantization.TensorAllocation[1] = profile.Spec.Quantization.TensorAllocation[1], profile.Spec.Quantization.TensorAllocation[0]
		}, want: "sorted by tensor_class"},
		{name: "calibration source absent", mutate: func(profile *qualification.ModelArtifactProfile) {
			profile.Spec.Quantization.Calibration.State = "referenced"
		}, want: "source is required"},
		{name: "tokenizer missing", mutate: func(profile *qualification.ModelArtifactProfile) { profile.Spec.Tokenizer.Path = "missing.json" }, want: "does not reference spec.files"},
		{name: "tokenizer not applicable", mutate: func(profile *qualification.ModelArtifactProfile) {
			profile.Spec.Tokenizer = qualification.ArtifactComponent{State: "not-applicable"}
		}, want: "tokenizer cannot be not-applicable"},
		{name: "tokenizer points at sidecar", mutate: func(profile *qualification.ModelArtifactProfile) {
			profile.Spec.Tokenizer.Path = "sidecars/drafter.gguf"
		}, want: "want tokenizer or weights"},
		{name: "template missing path", mutate: func(profile *qualification.ModelArtifactProfile) { profile.Spec.Template.Path = "" }, want: "does not reference spec.files"},
		{name: "sidecar missing", mutate: func(profile *qualification.ModelArtifactProfile) { profile.Spec.Sidecars = nil }, want: "must include \"sidecars/drafter.gguf\""},
		{name: "non-sidecar listed", mutate: func(profile *qualification.ModelArtifactProfile) {
			profile.Spec.Sidecars = []string{"models/example.gguf", "sidecars/drafter.gguf"}
		}, want: "non-sidecar purpose"},
		{name: "unknown license", mutate: func(profile *qualification.ModelArtifactProfile) { profile.Spec.License.ID = "Example License" }, want: "license.id"},
		{name: "vendored redistribution", mutate: func(profile *qualification.ModelArtifactProfile) { profile.Spec.License.Redistribution = "vendored" }, want: "referenced-not-vendored"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := parseModelArtifactFixture(t)
			tt.mutate(&profile)

			_, err := qualification.MarshalModelArtifactProfile(profile)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("MarshalModelArtifactProfile() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestParseModelArtifactProfileRefusesNoncanonicalOrAmbiguousYAML(t *testing.T) {
	canonical := string(readModelArtifactFixture(t))
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
		{name: "noncanonical mapping order", input: "schema: temper-qualification-model-artifact/v1\n" + strings.Replace(canonical, "schema: temper-qualification-model-artifact/v1\n", "", 1), want: "not canonical"},
		{name: "noncanonical integer", input: strings.Replace(canonical, "revision: 1", "revision: 01", 1), want: "not canonical"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := qualification.ParseModelArtifactProfile([]byte(tt.input))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParseModelArtifactProfile() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func readModelArtifactFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/model-artifact.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func parseModelArtifactFixture(t *testing.T) qualification.ModelArtifactProfile {
	t.Helper()
	profile, err := qualification.ParseModelArtifactProfile(readModelArtifactFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	return profile
}
