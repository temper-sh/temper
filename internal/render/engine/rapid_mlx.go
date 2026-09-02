package engine

import (
	"errors"
	"fmt"
)

type rapidMLXOptions struct {
	layoutID              string
	modelPath             string
	multimodal            bool
	maxTokens             int
	maxNumSeqs            int
	maxConcurrentRequests int
	prefillBatchSize      int
	completionBatchSize   int
	gpuMemoryUtilization  float64
	prefixCache           string
	cacheMemoryMiB        *int
	kvCacheDType          string
	pflash                string
	thinking              string
	reasoningParser       string
	speculation           string
	speculativeTokens     int
}

func buildRapidMLX(request Request) (Command, error) {
	if err := validateRapidMLXRequest(request); err != nil {
		return Command{}, err
	}
	tuning := *request.RapidMLX
	return rapidMLXCommand(rapidMLXOptions{
		layoutID:              request.LayoutID,
		modelPath:             request.ModelPath,
		multimodal:            len(request.Modalities) == 2,
		maxTokens:             request.MaxTokens,
		maxNumSeqs:            tuning.MaxNumSeqs,
		maxConcurrentRequests: tuning.MaxConcurrentRequests,
		prefillBatchSize:      tuning.PrefillBatchSize,
		completionBatchSize:   tuning.CompletionBatchSize,
		gpuMemoryUtilization:  tuning.GPUMemoryUtilization,
		prefixCache:           tuning.PrefixCache,
		cacheMemoryMiB:        tuning.CacheMemoryMiB,
		kvCacheDType:          tuning.KVCacheDType,
		pflash:                tuning.PFlash,
		thinking:              request.Thinking,
		reasoningParser:       tuning.ReasoningParser,
		speculation:           request.Speculation,
		speculativeTokens:     request.SpeculativeTokens,
	})
}

func validateRapidMLXRequest(request Request) error {
	if err := validateChatRequest(request, RapidMLX, "mlx-safetensors", []string{"text"}, []string{"text", "image"}); err != nil {
		return err
	}
	tuning := *request.RapidMLX
	if err := validatePositiveBatching(RapidMLX, tuning.MaxNumSeqs, tuning.MaxConcurrentRequests, tuning.PrefillBatchSize, tuning.CompletionBatchSize); err != nil {
		return err
	}
	if tuning.MaxConcurrentRequests < tuning.MaxNumSeqs {
		return errors.New("rapid-mlx max concurrent requests must not be below max sequences")
	}
	if err := validateFraction(RapidMLX, "GPU memory utilization", tuning.GPUMemoryUtilization); err != nil {
		return err
	}
	if err := validateOnOff(RapidMLX, "prefix cache", tuning.PrefixCache); err != nil {
		return err
	}
	if tuning.CacheMemoryMiB != nil && *tuning.CacheMemoryMiB < 0 {
		return errors.New("rapid-mlx cache memory must be zero or greater")
	}
	if tuning.KVCacheDType != "bf16" && tuning.KVCacheDType != "int8" && tuning.KVCacheDType != "int4" {
		return fmt.Errorf("rapid-mlx KV cache dtype %q must be bf16, int8 or int4", tuning.KVCacheDType)
	}
	if tuning.PFlash != "off" && tuning.PFlash != "auto" && tuning.PFlash != "always" {
		return fmt.Errorf("rapid-mlx pflash %q must be off, auto or always", tuning.PFlash)
	}
	if len(request.Modalities) == 2 && tuning.PFlash != "off" {
		return errors.New("rapid-mlx multimodal launches require pflash off")
	}
	if request.Thinking == "on" {
		if err := requireStableOption(RapidMLX, "reasoning parser", tuning.ReasoningParser); err != nil {
			return err
		}
	} else if tuning.ReasoningParser != "" {
		return errors.New("rapid-mlx reasoning parser requires thinking on")
	}
	return validateSpeculation(RapidMLX, request.Speculation, request.SpeculativeTokens)
}
