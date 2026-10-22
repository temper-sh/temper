package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Frame lays out already-rendered regions. It owns margins and keeps the footer
// fixed; scrolling, focus, and commands remain with the tool.
type Frame struct {
	Width, Height  int
	Header, Footer string
}

// Target is the cell rectangle of one rendered control. Its index identifies
// the caller's action or section; the caller owns activation and focus.
type Target struct {
	Index, X, Y, Width, Height int
}

func (f Frame) HeaderTop() int { return f.inset() }

func (f Frame) BodyTop() int {
	header, _ := f.regions()
	return f.inset() + len(lines(header))
}

// FooterTop is the origin of the untrimmed footer. When a short frame drops
// leading footer lines, callers clip their targets to the visible footer.
func (f Frame) FooterTop() int { return f.Height - f.inset() - len(lines(f.Footer)) }

func ContentWidth(width int) int {
	if width < 50 {
		return max(1, width-2)
	}
	return max(1, min(108, width-4))
}

func (f Frame) inset() int {
	if f.Height >= 18 {
		return 1
	}
	return 0
}

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func (f Frame) regions() (string, string) {
	header, footer := lines(f.Header), lines(f.Footer)
	space := max(0, f.Height-2*f.inset()-1)
	// Preserve the bottom action/help rows when an external notice is very long.
	if len(footer) > space/2 {
		footer = footer[len(footer)-space/2:]
	}
	if len(header) > space-len(footer) {
		header = header[:space-len(footer)]
	}
	return strings.Join(header, "\n"), strings.Join(footer, "\n")
}

func (f Frame) BodyHeight() int {
	header, footer := f.regions()
	return max(1, f.Height-2*f.inset()-len(lines(header))-len(lines(footer)))
}

func (f Frame) Render(body string) string {
	if f.Width <= 0 || f.Height <= 0 {
		return ""
	}
	w := ContentWidth(f.Width)
	header, footer := f.regions()
	bodyLines := lines(body)
	if len(bodyLines) > f.BodyHeight() {
		bodyLines = bodyLines[:f.BodyHeight()]
	}
	for i, s := range bodyLines {
		bodyLines[i] = ansi.Truncate(s, w, "")
	}
	body = Style(Canvas).Width(w).Height(f.BodyHeight()).Render(strings.Join(bodyLines, "\n"))
	var regions []string
	for _, s := range []string{header, body, footer} {
		if s != "" {
			regions = append(regions, s)
		}
	}
	content := strings.Split(strings.Join(regions, "\n"), "\n")
	for i, s := range content {
		content[i] = ansi.Truncate(s, w, "")
	}
	left := (f.Width - w) / 2
	return Style(Canvas).Width(f.Width).Height(f.Height).
		Padding(f.inset(), f.Width-w-left, f.inset(), left).Render(strings.Join(content, "\n"))
}

type Button struct {
	Label             string
	Kind              Element
	Focused, Disabled bool
}

func (b Button) render(tool Tool, width int) string {
	element := b.Kind
	if element == "" {
		element = Action
	}
	if b.Disabled {
		element = UnavailableAction
	} else if b.Focused {
		switch element {
		case PrimaryAction:
			element = PrimaryActionFocused
		case DangerAction:
			element = DangerActionFocused
		case QuietAction:
			element = QuietActionFocused
		default:
			element = ActionFocused
		}
	}
	return tool.Style(element).Padding(0, 1).Render(ansi.Truncate(label(b.Label), max(1, width-2), "…"))
}

func Actions(tool Tool, buttons []Button, width int) string {
	text, _ := ActionsWithTargets(tool, buttons, width)
	return text
}

func ActionsWithTargets(tool Tool, buttons []Button, width int) (string, []Target) {
	var rows []string
	var targets []Target
	row := ""
	for i, button := range buttons {
		part := button.render(tool, width)
		if row != "" && lipgloss.Width(row)+1+lipgloss.Width(part) > width {
			rows = append(rows, row)
			row = ""
		}
		if row != "" {
			row += " "
		}
		targets = append(targets, Target{Index: i, X: lipgloss.Width(row), Y: len(rows), Width: lipgloss.Width(part), Height: 1})
		row += part
	}
	if row != "" {
		rows = append(rows, row)
	}
	return strings.Join(rows, "\n"), targets
}

type Hint struct{ Key, Action string }

// KeyHelp wraps whole key/action pairs; narrow frames reserve two rows.
func KeyHelp(hints []Hint, width int) string {
	var rows []string
	row := ""
	for _, hint := range hints {
		part := Style(Body).Render(label(hint.Key)) + " " + Style(Muted).Render(label(hint.Action))
		part = ansi.Truncate(part, max(1, width), "…")
		if row != "" && lipgloss.Width(row)+2+lipgloss.Width(part) > width {
			rows = append(rows, row)
			row = ""
		}
		if row != "" {
			row += "  "
		}
		row += part
	}
	rows = append(rows, row)
	if width < 50 && len(rows) < 2 {
		rows = append(rows, "")
	}
	return strings.Join(rows, "\n")
}
