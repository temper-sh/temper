package engine_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/render/engine"
)

func TestRapidSidecarReachesNativeSpeculationAndProcessIdentity(t *testing.T) {
	request := engine.Request{
		Engine: engine.RapidMLX, LayoutID: "qwen", ModelPath: "/target",
		DraftModelPath: `/models/owner's "draft"/mtp.safetensors`,
		ArtifactFormat: "mlx-safetensors", Interface: engine.InterfaceChatCompletions,
		Modalities: []string{"text"}, Window: 65536, MaxTokens: 65536, Thinking: "off",
		Speculation: "mtp", SpeculativeTokens: 3,
		RapidMLX: &engine.RapidMLXTuning{MaxNumSeqs: 1, MaxConcurrentRequests: 1,
			PrefillBatchSize: 1, CompletionBatchSize: 1, GPUMemoryUtilization: 0.75,
			PrefixCache: "off", KVCacheDType: "bf16", PFlash: "off"},
	}
	command, err := engine.Build(request)
	if err != nil {
		t.Fatal(err)
	}
	args := command.Arguments()
	at := slices.Index(args, "--speculative-config")
	if at < 0 || at+1 >= len(args) {
		t.Fatal("MTP config absent")
	}
	var config struct {
		Method string `json:"method"`
		Model  string `json:"model"`
		Tokens int    `json:"num_speculative_tokens"`
	}
	if err := json.Unmarshal([]byte(args[at+1]), &config); err != nil {
		t.Fatal(err)
	}
	if config.Method != "mtp" || config.Model != request.DraftModelPath || config.Tokens != 3 {
		t.Fatalf("sidecar or depth changed: %#v", config)
	}
	if !slices.Equal(command.Runtime().Requirement.Arguments, args) {
		t.Fatal("process ownership contract lost the exact draft argument")
	}
	for _, change := range []func(*engine.Request){
		func(r *engine.Request) { r.Speculation, r.SpeculativeTokens = "none", 0 },
		func(r *engine.Request) { r.Modalities = []string{"text", "image"} },
		func(r *engine.Request) { r.DraftModelPath = "/draft\nunsafe" },
	} {
		invalid := request
		change(&invalid)
		if _, err := engine.Build(invalid); err == nil {
			t.Fatal("invalid Rapid draft request accepted")
		}
	}
}

func TestExternalDraftUsesExplicitPlacementAndIndependentCache(t *testing.T) {
	request := validLlamaServerRequest()
	request.Speculation, request.SpeculativeTokens = "dflash2", 15
	request.DraftModelPath = "/models/assistant's draft.gguf"
	cpu := 0
	request.NGL = &cpu
	command, err := engine.Build(request)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(command.Lines(), "\n")
	for _, want := range []string{"--spec-type draft-dflash", "--spec-draft-n-max 15", `--model-draft '/models/assistant'\''s draft.gguf'`, "--gpu-layers-draft 0", "--device-draft none", "--device none", "--no-kv-offload", "--no-op-offload", "--cache-type-k-draft f16", "--cache-type-v-draft f16", "-ctk q8_0 -ctv q8_0"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}

func TestExternalDraftRefusesSilentFallback(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*engine.Request)
	}{
		{"missing material", func(r *engine.Request) { r.DraftModelPath = "" }},
		{"missing placement", func(r *engine.Request) { r.NGL = nil }},
		{"no speculation", func(r *engine.Request) { r.Speculation, r.SpeculativeTokens = "none", 0 }},
		{"engine-owned block on llama", func(r *engine.Request) { r.SpeculativeTokens = 0 }},
		{"oversized block", func(r *engine.Request) { r.SpeculativeTokens = 16 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := validLlamaServerRequest()
			r.Speculation, r.SpeculativeTokens, r.DraftModelPath = "dflash2", 7, "/draft.gguf"
			gpu := 99
			r.NGL = &gpu
			tc.change(&r)
			if _, err := engine.Build(r); err == nil {
				t.Fatal("incomplete draft request accepted")
			}
		})
	}
}
