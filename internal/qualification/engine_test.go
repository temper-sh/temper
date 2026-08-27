package qualification_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/qualification"
)

func TestParseEngineProfileRoundTripsCanonicalFixture(t *testing.T) {
	data := readEngineFixture(t)

	profile, err := qualification.ParseEngineProfile(data)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Schema != qualification.EngineSchemaV1 || profile.ID != "example-local-engine" || profile.Revision != 1 || profile.QualificationStatus != qualification.QualificationStatusLab {
		t.Fatalf("profile identity = %#v", profile.ProfileEnvelope)
	}
	if profile.Spec.Software.RootVersion != "b1234" || profile.Spec.API.LayoutContract != qualification.RuntimeLayoutContractV1 || profile.Spec.API.Protocol != "openai-chat-completions/v1" || profile.Spec.ServiceContract.Shutdown.Signal != "SIGTERM" {
		t.Fatalf("engine spec = %#v", profile.Spec)
	}

	encoded, err := qualification.MarshalEngineProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, data) {
		t.Fatalf("round trip changed canonical bytes\n got:\n%s\nwant:\n%s", encoded, data)
	}
}

func TestEngineProfileValidationRefusesIncompleteSoftwareIdentity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*qualification.EngineProfile)
		want   string
	}{
		{name: "wrong catalog schema", mutate: func(profile *qualification.EngineProfile) { profile.Spec.Software.Catalog.Schema = "unknown/v1" }, want: "catalog.schema"},
		{name: "zero catalog sequence", mutate: func(profile *qualification.EngineProfile) { profile.Spec.Software.Catalog.Sequence = 0 }, want: "sequence must be greater"},
		{name: "invalid catalog digest", mutate: func(profile *qualification.EngineProfile) { profile.Spec.Software.Catalog.SHA256 = "nope" }, want: "catalog.sha256"},
		{name: "unstable package", mutate: func(profile *qualification.EngineProfile) { profile.Spec.Software.Package = "Example Engine" }, want: "software.package"},
		{name: "unstable method", mutate: func(profile *qualification.EngineProfile) { profile.Spec.Software.Method = "Release Artifact" }, want: "software.method"},
		{name: "unstable adapter", mutate: func(profile *qualification.EngineProfile) { profile.Spec.Software.Adapter = "Example Adapter" }, want: "software.adapter"},
		{name: "wrong operating system", mutate: func(profile *qualification.EngineProfile) { profile.Spec.Software.Target.OS = "linux" }, want: "unversioned darwin/arm64"},
		{name: "wrong architecture", mutate: func(profile *qualification.EngineProfile) { profile.Spec.Software.Target.Arch = "amd64" }, want: "unversioned darwin/arm64"},
		{name: "versioned distribution", mutate: func(profile *qualification.EngineProfile) {
			profile.Spec.Software.Target.Distribution = "macos"
			profile.Spec.Software.Target.DistributionVersion = "15"
		}, want: "unversioned darwin/arm64"},
		{name: "empty root version", mutate: func(profile *qualification.EngineProfile) { profile.Spec.Software.RootVersion = "" }, want: "root_version must be nonempty and trimmed"},
		{name: "multiline root version", mutate: func(profile *qualification.EngineProfile) { profile.Spec.Software.RootVersion = "b1234\nother" }, want: "must not contain control characters"},
		{name: "invalid closure digest", mutate: func(profile *qualification.EngineProfile) { profile.Spec.Software.ClosureDigest = "nope" }, want: "closure_digest"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := parseEngineFixture(t)
			tt.mutate(&profile)

			_, err := qualification.MarshalEngineProfile(profile)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("MarshalEngineProfile() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestEngineProfileValidationRefusesOpenServingContract(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*qualification.EngineProfile)
		want   string
	}{
		{name: "moving protocol", mutate: func(profile *qualification.EngineProfile) { profile.Spec.API.Protocol = "OpenAI latest" }, want: "exact protocol revision"},
		{name: "unknown layout contract", mutate: func(profile *qualification.EngineProfile) { profile.Spec.API.LayoutContract = "engine-defaults/v1" }, want: "api.layout_contract"},
		{name: "supported tool calls without request schema", mutate: func(profile *qualification.EngineProfile) { profile.Spec.API.ToolCalls.RequestSchema = "" }, want: "request_schema"},
		{name: "supported tool calls without response schema", mutate: func(profile *qualification.EngineProfile) { profile.Spec.API.ToolCalls.ResponseSchema = "" }, want: "response_schema"},
		{name: "supported tool calls without parser revision", mutate: func(profile *qualification.EngineProfile) { profile.Spec.API.ToolCalls.ParserRevision = "" }, want: "parser_revision"},
		{name: "unsupported tool calls retain details", mutate: func(profile *qualification.EngineProfile) { profile.Spec.API.ToolCalls.State = "unsupported" }, want: "must be absent"},
		{name: "unknown tool-call state", mutate: func(profile *qualification.EngineProfile) { profile.Spec.API.ToolCalls.State = "partial" }, want: "supported or unsupported"},
		{name: "empty capabilities", mutate: func(profile *qualification.EngineProfile) { profile.Spec.Capabilities = nil }, want: "capabilities must not be empty"},
		{name: "unknown capability", mutate: func(profile *qualification.EngineProfile) { profile.Spec.Capabilities[0] = "magic" }, want: "is not supported"},
		{name: "unsorted capabilities", mutate: func(profile *qualification.EngineProfile) {
			profile.Spec.Capabilities[0], profile.Spec.Capabilities[1] = profile.Spec.Capabilities[1], profile.Spec.Capabilities[0]
		}, want: "unique and sorted"},
		{name: "streaming disagreement", mutate: func(profile *qualification.EngineProfile) { profile.Spec.API.Streaming = false }, want: "streaming capability"},
		{name: "tool-call disagreement", mutate: func(profile *qualification.EngineProfile) {
			profile.Spec.Capabilities = []string{"chat-completions", "streaming"}
		}, want: "tool-calls capability"},
		{name: "unknown process isolation", mutate: func(profile *qualification.EngineProfile) { profile.Spec.ProcessIsolation = "daemon" }, want: "foreground-child or isolated-service"},
		{name: "unknown readiness protocol", mutate: func(profile *qualification.EngineProfile) { profile.Spec.ServiceContract.Readiness.Protocol = "tcp" }, want: "must be http"},
		{name: "relative readiness path", mutate: func(profile *qualification.EngineProfile) { profile.Spec.ServiceContract.Readiness.Path = "health" }, want: "canonical absolute HTTP path"},
		{name: "unclean readiness path", mutate: func(profile *qualification.EngineProfile) {
			profile.Spec.ServiceContract.Readiness.Path = "/status/../health"
		}, want: "canonical absolute HTTP path"},
		{name: "readiness query", mutate: func(profile *qualification.EngineProfile) {
			profile.Spec.ServiceContract.Readiness.Path = "/health?full=true"
		}, want: "canonical absolute HTTP path"},
		{name: "unescaped readiness path", mutate: func(profile *qualification.EngineProfile) {
			profile.Spec.ServiceContract.Readiness.Path = "/health check"
		}, want: "canonical absolute HTTP path"},
		{name: "invalid readiness status", mutate: func(profile *qualification.EngineProfile) { profile.Spec.ServiceContract.Readiness.ExpectedStatus = 99 }, want: "valid HTTP status"},
		{name: "unknown shutdown mechanism", mutate: func(profile *qualification.EngineProfile) {
			profile.Spec.ServiceContract.Shutdown.Mechanism = "endpoint"
		}, want: "must be signal"},
		{name: "unsafe shutdown signal", mutate: func(profile *qualification.EngineProfile) { profile.Spec.ServiceContract.Shutdown.Signal = "SIGKILL" }, want: "SIGINT or SIGTERM"},
		{name: "zero shutdown grace", mutate: func(profile *qualification.EngineProfile) {
			profile.Spec.ServiceContract.Shutdown.GracePeriodMillis = 0
		}, want: "must be greater than zero"},
		{name: "network required after install", mutate: func(profile *qualification.EngineProfile) { profile.Spec.ServiceContract.OfflineAfterInstall = false }, want: "offline_after_install must be true"},
		{name: "qualification dependency", mutate: func(profile *qualification.EngineProfile) {
			profile.Dependencies = []qualification.ProfileDependency{{
				Relationship: "artifact", Profile: qualification.Reference{Schema: qualification.ModelArtifactSchemaV1, ID: "example-artifact", Revision: 1, SHA256: strings.Repeat("a", 64)},
			}}
		}, want: "engine dependencies must be empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := parseEngineFixture(t)
			tt.mutate(&profile)

			_, err := qualification.MarshalEngineProfile(profile)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("MarshalEngineProfile() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestParseEngineProfileRefusesNoncanonicalOrAmbiguousYAML(t *testing.T) {
	canonical := string(readEngineFixture(t))
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
		{name: "noncanonical mapping order", input: "schema: temper-qualification-engine/v1\n" + strings.Replace(canonical, "schema: temper-qualification-engine/v1\n", "", 1), want: "not canonical"},
		{name: "noncanonical integer", input: strings.Replace(canonical, "revision: 1", "revision: 01", 1), want: "not canonical"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := qualification.ParseEngineProfile([]byte(tt.input))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParseEngineProfile() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func readEngineFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/engine.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func parseEngineFixture(t *testing.T) qualification.EngineProfile {
	t.Helper()
	profile, err := qualification.ParseEngineProfile(readEngineFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	return profile
}
