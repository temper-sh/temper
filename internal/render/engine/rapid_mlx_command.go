package engine

import (
	"fmt"
	"strconv"
)

func rapidMLXCommand(options rapidMLXOptions) (Command, error) {
	groups := [][]commandWord{
		{knownWord("--no-telemetry"), knownWord("serve"), dataWord(options.modelPath)},
		{knownWord("--host"), knownWord("127.0.0.1"), knownWord("--port"), portWord()},
		{knownWord("--served-model-name"), dataWord(options.layoutID)},
		{knownWord("--max-tokens"), knownWord(strconv.Itoa(options.maxTokens))},
		{knownWord("--max-num-seqs"), knownWord(strconv.Itoa(options.maxNumSeqs))},
		{knownWord("--max-concurrent-requests"), knownWord(strconv.Itoa(options.maxConcurrentRequests))},
		{knownWord("--prefill-batch-size"), knownWord(strconv.Itoa(options.prefillBatchSize))},
		{knownWord("--completion-batch-size"), knownWord(strconv.Itoa(options.completionBatchSize))},
		{knownWord("--gpu-memory-utilization"), knownWord(strconv.FormatFloat(options.gpuMemoryUtilization, 'g', -1, 64))},
	}
	if options.prefixCache == "on" {
		groups = append(groups, []commandWord{knownWord("--enable-prefix-cache")})
	} else {
		groups = append(groups, []commandWord{knownWord("--disable-prefix-cache")})
	}
	if options.toolCallParser != "" {
		groups = append(groups, []commandWord{knownWord("--enable-auto-tool-choice"), knownWord("--tool-call-parser"), dataWord(options.toolCallParser)})
	}
	if options.prefillStepSize > 0 {
		groups = append(groups, []commandWord{knownWord("--prefill-step-size"), knownWord(strconv.Itoa(options.prefillStepSize))})
	}
	if options.requestTimeoutSeconds > 0 {
		groups = append(groups, []commandWord{knownWord("--timeout"), knownWord(strconv.Itoa(options.requestTimeoutSeconds))})
	}
	if options.cacheMemoryMiB != nil {
		groups = append(groups, []commandWord{knownWord("--cache-memory-mb"), knownWord(strconv.Itoa(*options.cacheMemoryMiB))})
	}
	groups = append(groups,
		[]commandWord{knownWord("--kv-cache-dtype"), knownWord(options.kvCacheDType)},
		[]commandWord{knownWord("--pflash"), knownWord(options.pflash)},
	)
	if options.multimodal {
		groups = append(groups, []commandWord{knownWord("--mllm")})
	} else {
		groups = append(groups, []commandWord{knownWord("--no-mllm")})
	}
	if options.thinking == "off" {
		groups = append(groups, []commandWord{knownWord("--no-thinking")})
	} else {
		groups = append(groups, []commandWord{knownWord("--reasoning-parser"), dataWord(options.reasoningParser)})
	}
	if options.speculation == "none" {
		groups = append(groups, []commandWord{knownWord("--no-spec-decode")})
	} else {
		config := fmt.Sprintf(`{"method":"mtp","num_speculative_tokens":%d}`, options.speculativeTokens)
		groups = append(groups, []commandWord{knownWord("--speculative-config"), dataWord(config)})
	}
	return commandFromLaunch(launchSpec{
		executable:     knownWord("rapid-mlx"),
		argumentGroups: groups,
	}, Runtime{
		Requirement: RuntimeRequirement{Package: RapidMLX, RelativeExecutable: "bin/rapid-mlx"},
		Environment: offlineEnvironment(
			EnvironmentAssignment{Name: "PYTHONDONTWRITEBYTECODE", Value: "1"},
			EnvironmentAssignment{Name: "PYTHONNOUSERSITE", Value: "1"},
			EnvironmentAssignment{Name: "RAPID_MLX_TELEMETRY", Value: "0"},
			EnvironmentAssignment{Name: "RAPID_MLX_PREFIX_CACHE_AUTOLOAD", Value: "0"},
		),
		CheckEndpoint: "/health/ready",
		ContextWindow: options.contextWindow,
	})
}
