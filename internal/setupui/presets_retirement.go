package setupui

import (
	"fmt"
	"strings"

	"github.com/temper-sh/temper/internal/setup"
)

func (m *PresetModel) successorOption(p PresetOption) (PresetOption, error) {
	if p.Successor == "" {
		return PresetOption{}, fmt.Errorf("%s", p.SuccessorUnavailable)
	}
	for _, target := range m.input.Presets {
		if target.ID == p.Successor {
			if existing, ok := m.draft.Presets[target.ID]; ok {
				target.Lock, target.Name = existing.Lock, existing.Name
				if m.input.Assess != nil {
					assessment, err := m.input.Assess(existing.Lock)
					if err != nil {
						return PresetOption{}, err
					}
					target.Unavailable = strings.Join(assessment.Refusals, "; ")
				}
			}
			if !target.buildable() || target.Unavailable != "" {
				return PresetOption{}, fmt.Errorf("successor unavailable: %s", target.Unavailable)
			}
			return target, nil
		}
	}
	return PresetOption{}, fmt.Errorf("recommended successor %s is unavailable", p.Successor)
}

func (m *PresetModel) beginRetirement(action string) {
	p, ok := m.selectedOption()
	if !ok || p.Retirement == nil {
		return
	}
	if _, selected := m.draft.Presets[p.ID]; !selected {
		m.notice = "Change already staged. Review or cancel this setup to keep the original choices."
		return
	}
	if action == "replace-retired" {
		if _, err := m.successorOption(p); err != nil {
			m.notice = err.Error()
			return
		}
	}
	m.field, m.notice = action, ""
	m.text.Blur()
	m.focusArea(presetFooterFocus)
	m.action = 0
	m.view.GotoTop()
}

func (m *PresetModel) acceptRetirement() {
	p, ok := m.selectedOption()
	if !ok || p.Retirement == nil {
		return
	}
	target := ""
	replacement := setup.Preset{}
	if m.field == "replace-retired" {
		option, err := m.successorOption(p)
		if err != nil {
			m.notice = err.Error()
			return
		}
		target = option.ID
		if existing, ok := m.draft.Presets[target]; ok {
			replacement = existing
		} else {
			replacement = setup.RetirementPreset(option.Lock, option.Name)
		}
	}
	next, err := setup.ChangeRetired(m.draft, p.ID, target, replacement)
	if err != nil {
		m.notice = err.Error()
		return
	}
	m.draft = next
	m.retirements = append(m.retirements, setup.RetirementChange{Preset: p.ID, Successor: target})
	m.field = ""
	m.focusArea(presetContentFocus)
	m.view.GotoTop()
	m.notice = "Retirement change staged. Continue to Review to apply it; Cancel keeps the installed selection."
}

func (m *PresetModel) retirementContent(body *screenBody, width int) {
	p, ok := m.selectedOption()
	if !ok {
		return
	}
	lines := []string{"Retired " + p.Retirement.RetiredOn + ": " + p.Retirement.Reason}
	lines = append(lines, "Shared HF files stay unless you choose deletion at Review, which lists each file.")
	title := "Remove " + p.Name
	if m.field == "replace-retired" {
		title = "Replace " + p.Name
		target, err := m.successorOption(p)
		if err != nil {
			lines = append(lines, err.Error())
		} else {
			lines = append(lines, "Successor: "+target.Name)
			lines = append(lines, "API model ID: "+p.ID+" → "+target.ID+". Clients using the old ID must select the successor.")
			old := m.draft.Presets[p.ID]
			for _, change := range setup.Changes(old.Lock, target.Lock) {
				lines = append(lines, change)
			}
			if selected, ok := m.draft.Presets[target.ID]; ok {
				lines = append(lines, "Use already-selected successor: "+selected.Name+". Its settings stay.")
			} else {
				lines = append(lines, "Use successor defaults. Existing custom settings and context/compute choices are reset.")
				if old.UserPreset != "" {
					lines = append(lines, "Your authored overrides are not transferred; the preset file is retained.")
				}
			}
			lines = append(lines, "Download and prepare before switching. Shared router follows the successor; other model settings stay.")
		}
	} else {
		lines = append(lines, "Remove this saved preset from inclusion, startup and default in every affected layout.")
		if m.draft.Presets[p.ID].UserPreset != "" {
			lines = append(lines, "Your authored preset file is retained.")
		}
	}
	if refs := m.draft.References(p.ID); len(refs) > 0 {
		lines = append(lines, "Affected layouts: "+strings.Join(refs, ", "))
	}
	lines = append(lines, "Clean newly unused managed files after a successful switch. Shared dependencies stay while another preset uses them.")
	body.add(infoBlock(title, lines, width), false)
	if m.notice != "" {
		body.add(warningBlock("Cannot apply", []string{m.notice}, width), false)
	}
}
