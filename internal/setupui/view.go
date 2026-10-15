package setupui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
)

// family is one kind of element's set of matching colors. Each kind of
// element has its own family, so a page shows a few clearly separate groups:
// preset cards, main navigation, secondary navigation, actions, layouts and
// the review. Inside a family the colors match; a preset card draws model,
// weights and engine as one gradient and takes every accent from it.
// Warnings stay amber and red everywhere.
type family struct {
	Name    [3]color.Color // a gradient: first, second, third
	Accent  color.Color    // numbers and details
	Link    color.Color
	Check   color.Color // chosen and done marks
	Border  color.Color // a focused element's border
	Surface color.Color // a focused element's background
}

func shades(first, second, third, accent, link, check, border, surface string) family {
	c := lipgloss.Color
	return family{Name: [3]color.Color{c(first), c(second), c(third)}, Accent: c(accent), Link: c(link), Check: c(check), Border: c(border), Surface: c(surface)}
}

// A neutral base keeps the families apart. A focused element inverts its own
// colors (see inverted).
var (
	colorBackground = lipgloss.Color("#1b1c22")
	colorSurface    = lipgloss.Color("#262833")
	colorBorder     = lipgloss.Color("#43465a")
	colorText       = lipgloss.Color("#eceef4")
	colorMuted      = lipgloss.Color("#a3a7b7")
	colorWarn       = lipgloss.Color("#ffc34f")
	colorDanger     = lipgloss.Color("#ff7082")

	// Preset cards: royal purple → indigo → blue for model, weights, engine.
	presetColors = shades("#b996ff", "#9b94ff", "#7ea3ff", "#c6cbff", "#9fb8ff", "#9b94ff", "#9b94ff", "#262341")
	// The TEMPER title above the step tabs.
	colorBrand = lipgloss.Color("#ffffff")
	// Main navigation: the step tabs. The current tab is yellow on dark and
	// done tabs are green.
	mainNavColors = shades("#1b1c23", "#ffb3d9", "#ffd1e8", "#ffb3d9", "#ff8fc8", "#009a54", "#e7ce54", "#33202c")
	// Secondary navigation: group filters, group and section headings. The
	// chosen filter is light text on green.
	secondaryNavColors = shades("#eceef4", "#7fe0d6", "#a8eae3", "#009a54", "#4fd6c8", "#4fd6c8", "#4fd6c8", "#009a54")
	// Actions: the footer buttons; the primary action first.
	actionColors = shades("#ff9e6b", "#ffc9a8", "#ffe0cc", "#ffc9a8", "#ff9e6b", "#ff9e6b", "#ff9e6b", "#2e241f")
	// Layout cards and the layout tables on Review: mint to sea green.
	layoutColors = shades("#5fe3a1", "#6fe0b8", "#7fdcc9", "#b3f0cf", "#7fe0b8", "#5fe3a1", "#5fe3a1", "#1c2c27")
	// Review downloads: sky blue.
	reviewColors = shades("#5cc4ff", "#8fd4ff", "#b9e5ff", "#cdeaff", "#7cc9ff", "#5cc4ff", "#5cc4ff", "#1a2838")
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
			style = style.Foreground(mainNavColors.Border).Background(mainNavColors.Name[0]).Bold(true).BorderForeground(mainNavColors.Border)
			label = "• " + label
		case i < active:
			style = style.Foreground(mainNavColors.Check)
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
		border = reviewColors.Border
	}
	heading := lipgloss.NewStyle().Foreground(reviewColors.Name[0]).Bold(true).Width(inner).Render(label)
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
				return style.Foreground(reviewColors.Name[0]).Bold(true)
			}
			if col == 1 && !narrow {
				style = style.Foreground(reviewColors.Accent)
			}
			if col == len(headers)-1 && row < len(downloads) {
				switch downloads[row].Status {
				case "Cached in Temper", "Cached in Hugging Face":
					style = style.Foreground(reviewColors.Check)
				case "May download":
					style = style.Foreground(colorMuted)
				default:
					style = style.Foreground(reviewColors.Name[1])
				}
			}
			return style
		}).String()
}

func optionCard(option Option, mark string, focused bool, width int, colors family) string {
	name := option.Name
	if name == "" {
		name = option.ID
	}
	border, background, titleColor := colorBorder, colorBackground, colorText
	if focused {
		border, background = colors.Border, colors.Surface
	}
	if strings.Contains(mark, "[x]") || strings.Contains(mark, "(*)") {
		titleColor = colors.Check
	}
	if mark != "" {
		name = mark + " " + name
	}
	inner := width - 4
	title := lipgloss.NewStyle().Foreground(titleColor).Background(background).Bold(true)
	heading := title.Width(inner).Render(name)
	if option.Size != "" {
		size := lipgloss.NewStyle().Foreground(colors.Accent).Background(background).Render(option.Size)
		nameWidth := max(1, inner-lipgloss.Width(size)-2)
		heading = lipgloss.JoinHorizontal(lipgloss.Top, title.Width(nameWidth).Render(name), "  ", size)
	}
	lines := []string{heading}
	if option.Components != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(colors.Accent).Background(background).Width(inner).Render(option.Components))
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
		lines = append(lines, lipgloss.NewStyle().Foreground(colors.Link).Background(background).Width(inner).Render("Assessment: "+option.AssessmentURL))
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
