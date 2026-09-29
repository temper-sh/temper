package setupui

import (
	"context"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/setup"
)

func presetEditor(t *testing.T) *PresetModel {
	t.Helper()
	raw, err := os.ReadFile("../../catalog/guided-setup.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	c := setup.EmptyConfiguration()
	c.Layouts = setup.StarterLayouts()
	input := PresetInput{Configuration: c}
	for _, id := range []string{"qwen3.8-27b-q4xl-splash", "qwen3.5-4b-q4km-off"} {
		p := d.Layouts[id]
		lock, err := catalog.CompilePreset(d, id, "", 0, d.Runtime.Router.Target)
		if err != nil {
			t.Fatal(err)
		}
		input.Presets = append(input.Presets, PresetOption{ID: id, Name: p.DisplayName, Recommended: p.Recommended, Lock: lock, Document: d})
	}
	return NewPresetModel(context.Background(), input, func(context.Context, setup.Configuration) (setup.Plan, error) { return setup.Plan{}, nil })
}
func editorKey(m *PresetModel, key rune) {
	_, _ = m.Update(tea.KeyPressMsg{Code: key, Text: string(key)})
}

func TestPresetTabsAndBackNavigationKeepOneSelection(t *testing.T) {
	m := presetEditor(t)
	editorKey(m, ' ')
	editorKey(m, 'a')
	m.cursor = 1
	editorKey(m, ' ')
	editorKey(m, 'a')
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

func TestLayoutInclusionStartupAndDefaultAreIndependent(t *testing.T) {
	m := presetEditor(t)
	editorKey(m, ' ')
	m.page = 1
	m.layout = "local"
	editorKey(m, ' ')
	editorKey(m, 'd')
	l := m.draft.Layouts["local"]
	if len(l.Startup) != 0 || l.Default == "" {
		t.Fatal("default implied preload")
	}
	editorKey(m, 's')
	editorKey(m, 'd')
	l = m.draft.Layouts["local"]
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
	m.page = 1
	editorKey(m, 'x')
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
	m.all = true
	m.draft.Presets[p.ID] = setup.Preset{Name: p.Name, Lock: p.Lock}
	m.field = "context"
	m.text.SetValue("8192")
	m.acceptEdit()
	saved := m.draft.Presets[p.ID]
	_, record := presetRecord(saved.Lock)
	if record.ContextWindowTokens != 8192 || !strings.Contains(saved.Name, "customized") {
		t.Fatal("alias edit lost source settings", m.notice)
	}
	if _, changed := m.draft.Presets["qwen3.5-4b-q4km-off"]; changed {
		t.Fatal("alias edit replaced catalog choice")
	}
}
