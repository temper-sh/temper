package setupui

import (
	"slices"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/temper-sh/temper/internal/tui"
)

// Preset cards have a row selection and three independent composition controls.
// Their labels, keyboard order and mouse targets all use this order.
var layoutPresetActions = [...]string{"member", "preload", "default"}

const layoutPresetControls = tui.ActionsFocus + 1 // Pointer area; focus stays in content.

func (m *LayoutModel) presetButtons(id string, focused bool) []tui.Button {
	layout := m.draft.Layouts[m.id]
	included := slices.Contains(layout.Presets, id)
	checked := []bool{included, slices.Contains(layout.Startup, id), layout.Default == id}
	disabled := []bool{!included && m.presetErrors[id] != "", !included, !included}
	var buttons []tui.Button
	for i, label := range []string{"Included", "Load on activation", "Default"} {
		mark, kind := "[ ] ", tui.QuietAction
		if checked[i] {
			mark, kind = "[x] ", tui.Action
		}
		buttons = append(buttons, tui.Button{Label: mark + label, Kind: kind,
			Focused: focused && m.control == i, Disabled: disabled[i]})
	}
	return buttons
}

func (m *LayoutModel) activatePresetControl() tea.Cmd {
	id := m.selectedPreset()
	if id == "" || m.presetButtons(id, true)[m.control].Disabled {
		return nil
	}
	return m.invoke(layoutPresetActions[m.control])
}

func (m *LayoutModel) presetCard(id string, focused bool, width int) (string, []tui.Target) {
	preset, available := m.draft.Presets[id]
	name := layoutText(preset.Name)
	if name == "" {
		name = id
	}
	if preset.UserPreset != "" {
		name += " · untested"
	}
	card := Option{Name: name}
	if reason := m.presetErrors[id]; reason != "" {
		if available {
			card.Name += " · saved version"
			card.Details = layoutText(reason)
		} else {
			card.Name += " · unavailable"
			card.DisabledReason = layoutText(reason)
		}
	}
	inner := width - 4 // Shared option-card border and padding.
	controls, targets := tui.ActionsWithTargets(tui.Layouts, m.presetButtons(id, focused), inner)
	card.Components = controls
	headingHeight := lipgloss.Height(lipgloss.NewStyle().Width(inner).Render(card.Name))
	for i := range targets {
		targets[i].X += 2
		targets[i].Y += headingHeight + 1
	}
	return optionCard(card, "", focused, width, layoutColors), targets
}
