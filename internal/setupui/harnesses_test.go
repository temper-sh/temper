package setupui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/harness"
	"github.com/temper-sh/temper/internal/setup"
)

func TestHarnessStepIsExplicitRevocableAndKeepsReviewedDestinations(t *testing.T) {
	input := presetEditor(t).input
	input.Harnesses = []harness.Option{
		{Selection: harness.Selection{ID: "pi", ModelsFile: "/reviewed/models.json"}, Name: "Pi", Detected: true},
		{Selection: harness.Selection{ID: "pig", ModelsFile: "/reviewed/models.json"}, Name: "PiG", Detected: false},
	}
	m := NewPresetModel(context.Background(), input, func(_ context.Context, c setup.Configuration) (setup.Plan, error) {
		return setup.Plan{Configuration: &c, CanPrepare: true}, nil
	})
	m.page = 1
	m.advance()
	if m.page != presetHarnessPage || len(m.draft.Integrations) != 0 {
		t.Fatal("detection implied selection")
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Shared models.json") || !strings.Contains(view, "/reviewed/models.json") {
		t.Fatal(view)
	}
	editorKey(m, ' ')
	if len(m.draft.Integrations) != 1 || len(input.Configuration.Integrations) != 0 {
		t.Fatal("selection not revocable")
	}
	m.cursor = 1
	editorKey(m, ' ')
	if len(m.draft.Integrations) != 1 || m.notice == "" {
		t.Fatal("unavailable harness selected")
	}
	preview := m.advance()
	m.Update(preview())
	if m.page != presetReviewPage || len(m.plan.Configuration.Integrations) != 1 {
		t.Fatal("selection lost at review")
	}
	m.back()
	m.cursor = 0
	editorKey(m, ' ')
	if len(m.draft.Integrations) != 0 {
		t.Fatal("could not deselect")
	}

	input.Configuration.Integrations = []harness.Selection{{ID: "pi", ModelsFile: "/saved/models.json"}}
	m = NewPresetModel(context.Background(), input, nil)
	if m.harnessOptions()[0].ModelsFile != "/saved/models.json" {
		t.Fatal("changed environment redirected saved integration")
	}
	m.page = presetHarnessPage
	m.toggleHarness()
	if len(input.Configuration.Integrations) != 1 {
		t.Fatal("cancelling would mutate saved selection")
	}
}

func TestHermesSelectionKeepsTheReviewedProfile(t *testing.T) {
	input := presetEditor(t).input
	target := harness.Selection{ID: "hermes", ModelsFile: "/home/.hermes/profiles/work/config.yaml"}
	input.Harnesses = []harness.Option{{Selection: target, Name: "Hermes", Detected: true}}
	m := NewPresetModel(context.Background(), input, func(_ context.Context, c setup.Configuration) (setup.Plan, error) {
		return setup.Plan{Configuration: &c, CanPrepare: true}, nil
	})
	m.page = presetHarnessPage
	if len(m.draft.Integrations) != 0 {
		t.Fatal("Hermes detection selected the integration")
	}
	m.toggleHarness()
	preview := m.advance()
	m.Update(preview())
	if len(m.plan.Configuration.Integrations) != 1 || m.plan.Configuration.Integrations[0] != target {
		t.Fatal("review lost the selected Hermes profile")
	}
	// A later configure session discovers another profile. The saved selection
	// remains authoritative even when that profile is no longer on PATH.
	input.Configuration.Integrations = []harness.Selection{target}
	input.Harnesses[0].ModelsFile = "/home/.hermes/profiles/personal/config.yaml"
	input.Harnesses[0].Detected = false
	m = NewPresetModel(context.Background(), input, nil)
	if m.harnessOptions()[0].Selection != target {
		t.Fatal("active profile change redirected an existing integration")
	}
	m.page = presetHarnessPage
	m.toggleHarness()
	if len(m.draft.Integrations) != 0 || len(input.Configuration.Integrations) != 1 {
		t.Fatal("deselection failed or mutated persisted configuration")
	}
}
