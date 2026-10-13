package catalog

import (
	"errors"
	"fmt"
	"slices"
)

// ComputeDevice is the chosen execution device, independent of layout membership.
type ComputeDevice string

const (
	ComputeCPU ComputeDevice = "cpu"
	ComputeGPU ComputeDevice = "gpu"
)

func (p Preset) validateCompute() error {
	kind := p.Config().Kind
	if len(p.ComputeOptions) == 0 || !slices.Contains(p.ComputeOptions, p.Compute) {
		return errors.New("compute must select one of the declared compute_options")
	}
	seen := map[ComputeDevice]bool{}
	for _, device := range p.ComputeOptions {
		if device != ComputeCPU && device != ComputeGPU || seen[device] {
			return errors.New("compute_options must contain distinct cpu or gpu choices")
		}
		seen[device] = true
		if device == ComputeCPU && kind != "llama-server/v1" && kind != "crispembed/v1" {
			return fmt.Errorf("%s supports GPU compute only", kind)
		}
		if device == ComputeGPU && kind == "crispembed/v1" {
			return errors.New("the CrispEmbed adapter supports CPU compute only")
		}
	}
	for _, profile := range p.Profiles {
		layers := profile.EngineConfig.GPULayers
		if kind == "llama-server/v1" && (layers < 0 || seen[ComputeGPU] && layers == 0) {
			return errors.New("gpu_layers must be nonnegative and positive when GPU compute is offered")
		}
	}
	return nil
}

// gpuOffloadLayers projects the selection without erasing authored GPU tuning.
func (p Preset) gpuOffloadLayers(c EngineConfig) int {
	if p.Compute == ComputeCPU {
		return 0
	}
	return c.GPULayers
}
