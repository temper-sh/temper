package catalog_test

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/software"
)

func TestEveryPresetHasManualCopyWithoutChangingExecution(t *testing.T) {
	raw, err := os.ReadFile("../testfixture/testdata/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for id, p := range d.Presets {
		count++
		if strings.TrimSpace(p.Presentation.Description) == "" {
			t.Fatalf("missing copy for %s", id)
		}
		before, err := catalog.CompilePreset(d, id, 0, 0, software.Target{OS: "darwin", Arch: "arm64"})
		if err != nil {
			t.Fatal(err)
		}
		edited, err := catalog.DescribePreset(d, id, "Owner's revised assessment.", nil, false)
		if err != nil {
			t.Fatal(err)
		}
		after, err := catalog.CompilePreset(edited, id, 0, 0, software.Target{OS: "darwin", Arch: "arm64"})
		if err != nil {
			t.Fatal(err)
		}
		if !catalog.SameExecution(before, after) {
			t.Fatal("editorial change changed execution")
		}
		if _, err := catalog.DescribePreset(d, id, " \n ", nil, false); err == nil {
			t.Fatal("blank copy accepted")
		}
	}
	if count == 0 {
		t.Fatal("catalog has no presets")
	}
	var roster []string
	for _, id := range d.Presentation.PresetOrder {
		if d.Presets[id].Presentation.Group == "general" {
			roster = append(roster, id)
		}
	}
	if !slices.Equal(roster, []string{"qwen3.8-27b-q4xl-splash", "muse-glimmer-30b-q4xl-llama", "gemma-4-26b-a4b-qat-ud-q4-k-xl-llama", "gemma-4-31b-qat-ud-q4-k-xl-llama", "qwen3.8-27b-q4xl-mtp"}) {
		t.Fatal("owner's recommendation roster/order changed", roster)
	}
	current, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := catalog.Parse(current)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Presets) != len(d.Presets) {
		t.Fatal("catalog lost nonrecommended presets")
	}
}

func TestGuidedPresetsDefaultToMediumThinking(t *testing.T) {
	raw, err := os.ReadFile("../testfixture/testdata/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	for id, p := range d.Presets {
		if p.Presentation.Group == "specialist" {
			// Extraction and translation use the publishers' direct-output
			// templates; reranking produces scores rather than generated text.
			if p.Interface == "reranking" {
				if p.RequestDefaults != (catalog.RequestDefaults{}) {
					t.Fatalf("%s carries generation defaults", id)
				}
			} else if p.RequestDefaults.Reasoning != "off" || p.RequestDefaults.Sampling.Temperature != 0 {
				t.Fatalf("%s must preserve its direct, greedy specialist interface", id)
			}
			continue
		}
		t.Run(id, func(t *testing.T) {
			lock, err := catalog.CompilePreset(d, id, 0, 0, software.Target{OS: "darwin", Arch: "arm64"})
			if err != nil {
				t.Fatal(err)
			}
			projections, err := lock.Projections()
			if err != nil {
				t.Fatal(err)
			}
			layout := projections.Manifest.Layouts[id]
			effort := p.Config().Controls.ReasoningEffort
			if p.Config().Splash != nil {
				effort = p.Config().Splash.ReasoningEffort
			}
			if layout.Thinking != "on" || effort != "medium" {
				t.Fatalf("default thinking = %s/%s; exceptions need a test showing why medium is unsuitable", layout.Thinking, effort)
			}
		})
	}
}
