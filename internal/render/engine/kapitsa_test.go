package engine_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/render/engine"
)

func kapitsaRequest() engine.Request {
	return engine.Request{Engine: engine.Kapitsa, LayoutID: "qwen", ModelPath: "/owned model", DraftModelPath: "/owned draft", ArtifactFormat: "mlx-safetensors", Interface: engine.InterfaceChatCompletions, Modalities: []string{"text"}, Window: 98304, Thinking: "off", Speculation: "dflash2",
		Sampling: &engine.SamplingDefaults{Temperature: new(float64(1)), TopP: new(float64(.95)), TopK: new(20), RepeatPenalty: new(float64(1)), Seed: new(421)}, Kapitsa: &engine.KapitsaTuning{KapitsaConfig: engine.KapitsaConfig{Slots: 1, MaxMemoryBytes: 25769803776, PrefillChunkTokens: 1024, ANE: true}, StatePath: "/owned home"}}
}

func TestKapitsaUsesExactMaterialBudgetAndOfflineRequestDefaults(t *testing.T) {
	r := kapitsaRequest()
	c, err := engine.Build(r)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"serve", "--host", "127.0.0.1", "--port", "${PORT}", "--model", "/owned model", "--model-id", "qwen", "--dflash", "/owned draft", "--slots", "1", "--context", "98304", "--mem-budget", "25.769803776", "--disk-cache", "0", "--prefill-chunk", "1024"}
	if !reflect.DeepEqual(c.Arguments(), want) {
		t.Fatal(c.Arguments())
	}
	runtime := c.Runtime()
	if !reflect.DeepEqual(runtime.Requirement.Arguments, want) || runtime.Requirement.Package != "kapitsa" || runtime.Requirement.RelativeExecutable != "kapitsa" || runtime.Requirement.Role != "engine" || runtime.CheckEndpoint != "/health" {
		t.Fatal(runtime)
	}
	if !reflect.DeepEqual(runtime.DefaultParameters, map[string]any{"temperature": float64(1), "top_p": .95, "top_k": 20, "seed": 421, "reasoning_effort": "none", "max_tokens": 98304}) {
		t.Fatal(runtime.DefaultParameters)
	}
	env := map[string]string{}
	for _, e := range runtime.Environment {
		env[e.Name] = e.Value
	}
	for name, value := range map[string]string{"KAPITSA_HOME": "/owned home", "KAPITSA_NO_AUTO_UPDATE": "1", "KAPITSA_NO_PROMPT": "1", "HF_HUB_OFFLINE": "1", "HF_HUB_DISABLE_TELEMETRY": "1", "NI_ANE": "1"} {
		if env[name] != value {
			t.Fatal("missing environment", name)
		}
	}
	if !strings.Contains(strings.Join(c.Lines(), " "), "'/owned model'") {
		t.Fatal("path was not safely quoted")
	}
}

func TestKapitsaRefusesImplicitOrUnsupportedExecution(t *testing.T) {
	for name, change := range map[string]func(*engine.Request){
		"implicit target":         func(r *engine.Request) { r.ModelPath = "qwen" },
		"implicit draft":          func(r *engine.Request) { r.DraftModelPath = "auto" },
		"template":                func(r *engine.Request) { r.ChatTemplatePath = "/template" },
		"slots":                   func(r *engine.Request) { r.Kapitsa.Slots = 0 },
		"chunk":                   func(r *engine.Request) { r.Kapitsa.PrefillChunkTokens = -1 },
		"budget":                  func(r *engine.Request) { r.Kapitsa.MaxMemoryBytes = 0 },
		"block":                   func(r *engine.Request) { r.SpeculativeTokens = 8 },
		"penalty":                 func(r *engine.Request) { r.Sampling.RepeatPenalty = new(float64(1.1)) },
		"seed":                    func(r *engine.Request) { r.Sampling.Seed = new(int(1 << 32)) },
		"native reasoning budget": func(r *engine.Request) { r.ReasoningBudgetTokens = new(1024) },
	} {
		t.Run(name, func(t *testing.T) {
			r := kapitsaRequest()
			change(&r)
			if _, err := engine.Build(r); err == nil {
				t.Fatal("invalid execution accepted")
			}
		})
	}
	if err := engine.KapitsaCompatibility("Apple M1", "14.0"); err != nil {
		t.Fatal("inherited another engine's platform floor", err)
	}
	if err := engine.KapitsaCompatibility("Apple M5", "13.6"); err == nil {
		t.Fatal("unsupported OS admitted")
	}
}
