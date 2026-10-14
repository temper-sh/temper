package setupui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
)

// One color per meaning, used the same way on every page: the same kind of
// thing always has the same color. Headings and plain text stay neutral. A
// focused element inverts its own colors (see inverted) instead of taking a
// focus hue. Palette "A-mid", chosen by the owner on 2026-10-14.
var (
	colorBackground = lipgloss.Color("#1d1f28")
	colorSurface    = lipgloss.Color("#2a2d3a")
	colorBorder     = lipgloss.Color("#4a4f63")
	colorText       = lipgloss.Color("#e8eaf2")
	colorMuted      = lipgloss.Color("#a0a6ba")
	colorLink       = lipgloss.Color("#70a9ff") // links
	colorGo         = lipgloss.Color("#67e97b") // primary action, chosen, done, fits, cached
	colorWarn       = lipgloss.Color("#ffc34f") // needs attention, unavailable, will download
	colorDanger     = lipgloss.Color("#ff7082") // error, does not fit, remove
	colorModel      = lipgloss.Color("#be95ff") // model names
	colorWeights    = lipgloss.Color("#ffa175") // weights, quantization and templates
	colorEngine     = lipgloss.Color("#3bedd2") // engines and compute
	colorLayout     = lipgloss.Color("#ff83c4") // layouts
	colorNumber     = lipgloss.Color("#72e1fe") // sizes, memory, tokens and counts
)

// inverted is the focused form of an element drawn in fg on bg.
func inverted(style lipgloss.Style, fg, bg color.Color) lipgloss.Style {
	return style.Foreground(bg).Background(fg).Bold(true)
}

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
		style := lipgloss.NewStyle().Padding(0, padding).Foreground(colorMuted).
			BorderForeground(colorBorder)
		switch {
		case i == active:
			border.Bottom, border.BottomLeft, border.BottomRight = " ", "┘", "└"
			style = inverted(style, colorText, colorBackground).BorderForeground(colorText)
			label = "• " + label
		case i < active:
			style = style.Foreground(colorGo)
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
	gap := lipgloss.NewStyle().Foreground(colorBorder).Render(strings.Repeat("─", max(0, width-lipgloss.Width(row))))
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
	border := colorBorder
	if focused {
		border = colorText
	}
	heading := lipgloss.NewStyle().Foreground(colorText).Bold(true).Width(inner).Render(label)
	content := heading + "\n" + lipgloss.NewStyle().Foreground(colorText).Width(inner).Render(strings.Join(section.Lines, "\n"))
	if open {
		content += "\n\n" + downloadTable(section.Downloads, inner)
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).
		Padding(0, 1).Width(width).Render(content), lipgloss.Height(heading)
}

func downloadTable(downloads []Download, width int) string {
	headers := []string{"File", "Size", "On install"}
	// Reserve the four border columns and enough room for a size and status.
	// Explicit widths keep long filenames from cropping the table's last cell.
	columns := []int{width - 41, 13, 24}
	narrow := width < 56
	if narrow {
		headers = []string{"File", "Install"}
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
		Border(lipgloss.NormalBorder()).BorderStyle(lipgloss.NewStyle().Foreground(colorBorder)).
		StyleFunc(func(row, col int) lipgloss.Style {
			style := lipgloss.NewStyle().Padding(0, 1).Width(columns[col]).Foreground(colorText)
			if row == table.HeaderRow {
				return style.Bold(true)
			}
			if col == 1 && !narrow {
				style = style.Foreground(colorNumber)
			}
			if col == len(headers)-1 && row < len(downloads) {
				switch downloads[row].Status {
				case "Cached in Temper", "Cached in Hugging Face":
					style = style.Foreground(colorGo)
				case "May download":
					style = style.Foreground(colorMuted)
				default:
					style = style.Foreground(colorWarn)
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
	border, background, titleColor := colorBorder, colorBackground, colorText
	if focused {
		border, background = colorText, colorSurface
	}
	if strings.Contains(mark, "[x]") || strings.Contains(mark, "(*)") {
		titleColor = colorGo
	}
	if mark != "" {
		name = mark + " " + name
	}
	inner := width - 4
	title := lipgloss.NewStyle().Foreground(titleColor).Background(background).Bold(true)
	heading := title.Width(inner).Render(name)
	if option.Size != "" {
		size := lipgloss.NewStyle().Foreground(colorNumber).Background(background).Render(option.Size)
		nameWidth := max(1, inner-lipgloss.Width(size)-2)
		heading = lipgloss.JoinHorizontal(lipgloss.Top, title.Width(nameWidth).Render(name), "  ", size)
	}
	lines := []string{heading}
	if option.Components != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorNumber).Background(background).Width(inner).Render(option.Components))
	}
	if option.Advice != nil {
		warning := "! " + option.Advice.Title
		if len(option.Advice.Lines) > 0 {
			warning += "\n" + option.Advice.Lines[0]
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(colorWarn).Background(background).Bold(true).Width(inner).Render(warning))
	}
	if option.Description != "" {
		lines = append(lines, "", lipgloss.NewStyle().Foreground(colorText).Background(background).Width(inner).Render(option.Description))
	}
	if option.AssessmentURL != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorLink).Background(background).Width(inner).Render("Assessment: "+option.AssessmentURL))
	}
	if option.Details != "" {
		lines = append(lines, "", lipgloss.NewStyle().Foreground(colorMuted).Background(background).Width(inner).Render(option.Details))
	}
	if option.DisabledReason != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorWarn).Background(background).Width(inner).Render("unavailable: "+option.DisabledReason))
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).
		Background(background).Padding(0, 1).Width(width).Render(strings.Join(lines, "\n"))
}

func infoBlock(title string, lines []string, width int) string {
	heading, border := colorText, colorBorder
	if title == "Attention" || title == "Installation is not possible yet" {
		heading, border = colorWarn, colorWarn
	} else if title == "Preview error" {
		heading, border = colorDanger, colorDanger
	}
	inner := width - 4
	content := lipgloss.NewStyle().Foreground(heading).Bold(true).Width(inner).Render(title) + "\n" +
		lipgloss.NewStyle().Foreground(colorText).Width(inner).Render(strings.Join(lines, "\n"))
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).
		Padding(0, 1).Width(width).Render(content)
}

func warningBlock(title string, lines []string, width int) string {
	inner := width - 4
	heading := lipgloss.NewStyle().Foreground(colorWarn).Bold(true).Width(inner).Render("! " + title)
	content := lipgloss.NewStyle().Foreground(colorText).Width(inner).Render(strings.Join(lines, "\n"))
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorWarn).
		Padding(0, 1).Width(width).Render(heading + "\n" + content)
}
