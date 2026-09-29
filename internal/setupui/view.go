package setupui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
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

type hitKind uint8

const (
	hitNext hitKind = iota
	hitChoice
	hitReview
	hitDownloads
	hitInstall
	hitShortcut
	hitFilter
	hitMember
	hitStartup
	hitDefault
	hitFocus
	hitPresetAction
	hitField
	hitIdle
)

type hitTarget struct {
	kind                hitKind
	index               int
	key                 string
	x, y, width, height int
}

func (t hitTarget) contains(x, y int) bool {
	return x >= t.x && x < t.x+t.width && y >= t.y && y < t.y+t.height
}

func stepTabs(labels []string, active, width int) string {
	padding := 3
	if width < 50 {
		padding = 1
	}
	items := make([]string, len(labels))
	for i, label := range labels {
		border := lipgloss.RoundedBorder()
		border.BottomLeft, border.BottomRight = "┴", "┴"
		style := lipgloss.NewStyle().Padding(0, padding).Foreground(nightMuted).
			BorderForeground(nightBorder)
		switch {
		case i == active:
			border.Bottom, border.BottomLeft, border.BottomRight = " ", "┘", "└"
			style = style.Foreground(nightBlue).Bold(true).BorderForeground(nightBlue)
			label = "• " + label
		case i < active:
			style = style.Foreground(nightGreen)
			label = "✓ " + label
		}
		items[i] = style.Border(border).Render(label)
	}
	// Keep whole tabs, including the active one, on a single row. Chevrons
	// disclose the steps outside the visible range on narrow terminals.
	start, end := 0, len(items)
	visible := func() string {
		parts := append([]string(nil), items[start:end]...)
		if start > 0 {
			parts = append([]string{"\n‹\n─"}, parts...)
		}
		if end < len(items) {
			parts = append(parts, "\n›\n─")
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
	row := visible()
	gap := lipgloss.NewStyle().Foreground(nightBorder).Render(strings.Repeat("─", max(0, width-lipgloss.Width(row))))
	return lipgloss.JoinHorizontal(lipgloss.Bottom, row, gap)
}

type screenBody struct {
	blocks               []string
	height               int
	lastCard             bool
	focusStart, focusEnd int
	downloadsTop         int
	targets              []hitTarget
}

func (b *screenBody) add(block string, focused bool) int {
	return b.addBlock(block, focused, false)
}

func (b *screenBody) addCard(block string, focused bool) int {
	return b.addBlock(block, focused, true)
}

func (b *screenBody) addBlock(block string, focused, card bool) int {
	if len(b.blocks) > 0 && !(card && b.lastCard) {
		b.blocks = append(b.blocks, "")
		b.height++ // Keep a blank line between sections, but stack adjacent cards.
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
	b.lastCard = card
	return start
}

func downloadsBlock(section Section, width int, open, focused bool) (string, int) {
	inner := width - 4
	unit := "files"
	if len(section.Downloads) == 1 {
		unit = "file"
	}
	label := fmt.Sprintf("▸ Downloads (%d %s) · d expand", len(section.Downloads), unit)
	if open {
		label = fmt.Sprintf("▾ Downloads (%d %s) · d collapse", len(section.Downloads), unit)
	}
	border := nightBorder
	if focused {
		border = nightBlue
	}
	heading := lipgloss.NewStyle().Foreground(nightCyan).Bold(true).Width(inner).Render(label)
	content := heading + "\n" + lipgloss.NewStyle().Foreground(nightText).Width(inner).Render(strings.Join(section.Lines, "\n"))
	if open {
		content += "\n\n" + downloadTable(section.Downloads, inner)
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).
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

func optionCard(option Option, mark string, focused bool, width int) string {
	name := option.Name
	if name == "" {
		name = option.ID
	}
	border, background, titleColor := nightBorder, nightBackground, nightBlue
	if focused {
		border, background = nightBlue, nightSurface
	}
	if strings.Contains(mark, "[x]") || strings.Contains(mark, "(*)") {
		titleColor = nightGreen
	}
	if mark != "" {
		name = mark + " " + name
	}
	inner := width - 4
	lines := []string{lipgloss.NewStyle().Foreground(titleColor).Background(background).Bold(true).Width(inner).Render(name)}
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
		lines = append(lines, "", lipgloss.NewStyle().Foreground(nightText).Background(background).Width(inner).Render(option.Description))
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
