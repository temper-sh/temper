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
	brand := lipgloss.NewStyle().Foreground(nightBlue).Bold(true).Render("TEMPER") +
		lipgloss.NewStyle().Foreground(nightMuted).Render("  /  Guided setup")
	if width >= 40 {
		progress := lipgloss.NewStyle().Foreground(nightPurple).Render(fmt.Sprintf("%d / 3", m.page+1))
		brand += strings.Repeat(" ", width-lipgloss.Width(brand)-lipgloss.Width(progress)) + progress
	}
	header := brand
	if !compact && m.input.Machine != "" {
		header += "\n" + lipgloss.NewStyle().Foreground(nightMuted).Width(width).Render(m.input.Machine) + "\n"
	}
	header += "\n" + stepTabs([]string{"Presets", "Layouts", "Review"}, m.page, width)
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
		foot = lipgloss.NewStyle().Foreground(nightMuted).Render(strings.Repeat("─", max(0, width-lipgloss.Width(label)))+label) + "\n" + foot
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
	content = lipgloss.NewStyle().Foreground(nightText).Background(nightBackground).
		Width(m.width).Height(m.height).PaddingLeft(left).PaddingRight(m.width - width - left).Render(content)
	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.BackgroundColor, v.ForegroundColor = nightBackground, nightText
	return v
}

func (m *PresetModel) filters(width int) (string, []hitTarget) {
	padding, margin := 1, 0
	if width >= 40 {
		padding = 2
	}
	if m.height >= 18 {
		margin = 1
	}
	const gap = 3
	labels := []string{"Recommended", "All"}
	if width >= 50 {
		n := 0
		for _, p := range m.input.Presets {
			if p.Recommended {
				n++
			}
		}
		labels = []string{fmt.Sprintf("Recommended %d", n), fmt.Sprintf("All %d", len(m.input.Presets))}
	}
	var buttons []string
	var targets []hitTarget
	x := 0
	for i, label := range labels {
		style := lipgloss.NewStyle().Padding(0, padding).Foreground(nightMuted)
		if (i == 1) == m.all {
			style = style.Foreground(nightCyan).Background(nightSurface).Bold(true)
			if m.focus == presetFilterFocus {
				style = style.Foreground(nightBackground).Background(nightBlue)
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
		row += strings.Repeat(" ", remaining) + lipgloss.NewStyle().Foreground(nightGreen).Render(count)
	}
	return lipgloss.NewStyle().Margin(margin, 0).Render(row), targets
}

func (m *PresetModel) presetContent(width int, compact bool) screenBody {
	body := screenBody{focusStart: -1, downloadsTop: -1}
	if m.field != "" {
		m.editContent(&body, width)
		return body
	}
	title, subtitle := "Choose your presets", "Select the models you want available. Recommendations are starting points; nothing is selected for you."
	if m.page == 1 {
		title, subtitle = "Arrange your layouts", "Group presets for different kinds of work. Local and Utility are editable starting points."
		if m.layout != "" {
			title = m.draft.Layouts[m.layout].Name + " · Presets"
			subtitle = "Include makes a preset available. Load on activation warms it up. Default is optional and does not imply loading."
		}
	} else if m.page == 2 {
		title, subtitle = "Review your setup", "Check downloads, memory and your layouts. Save keeps your choices; Prepare also installs the missing files."
	}
	if !compact {
		body.add(lipgloss.NewStyle().Foreground(nightCyan).Bold(true).Width(width).Render(title)+"\n"+
			lipgloss.NewStyle().Foreground(nightMuted).Width(width).Render(subtitle), false)
	}
	switch m.page {
	case 0:
		rows := m.visible()
		if len(rows) == 0 {
			body.add(infoBlock("No recommended presets", []string{"Open All to browse the catalog and your saved presets."}, width), false)
		}
		for i, p := range rows {
			if i == 0 || p.Tier != rows[i-1].Tier || (p.Unavailable == "") != (rows[i-1].Unavailable == "") {
				group := catalog.MemoryTier(p.Tier).Label()
				if p.Tier != "" {
					group += " · estimated placement"
				}
				if p.Unavailable != "" {
					group = "Unavailable on this machine · " + group
				}
				body.add(lipgloss.NewStyle().Foreground(nightPurple).Bold(true).Width(width).Render(group), false)
			}
			mark, lock := "[ ]", p.Lock
			if selected, ok := m.draft.Presets[p.ID]; ok {
				mark, p.Name, lock = "[x]", selected.Name, selected.Lock
			}
			_, record := presetRecord(lock)
			template := "Model's embedded template"
			if len(record.Patches) > 0 {
				template = strings.Join(record.Patches, ", ")
			}
			focused := m.focus == presetContentFocus && i == m.cursor
			background := nightBackground
			if focused {
				background = nightSurface
			}
			metadata := lipgloss.NewStyle().Background(background)
			details := metadata.Foreground(nightPurple).Render("Context "+tokenCount(record.ContextWindowTokens)+" tokens") +
				metadata.Foreground(nightMuted).Render(" · ") + metadata.Foreground(nightCyan).Render(template)
			if p.Details != "" {
				details = p.Details + "\n" + details
			}
			if strings.HasSuffix(p.Name, " (customized)") {
				details = "Customized preset\n" + details
			}
			card := optionCard(Option{Name: p.heading(background), Description: p.Description,
				Details: details, AssessmentURL: p.AssessmentURL, DisabledReason: p.Unavailable}, mark, focused, width)
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
	case 2:
		m.reviewContent(&body, width)
	}
	if m.notice != "" {
		body.add(infoBlock("Attention", []string{m.notice}, width), false)
	}
	return body
}

func (p PresetOption) heading(background color.Color) string {
	var parts []string
	colors := []color.Color{nightBlue, nightCyan, nightPurple}
	style := lipgloss.NewStyle().Background(background).Bold(true)
	for i, value := range []string{p.Model, weightsLabel(p.Weights), p.Engine} {
		if value != "" {
			parts = append(parts, style.Foreground(colors[i]).Render(value))
		}
	}
	if len(parts) == 0 {
		return p.Name
	}
	return strings.Join(parts, style.Foreground(nightMuted).Render(" · "))
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
		card := optionCard(Option{Name: l.Name, Components: fmt.Sprintf("%d included · %d load on activation · idle %s", len(l.Presets), len(l.Startup), idleDuration(l.IdleSeconds)),
			Description: "Presets: " + m.presetNames(l.Presets) + "\nDefault: " + def,
			Details:     "Enter to edit presets, startup loading and default."}, "", focused, width)
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
		border, background := nightBorder, nightBackground
		focused := m.focus == presetContentFocus && i == m.cursor
		if focused {
			border, background = nightBlue, nightSurface
		}
		inner := width - 2 - 2*padding
		name := m.draft.Presets[id].Name
		for _, p := range m.input.Presets {
			if p.ID == id {
				name = p.heading(background)
				break
			}
		}
		heading := lipgloss.NewStyle().Foreground(nightBlue).Background(background).Bold(true).Width(inner).Render(name)
		included, startup, def := slices.Contains(l.Presets, id), slices.Contains(l.Startup, id), l.Default == id
		var switches []string
		var targets []hitTarget
		x, y := 0, lipgloss.Height(heading)
		for j, label := range []string{"Included", "Load on activation", "Default"} {
			on := []bool{included, startup, def}[j]
			mark, textColor := "[ ]", nightMuted
			if on {
				mark, textColor = "[x]", []color.Color{nightGreen, nightCyan, nightPurple}[j]
			}
			style := lipgloss.NewStyle().Foreground(textColor).Background(background)
			if focused && j == m.control {
				style = style.Foreground(nightBackground).Background(nightBlue).Bold(true)
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
			content += "\n" + lipgloss.NewStyle().Foreground(nightAmber).Background(background).Width(inner).
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
	block := optionCard(Option{Name: "Idle unload · " + idleDuration(l.IdleSeconds), Description: "Inactive models unload after this interval. Startup loading does not keep them resident. Enter to change."}, "", focused, width)
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
	heading := lipgloss.NewStyle().Foreground(nightCyan).Bold(true).Width(inner).Render(title)
	content := heading + "\n\n" + m.text.View() + "\n\n" + lipgloss.NewStyle().Foreground(nightMuted).Width(inner).Render(detail)
	border := nightBorder
	if m.focus == presetContentFocus {
		border = nightBlue
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
		style := lipgloss.NewStyle().Padding(0, 1).Foreground(nightCyan).Background(nightSurface)
		if !button.available {
			style = style.Foreground(nightMuted)
			label += " ×"
		}
		if m.focus == presetFooterFocus && i == active {
			style = style.Foreground(nightBackground).Background(nightBlue).Bold(true)
			if !button.available {
				style = style.Background(nightAmber)
			}
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
	if !compact && m.field == "" && m.page == 2 {
		detail := []string{
			"Save choices only; no downloads or model starts.",
			"Save and prepare missing files; no model starts.",
			"Return to your layouts.",
			"Leave without saving your changes.",
			"Refresh the preview's download and machine checks.",
		}[active]
		if !definitions[active].available {
			detail = "Unavailable until the preview and preparation checks pass."
		}
		parts = append(parts, lipgloss.NewStyle().Foreground(nightMuted).Width(width).Render(detail))
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
	} else if m.page == 2 {
		help = strings.ReplaceAll(help, "Next", "Save")
	} else if m.layout != "" {
		help = strings.ReplaceAll(help, "Next", "Done")
	} else if m.page == 1 {
		help = strings.ReplaceAll(help, "Next", "Review")
	}
	if compact {
		lines := strings.Split(help, "\n")
		for i := range lines {
			lines[i] = ansi.Truncate(lines[i], width, "")
		}
		help = strings.Join(lines, "\n")
	}
	parts = append(parts, lipgloss.NewStyle().Foreground(nightMuted).Width(width).Render(help))
	if m.notice != "" {
		parts = append([]string{lipgloss.NewStyle().Foreground(nightAmber).Render(ansi.Truncate(m.notice, width, "…"))}, parts...)
		for i := range targets {
			targets[i].y++
		}
	}
	return strings.Join(parts, "\n"), targets
}
