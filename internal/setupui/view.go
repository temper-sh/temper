package setupui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/catalog"
)

// Tokyo Night: https://github.com/enkia/tokyo-night-vscode-theme#color-palette.
// Keep text brighter than the theme's comment color; borders can be subdued.
var (
	nightBackground = lipgloss.Color("#1a1b26")
	nightSurface    = lipgloss.Color("#24283b")
	nightText       = lipgloss.Color("#c0caf5")
	nightMuted      = lipgloss.Color("#9aa5ce")
	nightBorder     = lipgloss.Color("#414868")
	nightBlue       = lipgloss.Color("#7aa2f7")
	nightCyan       = lipgloss.Color("#7dcfff")
	nightGreen      = lipgloss.Color("#9ece6a")
	nightAmber      = lipgloss.Color("#e0af68")
	nightRed        = lipgloss.Color("#f7768e")
	nightPurple     = lipgloss.Color("#bb9af7")
)

type screenTab struct {
	label     string
	stage     stage
	modeAt    int
	profileAt int
}

type hitKind uint8

const (
	hitNext hitKind = iota
	hitChoice
	hitReview
	hitDownloads
	hitInstall
)

type hitTarget struct {
	kind                hitKind
	index               int
	x, y, width, height int
}

func (t hitTarget) contains(x, y int) bool {
	return x >= t.x && x < t.x+t.width && y >= t.y && y < t.y+t.height
}

func (m Model) tabs() ([]screenTab, int) {
	tabs := []screenTab{{label: "Modes", stage: stageModes}}
	modes := m.chosenModes()
	if len(modes) == 0 {
		tabs = append(tabs, screenTab{label: "Models", stage: stageProfile})
	}
	for i, mode := range modes {
		tabs = append(tabs, screenTab{label: m.modeName(mode), stage: stageProfile, modeAt: i})
		profiles := m.chosenProfiles(mode)
		for j, profile := range profiles {
			suffix := ""
			if len(profiles) > 1 {
				suffix = fmt.Sprintf(" %d/%d", j+1, len(profiles))
			}
			if hasTemplateChoices(profile) {
				tabs = append(tabs, screenTab{label: "Templates" + suffix, stage: stageTemplates, modeAt: i, profileAt: j})
			}
			if len(profile.Contexts) > 0 {
				tabs = append(tabs, screenTab{label: "Context" + suffix, stage: stageContext, modeAt: i, profileAt: j})
			}
		}
	}
	tabs = append(tabs, screenTab{label: "Software", stage: stageSoftware}, screenTab{label: "Review", stage: stageReview})
	active := 0
	for i, tab := range tabs {
		if (m.stage == stageTemplates || m.stage == stageContext) && tab.profileAt != m.profileAt {
			continue
		}
		if tab.stage == m.stage && (m.stage != stageProfile && m.stage != stageTemplates && m.stage != stageContext || tab.modeAt == m.modeAt) {
			active = i
		}
	}
	return tabs, active
}

func (m Model) modeName(id string) string {
	for _, mode := range m.input.Modes {
		if mode.ID == id && mode.Name != "" {
			return mode.Name
		}
	}
	return id
}

func (m Model) tabBar(width int) string {
	tabs, active := m.tabs()
	items := make([]string, len(tabs))
	for i, tab := range tabs {
		style := lipgloss.NewStyle().Padding(0, 1).Foreground(nightMuted).
			Border(lipgloss.NormalBorder(), false, false, true, false).BorderForeground(nightBorder)
		label := tab.label
		switch {
		case i == active:
			style = style.Background(nightSurface).Foreground(nightBlue).Bold(true).BorderForeground(nightBlue)
			label = "• " + label
		case i < active:
			style = style.Foreground(nightGreen)
			label = "✓ " + label
		}
		items[i] = style.Render(label)
	}
	// Keep whole tabs, including the active one, on a single row. Chevrons
	// disclose the steps outside the visible range on narrow terminals.
	start, end := 0, len(items)
	visible := func() string {
		parts := append([]string(nil), items[start:end]...)
		if start > 0 {
			parts = append([]string{"‹\n "}, parts...)
		}
		if end < len(items) {
			parts = append(parts, "›\n ")
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	}
	for lipgloss.Width(visible()) > width && end-start > 1 {
		if active-start > end-active-1 {
			start++
		} else {
			end--
		}
	}
	return visible()
}

func (m *Model) View() tea.View {
	m.hitTargets = nil
	if m.width < 28 || m.height < 10 {
		content := lipgloss.NewStyle().Width(m.width).MaxHeight(m.height).
			Render("Enlarge terminal to 28 × 10.\nq to cancel")
		v := tea.NewView(content)
		v.AltScreen = true
		return v
	}
	width := min(108, m.width-4)
	if m.width < 50 {
		width = m.width - 2
	}
	compact := m.height < 18 || m.width < 50
	tabs, active := m.tabs()
	brand := lipgloss.NewStyle().Foreground(nightBlue).Bold(true).Render("TEMPER") +
		lipgloss.NewStyle().Foreground(nightMuted).Render("  /  Guided setup")
	progress := lipgloss.NewStyle().Foreground(nightPurple).Render(fmt.Sprintf("%d / %d", active+1, len(tabs)))
	if width >= 40 {
		brand += strings.Repeat(" ", max(1, width-lipgloss.Width(brand)-lipgloss.Width(progress))) + progress
	}
	header := brand
	if !compact && m.input.Machine != "" {
		header += "\n" + lipgloss.NewStyle().Foreground(nightMuted).Width(width).Render(m.input.Machine) + "\n"
	} else if compact && m.stage == stageModes && m.height >= 12 {
		header += "\n" + lipgloss.NewStyle().Foreground(nightMuted).Render(ansi.Truncate(m.input.Machine, width, "…"))
	}
	header += "\n" + m.tabBar(width)
	foot, footerTargets := m.footer(width, compact)
	body := m.screenContent(width, compact)
	m.viewport.SetWidth(width)
	footerHeight := lipgloss.Height(foot)
	if !compact {
		footerHeight++ // Fixed divider and scroll position above the controls.
	}
	m.viewport.SetHeight(max(1, m.height-lipgloss.Height(header)-footerHeight))
	m.viewport.SetContent(strings.Join(body.blocks, "\n\n"))
	if m.stage != stageReview && m.focusSelection && body.focusStart >= 0 {
		m.viewport.EnsureVisible(body.focusEnd, 0, 0)
		m.viewport.EnsureVisible(body.focusStart, 0, 0)
	}
	m.focusSelection = false
	if m.focusDownloads && body.downloadsTop >= 0 {
		m.viewport.SetYOffset(body.downloadsTop)
	}
	m.focusDownloads = false
	if !compact {
		label := ""
		if m.viewport.TotalLineCount() > m.viewport.Height() {
			keys := "trackpad / PgUp/PgDown"
			if m.stage == stageReview {
				keys = "↑↓ / trackpad"
			}
			label = fmt.Sprintf(" %.0f%% · %s scroll ", m.viewport.ScrollPercent()*100, keys)
		}
		divider := strings.Repeat("─", max(0, width-lipgloss.Width(label))) + label
		foot = lipgloss.NewStyle().Foreground(nightMuted).Render(divider) + "\n" + foot
	}
	content := header + "\n" + m.viewport.View() + "\n" + foot
	left := (m.width - width) / 2
	bodyTop := lipgloss.Height(header)
	for _, target := range body.targets {
		top := target.y - m.viewport.YOffset()
		bottom := min(m.viewport.Height(), top+target.height)
		top = max(0, top)
		if bottom > top {
			target.x += left
			target.y, target.height = bodyTop+top, bottom-top
			m.hitTargets = append(m.hitTargets, target)
		}
	}
	footerTop := bodyTop + m.viewport.Height()
	if !compact {
		footerTop++ // Divider precedes footer controls.
	}
	for _, target := range footerTargets {
		target.x += left
		target.y += footerTop
		m.hitTargets = append(m.hitTargets, target)
	}
	content = lipgloss.NewStyle().Foreground(nightText).Background(nightBackground).
		Width(m.width).Height(m.height).PaddingLeft(left).PaddingRight(m.width - width - left).Render(content)
	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.BackgroundColor, v.ForegroundColor = nightBackground, nightText
	return v
}

type screenBody struct {
	blocks               []string
	height               int
	focusStart, focusEnd int
	downloadsTop         int
	targets              []hitTarget
}

func (b *screenBody) add(block string, focused bool) int {
	if len(b.blocks) > 0 {
		b.height++ // One blank line between blocks; heights already count lines.
	}
	start := b.height
	if focused {
		// The title is just inside the card's border. End excludes the bottom
		// border so short viewports prioritize readable content over decoration.
		b.focusStart = b.height + 1
		b.focusEnd = b.height + lipgloss.Height(block) - 2
	}
	b.blocks = append(b.blocks, block)
	b.height += lipgloss.Height(block)
	return start
}

func (m Model) screenContent(width int, compact bool) screenBody {
	body := screenBody{focusStart: -1, downloadsTop: -1}
	var title, subtitle string
	switch m.stage {
	case stageModes:
		title, subtitle = "Choose your modes", "Select local models, utility helpers, or both. Modes run separately."
	case stageProfile:
		title, subtitle = m.modeName(m.currentMode())+" · Model setup", "Choose a profile for this mode. Machine limits appear with each choice."
		if m.currentMode() == "local" {
			subtitle = "Space selects models to install. Enter or d chooses the default. Only the chosen model runs; alternatives stay installed."
		}
	case stageTemplates:
		title, subtitle = m.modeName(m.currentMode())+" · Templates", "Keep the proposed defaults or choose another template for each model."
	case stageContext:
		title, subtitle = m.modeName(m.currentMode())+" · Context", "Choose how much text the model can keep in context. Automatic is available where reviewed measurements apply."
	case stageSoftware:
		title, subtitle = "Choose software", "Select which engine and router releases Temper should prepare."
	case stageReview:
		title, subtitle = "Review selected setup", "Check your selections, downloads and machine limits before continuing."
	}
	if m.stage == stageTemplates || m.stage == stageContext {
		if profile, ok := m.currentProfile(); ok {
			title += " · " + profile.Name
		}
	}
	if !compact {
		body.add(lipgloss.NewStyle().Foreground(nightCyan).Bold(true).Width(width).Render(title)+"\n"+
			lipgloss.NewStyle().Foreground(nightMuted).Width(width).Render(subtitle), false)
	}
	addOption := func(row int, mark string, option Option) {
		card := m.optionCard(row, mark, option, width)
		top := body.add(card, row == m.cursor)
		body.targets = append(body.targets, hitTarget{kind: hitChoice, index: row, y: top, width: width, height: lipgloss.Height(card)})
		if row == m.cursor && option.Advice != nil {
			body.add(warningBlock(option.Advice.Title, option.Advice.Lines, width), false)
		}
	}
	switch m.stage {
	case stageModes:
		if compact && m.input.Machine != "" {
			body.add(infoBlock("Your machine", []string{m.input.Machine}, width), false)
		}
		for i, option := range m.input.Modes {
			mark := "[ ]"
			if m.selected[option.ID] {
				mark = "[x]"
			}
			addOption(i, mark, option)
		}
		body.add(lipgloss.NewStyle().Foreground(nightMuted).Width(width).Render("Off is always available. Nothing starts during setup."), false)
	case stageProfile:
		profiles := m.availableProfiles()
		if len(profiles) == 0 {
			body.add(infoBlock("No profiles available", []string{"No catalog profile is available for this mode. Return to Modes to choose another."}, width), false)
		}
		for i, profile := range profiles {
			group := catalog.MemoryTier(profile.MemoryTier).Label()
			if profile.MemoryTier != "" {
				group += " · estimated placement"
			}
			if profile.DisabledReason != "" {
				group = "Unavailable on this machine · " + group
			}
			if i == 0 || profile.MemoryTier != profiles[i-1].MemoryTier || (profile.DisabledReason == "") != (profiles[i-1].DisabledReason == "") {
				body.add(lipgloss.NewStyle().Foreground(nightPurple).Bold(true).Width(width).Render(group), false)
			}
			mark := "( )"
			if m.profiles[m.currentMode()] == profile.ID {
				mark = "(*)"
			}
			if m.currentMode() == "local" {
				check := "[ ]"
				if m.profiles["local"] == profile.ID || m.additional[profile.ID] {
					check = "[x]"
				}
				mark = check + " " + mark
				// The checkbox toggles installation; the rest of the card chooses
				// the default, matching Space and Enter respectively.
				top := body.height + 1
				body.targets = append(body.targets, hitTarget{kind: hitInstall, index: i, x: 4, y: top + 1, width: 3, height: 1})
			}
			addOption(i, mark, profile.Option)
		}
	case stageTemplates:
		if profile, ok := m.currentProfile(); ok {
			row := 0
			for _, template := range profile.Templates {
				if len(templateChoices(template)) < 2 {
					continue
				}
				body.add(lipgloss.NewStyle().Foreground(nightPurple).Bold(true).Width(width).Render(template.Name), false)
				for _, option := range template.Options {
					mark := "( )"
					if m.patches[m.currentMode()][profile.ID][template.Layout] == option.ID {
						mark = "(*)"
					}
					if option.ID == template.Default {
						option.Description = strings.TrimSpace("Proposed default. " + option.Description)
					}
					addOption(row, mark, option)
					row++
				}
			}
		}
	case stageContext:
		if profile, ok := m.currentProfile(); ok {
			for i, window := range profile.Contexts {
				card, inputOffset := m.contextCard(profile, window, i, width)
				top := body.add(card, false)
				if i == m.cursor {
					body.focusStart, body.focusEnd = top+inputOffset, top+inputOffset
				}
				body.targets = append(body.targets, hitTarget{kind: hitChoice, index: i, y: top, width: width, height: lipgloss.Height(card)})
			}
		}
		body.add(infoBlock("Context and memory", []string{"The window includes input and output. Enter a number where automatic context is unavailable. Otherwise, automatic selects the largest tested window matching this machine, template and the software selected next.", "Ctrl+U clears the field; a restores automatic where available. A model's limit alone does not establish memory fit."}, width), false)
	case stageSoftware:
		for i, option := range softwareOptions {
			mark := "( )"
			if m.software == option.ID {
				mark = "(*)"
			}
			addOption(i, mark, m.softwareOption(option))
		}
	case stageReview:
		if m.loading {
			body.add(infoBlock("Calculating preview…", []string{"Resolving your choices and checking downloads and machine limits.", "Latest/Tested may read software archives to verify their contents."}, width), false)
		} else if m.reviewErr != nil {
			body.add(infoBlock("Preview error", []string{m.reviewErr.Error(), "Press r to retry, or go back to change your choices."}, width), false)
		} else {
			// Memory actions lead review, ahead of even the transfer summary.
			for _, section := range m.review.Sections {
				if section.Warning {
					body.add(warningBlock(section.Title, section.Lines, width), false)
				}
			}
			for _, section := range m.review.Sections {
				if section.Downloads != nil {
					block, headingHeight := m.downloadsBlock(section, width)
					body.downloadsTop = body.add(block, false)
					body.targets = append(body.targets, hitTarget{kind: hitDownloads, x: 1, y: body.downloadsTop + 1, width: width - 2, height: headingHeight})
				}
			}
			if m.software != "" {
				body.add(lipgloss.NewStyle().Foreground(nightPurple).Width(width).Render("Software: "+m.software), false)
			}
			for _, section := range m.review.Sections {
				if section.Downloads == nil && !section.Warning {
					body.add(infoBlock(section.Title, section.Lines, width), false)
				}
			}
		}
		if !m.review.CanPrepare && m.review.PrepareReason != "" {
			body.add(infoBlock("Preparation unavailable", []string{m.review.PrepareReason}, width), false)
		}
	}
	if m.notice != "" {
		body.add(infoBlock("Attention", []string{m.notice}, width), false)
	}
	return body
}

func (m Model) downloadsBlock(section Section, width int) (string, int) {
	inner := width - 4
	label := fmt.Sprintf("▸ Downloads (%d files) · d expand", len(section.Downloads))
	if m.downloadsOpen {
		label = fmt.Sprintf("▾ Downloads (%d files) · d collapse", len(section.Downloads))
	}
	heading := lipgloss.NewStyle().Foreground(nightCyan).Bold(true).Width(inner).Render(label)
	content := heading + "\n" + lipgloss.NewStyle().Foreground(nightText).Width(inner).Render(strings.Join(section.Lines, "\n"))
	if m.downloadsOpen {
		content += "\n\n" + downloadTable(section.Downloads, inner)
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(nightBorder).
		Padding(0, 1).Width(width).Render(content), lipgloss.Height(heading)
}

func downloadTable(downloads []Download, width int) string {
	headers := []string{"File", "Size", "On Prepare"}
	// Reserve the four border columns and enough room for a size and status.
	// Explicit widths keep long filenames from cropping the table's last cell.
	columns := []int{width - 41, 13, 24}
	narrow := width < 56
	if narrow {
		headers = []string{"File", "Prepare"}
		fileWidth := (width - 3) / 2
		columns = []int{fileWidth, width - 3 - fileWidth}
	}
	rows := make([][]string, 0, len(downloads))
	for _, item := range downloads {
		row := []string{item.File, item.Size, item.Status}
		if narrow {
			row = []string{item.File, item.Size + "\n" + item.Status}
		}
		rows = append(rows, row)
	}
	return table.New().Headers(headers...).Rows(rows...).Width(width).Wrap(true).
		Border(lipgloss.NormalBorder()).BorderStyle(lipgloss.NewStyle().Foreground(nightBorder)).
		StyleFunc(func(row, col int) lipgloss.Style {
			style := lipgloss.NewStyle().Padding(0, 1).Width(columns[col]).Foreground(nightText)
			if row == table.HeaderRow {
				return style.Foreground(nightCyan).Bold(true)
			}
			if col == len(headers)-1 && row < len(downloads) {
				switch downloads[row].Status {
				case "Cached in Temper", "Cached in Hugging Face":
					style = style.Foreground(nightGreen)
				case "May download":
					style = style.Foreground(nightMuted)
				default:
					style = style.Foreground(nightAmber)
				}
			}
			return style
		}).String()
}

func (m Model) optionCard(row int, mark string, option Option, width int) string {
	name := option.Name
	if name == "" {
		name = option.ID
	}
	border, background, titleColor := nightBorder, nightBackground, nightText
	prefix := "  "
	if row == m.cursor {
		border, background = nightBlue, nightSurface
		prefix = "> "
	}
	if strings.Contains(mark, "[x]") || strings.Contains(mark, "(*)") {
		titleColor = nightGreen
	}
	inner := width - 4
	lines := []string{lipgloss.NewStyle().Foreground(titleColor).Background(background).Bold(true).Width(inner).Render(prefix + mark + " " + name)}
	if option.Components != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(nightCyan).Background(background).Width(inner).Render(option.Components))
	}
	if option.Advice != nil {
		warning := "! " + option.Advice.Title
		if len(option.Advice.Lines) > 0 {
			warning += "\n" + option.Advice.Lines[0]
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(nightAmber).Background(background).Bold(true).Width(inner).Render(warning))
	}
	if option.Description != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(nightText).Background(background).Width(inner).Render(option.Description))
	}
	if option.AssessmentURL != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(nightCyan).Background(background).Width(inner).Render("Assessment: "+option.AssessmentURL))
	}
	if option.Details != "" {
		lines = append(lines, "", lipgloss.NewStyle().Foreground(nightMuted).Background(background).Width(inner).Render(option.Details))
	}
	if option.DisabledReason != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(nightAmber).Background(background).Width(inner).Render("unavailable: "+option.DisabledReason))
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).
		Background(background).Padding(0, 1).Width(width).Render(strings.Join(lines, "\n"))
}

func infoBlock(title string, lines []string, width int) string {
	color := nightCyan
	if title == "Attention" || title == "Preparation unavailable" {
		color = nightAmber
	} else if title == "Preview error" {
		color = nightRed
	}
	inner := width - 4
	heading := lipgloss.NewStyle().Foreground(color).Bold(true).Width(inner).Render(title)
	content := lipgloss.NewStyle().Foreground(nightText).Width(inner).Render(strings.Join(lines, "\n"))
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(nightBorder).
		Padding(0, 1).Width(width).Render(heading + "\n" + content)
}

func warningBlock(title string, lines []string, width int) string {
	inner := width - 4
	heading := lipgloss.NewStyle().Foreground(nightAmber).Bold(true).Width(inner).Render("! " + title)
	content := lipgloss.NewStyle().Foreground(nightText).Width(inner).Render(strings.Join(lines, "\n"))
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(nightAmber).
		Padding(0, 1).Width(width).Render(heading + "\n" + content)
}

func (m Model) footer(width int, compact bool) (string, []hitTarget) {
	var parts []string
	var targets []hitTarget
	if m.notice != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(nightAmber).Render(ansi.Truncate(m.notice, width, "…")))
	}
	if m.stage == stageReview {
		actions, buttons := m.reviewActions(width)
		targets = buttons
		parts = append(parts, actions)
		if !compact {
			detail := []string{"Save choices only; no model downloads.", "Save, download missing files and verify cached weights; no model starts.", "Return to software choices.", "Leave setup without saving."}[m.cursor]
			if !m.reviewActionAvailable(m.cursor) {
				detail = "Unavailable until the preview and preparation checks pass."
			}
			parts = append(parts, lipgloss.NewStyle().Foreground(nightMuted).Width(width).Render(detail))
		}
	} else {
		style := lipgloss.NewStyle().Padding(0, 1).Foreground(nightCyan).Background(nightSurface).Bold(true)
		if m.cursor == m.optionCount() {
			style = style.Foreground(nightBackground).Background(nightBlue)
		}
		button := style.Render("Next →")
		targets = []hitTarget{{kind: hitNext, width: lipgloss.Width(button), height: 1}}
		parts = append(parts, button)
	}
	if m.notice != "" {
		for i := range targets {
			targets[i].y++
		}
	}
	help := "↑↓ move · Enter select/next · Tab/n next · Esc back · q cancel"
	if m.stage == stageReview {
		help = "↑↓ scroll · ←→/Tab action · Enter confirm · d downloads · r retry · Esc back · q cancel"
	} else if compact {
		help = "↑↓ move · Enter select/next\nTab/n next · Esc back · q"
	}
	if compact && m.stage == stageReview {
		help = "↑↓ scroll · ←→ action\nEnter confirm · d files\nEsc back · q cancel"
	}
	if m.stage == stageContext {
		help = "↑↓ field · ←→ edit · Ctrl+U clear · a automatic · Enter/Tab next · Esc back · q cancel"
		if compact {
			help = "↑↓ field · ←→ edit\nCtrl+U clear · a auto\nTab next · Esc back · q"
		}
	}
	if m.stage == stageProfile && m.currentMode() == "local" {
		help = "↑↓ move · Space install · Enter/d default · Tab/n next · Esc back · q cancel"
		if compact {
			help = "↑↓ move · Space install\nEnter/d default · Tab next\nEsc back · q cancel"
		}
	}
	parts = append(parts, lipgloss.NewStyle().Foreground(nightMuted).Width(width).Render(help))
	return strings.Join(parts, "\n"), targets
}

func (m Model) reviewActionAvailable(index int) bool {
	if index >= 2 {
		return true
	}
	return !m.loading && m.reviewErr == nil && m.review.Token != "" && (index != 1 || m.review.CanPrepare)
}

func (m Model) reviewActions(width int) (string, []hitTarget) {
	names := []string{"Save configuration", "Prepare installation", "Back", "Cancel"}
	if width < 34 {
		names = []string{"Save", "Prepare", "Back", "Cancel"}
	}
	buttons := make([]string, len(names))
	for i, name := range names {
		style := lipgloss.NewStyle().Padding(0, 1).Foreground(nightText).Background(nightSurface)
		prefix := "  "
		if !m.reviewActionAvailable(i) {
			style = style.Foreground(nightMuted)
			name += " ×"
		}
		if i == m.cursor {
			prefix = "> "
			style = style.Foreground(nightBackground).Background(nightBlue).Bold(true)
			if !m.reviewActionAvailable(i) {
				style = style.Background(nightAmber)
			}
		}
		buttons[i] = style.Render(prefix + name)
	}
	row := strings.Join(buttons, " ")
	if lipgloss.Width(row) > width {
		row = buttons[m.cursor] + lipgloss.NewStyle().Foreground(nightMuted).Render(fmt.Sprintf(" %d/4", m.cursor+1))
		return row, []hitTarget{{kind: hitReview, index: m.cursor, width: lipgloss.Width(buttons[m.cursor]), height: 1}}
	}
	var targets []hitTarget
	x := 0
	for i, button := range buttons {
		w := lipgloss.Width(button)
		targets = append(targets, hitTarget{kind: hitReview, index: i, x: x, width: w, height: 1})
		x += w + 1
	}
	return row, targets
}
