package engine_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/render/engine"
)

func TestLlamaOmittedOutputStopsAtContextWithoutShiftingHistory(t *testing.T) {
	for _, controls := range []*engine.LlamaServerControls{
		nil,
		{ContextShift: true, ReasoningEffort: "medium", Threads: 4, ThreadsBatch: 4, Fit: "off", LoadMode: "mmap"},
	} {
		request := validLlamaServerRequest()
		request.MaxTokens = 0
		request.LlamaServer.Controls = controls
		command, err := engine.Build(request)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.Join(command.Lines(), "\n")
		if !strings.Contains(text, "--predict -1") || !strings.Contains(text, "--no-context-shift") || strings.Contains(text, "\n--context-shift\n") {
			t.Fatalf("omitted cap must stop at the context boundary: %s", text)
		}
	}
}

func TestVLLMOutputDefaultsDoNotBecomeGenerationConfigCeilings(t *testing.T) {
	for _, limit := range []int{0, 4096} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			request := engine.Request{
				Engine: engine.VLLMMetal, LayoutID: "qwen", ModelPath: "/temper/model/qwen",
				ArtifactFormat: "safetensors", Interface: engine.InterfaceChatCompletions,
				Modalities: []string{"text"}, Window: 65536, MaxTokens: limit, Thinking: "off", Speculation: "none",
				VLLMMetal: &engine.VLLMMetalTuning{MaxNumSeqs: 1, MaxNumBatchedTokens: 4096, GPUMemoryUtilization: 0.9, KVCacheDType: "bfloat16", PrefixCache: "off"},
			}
			command, err := engine.Build(request)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.Join(command.Lines(), "\n"), `--override-generation-config '{"max_new_tokens":null}'`) {
				t.Fatal("model generation_config can still impose an output ceiling")
			}
			defaults := command.Runtime().DefaultParameters
			if limit == 0 && len(defaults) != 0 || limit > 0 && defaults["max_tokens"] != limit {
				t.Fatalf("request defaults = %v, want optional %d", defaults, limit)
			}
		})
	}
}

func TestRapidCannotSilentlyReplaceOmissionWithItsFiniteEngineDefault(t *testing.T) {
	request := engine.Request{
		Engine: engine.RapidMLX, LayoutID: "qwen", ModelPath: "/temper/model/qwen",
		ArtifactFormat: "mlx-safetensors", Interface: engine.InterfaceChatCompletions,
		Modalities: []string{"text"}, Window: 65536, Thinking: "off", Speculation: "none",
		RapidMLX: &engine.RapidMLXTuning{MaxNumSeqs: 1, MaxConcurrentRequests: 1, PrefillBatchSize: 1, CompletionBatchSize: 1, GPUMemoryUtilization: 0.9, PrefixCache: "off", KVCacheDType: "bf16", PFlash: "off"},
	}
	if _, err := engine.Build(request); err == nil || !strings.Contains(err.Error(), "cannot express remaining-context") {
		t.Fatalf("Rapid silently substituted its own cap: %v", err)
	}
}
