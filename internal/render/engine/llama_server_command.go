package engine

import "strconv"

func llamaServerCommand(options llamaServerOptions) (Command, error) {
	groups := [][]commandWord{
		{knownWord("--host"), knownWord("127.0.0.1"), knownWord("--port"), portWord()},
		{knownWord("-m"), dataWord(options.modelPath)},
		{knownWord("--offline")},
		{knownWord("--no-mmproj")},
	}
	if options.reranking {
		groups = append(groups, []commandWord{knownWord("--reranking")})
	} else {
		groups = append(groups, []commandWord{knownWord("--jinja")})
	}
	groups = append(groups,
		[]commandWord{knownWord("--parallel"), knownWord(strconv.Itoa(options.parallel))},
		[]commandWord{knownWord("-c"), knownWord(strconv.Itoa(options.window))},
	)
	if options.contextCheckpoints != nil {
		groups = append(groups, []commandWord{knownWord("--ctx-checkpoints"), knownWord(strconv.Itoa(*options.contextCheckpoints))})
	}
	if options.promptCacheRAMMiB != nil {
		groups = append(groups, []commandWord{knownWord("--cache-ram"), knownWord(strconv.Itoa(*options.promptCacheRAMMiB))})
	}
	groups = append(groups, []commandWord{knownWord("-fa"), knownWord(options.flashAttention)})
	if options.kv != "" {
		groups = append(groups, []commandWord{
			knownWord("-ctk"), knownWord(options.kv),
			knownWord("-ctv"), knownWord(options.kv),
		})
	}
	groups = append(groups,
		[]commandWord{knownWord("-b"), knownWord(strconv.Itoa(options.batch))},
		[]commandWord{knownWord("-ub"), knownWord(strconv.Itoa(options.ubatch))},
	)
	if options.specType != "" {
		groups = append(groups,
			[]commandWord{knownWord("--spec-type"), knownWord(options.specType)},
			[]commandWord{knownWord("--spec-draft-n-max"), knownWord(strconv.Itoa(options.specDraftNMax))},
		)
	}
	if options.chatTemplatePath != "" {
		groups = append(groups, []commandWord{knownWord("--chat-template-file"), dataWord(options.chatTemplatePath)})
	}
	if options.reasoning != "" {
		groups = append(groups, []commandWord{knownWord("--reasoning"), knownWord(options.reasoning)})
	}
	if options.ngl != nil {
		groups = append(groups, []commandWord{knownWord("-ngl"), knownWord(strconv.Itoa(*options.ngl))})
	}
	return commandFromLaunch(launchSpec{
		executable:     knownWord("llama-server"),
		argumentGroups: groups,
	}, Runtime{
		Requirement:   RuntimeRequirement{Package: "llama-cpp", RelativeExecutable: "llama-server"},
		CheckEndpoint: "/health",
		ContextWindow: options.window,
	})
}
