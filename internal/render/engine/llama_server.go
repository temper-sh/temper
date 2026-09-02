package engine

import (
	"errors"
	"fmt"
)

type llamaServerOptions struct {
	modelPath          string
	chatTemplatePath   string
	reranking          bool
	parallel           int
	window             int
	contextCheckpoints *int
	promptCacheRAMMiB  *int
	flashAttention     string
	kv                 string
	batch              int
	ubatch             int
	specType           string
	specDraftNMax      int
	reasoning          string
	ngl                *int
}

func buildLlamaServer(request Request) (Command, error) {
	if err := validateLlamaServerRequest(request); err != nil {
		return Command{}, err
	}

	options := llamaServerOptions{
		modelPath:          request.ModelPath,
		chatTemplatePath:   request.ChatTemplatePath,
		reranking:          request.Interface == InterfaceReranking,
		parallel:           request.LlamaServer.Parallel,
		window:             request.Window,
		contextCheckpoints: request.LlamaServer.ContextCheckpoints,
		promptCacheRAMMiB:  request.LlamaServer.PromptCacheRAMMiB,
		flashAttention:     request.LlamaServer.FlashAttention,
		batch:              request.LlamaServer.Batch,
		ubatch:             request.LlamaServer.UBatch,
		specType:           request.Speculation,
		specDraftNMax:      request.SpeculativeTokens,
		ngl:                request.NGL,
	}
	if options.specType == "mtp" {
		options.specType = "draft-mtp"
	} else if options.specType == "none" {
		options.specType = ""
	}
	if request.Interface == InterfaceChatCompletions {
		options.reasoning = request.Thinking
		options.kv = "f16"
		if request.KVCache == "q8" {
			options.kv = "q8_0"
		}
	}
	return llamaServerCommand(options)
}

func validateLlamaServerRequest(request Request) error {
	if request.ModelPath == "" {
		return errors.New("llama-server model path is required")
	}
	if request.ArtifactFormat != "gguf" {
		return fmt.Errorf("llama-server artifact format %q must be gguf", request.ArtifactFormat)
	}
	if len(request.Modalities) != 1 || request.Modalities[0] != "text" {
		return errors.New("llama-server launch requires the text-only contract")
	}
	if request.Window <= 0 {
		return errors.New("llama-server window must be greater than zero")
	}
	tuning := *request.LlamaServer
	if tuning.Parallel <= 0 || tuning.Batch <= 0 || tuning.UBatch <= 0 {
		return errors.New("llama-server parallel, batch and ubatch must be greater than zero")
	}
	if tuning.UBatch > tuning.Batch {
		return errors.New("llama-server ubatch must not exceed batch")
	}
	if tuning.FlashAttention != "on" && tuning.FlashAttention != "off" && tuning.FlashAttention != "auto" {
		return fmt.Errorf("llama-server flash attention %q must be on, off or auto", tuning.FlashAttention)
	}
	if tuning.ContextCheckpoints != nil && *tuning.ContextCheckpoints < 0 {
		return errors.New("llama-server context checkpoints must be zero or greater")
	}
	if tuning.PromptCacheRAMMiB != nil && *tuning.PromptCacheRAMMiB < 0 {
		return errors.New("llama-server prompt cache RAM must be zero or greater")
	}
	if request.NGL != nil && *request.NGL < 0 {
		return errors.New("llama-server ngl must be zero or greater")
	}

	switch request.Speculation {
	case "none":
		if request.SpeculativeTokens != 0 {
			return errors.New("llama-server speculation maximum requires a type")
		}
	case "mtp":
		if request.Interface != InterfaceChatCompletions {
			return errors.New("llama-server draft-mtp speculation requires chat completions")
		}
		if request.SpeculativeTokens <= 0 || request.SpeculativeTokens > 16 {
			return errors.New("llama-server draft-mtp maximum must be between 1 and 16")
		}
	default:
		return fmt.Errorf("llama-server speculation type %q is unsupported", request.Speculation)
	}

	switch request.Interface {
	case InterfaceChatCompletions:
		if request.MaxTokens <= 0 || request.MaxTokens >= request.Window {
			return errors.New("llama-server coder max tokens must be greater than zero and below window")
		}
		if request.KVCache != "q8" && request.KVCache != "f16" {
			return fmt.Errorf("llama-server KV cache %q must be q8 or f16", request.KVCache)
		}
		if request.Thinking != "on" && request.Thinking != "off" {
			return fmt.Errorf("llama-server coder thinking %q must be on or off", request.Thinking)
		}
	case InterfaceReranking:
		if request.MaxTokens != 0 || request.Thinking != "" || request.ChatTemplatePath != "" {
			return errors.New("llama-server reranker cannot declare chat-completion tuning")
		}
	default:
		return fmt.Errorf("llama-server interface %q is unsupported", request.Interface)
	}
	return nil
}
