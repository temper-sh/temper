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
	if c := options.controls; c != nil {
		if options.maxTokens > 0 {
			groups = append(groups, []commandWord{knownWord("--predict"), knownWord(strconv.Itoa(options.maxTokens))})
		}
		if c.CheckpointMinStep != nil {
			groups = append(groups, []commandWord{knownWord("--checkpoint-min-step"), knownWord(strconv.Itoa(*c.CheckpointMinStep))})
		}
		groups = append(groups,
			[]commandWord{knownWord("--cache-reuse"), knownWord(strconv.Itoa(c.CacheReuse))},
			[]commandWord{knownWord("--reasoning-effort"), knownWord(c.ReasoningEffort)},
			[]commandWord{knownWord(llamaBooleanFlag(c.PreserveReasoning, "--reasoning-preserve", "--no-reasoning-preserve"))},
			[]commandWord{knownWord(llamaBooleanFlag(c.ContextShift, "--context-shift", "--no-context-shift"))},
			[]commandWord{knownWord(llamaBooleanFlag(c.CachePrompt, "--cache-prompt", "--no-cache-prompt"))},
			[]commandWord{knownWord("--fit"), knownWord(c.Fit)},
			[]commandWord{knownWord("--threads"), knownWord(strconv.Itoa(c.Threads))},
			[]commandWord{knownWord("--threads-batch"), knownWord(strconv.Itoa(c.ThreadsBatch))},
			[]commandWord{knownWord("--load-mode"), knownWord(c.LoadMode)},
		)
	}
	if s := options.sampling; s != nil {
		for _, pair := range []struct {
			name  string
			value float64
		}{
			{"--temp", s.Temperature}, {"--top-p", s.TopP}, {"--min-p", s.MinP},
			{"--repeat-penalty", s.RepeatPenalty}, {"--presence-penalty", s.PresencePenalty},
		} {
			groups = append(groups, []commandWord{knownWord(pair.name), knownWord(strconv.FormatFloat(pair.value, 'g', -1, 64))})
		}
		groups = append(groups,
			[]commandWord{knownWord("--top-k"), knownWord(strconv.Itoa(s.TopK))},
			[]commandWord{knownWord("--seed"), knownWord(strconv.Itoa(s.Seed))},
		)
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

func llamaBooleanFlag(enabled bool, positive, negative string) string {
	if enabled {
		return positive
	}
	return negative
}
