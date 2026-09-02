package engine

import "strconv"

func mlxVLMCommand(options mlxVLMOptions) (Command, error) {
	groups := [][]commandWord{
		{knownWord("--host"), knownWord("127.0.0.1"), knownWord("--port"), portWord()},
		{knownWord("--model"), dataWord(options.modelPath)},
		{knownWord("--max-tokens"), knownWord(strconv.Itoa(options.maxTokens))},
		{knownWord("--max-num-seqs"), knownWord(strconv.Itoa(options.maxNumSeqs))},
		{knownWord("--prefill-step-size"), knownWord(strconv.Itoa(options.prefillStepSize))},
		{knownWord("--vision-cache-size"), knownWord(strconv.Itoa(options.visionCacheSize))},
	}
	if options.thinking == "on" {
		groups = append(groups, []commandWord{knownWord("--enable-thinking")})
	}
	if options.kvBits != nil {
		groups = append(groups,
			[]commandWord{knownWord("--kv-bits"), knownWord(strconv.FormatFloat(*options.kvBits, 'g', -1, 64))},
			[]commandWord{knownWord("--kv-quant-scheme"), knownWord(options.kvQuantScheme)},
			[]commandWord{knownWord("--kv-group-size"), knownWord(strconv.Itoa(options.kvGroupSize))},
		)
	}
	if options.maxKVSize != nil {
		groups = append(groups, []commandWord{knownWord("--max-kv-size"), knownWord(strconv.Itoa(*options.maxKVSize))})
	}
	return commandFromLaunch(launchSpec{
		executable:     knownWord("mlx_vlm.server"),
		argumentGroups: groups,
	}, Runtime{
		Requirement:   RuntimeRequirement{Package: MLXVLM, RelativeExecutable: "bin/mlx_vlm.server"},
		Environment:   offlineEnvironment(),
		CheckEndpoint: "/health",
		UseModelName:  options.modelPath,
		ContextWindow: options.contextWindow,
	})
}
