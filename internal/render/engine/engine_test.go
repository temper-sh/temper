package engine_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/render/engine"
)

func TestBuildMapsLlamaServerSemanticsToAnExactCommand(t *testing.T) {
	contextCheckpoints := 16
	promptCacheRAMMiB := 0
	ngl := 0
	request := validLlamaServerRequest()
	request.ChatTemplatePath = "/temper/artifacts/template.jinja"
	request.NGL = &ngl
	request.LlamaServer.ContextCheckpoints = &contextCheckpoints
	request.LlamaServer.PromptCacheRAMMiB = &promptCacheRAMMiB
	request.Speculation = "mtp"
	request.SpeculativeTokens = 3

	command, err := engine.Build(request)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"llama-server --host 127.0.0.1 --port ${PORT}",
		"-m '/temper/artifacts/model.gguf'",
		"--offline",
		"--no-mmproj",
		"--jinja",
		"--parallel 1",
		"-c 24576",
		"--ctx-checkpoints 16",
		"--cache-ram 0",
		"-fa on",
		"-ctk q8_0 -ctv q8_0",
		"-b 512",
		"-ub 512",
		"--spec-type draft-mtp",
		"--spec-draft-n-max 3",
		"--chat-template-file '/temper/artifacts/template.jinja'",
		"--reasoning off",
		"-ngl 0",
	}
	assertCommand(t, command, want, engine.Runtime{
		Requirement:   engine.RuntimeRequirement{Package: "llama-cpp", RelativeExecutable: "llama-server"},
		CheckEndpoint: "/health",
		ContextWindow: 24576,
	})
}

func TestBuildMapsRerankingSemanticsToAnExactCommand(t *testing.T) {
	ngl := 0
	request := validLlamaServerRequest()
	request.Interface = engine.InterfaceReranking
	request.Window = 4096
	request.MaxTokens = 0
	request.KVCache = ""
	request.Thinking = ""
	request.NGL = &ngl
	request.LlamaServer.FlashAttention = "auto"
	request.LlamaServer.Batch = 256
	request.LlamaServer.UBatch = 256

	command, err := engine.Build(request)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"llama-server --host 127.0.0.1 --port ${PORT}",
		"-m '/temper/artifacts/model.gguf'",
		"--offline",
		"--no-mmproj",
		"--reranking",
		"--parallel 1",
		"-c 4096",
		"-fa auto",
		"-b 256",
		"-ub 256",
		"-ngl 0",
	}
	if !reflect.DeepEqual(command.Lines(), want) {
		t.Fatalf("Build() lines =\n%q\nwant\n%q", command.Lines(), want)
	}
}

func TestBuildMapsRapidMLXSemanticsToAnExactCommand(t *testing.T) {
	cache := 0
	request := engine.Request{
		Engine: engine.RapidMLX, LayoutID: "glm-large", ModelPath: "/temper/model/glm",
		ArtifactFormat: "mlx-safetensors", Interface: engine.InterfaceChatCompletions,
		Modalities: []string{"text"}, Window: 131072, MaxTokens: 8192, Thinking: "off",
		Speculation: "mtp", SpeculativeTokens: 3,
		RapidMLX: &engine.RapidMLXTuning{
			MaxNumSeqs: 2, MaxConcurrentRequests: 2, PrefillBatchSize: 1,
			CompletionBatchSize: 2, GPUMemoryUtilization: 0.95, PrefixCache: "on",
			CacheMemoryMiB: &cache, KVCacheDType: "bf16", PFlash: "off",
		},
	}
	command, err := engine.Build(request)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"rapid-mlx --no-telemetry serve '/temper/model/glm'",
		"--host 127.0.0.1 --port ${PORT}",
		"--served-model-name 'glm-large'",
		"--max-tokens 8192",
		"--max-num-seqs 2",
		"--max-concurrent-requests 2",
		"--prefill-batch-size 1",
		"--completion-batch-size 2",
		"--gpu-memory-utilization 0.95",
		"--enable-prefix-cache",
		"--cache-memory-mb 0",
		"--kv-cache-dtype bf16",
		"--pflash off",
		"--no-mllm",
		"--no-thinking",
		"--speculative-config '{\"method\":\"mtp\",\"num_speculative_tokens\":3}'",
	}
	if !reflect.DeepEqual(command.Lines(), want) {
		t.Fatalf("Build() lines =\n%q\nwant\n%q", command.Lines(), want)
	}
	runtime := command.Runtime()
	if runtime.Requirement.Package != engine.RapidMLX || runtime.CheckEndpoint != "/health/ready" || runtime.ContextWindow != 131072 || !hasEnvironment(runtime, "HF_HUB_OFFLINE", "1") || !hasEnvironment(runtime, "RAPID_MLX_TELEMETRY", "0") {
		t.Fatalf("Build() runtime = %#v", runtime)
	}
}

func TestBuildMapsMLXVLMSemanticsWithoutPretendingMaxKVIsContext(t *testing.T) {
	kvBits := 3.5
	maxKV := 32768
	request := engine.Request{
		Engine: engine.MLXVLM, LayoutID: "vision", ModelPath: "/temper/model/vision",
		ArtifactFormat: "mlx-safetensors", Interface: engine.InterfaceChatCompletions,
		Modalities: []string{"text", "image"}, Window: 65536, MaxTokens: 4096, Thinking: "on",
		Speculation: "none",
		MLXVLM: &engine.MLXVLMTuning{
			MaxNumSeqs: 1, PrefillStepSize: 2048, VisionCacheSize: 8,
			KVBits: &kvBits, KVQuantScheme: "turboquant", KVGroupSize: 64, MaxKVSize: &maxKV,
		},
	}
	command, err := engine.Build(request)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(command.Lines(), "\n")
	for _, wanted := range []string{
		"mlx_vlm.server --host 127.0.0.1 --port ${PORT}",
		"--model '/temper/model/vision'", "--enable-thinking", "--kv-bits 3.5",
		"--kv-quant-scheme turboquant", "--max-kv-size 32768",
	} {
		if !strings.Contains(got, wanted) {
			t.Errorf("command does not contain %q:\n%s", wanted, got)
		}
	}
	if strings.Contains(got, "--max-model-len") || strings.Contains(got, "--max-kv-size 65536") {
		t.Fatalf("window was falsely mapped to MLX-VLM max KV size:\n%s", got)
	}
	if command.Runtime().UseModelName != "/temper/model/vision" {
		t.Fatalf("MLX-VLM did not pin the forwarded model name: %#v", command.Runtime())
	}
	if command.Runtime().ContextWindow != 65536 {
		t.Fatalf("MLX-VLM did not preserve the advertised context contract: %#v", command.Runtime())
	}
}

func TestBuildMapsVLLMMetalSemanticsToAnExactCommand(t *testing.T) {
	request := engine.Request{
		Engine: engine.VLLMMetal, LayoutID: "qwen", ModelPath: "/temper/model/qwen",
		ArtifactFormat: "safetensors", Interface: engine.InterfaceChatCompletions,
		Modalities: []string{"text"}, Window: 65536, MaxTokens: 4096, Thinking: "off",
		Speculation: "none",
		VLLMMetal: &engine.VLLMMetalTuning{
			MaxNumSeqs: 1, MaxNumBatchedTokens: 4096, GPUMemoryUtilization: 0.9,
			KVCacheDType: "bfloat16", PrefixCache: "off",
		},
	}
	command, err := engine.Build(request)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"vllm serve '/temper/model/qwen'",
		"--host 127.0.0.1 --port ${PORT}",
		"--served-model-name 'qwen'",
		"--max-model-len 65536",
		"--max-num-seqs 1",
		"--max-num-batched-tokens 4096",
		"--gpu-memory-utilization 0.9",
		"--kv-cache-dtype bfloat16",
		"--no-enable-prefix-caching",
		"--default-chat-template-kwargs '{\"enable_thinking\":false}'",
	}
	if !reflect.DeepEqual(command.Lines(), want) {
		t.Fatalf("Build() lines =\n%q\nwant\n%q", command.Lines(), want)
	}
	runtime := command.Runtime()
	if runtime.ContextWindow != 65536 || !hasEnvironment(runtime, "VLLM_METAL_USE_PAGED_ATTENTION", "1") || !hasEnvironment(runtime, "VLLM_NO_USAGE_STATS", "1") {
		t.Fatalf("Build() runtime = %#v", runtime)
	}
}

func TestBuildRefusesVLLMMetalMTPWithPrefixCaching(t *testing.T) {
	request := engine.Request{
		Engine: engine.VLLMMetal, LayoutID: "qwen", ModelPath: "/temper/model/qwen",
		ArtifactFormat: "safetensors", Interface: engine.InterfaceChatCompletions,
		Modalities: []string{"text"}, Window: 65536, MaxTokens: 4096, Thinking: "off",
		Speculation: "mtp", SpeculativeTokens: 2,
		VLLMMetal: &engine.VLLMMetalTuning{
			MaxNumSeqs: 1, MaxNumBatchedTokens: 4096, GPUMemoryUtilization: 0.9,
			KVCacheDType: "bfloat16", PrefixCache: "on",
		},
	}

	_, err := engine.Build(request)
	if err == nil || !strings.Contains(err.Error(), "MTP with prefix caching is refused") {
		t.Fatalf("Build() error = %v, want an MTP/prefix-cache refusal", err)
	}
}

func TestBuildDistinguishesOmittedTuningFromExplicitZero(t *testing.T) {
	withoutOverrides, err := engine.Build(validLlamaServerRequest())
	if err != nil {
		t.Fatal(err)
	}
	without := strings.Join(withoutOverrides.Lines(), "\n")
	if strings.Contains(without, "--ctx-checkpoints") || strings.Contains(without, "--cache-ram") {
		t.Fatalf("omitted tuning rendered an override:\n%s", without)
	}

	zero := 0
	request := validLlamaServerRequest()
	request.LlamaServer.ContextCheckpoints = &zero
	request.LlamaServer.PromptCacheRAMMiB = &zero
	withOverrides, err := engine.Build(request)
	if err != nil {
		t.Fatal(err)
	}
	with := strings.Join(withOverrides.Lines(), "\n")
	for _, wanted := range []string{"--ctx-checkpoints 0", "--cache-ram 0"} {
		if !strings.Contains(with, wanted) {
			t.Errorf("explicit zero did not render %q:\n%s", wanted, with)
		}
	}
}

func TestBuildQuotesDataButPreservesTheOwnedPortPlaceholder(t *testing.T) {
	request := validLlamaServerRequest()
	request.ModelPath = "/temper/model's.gguf"

	command, err := engine.Build(request)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(command.Lines(), "\n")
	if !strings.Contains(got, `-m '/temper/model'\''s.gguf'`) {
		t.Fatalf("model path was not shell quoted:\n%s", got)
	}
	if !strings.Contains(got, "llama-server --host 127.0.0.1 --port ${PORT}") {
		t.Fatalf("owned port placeholder was not preserved:\n%s", got)
	}
}

func TestBuildRefusesInvalidOrMismatchedVariants(t *testing.T) {
	tests := []struct {
		name string
		edit func(*engine.Request)
		want string
	}{
		{name: "control character", edit: func(request *engine.Request) { request.ModelPath = "/temper/model\nunsafe.gguf" }, want: "unsupported control character"},
		{name: "unknown engine", edit: func(request *engine.Request) { request.Engine = "unknown" }, want: `engine "unknown" is not supported`},
		{name: "mismatched block", edit: func(request *engine.Request) { request.Engine = engine.MLXVLM }, want: "does not match its tuning variant"},
		{name: "two blocks", edit: func(request *engine.Request) { request.RapidMLX = &engine.RapidMLXTuning{} }, want: "exactly one tuning variant"},
		{name: "unsupported multimodal", edit: func(request *engine.Request) { request.Modalities = []string{"text", "image"} }, want: "text-only contract"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := validLlamaServerRequest()
			test.edit(&request)
			_, err := engine.Build(request)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Build() error = %v, want %q", err, test.want)
			}
		})
	}
}

func validLlamaServerRequest() engine.Request {
	return engine.Request{
		Engine: engine.LlamaServer, LayoutID: "coder", ModelPath: "/temper/artifacts/model.gguf",
		ArtifactFormat: "gguf", KVCache: "q8", Interface: engine.InterfaceChatCompletions,
		Modalities: []string{"text"}, Window: 24576, MaxTokens: 4096, Thinking: "off",
		Speculation: "none",
		LlamaServer: &engine.LlamaServerTuning{
			Parallel: 1, FlashAttention: "on", Batch: 512, UBatch: 512,
		},
	}
}

func assertCommand(t *testing.T, got engine.Command, lines []string, runtime engine.Runtime) {
	t.Helper()
	if !reflect.DeepEqual(got.Lines(), lines) {
		t.Fatalf("Build() lines =\n%q\nwant\n%q", got.Lines(), lines)
	}
	if !reflect.DeepEqual(got.Runtime(), runtime) {
		t.Fatalf("Build() runtime = %#v, want %#v", got.Runtime(), runtime)
	}
}

func hasEnvironment(runtime engine.Runtime, name, value string) bool {
	for _, assignment := range runtime.Environment {
		if assignment.Name == name && assignment.Value == value {
			return true
		}
	}
	return false
}
