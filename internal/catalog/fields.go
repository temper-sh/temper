package catalog

import "strings"

// Field is one setting a user fork can change. Path is its place in a fork's
// changes, which nest like a preset record; the compiler stays the authority
// on which values and combinations an engine accepts.
type Field struct {
	Path    string   `json:"path"`
	Type    string   `json:"type"`
	Choices []string `json:"choices,omitempty"`
	Help    string   `json:"help"`
}

// Field types.
const (
	FieldInteger = "integer"
	FieldNumber  = "number"
	FieldText    = "text"
	FieldSwitch  = "switch"
	FieldChoice  = "choice"
)

// presetFields apply to every engine. engine_config and context_window_tokens
// change the profile Temper selects for the Mac.
var presetFields = []Field{
	{"artifact", FieldText, nil, "Weights ID: the origin's, or weights of the same model (same_model_as)."},
	{"template", FieldText, nil, "Chat template ID, or builtin for the template inside the weights."},
	{"compute", FieldChoice, []string{"cpu", "gpu"}, "Device, one of the origin's compute options."},
	{"context_window_tokens", FieldInteger, nil, "Total context in tokens, input and output, up to the preset's limit."},
	{"request_defaults.reasoning", FieldChoice, []string{"on", "off"}, "Whether the model reasons before it answers."},
	{"request_defaults.max_output_tokens", FieldInteger, nil, "Default output cap in tokens; 0 uses the remaining context."},
	{"request_defaults.sampling.temperature", FieldNumber, nil, "Sampling temperature; 0 is greedy."},
	{"request_defaults.sampling.top_k", FieldInteger, nil, "Sample from the k most likely tokens; 0 turns it off."},
	{"request_defaults.sampling.top_p", FieldNumber, nil, "Sample from the smallest set with this total probability (above 0, at most 1)."},
	{"request_defaults.sampling.min_p", FieldNumber, nil, "Drop tokens below this share of the most likely token's probability (0 to 1)."},
	{"request_defaults.sampling.repeat_penalty", FieldNumber, nil, "Penalty for repeated tokens; 1 is none."},
	{"request_defaults.sampling.presence_penalty", FieldNumber, nil, "Penalty for tokens that already appeared; 0 is none."},
	{"request_defaults.sampling.seed", FieldInteger, nil, "Fixed seed for repeatable answers; null explicitly selects random in a fork."},
	{"speculation.method", FieldChoice, []string{"none", "mtp", "dflash", "dflash2"}, "Speculative decoding method."},
	{"speculation.source", FieldChoice, []string{"none", "embedded", "artifact"}, "Where the draft comes from: none, inside the weights, or draft weights."},
	{"speculation.draft_artifact", FieldText, nil, "Draft weights ID when the source is artifact."},
	{"speculation.max_draft_tokens", FieldInteger, nil, "Most tokens drafted per step."},
}

var llamaFields = []Field{
	{"parallel", FieldInteger, nil, "Requests served at the same time (slots)."},
	{"kv_cache", FieldChoice, []string{"q4", "q8", "f16"}, "Precision of the KV cache, the memory that holds the context."},
	{"flash_attention", FieldChoice, []string{"on", "off", "auto"}, "Flash attention."},
	{"batch_tokens", FieldInteger, nil, "Prompt tokens read per step (logical batch)."},
	{"microbatch_tokens", FieldInteger, nil, "Tokens per physical batch; at most batch_tokens."},
	{"context_checkpoints", FieldInteger, nil, "Saved context states per slot, for prompt reuse."},
	{"prompt_cache_ram_mib", FieldInteger, nil, "Memory for cached prompts in MiB; 0 turns the cache off."},
	{"gpu_layers", FieldInteger, nil, "Model layers on the GPU."},
	{"controls.checkpoint_min_step", FieldInteger, nil, "Fewest tokens between two context checkpoints."},
	{"controls.cache_reuse", FieldInteger, nil, "Smallest cached chunk, in tokens, reused by shifting; 0 turns it off."},
	{"controls.reasoning_effort", FieldChoice, []string{"low", "medium", "high", "xhigh"}, "Reasoning effort, for models that support it."},
	{"controls.preserve_reasoning", FieldSwitch, nil, "Keep earlier reasoning in the conversation."},
	{"controls.context_shift", FieldSwitch, nil, "Drop the oldest context when the window is full."},
	{"controls.cache_prompt", FieldSwitch, nil, "Reuse the cached prompt between requests."},
	{"controls.fit", FieldChoice, []string{"on", "off"}, "Let llama.cpp shrink settings to fit memory."},
	{"controls.threads", FieldInteger, nil, "CPU threads for generation; 0 uses the engine default."},
	{"controls.threads_batch", FieldInteger, nil, "CPU threads for reading the prompt; 0 uses the engine default."},
	{"controls.cpu_moe_layers", FieldInteger, nil, "Keep MoE weights in the first N model layers on CPU; 0 leaves placement unchanged. Independent of GPU layers."},
	{"controls.load_mode", FieldChoice, []string{"auto", "none", "mmap", "mlock", "mmap+mlock", "dio"}, "How the weights are loaded into memory."},
}

var splashFields = []Field{
	{"kv_cache", FieldChoice, []string{"int8", "bf16"}, "Precision of the KV cache."},
	{"max_memory_bytes", FieldInteger, nil, "Engine memory cap in bytes."},
	{"reasoning_effort", FieldChoice, []string{"none", "low", "medium", "xhigh"}, "Default reasoning effort."},
	{"request_timeout_seconds", FieldInteger, nil, "Longest time one request may take."},
}

var rapidMLXFields = []Field{
	{"rapid_mlx.tool_call_parser", FieldText, nil, "Tool call parser ID."},
	{"rapid_mlx.prefill_step_size", FieldInteger, nil, "Prompt tokens per prefill step; 0 uses the engine default."},
	{"rapid_mlx.request_timeout_seconds", FieldInteger, nil, "Longest time one request may take; 0 uses the engine default."},
	{"rapid_mlx.max_num_seqs", FieldInteger, nil, "Sequences decoded at the same time."},
	{"rapid_mlx.max_concurrent_requests", FieldInteger, nil, "Requests accepted at the same time; at least max_num_seqs."},
	{"rapid_mlx.prefill_batch_size", FieldInteger, nil, "Prompts read in one batch."},
	{"rapid_mlx.completion_batch_size", FieldInteger, nil, "Sequences per decoding batch."},
	{"rapid_mlx.gpu_memory_utilization", FieldNumber, nil, "Share of GPU memory the engine may use (above 0, at most 1)."},
	{"rapid_mlx.prefix_cache", FieldChoice, []string{"on", "off"}, "Reuse cached prompt prefixes."},
	{"rapid_mlx.cache_memory_mib", FieldInteger, nil, "Prefix cache memory in MiB."},
	{"rapid_mlx.kv_cache_dtype", FieldChoice, []string{"bf16", "int8", "int4"}, "Precision of the KV cache."},
	{"rapid_mlx.pflash", FieldChoice, []string{"off", "auto", "always"}, "PFlash prefill."},
	{"rapid_mlx.reasoning_parser", FieldText, nil, "Reasoning parser ID; needs reasoning on."},
}

var vllmMetalFields = []Field{
	{"vllm_metal.language_model_only", FieldSwitch, nil, "Load only the language model of a multimodal checkpoint."},
	{"vllm_metal.chunked_prefill", FieldSwitch, nil, "Read long prompts in chunks."},
	{"vllm_metal.block_size", FieldInteger, nil, "KV cache block size: 16, or 0 for the engine default."},
	{"vllm_metal.tool_call_parser", FieldText, nil, "Tool call parser ID."},
	{"vllm_metal.reasoning_parser", FieldText, nil, "Reasoning parser ID."},
	{"vllm_metal.max_num_seqs", FieldInteger, nil, "Sequences decoded at the same time."},
	{"vllm_metal.max_num_batched_tokens", FieldInteger, nil, "Most tokens in one batch."},
	{"vllm_metal.gpu_memory_utilization", FieldNumber, nil, "Share of GPU memory the engine may use (above 0, at most 1)."},
	{"vllm_metal.kv_cache_dtype", FieldChoice, []string{"auto", "bfloat16", "float16"}, "Precision of the KV cache."},
	{"vllm_metal.prefix_cache", FieldChoice, []string{"on", "off"}, "Reuse cached prompt prefixes."},
}

var crispEmbedFields = []Field{
	{"threads", FieldInteger, nil, "CPU threads."},
}

var engineFields = map[string][]Field{
	"kapitsa/v1": {
		{"slots", FieldInteger, nil, "Concurrent requests; each keeps its own context cache."},
		{"max_memory_bytes", FieldInteger, nil, "Engine memory ceiling in bytes; not minimum machine RAM."},
		{"prefill_chunk_tokens", FieldInteger, nil, "Prompt tokens per prefill pass."},
		{"ane", FieldSwitch, nil, "Use Apple Neural Engine prefill."},
	},
	"llama-server/v1": llamaFields,
	"splash/v1":       splashFields,
	"splash/v2":       splashFields,
	"rapid-mlx/v1":    rapidMLXFields,
	"vllm-metal/v1":   vllmMetalFields,
	"crispembed/v1":   crispEmbedFields,
}

// Fields lists the settings a fork of a preset with this engine_config kind
// can change: the preset settings, then the engine's under engine_config.
func Fields(kind string) []Field {
	result := append([]Field(nil), presetFields...)
	if kind == "llama-server/v1" {
		result = append(result, Field{"request_defaults.reasoning_budget_tokens", FieldInteger, nil, "Default native thinking budget: -1 unrestricted, 0 immediate end, positive tokens; callers can override."})
	}
	for _, f := range engineFields[kind] {
		f.Path = "engine_config." + f.Path
		result = append(result, f)
	}
	return result
}

// field finds the row for one dotted path, and whether path is a group that
// holds rows (such as request_defaults.sampling).
func field(fields []Field, path string) (Field, bool, bool) {
	group := false
	for _, f := range fields {
		if f.Path == path {
			return f, true, false
		}
		group = group || strings.HasPrefix(f.Path, path+".")
	}
	return Field{}, false, group
}
