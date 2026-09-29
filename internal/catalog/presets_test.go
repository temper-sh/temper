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

func TestRecommendedPresetsHaveManualCopyWithoutChangingExecution(t *testing.T) {
	raw, err := os.ReadFile("../../catalog/guided-setup.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for id, p := range d.Presets {
		if !p.Recommended {
			continue
		}
		count++
		if strings.TrimSpace(p.Description) == "" {
			t.Fatalf("missing copy for %s", id)
		}
		before, err := catalog.CompilePreset(d, id, "", 0, software.Target{OS: "darwin", Arch: "arm64"})
		if err != nil {
			t.Fatal(err)
		}
		edited, err := catalog.DescribePreset(d, id, "Owner's revised assessment.", nil, false)
		if err != nil {
			t.Fatal(err)
		}
		after, err := catalog.CompilePreset(edited, id, "", 0, software.Target{OS: "darwin", Arch: "arm64"})
		if err != nil {
			t.Fatal(err)
		}
		if before.ExecutionDigest != after.ExecutionDigest {
			t.Fatal("editorial change changed execution")
		}
		if _, err := catalog.DescribePreset(d, id, " \n ", nil, false); err == nil {
			t.Fatal("blank recommended copy accepted")
		}
	}
	if count != 5 {
		t.Fatalf("recommendations = %d", count)
	}
	var roster []string
	for _, id := range d.PresetOrder {
		if d.Presets[id].Recommended {
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
		t.Fatal("All lost nonrecommended presets")
	}
}

func TestGuidedPresetsDefaultToMediumThinking(t *testing.T) {
	raw, err := os.ReadFile("../../catalog/guided-setup.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	for id, p := range d.Presets {
		t.Run(id, func(t *testing.T) {
			lock, err := catalog.CompilePreset(d, id, "", 0, software.Target{OS: "darwin", Arch: "arm64"})
			if err != nil {
				t.Fatal(err)
			}
			projections, err := lock.Projections()
			if err != nil {
				t.Fatal(err)
			}
			layout := projections.Manifest.Layouts[id]
			effort := p.EngineConfig.Controls.ReasoningEffort
			if p.EngineConfig.Splash != nil {
				effort = p.EngineConfig.Splash.ReasoningEffort
			}
			if layout.Thinking != "on" || effort != "medium" {
				t.Fatalf("default thinking = %s/%s; exceptions need a test showing why medium is unsuitable", layout.Thinking, effort)
			}
		})
	}

	// The new default must not inherit the old Gemma 31B execution witness.
	// Restoring its former off setting still reconstructs that exact composition.
	const id = "gemma-4-31b-qat-ud-q4-k-xl-llama"
	const historical = "694d4a685ec673fb79aaa88eac4197a3a35cdd647696c7538a47559c1f81330b"
	current, err := catalog.ContextExecutionSHA256(d, id, "", 40960)
	if err != nil || current == historical {
		t.Fatal("changed thinking default inherited historical evidence", current, err)
	}
	old := d.Presets[id]
	old.RequestDefaults.Reasoning = "off"
	d.Presets[id] = old
	restored, err := catalog.ContextExecutionSHA256(d, id, "", 40960)
	if err != nil || restored != historical {
		t.Fatal("change went beyond the new thinking default", restored, err)
	}
}
