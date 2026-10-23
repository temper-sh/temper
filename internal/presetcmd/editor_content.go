package presetcmd

func shownFieldValue(path string, value any) string {
	text := shownValue(value)
	if value != nil {
		switch path {
		case "context_window_tokens", "request_defaults.max_output_tokens", "request_defaults.reasoning_budget_tokens":
			text += " tokens"
		}
	}
	return text
}
