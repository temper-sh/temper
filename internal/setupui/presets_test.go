package setupui

import (
	"context"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/setup"
)

func presetEditor(t *testing.T, ids ...string) *PresetModel {
	t.Helper()
	raw, err := os.ReadFile("../testfixture/testdata/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	c := setup.EmptyConfiguration()
	input := PresetInput{Configuration: c, NewConfiguration: true, Groups: d.Groups()}
	if len(ids) == 0 {
		ids = []string{"qwen3.8-27b-q4xl-splash", "qwen3.5-4b-q4km-off"}
	}
	for _, id := range ids {
		p := d.Presets[id]
		lock, err := catalog.CompilePreset(d, id, 0, 0, d.Runtime.Router.Target)
		if err != nil {
			t.Fatal(err)
		}
		group, section := d.Placement(p.Presentation)
		option := PresetOption{ID: id, Name: p.Presentation.DisplayName, Group: group.ID, Lock: lock}
		if section != nil {
			option.Section = section.ID
		}
		input.Presets = append(input.Presets, option)
	}
	return NewPresetModel(context.Background(), input, func(context.Context, setup.Configuration) (setup.Plan, error) { return setup.Plan{}, nil })
}
func editorKey(m *PresetModel, key rune) {
	_, _ = m.Update(tea.KeyPressMsg{Code: key, Text: string(key)})
}

func TestPresetTabsAndBackNavigationKeepOneSelection(t *testing.T) {
	m := presetEditor(t)
	editorKey(m, ' ')
	editorKey(m, 'o')
	m.cursor = 0
	editorKey(m, ' ')
	editorKey(m, 'g')
	if len(m.draft.Presets) != 2 {
		t.Fatal("tab switch discarded selection")
	}
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if len(m.draft.Presets) != 2 || m.page != 0 {
		t.Fatal("back navigation discarded presets")
	}
	if len(m.input.Configuration.Presets) != 0 {
		t.Fatal("revocable draft mutated caller configuration")
	}
}

func TestFirstSetupSuggestsOneLayoutWithSelectedPresetsAndNoStartup(t *testing.T) {
	m := presetEditor(t)
	editorKey(m, ' ')
	editorKey(m, 'o')
	m.cursor = 0
	editorKey(m, ' ')
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	l, ok := m.draft.Layouts["default"]
	if !ok || len(m.draft.Layouts) != 1 || l.Name != "Default" {
		t.Fatalf("expected one editable Default layout: %+v", m.draft.Layouts)
	}
	if !slices.Equal(l.Presets, []string{"qwen3.5-4b-q4km-off", "qwen3.8-27b-q4xl-splash"}) || len(l.Startup) != 0 || l.Default != "" {
		t.Fatalf("selected presets should be included without startup or default routing: %+v", l)
	}
	if len(m.input.Configuration.Layouts) != 0 {
		t.Fatal("suggestion changed the caller's configuration")
	}
}

func TestReturningToLayoutsPreservesEditsToTheSuggestion(t *testing.T) {
	m := presetEditor(t)
	editorKey(m, ' ')
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // Open Default.
	editorKey(m, ' ')                             // Remove its suggested membership.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})   // Layouts.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})   // Presets.
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(m.draft.Layouts["default"].Presets) != 0 {
		t.Fatal("returning to Layouts replaced an explicit membership edit")
	}
	editorKey(m, 'x')
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(m.draft.Layouts) != 0 {
		t.Fatal("returning to Layouts recreated a removed suggestion")
	}
}

func TestExistingConfigurationKeepsItsLayoutsWhenPresetsChange(t *testing.T) {
	input := presetEditor(t).input
	input.NewConfiguration = false
	input.Configuration.Layouts = map[string]setup.Layout{
		"local":   {Name: "Local", Presets: []string{}, Startup: []string{}, IdleSeconds: 1800},
		"utility": {Name: "Utility", Presets: []string{}, Startup: []string{}, IdleSeconds: 1800},
		"default": {Name: "My own layout", Presets: []string{}, Startup: []string{}, IdleSeconds: 60},
	}
	m := NewPresetModel(context.Background(), input, nil)
	editorKey(m, ' ')
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !reflect.DeepEqual(m.draft.Layouts, input.Configuration.Layouts) {
		t.Fatalf("saved layouts were replaced or populated: %+v", m.draft.Layouts)
	}
}

func TestLayoutInclusionStartupAndDefaultAreIndependent(t *testing.T) {
	m := presetEditor(t)
	editorKey(m, ' ')
	m.advance()
	m.layout = "default"
	editorKey(m, 'd')
	l := m.draft.Layouts["default"]
	if len(l.Startup) != 0 || l.Default == "" {
		t.Fatal("default implied preload")
	}
	editorKey(m, 's')
	editorKey(m, 'd')
	l = m.draft.Layouts["default"]
	if len(l.Startup) != 1 || l.Default != "" {
		t.Fatal("startup requires default")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	editorKey(m, ' ')
	if err := m.draft.Validate(); err == nil || !strings.Contains(m.notice, "explicitly") {
		t.Fatal("membership removal silently corrected startup")
	}
	editorKey(m, 's')
	if err := m.draft.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestStarterLayoutsCanBeRemovedAndNamedCompositionsCreated(t *testing.T) {
	m := presetEditor(t)
	m.advance()
	editorKey(m, 'x')
	if len(m.draft.Layouts) != 0 {
		t.Fatal("starters became required types")
	}
	m.field = "new"
	m.text.SetValue("Editing desk")
	m.acceptEdit()
	if m.draft.Layouts["editing-desk"].Name != "Editing desk" {
		t.Fatal("custom composition missing")
	}
	m.field = "rename"
	m.text.SetValue("Review desk")
	m.acceptEdit()
	if m.draft.Layouts["editing-desk"].Name != "Review desk" {
		t.Fatal("rename lost stable identity")
	}
}

func TestSavedAliasCustomizesItsExactSourceRecord(t *testing.T) {
	m := presetEditor(t)
	p := m.input.Presets[1]
	p.ID = "my-helper"
	p.Name = "My helper"
	m.input.Presets = []PresetOption{p}
	m.group = "other"
	m.draft.Presets[p.ID] = setup.Preset{Name: p.Name, Lock: p.Lock}
	m.field = "context"
	m.text.SetValue("8192")
	m.acceptEdit()
	saved := m.draft.Presets[p.ID]
	_, record := presetRecord(saved.Lock)
	if record.Window() != 8192 || !strings.Contains(saved.Name, "customized") {
		t.Fatal("alias edit lost source settings", m.notice)
	}
	if _, changed := m.draft.Presets["qwen3.5-4b-q4km-off"]; changed {
		t.Fatal("alias edit replaced catalog choice")
	}
}

func TestPresetWithoutALockShowsWhyAndCannotBeSelected(t *testing.T) {
	m := presetEditor(t, "qwen3.5-4b-q4km-off")
	m.input.Presets = append(m.input.Presets, PresetOption{ID: "future", Name: "Future model", Description: "A later model.", Group: m.input.Presets[0].Group, Unavailable: "needs Temper 0.2.0 or newer"})
	m.group = m.input.Presets[0].Group
	m.cursor = slices.IndexFunc(m.visible(), func(p PresetOption) bool { return p.ID == "future" })
	editorKey(m, ' ')
	if _, ok := m.draft.Presets["future"]; ok || !strings.Contains(m.notice, "needs Temper 0.2.0") {
		t.Fatalf("selected a preset without a lock: %q", m.notice)
	}
	editorKey(m, 'c')
	if m.field != "" || !strings.Contains(m.notice, "needs Temper 0.2.0") {
		t.Fatalf("customized a preset without a lock: %q", m.notice)
	}
	if view := m.View().Content; !strings.Contains(view, "needs Temper 0.2.0") || strings.Contains(view, "Context 0") {
		t.Fatalf("card did not explain or showed empty settings:\n%s", view)
	}
}
