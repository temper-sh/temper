package catalog

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/render/engine"
	"gopkg.in/yaml.v3"
)

// EngineConfig is a closed union of supported adapter configurations.
// Fields from different adapters cannot mix.
type EngineConfig struct {
	LlamaConfig
	Splash     *SplashConfig
	Python     *PythonEngineConfig
	CrispEmbed *CrispEmbedConfig
	Kapitsa    *KapitsaConfig
}

type KapitsaConfig struct {
	Kind                 string `yaml:"kind" json:"kind"`
	engine.KapitsaConfig `yaml:",inline"`
}

type CrispEmbedConfig struct {
	Kind                    string `yaml:"kind" json:"kind"`
	engine.CrispEmbedTuning `yaml:",inline"`
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
	if c.Kapitsa != nil {
		base := c.LlamaConfig
		base.Kind = ""
		if base != (LlamaConfig{}) || c.Splash != nil || c.Python != nil || c.CrispEmbed != nil || c.Kind != "kapitsa/v1" || c.Kapitsa.Kind != c.Kind {
			return nil, errors.New("Kapitsa engine_config requires only its own tuning")
		}
		return c.Kapitsa, nil
	}
	if c.CrispEmbed != nil {
		base := c.LlamaConfig
		base.Kind = ""
		if base != (LlamaConfig{}) || c.Splash != nil || c.Python != nil || c.Kind != "crispembed/v1" || c.CrispEmbed.Kind != c.Kind {
			return nil, errors.New("CrispEmbed engine_config requires only its own tuning")
		}
		return c.CrispEmbed, nil
	}
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
		if c.Kind != "llama-server/v1" {
			return nil, errors.New("engine_config requires a supported kind and its matching fields")
		}
		return c.LlamaConfig, nil
	}
	base := c.LlamaConfig
	base.Kind = ""
	if base != (LlamaConfig{}) || (c.Kind != "splash/v1" && c.Kind != "splash/v2") || c.Splash.Kind != c.Kind {
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
	case "kapitsa/v1":
		c.Kind = discriminator.Kind
		c.Kapitsa = &KapitsaConfig{}
		return decode(raw, c.Kapitsa)
	case "crispembed/v1":
		c.Kind = discriminator.Kind
		c.CrispEmbed = &CrispEmbedConfig{}
		return decode(raw, c.CrispEmbed)
	case "llama-server/v1":
		return decode(raw, &c.LlamaConfig)
	case "splash/v1", "splash/v2":
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
