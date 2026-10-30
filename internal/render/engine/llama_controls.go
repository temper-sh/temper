package engine

import (
	"errors"
	"math"
)

// Validate checks the explicit controls at both the manifest boundary and
// command construction, before a consumer can perform runtime effects.
func (c LlamaServerControls) Validate() error {
	if c.CPUMoELayers < 0 || c.CPUMoELayers > math.MaxInt32 {
		return errors.New("llama-server cpu_moe_layers requires a nonnegative 32-bit layer count")
	}
	if c.CheckpointMinStep != nil && *c.CheckpointMinStep < 0 {
		return errors.New("llama-server checkpoint spacing must be nonnegative")
	}
	if c.CacheReuse < 0 || c.Threads < 0 || c.ThreadsBatch < 0 {
		return errors.New("llama-server controls require nonnegative cache reuse and thread counts (zero uses engine defaults)")
	}
	if c.ReasoningEffort != "" && c.ReasoningEffort != "low" && c.ReasoningEffort != "medium" && c.ReasoningEffort != "high" && c.ReasoningEffort != "xhigh" {
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

// Validate checks only supplied sampling overrides.
func (s SamplingDefaults) Validate() error {
	for _, v := range []*float64{s.Temperature, s.TopP, s.MinP, s.RepeatPenalty, s.PresencePenalty} {
		if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0)) {
			return errors.New("sampling values must be finite")
		}
	}
	if s.Temperature != nil && *s.Temperature < 0 || s.TopK != nil && *s.TopK < 0 ||
		s.TopP != nil && (*s.TopP <= 0 || *s.TopP > 1) || s.MinP != nil && (*s.MinP < 0 || *s.MinP > 1) ||
		s.RepeatPenalty != nil && *s.RepeatPenalty <= 0 || s.Seed != nil && *s.Seed < 0 {
		return errors.New("sampling defaults are out of range")
	}
	return nil
}
