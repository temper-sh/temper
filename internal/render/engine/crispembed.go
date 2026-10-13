package engine

import (
	"errors"
	"slices"
	"strconv"
)

// CrispEmbedTuning owns the CPU reranking route. The model supplies its fixed
// sequence limit; this server has no context-window or decoding controls.
type CrispEmbedTuning struct {
	Threads int `yaml:"threads" json:"threads"`
}

func (t CrispEmbedTuning) Validate() error {
	if t.Threads <= 0 {
		return errors.New("CrispEmbed threads must be positive")
	}
	return nil
}

func buildCrispEmbed(request Request) (Command, error) {
	if request.ModelPath == "" || request.ArtifactFormat != "gguf" || request.Interface != InterfaceReranking || !slices.Equal(request.Modalities, []string{"text"}) {
		return Command{}, errors.New("CrispEmbed requires a GGUF text reranker")
	}
	if request.Window <= 0 || request.MaxTokens != 0 || request.Thinking != "" || request.ChatTemplatePath != "" || request.Sampling != nil || request.KVCache != "" || request.NGL != nil || request.Speculation != "none" || request.SpeculativeTokens != 0 {
		return Command{}, errors.New("CrispEmbed requires its model sequence limit and no generation or offload controls")
	}
	if err := request.CrispEmbed.Validate(); err != nil {
		return Command{}, err
	}
	return commandFromLaunch(launchSpec{
		executable: knownWord("crispembed-server"),
		argumentGroups: [][]commandWord{
			{knownWord("--host"), knownWord("127.0.0.1"), knownWord("--port"), portWord()},
			{knownWord("-m"), dataWord(request.ModelPath)},
			{knownWord("-t"), knownWord(strconv.Itoa(request.CrispEmbed.Threads))},
			{knownWord("--gpu-backend"), knownWord("cpu")},
		},
	}, Runtime{
		Requirement:   RuntimeRequirement{Package: "crispembed", RelativeExecutable: "crispembed-server"},
		Environment:   offlineEnvironment(EnvironmentAssignment{Name: "CRISPEMBED_OFFLINE", Value: "1"}),
		CheckEndpoint: "/health/ready",
		ContextWindow: request.Window,
	})
}
