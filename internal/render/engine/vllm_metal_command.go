package engine

import (
	"fmt"
	"strconv"
)

func vllmMetalCommand(options vllmMetalOptions) (Command, error) {
	groups := [][]commandWord{
		{knownWord("serve"), dataWord(options.modelPath)},
		{knownWord("--host"), knownWord("127.0.0.1"), knownWord("--port"), portWord()},
		{knownWord("--served-model-name"), dataWord(options.layoutID)},
		{knownWord("--max-model-len"), knownWord(strconv.Itoa(options.window))},
		{knownWord("--max-num-seqs"), knownWord(strconv.Itoa(options.maxNumSeqs))},
		{knownWord("--max-num-batched-tokens"), knownWord(strconv.Itoa(options.maxNumBatchedTokens))},
		{knownWord("--gpu-memory-utilization"), knownWord(strconv.FormatFloat(options.gpuMemoryUtilization, 'g', -1, 64))},
		{knownWord("--kv-cache-dtype"), knownWord(options.kvCacheDType)},
	}
	if options.prefixCache == "on" {
		groups = append(groups, []commandWord{knownWord("--enable-prefix-caching")})
	} else {
		groups = append(groups, []commandWord{knownWord("--no-enable-prefix-caching")})
	}
	thinking := `{"enable_thinking":false}`
	if options.thinking == "on" {
		thinking = `{"enable_thinking":true}`
	}
	groups = append(groups, []commandWord{knownWord("--default-chat-template-kwargs"), dataWord(thinking)})
	if options.speculation == "mtp" {
		config := fmt.Sprintf(`{"method":"mtp","num_speculative_tokens":%d}`, options.speculativeTokens)
		groups = append(groups, []commandWord{knownWord("--speculative-config"), dataWord(config)})
	}
	return commandFromLaunch(launchSpec{
		executable:     knownWord("vllm"),
		argumentGroups: groups,
	}, Runtime{
		Requirement: RuntimeRequirement{Package: VLLMMetal, RelativeExecutable: "bin/vllm"},
		Environment: offlineEnvironment(
			EnvironmentAssignment{Name: "VLLM_DO_NOT_TRACK", Value: "1"},
			EnvironmentAssignment{Name: "VLLM_METAL_MEMORY_FRACTION", Value: "auto"},
			EnvironmentAssignment{Name: "VLLM_METAL_USE_PAGED_ATTENTION", Value: "1"},
			EnvironmentAssignment{Name: "VLLM_NO_USAGE_STATS", Value: "1"},
		),
		CheckEndpoint: "/health",
	})
}
