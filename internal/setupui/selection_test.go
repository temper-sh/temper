package setupui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestInstallChoicesDoNotChooseOrReplaceTheDefault(t *testing.T) {
	in := setupInput()
	in.Profiles = append(in.Profiles, Profile{Option: Option{ID: "alternate", Name: "Alternate"}, Mode: "local"})
	m := NewModel(context.Background(), in, readyPreview)
	press(m, tea.KeyEnter)
	press(m, 'n')
	press(m, tea.KeySpace) // Install Compact without choosing the foreground.
	press(m, 'n')
	if m.stage != stageProfile || m.choices().DefaultProfile != "" || len(m.choices().Profiles) != 1 {
		t.Fatalf("install implicitly chose a default: %+v", m.choices())
	}
	press(m, tea.KeyEnter) // Compact is now the explicit default.
	press(m, tea.KeyDown)
	press(m, tea.KeySpace) // Also install Alternate.
	if c := m.choices(); c.DefaultProfile != "compact" || len(c.Profiles) != 2 {
		t.Fatalf("installing an alternative replaced the default: %+v", c)
	}
	press(m, 'd') // Switch the default, retaining Compact.
	if c := m.choices(); c.DefaultProfile != "alternate" || len(c.Profiles) != 2 {
		t.Fatalf("default switch lost an installed choice: %+v", c)
	}
	press(m, tea.KeySpace) // Remove the default.
	press(m, 'n')
	if m.stage != stageProfile || m.choices().DefaultProfile != "" || len(m.choices().Profiles) != 1 {
		t.Fatalf("removal silently chose a replacement default: %+v", m.choices())
	}
	press(m, tea.KeyUp)
	press(m, tea.KeyEnter)
	press(m, 'n')
	if m.stage != stageTemplates {
		t.Fatal("could not continue after explicitly choosing the remaining model")
	}
}

func TestAlternativesKeepIndependentTemplatesAndContextThroughReview(t *testing.T) {
	in := setupInput()
	in.Profiles[0].Contexts = []ContextWindow{{Layout: "small", Name: "Compact", Minimum: 1025, Maximum: 32768, ManualRequired: true}}
	alternate := in.Profiles[0]
	alternate.Option = Option{ID: "alternate", Name: "Alternate composition"}
	in.Profiles = append(in.Profiles, alternate)
	var reviewed Choices
	m := NewModel(context.Background(), in, func(_ context.Context, c Choices) (Review, error) {
		reviewed = c
		return Review{Token: "both-models", CanPrepare: true}, nil
	})
	press(m, tea.KeyEnter)
	press(m, 'n')
	press(m, tea.KeyEnter) // Compact default.
	press(m, tea.KeyDown)
	press(m, tea.KeySpace) // Install Alternate.
	press(m, 'n')          // Compact templates.
	press(m, tea.KeyDown)
	press(m, tea.KeyEnter) // Sharp for Compact only.
	press(m, 'n')
	for _, key := range "8192" {
		m.Update(tea.KeyPressMsg{Code: key, Text: string(key)})
	}
	press(m, tea.KeyTab) // Alternate templates.
	if m.stage != stageTemplates || m.profileAt != 1 {
		t.Fatalf("did not configure the alternative: stage=%v profile=%d", m.stage, m.profileAt)
	}
	press(m, 'n') // Keep Alternate's embedded template.
	for _, key := range "16384" {
		m.Update(tea.KeyPressMsg{Code: key, Text: string(key)})
	}
	press(m, tea.KeyTab)
	press(m, tea.KeyEnter) // Recorded software.
	cmd := press(m, 'n')
	if cmd == nil {
		t.Fatal("review did not start")
	}
	m.Update(cmd())
	if reviewed.DefaultProfile != "compact" || len(reviewed.Profiles) != 2 {
		t.Fatalf("review lost selection: %+v", reviewed)
	}
	if reviewed.Profiles[0].Templates["small"] != "sharp" || reviewed.Profiles[0].ContextWindows["small"] != 8192 || reviewed.Profiles[1].Templates["small"] != "" || reviewed.Profiles[1].ContextWindows["small"] != 16384 {
		t.Fatalf("choices leaked between compositions: %+v", reviewed)
	}
	// A missing automatic point must route to the alternative, preserving the
	// default and the first model's settings.
	m.Update(previewMsg{seq: m.seq, review: Review{ContextRequired: &ContextRequest{Profile: "alternate", Layout: "small"}}})
	if m.stage != stageContext || m.profileAt != 1 || m.choices().DefaultProfile != "compact" {
		t.Fatal("context request did not return to the selected alternative")
	}
	press(m, tea.KeyEsc) // Its template screen.
	press(m, tea.KeyEsc) // Previous model's context screen.
	if p, _ := m.currentProfile(); p.ID != "compact" || m.stage != stageContext || m.contextChoices("local", "compact")["small"] != 8192 {
		t.Fatal("back navigation lost the first model's context")
	}
}

func TestModelGroupsAndSeparateInstallCheckboxRemainUsable(t *testing.T) {
	in := setupInput()
	in.Profiles[0].MemoryTier = "S"
	in.Profiles[0].Components = "Weights: Unsloth Q4 GGUF\nEngine: llama.cpp"
	in.Profiles = append(in.Profiles, Profile{Option: Option{ID: "tiny", Name: "Tiny"}, Mode: "local", MemoryTier: "XS"})
	m := NewModel(context.Background(), in, readyPreview)
	press(m, tea.KeyEnter)
	press(m, 'n')
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 45})
	view := ansi.Strip(m.View().Content)
	if strings.Index(view, "S · >16–32 GB") < 0 || strings.Index(view, "S · >16–32 GB") >= strings.Index(view, "XS · up to 16 GB") {
		t.Fatalf("tier order/ranges missing: %s", view)
	}
	if !strings.Contains(view, "Weights: Unsloth Q4 GGUF") || !strings.Contains(view, "Engine: llama.cpp") {
		t.Fatalf("composition missing: %s", view)
	}
	clickText(t, m, "[ ]")
	if len(m.choices().Profiles) != 1 || m.choices().DefaultProfile != "" {
		t.Fatal("clicking Install selected a default")
	}
	clickText(t, m, "Compact general")
	if m.choices().DefaultProfile != "compact" {
		t.Fatal("clicking the foreground choice did not select the default")
	}
}
