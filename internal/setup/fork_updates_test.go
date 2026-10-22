package setup_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/setup"
)

func TestForkComparisonUsesExplicitOverridesAndSelectedOriginProfile(t *testing.T) {
	d := catalogDocument(t)
	p := d.Presets[compactLayout]
	large := p.Profiles[0]
	large.MinMemoryGiB, large.ContextWindowTokens, large.EngineConfig.BatchTokens = 16, 16384, 1024
	p.Profiles = append([]catalog.Profile{p.Profiles[0]}, large)
	p.RequestDefaults.Sampling.Seed = nil
	p.RequestDefaults.ReasoningBudgetTokens = nil
	d.Presets[compactLayout] = p
	file := catalog.UserPreset{Origin: compactLayout, Changes: map[string]any{
		"context_window_tokens": 8192,
		"engine_config":         map[string]any{"batch_tokens": 256, "controls": map[string]any{"cache_prompt": false}},
		"request_defaults":      map[string]any{"reasoning_budget_tokens": 0, "sampling": map[string]any{"temperature": 0.7, "seed": 17}},
	}}
	before := setup.CaptureForkBaseline(file, d, 24)
	// Exercise the persisted representation: unknown, unset and zero must
	// survive a configuration round trip as distinct facts.
	raw, err := json.Marshal(before)
	if err != nil || json.Unmarshal(raw, &before) != nil {
		t.Fatal("baseline could not round trip", err)
	}
	p.Profiles[0].EngineConfig.BatchTokens = 4096 // Not this Mac's profile.
	p.Profiles[1].ContextWindowTokens = 12288
	p.Profiles[1].EngineConfig.BatchTokens = 2048
	p.Profiles[1].EngineConfig.Controls.CachePrompt = false
	p.RequestDefaults.Sampling.Temperature = 0.9
	p.RequestDefaults.Sampling.TopP = 0.95 // Inherited; never an override report.
	p.RequestDefaults.Sampling.Seed = new(0)
	p.RequestDefaults.ReasoningBudgetTokens = new(-1)
	d.Presets[compactLayout] = p
	after := setup.CaptureForkBaseline(file, d, 24)
	changes, untracked := setup.CompareForkOverrides(before, after, file)
	want := []setup.OverrideChange{
		{Path: "context_window_tokens", From: json.RawMessage("16384"), To: json.RawMessage("12288"), Override: json.RawMessage("8192")},
		{Path: "engine_config.batch_tokens", From: json.RawMessage("1024"), To: json.RawMessage("2048"), Override: json.RawMessage("256")},
		{Path: "engine_config.controls.cache_prompt", From: json.RawMessage("true"), To: json.RawMessage("false"), Override: json.RawMessage("false")},
		{Path: "request_defaults.reasoning_budget_tokens", From: json.RawMessage("null"), To: json.RawMessage("-1"), Override: json.RawMessage("0")},
		{Path: "request_defaults.sampling.seed", From: json.RawMessage("null"), To: json.RawMessage("0"), Override: json.RawMessage("17")},
		{Path: "request_defaults.sampling.temperature", From: json.RawMessage("0.7"), To: json.RawMessage("0.9"), Override: json.RawMessage("0.7")},
	}
	if !reflect.DeepEqual(changes, want) || len(untracked) != 0 {
		t.Fatalf("masked catalog changes: %+v, untracked %v", changes, untracked)
	}
	unchanged, _ := json.Marshal(before)
	if string(unchanged) != string(raw) {
		t.Fatal("comparison advanced the saved baseline")
	}
	if changes, untracked := setup.CompareForkOverrides(after, after, file); len(changes)+len(untracked) != 0 {
		t.Fatal("unchanged explicit overrides reported a catalog change", changes, untracked)
	}
	p.RequestDefaults.Sampling.Seed = nil
	d.Presets[compactLayout] = p
	removed := setup.CaptureForkBaseline(file, d, 24)
	changes, untracked = setup.CompareForkOverrides(after, removed, file)
	if len(changes) != 1 || changes[0].Path != "request_defaults.sampling.seed" || string(changes[0].From) != "0" || string(changes[0].To) != "null" || len(untracked) != 0 {
		t.Fatal("removing a zero-valued optional catalog field lost its meaning", changes, untracked)
	}
}

func TestForkComparisonDoesNotInventHistoryOrTrackRemovedOverrides(t *testing.T) {
	d := catalogDocument(t)
	file := catalog.UserPreset{Origin: compactLayout, Changes: map[string]any{"request_defaults": map[string]any{"sampling": map[string]any{"temperature": 0.6}}}}
	before := setup.CaptureForkBaseline(file, d, 8)
	file.Changes = map[string]any{"engine_config": map[string]any{"batch_tokens": 256}}
	after := setup.CaptureForkBaseline(file, d, 8)
	for _, previous := range []*setup.ForkBaseline{before, nil, {Origin: "another-origin", Values: after.Values}} {
		changes, untracked := setup.CompareForkOverrides(previous, after, file)
		if len(changes) != 0 || !reflect.DeepEqual(untracked, []string{"engine_config.batch_tokens"}) {
			t.Fatal("unknown or different origin invented history", changes, untracked)
		}
	}
	if _, retained := after.Values["request_defaults.sampling.temperature"]; retained {
		t.Fatal("removed override retained a comparison value")
	}
	file.Changes = nil
	if b := setup.CaptureForkBaseline(file, d, 8); b == nil || len(b.Values) != 0 {
		t.Fatal("an override-free fork kept stale fields", b)
	}
	for _, file := range []catalog.UserPreset{{Preset: new(d.Presets[compactLayout])}, {Origin: "missing-origin"}} {
		if baseline := setup.CaptureForkBaseline(file, d, 8); baseline != nil {
			t.Fatal("copy or missing origin acquired a baseline", baseline)
		}
	}
}
