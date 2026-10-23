package setupui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/temper-sh/temper/internal/tui"
)

// family supplies the shared content roles to card and review renderers.
type family struct {
	Name    [3]color.Color // model, weights, engine
	Accent  color.Color    // numbers and details
	Link    color.Color
	Check   color.Color // chosen and done marks
	Border  color.Color // a focused element's border
	Surface color.Color // a focused element's background
}

// Existing content renderers consume the shared palette. The wizard's blue
// accent belongs to navigation and actions; content uses common neutral roles.
var (
	colorBackground    = tui.Colors(tui.Canvas).Background
	colorSurface       = tui.Colors(tui.CardFocused).Background
	colorBorder        = tui.Colors(tui.Divider).Text
	colorText          = tui.Colors(tui.Body).Text
	colorMuted         = tui.Colors(tui.Muted).Text
	colorWarn          = tui.Colors(tui.Warning).Text
	colorDanger        = tui.Colors(tui.Error).Text
	presetColors       = contentColors()
	secondaryNavColors = contentColors()
	layoutColors       = contentColors()
	reviewColors       = contentColors()
)

func contentColors() family {
	return family{
		Name:   [3]color.Color{tui.Colors(tui.ModelName).Text, tui.Colors(tui.WeightsName).Text, tui.Colors(tui.EngineName).Text},
		Accent: tui.Colors(tui.CardValue).Text, Link: tui.Colors(tui.Link).Text,
		Check: tui.Colors(tui.Selected).Text, Border: tui.Colors(tui.CardFocused).Border,
		Surface: tui.Colors(tui.CardFocused).Background,
	}
}

type hitKind uint8

const (
	hitNext hitKind = iota
	hitChoice
	hitReview
	hitShortcut
	hitFilter
	hitPresetAction
	hitStep
	hitField
	hitWizardCompute
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

type screenBody struct {
	blocks               []string
	height               int
	lastCard             bool
	focusStart, focusEnd int
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
