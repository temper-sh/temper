package engine

import (
	"errors"
	"math"
)

// Validate checks the explicit controls at both the manifest boundary and
// command construction, before a consumer can perform runtime effects.
func (c LlamaServerControls) Validate() error {
	if c.CheckpointMinStep != nil && *c.CheckpointMinStep < 0 {
		return errors.New("llama-server checkpoint spacing must be nonnegative")
	}
	if c.CacheReuse < 0 || c.Threads <= 0 || c.ThreadsBatch <= 0 {
		return errors.New("llama-server controls require nonnegative cache reuse and positive thread counts")
	}
	if c.ReasoningEffort != "low" && c.ReasoningEffort != "medium" && c.ReasoningEffort != "high" && c.ReasoningEffort != "xhigh" {
		return errors.New("llama-server reasoning effort must be low, medium, high or xhigh")
	}
	if c.Fit != "on" && c.Fit != "off" {
		return errors.New("llama-server fit must be on or off")
	}
	switch c.LoadMode {
	case "auto", "none", "mmap", "mlock", "mmap+mlock", "dio":
	default:
		return errors.New("llama-server load mode is unsupported")
	}
	return nil
}

// Validate refuses values that cannot be represented by the llama.cpp API.
func (s SamplingDefaults) Validate() error {
	for _, value := range []float64{s.Temperature, s.TopP, s.MinP, s.RepeatPenalty, s.PresencePenalty} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return errors.New("llama-server sampling values must be finite")
		}
	}
	if s.Temperature < 0 || s.TopK < 0 || s.TopP <= 0 || s.TopP > 1 || s.MinP < 0 || s.MinP > 1 || s.RepeatPenalty <= 0 || s.Seed < 0 {
		return errors.New("llama-server sampling defaults are out of range")
	}
	return nil
}
