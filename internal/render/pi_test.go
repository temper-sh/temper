package render_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/render"
	"github.com/temper-sh/temper/internal/software"
)

func TestPiCanOverrideThinkingDefaultsThroughEachSupportedEngine(t *testing.T) {
	for _, tc := range []struct {
		preset, thinking, effort, format, start string
	}{
		{"qwen3.8-27b-q4xl-mtp", "on", "medium", "chat-template", "medium"},
		{"qwen3.8-27b-q4xl-mtp", "off", "medium", "chat-template", "off"},
		{"qwen3.8-27b-q4xl-mtp", "on", "low", "chat-template", "low"},
		{"qwen3.8-27b-q4xl-mtp", "on", "xhigh", "chat-template", "xhigh"},
		{"qwen3.8-27b-q4xl-splash", "on", "medium", "openai", "medium"},
		{"qwen3.8-27b-q4xl-splash", "off", "none", "openai", "off"},
	} {
		t.Run(tc.preset+"/"+tc.start, func(t *testing.T) {
			d := piCatalog(t)
			p := d.Presets[tc.preset]
			p.RequestDefaults.Reasoning = tc.thinking
			p.EngineConfig.Controls.ReasoningEffort = tc.effort
			if p.EngineConfig.Splash != nil {
				p.EngineConfig.Controls.ReasoningEffort = ""
				p.EngineConfig.Splash.ReasoningEffort = tc.effort
			}
			d.Presets[tc.preset] = p
			bundle := renderPresetWithPi(t, d, tc.preset, nil)
			var models struct {
				Providers map[string]struct {
					Compat map[string]any
					Models []struct {
						Reasoning        bool
						ThinkingLevelMap map[string]any
						Compat           struct {
							SupportsReasoningEffort bool
							ThinkingFormat          string
							ChatTemplateKwargs      map[string]map[string]string
						}
					}
				}
			}
			if err := json.Unmarshal(artifact(t, bundle, "pi/models.json"), &models); err != nil {
				t.Fatal(err)
			}
			provider := models.Providers["local"]
			if _, found := provider.Compat["supportsReasoningEffort"]; found {
				t.Fatal("provider imposes a blanket effort policy")
			}
			if len(provider.Models) != 1 {
				t.Fatalf("models = %d, want one selected preset", len(provider.Models))
			}
			model := provider.Models[0]
			if !model.Reasoning || !model.Compat.SupportsReasoningEffort || model.Compat.ThinkingFormat != tc.format {
				t.Fatalf("Pi cannot select thinking independently of the server default: %+v", model)
			}
			if model.ThinkingLevelMap["off"] != "none" {
				t.Fatal("off would omit an override instead of disabling server-default thinking")
			}
			if tc.start == "xhigh" && model.ThinkingLevelMap["xhigh"] != "xhigh" {
				t.Fatal("Pi would clamp the preset's explicit xhigh exception")
			}
			if tc.format == "chat-template" {
				for key, variable := range map[string]string{"enable_thinking": "thinking.enabled", "reasoning_effort": "thinking.effort"} {
					if model.Compat.ChatTemplateKwargs[key]["$var"] != variable {
						t.Fatalf("%s does not follow Pi's selected thinking level", key)
					}
				}
			} else if value, found := model.ThinkingLevelMap["high"]; !found || value != nil || model.ThinkingLevelMap["xhigh"] != "xhigh" {
				t.Fatal("Splash must expose its supported xhigh level")
			}
			var settings struct {
				DefaultThinkingLevel string
				ModelThinkingLevels  map[string]string
			}
			if err := json.Unmarshal(artifact(t, bundle, "pi/settings.json"), &settings); err != nil {
				t.Fatal(err)
			}
			if settings.DefaultThinkingLevel != "medium" || settings.ModelThinkingLevels["local/"+tc.preset] != tc.start {
				t.Fatalf("Pi's defaults differ from the preset: %+v", settings)
			}
		})
	}
}

func TestPiPreservesExplicitThinkingPreferences(t *testing.T) {
	const preset = "qwen3.8-27b-q4xl-mtp"
	for _, base := range []string{
		`{"defaultThinkingLevel":"low","modelThinkingLevels":{"remote/model":"high"}}`,
		`{"modelThinkingLevels":{"local/qwen3.8-27b-q4xl-mtp":"off","remote/model":"high"}}`,
	} {
		bundle := renderPresetWithPi(t, piCatalog(t), preset, []byte(base))
		var settings struct {
			DefaultThinkingLevel string
			ModelThinkingLevels  map[string]string
		}
		if err := json.Unmarshal(artifact(t, bundle, "pi/settings.json"), &settings); err != nil {
			t.Fatal(err)
		}
		if settings.ModelThinkingLevels["remote/model"] != "high" {
			t.Fatal("render changed another provider's thinking preference")
		}
		if settings.DefaultThinkingLevel == "low" {
			if _, exists := settings.ModelThinkingLevels["local/"+preset]; exists {
				t.Fatal("preset default masked the owner's global thinking preference")
			}
		} else if settings.DefaultThinkingLevel != "medium" || settings.ModelThinkingLevels["local/"+preset] != "off" {
			t.Fatalf("render changed the owner's model preference: %+v", settings)
		}
	}
}

func piCatalog(t *testing.T) catalog.Document {
	t.Helper()
	raw, err := os.ReadFile("../../catalog/guided-setup.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func renderPresetWithPi(t *testing.T, d catalog.Document, id string, base []byte) render.Bundle {
	t.Helper()
	lock, err := catalog.CompilePreset(d, id, "", 0, software.Target{OS: "darwin", Arch: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := lock.Projections()
	if err != nil {
		t.Fatal(err)
	}
	mode := p.Manifest.Modes[id]
	mode.Harnesses = []string{"pi"}
	mode.ExternalForeground, mode.Foreground = false, id
	mode.Members.Resident, mode.Members.OnDemand = mode.Members.OnDemand, nil
	p.Manifest.Modes[id] = mode
	bundle, err := render.Build(render.Inputs{Manifest: p.Manifest, Lock: p.Artifacts, Mode: id, Root: "/temper", PiSettingsBase: base})
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}
