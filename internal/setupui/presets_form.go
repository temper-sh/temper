package setupui

import (
	"slices"
	"strings"

	"github.com/temper-sh/temper/internal/tui"
)

type wizardControl struct {
	preset int
	kind   string
}

func (m *PresetModel) wizardControls() []wizardControl {
	var rows []wizardControl
	for i, p := range m.visible() {
		rows = append(rows, wizardControl{i, ""})
		if selected, ok := m.draft.Presets[p.ID]; ok {
			_, record := presetRecord(selected.Lock)
			if len(record.ComputeOptions) > 1 {
				rows = append(rows, wizardControl{i, "compute"})
			}
		}
	}
	return rows
}

func (m *PresetModel) wizardForm(body *screenBody, p PresetOption, index, width int) {
	selected, ok := m.draft.Presets[p.ID]
	if !ok {
		return
	}
	_, record := presetRecord(selected.Lock)
	if len(record.ComputeOptions) > 1 {
		var labels []string
		for _, device := range record.ComputeOptions {
			labels = append(labels, strings.ToUpper(string(device)))
		}
		focused := m.cursor == index && m.control == "compute" && m.focus == presetContentFocus
		options, targets := tui.FormChoices(tui.Wizard, labels, slices.Index(record.ComputeOptions, record.Compute), width, focused)
		top := body.add(tui.Style(tui.Body).Render("Compute")+"\n"+options, focused)
		if focused {
			body.focusStart, body.focusEnd = top, body.height-1
		}
		for _, target := range targets {
			body.targets = append(body.targets, hitTarget{kind: hitWizardCompute, index: index, key: string(record.ComputeOptions[target.Index]), x: target.X, y: top + 1 + target.Y, width: target.Width, height: target.Height})
		}
	}
}
