package setupui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func contextInput() Input {
	in := setupInput()
	in.Profiles[0].Templates = []Template{{Layout: "small", Options: []Option{{ID: "", Name: "Embedded"}}}}
	in.Profiles[0].Contexts = []ContextWindow{{Layout: "small", Name: "Compact chat", Minimum: 4097, Maximum: 262144, RecordedDefaults: map[string]int{"": 32768}}}
	in.Profiles[1].Contexts = in.Profiles[0].Contexts
	return in
}

func ctrl(m *Model, key rune) {
	m.Update(tea.KeyPressMsg{Code: key, Mod: tea.ModCtrl})
}

func TestMissingContextFindingsAskForInputBeforeReview(t *testing.T) {
	in := contextInput()
	in.Profiles[1].Contexts = []ContextWindow{{Layout: "small", Name: "Helper", Minimum: 4097, Maximum: 65536, ManualRequired: true}}
	m := NewModel(context.Background(), in, readyPreview)
	press(m, tea.KeyDown)
	press(m, tea.KeyEnter) // utility
	press(m, 'n')
	press(m, tea.KeyEnter) // helper profile
	press(m, 'n')
	view := ansi.Strip(m.View().Content)
	if m.stage != stageContext || strings.Contains(view, "Tokens: auto") || !strings.Contains(view, "Enter a token count") {
		t.Fatalf("unavailable automatic context was offered: %s", view)
	}
	press(m, 'a') // An unavailable automatic choice must not replace manual input.
	if cmd := press(m, 'n'); cmd != nil || m.stage != stageContext || m.notice == "" {
		t.Fatal("missing context advanced to a failing preview")
	}
	m.Update(tea.PasteMsg{Content: "16384"})
	press(m, 'n')
	press(m, tea.KeyEnter) // recorded
	cmd := press(m, 'n')
	if cmd == nil {
		t.Fatal("explicit context could not reach review")
	}
	m.Update(cmd())
	press(m, tea.KeyEnter) // save
	if m.decision.Action != "save" || m.decision.Choices.Profiles[0].ContextWindows["small"] != 16384 {
		t.Fatalf("manual context lost at save: %+v", m.decision)
	}
}

func TestResolvedContextRequestReturnsToItsModeAndPreservesOtherChoices(t *testing.T) {
	var reviewed Choices
	m := NewModel(context.Background(), contextInput(), func(ctx context.Context, choices Choices) (Review, error) {
		if choices.Profiles[1].ContextWindows["small"] == 0 {
			return Review{ContextRequired: &ContextRequest{Profile: "specialists", Layout: "small"}}, nil
		}
		reviewed = choices
		return readyPreview(ctx, choices)
	})
	press(m, tea.KeyEnter) // local
	press(m, tea.KeyDown)
	press(m, tea.KeyEnter) // utility too
	press(m, 'n')
	press(m, tea.KeyEnter)
	press(m, 'n') // local context
	ctrl(m, 'u')
	m.Update(tea.PasteMsg{Content: "65536"})
	press(m, 'n')
	press(m, tea.KeyEnter)
	press(m, 'n') // utility context stays automatic
	press(m, 'n')
	press(m, tea.KeyDown)
	press(m, tea.KeyEnter) // latest
	cmd := press(m, 'n')
	if cmd == nil {
		t.Fatal("preview not started")
	}
	m.Update(cmd())
	if m.stage != stageContext || m.currentMode() != "utility" || m.reviewErr != nil || m.loading {
		t.Fatalf("context requirement became a preview error: stage=%v, mode=%s, error=%v", m.stage, m.currentMode(), m.reviewErr)
	}
	if choices := m.choices(); choices.Software != "latest" || choices.Profiles[0].ContextWindows["small"] != 65536 {
		t.Fatalf("returning to context lost other choices: %+v", choices)
	}
	if view := ansi.Strip(m.View().Content); strings.Contains(view, "Preview error") || !strings.Contains(view, "Enter a token count") {
		t.Fatalf("missing contextual recovery instructions: %s", view)
	}
	ctrl(m, 'u')
	m.Update(tea.PasteMsg{Content: "32768"})
	press(m, 'n') // retains latest
	cmd = press(m, 'n')
	if cmd == nil {
		t.Fatal("corrected context could not reach review")
	}
	m.Update(cmd())
	press(m, tea.KeyEnter)
	if m.decision.Action != "save" || reviewed.Software != "latest" || reviewed.Profiles[0].ContextWindows["small"] != 65536 || reviewed.Profiles[1].ContextWindows["small"] != 32768 {
		t.Fatalf("context recovery failed to save the reviewed choices: %+v", m.decision)
	}
}

func TestContextAutomaticDefaultAndEditableWindowReachReview(t *testing.T) {
	var reviewed Choices
	m := NewModel(context.Background(), contextInput(), func(ctx context.Context, c Choices) (Review, error) {
		reviewed = c
		return readyPreview(ctx, c)
	})
	press(m, tea.KeyEnter) // local
	press(m, 'n')
	press(m, tea.KeyEnter) // compact
	press(m, 'n')          // no template alternatives; context still appears
	if m.stage != stageContext || m.choices().Profiles[0].ContextWindows["small"] != 0 {
		t.Fatalf("automatic context was not the default: %+v", m.choices())
	}
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Context", "262144", "Next", "Ctrl+U"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q: %s", want, view)
		}
	}
	if strings.Contains(view, "Templates") {
		t.Fatal("fixed template acquired a tab")
	}
	press(m, tea.KeyLeft)
	press(m, tea.KeyBackspace)
	if m.stage != stageContext {
		t.Fatal("editing keys navigated away from the context field")
	}
	press(m, tea.KeyEnd)
	ctrl(m, 'u')
	for _, digit := range "65536" {
		m.Update(tea.KeyPressMsg{Code: digit, Text: string(digit)})
	}
	if got := m.choices().Profiles[0].ContextWindows["small"]; got != 65536 {
		t.Fatalf("typed override = %d", got)
	}
	press(m, tea.KeyEsc)
	press(m, tea.KeyEnter) // reselect the same model
	press(m, 'n')
	if m.stage != stageContext || m.choices().Profiles[0].ContextWindows["small"] != 65536 {
		t.Fatal("back/reselection discarded override")
	}
	press(m, tea.KeyEnter) // accept context
	press(m, tea.KeyEnter) // recorded software
	cmd := press(m, 'n')
	if cmd == nil {
		t.Fatal("preview not started")
	}
	m.Update(cmd())
	press(m, tea.KeyEnter) // save
	if m.decision.Action != "save" || reviewed.Profiles[0].ContextWindows["small"] != 65536 || m.decision.Choices.Profiles[0].ContextWindows["small"] != 65536 {
		t.Fatalf("review/save lost context: %+v", m.decision)
	}
}

func TestContextInvalidValuesRefuseNextAndAutomaticRecovers(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "4096", "262145", "abc", "9999999999999999999999"} {
		t.Run(value, func(t *testing.T) {
			m := NewModel(context.Background(), contextInput(), readyPreview)
			press(m, tea.KeyEnter)
			press(m, 'n')
			press(m, tea.KeyEnter)
			press(m, 'n')
			ctrl(m, 'u')
			m.Update(tea.PasteMsg{Content: value})
			press(m, 'n')
			if m.stage != stageContext || !strings.Contains(m.notice, "between 4097 and 262144") {
				t.Fatalf("invalid context advanced: stage=%v notice=%s", m.stage, m.notice)
			}
			press(m, 'a')
			press(m, 'n')
			if m.stage != stageSoftware || m.choices().Profiles[0].ContextWindows["small"] != 0 {
				t.Fatal("automatic reset did not recover")
			}
		})
	}
}

func TestContextChoicesRemainIndependentAcrossModes(t *testing.T) {
	m := NewModel(context.Background(), contextInput(), readyPreview)
	press(m, tea.KeyEnter) // local
	press(m, tea.KeyDown)
	press(m, tea.KeyEnter) // utility too
	press(m, 'n')
	press(m, tea.KeyEnter) // local compact
	press(m, 'n')
	ctrl(m, 'u')
	m.Update(tea.PasteMsg{Content: "65536"})
	press(m, 'n')
	press(m, tea.KeyEnter) // utility profile; same layout
	press(m, 'n')
	if m.currentMode() != "utility" || m.stage != stageContext {
		t.Fatal("utility context was skipped")
	}
	choices := m.choices()
	if choices.Profiles[0].ContextWindows["small"] != 65536 || choices.Profiles[1].ContextWindows["small"] != 0 {
		t.Fatalf("contexts leaked between modes: %+v", choices)
	}
	press(m, 'n')
	press(m, tea.KeyEsc) // software back to utility context
	if m.stage != stageContext || m.currentMode() != "utility" {
		t.Fatal("back skipped utility context")
	}
	press(m, tea.KeyEsc) // utility model
	press(m, tea.KeyEsc) // previous mode's last screen
	if m.stage != stageContext || m.currentMode() != "local" || m.choices().Profiles[0].ContextWindows["small"] != 65536 {
		t.Fatal("back across modes lost local context")
	}
}

func TestContextHintFollowsTemplateWhileAutomaticRemainsUnresolved(t *testing.T) {
	in := contextInput()
	in.Profiles[0].Templates = []Template{{Layout: "small", Default: "", Options: []Option{{ID: "", Name: "Embedded"}, {ID: "alternative", Name: "Alternative"}}}}
	m := NewModel(context.Background(), in, readyPreview)
	press(m, tea.KeyEnter)
	press(m, 'n')
	press(m, tea.KeyEnter)
	press(m, 'n')
	press(m, 'n') // Context with the tested embedded template.
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "largest verified window 32768") || strings.Contains(view, "Maximum/default") {
		t.Fatalf("misleading default: %s", view)
	}
	press(m, tea.KeyEsc)
	press(m, tea.KeyDown)
	press(m, tea.KeyEnter)
	press(m, 'n')
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "tested context unknown") || strings.Contains(view, "largest verified window 32768") {
		t.Fatalf("template inherited a tested hint: %s", view)
	}
	if len(m.choices().Profiles[0].ContextWindows) != 0 {
		t.Fatal("automatic hint became an explicit number before software resolution")
	}
}

func TestContextInputRemainsVisibleAndMouseNextValidatesAfterResize(t *testing.T) {
	for _, size := range [][2]int{{28, 10}, {36, 12}, {48, 16}, {80, 24}, {120, 40}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			in := contextInput()
			in.Profiles[0].Contexts[0].Name = "Qwen3.8 27B Q4 XL / medium thinking / MTP"
			m := NewModel(context.Background(), in, readyPreview)
			press(m, tea.KeyEnter)
			press(m, 'n')
			press(m, tea.KeyEnter)
			press(m, 'n')
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			view := ansi.Strip(m.View().Content)
			if !strings.Contains(view, "Tokens: auto") || !strings.Contains(view, "Next") || lipgloss.Width(view) > size[0] || lipgloss.Height(view) > size[1] {
				t.Fatalf("context input hidden or frame overflow: %s", view)
			}
			ctrl(m, 'u')
			m.Update(tea.PasteMsg{Content: "999999"})
			m.View()
			click := func() {
				t.Helper()
				for _, target := range m.hitTargets {
					if target.kind == hitNext {
						m.Update(tea.MouseClickMsg{X: target.x, Y: target.y, Button: tea.MouseLeft})
						return
					}
				}
				t.Fatal("Next button has no visible target")
			}
			click()
			if m.stage != stageContext || m.notice == "" {
				t.Fatal("mouse bypassed context bounds")
			}
			press(m, 'a')
			m.View()
			click()
			if m.stage != stageSoftware || m.choices().Profiles[0].ContextWindows["small"] != 0 {
				t.Fatal("mouse Next lost automatic choice")
			}
		})
	}
}
