package qualification

import "fmt"

const RuntimeLayoutContractV1 = "temper-runtime-layout/v1"

// ModelRuntimeProfile binds one artifact and engine to output-affecting layout
// identity and an explicit performance inventory. Placement remains a mode
// concern and user selection remains outside the qualification catalog.
type ModelRuntimeProfile struct {
	ProfileEnvelope `yaml:",inline"`
	Spec            ModelRuntimeSpec `yaml:"spec"`
}

type ModelRuntimeSpec struct {
	ArtifactProfile Reference          `yaml:"artifact_profile"`
	EngineProfile   Reference          `yaml:"engine_profile"`
	Layout          RuntimeLayout      `yaml:"layout"`
	Performance     RuntimePerformance `yaml:"performance"`
}

type RuntimeLayout struct {
	Role         string             `yaml:"role"`
	Window       uint64             `yaml:"window"`
	MaxTokens    uint64             `yaml:"max_tokens,omitempty"`
	KV           string             `yaml:"kv,omitempty"`
	Thinking     string             `yaml:"thinking,omitempty"`
	ChatTemplate string             `yaml:"chat_template"`
	Batching     RuntimeBatching    `yaml:"batching"`
	Speculation  RuntimeSpeculation `yaml:"speculation"`
	Sampling     RuntimeSampling    `yaml:"sampling"`
}

type RuntimeBatching struct {
	Parallel       uint64 `yaml:"parallel"`
	FlashAttention string `yaml:"flash_attention"`
	Batch          uint64 `yaml:"batch"`
	UBatch         uint64 `yaml:"ubatch"`
}

type RuntimeSpeculation struct {
	State          string `yaml:"state"`
	MethodRevision string `yaml:"method_revision,omitempty"`
	Sidecar        string `yaml:"sidecar,omitempty"`
	DraftTokens    uint64 `yaml:"draft_tokens,omitempty"`
}

type RuntimeSampling struct {
	State       string  `yaml:"state"`
	Temperature string  `yaml:"temperature,omitempty"`
	TopP        string  `yaml:"top_p,omitempty"`
	TopK        *uint64 `yaml:"top_k,omitempty"`
	MinP        string  `yaml:"min_p,omitempty"`
	Seed        *int64  `yaml:"seed,omitempty"`
	Unspecified string  `yaml:"unspecified_parameters,omitempty"`
}

// ParseModelRuntimeProfile accepts only canonical model-runtime YAML.
func ParseModelRuntimeProfile(data []byte) (ModelRuntimeProfile, error) {
	return decodeCanonicalProfile(data, "model runtime profile", MarshalModelRuntimeProfile)
}

// MarshalModelRuntimeProfile validates a model runtime and returns canonical
// YAML.
func MarshalModelRuntimeProfile(profile ModelRuntimeProfile) ([]byte, error) {
	return encodeCanonicalProfile(profile, "model runtime profile", ModelRuntimeProfile.Validate)
}

// Validate enforces the v1 common envelope, exact dependency roots, layout,
// and explicit performance axes.
func (p ModelRuntimeProfile) Validate() error {
	var problems []string
	problem := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	validateProfileEnvelope(p.ProfileEnvelope, ModelRuntimeSchemaV1, problem)
	validateRuntimeReference("spec.artifact_profile", p.Spec.ArtifactProfile, ModelArtifactSchemaV1, problem)
	validateRuntimeReference("spec.engine_profile", p.Spec.EngineProfile, EngineSchemaV1, problem)
	validateRuntimeDependencies(p.Dependencies, p.Spec, problem)
	validateRuntimeLayout(p.Spec.Layout, problem)
	if len(p.Roles) != 1 || p.Roles[0] != p.Spec.Layout.Role {
		problem("roles must contain exactly the layout role %q", p.Spec.Layout.Role)
	}
	validateRuntimePerformance(p.Spec.Performance, p.Evidence, problem)
	validateModelRuntimeEvidenceScopes(p, problem)
	validateQualifiedRuntime(p, problem)

	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

func validateModelRuntimeEvidenceScopes(profile ModelRuntimeProfile, problem func(string, ...any)) {
	for index, evidence := range profile.Evidence {
		location := fmt.Sprintf("evidence[%d].scope", index)
		if !scopeReferenceEqualsReference(evidence.Scope.ArtifactProfile, profile.Spec.ArtifactProfile) {
			problem("%s.artifact_profile must exactly match spec.artifact_profile", location)
		}
		if !scopeReferenceEqualsReference(evidence.Scope.EngineProfile, profile.Spec.EngineProfile) {
			problem("%s.engine_profile must exactly match spec.engine_profile", location)
		}
		if evidence.Scope.MachineBucket != nil && !referenceSetContains(profile.Applicability.MachineBuckets, *evidence.Scope.MachineBucket) {
			problem("%s.machine_bucket must be present in applicability.machine_buckets", location)
		}
	}
}

func referenceSetContains(references []Reference, want Reference) bool {
	for _, reference := range references {
		if reference == want {
			return true
		}
	}
	return false
}

func validateRuntimeReference(location string, reference Reference, schema string, problem func(string, ...any)) {
	validateReference(location, reference, problem)
	if reference.Schema != schema {
		problem("%s schema is %q, want %q", location, reference.Schema, schema)
	}
}

func validateRuntimeDependencies(dependencies []ProfileDependency, spec ModelRuntimeSpec, problem func(string, ...any)) {
	want := []ProfileDependency{
		{Relationship: "artifact", Profile: spec.ArtifactProfile},
		{Relationship: "engine", Profile: spec.EngineProfile},
	}
	if len(dependencies) != len(want) {
		problem("model runtime dependencies must contain exactly artifact and engine")
		return
	}
	for index := range want {
		if dependencies[index] != want[index] {
			problem("dependencies[%d] must exactly repeat the spec.%s_profile reference", index, want[index].Relationship)
		}
	}
}

func validateRuntimeLayout(layout RuntimeLayout, problem func(string, ...any)) {
	if layout.Role != "coder" && layout.Role != "rerank" {
		problem("spec.layout.role %q must be coder or rerank", layout.Role)
	}
	if layout.Window == 0 {
		problem("spec.layout.window must be greater than zero")
	}
	validateRuntimeBatching(layout.Batching, problem)
	validateRuntimeSpeculation(layout.Speculation, problem)
	validateRuntimeSampling(layout.Sampling, problem)

	switch layout.Role {
	case "coder":
		if layout.MaxTokens == 0 || layout.MaxTokens >= layout.Window {
			problem("spec.layout.max_tokens must be greater than zero and below window for coder")
		}
		if layout.KV != "q8" && layout.KV != "f16" {
			problem("spec.layout.kv %q must be q8 or f16 for coder", layout.KV)
		}
		if layout.Thinking != "on" && layout.Thinking != "off" {
			problem("spec.layout.thinking %q must be on or off for coder", layout.Thinking)
		}
		if layout.ChatTemplate != "artifact" {
			problem("spec.layout.chat_template must be artifact for coder")
		}
		if layout.Sampling.State != "configured" {
			problem("spec.layout.sampling.state must be configured for coder")
		}
	case "rerank":
		if layout.MaxTokens != 0 || layout.KV != "" || layout.Thinking != "" {
			problem("rerank layout cannot declare coder-only max_tokens, kv, or thinking")
		}
		if layout.ChatTemplate != "not-applicable" {
			problem("spec.layout.chat_template must be not-applicable for rerank")
		}
		if layout.Speculation.State != "disabled" {
			problem("spec.layout.speculation.state must be disabled for rerank")
		}
		if layout.Sampling.State != "not-applicable" {
			problem("spec.layout.sampling.state must be not-applicable for rerank")
		}
	}
}

func validateRuntimeBatching(batching RuntimeBatching, problem func(string, ...any)) {
	if batching.Parallel == 0 {
		problem("spec.layout.batching.parallel must be greater than zero")
	}
	if batching.Batch == 0 {
		problem("spec.layout.batching.batch must be greater than zero")
	}
	if batching.UBatch == 0 {
		problem("spec.layout.batching.ubatch must be greater than zero")
	}
	if batching.Batch > 0 && batching.UBatch > batching.Batch {
		problem("spec.layout.batching.ubatch must not exceed batch")
	}
	if batching.FlashAttention != "auto" && batching.FlashAttention != "off" && batching.FlashAttention != "on" {
		problem("spec.layout.batching.flash_attention %q must be auto, off, or on", batching.FlashAttention)
	}
}

func validateRuntimeSpeculation(speculation RuntimeSpeculation, problem func(string, ...any)) {
	switch speculation.State {
	case "disabled":
		if speculation.MethodRevision != "" || speculation.Sidecar != "" || speculation.DraftTokens != 0 {
			problem("spec.layout.speculation details must be absent when state is disabled")
		}
	case "drafter":
		validateEnabledSpeculation(speculation, problem)
		if !safeArtifactPath(speculation.Sidecar) {
			problem("spec.layout.speculation.sidecar %q is not a safe canonical relative path", speculation.Sidecar)
		}
	case "mtp":
		validateEnabledSpeculation(speculation, problem)
		if speculation.Sidecar != "" {
			problem("spec.layout.speculation.sidecar must be absent for mtp")
		}
	default:
		problem("spec.layout.speculation.state %q must be disabled, drafter, or mtp", speculation.State)
	}
}

func validateEnabledSpeculation(speculation RuntimeSpeculation, problem func(string, ...any)) {
	if !exactRevisionIDPattern.MatchString(speculation.MethodRevision) {
		problem("spec.layout.speculation.method_revision %q is not an exact revision", speculation.MethodRevision)
	}
	if speculation.DraftTokens == 0 {
		problem("spec.layout.speculation.draft_tokens must be greater than zero when enabled")
	}
}

func validateRuntimeSampling(sampling RuntimeSampling, problem func(string, ...any)) {
	switch sampling.State {
	case "configured":
		validateCanonicalNonnegativeDecimal("spec.layout.sampling.temperature", sampling.Temperature, problem)
		validateCanonicalDecimal("spec.layout.sampling.top_p", sampling.TopP, "0", "1", problem)
		if sampling.TopP == "0" {
			problem("spec.layout.sampling.top_p must be greater than zero")
		}
		validateCanonicalDecimal("spec.layout.sampling.min_p", sampling.MinP, "0", "1", problem)
		if sampling.TopK == nil {
			problem("spec.layout.sampling.top_k is required when configured")
		}
		if sampling.Seed == nil {
			problem("spec.layout.sampling.seed is required when configured")
		}
		if sampling.Unspecified != "engine-defaults" {
			problem("spec.layout.sampling.unspecified_parameters must be engine-defaults when configured")
		}
	case "not-applicable":
		if sampling.Temperature != "" || sampling.TopP != "" || sampling.TopK != nil || sampling.MinP != "" || sampling.Seed != nil || sampling.Unspecified != "" {
			problem("spec.layout.sampling values must be absent when state is not-applicable")
		}
	default:
		problem("spec.layout.sampling.state %q must be configured or not-applicable", sampling.State)
	}
}
