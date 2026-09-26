package setupui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func setupInput() Input {
	return Input{
		Machine: "Apple Silicon · 16 GiB · 90 GiB free",
		Modes: []Option{
			{ID: "local", Name: "Local", Description: "A local foreground model"},
			{ID: "utility", Name: "Utility", Description: "Local helpers beside a harness model"},
		},
		Profiles: []Profile{
			{Option: Option{ID: "compact", Name: "Compact general assistant", Description: "Everyday chat; coding unqualified"}, Mode: "local", Templates: []Template{
				{Layout: "small", Name: "Chat template", Default: "", Options: []Option{{ID: "", Name: "Embedded"}, {ID: "sharp", Name: "Sharp"}}},
			}},
			{Option: Option{ID: "specialists", Name: "Local specialists", Description: "Reranking and extraction"}, Mode: "utility"},
		},
	}
}

func readyPreview(_ context.Context, _ Choices) (Review, error) {
	return Review{Sections: []Section{{Title: "Downloads", Lines: []string{"Download 3 GiB; resident 2 GiB"}}}, CanPrepare: true, Token: "review-1"}, nil
}

func press(m *Model, key rune) tea.Cmd {
	_, cmd := m.Update(tea.KeyPressMsg{Code: key})
	return cmd
}

func TestModesRequireExplicitAvailableChoice(t *testing.T) {
	in := setupInput()
	in.Modes[0].DisabledReason = "No qualified foreground fits"
	m := NewModel(context.Background(), in, readyPreview)
	press(m, tea.KeyTab)
	if m.stage != stageModes || !strings.Contains(m.notice, "Choose at least one") {
		t.Fatalf("empty mode selection advanced: stage=%v notice=%q", m.stage, m.notice)
	}
	press(m, tea.KeySpace)
	if m.selected["local"] || !strings.Contains(m.notice, "No qualified foreground") {
		t.Fatalf("disabled local mode selected: %+v, %q", m.selected, m.notice)
	}
	press(m, tea.KeyDown)
	press(m, tea.KeySpace)
	press(m, tea.KeyTab)
	if m.stage != stageProfile || m.currentMode() != "utility" {
		t.Fatalf("utility selection = stage %v, mode %q", m.stage, m.currentMode())
	}
}

func TestDisabledProfileCannotBeChosen(t *testing.T) {
	in := setupInput()
	in.Profiles[0].DisabledReason = "Insufficient available memory"
	m := NewModel(context.Background(), in, readyPreview)
	press(m, tea.KeyEnter) // local mode
	press(m, tea.KeyTab)
	press(m, tea.KeyEnter) // disabled compact profile
	press(m, tea.KeyTab)
	if m.stage != stageProfile || m.profiles["local"] != "" {
		t.Fatalf("disabled profile advanced: stage=%v profiles=%+v", m.stage, m.profiles)
	}
	if !strings.Contains(m.notice, "Choose a profile") {
		t.Fatalf("missing refusal: %q", m.notice)
	}
}

func TestUtilityWorksWithoutLocalMain(t *testing.T) {
	m := NewModel(context.Background(), setupInput(), readyPreview)
	press(m, tea.KeyDown)
	press(m, tea.KeyEnter) // utility
	press(m, tea.KeyTab)
	press(m, tea.KeyTab) // no profile chosen
	if m.stage != stageProfile {
		t.Fatalf("advanced without profile: %v", m.stage)
	}
	press(m, tea.KeyEnter) // specialists profile
	press(m, tea.KeyTab)   // No template alternatives: directly to software.
	press(m, tea.KeyEnter) // recorded
	cmd := press(m, tea.KeyTab)
	if cmd == nil || m.stage != stageReview {
		t.Fatalf("preview not started: stage %v", m.stage)
	}
	m.Update(cmd())
	if m.loading || len(m.review.Sections) != 1 {
		t.Fatalf("preview = %+v", m.review)
	}
	choices := m.choices()
	if len(choices.Profiles) != 1 || choices.Profiles[0].Mode != "utility" || choices.Profiles[0].Profile != "specialists" {
		t.Fatalf("utility choices = %+v", choices)
	}
	if choices.Software != "recorded" {
		t.Fatalf("software = %q", choices.Software)
	}
}

func TestCompactLocalTemplateDefaultAndOverrideSurviveBack(t *testing.T) {
	m := NewModel(context.Background(), setupInput(), readyPreview)
	press(m, tea.KeyEnter) // local explicitly
	press(m, tea.KeyTab)
	press(m, tea.KeyEnter) // compact explicitly
	press(m, tea.KeyTab)
	if got := m.choices().Profiles[0].Templates["small"]; got != "" {
		t.Fatalf("default patch = %q", got)
	}
	press(m, tea.KeyDown)
	press(m, tea.KeyEnter) // Sharp override
	press(m, tea.KeyEsc)   // back to profile
	press(m, tea.KeyTab)   // return to templates
	if got := m.choices().Profiles[0].Templates["small"]; got != "sharp" {
		t.Fatalf("override lost: %q", got)
	}
	press(m, tea.KeyTab)
	if m.stage != stageSoftware {
		t.Fatalf("stage = %v", m.stage)
	}
	press(m, tea.KeyTab)
	if m.stage != stageSoftware {
		t.Fatal("software advanced without explicit choice")
	}
}

func TestTemplateScreenSkippedWithoutAlternatives(t *testing.T) {
	for _, tc := range []struct {
		name      string
		templates []Template
		want      string
	}{
		{name: "no templates"},
		{name: "embedded only", templates: []Template{{Layout: "small", Options: []Option{{ID: "", Name: "Embedded"}}}}},
		{name: "one available default", templates: []Template{{Layout: "small", Default: "sharp", Options: []Option{
			{ID: "", Name: "Embedded", DisabledReason: "Unavailable for this configuration"}, {ID: "sharp", Name: "Sharp"},
		}}}, want: "sharp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := setupInput()
			input.Profiles[0].Templates = tc.templates
			m := NewModel(context.Background(), input, readyPreview)
			press(m, tea.KeyEnter)
			press(m, 'n')
			press(m, tea.KeyEnter)
			if view := ansi.Strip(m.View().Content); strings.Contains(view, "Templates") {
				t.Fatalf("tab for a nonexistent choice: %s", view)
			}
			press(m, 'n')
			if m.stage != stageSoftware {
				t.Fatalf("did not skip fixed templates: %v", m.stage)
			}
			if len(tc.templates) > 0 {
				if value, ok := m.choices().Profiles[0].Templates["small"]; !ok || value != tc.want {
					t.Fatalf("fixed template lost: %+v", m.choices())
				}
			}
			press(m, tea.KeyEsc)
			if m.stage != stageProfile {
				t.Fatalf("Back entered a skipped template screen: %v", m.stage)
			}
		})
	}
}

func TestSkippedTemplateNavigationAcrossModesAndProfileChanges(t *testing.T) {
	input := setupInput()
	input.Profiles[0].Templates = nil
	input.Profiles = append(input.Profiles, Profile{Option: Option{ID: "other", Name: "Other"}, Mode: "local", Templates: setupInput().Profiles[0].Templates})
	m := NewModel(context.Background(), input, readyPreview)
	press(m, tea.KeyEnter) // Local
	press(m, tea.KeyDown)
	press(m, tea.KeyEnter) // Utility
	press(m, 'n')
	press(m, tea.KeyEnter) // Compact; no alternatives
	press(m, 'n')
	if m.stage != stageProfile || m.currentMode() != "utility" {
		t.Fatal("fixed local templates did not skip to the utility profile")
	}
	press(m, tea.KeyEnter)
	press(m, 'n')
	press(m, tea.KeyEsc)
	if m.stage != stageProfile || m.currentMode() != "utility" {
		t.Fatal("Back from software did not skip fixed utility templates")
	}
	press(m, tea.KeyEsc)
	if m.stage != stageProfile || m.currentMode() != "local" {
		t.Fatal("Back across modes entered a skipped template screen")
	}
	press(m, tea.KeyDown)
	press(m, tea.KeyEnter) // Other profile has alternatives.
	press(m, 'n')
	if m.stage != stageTemplates || !strings.Contains(ansi.Strip(m.View().Content), "• Templates") {
		t.Fatal("changed profile did not add its template screen and tab")
	}
	press(m, tea.KeyDown)
	press(m, tea.KeyEnter) // Sharp
	press(m, tea.KeyEsc)
	press(m, tea.KeyEnter) // Compact again
	press(m, tea.KeyDown)
	press(m, tea.KeySpace) // Stop installing Other; a default change retains it.
	press(m, 'n')
	if m.stage != stageProfile || m.currentMode() != "utility" {
		t.Fatal("returning to a fixed profile retained the template screen")
	}
}

func TestMixedProfileShowsOnlyTemplatesWithAlternatives(t *testing.T) {
	input := setupInput()
	input.Profiles[0].Templates = append([]Template{{Layout: "fixed", Name: "Fixed template", Default: "fixed-patch", Options: []Option{{ID: "fixed-patch", Name: "Only choice"}}}}, input.Profiles[0].Templates...)
	m := NewModel(context.Background(), input, readyPreview)
	press(m, tea.KeyEnter)
	press(m, 'n')
	press(m, tea.KeyEnter)
	press(m, 'n')
	view := ansi.Strip(m.View().Content)
	if strings.Contains(view, "Fixed template") || !strings.Contains(view, "Chat template") {
		t.Fatalf("fixed template obscures alternatives: %s", view)
	}
	press(m, tea.KeyDown)
	press(m, tea.KeyEnter)
	choices := m.choices().Profiles[0].Templates
	if choices["fixed"] != "fixed-patch" || choices["small"] != "sharp" {
		t.Fatalf("visible template rows changed the wrong model: %+v", choices)
	}
}

func TestNextButtonRequiresExplicitChoices(t *testing.T) {
	m := NewModel(context.Background(), setupInput(), readyPreview)
	press(m, tea.KeyUp) // Focus Next without choosing a mode.
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Next →") {
		t.Fatalf("Next button is absent: %s", view)
	}
	press(m, tea.KeyEnter)
	if m.stage != stageModes || len(m.choices().Profiles) != 0 {
		t.Fatal("Next implicitly selected a mode")
	}
	press(m, tea.KeyDown)
	press(m, tea.KeyEnter) // Explicit Local
	press(m, tea.KeyUp)
	press(m, tea.KeyEnter) // Next to profiles
	if m.stage != stageProfile {
		t.Fatal("Next did not advance after explicit mode choice")
	}
	press(m, tea.KeyUp)
	press(m, tea.KeyEnter)
	if m.stage != stageProfile || m.profiles["local"] != "" {
		t.Fatal("Next implicitly selected a profile")
	}
	press(m, tea.KeyDown)
	press(m, tea.KeyEnter)
	press(m, tea.KeyUp)
	press(m, tea.KeyEnter) // Next to templates
	press(m, tea.KeyUp)
	press(m, tea.KeySpace) // Next accepts proposed template defaults.
	if m.stage != stageSoftware {
		t.Fatal("Next did not advance from templates")
	}
	press(m, tea.KeyUp)
	press(m, tea.KeyEnter)
	if m.stage != stageSoftware || m.software != "" || m.decision.Action != "" {
		t.Fatal("Next implicitly selected software or committed a decision")
	}
}

func TestReviewErrorPreventsSaveAndPrepare(t *testing.T) {
	m := NewModel(context.Background(), setupInput(), func(context.Context, Choices) (Review, error) {
		return Review{}, errors.New("upstream unavailable")
	})
	press(m, tea.KeyEnter)
	press(m, tea.KeyTab)
	press(m, tea.KeyEnter)
	press(m, tea.KeyTab)
	press(m, tea.KeyTab)
	press(m, tea.KeyEnter)
	cmd := press(m, tea.KeyTab)
	m.Update(cmd())
	press(m, tea.KeyRight) // prepare
	if cmd := press(m, tea.KeyEnter); cmd != nil || m.decision.Action != "" {
		t.Fatalf("prepared despite error: decision=%+v", m.decision)
	}
	press(m, tea.KeyLeft)
	if cmd := press(m, tea.KeyEnter); cmd != nil || m.decision.Action != "" {
		t.Fatalf("saved despite error: decision=%+v", m.decision)
	}
}

func TestDecisionCarriesAcceptedReviewToken(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cursor int
		action string
	}{
		{name: "save", cursor: 0, action: "save"},
		{name: "prepare", cursor: 1, action: "prepare"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewModel(context.Background(), setupInput(), readyPreview)
			press(m, tea.KeyEnter)
			press(m, tea.KeyTab)
			press(m, tea.KeyEnter)
			press(m, tea.KeyTab)
			press(m, tea.KeyTab)
			press(m, tea.KeyEnter)
			cmd := press(m, tea.KeyTab)
			m.Update(cmd())
			for i := 0; i < tc.cursor; i++ {
				press(m, tea.KeyRight)
			}
			if cmd := press(m, tea.KeyEnter); cmd == nil {
				t.Fatal("decision did not quit")
			}
			if m.decision.Action != tc.action || m.decision.ReviewToken != "review-1" {
				t.Fatalf("decision = %+v", m.decision)
			}
		})
	}
}

func TestBackAndChangedChoiceRejectStalePreview(t *testing.T) {
	m := NewModel(context.Background(), setupInput(), func(_ context.Context, choices Choices) (Review, error) {
		return Review{CanPrepare: true, Token: choices.Software}, nil
	})
	press(m, tea.KeyEnter)
	press(m, tea.KeyTab)
	press(m, tea.KeyEnter)
	press(m, tea.KeyTab)
	press(m, tea.KeyTab)
	press(m, tea.KeyEnter) // recorded
	old := press(m, tea.KeyTab)
	press(m, tea.KeyEsc) // leave review before the old callback returns
	press(m, tea.KeyDown)
	press(m, tea.KeyEnter) // latest
	current := press(m, tea.KeyTab)
	m.Update(current())
	m.Update(old())
	if m.review.Token != "latest" {
		t.Fatalf("stale callback replaced review: %+v", m.review)
	}
	press(m, tea.KeyEnter)
	if m.decision.ReviewToken != "latest" || m.decision.Choices.Software != "latest" {
		t.Fatalf("accepted stale plan: %+v", m.decision)
	}
}

func TestTestedSoftwareVisibleButDisabled(t *testing.T) {
	in := setupInput()
	in.Profiles[0].SoftwareUnavailable = map[string]string{"tested": "No tested release recorded"}
	m := NewModel(context.Background(), in, readyPreview)
	press(m, tea.KeyEnter) // local
	press(m, tea.KeyTab)
	press(m, tea.KeyEnter) // compact
	press(m, tea.KeyTab)
	press(m, tea.KeyTab) // software
	press(m, tea.KeyDown)
	press(m, tea.KeyDown) // tested
	if view := m.View().Content; !strings.Contains(view, "Tested") || !strings.Contains(view, "unavailable: local:") {
		t.Fatalf("disabled tested option is not visible: %q", view)
	}
	if reason := m.softwareOption(softwareOptions[2]).DisabledReason; reason != "local: No tested release recorded" {
		t.Fatalf("tested reason = %q", reason)
	}
	press(m, tea.KeyEnter)
	if m.software != "" || !strings.Contains(m.notice, "No tested release recorded") {
		t.Fatalf("disabled tested software selected: %q %q", m.software, m.notice)
	}
	press(m, tea.KeyTab)
	if m.stage != stageSoftware {
		t.Fatalf("advanced after disabled software: %v", m.stage)
	}
}

func TestChangedProfileInvalidatesPreviousSoftwareChoice(t *testing.T) {
	in := setupInput()
	in.Profiles = append(in.Profiles, Profile{Option: Option{ID: "alternate", Name: "Alternate local"}, Mode: "local", SoftwareUnavailable: map[string]string{"tested": "Alternate has no tested release"}})
	m := NewModel(context.Background(), in, readyPreview)
	press(m, tea.KeyEnter) // local
	press(m, tea.KeyTab)
	press(m, tea.KeyEnter) // compact
	press(m, tea.KeyTab)
	press(m, tea.KeyTab) // software
	press(m, tea.KeyDown)
	press(m, tea.KeyDown)
	press(m, tea.KeyEnter) // tested
	press(m, tea.KeyEsc)   // templates
	press(m, tea.KeyEsc)   // profile
	press(m, tea.KeyDown)
	press(m, tea.KeyEnter) // alternate profile
	press(m, tea.KeyUp)
	press(m, tea.KeySpace) // Remove the previously installed compact profile.
	press(m, tea.KeyTab)   // No template alternatives: directly to software.
	if m.software != "tested" {
		t.Fatalf("previous explicit choice lost: %q", m.software)
	}
	press(m, tea.KeyTab)
	if m.stage != stageSoftware || !strings.Contains(m.notice, "Alternate has no tested release") {
		t.Fatalf("previous choice remained valid: stage=%v notice=%q", m.stage, m.notice)
	}
	press(m, tea.KeyEnter) // recorded is available
	if cmd := press(m, tea.KeyTab); cmd == nil || m.stage != stageReview {
		t.Fatal("available replacement did not advance")
	}
}

func TestSoftwareRestrictionsCombineAcrossModes(t *testing.T) {
	in := setupInput()
	in.Profiles[0].SoftwareUnavailable = map[string]string{"latest": "local release cannot be resolved"}
	in.Profiles[1].SoftwareUnavailable = map[string]string{"latest": "utility release cannot be resolved", "tested": "utility has no tested release"}
	m := NewModel(context.Background(), in, readyPreview)
	press(m, tea.KeyEnter) // local
	press(m, tea.KeyDown)
	press(m, tea.KeyEnter) // utility
	press(m, tea.KeyTab)
	press(m, tea.KeyEnter) // compact
	press(m, tea.KeyTab)
	press(m, tea.KeyTab) // utility profile
	press(m, tea.KeyEnter)
	press(m, tea.KeyTab)  // software
	press(m, tea.KeyDown) // latest
	view := m.View().Content
	if !strings.Contains(view, "unavailable:") || !strings.Contains(view, "local:") || !strings.Contains(view, "utility:") {
		t.Fatalf("combined restrictions missing: %q", view)
	}
	if reason := m.softwareOption(softwareOptions[1]).DisabledReason; reason != "local: local release cannot be resolved; utility: utility release cannot be resolved" {
		t.Fatalf("combined reason = %q", reason)
	}
	press(m, tea.KeyEnter)
	if m.software != "" {
		t.Fatalf("latest selected despite combined refusal: %q", m.software)
	}
	press(m, tea.KeyDown) // tested
	press(m, tea.KeyEnter)
	if m.software != "" || !strings.Contains(m.notice, "utility has no tested release") {
		t.Fatalf("tested selected despite utility refusal: %q %q", m.software, m.notice)
	}
}

func TestLongReviewOpensAtTopAndKeepsExplicitPageScroll(t *testing.T) {
	review := Review{Token: "review-1", CanPrepare: true, Sections: []Section{{Title: "Downloads"}}}
	for i := 0; i < 30; i++ {
		review.Sections[0].Lines = append(review.Sections[0].Lines, "Review detail "+strings.Repeat("long wrapped description ", 3))
	}
	m := NewModel(context.Background(), setupInput(), func(context.Context, Choices) (Review, error) { return review, nil })
	m.selected["local"] = true
	m.profiles["local"] = "compact"
	m.software = "recorded"
	m.stage = stageSoftware
	m.Update(tea.WindowSizeMsg{Width: 36, Height: 10})
	m.viewport.SetContent(strings.Repeat("previous screen\n", 50))
	m.viewport.SetYOffset(20)
	cmd := press(m, tea.KeyTab)
	if cmd == nil {
		t.Fatal("review preview did not start")
	}
	m.Update(cmd())
	first := m.View().Content
	if m.viewport.YOffset() != 0 || !strings.Contains(first, "Review") || !strings.Contains(first, "> Save configuration") {
		t.Fatalf("review did not open at top: offset=%d view=%q", m.viewport.YOffset(), first)
	}
	if !strings.Contains(first, "↑↓ scroll") {
		t.Fatalf("scroll hint absent: %q", first)
	}
	press(m, tea.KeyPgDown)
	pageDownOffset := m.viewport.YOffset()
	if pageDownOffset == 0 {
		t.Fatal("PgDown did not scroll")
	}
	m.View()
	if m.viewport.YOffset() != pageDownOffset {
		t.Fatalf("View snapped PgDown from %d to %d", pageDownOffset, m.viewport.YOffset())
	}
	press(m, tea.KeyPgUp)
	pageUpOffset := m.viewport.YOffset()
	if pageUpOffset >= pageDownOffset {
		t.Fatalf("PgUp did not scroll: %d to %d", pageDownOffset, pageUpOffset)
	}
	m.View()
	if m.viewport.YOffset() != pageUpOffset {
		t.Fatalf("View snapped PgUp from %d to %d", pageUpOffset, m.viewport.YOffset())
	}

	press(m, tea.KeyRight) // actions stay outside the scrolled content
	actionView := m.View().Content
	if !strings.Contains(actionView, "> Prepare installation") {
		t.Fatalf("focused action not visible: %q", actionView)
	}
	if m.viewport.YOffset() != pageUpOffset {
		t.Fatalf("action navigation moved review content: %d to %d", pageUpOffset, m.viewport.YOffset())
	}
	press(m, tea.KeyPgUp)
	manualOffset := m.viewport.YOffset()
	m.View()
	if m.viewport.YOffset() != manualOffset {
		t.Fatalf("manual scroll snapped to action: %d to %d", manualOffset, m.viewport.YOffset())
	}
	press(m, tea.KeyRight) // focus Back
	if actionView = m.View().Content; !strings.Contains(actionView, "> Back") {
		t.Fatalf("Back action not visible: %q", actionView)
	}
}

func TestSelectionCursorStaysVisibleWhenRowsWrap(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stage  stage
		cursor int
		want   string
	}{
		{name: "profile", stage: stageProfile, cursor: 0, want: "> [x] (*) Compact general"},
		{name: "template", stage: stageTemplates, cursor: 1, want: "> ( ) Sharp"},
		{name: "software", stage: stageSoftware, cursor: 2, want: "> ( ) Tested"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewModel(context.Background(), setupInput(), readyPreview)
			m.selected["local"] = true
			m.profiles["local"] = "compact"
			m.stage = tc.stage
			m.Update(tea.WindowSizeMsg{Width: 36, Height: 10})
			for i := 0; i < tc.cursor; i++ {
				press(m, tea.KeyDown)
			}

			if view := ansi.Strip(m.View().Content); !strings.Contains(view, tc.want) {
				t.Fatalf("focused option %q is outside viewport: %q", tc.want, view)
			}
		})
	}
}

func TestOutstandingPreviewIsCanceledOnBackRetryAndQuit(t *testing.T) {
	for _, action := range []string{"back", "retry", "quit"} {
		t.Run(action, func(t *testing.T) {
			started := make(chan context.Context, 1)
			m := NewModel(context.Background(), setupInput(), func(ctx context.Context, _ Choices) (Review, error) {
				started <- ctx
				<-ctx.Done()
				return Review{}, ctx.Err()
			})
			m.stage = stageReview
			_, first := m.startPreview()
			finished := make(chan tea.Msg, 1)
			go func() { finished <- first() }()
			var active context.Context
			select {
			case active = <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("preview did not start")
			}
			switch action {
			case "back":
				press(m, tea.KeyEsc)
			case "retry":
				if retry := press(m, 'r'); retry == nil {
					t.Fatal("retry did not start another preview")
				}
			case "quit":
				press(m, 'q')
			}
			select {
			case <-active.Done():
			case <-time.After(2 * time.Second):
				t.Fatal("old preview context remained active")
			}
			var result tea.Msg
			select {
			case result = <-finished:
			case <-time.After(2 * time.Second):
				t.Fatal("canceled preview did not finish")
			}
			m.Update(result)
			if m.reviewErr != nil || m.review.Token != "" {
				t.Fatalf("stale completion changed review: %+v %v", m.review, m.reviewErr)
			}
			if action == "retry" {
				if !m.loading || m.stage != stageReview {
					t.Fatalf("stale completion stopped retry: loading=%v stage=%v", m.loading, m.stage)
				}
				press(m, 'q') // cancel the second request created by retry
			}
		})
	}
}

func TestCancelResizeAndStalePreview(t *testing.T) {
	m := NewModel(context.Background(), setupInput(), readyPreview)
	m.Update(tea.WindowSizeMsg{Width: 42, Height: 12})
	if got := m.View().Content; !strings.Contains(got, "Apple Silicon") {
		t.Fatalf("machine summary absent: %q", got)
	}
	press(m, tea.KeyEsc)
	if m.decision.Action != "cancel" {
		t.Fatalf("Esc decision = %+v", m.decision)
	}

	m = NewModel(context.Background(), setupInput(), readyPreview)
	press(m, tea.KeyEnter)
	press(m, tea.KeyTab)
	press(m, tea.KeyEnter)
	press(m, tea.KeyTab)
	press(m, tea.KeyTab)
	press(m, tea.KeyEnter)
	cmd := press(m, tea.KeyTab)
	press(m, tea.KeyRight)
	press(m, tea.KeyRight)
	press(m, tea.KeyEnter) // Back action on review
	m.Update(cmd())
	if m.stage != stageSoftware || m.loading {
		t.Fatalf("stale preview changed state: stage %v loading %v", m.stage, m.loading)
	}
}
