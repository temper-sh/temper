package setupui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/setup"
)

func retiredEditor(t *testing.T) *PresetModel {
	t.Helper()
	base := presetEditor(t)
	input := base.input
	old := &input.Presets[0]
	old.Group = "retired"
	old.Retirement = &catalog.Retirement{RetiredOn: "2026-09-13", Reason: "Lower-memory replacement", Successor: input.Presets[1].ID}
	old.Successor = input.Presets[1].ID
	input.NewConfiguration = false
	input.Configuration.Presets[old.ID] = setup.Preset{Name: old.Name, Lock: old.Lock, Choices: setup.Choices{ContextWindow: 8192}}
	saved := input.Configuration.Presets[old.ID]
	saved.UserPreset = old.Lock.Preset
	input.Configuration.Presets[old.ID] = saved
	input.Configuration.Layouts["work"] = setup.Layout{Name: "Work", Presets: []string{old.ID}, Startup: []string{old.ID}, Default: old.ID, IdleSeconds: 60}
	m := NewPresetModel(context.Background(), input, func(_ context.Context, c setup.Configuration) (setup.Plan, error) {
		return setup.Plan{Configuration: &c, CanPrepare: true}, nil
	})
	m.selectGroup("retired")
	return m
}

func TestStagedRetirementKeepsItsSuccessorSelected(t *testing.T) {
	for _, assigned := range []bool{true, false} {
		t.Run(fmt.Sprint("assigned=", assigned), func(t *testing.T) {
			m := retiredEditor(t)
			if !assigned {
				m.draft.Layouts = map[string]setup.Layout{}
				m.input.Configuration.Layouts = map[string]setup.Layout{}
			}
			target := m.input.Presets[1]
			m.activateShortcut("replace-retired")
			m.activateShortcut("apply")
			m.selectGroup(target.Group)
			m.cursor = 0
			if p, ok := m.selectedOption(); !ok || p.ID != target.ID {
				t.Fatal("fixture did not select successor", p.ID)
			}
			m.toggleMember()
			if _, selected := m.draft.Presets[target.ID]; !selected || !strings.Contains(m.notice, "staged replacement") {
				t.Fatal("successor could be deselected", m.notice)
			}
			m.page = presetReviewPage
			m.Update(m.startPreview()())
			if m.reviewErr != nil || !m.reviewReady || len(m.retirements) != 1 {
				t.Fatal("replacement no longer reviewable", m.reviewErr)
			}
		})
	}
}

func TestInvalidDraftDoesNotReachPreview(t *testing.T) {
	m := retiredEditor(t)
	delete(m.draft.Presets, m.input.Presets[0].ID)
	m.preview = func(context.Context, setup.Configuration) (setup.Plan, error) {
		t.Fatal("invalid draft reached preview")
		return setup.Plan{}, nil
	}
	m.page = presetReviewPage
	m.Update(m.startPreview()())
	if m.reviewErr == nil || m.actionAvailable(0) {
		t.Fatal("invalid draft could be applied", m.reviewErr)
	}
}

func TestRetiredTabRequiresAnExplicitlyRetiredSavedSelection(t *testing.T) {
	m := retiredEditor(t)
	if m.groups()[len(m.groups())-1].ID != "retired" {
		t.Fatal(m.groups())
	}
	delete(m.input.Configuration.Presets, m.input.Presets[0].ID)
	m.input.Groups = append(m.input.Groups, catalog.Group{ID: "retired", DisplayName: "Retired"})
	for _, g := range m.groups() {
		if g.ID == "retired" {
			t.Fatal("retired tab offered for an uninstalled preset")
		}
	}
	m = retiredEditor(t)
	m.input.Presets[0].Retirement = nil
	for _, g := range m.groups() {
		if g.ID == "retired" {
			t.Fatal("absence alone created retirement")
		}
	}
}

func TestRetirementKeepAndCancelDoNotChangeSelectionOrWeightConsent(t *testing.T) {
	m := retiredEditor(t)
	before, _ := m.draft.Bytes()
	m.activateShortcut("keep-retired")
	if after, _ := m.draft.Bytes(); string(after) != string(before) {
		t.Fatal("Keep using changed selection")
	}
	m.activateShortcut("replace-retired")
	// A card covers one preset; deletion consent belongs to the listed files.
	m.activateShortcut("retired-weights")
	m.activateShortcut("cancel-edit")
	if m.deleteRetiredWeights || len(m.retirements) != 0 {
		t.Fatal("cancelled prompt granted consent")
	}
	if after, _ := m.draft.Bytes(); string(after) != string(before) {
		t.Fatal("cancelled prompt changed selection")
	}
}

func TestRetirementReplacementUsesDefaultsAndUpdatesLayoutReferences(t *testing.T) {
	m := retiredEditor(t)
	old, target := m.input.Presets[0], m.input.Presets[1]
	m.activateShortcut("replace-retired")
	for _, width := range []int{40, 100} {
		m.width = width
		m.height = 40
		text := ansi.Strip(m.View().Content)
		if !strings.Contains(strings.Join(strings.Fields(text), " "), "Retired 2026-09-13") {
			t.Fatalf("missing retirement at %d: %s", width, text)
		}
		body := screenBody{focusStart: -1}
		m.retirementContent(&body, width)
		full := strings.Join(strings.Fields(ansi.Strip(strings.Join(body.blocks, "\n"))), " ")
		for _, want := range []string{"successor defaults", "authored", "Affected layouts", "Shared HF files stay"} {
			if !strings.Contains(full, want) {
				t.Fatal("scrollable review omitted", want, full)
			}
		}
	}
	m.activateShortcut("apply")
	if _, ok := m.draft.Presets[old.ID]; ok {
		t.Fatal("retired selection retained in replacement draft")
	}
	if m.draft.Presets[target.ID].Choices != (setup.Choices{}) {
		t.Fatal("silently transferred custom choices")
	}
	l := m.draft.Layouts["work"]
	if l.Default != target.ID || len(l.Presets) != 1 || l.Presets[0] != target.ID || l.Startup[0] != target.ID {
		t.Fatal(l)
	}
	if len(m.retirements) != 1 || m.deleteRetiredWeights {
		t.Fatal("lost the staged retirement or granted deletion before review", m.retirements)
	}
	m.reviewReady = true
	m.plan.CanPrepare = true
	if m.actionAvailable(1) || !m.actionAvailable(0) {
		t.Fatal("replacement could be saved without preparing")
	}
	m.action = 1
	m.finish()
	if !strings.Contains(m.notice, "require preparation") {
		t.Fatal("disabled Save implied a pending check", m.notice)
	}
	m.activateShortcut("c")
	if m.field != "" {
		t.Fatal("staged retired preset could be reintroduced through Context")
	}
	if _, ok := m.input.Configuration.Presets[old.ID]; !ok {
		t.Fatal("editing modified the caller's installed configuration")
	}
}

func TestRetirementUnavailableSuccessorCanBeKeptOrRemoved(t *testing.T) {
	m := retiredEditor(t)
	m.input.Presets[1].Unavailable = "requires a newer Mac"
	m.activateShortcut("replace-retired")
	if m.field != "" || !strings.Contains(m.notice, "newer Mac") {
		t.Fatal(m.field, m.notice)
	}
	m.activateShortcut("remove-retired")
	m.activateShortcut("apply")
	l := m.draft.Layouts["work"]
	if len(l.Presets)+len(l.Startup) != 0 || l.Default != "" {
		t.Fatal(l)
	}
	if len(m.retirements) != 1 || m.retirements[0].Successor != "" {
		t.Fatal(m.retirements)
	}
}

func TestRetiredCardsDoNotRepeatAHistoricalRecommendation(t *testing.T) {
	m := retiredEditor(t)
	m.input.Presets[0].Description = "Our top choice; battle-tested default"
	text := ansi.Strip(m.View().Content)
	if strings.Contains(text, "top choice") || strings.Contains(text, "battle-tested") || !strings.Contains(text, "Lower-memory replacement") {
		t.Fatal(text)
	}
}

func TestRetirementReviewUsesTheExistingSuccessorsActualSettings(t *testing.T) {
	m := retiredEditor(t)
	target := m.input.Presets[1]
	_, record := presetRecord(target.Lock)
	lock, err := catalog.ConfigureExecution(target.Lock, target.Lock.Preset, catalog.ExecutionSettings{ContextWindowTokens: 8192, Compute: record.Compute})
	if err != nil {
		t.Fatal(err)
	}
	m.draft.Presets[target.ID] = setup.Preset{Name: "My existing choice", Choices: setup.Choices{ContextWindow: 8192}, Lock: lock}
	m.activateShortcut("replace-retired")
	body := screenBody{focusStart: -1}
	m.retirementContent(&body, 100)
	text := ansi.Strip(strings.Join(body.blocks, "\n"))
	if !strings.Contains(text, "→ 8192 tokens") || !strings.Contains(text, "My existing choice") {
		t.Fatal(text)
	}
	m.activateShortcut("apply")
	if got := m.draft.Presets[target.ID]; got.Name != "My existing choice" || got.Choices.ContextWindow != 8192 || !catalog.SameExecution(got.Lock, lock) {
		t.Fatal("existing successor was overwritten")
	}
}

func TestRetirementReviewNamesSharedFilesBeforeDeletionConsent(t *testing.T) {
	m := retiredEditor(t)
	files := []SharedFile{{Name: "org/model/model-Q4.gguf", Bytes: 3 << 30}, {Name: "org/model/mmproj.gguf", Bytes: 1 << 30}}
	listed := map[string]bool{}
	m.input.RetiredFiles = func(_ setup.Configuration, retired map[string]setup.Preset) ([]SharedFile, error) {
		for id := range retired {
			listed[id] = true
		}
		return files, nil
	}
	old := m.input.Presets[0].ID
	m.activateShortcut("remove-retired")
	m.activateShortcut("apply")
	m.page = presetReviewPage
	m.Update(m.startPreview()())
	if !listed[old] || len(listed) != 1 {
		t.Fatal("review listed files for other selections", listed)
	}
	review := func() string {
		body := screenBody{focusStart: -1}
		m.reviewSummary(&body, 100)
		return ansi.Strip(strings.Join(body.blocks, "\n"))
	}
	for _, want := range []string{"org/model/model-Q4.gguf · 3.00 GiB", "org/model/mmproj.gguf · 1.00 GiB", "other applications may use them", "Keep these files"} {
		if !strings.Contains(review(), want) {
			t.Fatalf("review omitted %q:\n%s", want, review())
		}
	}
	editorKey(m, 'w')
	if !m.deleteRetiredWeights || !strings.Contains(review(), "DELETE these files") {
		t.Fatal("w did not grant deletion of the listed files")
	}
	// Another check may list different files; consent does not carry over.
	m.Update(m.startPreview()())
	if m.deleteRetiredWeights {
		t.Fatal("deletion consent survived a new review")
	}
	editorKey(m, 'w')
	m.reviewReady, m.plan.CanPrepare, m.action = true, true, 0
	m.finish()
	if !m.Decision.DeleteRetiredWeights || len(m.Decision.Retirements) != 1 {
		t.Fatal(m.Decision)
	}
	files = nil
	m.Update(m.startPreview()())
	editorKey(m, 'w')
	if m.deleteRetiredWeights || !strings.Contains(review(), "No unused shared HF files") {
		t.Fatal("offered deletion without files to name")
	}
}
