package qualification

import (
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/temper-sh/temper/internal/software"
	softwarecatalog "github.com/temper-sh/temper/internal/software/catalog"
)

// EngineProfile binds a tested software-supply identity to one closed serving and
// process contract. It neither copies a software lock nor claims installation.
type EngineProfile struct {
	ProfileEnvelope `yaml:",inline"`
	Spec            EngineSpec `yaml:"spec"`
}

type EngineSpec struct {
	Software         EngineSoftware        `yaml:"software"`
	API              EngineAPI             `yaml:"api"`
	Capabilities     []string              `yaml:"capabilities"`
	ProcessIsolation string                `yaml:"process_isolation"`
	ServiceContract  EngineServiceContract `yaml:"service_contract"`
}

type EngineSoftware struct {
	Catalog       SoftwareCatalogReference `yaml:"catalog"`
	Package       string                   `yaml:"package"`
	Method        string                   `yaml:"method"`
	Adapter       string                   `yaml:"adapter"`
	Target        software.Target          `yaml:"target"`
	RootVersion   string                   `yaml:"root_version"`
	ClosureDigest string                   `yaml:"closure_digest"`
}

type SoftwareCatalogReference struct {
	Schema   string `yaml:"schema"`
	Sequence uint64 `yaml:"sequence"`
	SHA256   string `yaml:"sha256"`
}

type EngineAPI struct {
	LayoutContract string                `yaml:"layout_contract"`
	Protocol       string                `yaml:"protocol"`
	Streaming      bool                  `yaml:"streaming"`
	ToolCalls      EngineToolCallSurface `yaml:"tool_calls"`
}

type EngineToolCallSurface struct {
	State          string `yaml:"state"`
	RequestSchema  string `yaml:"request_schema,omitempty"`
	ResponseSchema string `yaml:"response_schema,omitempty"`
	ParserRevision string `yaml:"parser_revision,omitempty"`
}

type EngineServiceContract struct {
	Readiness           EngineReadiness `yaml:"readiness"`
	Shutdown            EngineShutdown  `yaml:"shutdown"`
	OfflineAfterInstall bool            `yaml:"offline_after_install"`
}

type EngineReadiness struct {
	Protocol       string `yaml:"protocol"`
	Path           string `yaml:"path"`
	ExpectedStatus int    `yaml:"expected_status"`
}

type EngineShutdown struct {
	Mechanism         string `yaml:"mechanism"`
	Signal            string `yaml:"signal"`
	GracePeriodMillis uint64 `yaml:"grace_period_millis"`
}

// ParseEngineProfile accepts only canonical engine-profile YAML.
func ParseEngineProfile(data []byte) (EngineProfile, error) {
	return decodeCanonicalProfile(data, "engine profile", MarshalEngineProfile)
}

// MarshalEngineProfile validates an engine profile and returns canonical YAML.
func MarshalEngineProfile(profile EngineProfile) ([]byte, error) {
	return encodeCanonicalProfile(profile, "engine profile", EngineProfile.Validate)
}

// Validate enforces the v1 common envelope and engine body.
func (p EngineProfile) Validate() error {
	var problems []string
	problem := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	validateProfileEnvelope(p.ProfileEnvelope, EngineSchemaV1, problem)
	if len(p.Dependencies) != 0 {
		problem("engine dependencies must be empty")
	}
	validateEngineEvidenceScopes(p, problem)
	validateEngineSpec(p.Spec, problem)

	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

func validateEngineEvidenceScopes(profile EngineProfile, problem func(string, ...any)) {
	for index, evidence := range profile.Evidence {
		if !scopeReferenceEqualsReference(evidence.Scope.EngineProfile, Reference{
			Schema: profile.Schema, ID: profile.ID, Revision: profile.Revision,
		}) {
			problem("evidence[%d].scope.engine_profile must identify the containing engine", index)
		}
	}
}

func validateEngineSpec(spec EngineSpec, problem func(string, ...any)) {
	if spec.Software.Catalog.Schema != softwarecatalog.SchemaV1 {
		problem("spec.software.catalog.schema is %q, want %q", spec.Software.Catalog.Schema, softwarecatalog.SchemaV1)
	}
	if spec.Software.Catalog.Sequence == 0 {
		problem("spec.software.catalog.sequence must be greater than zero")
	}
	if !sha256Pattern.MatchString(spec.Software.Catalog.SHA256) {
		problem("spec.software.catalog.sha256 must be 64 lowercase hexadecimal characters")
	}
	validateStableID("spec.software.package", spec.Software.Package, problem)
	validateStableID("spec.software.method", spec.Software.Method, problem)
	validateStableID("spec.software.adapter", spec.Software.Adapter, problem)
	if err := spec.Software.Target.Validate(); err != nil {
		problem("spec.software.target: %v", err)
	}
	wantTarget := software.Target{OS: "darwin", Arch: "arm64"}
	if spec.Software.Target != wantTarget {
		problem("spec.software.target must identify unversioned darwin/arm64")
	}
	validateLine("spec.software.root_version", spec.Software.RootVersion, problem)
	if !sha256Pattern.MatchString(spec.Software.ClosureDigest) {
		problem("spec.software.closure_digest must be 64 lowercase hexadecimal characters")
	}

	if spec.API.LayoutContract != RuntimeLayoutContractV1 {
		problem("spec.api.layout_contract is %q, want %q", spec.API.LayoutContract, RuntimeLayoutContractV1)
	}
	if !exactRevisionIDPattern.MatchString(spec.API.Protocol) {
		problem("spec.api.protocol %q is not an exact protocol revision", spec.API.Protocol)
	}
	validateToolCallSurface(spec.API.ToolCalls, problem)
	validateEngineCapabilities(spec, problem)
	if spec.ProcessIsolation != "foreground-child" && spec.ProcessIsolation != "isolated-service" {
		problem("spec.process_isolation %q must be foreground-child or isolated-service", spec.ProcessIsolation)
	}
	validateEngineServiceContract(spec.ServiceContract, problem)
}

func validateToolCallSurface(surface EngineToolCallSurface, problem func(string, ...any)) {
	switch surface.State {
	case "supported":
		if !exactRevisionIDPattern.MatchString(surface.RequestSchema) {
			problem("spec.api.tool_calls.request_schema %q is not an exact schema revision", surface.RequestSchema)
		}
		if !exactRevisionIDPattern.MatchString(surface.ResponseSchema) {
			problem("spec.api.tool_calls.response_schema %q is not an exact schema revision", surface.ResponseSchema)
		}
		if !exactRevisionIDPattern.MatchString(surface.ParserRevision) {
			problem("spec.api.tool_calls.parser_revision %q is not an exact parser revision", surface.ParserRevision)
		}
	case "unsupported":
		if surface.RequestSchema != "" || surface.ResponseSchema != "" || surface.ParserRevision != "" {
			problem("spec.api.tool_calls schemas and parser must be absent when state is unsupported")
		}
	default:
		problem("spec.api.tool_calls.state %q must be supported or unsupported", surface.State)
	}
}

func validateEngineCapabilities(spec EngineSpec, problem func(string, ...any)) {
	if len(spec.Capabilities) == 0 {
		problem("spec.capabilities must not be empty")
	}
	previous := ""
	for index, capability := range spec.Capabilities {
		location := fmt.Sprintf("spec.capabilities[%d]", index)
		switch capability {
		case "chat-completions", "drafter-speculation", "embeddings", "mtp-speculation", "rerank", "streaming", "tool-calls":
		default:
			problem("%s %q is not supported", location, capability)
		}
		if index > 0 && capability <= previous {
			problem("spec.capabilities must be unique and sorted")
		}
		previous = capability
	}
	if spec.API.Streaming != contains(spec.Capabilities, "streaming") {
		problem("spec.api.streaming must agree with the streaming capability")
	}
	toolCallsSupported := spec.API.ToolCalls.State == "supported"
	if toolCallsSupported != contains(spec.Capabilities, "tool-calls") {
		problem("spec.api.tool_calls state must agree with the tool-calls capability")
	}
}

func validateEngineServiceContract(contract EngineServiceContract, problem func(string, ...any)) {
	if contract.Readiness.Protocol != "http" {
		problem("spec.service_contract.readiness.protocol must be http")
	}
	if !safeHTTPPath(contract.Readiness.Path) {
		problem("spec.service_contract.readiness.path %q is not a canonical absolute HTTP path", contract.Readiness.Path)
	}
	if contract.Readiness.ExpectedStatus < 100 || contract.Readiness.ExpectedStatus > 599 {
		problem("spec.service_contract.readiness.expected_status must be a valid HTTP status")
	}
	if contract.Shutdown.Mechanism != "signal" {
		problem("spec.service_contract.shutdown.mechanism must be signal")
	}
	if contract.Shutdown.Signal != "SIGINT" && contract.Shutdown.Signal != "SIGTERM" {
		problem("spec.service_contract.shutdown.signal %q must be SIGINT or SIGTERM", contract.Shutdown.Signal)
	}
	if contract.Shutdown.GracePeriodMillis == 0 {
		problem("spec.service_contract.shutdown.grace_period_millis must be greater than zero")
	}
	if !contract.OfflineAfterInstall {
		problem("spec.service_contract.offline_after_install must be true")
	}
}

func validateStableID(location, value string, problem func(string, ...any)) {
	if !stableIDPattern.MatchString(value) {
		problem("%s %q is not a lowercase stable id", location, value)
	}
}

func safeHTTPPath(value string) bool {
	parsed, err := url.ParseRequestURI(value)
	return err == nil &&
		!parsed.IsAbs() && parsed.Host == "" && parsed.RawQuery == "" && parsed.Fragment == "" &&
		strings.HasPrefix(value, "/") && !strings.Contains(value, "\\") &&
		parsed.EscapedPath() == value && path.Clean(value) == value
}
