package setupui

import (
	"maps"
	"slices"
	"strings"

	"github.com/temper-sh/temper/internal/setup"
)

// defaultLayout is the layout first setup creates; reruns may extend it.
const defaultLayout = "default"

// defaultAdditions are presets newly selected in this rerun that no layout
// includes yet, when the configuration still has its Default layout. Presets
// already saved outside layouts stay where their owner placed them.
func (m *PresetModel) defaultAdditions() []string {
	if m.input.NewConfiguration {
		return nil
	}
	if _, ok := m.draft.Layouts[defaultLayout]; !ok {
		return nil
	}
	var ids []string
	for _, id := range m.presetIDs() {
		if _, saved := m.input.Configuration.Presets[id]; saved {
			continue
		}
		if len(m.draft.References(id)) == 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

// reviewDraft is the configuration Review checks and saves: the draft, with
// the new presets added to Default unless the user turned that off. Default
// gains on-demand members only; its startup and default choices are kept.
func (m *PresetModel) reviewDraft() setup.Configuration {
	additions := m.defaultAdditions()
	if m.keepDefault || len(additions) == 0 {
		return m.draft
	}
	c := m.draft
	c.Layouts = maps.Clone(m.draft.Layouts)
	layout := c.Layouts[defaultLayout]
	layout.Presets = append(slices.Clone(layout.Presets), additions...)
	c.Layouts[defaultLayout] = layout
	return c
}

// defaultName is the Default layout's display name; users may rename it.
func (m *PresetModel) defaultName() string {
	return m.draft.Layouts[defaultLayout].Name
}

func (m *PresetModel) presetNames(ids []string) string {
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		names = append(names, m.draft.Presets[id].Name)
	}
	return strings.Join(names, ", ")
}
