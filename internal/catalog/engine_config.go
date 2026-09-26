package catalog

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/render/engine"
	"gopkg.in/yaml.v3"
)

// EngineConfig preserves the issued llama-server representation while giving
// Splash its own closed configuration. Fields from different adapters cannot mix.
type EngineConfig struct {
	LlamaConfig
	Splash *SplashConfig
	Python *PythonEngineConfig
}

type PythonEngineConfig struct {
	Kind      string                    `yaml:"kind" json:"kind"`
	RapidMLX  *manifest.RapidMLXTuning  `yaml:"rapid_mlx,omitempty" json:"rapid_mlx,omitempty"`
	VLLMMetal *manifest.VLLMMetalTuning `yaml:"vllm_metal,omitempty" json:"vllm_metal,omitempty"`
}

func (p PythonEngineConfig) MarshalJSON() ([]byte, error) {
	data, err := yaml.Marshal(p)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err := yaml.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

type SplashConfig struct {
	Kind                string `yaml:"kind" json:"kind"`
	engine.SplashConfig `yaml:",inline"`
}

func (c EngineConfig) value() (any, error) {
	if c.Python != nil {
		base := c.LlamaConfig
		base.Kind = ""
		p := c.Python
		if base != (LlamaConfig{}) || c.Splash != nil || c.Kind != p.Kind ||
			!(p.Kind == "rapid-mlx/v1" && p.RapidMLX != nil && p.VLLMMetal == nil ||
				p.Kind == "vllm-metal/v1" && p.VLLMMetal != nil && p.RapidMLX == nil) {
			return nil, errors.New("Python engine_config requires exactly its own tuning variant")
		}
		return p, nil
	}
	if c.Splash == nil {
		if c.Kind != "llama-server/v2" {
			return nil, errors.New("engine_config requires a supported kind and its matching fields")
		}
		return c.LlamaConfig, nil
	}
	base := c.LlamaConfig
	base.Kind = ""
	if base != (LlamaConfig{}) || c.Kind != "splash/v1" || c.Splash.Kind != c.Kind {
		return nil, errors.New("Splash engine_config cannot contain llama-server fields")
	}
	return c.Splash, nil
}

func (c EngineConfig) MarshalJSON() ([]byte, error) {
	value, err := c.value()
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func (c EngineConfig) MarshalYAML() (any, error) { return c.value() }

func (c *EngineConfig) UnmarshalYAML(node *yaml.Node) error {
	var discriminator struct {
		Kind string `yaml:"kind"`
	}
	if err := node.Decode(&discriminator); err != nil {
		return err
	}
	raw, err := yaml.Marshal(node)
	if err != nil {
		return err
	}
	*c = EngineConfig{}
	switch discriminator.Kind {
	case "llama-server/v2":
		return decode(raw, &c.LlamaConfig)
	case "splash/v1":
		c.Kind = discriminator.Kind
		c.Splash = &SplashConfig{}
		return decode(raw, c.Splash)
	case "rapid-mlx/v1", "vllm-metal/v1":
		c.Kind = discriminator.Kind
		c.Python = &PythonEngineConfig{}
		return decode(raw, c.Python)
	default:
		return fmt.Errorf("unsupported engine_config kind %q", discriminator.Kind)
	}
}

func (c *EngineConfig) UnmarshalJSON(raw []byte) error {
	var node yaml.Node
	if err := yaml.Unmarshal(raw, &node); err != nil {
		return err
	}
	return c.UnmarshalYAML(node.Content[0])
}
