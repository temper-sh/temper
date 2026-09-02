// Package engine turns semantic layout requests into exact process commands.
// It is pure: callers provide resolved paths and tuning, and it performs no
// filesystem, process, or network access.
package engine

import (
	"errors"
	"fmt"
)

const (
	LlamaServer = "llama-server"
	RapidMLX    = "rapid-mlx"
	MLXVLM      = "mlx-vlm"
	VLLMMetal   = "vllm-metal"

	InterfaceChatCompletions = "chat-completions"
	InterfaceReranking       = "reranking"
)

// Request is the closed launch-family input. Common fields carry semantic
// layout facts; exactly one selected engine owns a typed tuning block.
type Request struct {
	Engine            string
	LayoutID          string
	ModelPath         string
	ArtifactFormat    string
	KVCache           string
	Interface         string
	Modalities        []string
	Window            int
	MaxTokens         int
	Thinking          string
	Speculation       string
	SpeculativeTokens int
	ChatTemplatePath  string
	NGL               *int
	LlamaServer       *LlamaServerTuning
	RapidMLX          *RapidMLXTuning
	MLXVLM            *MLXVLMTuning
	VLLMMetal         *VLLMMetalTuning
}

// LlamaServerTuning is the typed llama.cpp-only tuning variant.
type LlamaServerTuning struct {
	Parallel           int
	FlashAttention     string
	Batch              int
	UBatch             int
	ContextCheckpoints *int
	PromptCacheRAMMiB  *int
}

// RapidMLXTuning is the typed Rapid-MLX-only tuning variant.
type RapidMLXTuning struct {
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

// MLXVLMTuning is the typed raw MLX-VLM-only tuning variant.
type MLXVLMTuning struct {
	MaxNumSeqs      int
	PrefillStepSize int
	VisionCacheSize int
	KVBits          *float64
	KVQuantScheme   string
	KVGroupSize     int
	MaxKVSize       *int
}

// VLLMMetalTuning is the typed vLLM-Metal-only tuning variant.
type VLLMMetalTuning struct {
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
	Package            string `json:"package"`
	RelativeExecutable string `json:"relative_executable"`
}

// Runtime describes supervisor metadata which is not part of shell command
// serialization.
type Runtime struct {
	Requirement   RuntimeRequirement
	Environment   []EnvironmentAssignment
	CheckEndpoint string
	UseModelName  string
}

// Command is an exact, safely serialized command for a process supervisor.
// Its representation stays private so callers cannot append unchecked shell.
type Command struct {
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
	return runtime
}

// Build dispatches one fresh request to its owned engine adapter.
func Build(request Request) (Command, error) {
	switch request.Engine {
	case LlamaServer, RapidMLX, MLXVLM, VLLMMetal:
	default:
		return Command{}, fmt.Errorf("engine %q is not supported", request.Engine)
	}
	if err := validateVariant(request); err != nil {
		return Command{}, err
	}
	switch request.Engine {
	case LlamaServer:
		return buildLlamaServer(request)
	case RapidMLX:
		return buildRapidMLX(request)
	case MLXVLM:
		return buildMLXVLM(request)
	case VLLMMetal:
		return buildVLLMMetal(request)
	}
	panic("unreachable engine dispatch")
}

func validateVariant(request Request) error {
	selected := 0
	for _, present := range []bool{
		request.LlamaServer != nil,
		request.RapidMLX != nil,
		request.MLXVLM != nil,
		request.VLLMMetal != nil,
	} {
		if present {
			selected++
		}
	}
	if selected != 1 {
		return errors.New("engine request must contain exactly one tuning variant")
	}
	matches := request.Engine == LlamaServer && request.LlamaServer != nil ||
		request.Engine == RapidMLX && request.RapidMLX != nil ||
		request.Engine == MLXVLM && request.MLXVLM != nil ||
		request.Engine == VLLMMetal && request.VLLMMetal != nil
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
