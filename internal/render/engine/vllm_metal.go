package engine

import (
	"errors"
	"fmt"
)

type vllmMetalOptions struct {
	layoutID             string
	modelPath            string
	window               int
	thinking             string
	maxNumSeqs           int
	maxNumBatchedTokens  int
	gpuMemoryUtilization float64
	kvCacheDType         string
	prefixCache          string
	speculation          string
	speculativeTokens    int
}

func buildVLLMMetal(request Request) (Command, error) {
	if err := validateVLLMMetalRequest(request); err != nil {
		return Command{}, err
	}
	tuning := *request.VLLMMetal
	return vllmMetalCommand(vllmMetalOptions{
		layoutID:             request.LayoutID,
		modelPath:            request.ModelPath,
		window:               request.Window,
		thinking:             request.Thinking,
		maxNumSeqs:           tuning.MaxNumSeqs,
		maxNumBatchedTokens:  tuning.MaxNumBatchedTokens,
		gpuMemoryUtilization: tuning.GPUMemoryUtilization,
		kvCacheDType:         tuning.KVCacheDType,
		prefixCache:          tuning.PrefixCache,
		speculation:          request.Speculation,
		speculativeTokens:    request.SpeculativeTokens,
	})
}

func validateVLLMMetalRequest(request Request) error {
	if err := validateChatRequest(request, VLLMMetal, "safetensors", []string{"text"}); err != nil {
		return err
	}
	tuning := *request.VLLMMetal
	if err := validatePositiveBatching(VLLMMetal, tuning.MaxNumSeqs, tuning.MaxNumBatchedTokens); err != nil {
		return err
	}
	if err := validateFraction(VLLMMetal, "GPU memory utilization", tuning.GPUMemoryUtilization); err != nil {
		return err
	}
	if tuning.KVCacheDType != "auto" && tuning.KVCacheDType != "bfloat16" && tuning.KVCacheDType != "float16" {
		return fmt.Errorf("vllm-metal KV cache dtype %q must be auto, bfloat16 or float16", tuning.KVCacheDType)
	}
	if err := validateOnOff(VLLMMetal, "prefix cache", tuning.PrefixCache); err != nil {
		return err
	}
	if request.Speculation == "mtp" && tuning.MaxNumSeqs != 1 {
		return errors.New("vllm-metal MTP experiments require max_num_seqs 1")
	}
	if request.Speculation == "mtp" && tuning.PrefixCache == "on" {
		return errors.New("vllm-metal MTP with prefix caching is refused until upstream output-corruption reports are cleared by qualification")
	}
	return validateSpeculation(VLLMMetal, request.Speculation, request.SpeculativeTokens)
}
