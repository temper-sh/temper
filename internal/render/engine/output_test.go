package engine_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/render/engine"
)

func TestSplashOutputDefaultsFollowTheSelectedFrontendContract(t *testing.T) {
	for _, adapter := range []string{"", "splash/v1", "splash/v2"} {
		for _, limit := range []int{0, 64} {
			t.Run(fmt.Sprintf("%s/%d", adapter, limit), func(t *testing.T) {
				request := engine.Request{
					Engine: engine.Splash, LayoutID: "qwen", ModelPath: "/temper/model.gguf",
					ArtifactFormat: "gguf", Interface: engine.InterfaceChatCompletions,
					Modalities: []string{"text"}, Window: 4096, MaxTokens: limit, Thinking: "off", Speculation: "dflash2",
					Sampling: &engine.SamplingDefaults{Temperature: 1, TopK: 20, TopP: .95, RepeatPenalty: 1},
					Splash: &engine.SplashTuning{Adapter: adapter, ModelID: "owner/qwen", AssemblyPath: "/temper/prepared", StatePath: "/temper/state",
						SplashConfig: engine.SplashConfig{KVCache: "int8", MaxMemoryBytes: 24 << 30, ReasoningEffort: "none", RequestTimeoutSeconds: 600}},
				}
				command, err := engine.Build(request)
				if err != nil {
					t.Fatal(err)
				}
				runtime := command.Runtime()
				args := runtime.Requirement.Arguments
				value, capped := runtime.DefaultParameters["max_tokens"]
				if adapter == "splash/v2" {
					if slices.Contains(args, "--max-new-tokens") || capped != (limit > 0) || capped && value != limit {
						t.Fatalf("source frontend output default: argv=%q defaults=%v", args, runtime.DefaultParameters)
					}
				} else {
					want := "4096"
					if limit > 0 {
						want = "64"
					}
					i := slices.Index(args, "--max-new-tokens")
					if capped || i < 0 || i+1 >= len(args) || args[i+1] != want {
						t.Fatalf("legacy frontend output default changed: argv=%q defaults=%v", args, runtime.DefaultParameters)
					}
				}
			})
		}
	}
}

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

func TestLlamaThreadDefaultsOmitOnlyTheUnspecifiedCounts(t *testing.T) {
	for _, counts := range [][2]int{{0, 0}, {4, 0}, {0, 8}, {4, 8}, {-1, 4}, {4, -1}} {
		t.Run(fmt.Sprint(counts), func(t *testing.T) {
			r := validLlamaServerRequest()
			r.LlamaServer.Controls = &engine.LlamaServerControls{ReasoningEffort: "medium", Fit: "off", LoadMode: "auto", Threads: counts[0], ThreadsBatch: counts[1]}
			command, err := engine.Build(r)
			if counts[0] < 0 || counts[1] < 0 {
				if err == nil {
					t.Fatal("negative thread count accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			text := strings.Join(command.Lines(), "\n")
			for i, flag := range []string{"--threads ", "--threads-batch "} {
				if strings.Contains(text, flag) != (counts[i] > 0) {
					t.Fatalf("wrong thread default: %s", text)
				}
				if counts[i] > 0 && !strings.Contains(text, fmt.Sprintf("%s%d", flag, counts[i])) {
					t.Fatalf("lost explicit count: %s", text)
				}
			}
		})
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
