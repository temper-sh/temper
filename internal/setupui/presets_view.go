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
)

func (m *PresetModel) View() tea.View {
	m.hitTargets = nil
	if m.width < 28 || m.height < 10 {
		v := tea.NewView(lipgloss.NewStyle().Width(m.width).MaxHeight(m.height).
			Render("Enlarge terminal to 28 × 10.\nq to cancel"))
		v.AltScreen = true
		return v
	}
	width := min(108, m.width-4)
	if m.width < 50 {
		width = m.width - 2
	}
	compact := m.height < 18 || m.width < 50
	left := (m.width - width) / 2
	brand := lipgloss.NewStyle().Foreground(colorBrand).Bold(true).Render("TEMPER") +
		lipgloss.NewStyle().Foreground(colorMuted).Render("  /  Guided setup")
	if width >= 40 {
		progress := lipgloss.NewStyle().Foreground(colorMuted).Render(fmt.Sprintf("%d / 4", m.page+1))
		brand += strings.Repeat(" ", width-lipgloss.Width(brand)-lipgloss.Width(progress)) + progress
	}
	header := brand
	if !compact && m.input.Machine != "" {
		header += "\n" + lipgloss.NewStyle().Foreground(colorMuted).Width(width).Render(m.input.Machine) + "\n"
	}
	header += "\n" + stepTabs([]string{"Presets", "Layouts", "Harnesses", "Review"}, m.page, width)
	if m.page == 0 && m.field == "" {
		filters, targets := m.filters(width)
		for _, target := range targets {
			target.x += left
			target.y += lipgloss.Height(header)
			m.hitTargets = append(m.hitTargets, target)
		}
		header += "\n" + filters
	}
	foot, footerTargets := m.presetFooter(width, compact)
	body := m.presetContent(width, compact)
	footerHeight := lipgloss.Height(foot)
	if !compact {
		footerHeight++
	}
	m.view.SetWidth(width)
	m.view.SetHeight(max(1, m.height-lipgloss.Height(header)-footerHeight))
	m.view.SetContent(strings.Join(body.blocks, "\n"))
	if m.focusSelection && body.focusStart >= 0 {
		m.view.EnsureVisible(body.focusEnd, 0, 0)
		m.view.EnsureVisible(body.focusStart, 0, 0)
	}
	m.focusSelection = false
	if m.focusDownloads && body.downloadsTop >= 0 {
		m.view.SetYOffset(body.downloadsTop)
		m.view.EnsureVisible(body.downloadsTop+1, 0, 0)
	}
	m.focusDownloads = false
	if !compact {
		label := ""
		if m.view.TotalLineCount() > m.view.Height() {
			label = fmt.Sprintf(" %.0f%% · trackpad / PgUp/PgDown ", m.view.ScrollPercent()*100)
		}
		foot = lipgloss.NewStyle().Foreground(colorMuted).Render(strings.Repeat("─", max(0, width-lipgloss.Width(label)))+label) + "\n" + foot
	}
	bodyTop := lipgloss.Height(header)
	for _, target := range body.targets {
		top := target.y - m.view.YOffset()
		bottom := min(m.view.Height(), top+target.height)
		top = max(0, top)
		if bottom > top {
			target.x += left
			target.y, target.height = bodyTop+top, bottom-top
			m.hitTargets = append(m.hitTargets, target)
		}
	}
	footerTop := bodyTop + m.view.Height()
	if !compact {
		footerTop++
	}
	for _, target := range footerTargets {
		target.x += left
		target.y += footerTop
		m.hitTargets = append(m.hitTargets, target)
	}
	content := header + "\n" + m.view.View() + "\n" + foot
	content = lipgloss.NewStyle().Foreground(colorText).Background(colorBackground).
		Width(m.width).Height(m.height).PaddingLeft(left).PaddingRight(m.width - width - left).Render(content)
	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.BackgroundColor, v.ForegroundColor = colorBackground, colorText
	return v
}

func (m *PresetModel) filters(width int) (string, []hitTarget) {
	padding, margin, gap := 0, 0, 2
	if width >= 34 {
		padding = 1
	}
	if width >= 50 {
		padding = 2
		gap = 3
	}
	if m.height >= 18 {
		margin = 1
	}
	counts := map[string]int{}
	for _, p := range m.input.Presets {
		counts[p.Group]++
	}
	var labels, counted []string
	for _, group := range m.groups() {
		labels = append(labels, group.DisplayName)
		counted = append(counted, fmt.Sprintf("%s %d", group.DisplayName, counts[group.ID]))
	}
	if width >= 50 && lipgloss.Width(strings.Join(counted, ""))+2*padding*len(counted)+gap*(len(counted)-1) <= width {
		labels = counted
	}
	var buttons []string
	var targets []hitTarget
	x := 0
	for i, label := range labels {
		style := lipgloss.NewStyle().Padding(0, padding).Foreground(colorMuted)
		if m.groups()[i].ID == m.group {
			style = style.Foreground(secondaryNavColors.Name[0]).Background(secondaryNavColors.Surface).Bold(true)
			if m.focus == presetFilterFocus {
				style = inverted(style, secondaryNavColors.Name[0], secondaryNavColors.Surface)
			}
		}
		button := style.Render(label)
		buttons = append(buttons, button)
		targets = append(targets, hitTarget{kind: hitFilter, index: i, x: x, y: margin, width: lipgloss.Width(button), height: 1})
		x += lipgloss.Width(button) + gap
	}
	row := strings.Join(buttons, strings.Repeat(" ", gap))
	count := fmt.Sprintf("%d selected", len(m.draft.Presets))
	if remaining := width - lipgloss.Width(row) - len(count); remaining > 0 {
		row += strings.Repeat(" ", remaining) + lipgloss.NewStyle().Foreground(secondaryNavColors.Accent).Render(count)
	}
	return lipgloss.NewStyle().Margin(margin, 0).Render(row), targets
}

func (m *PresetModel) presetContent(width int, compact bool) screenBody {
	body := screenBody{focusStart: -1, downloadsTop: -1}
	if m.field != "" {
		m.editContent(&body, width)
		return body
	}
	title, subtitle := "Choose your presets", m.currentGroup().Description
	if m.page == 1 {
		title, subtitle = "Arrange your layouts", "Edit layouts or add one, such as Helper for small models."
		if m.layout != "" {
			title = m.draft.Layouts[m.layout].Name + " · Presets"
			subtitle = "Include makes a preset available. Load on activation warms it up. Default is optional and does not imply loading."
		}
	} else if m.page == presetHarnessPage {
		title, subtitle = "Connect your harnesses", "Choose which apps receive this root’s active models."
	} else if m.page == presetReviewPage {
		title, subtitle = "Review your setup", "Check downloads, memory and your layouts."
	}
	if !compact {
		body.add(lipgloss.NewStyle().Foreground(colorText).Bold(true).Width(width).Render(title)+"\n"+
			lipgloss.NewStyle().Foreground(colorMuted).Width(width).Render(subtitle), false)
	}
	switch m.page {
	case 0:
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
	case 1:
		if m.layout == "" {
			m.layoutCards(&body, width)
		} else {
			m.membershipCards(&body, width)
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

// buttonColor gives a footer button its action color: the primary action
// (forward, install, apply) strongest, other actions lighter, leaving muted
// and removing red. An install that cannot run warns.
func buttonColor(b presetButton) color.Color {
	t := b.target
	switch {
	case !b.available && t.kind == hitReview && t.index == 0:
		return colorWarn
	case !b.available:
		return colorMuted
	case t.kind == hitNext, t.kind == hitReview && t.index == 0, t.key == "apply":
		return actionColors.Name[0]
	case t.key == "x":
		return colorDanger
	case t.kind == hitReview && t.index == 3, t.key == "cancel-edit", t.key == "back" && b.label == "Cancel":
		return colorMuted
	}
	return actionColors.Name[1]
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

func (m *PresetModel) presetNames(ids []string) string {
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		names = append(names, m.draft.Presets[id].Name)
	}
	if len(names) == 0 {
		return "None"
	}
	return strings.Join(names, ", ")
}

func (m *PresetModel) layoutCards(body *screenBody, width int) {
	ids := m.layoutIDs()
	if len(ids) == 0 {
		body.add(infoBlock("No layouts yet", []string{"Choose New to create a layout, or continue to save your presets on their own."}, width), false)
	}
	for i, id := range ids {
		l := m.draft.Layouts[id]
		def := "None · requests name a preset"
		if l.Default != "" {
			def = m.draft.Presets[l.Default].Name
		}
		focused := m.focus == presetContentFocus && i == m.cursor
		background := colorBackground
		if focused {
			background = layoutColors.Surface
		}
		name := lipgloss.NewStyle().Foreground(layoutColors.Name[0]).Background(background).Bold(true).Render(l.Name)
		card := optionCard(Option{Name: name, Components: fmt.Sprintf("%d included · %d load on activation · idle %s", len(l.Presets), len(l.Startup), idleDuration(l.IdleSeconds)),
			Description: "Presets: " + m.presetNames(l.Presets) + "\nDefault: " + def,
			Details:     "Enter to edit presets, startup loading and default."}, "", focused, width, layoutColors)
		top := body.addCard(card, focused)
		if focused {
			body.focusStart, body.focusEnd = top+1, top+1
		}
		body.targets = append(body.targets, hitTarget{kind: hitChoice, index: i, y: top, width: width, height: lipgloss.Height(card)})
	}
}

func idleDuration(seconds int) string {
	if seconds%60 == 0 {
		return fmt.Sprintf("%d min", seconds/60)
	}
	return fmt.Sprintf("%d sec", seconds)
}

func (m *PresetModel) membershipCards(body *screenBody, width int) {
	l := m.draft.Layouts[m.layout]
	ids := m.presetIDs()
	padding := 1
	if width < 30 {
		padding = 0
	}
	if len(ids) == 0 {
		body.add(infoBlock("Choose presets first", []string{"Return to Presets to select models for this layout. Empty layouts can be saved."}, width), false)
	}
	for i, id := range ids {
		border, background := colorBorder, colorBackground
		focused := m.focus == presetContentFocus && i == m.cursor
		if focused {
			border, background = layoutColors.Border, layoutColors.Surface
		}
		inner := width - 2 - 2*padding
		name := m.draft.Presets[id].Name
		for _, p := range m.input.Presets {
			if p.ID == id {
				name = p.heading(background)
				break
			}
		}
		heading := lipgloss.NewStyle().Foreground(presetColors.Name[0]).Background(background).Bold(true).Width(inner).Render(name)
		included, startup, def := slices.Contains(l.Presets, id), slices.Contains(l.Startup, id), l.Default == id
		var switches []string
		var targets []hitTarget
		x, y := 0, lipgloss.Height(heading)
		for j, label := range []string{"Included", "Load on activation", "Default"} {
			on := []bool{included, startup, def}[j]
			mark, textColor := "[ ]", colorMuted
			if on {
				mark, textColor = "[x]", layoutColors.Check
			}
			style := lipgloss.NewStyle().Foreground(textColor).Background(background)
			if focused && j == m.control {
				style = inverted(style, textColor, background)
			}
			button := style.Render(mark + " " + label)
			if x > 0 && x+3+lipgloss.Width(button) > inner {
				switches = append(switches, "\n")
				x, y = 0, y+1
			} else if x > 0 {
				switches = append(switches, lipgloss.NewStyle().Background(background).Render("   "))
				x += 3
			}
			targets = append(targets, hitTarget{kind: []hitKind{hitMember, hitStartup, hitDefault}[j], index: i, x: x + 1 + padding, y: y + 1, width: lipgloss.Width(button), height: 1})
			switches = append(switches, button)
			x += lipgloss.Width(button)
		}
		content := heading + "\n" + strings.Join(switches, "")
		if !included && (startup || def) {
			content += "\n" + lipgloss.NewStyle().Foreground(colorWarn).Background(background).Width(inner).
				Render("Include this preset or clear its startup/default choices before saving.")
		}
		card := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Background(background).Padding(0, padding).Width(width).Render(content)
		top := body.addCard(card, focused)
		if focused {
			body.focusStart, body.focusEnd = top+targets[m.control].y, top+targets[m.control].y
		}
		for _, target := range targets {
			target.y += top
			body.targets = append(body.targets, target)
		}
		// The name focuses a row; only its explicit switches change its choices.
		body.targets = append(body.targets, hitTarget{kind: hitFocus, index: i, x: 1 + padding, y: top + 1, width: inner, height: lipgloss.Height(heading)})
	}
	focused := m.focus == presetContentFocus && m.cursor == len(ids)
	block := optionCard(Option{Name: "Idle unload · " + idleDuration(l.IdleSeconds), Description: "Inactive models unload after this interval. Startup loading does not keep them resident. Enter to change."}, "", focused, width, layoutColors)
	top := body.addCard(block, focused)
	if focused {
		body.focusStart, body.focusEnd = top+1, top+1
	}
	body.targets = append(body.targets, hitTarget{kind: hitIdle, y: top, width: width, height: lipgloss.Height(block)})
}

func (m *PresetModel) editContent(body *screenBody, width int) {
	title, detail := "New layout", "Give this group of presets a name. You can choose its presets next."
	switch m.field {
	case "rename":
		title, detail = "Rename layout", "Change the display name while keeping this layout's presets and settings."
	case "context":
		title, detail = "Context window", "Total tokens for input and output. A larger window requires more memory."
		if p, ok := m.selectedOption(); ok {
			title += " · " + p.Name
			if refs := m.draft.References(p.ID); len(refs) > 0 {
				var names []string
				for _, id := range refs {
					names = append(names, m.draft.Layouts[id].Name)
				}
				detail += "\nThis shared preset will change in: " + strings.Join(names, ", ") + "."
			}
		}
	case "idle":
		title, detail = "Idle unload · "+m.draft.Layouts[m.layout].Name, "Seconds before an inactive model unloads. Enter a positive number."
	}
	inner := width - 4
	m.text.SetWidth(max(1, inner-3))
	heading := lipgloss.NewStyle().Foreground(colorText).Bold(true).Width(inner).Render(title)
	content := heading + "\n\n" + m.text.View() + "\n\n" + lipgloss.NewStyle().Foreground(colorMuted).Width(inner).Render(detail)
	border := colorBorder
	if m.focus == presetContentFocus {
		border = actionColors.Border
	}
	top := body.add(lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(0, 1).Width(width).Render(content), false)
	// Even a small viewport keeps the field, rather than its explanation, in view.
	inputLine := top + lipgloss.Height(heading) + 2
	if m.focus == presetContentFocus {
		body.focusStart, body.focusEnd = inputLine, inputLine
	}
	body.targets = append(body.targets, hitTarget{kind: hitField, x: 2, y: inputLine, width: inner, height: 1})
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
	for _, section := range sections {
		if section.Downloads == nil {
			continue
		}
		files := make([]Download, 0, len(section.Downloads))
		for _, file := range section.Downloads {
			files = append(files, Download{File: file.Name, Size: setup.DownloadSize(file.Bytes), Status: file.Action()})
		}
		focused := m.focus == presetContentFocus
		block, headingHeight := downloadsBlock(Section{Lines: section.Lines, Downloads: files}, width, m.downloadsOpen, focused)
		body.downloadsTop = body.add(block, false)
		if focused {
			body.focusStart, body.focusEnd = body.downloadsTop+1, body.downloadsTop+1
		}
		body.targets = append(body.targets, hitTarget{kind: hitDownloads, x: 1, y: body.downloadsTop + 1, width: width - 2, height: headingHeight})
	}
	m.reviewLayouts(body, width)
}

func (m *PresetModel) presetFooter(width int, compact bool) (string, []hitTarget) {
	definitions := m.footerButtons()
	buttons := make([]string, len(definitions))
	active := max(0, min(m.action, len(definitions)-1))
	for i, button := range definitions {
		label := button.label
		if width < 70 && (button.target.kind != hitNext || width < 40) {
			label = button.short
		}
		fg := buttonColor(button)
		if !button.available && fg != colorWarn {
			label += " ×" // The refused install already says it cannot run.
		}
		style := lipgloss.NewStyle().Padding(0, 1).Foreground(fg).Background(colorSurface)
		if m.focus == presetFooterFocus && i == active {
			style = inverted(style, fg, colorSurface)
		}
		buttons[i] = style.Render(label)
	}
	// Every action remains in the focus order even when only its neighbors fit.
	start, end := 0, len(buttons)
	row := func() string {
		line := strings.Join(buttons[start:end], " ")
		if start > 0 {
			line = "‹ " + line
		}
		if end < len(buttons) {
			line += " ›"
		}
		return line
	}
	for lipgloss.Width(row()) > width && end-start > 1 {
		if active-start > end-active-1 {
			start++
		} else {
			end--
		}
	}
	var targets []hitTarget
	x := 0
	if start > 0 {
		x = 2
	}
	for i := start; i < end; i++ {
		w := lipgloss.Width(buttons[i])
		targets = append(targets, hitTarget{kind: hitPresetAction, index: i, x: x, width: w, height: 1})
		x += w + 1
	}
	parts := []string{row()}
	if !compact && m.field == "" && m.page == presetReviewPage {
		detail := []string{
			"Enter downloads what is missing, installs it and starts the default layout.",
			"Enter saves your choices. Nothing is downloaded or started.",
			"Enter returns to your layouts.",
			"Enter leaves without saving.",
			"Enter checks downloads and memory again.",
		}[active]
		switch {
		case definitions[active].available:
		case !m.reviewReady || m.loading:
			detail = "Checking downloads and memory…"
		case active == 0:
			detail = "Can't install: " + strings.Join(m.plan.Refusals, "; ")
		}
		parts = append(parts, lipgloss.NewStyle().Foreground(colorMuted).Width(width).Render(detail))
	}
	help := "↑↓ rows · ←→ controls · Enter/Space activate · Tab Next · Shift+Tab Back · PgUp/PgDown scroll · q cancel"
	if compact {
		help = "↑↓ rows · ←→ controls\nEnter/Space activate\nTab Next · ⇧Tab Back · q"
		if m.height < 12 {
			help = "Arrows move · Enter select\nTab Next · ⇧Tab Back · q"
		}
	}
	if m.field != "" {
		help = "↑↓ field/buttons · ←→ move · Enter apply · Tab Apply · Shift+Tab Cancel · Esc cancel"
		if compact {
			help = "↑↓ field/buttons · ←→ edit\nEnter apply · Esc cancel\nTab Apply · ⇧Tab Cancel"
			if m.height < 12 {
				help = "Arrows move · Enter apply\nTab Apply · ⇧Tab Cancel"
			}
		}
	} else if m.page == presetReviewPage {
		help = strings.ReplaceAll(help, "Next", "Install")
	} else if m.layout != "" {
		help = strings.ReplaceAll(help, "Next", "Done")
	} else if m.page == presetHarnessPage {
		help = strings.ReplaceAll(help, "Next", "Review")
	}
	if compact {
		lines := strings.Split(help, "\n")
		for i := range lines {
			lines[i] = ansi.Truncate(lines[i], width, "")
		}
		help = strings.Join(lines, "\n")
	}
	parts = append(parts, lipgloss.NewStyle().Foreground(colorMuted).Width(width).Render(help))
	if m.notice != "" {
		parts = append([]string{lipgloss.NewStyle().Foreground(colorWarn).Render(ansi.Truncate(m.notice, width, "…"))}, parts...)
		for i := range targets {
			targets[i].y++
		}
	}
	return strings.Join(parts, "\n"), targets
}
