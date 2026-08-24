package qualification_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/qualification"
)

func TestParseModelRuntimeProfileRoundTripsCanonicalFixture(t *testing.T) {
	data := readModelRuntimeFixture(t)

	profile, err := qualification.ParseModelRuntimeProfile(data)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Schema != qualification.ModelRuntimeSchemaV1 || profile.ID != "example-coder-runtime" || profile.Revision != 1 || profile.Status != qualification.ProfileStatusLab {
		t.Fatalf("profile identity = %#v", profile.ProfileEnvelope)
	}
	if profile.Spec.Layout.Role != "coder" || profile.Spec.Layout.Window != 8192 || profile.Spec.Performance.TaskSuccess.State != "unmeasured" {
		t.Fatalf("runtime spec = %#v", profile.Spec)
	}

	encoded, err := qualification.MarshalModelRuntimeProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, data) {
		t.Fatalf("round trip changed canonical bytes\n got:\n%s\nwant:\n%s", encoded, data)
	}
}

func TestModelRuntimeProfileValidationRefusesIncompleteComposition(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*qualification.ModelRuntimeProfile)
		want   string
	}{
		{name: "artifact schema", mutate: func(profile *qualification.ModelRuntimeProfile) {
			profile.Spec.ArtifactProfile.Schema = qualification.EngineSchemaV1
		}, want: "artifact_profile schema"},
		{name: "engine schema", mutate: func(profile *qualification.ModelRuntimeProfile) {
			profile.Spec.EngineProfile.Schema = qualification.ModelArtifactSchemaV1
		}, want: "engine_profile schema"},
		{name: "missing dependency", mutate: func(profile *qualification.ModelRuntimeProfile) { profile.Dependencies = profile.Dependencies[:1] }, want: "exactly artifact and engine"},
		{name: "dependency does not repeat spec", mutate: func(profile *qualification.ModelRuntimeProfile) {
			profile.Dependencies[0].Profile.SHA256 = strings.Repeat("f", 64)
		}, want: "exactly repeat"},
		{name: "several roles", mutate: func(profile *qualification.ModelRuntimeProfile) { profile.Roles = []string{"coder", "rerank"} }, want: "exactly the layout role"},
		{name: "role disagrees", mutate: func(profile *qualification.ModelRuntimeProfile) { profile.Roles = []string{"rerank"} }, want: "exactly the layout role"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := parseModelRuntimeFixture(t)
			tt.mutate(&profile)

			_, err := qualification.MarshalModelRuntimeProfile(profile)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("MarshalModelRuntimeProfile() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestModelRuntimeProfileValidationRefusesOpenLayoutIdentity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*qualification.ModelRuntimeProfile)
		want   string
	}{
		{name: "unknown role", mutate: func(profile *qualification.ModelRuntimeProfile) { profile.Spec.Layout.Role = "vision" }, want: "coder or rerank"},
		{name: "zero window", mutate: func(profile *qualification.ModelRuntimeProfile) { profile.Spec.Layout.Window = 0 }, want: "window must be greater"},
		{name: "zero max tokens", mutate: func(profile *qualification.ModelRuntimeProfile) { profile.Spec.Layout.MaxTokens = 0 }, want: "max_tokens"},
		{name: "max tokens reaches window", mutate: func(profile *qualification.ModelRuntimeProfile) {
			profile.Spec.Layout.MaxTokens = profile.Spec.Layout.Window
		}, want: "max_tokens"},
		{name: "unknown kv", mutate: func(profile *qualification.ModelRuntimeProfile) { profile.Spec.Layout.KV = "q4" }, want: "must be q8 or f16"},
		{name: "unknown thinking", mutate: func(profile *qualification.ModelRuntimeProfile) { profile.Spec.Layout.Thinking = "auto" }, want: "must be on or off"},
		{name: "template not artifact", mutate: func(profile *qualification.ModelRuntimeProfile) { profile.Spec.Layout.ChatTemplate = "default" }, want: "must be artifact"},
		{name: "zero parallel", mutate: func(profile *qualification.ModelRuntimeProfile) { profile.Spec.Layout.Batching.Parallel = 0 }, want: "parallel must be greater"},
		{name: "zero batch", mutate: func(profile *qualification.ModelRuntimeProfile) { profile.Spec.Layout.Batching.Batch = 0 }, want: "batch must be greater"},
		{name: "ubatch exceeds batch", mutate: func(profile *qualification.ModelRuntimeProfile) { profile.Spec.Layout.Batching.UBatch++ }, want: "must not exceed batch"},
		{name: "unknown flash attention", mutate: func(profile *qualification.ModelRuntimeProfile) {
			profile.Spec.Layout.Batching.FlashAttention = "sometimes"
		}, want: "must be auto, off, or on"},
		{name: "unknown speculation", mutate: func(profile *qualification.ModelRuntimeProfile) { profile.Spec.Layout.Speculation.State = "adaptive" }, want: "disabled, drafter, or mtp"},
		{name: "disabled speculation details", mutate: func(profile *qualification.ModelRuntimeProfile) { profile.Spec.Layout.Speculation.DraftTokens = 4 }, want: "details must be absent"},
		{name: "drafter without sidecar", mutate: func(profile *qualification.ModelRuntimeProfile) {
			profile.Spec.Layout.Speculation.State = "drafter"
			profile.Spec.Layout.Speculation.MethodRevision = "draft/v1"
			profile.Spec.Layout.Speculation.DraftTokens = 4
		}, want: "safe canonical relative path"},
		{name: "mtp with sidecar", mutate: func(profile *qualification.ModelRuntimeProfile) {
			profile.Spec.Layout.Speculation.State = "mtp"
			profile.Spec.Layout.Speculation.MethodRevision = "mtp/v1"
			profile.Spec.Layout.Speculation.DraftTokens = 4
			profile.Spec.Layout.Speculation.Sidecar = "sidecars/drafter.gguf"
		}, want: "must be absent for mtp"},
		{name: "noncanonical temperature", mutate: func(profile *qualification.ModelRuntimeProfile) { profile.Spec.Layout.Sampling.Temperature = "0.0" }, want: "canonical nonnegative decimal"},
		{name: "top p above one", mutate: func(profile *qualification.ModelRuntimeProfile) { profile.Spec.Layout.Sampling.TopP = "1.1" }, want: "between 0 and 1"},
		{name: "zero top p", mutate: func(profile *qualification.ModelRuntimeProfile) { profile.Spec.Layout.Sampling.TopP = "0" }, want: "must be greater than zero"},
		{name: "missing top k", mutate: func(profile *qualification.ModelRuntimeProfile) { profile.Spec.Layout.Sampling.TopK = nil }, want: "top_k is required"},
		{name: "missing seed", mutate: func(profile *qualification.ModelRuntimeProfile) { profile.Spec.Layout.Sampling.Seed = nil }, want: "seed is required"},
		{name: "open unspecified parameters", mutate: func(profile *qualification.ModelRuntimeProfile) {
			profile.Spec.Layout.Sampling.Unspecified = "client-choice"
		}, want: "unspecified_parameters must be engine-defaults"},
		{name: "not-applicable sampling retains values", mutate: func(profile *qualification.ModelRuntimeProfile) {
			profile.Spec.Layout.Sampling.State = "not-applicable"
		}, want: "values must be absent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := parseModelRuntimeFixture(t)
			tt.mutate(&profile)

			_, err := qualification.MarshalModelRuntimeProfile(profile)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("MarshalModelRuntimeProfile() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestModelRuntimeProfileAcceptsExplicitZeroSamplingValues(t *testing.T) {
	profile := parseModelRuntimeFixture(t)
	topK := uint64(0)
	seed := int64(0)
	profile.Spec.Layout.Sampling.TopK = &topK
	profile.Spec.Layout.Sampling.Seed = &seed

	data, err := qualification.MarshalModelRuntimeProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("seed: 0\n")) || !bytes.Contains(data, []byte("top_k: 0\n")) {
		t.Fatalf("canonical sampling omitted explicit zero values:\n%s", data)
	}
}

func TestModelRuntimeProfileAcceptsClosedRerankLayout(t *testing.T) {
	profile := parseModelRuntimeFixture(t)
	profile.Spec.Layout.Role = "rerank"
	profile.Spec.Layout.MaxTokens = 0
	profile.Spec.Layout.KV = ""
	profile.Spec.Layout.Thinking = ""
	profile.Spec.Layout.ChatTemplate = "not-applicable"
	profile.Spec.Layout.Sampling = qualification.RuntimeSampling{State: "not-applicable"}
	profile.Roles = []string{"rerank"}

	if _, err := qualification.MarshalModelRuntimeProfile(profile); err != nil {
		t.Fatalf("MarshalModelRuntimeProfile() error = %v", err)
	}
}

func TestModelRuntimePerformanceRequiresExplicitTypedAxes(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*qualification.ModelRuntimeProfile)
		want   string
	}{
		{name: "omitted axis", mutate: func(profile *qualification.ModelRuntimeProfile) {
			profile.Spec.Performance.Context = qualification.PerformanceAxis{}
		}, want: "context.state"},
		{name: "unmeasured without reason", mutate: func(profile *qualification.ModelRuntimeProfile) { profile.Spec.Performance.Memory.Reason = "" }, want: "memory.reason"},
		{name: "unmeasured with observations", mutate: func(profile *qualification.ModelRuntimeProfile) {
			profile.Spec.Performance.Throughput.Observations = []qualification.PerformanceObservation{{}}
		}, want: "must be absent"},
		{name: "measured without observations", mutate: func(profile *qualification.ModelRuntimeProfile) {
			profile.Spec.Performance.TaskSuccess.State = "measured"
			profile.Spec.Performance.TaskSuccess.Reason = ""
		}, want: "observations must not be empty"},
		{name: "unknown performance state", mutate: func(profile *qualification.ModelRuntimeProfile) {
			profile.Spec.Performance.Regressions.State = "estimated"
		}, want: "measured, not-applicable, or unmeasured"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := parseModelRuntimeFixture(t)
			tt.mutate(&profile)

			_, err := qualification.MarshalModelRuntimeProfile(profile)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("MarshalModelRuntimeProfile() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestModelRuntimePerformanceRefusesUntypedOrUnwitnessedMeasurement(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*qualification.PerformanceObservation)
		want   string
	}{
		{name: "unknown metric", mutate: func(observation *qualification.PerformanceObservation) { observation.Metric = "magic-score" }, want: "is not supported for throughput"},
		{name: "wrong value kind", mutate: func(observation *qualification.PerformanceObservation) {
			observation.Value.Kind = qualification.PerformanceValueInteger
		}, want: "want \"decimal\""},
		{name: "two value variants", mutate: func(observation *qualification.PerformanceObservation) {
			value := uint64(12)
			observation.Value.Integer = &value
		}, want: "exactly one typed value"},
		{name: "noncanonical decimal", mutate: func(observation *qualification.PerformanceObservation) { observation.Value.Decimal = "12.50" }, want: "canonical nonnegative decimal"},
		{name: "unknown witness", mutate: func(observation *qualification.PerformanceObservation) { observation.Witness = "missing-evidence" }, want: "unknown evidence id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := parseModelRuntimeFixture(t)
			profile.Spec.Performance.Throughput = qualification.PerformanceAxis{
				State: "measured",
				Observations: []qualification.PerformanceObservation{{
					Metric: "decode-tokens-per-second", Definition: "Fake decoded tokens divided by elapsed generation seconds", Witness: "fake-witness",
					Value: qualification.PerformanceValue{Kind: qualification.PerformanceValueDecimal, Decimal: "12.5"},
				}},
			}
			tt.mutate(&profile.Spec.Performance.Throughput.Observations[0])

			_, err := qualification.MarshalModelRuntimeProfile(profile)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("MarshalModelRuntimeProfile() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestModelRuntimePerformanceRefusesInvalidSuccessFraction(t *testing.T) {
	tests := []struct {
		name string
		rate qualification.PerformanceSuccessFraction
		want string
	}{
		{name: "no attempts", rate: qualification.PerformanceSuccessFraction{}, want: "attempts must be greater than zero"},
		{name: "too many successes", rate: qualification.PerformanceSuccessFraction{Attempts: 2, Successes: 3}, want: "successes must not exceed attempts"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := parseModelRuntimeFixture(t)
			profile.Spec.Performance.TaskSuccess = qualification.PerformanceAxis{
				State: "measured",
				Observations: []qualification.PerformanceObservation{{
					Metric: "first-attempt-task-success", Definition: "Fake first completed attempt for each retained task", Witness: "fake-witness",
					Value: qualification.PerformanceValue{Kind: qualification.PerformanceValueSuccessFraction, SuccessFraction: &tt.rate},
				}},
			}

			_, err := qualification.MarshalModelRuntimeProfile(profile)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("MarshalModelRuntimeProfile() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestParseModelRuntimeProfileRefusesNoncanonicalOrAmbiguousYAML(t *testing.T) {
	canonical := string(readModelRuntimeFixture(t))
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
		{name: "noncanonical mapping order", input: "schema: temper-qualification-model-runtime/v1\n" + strings.Replace(canonical, "schema: temper-qualification-model-runtime/v1\n", "", 1), want: "not canonical"},
		{name: "noncanonical integer", input: strings.Replace(canonical, "revision: 1", "revision: 01", 1), want: "not canonical"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := qualification.ParseModelRuntimeProfile([]byte(tt.input))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParseModelRuntimeProfile() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func readModelRuntimeFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/model-runtime.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func parseModelRuntimeFixture(t *testing.T) qualification.ModelRuntimeProfile {
	t.Helper()
	profile, err := qualification.ParseModelRuntimeProfile(readModelRuntimeFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	return profile
}
