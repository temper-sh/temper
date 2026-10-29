package setupui

import (
	"fmt"
	"image/color"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/tui"
)

func (m *PresetModel) View() tea.View {
	m.hitTargets = nil
	if m.width < 28 || m.height < 10 {
		v := tea.NewView(ansi.Truncate("Enlarge terminal to 28 × 10.", m.width, ""))
		v.AltScreen = true
		return v
	}
	width := tui.ContentWidth(m.width)
	header := tui.Header(tui.Wizard, "", width) + "\n"
	if m.height >= 18 {
		header += "\n"
	}
	if explanation := m.pageExplanation(width); explanation != "" {
		header += explanation + "\n\n"
	}
	step := -1
	if m.focus == presetStepsFocus {
		step = m.step
	}
	navigation, steps := tui.Steps([]string{"Presets", "Harnesses", "Review"}, m.page, step, width)
	stepTop := strings.Count(header, "\n")
	header += navigation + "\n"
	var headerTargets []hitTarget
	if m.field == "" {
		for _, target := range steps {
			headerTargets = append(headerTargets, hitTarget{kind: hitStep, index: target.Index, x: target.X, y: stepTop + target.Y, width: target.Width, height: target.Height})
		}
	}
	if m.page == 0 && m.field == "" {
		filters, targets := m.filters(width)
		for _, target := range targets {
			target.y += strings.Count(header, "\n")
			headerTargets = append(headerTargets, target)
		}
		header += filters + "\n"
	}
	footer, footerTargets := m.presetFooter(width)
	frame := tui.Frame{Width: m.width, Height: m.height, Header: header, Footer: footer}
	body := m.presetContent(width)
	m.view.SetWidth(width)
	m.view.SetHeight(frame.BodyHeight())
	m.view.SetContent(strings.Join(body.blocks, "\n"))
	if m.focusSelection && body.focusStart >= 0 {
		m.view.EnsureVisible(body.focusEnd, 0, 0)
		m.view.EnsureVisible(body.focusStart, 0, 0)
	}
	m.focusSelection = false
	left := (m.width - width) / 2
	place := func(targets []hitTarget, origin, top, bottom int) {
		for _, target := range targets {
			y := origin + target.y
			end := min(bottom, y+target.height)
			y = max(top, y)
			if end <= y {
				continue
			}
			target.x += left
			target.y, target.height = y, end-y
			m.hitTargets = append(m.hitTargets, target)
		}
	}
	place(headerTargets, frame.HeaderTop(), frame.HeaderTop(), frame.BodyTop())
	place(body.targets, frame.BodyTop()-m.view.YOffset(), frame.BodyTop(), frame.BodyTop()+frame.BodyHeight())
	place(footerTargets, frame.FooterTop(), frame.BodyTop()+frame.BodyHeight(), m.height-frame.HeaderTop())
	v := tea.NewView(frame.Render(m.view.View()))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.BackgroundColor, v.ForegroundColor = tui.Colors(tui.Canvas).Background, tui.Colors(tui.Body).Text
	return v
}

func (m *PresetModel) pageExplanation(width int) string {
	if m.width < 50 || m.height < 18 {
		return ""
	}
	title, explanation := "Choose your presets", m.currentGroup().Description
	switch {
	case m.field == "compute":
		title, explanation = "Choose where the model runs", "CPU uses system RAM. GPU also uses the wired-memory allowance."
	case m.field == "context":
		title, explanation = "Set the context window", "Context is the total space for input and output, in tokens. A larger window requires more memory."
	case m.page == presetHarnessPage:
		title, explanation = "Connect your harnesses", "Choose which apps receive this root’s active models."
	case m.page == presetReviewPage:
		title, explanation = "Review your setup", "Check models, apps and downloads, then install or save for later."
	}
	return tui.PageExplanation(title, explanation, width)
}

func (m *PresetModel) filters(width int) (string, []hitTarget) {
	var labels []string
	current := 0
	for i, group := range m.groups() {
		labels = append(labels, group.DisplayName)
		if group.ID == m.group {
			current = i
		}
	}
	row, controls := tui.SecondaryWithTargets(tui.Wizard, labels, current, width, m.focus == presetFilterFocus)
	var targets []hitTarget
	for _, target := range controls {
		targets = append(targets, hitTarget{kind: hitFilter, index: target.Index, x: target.X, y: target.Y, width: target.Width, height: target.Height})
	}
	return row, targets
}

func (m *PresetModel) presetContent(width int) screenBody {
	body := screenBody{focusStart: -1}
	if m.field != "" {
		m.editContent(&body, width)
		return body
	}
	switch m.page {
	case 0:
		if m.input.Machine != "" {
			body.add(tui.Style(tui.Muted).Width(width).Render(m.input.Machine), false)
		}
		rows := m.visible()
		if len(rows) == 0 {
			body.add(infoBlock("No "+strings.ToLower(m.currentGroup().DisplayName)+" presets", []string{"Choose another tab to browse available presets. Your selections are kept across tabs."}, width), false)
		}
		for i, p := range rows {
			section, hasSection := m.section(p.Section)
			newTask := hasSection && (i == 0 || p.Section != rows[i-1].Section)
			if newTask {
				body.add(lipgloss.NewStyle().Foreground(secondaryNavColors.Name[1]).Bold(true).Width(width).Render(section.DisplayName)+"\n"+
					lipgloss.NewStyle().Foreground(colorMuted).Width(width).Render(section.Description), false)
			}
			if i == 0 || newTask || p.Tier != rows[i-1].Tier || (p.Unavailable == "") != (rows[i-1].Unavailable == "") {
				group := catalog.MemoryTier(p.Tier).Label()
				if p.Tier != "" {
					group += " · estimated placement"
				}
				if p.Unavailable != "" {
					group = "Unavailable on this machine · " + group
				}
				body.add(lipgloss.NewStyle().Foreground(secondaryNavColors.Name[0]).Bold(true).Width(width).Render(group), false)
			}
			mark, lock := "[ ]", p.Lock
			if selected, ok := m.draft.Presets[p.ID]; ok {
				mark, p.Name, lock = "[x]", selected.Name, selected.Lock
			}
			_, record := presetRecord(lock)
			focused := m.focus == presetContentFocus && i == m.cursor
			background := colorBackground
			if focused {
				background = presetColors.Surface
			}
			metadata := lipgloss.NewStyle().Background(background)
			label := "Context "
			if record.Interface == "reranking" && !record.ContextConfigurable() {
				label = "Fixed pair limit "
			}
			details := metadata.Foreground(colorMuted).Render(label) + metadata.Foreground(presetColors.Accent).Render(tokenCount(record.Window())+" tokens")
			if p.MemoryBytes > 0 {
				label, limit := " · Memory estimate ", " · Total fit unknown"
				if p.MemoryBasis == "measured" {
					label, limit = " · Measured peak ", " · Observed workload"
				}
				details += metadata.Foreground(colorMuted).Render(label) + metadata.Foreground(presetColors.Accent).Render(setup.Size(p.MemoryBytes)) + metadata.Foreground(colorMuted).Render(limit)
			}
			if template := record.TemplateID(); record.Interface == "chat-completions" && template != catalog.BuiltinTemplate {
				name := lock.Records.Templates[template].Presentation.DisplayName
				if name == "" {
					name = template
				}
				details += metadata.Foreground(colorMuted).Render(" · Template ") + metadata.Foreground(presetColors.Name[1]).Render(name)
			}
			compute := ""
			if len(record.ComputeOptions) > 1 {
				compute = "Compute " + strings.ToUpper(string(record.Compute))
			} else if record.Compute == catalog.ComputeCPU {
				compute = "CPU only"
			}
			if compute != "" {
				details = metadata.Foreground(presetColors.Name[2]).Render(compute) + metadata.Foreground(colorMuted).Render(" · ") + details
			}
			if len(lock.Records.Presets) == 0 {
				details = ""
			}
			if p.Details != "" {
				details = strings.TrimSuffix(p.Details+"\n"+details, "\n")
			}
			if strings.HasSuffix(p.Name, " (customized)") {
				details = "Customized preset\n" + details
			}
			description := strings.TrimSpace(p.Description)
			if description != "" {
				description += " · "
			}
			if license := licenseLink(lock.Records.Artifacts[record.Artifact], presetColors.Link, colorText); license != "" {
				description += license
			} else {
				description = strings.TrimSuffix(description, " · ")
			}
			size := ""
			if p.ModelBytes > 0 {
				size = setup.Size(p.ModelBytes)
			}
			card := optionCard(Option{Name: p.heading(background), Size: size, Description: description,
				Details: details, AssessmentURL: p.AssessmentURL, DisabledReason: p.Unavailable}, mark, focused, width, presetColors)
			top := body.addCard(card, focused)
			if focused {
				body.focusStart, body.focusEnd = top+1, top+1
			}
			body.targets = append(body.targets, hitTarget{kind: hitChoice, index: i, y: top, width: width, height: lipgloss.Height(card)})
		}
	case presetHarnessPage:
		m.harnessCards(&body, width)
	case presetReviewPage:
		m.reviewContent(&body, width)
	}
	if m.notice != "" {
		body.add(infoBlock("Attention", []string{m.notice}, width), false)
	}
	return body
}

// licenseLink draws the license as a link in the link color, then gives the
// rest of the enclosing card or table cell its own color back. It toggles
// only color and underline, so the enclosing background and boldness stay.
func licenseLink(artifact catalog.Artifact, link, after color.Color) string {
	if artifact.License == "" {
		return ""
	}
	return ansi.SetHyperlink(artifact.LicenseURL(), "") +
		ansi.NewStyle().ForegroundColor(link).Underline(true).String() + artifact.License +
		ansi.NewStyle().ForegroundColor(after).Underline(false).String() + ansi.ResetHyperlink()
}

func (p PresetOption) heading(background color.Color) string {
	var parts []string
	colors := presetColors.Name
	style := lipgloss.NewStyle().Background(background).Bold(true)
	for i, value := range []string{p.Model, weightsLabel(p.Weights), p.Engine} {
		if value != "" {
			parts = append(parts, style.Foreground(colors[i]).Render(value))
		}
	}
	if len(parts) == 0 {
		return p.Name
	}
	return strings.Join(parts, style.Foreground(colorMuted).Render(" · "))
}

func weightsLabel(name string) string {
	// Saved locks can retain the format suffix in their display names.
	return strings.TrimSuffix(strings.TrimSuffix(name, " · GGUF"), " GGUF")
}

func tokenCount(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func (m *PresetModel) editContent(body *screenBody, width int) {
	title, detail := "Context window", "Total tokens for input and output. A larger window requires more memory."
	if m.field == "compute" {
		title, detail = "Compute device", "CPU uses system RAM. GPU also uses the wired-memory allowance."
	}
	if m.pageExplanation(width) != "" {
		detail = ""
	}
	if p, ok := m.selectedOption(); ok {
		title += " · " + p.Name
		if refs := m.draft.References(p.ID); len(refs) > 0 {
			var names []string
			for _, id := range refs {
				names = append(names, m.draft.Layouts[id].Name)
			}
			detail = strings.TrimSpace(detail + "\nThis shared preset will change in: " + strings.Join(names, ", ") + ".")
		}
	}
	inner := width - 4
	m.text.SetWidth(max(1, inner-3))
	heading := lipgloss.NewStyle().Foreground(colorText).Bold(true).Width(inner).Render(title)
	input := m.text.View()
	var choices []hitTarget
	if m.field == "compute" {
		p, _ := m.selectedOption()
		_, record := presetRecord(m.draft.Presets[p.ID].Lock)
		var labels []string
		for _, device := range record.ComputeOptions {
			labels = append(labels, strings.ToUpper(string(device)))
		}
		var targets []tui.Target
		input, targets = tui.FormChoices(tui.Wizard, labels, slices.Index(record.ComputeOptions, m.compute), inner, m.focus == presetContentFocus)
		for _, target := range targets {
			choices = append(choices, hitTarget{kind: hitWizardCompute, key: string(record.ComputeOptions[target.Index]), x: 2 + target.X, y: target.Y, width: target.Width, height: target.Height})
		}
	}
	content := heading + "\n\n" + input
	if detail != "" {
		content += "\n\n" + lipgloss.NewStyle().Foreground(colorMuted).Width(inner).Render(detail)
	}
	border := colorBorder
	if m.focus == presetContentFocus {
		border = tui.Wizard.Colors(tui.InputFocused).Border
	}
	top := body.add(lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(0, 1).Width(width).Render(content), false)
	// Even a small viewport keeps the field, rather than its explanation, in view.
	inputLine := top + lipgloss.Height(heading) + 2
	if m.focus == presetContentFocus {
		body.focusStart, body.focusEnd = inputLine, inputLine
	}
	if m.field == "compute" {
		for _, target := range choices {
			target.y += inputLine
			body.targets = append(body.targets, target)
		}
	} else {
		body.targets = append(body.targets, hitTarget{kind: hitField, x: 2, y: inputLine, width: inner, height: 1})
	}
	if m.notice != "" {
		body.add(warningBlock("Check this value", []string{m.notice}, width), false)
	}
}

func (m *PresetModel) reviewContent(body *screenBody, width int) {
	if m.loading {
		body.add(infoBlock("Checking your setup…", []string{"Resolving exact presets, downloads and machine limits."}, width), false)
		return
	}
	if m.reviewErr != nil {
		body.add(infoBlock("Preview error", []string{m.reviewErr.Error(), "Press r to retry, or go back to change your choices."}, width), false)
		return
	}
	if !m.reviewReady {
		return
	}
	sections := m.plan.Sections()
	for _, section := range sections {
		if section.Warning {
			body.add(warningBlock(section.Title, section.Lines, width), false)
		}
	}
	m.reviewSummary(body, width)
	m.reviewDownloads(body, width)
	m.reviewMemory(body, width)
}

func (m *PresetModel) presetFooter(width int) (string, []hitTarget) {
	definitions := m.footerButtons()
	var buttons []tui.Button
	for i, button := range definitions {
		label := button.label
		if width < 70 {
			label = button.short
		}
		kind := tui.Action
		target := button.target
		switch {
		case target.kind == hitNext, target.kind == hitReview && target.index == 0, target.key == "apply":
			kind = tui.PrimaryAction
		case target.key == "back", target.key == "cancel-edit", target.kind == hitReview && target.index >= 2:
			kind = tui.QuietAction
		}
		buttons = append(buttons, tui.Button{Label: label, Kind: kind, Focused: m.focus == presetFooterFocus && m.action == i, Disabled: !button.available})
	}
	var parts []string
	if m.notice != "" {
		parts = append(parts, tui.Style(tui.Notice).Render(ansi.Wrap(m.notice, width, "")))
	}
	parts = append(parts, tui.Style(tui.Divider).Render(strings.Repeat("─", width)))
	actions, controls := tui.ActionsWithTargets(tui.Wizard, buttons, width)
	actionsTop := lipgloss.Height(strings.Join(parts, "\n"))
	var targets []hitTarget
	for _, target := range controls {
		targets = append(targets, hitTarget{kind: hitPresetAction, index: target.Index, x: target.X, y: actionsTop + target.Y, width: target.Width, height: target.Height})
	}
	parts = append(parts, actions)
	hints := []tui.Hint{{Key: "↑↓", Action: "choose"}, {Key: "Space", Action: "select"}, {Key: "Tab", Action: "controls"}, {Key: "q", Action: "cancel"}}
	if m.field == "compute" && m.focus == presetContentFocus {
		hints = []tui.Hint{{Key: "←→", Action: "choose"}, {Key: "Enter", Action: "apply"}, {Key: "Tab", Action: "actions"}, {Key: "Esc", Action: "cancel"}}
	} else if m.field != "" {
		hints = tui.InputHints(m.focus == presetFooterFocus)
	} else if m.focus != presetContentFocus {
		hints = tui.FocusHints(m.focus)
	} else if m.page == presetReviewPage {
		hints = []tui.Hint{{Key: "↑↓ / PgUp/Dn", Action: "scroll"}, {Key: "Tab", Action: "actions"}}
	}
	parts = append(parts, tui.KeyHelp(hints, width))
	return strings.Join(parts, "\n"), targets
}
