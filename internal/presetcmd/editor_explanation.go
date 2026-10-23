package presetcmd

import "github.com/temper-sh/temper/internal/tui"

func (m *Editor) pageExplanation(width int) string {
	if m.width < 50 || m.height < 18 {
		return ""
	}
	var title, explanation string
	switch {
	case m.confirm != "":
		title, explanation = "Confirm the change", "Review the consequences below before continuing. Back returns without applying this action."
	case m.input == "new-name":
		title, explanation = "Name your preset", "Create an independent copy of these settings. Apply starts a draft; Save keeps it."
		if m.pending.Fork() {
			explanation = "Create a fork that follows its catalog preset while keeping your overrides. Apply starts a draft; Save keeps it."
		}
	case m.input == "filter":
		title, explanation = "Filter weights files", "Narrow the list by file name. Selected files stay selected when filtered out."
	case m.mode == "manager":
		title, explanation = "Your presets", "Open a saved preset to tune its settings. Choose New to start from a catalog preset."
		if m.catalogTab {
			title, explanation = "Browse catalog presets", "Open a preset to inspect its settings and memory needs, then fork it for tuning or make an independent copy."
		}
	case m.mode == "overview":
		title, explanation = "Inspect this preset", "Fork follows the catalog preset with your overrides. Copy keeps independent settings. Both start an editable draft."
	case m.mode == "form":
		title, explanation = "Choose weights", m.formExplanation()
		if m.formKind == "template" {
			title = "Choose a chat template"
		}
	case m.mode == "local-files":
		title, explanation = "Choose a local template", "Browse folders and choose a file. It will be copied when you Save the preset."
	case m.mode == "files":
		title, explanation = "Select weights files", "Choose the files for one model build, including every part of a split model. Use weights applies the selection to your draft."
	case m.mode == "report":
		title, explanation = "Check this preset", "Inspect validation, machine fit and layout references before using the preset."
		if m.reportTitle == "Source details" {
			title, explanation = "Inspect source details", "See where the weights and template come from, with their exact revisions and files."
		}
	case m.mode == "editor":
		switch m.page {
		case 0:
			title, explanation = "General settings", "Choose the preset's name, model files and supported settings. Larger context windows use more memory."
		case 1:
			title, explanation = "Sampling settings", "Control how the model chooses its next token. These settings affect variation, repetition and consistency."
		case 2:
			title, explanation = "Engine settings", "Tune how the engine runs the model, including compute and memory use. Available controls depend on the engine."
		case 3:
			title, explanation = "Speculative decoding", "Draft candidate tokens for the model to verify. Speed and memory cost depend on the model, engine and workload."
		case 4:
			title, explanation = "Review your preset", "Check the draft and memory assessment before saving. Save keeps the preset; layouts and running models change separately."
			if m.options.DryRun {
				explanation = "Check the draft and memory assessment. Validate checks whether it can be saved without writing the preset."
			}
		}
	}
	return tui.PageExplanation(title, explanation, width)
}
