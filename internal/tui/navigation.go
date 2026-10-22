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
	if width <= 0 || len(labels) == 0 {
		return ""
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
		lengths[i] = ansi.StringWidth(text[i])
		total += lengths[i]
	}
	for total > available {
		longest := -1
		for i, n := range lengths {
			// Keep a short current location intact while shortening long ancestors.
			if i == len(lengths)-1 && ansi.StringWidth(text[i]) <= available/2 {
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
	for i, s := range text {
		style := Style(Step).Bold(true)
		if i == len(text)-1 {
			style = Style(StepCurrent).Bold(true)
		}
		if i == focus {
			style = style.Foreground(Colors(Body).Text).Underline(true)
		}
		text[i] = style.Render(ansi.Truncate(s, lengths[i], "…"))
	}
	row := ansi.Truncate(strings.Repeat(" ", gap)+strings.Join(text, separator), width, "…")
	border := Style(NavigationLine).Render(strings.Repeat("─", width))
	return border + "\n" + row + "\n" + border
}

// Secondary keeps the current section visible, retaining whole labels where
// they fit. Bracket space is reserved on inactive labels to prevent shifting.
func Secondary(tool Tool, labels []string, current, width int, focused bool) string {
	if len(labels) == 0 || width <= 0 {
		return ""
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
	if start > 0 {
		parts = append(parts, Style(Body).Render("‹"))
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
	}
	if end < len(labels) {
		parts = append(parts, Style(Body).Render("›"))
	}
	return lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Render(ansi.Truncate(strings.Join(parts, " "), width, "…"))
}
