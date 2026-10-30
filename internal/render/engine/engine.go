// Package engine turns semantic layout requests into exact process commands.
// It is pure: callers provide resolved paths and tuning, and it performs no
// filesystem, process, or network access.
package engine

import (
	"errors"
	"fmt"
	"maps"
	"math"
)

const (
	LlamaServer = "llama-server"
	RapidMLX    = "rapid-mlx"
	VLLMMetal   = "vllm-metal"
	Splash      = "splash"
	Kapitsa     = "kapitsa"

	InterfaceChatCompletions = "chat-completions"
	InterfaceReranking       = "reranking"
	CrispEmbed               = "crispembed"
)

// Request is the closed launch-family input. Common fields carry semantic
// layout facts; exactly one selected engine owns a typed tuning block.
type Request struct {
	CrispEmbed            *CrispEmbedTuning
	Engine                string
	LayoutID              string
	ModelPath             string
	DraftModelPath        string
	ProjectorPath         string
	ArtifactFormat        string
	KVCache               string
	Interface             string
	Modalities            []string
	Window                int
	MaxTokens             int // Zero means no separate output cap; the adapter maps it to native behavior.
	Thinking              string
	ReasoningBudgetTokens *int // Nil preserves the engine default; -1 is unrestricted.
	Speculation           string
	SpeculativeTokens     int
	ChatTemplatePath      string
	NGL                   *int
	LlamaServer           *LlamaServerTuning
	RapidMLX              *RapidMLXTuning
	VLLMMetal             *VLLMMetalTuning
	Splash                *SplashTuning
	Kapitsa               *KapitsaTuning
	Sampling              *SamplingDefaults
}

// LlamaServerTuning is the typed llama.cpp-only tuning variant.
type LlamaServerTuning struct {
	Parallel           int
	FlashAttention     string
	Batch              int
	UBatch             int
	ContextCheckpoints *int
	PromptCacheRAMMiB  *int
	Controls           *LlamaServerControls
}

// LlamaServerControls makes output-affecting defaults explicit for refreshed
// engine layouts. A nil block preserves the existing launch contract. Zero
// thread counts omit their flags so the engine chooses its defaults.
type LlamaServerControls struct {
	CheckpointMinStep *int   `yaml:"checkpoint_min_step,omitempty" json:"checkpoint_min_step,omitempty"`
	CacheReuse        int    `yaml:"cache_reuse" json:"cache_reuse"`
	ReasoningEffort   string `yaml:"reasoning_effort,omitempty" json:"reasoning_effort,omitempty"`
	PreserveReasoning bool   `yaml:"preserve_reasoning" json:"preserve_reasoning"`
	ContextShift      bool   `yaml:"context_shift" json:"context_shift"`
	CachePrompt       bool   `yaml:"cache_prompt" json:"cache_prompt"`
	Fit               string `yaml:"fit" json:"fit"`
	Threads           int    `yaml:"threads" json:"threads"`
	ThreadsBatch      int    `yaml:"threads_batch" json:"threads_batch"`
	CPUMoELayers      int    `yaml:"cpu_moe_layers,omitempty" json:"cpu_moe_layers,omitempty"`
	LoadMode          string `yaml:"load_mode" json:"load_mode"`
}

// SamplingDefaults contains independent optional server defaults. Nil delegates
// to the pinned engine; explicit requests may override supplied defaults.
type SamplingDefaults struct {
	Temperature     *float64 `yaml:"temperature,omitempty" json:"temperature,omitempty"`
	TopK            *int     `yaml:"top_k,omitempty" json:"top_k,omitempty"`
	TopP            *float64 `yaml:"top_p,omitempty" json:"top_p,omitempty"`
	MinP            *float64 `yaml:"min_p,omitempty" json:"min_p,omitempty"`
	RepeatPenalty   *float64 `yaml:"repeat_penalty,omitempty" json:"repeat_penalty,omitempty"`
	PresencePenalty *float64 `yaml:"presence_penalty,omitempty" json:"presence_penalty,omitempty"`
	// Seed fixes generation for repeatable runs. Without one the engine
	// picks a random seed, so asking again gives a new answer.
	Seed *int `yaml:"seed,omitempty" json:"seed,omitempty"`
}

// RapidMLXTuning is the typed Rapid-MLX-only tuning variant.
type RapidMLXTuning struct {
	ToolCallParser        string
	PrefillStepSize       int
	RequestTimeoutSeconds int
	MaxNumSeqs            int
	MaxConcurrentRequests int
	PrefillBatchSize      int
	CompletionBatchSize   int
	GPUMemoryUtilization  float64
	PrefixCache           string
	CacheMemoryMiB        *int
	KVCacheDType          string
	PFlash                string
	ReasoningParser       string
}

// VLLMMetalTuning is the typed vLLM-Metal-only tuning variant.
type VLLMMetalTuning struct {
	LanguageModelOnly    bool
	ChunkedPrefill       bool
	BlockSize            int
	ToolCallParser       string
	ReasoningParser      string
	MaxNumSeqs           int
	MaxNumBatchedTokens  int
	GPUMemoryUtilization float64
	KVCacheDType         string
	PrefixCache          string
}

// EnvironmentAssignment is one explicit child-process environment value.
type EnvironmentAssignment struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// RuntimeRequirement describes one executable required by a rendered launch.
// RelativeExecutable is resolved below the receipted package payload.
type RuntimeRequirement struct {
	Package            string   `json:"package"`
	RelativeExecutable string   `json:"relative_executable"`
	Role               string   `json:"role,omitempty"`
	Arguments          []string `json:"arguments,omitempty"`
}

// Runtime describes supervisor metadata which is not part of shell command
// serialization.
type Runtime struct {
	Requirement            RuntimeRequirement
	Environment            []EnvironmentAssignment
	CheckEndpoint          string
	UseModelName           string
	ContextWindow          int
	AdditionalRequirements []RuntimeRequirement
	DefaultParameters      map[string]any
}

// Command is an exact, safely serialized command for a process supervisor.
// Its representation stays private so callers cannot append unchecked shell.
type Command struct {
	spec    launchSpec
	lines   []string
	runtime Runtime
}

// Lines returns a defensive copy of the command's folded-shell lines.
func (c Command) Lines() []string {
	return append([]string(nil), c.lines...)
}

// Runtime returns a defensive copy of the command's supervisor metadata.
func (c Command) Runtime() Runtime {
	runtime := c.runtime
	runtime.Environment = append([]EnvironmentAssignment(nil), c.runtime.Environment...)
	runtime.Requirement.Arguments = append([]string(nil), runtime.Requirement.Arguments...)
	runtime.AdditionalRequirements = append([]RuntimeRequirement(nil), runtime.AdditionalRequirements...)
	for i := range runtime.AdditionalRequirements {
		runtime.AdditionalRequirements[i].Arguments = append([]string(nil), runtime.AdditionalRequirements[i].Arguments...)
	}
	runtime.DefaultParameters = maps.Clone(runtime.DefaultParameters)
	return runtime
}

// Build dispatches one fresh request to its owned engine adapter.
func Build(request Request) (Command, error) {
	if budget := request.ReasoningBudgetTokens; budget != nil {
		if request.Engine != LlamaServer || request.Interface != InterfaceChatCompletions || *budget < -1 || *budget > math.MaxInt32 {
			return Command{}, errors.New("reasoning_budget_tokens requires llama-server chat and a value from -1 to 2147483647")
		}
	}
	if request.Sampling != nil && request.Engine != LlamaServer && request.Engine != Splash && request.Engine != Kapitsa {
		return Command{}, errors.New("explicit sampling defaults require llama-server, Splash or Kapitsa")
	}
	switch request.Engine {
	case LlamaServer, RapidMLX, VLLMMetal, Splash, CrispEmbed, Kapitsa:
	default:
		return Command{}, fmt.Errorf("engine %q is not supported", request.Engine)
	}
	if err := validateVariant(request); err != nil {
		return Command{}, err
	}
	switch request.Engine {
	case Kapitsa:
		return buildKapitsa(request)
	case CrispEmbed:
		return buildCrispEmbed(request)
	case LlamaServer:
		return buildLlamaServer(request)
	case RapidMLX:
		return buildRapidMLX(request)
	case VLLMMetal:
		return buildVLLMMetal(request)
	case Splash:
		return buildSplash(request)
	}
	panic("unreachable engine dispatch")
}

func validateVariant(request Request) error {
	if request.ProjectorPath != "" && request.Engine != LlamaServer {
		return errors.New("a GGUF projector path requires llama-server")
	}
	if request.DraftModelPath != "" && request.Engine != LlamaServer && request.Engine != RapidMLX && request.Engine != Kapitsa {
		return errors.New("a draft path requires llama-server, Rapid MLX or Kapitsa")
	}
	selected := 0
	for _, present := range []bool{
		request.CrispEmbed != nil,
		request.LlamaServer != nil,
		request.RapidMLX != nil,
		request.VLLMMetal != nil,
		request.Splash != nil,
		request.Kapitsa != nil,
	} {
		if present {
			selected++
		}
	}
	if selected != 1 {
		return errors.New("engine request must contain exactly one tuning variant")
	}
	matches := request.Engine == LlamaServer && request.LlamaServer != nil ||
		request.Engine == Kapitsa && request.Kapitsa != nil ||
		request.Engine == RapidMLX && request.RapidMLX != nil ||
		request.Engine == VLLMMetal && request.VLLMMetal != nil ||
		request.Engine == Splash && request.Splash != nil ||
		request.Engine == CrispEmbed && request.CrispEmbed != nil
	if !matches {
		return fmt.Errorf("engine %q does not match its tuning variant", request.Engine)
	}
	return nil
}

func offlineEnvironment(extra ...EnvironmentAssignment) []EnvironmentAssignment {
	base := []EnvironmentAssignment{
		{Name: "DO_NOT_TRACK", Value: "1"},
		{Name: "HF_HUB_DISABLE_IMPLICIT_TOKEN", Value: "1"},
		{Name: "HF_HUB_DISABLE_TELEMETRY", Value: "1"},
		{Name: "HF_HUB_OFFLINE", Value: "1"},
		{Name: "TRANSFORMERS_OFFLINE", Value: "1"},
	}
	return append(base, extra...)
}
