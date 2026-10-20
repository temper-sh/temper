package setupui

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/temper-sh/temper/internal/harness"
)

func (m *PresetModel) harnessOptions() []harness.Option {
	options := slices.Clone(m.input.Harnesses)
	for _, saved := range m.draft.Integrations {
		i := slices.IndexFunc(options, func(o harness.Option) bool { return o.ID == saved.ID })
		if i < 0 {
			options = append(options, harness.Option{Selection: saved, Name: strings.ToUpper(saved.ID), Detail: "Saved destination; installation not detected."})
		} else {
			if options[i].ModelsFile != saved.ModelsFile {
				options[i].Problem = "" // The saved target is preflighted at activation.
				options[i].Detail = ""
			}
			options[i].Selection = saved
		}
	}
	return options
}

func (m *PresetModel) toggleHarness() {
	options := m.harnessOptions()
	if m.cursor >= len(options) {
		return
	}
	o := options[m.cursor]
	i := slices.IndexFunc(m.draft.Integrations, func(s harness.Selection) bool { return s.ID == o.ID })
	if i >= 0 {
		m.draft.Integrations = slices.Delete(m.draft.Integrations, i, i+1)
	} else if o.Problem != "" {
		m.notice = o.Problem
	} else if o.Detected {
		m.draft.Integrations = append(m.draft.Integrations, o.Selection)
	} else {
		m.notice = "Install " + o.Name + " first, or explicitly configure its destination with configure --file."
	}
}

func (m *PresetModel) harnessCards(body *screenBody, width int) {
	options := m.harnessOptions()
	if len(options) == 0 {
		body.add(infoBlock("No supported harness detected", []string{"Install Pi, PiG or Hermes, then return to configure. You can continue without an integration."}, width), false)
	}
	for i, o := range options {
		selected := slices.ContainsFunc(m.draft.Integrations, func(s harness.Selection) bool { return s.ID == o.ID })
		mark := "[ ]"
		if selected {
			mark = "[x]"
		}
		detection := "Not found on PATH"
		if o.Detected {
			detection = "Detected"
		}
		focused := m.focus == presetContentFocus && m.cursor == i
		detail := o.Detail
		for _, other := range options {
			if o.ID != "hermes" && other.ID != "hermes" && o.ID != other.ID && o.ModelsFile == other.ModelsFile {
				detail = "Shared models.json: models are visible to both Pi and PiG."
				break
			}
		}
		if o.Problem != "" {
			detail = o.Problem
		}
		card := optionCard(Option{Name: mark + " " + o.Name, Components: detection, Description: o.ModelsFile, Details: detail}, "", focused, width, layoutColors)
		top := body.addCard(card, focused)
		body.targets = append(body.targets, hitTarget{kind: hitChoice, index: i, y: top, width: width, height: lipgloss.Height(card)})
	}
	body.add(infoBlock("On layout activation", []string{"Selected harnesses receive the included chat models. Saving alone does not change harness files. Existing defaults and active sessions stay under your control."}, width), false)
}
