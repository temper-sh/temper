package setupui

import (
	"context"
	"encoding/json"
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

func TestFirstSetupCreatesDefaultWithSelectedPresetsAndNoStartup(t *testing.T) {
	m := presetEditor(t)
	editorKey(m, ' ')
	editorKey(m, 'o')
	m.cursor = 0
	editorKey(m, ' ')
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	l, ok := m.draft.Layouts["default"]
	if !ok || len(m.draft.Layouts) != 1 || l.Name != "Default" {
		t.Fatalf("expected one Default layout: %+v", m.draft.Layouts)
	}
	if !slices.Equal(l.Presets, []string{"qwen3.5-4b-q4km-off", "qwen3.8-27b-q4xl-splash"}) || len(l.Startup) != 0 || l.Default != "" {
		t.Fatalf("selected presets should be included without startup or default routing: %+v", l)
	}
	if len(m.input.Configuration.Layouts) != 0 {
		t.Fatal("first setup changed the caller's configuration")
	}
}

func TestFirstSetupRebuildsDefaultAfterChangingSelection(t *testing.T) {
	m := presetEditor(t)
	editorKey(m, ' ')
	m.advance()
	if m.page != presetHarnessPage {
		t.Fatal("Presets did not advance directly to Harnesses")
	}
	m.back()
	editorKey(m, ' ')
	editorKey(m, 'o')
	editorKey(m, ' ')
	m.advance()
	layout := m.draft.Layouts["default"]
	if !slices.Equal(layout.Presets, []string{"qwen3.5-4b-q4km-off"}) || len(layout.Startup) != 0 || layout.Default != "" {
		t.Fatalf("Default did not follow the revised first-setup selection: %+v", layout)
	}
	if err := m.draft.Validate(); err != nil {
		t.Fatal(err)
	}
	editorKey(m, 'q')
	if m.Decision.Action != "cancel" || len(m.input.Configuration.Presets) != 0 || len(m.input.Configuration.Layouts) != 0 {
		t.Fatal("cancel committed first-setup choices")
	}
}

func TestExistingConfigurationKeepsItsLayoutsWhenPresetsChange(t *testing.T) {
	for _, state := range []string{"custom Default", "no Default", "no layouts"} {
		t.Run(state, func(t *testing.T) {
			input := presetEditor(t).input
			input.NewConfiguration = false
			saved := input.Presets[1]
			input.Configuration.Presets[saved.ID] = setup.Preset{Name: saved.Name, Lock: saved.Lock}
			input.Configuration.Layouts = map[string]setup.Layout{}
			if state != "no layouts" {
				input.Configuration.Layouts["writing"] = setup.Layout{Name: "Writing desk", Presets: []string{saved.ID}, Startup: []string{saved.ID}, Default: saved.ID, IdleSeconds: 60}
			}
			if state == "custom Default" {
				input.Configuration.Layouts["default"] = setup.Layout{Name: "My own layout", Presets: []string{saved.ID}, Startup: []string{}, IdleSeconds: 600}
			}
			before, _ := input.Configuration.Bytes()
			m := NewPresetModel(context.Background(), input, func(_ context.Context, c setup.Configuration) (setup.Plan, error) {
				return setup.Plan{Configuration: &c, CanPrepare: true}, nil
			})
			editorKey(m, ' ')
			m.advance()
			preview := m.advance()
			m.Update(preview())
			if !reflect.DeepEqual(m.plan.Configuration.Layouts, input.Configuration.Layouts) {
				t.Fatalf("saved layouts or startup/default choices changed: %+v", m.plan.Configuration.Layouts)
			}
			m.action = 1
			m.finish()
			if m.Decision.Action != "save" || len(m.Decision.Configuration.Presets) != 2 {
				t.Fatal("rerun failed to save new presets independently of layout membership")
			}
			after, _ := input.Configuration.Bytes()
			if string(before) != string(after) {
				t.Fatal("draft changed the caller's configuration")
			}
		})
	}
}

func TestWizardRefusesRemovingReferencedPresetsAndNamesTheLayout(t *testing.T) {
	input := presetEditor(t).input
	input.NewConfiguration = false
	saved := input.Presets[0]
	input.Configuration.Presets[saved.ID] = setup.Preset{Name: saved.Name, Lock: saved.Lock}
	input.Configuration.Layouts["writing"] = setup.Layout{Name: "Writing desk", Presets: []string{saved.ID}, Startup: []string{saved.ID}, Default: saved.ID, IdleSeconds: 60}
	m := NewPresetModel(context.Background(), input, nil)
	before, _ := m.draft.Bytes()
	editorKey(m, ' ')
	after, _ := m.draft.Bytes()
	if string(before) != string(after) || !strings.Contains(m.notice, "Writing desk") || !strings.Contains(m.notice, "temper layout") {
		t.Fatalf("removing a referenced preset changed its layout or failed to name the correction: %q", m.notice)
	}
	// Saved presets outside layouts can still be deselected.
	editorKey(m, 'o')
	editorKey(m, ' ')
	editorKey(m, ' ')
	if len(m.draft.Presets) != 1 {
		t.Fatal("unreferenced preset could not be removed")
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

func TestWizardRetainsAuthoredPresetSourceWhenReselectingAndCustomizing(t *testing.T) {
	m := presetEditor(t, "qwen3.5-4b-q4km-off")
	p := &m.input.Presets[0]
	p.UserPreset = p.Lock.Preset
	p.ForkBaseline = &setup.ForkBaseline{Origin: p.UserPreset, Values: map[string]json.RawMessage{"request_defaults.sampling.temperature": json.RawMessage("0.7")}}
	m.toggleMember()
	m.toggleMember()
	m.toggleMember()
	if !m.customize(*p, 8192, "") {
		t.Fatal(m.notice)
	}
	if m.draft.Presets[p.ID].UserPreset != p.UserPreset || m.draft.Presets[p.ID].ForkBaseline != p.ForkBaseline {
		t.Fatal("wizard detached the authored source from its saved lock")
	}
	if err := m.draft.Validate(); err != nil {
		t.Fatal(err)
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
