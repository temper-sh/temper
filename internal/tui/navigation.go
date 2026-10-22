package tui

import (
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Labels are plain text, including when sourced from catalog or user metadata.
func label(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, ansi.Strip(s))
}

func Header(tool Tool, badge string, width int) string {
	text := Style(Brand).Bold(true).Render("TEMPER") + Style(Muted).Render(" / ") + tool.Style(Heading).Render(string(tool))
	if badge != "" {
		text += Style(Muted).Render(" · " + label(badge))
	}
	return ansi.Truncate(text, max(0, width), "…")
}

// Breadcrumbs renders a path, with the last label current. Focus is an ancestor
// index, or -1 when focus belongs elsewhere. The caller owns back navigation.
func Breadcrumbs(labels []string, focus, width int) string {
	text, _ := primaryNavigation(labels, len(labels)-1, focus, width, false)
	return text
}

// Steps shares breadcrumb geometry, adding completed marks before the current
// wizard step. Targets follow the rendered labels, including truncation.
func Steps(labels []string, current, focus, width int) (string, []Target) {
	return primaryNavigation(labels, current, focus, width, true)
}

func primaryNavigation(labels []string, current, focus, width int, steps bool) (string, []Target) {
	if width <= 0 || len(labels) == 0 {
		return "", nil
	}
	gap := 2
	if width < 50 {
		gap = 1
	}
	separator := strings.Repeat(" ", gap) + Style(Step).Render("❯") + strings.Repeat(" ", gap)
	available := max(len(labels), width-2*gap-(len(labels)-1)*(2*gap+1))
	lengths := make([]int, len(labels))
	text := make([]string, len(labels))
	total := 0
	for i, s := range labels {
		text[i] = label(s)
		if steps && i < current {
			text[i] = "✓ " + text[i]
		}
		lengths[i] = ansi.StringWidth(text[i])
		total += lengths[i]
	}
	for total > available {
		longest := -1
		for i, n := range lengths {
			// Keep a short current location intact while shortening long ancestors.
			if i == current && ansi.StringWidth(text[i]) <= available/2 {
				continue
			}
			if n > 1 && (longest < 0 || n > lengths[longest]) {
				longest = i
			}
		}
		if longest < 0 {
			longest = len(lengths) - 1
		}
		lengths[longest]--
		total--
	}
	var targets []Target
	x := gap
	for i, s := range text {
		style := Style(Step).Bold(true)
		if steps && i < current {
			style = Style(StepDone).Bold(true)
		}
		if i == current {
			style = Style(StepCurrent).Bold(true)
		}
		if i == focus {
			style = style.Foreground(Colors(Body).Text).Underline(true)
		}
		text[i] = style.Render(ansi.Truncate(s, lengths[i], "…"))
		targets = append(targets, Target{Index: i, X: x, Y: 1, Width: min(lipgloss.Width(text[i]), max(0, width-x)), Height: 1})
		x += lipgloss.Width(text[i]) + 2*gap + 1
	}
	row := ansi.Truncate(strings.Repeat(" ", gap)+strings.Join(text, separator), width, "…")
	border := Style(NavigationLine).Render(strings.Repeat("─", width))
	return border + "\n" + row + "\n" + border, targets
}

// Secondary keeps the current section visible, retaining whole labels where
// they fit. Bracket space is reserved on inactive labels to prevent shifting.
func Secondary(tool Tool, labels []string, current, width int, focused bool) string {
	text, _ := SecondaryWithTargets(tool, labels, current, width, focused)
	return text
}

func SecondaryWithTargets(tool Tool, labels []string, current, width int, focused bool) (string, []Target) {
	if len(labels) == 0 || width <= 0 {
		return "", nil
	}
	current = max(0, min(current, len(labels)-1))
	start, end := 0, len(labels)
	budget := max(1, width-2)
	visibleWidth := func() int {
		n := end - start - 1
		for _, s := range labels[start:end] {
			n += ansi.StringWidth(label(s)) + 2
		}
		if start > 0 {
			n += 2
		}
		if end < len(labels) {
			n += 2
		}
		return n
	}
	for visibleWidth() > budget && end-start > 1 {
		if current-start > end-current-1 {
			start++
		} else {
			end--
		}
	}
	var parts []string
	var targets []Target
	x := 0
	if start > 0 {
		parts = append(parts, Style(Body).Render("‹"))
		targets = append(targets, Target{Index: start - 1, X: 0, Width: 1, Height: 1})
		x = 2
	}
	for i := start; i < end; i++ {
		room := budget - 2
		if start > 0 {
			room -= 2
		}
		if end < len(labels) {
			room -= 2
		}
		s := ansi.Truncate(label(labels[i]), max(1, room), "…")
		style := tool.Style(Filter)
		if i == current {
			s = "[" + s + "]"
			style = tool.Style(FilterChosen).Underline(focused)
		} else {
			s = " " + s + " "
		}
		parts = append(parts, style.Render(s))
		targets = append(targets, Target{Index: i, X: x, Width: ansi.StringWidth(s), Height: 1})
		x += ansi.StringWidth(s) + 1
	}
	if end < len(labels) {
		parts = append(parts, Style(Body).Render("›"))
		targets = append(targets, Target{Index: end, X: x, Width: 1, Height: 1})
	}
	row := ansi.Truncate(strings.Join(parts, " "), width, "…")
	left := (width - lipgloss.Width(row)) / 2
	for i := range targets {
		targets[i].X += left
	}
	return lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Render(row), targets
}
