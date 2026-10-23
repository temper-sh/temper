package tui

// Choices renders a single selection using shared button geometry. A negative
// selection leaves every option unselected; the caller owns pending values.
func Choices(tool Tool, labels []string, selected, width int, focused bool) (string, []Target) {
	var buttons []Button
	for i, label := range labels {
		mark, kind := "[ ] ", QuietAction
		if i == selected {
			mark, kind = "[x] ", Action
		}
		buttons = append(buttons, Button{Label: mark + label, Kind: kind, Focused: focused && i == selected})
	}
	return ActionsWithTargets(tool, buttons, width)
}
