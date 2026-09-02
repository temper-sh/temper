package engine

import (
	"errors"
	"fmt"
)

type mlxVLMOptions struct {
	modelPath       string
	contextWindow   int
	maxTokens       int
	thinking        string
	maxNumSeqs      int
	prefillStepSize int
	visionCacheSize int
	kvBits          *float64
	kvQuantScheme   string
	kvGroupSize     int
	maxKVSize       *int
}

func buildMLXVLM(request Request) (Command, error) {
	if err := validateMLXVLMRequest(request); err != nil {
		return Command{}, err
	}
	tuning := *request.MLXVLM
	return mlxVLMCommand(mlxVLMOptions{
		modelPath:       request.ModelPath,
		contextWindow:   request.Window,
		maxTokens:       request.MaxTokens,
		thinking:        request.Thinking,
		maxNumSeqs:      tuning.MaxNumSeqs,
		prefillStepSize: tuning.PrefillStepSize,
		visionCacheSize: tuning.VisionCacheSize,
		kvBits:          tuning.KVBits,
		kvQuantScheme:   tuning.KVQuantScheme,
		kvGroupSize:     tuning.KVGroupSize,
		maxKVSize:       tuning.MaxKVSize,
	})
}

func validateMLXVLMRequest(request Request) error {
	if err := validateChatRequest(request, MLXVLM, "mlx-safetensors", []string{"text"}, []string{"text", "image"}); err != nil {
		return err
	}
	tuning := *request.MLXVLM
	if err := validatePositiveBatching(MLXVLM, tuning.MaxNumSeqs, tuning.PrefillStepSize); err != nil {
		return err
	}
	if tuning.VisionCacheSize < 0 {
		return errors.New("mlx-vlm vision cache size must be zero or greater")
	}
	if tuning.KVBits == nil {
		if tuning.KVQuantScheme != "" || tuning.KVGroupSize != 0 {
			return errors.New("mlx-vlm KV quantization settings require kv_bits")
		}
	} else {
		bits := *tuning.KVBits
		if bits != 3.5 && bits != 4 && bits != 8 {
			return fmt.Errorf("mlx-vlm KV bits %g must be 3.5, 4 or 8", bits)
		}
		if tuning.KVQuantScheme != "uniform" && tuning.KVQuantScheme != "turboquant" {
			return fmt.Errorf("mlx-vlm KV quantization scheme %q must be uniform or turboquant", tuning.KVQuantScheme)
		}
		if tuning.KVGroupSize <= 0 {
			return errors.New("mlx-vlm KV group size must be greater than zero when KV quantization is selected")
		}
		if bits == 3.5 && tuning.KVQuantScheme != "turboquant" {
			return errors.New("mlx-vlm 3.5-bit KV requires turboquant")
		}
	}
	if tuning.MaxKVSize != nil && *tuning.MaxKVSize <= 0 {
		return errors.New("mlx-vlm max KV size must be greater than zero when present")
	}
	if request.Speculation != "none" || request.SpeculativeTokens != 0 {
		return errors.New("mlx-vlm does not support the selected speculation contract")
	}
	return nil
}
