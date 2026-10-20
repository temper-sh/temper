package catalog_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/render"
	"gopkg.in/yaml.v3"
)

func TestReasoningBudgetSurvivesLockConfigureAndRendering(t *testing.T) {
	for _, tc := range []struct {
		name   string
		budget *int
	}{
		{"omitted", nil},
		{"unrestricted", new(-1)},
		{"immediate end", new(0)},
		{"bounded thinking", new(1024)},
		{"largest native counter", new(math.MaxInt32)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := guidedCatalog(t)
			p := d.Presets[mtp]
			p.RequestDefaults.ReasoningBudgetTokens = tc.budget
			p.RequestDefaults.MaxOutputTokens = 0
			d.Presets[mtp] = p
			raw, err := json.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			d, err = catalog.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			lock := compileGuided(t, d, mtp)
			raw, err = catalog.MarshalLock(lock)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(raw, []byte(`"reasoning_budget_tokens"`)) != (tc.budget != nil) {
				t.Fatal("omitted budget and explicit zero did not retain distinct representations")
			}
			lock, err = catalog.ParseLock(raw)
			if err != nil {
				t.Fatal(err)
			}
			lock, err = catalog.ConfigureExecution(lock, mtp, catalog.ExecutionSettings{ContextWindowTokens: 8192})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(lock.Records.Presets[mtp].RequestDefaults.ReasoningBudgetTokens, tc.budget) {
				t.Fatal("context edit changed the thinking default")
			}
			projection, err := lock.Projections()
			if err != nil {
				t.Fatal(err)
			}
			raw, err = yaml.Marshal(projection.Manifest)
			if err != nil {
				t.Fatal(err)
			}
			projection.Manifest, err = manifest.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			bundle, err := render.Build(render.Inputs{StartPort: 10001, Manifest: projection.Manifest, Lock: projection.Artifacts, Mode: mtp, Root: "/reasoning-test"})
			if err != nil {
				t.Fatal(err)
			}
			var config struct {
				Models map[string]struct {
					Cmd     string
					Filters struct {
						SetParams map[string]any `yaml:"setParams"`
					}
				}
			}
			for _, file := range bundle.Artifacts {
				if file.Path == "llama-swap/config.yaml" {
					if err := yaml.Unmarshal(file.Data, &config); err != nil {
						t.Fatal(err)
					}
				}
			}
			model, ok := config.Models[mtp]
			if !ok {
				t.Fatal("missing model configuration")
			}
			if !strings.Contains(model.Cmd, "--predict -1") {
				t.Fatal("thinking budget imposed a total output cap")
			}
			if tc.budget == nil {
				if len(model.Filters.SetParams) != 0 || strings.Contains(model.Cmd, "--reasoning-budget") {
					t.Fatal("omission changed existing launch behavior")
				}
				return
			}
			// Assert the upstream filter contract, not a reimplementation of it:
			// '?' preserves a caller's alias; the canonical native field wins
			// over that alias. CLI -1 keeps either request spelling of -1 unlimited.
			want := map[string]any{"thinking_budget_tokens?": *tc.budget}
			if !reflect.DeepEqual(model.Filters.SetParams, want) || !strings.Contains(model.Cmd, "--reasoning-budget -1") {
				t.Fatalf("request override contract lost: command=%s filters=%v", model.Cmd, model.Filters.SetParams)
			}
		})
	}
}

func TestUserForkCanOverrideReasoningBudgetIncludingZeroAndUnrestricted(t *testing.T) {
	d := guidedCatalog(t)
	p := d.Presets[mtp]
	p.RequestDefaults.ReasoningBudgetTokens = new(1024)
	d.Presets[mtp] = p
	origin := compileGuided(t, d, mtp)
	for _, budget := range []int{-1, 0, 512, 2048} {
		t.Run(fmt.Sprint(budget), func(t *testing.T) {
			user := userPreset(t, "mine", fmt.Sprintf(`{"schema":"temper-preset/v1","origin":%q,"changes":{"request_defaults":{"reasoning_budget_tokens":%d}}}`, mtp, budget))
			lock, _ := compileUser(t, d, "mine", user)
			got := lock.Records.Presets["mine"].RequestDefaults.ReasoningBudgetTokens
			if got == nil || *got != budget {
				t.Fatalf("budget = %v", got)
			}
			if catalog.SameExecution(origin, lock) {
				t.Fatal("budget change did not change execution identity")
			}
		})
	}
	if *d.Presets[mtp].RequestDefaults.ReasoningBudgetTokens != 1024 {
		t.Fatal("fork changed its origin")
	}
}

func TestReasoningBudgetInvalidOrUnsupportedPresetsAreRefused(t *testing.T) {
	for _, tc := range []struct {
		id     string
		budget int
		reason string
	}{
		{mtp, -2, "reasoning_budget_tokens"},
		{mtp, math.MaxInt32 + 1, "reasoning_budget_tokens"},
		{"qwen3.8-27b-q4xl-splash", 1024, "reasoning_budget_tokens"},
		{"ettin-reranker-150m-q8-crispembed", 0, "generation defaults"},
	} {
		t.Run(fmt.Sprintf("%s/%d", tc.id, tc.budget), func(t *testing.T) {
			d := guidedCatalog(t)
			p := d.Presets[tc.id]
			p.RequestDefaults.ReasoningBudgetTokens = &tc.budget
			d.Presets[tc.id] = p
			if _, err := catalog.CompilePreset(d, tc.id, 32, 0, target); err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("expected refusal for %s, got %v", tc.reason, err)
			}
		})
	}
}

func TestManifestReasoningBudgetRejectsNativeCounterOverflow(t *testing.T) {
	lock := compileGuided(t, guidedCatalog(t), mtp)
	projection, err := lock.Projections()
	if err != nil {
		t.Fatal(err)
	}
	layout := projection.Manifest.Layouts[mtp]
	layout.ReasoningBudgetTokens = new(math.MaxInt32 + 1)
	projection.Manifest.Layouts[mtp] = layout
	if err := projection.Manifest.Validate(); err == nil || !strings.Contains(err.Error(), "reasoning_budget_tokens") {
		t.Fatalf("manifest accepted a budget outside the native counter range: %v", err)
	}
}
