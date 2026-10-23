package engine

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
)

// KapitsaConfig selects the native MLX/DFlash2 route. The adapter always disables
// disk caching, automatic updates and model discovery; paths are explicit.
type KapitsaConfig struct {
	Slots              int   `yaml:"slots" json:"slots"`
	MaxMemoryBytes     int64 `yaml:"max_memory_bytes" json:"max_memory_bytes"`
	PrefillChunkTokens int   `yaml:"prefill_chunk_tokens" json:"prefill_chunk_tokens"`
	ANE                bool  `yaml:"ane" json:"ane"`
}

type KapitsaTuning struct {
	KapitsaConfig
	StatePath string
}

func (c KapitsaConfig) Validate() error {
	if c.Slots < 1 || c.Slots > math.MaxInt32 || c.PrefillChunkTokens < 1 || c.PrefillChunkTokens > math.MaxInt32 || c.MaxMemoryBytes < 1 || c.MaxMemoryBytes > 1<<53 {
		return errors.New("Kapitsa requires positive native slot/chunk counts and a memory allowance representable in bytes")
	}
	return nil
}

// KapitsaCompatibility is the pinned release's upstream platform floor, not a
// claim about memory fit or which hardware has been measured.
func KapitsaCompatibility(chip, macOS string) error {
	match := appleMChip.FindStringSubmatch(chip)
	major, _ := strconv.Atoi(strings.Split(macOS, ".")[0])
	if len(match) != 2 || match[1] == "0" || major < 14 {
		return errors.New("Kapitsa requires Apple M1 or newer and macOS 14 or later")
	}
	return nil
}

func buildKapitsa(r Request) (Command, error) {
	if err := validateChatRequest(r, Kapitsa, "mlx-safetensors", []string{"text"}); err != nil {
		return Command{}, err
	}
	if err := r.Kapitsa.KapitsaConfig.Validate(); err != nil {
		return Command{}, err
	}
	if r.Window > math.MaxInt32 || !filepath.IsAbs(r.ModelPath) || !filepath.IsAbs(r.DraftModelPath) || !filepath.IsAbs(r.Kapitsa.StatePath) || r.Speculation != "dflash2" || r.SpeculativeTokens != 0 || r.KVCache != "" {
		return Command{}, errors.New("Kapitsa requires absolute target/draft/home paths, native context, engine-owned DFlash2 and FP16 KV")
	}
	if r.Sampling == nil {
		return Command{}, errors.New("Kapitsa requires explicit request defaults")
	}
	s := r.Sampling
	if err := s.Validate(); err != nil {
		return Command{}, err
	}
	if s.MinP != 0 || s.RepeatPenalty != 1 || s.PresencePenalty != 0 || s.Seed != nil && uint64(*s.Seed) > math.MaxUint32 {
		return Command{}, errors.New("Kapitsa supports neutral penalties/min_p and a 32-bit nonnegative seed")
	}
	c := r.Kapitsa
	// Decimal GB, rendered from integer bytes without a binary floating-point
	// conversion: 24 GiB is exactly 25.769803776 GB.
	gb := fmt.Sprintf("%d.%09d", c.MaxMemoryBytes/1_000_000_000, c.MaxMemoryBytes%1_000_000_000)
	defaults := map[string]any{"temperature": s.Temperature, "top_p": s.TopP, "top_k": s.TopK}
	if s.Seed != nil {
		defaults["seed"] = *s.Seed
	}
	// Kapitsa otherwise imposes a 16,384-token default. A zero preset cap
	// means remaining context; the engine stops at its context boundary.
	maxTokens := r.MaxTokens
	if maxTokens == 0 {
		maxTokens = r.Window
	}
	defaults["max_tokens"] = maxTokens
	if r.Thinking == "off" {
		defaults["reasoning_effort"] = "none"
	} else {
		defaults["reasoning_effort"] = "high"
	}
	ane := "0"
	if c.ANE {
		ane = "1"
	}
	return commandFromLaunch(launchSpec{executable: knownWord("kapitsa"), argumentGroups: [][]commandWord{
		{knownWord("serve"), knownWord("--host"), knownWord("127.0.0.1"), knownWord("--port"), portWord()},
		{knownWord("--model"), dataWord(r.ModelPath), knownWord("--model-id"), dataWord(r.LayoutID)},
		{knownWord("--dflash"), dataWord(r.DraftModelPath)},
		{knownWord("--slots"), knownWord(strconv.Itoa(c.Slots)), knownWord("--context"), knownWord(strconv.Itoa(r.Window))},
		{knownWord("--mem-budget"), knownWord(gb), knownWord("--disk-cache"), knownWord("0")},
		{knownWord("--prefill-chunk"), knownWord(strconv.Itoa(c.PrefillChunkTokens))},
	}}, Runtime{Requirement: RuntimeRequirement{Package: Kapitsa, RelativeExecutable: "kapitsa"}, CheckEndpoint: "/health", UseModelName: r.LayoutID, ContextWindow: r.Window, DefaultParameters: defaults,
		Environment: offlineEnvironment(EnvironmentAssignment{Name: "KAPITSA_HOME", Value: c.StatePath}, EnvironmentAssignment{Name: "KAPITSA_NO_AUTO_UPDATE", Value: "1"}, EnvironmentAssignment{Name: "KAPITSA_NO_PROMPT", Value: "1"}, EnvironmentAssignment{Name: "NI_ANE", Value: ane}),
	})
}
