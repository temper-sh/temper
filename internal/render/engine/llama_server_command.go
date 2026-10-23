package engine

import "strconv"

func llamaServerCommand(options llamaServerOptions) (Command, error) {
	groups := [][]commandWord{
		{knownWord("--host"), knownWord("127.0.0.1"), knownWord("--port"), portWord()},
		{knownWord("-m"), dataWord(options.modelPath)},
		{knownWord("--offline")},
	}
	if options.projectorPath == "" {
		groups = append(groups, []commandWord{knownWord("--no-mmproj")})
	} else {
		groups = append(groups, []commandWord{knownWord("--mmproj"), dataWord(options.projectorPath)})
		if options.ngl != nil && *options.ngl == 0 {
			groups = append(groups, []commandWord{knownWord("--no-mmproj-offload")})
		}
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
	if options.draftModelPath != "" {
		groups = append(groups,
			[]commandWord{knownWord("--model-draft"), dataWord(options.draftModelPath)},
			[]commandWord{knownWord("--gpu-layers-draft"), knownWord(strconv.Itoa(*options.ngl))},
			[]commandWord{knownWord("--cache-type-k-draft"), knownWord("f16")},
			[]commandWord{knownWord("--cache-type-v-draft"), knownWord("f16")},
		)
	}
	if options.chatTemplatePath != "" {
		groups = append(groups, []commandWord{knownWord("--chat-template-file"), dataWord(options.chatTemplatePath)})
	}
	if options.reasoning != "" {
		groups = append(groups, []commandWord{knownWord("--reasoning"), knownWord(options.reasoning)})
	}
	var defaults map[string]any
	if budget := options.reasoningBudgetTokens; budget != nil {
		// Native request -1 falls back to the CLI budget. Keep that fallback
		// unrestricted so callers can opt out of a finite preset default.
		groups = append(groups, []commandWord{knownWord("--reasoning-budget"), knownWord("-1")})
		// The native alias is lower priority than reasoning_budget_tokens.
		// A router set-if-absent default on the alias respects either spelling.
		defaults = map[string]any{"thinking_budget_tokens": *budget}
	}
	if options.ngl != nil {
		groups = append(groups, []commandWord{knownWord("-ngl"), knownWord(strconv.Itoa(*options.ngl))})
		if *options.ngl == 0 {
			groups = append(groups,
				[]commandWord{knownWord("--device"), knownWord("none")},
				[]commandWord{knownWord("--no-kv-offload")},
				[]commandWord{knownWord("--no-op-offload")},
			)
			if options.draftModelPath != "" {
				groups = append(groups, []commandWord{knownWord("--device-draft"), knownWord("none")})
			}
		}
	}
	if !options.reranking {
		groups = append(groups, []commandWord{knownWord("--predict"), knownWord(strconv.Itoa(options.maxTokens))})
		if options.maxTokens == -1 && options.controls == nil {
			groups = append(groups, []commandWord{knownWord("--no-context-shift")})
		}
	}
	if c := options.controls; c != nil {
		if c.CheckpointMinStep != nil {
			groups = append(groups, []commandWord{knownWord("--checkpoint-min-step"), knownWord(strconv.Itoa(*c.CheckpointMinStep))})
		}
		groups = append(groups,
			[]commandWord{knownWord("--cache-reuse"), knownWord(strconv.Itoa(c.CacheReuse))},
			[]commandWord{knownWord("--reasoning-effort"), knownWord(c.ReasoningEffort)},
			[]commandWord{knownWord(llamaBooleanFlag(c.PreserveReasoning, "--reasoning-preserve", "--no-reasoning-preserve"))},
			// Without an output cap, stop at the context boundary instead of discarding history.
			[]commandWord{knownWord(llamaBooleanFlag(c.ContextShift && options.maxTokens != -1, "--context-shift", "--no-context-shift"))},
			[]commandWord{knownWord(llamaBooleanFlag(c.CachePrompt, "--cache-prompt", "--no-cache-prompt"))},
			[]commandWord{knownWord("--fit"), knownWord(c.Fit)},
		)
		if c.Threads > 0 {
			groups = append(groups, []commandWord{knownWord("--threads"), knownWord(strconv.Itoa(c.Threads))})
		}
		if c.ThreadsBatch > 0 {
			groups = append(groups, []commandWord{knownWord("--threads-batch"), knownWord(strconv.Itoa(c.ThreadsBatch))})
		}
		groups = append(groups, []commandWord{knownWord("--load-mode"), knownWord(c.LoadMode)})
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
		groups = append(groups, []commandWord{knownWord("--top-k"), knownWord(strconv.Itoa(s.TopK))})
		if s.Seed != nil {
			groups = append(groups, []commandWord{knownWord("--seed"), knownWord(strconv.Itoa(*s.Seed))})
		}
	}
	return commandFromLaunch(launchSpec{
		executable:     knownWord("llama-server"),
		argumentGroups: groups,
	}, Runtime{
		Requirement:       RuntimeRequirement{Package: "llama-cpp", RelativeExecutable: "llama-server"},
		CheckEndpoint:     "/health",
		ContextWindow:     options.window,
		DefaultParameters: defaults,
	})
}

func llamaBooleanFlag(enabled bool, positive, negative string) string {
	if enabled {
		return positive
	}
	return negative
}
