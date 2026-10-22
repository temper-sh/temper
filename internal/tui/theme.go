package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Element names a visible role or state. Focus and selection remain distinct;
// focused actions exchange their normal text and surface colors.
type Element string

const (
	Canvas                   Element = "canvas"
	Body                     Element = "body"
	Muted                    Element = "muted"
	Divider                  Element = "divider"
	NavigationLine           Element = "navigation-line"
	Brand                    Element = "brand"
	Heading                  Element = "heading"
	SectionHeading           Element = "section-heading"
	Step                     Element = "step"
	StepCurrent              Element = "step-current"
	StepDone                 Element = "step-done"
	Filter                   Element = "filter"
	FilterChosen             Element = "filter-chosen"
	FilterFocused            Element = "filter-chosen-focused"
	Card                     Element = "card"
	CardFocused              Element = "card-focused"
	CardTitle                Element = "card-title"
	ModelName                Element = "model-name"
	WeightsName              Element = "weights-name"
	EngineName               Element = "engine-name"
	CardValue                Element = "card-value"
	Link                     Element = "link"
	Selected                 Element = "selected"
	Toggle                   Element = "toggle"
	ToggleOn                 Element = "toggle-on"
	ToggleFocused            Element = "toggle-focused"
	ToggleOnFocused          Element = "toggle-on-focused"
	Action                   Element = "action"
	ActionFocused            Element = "action-focused"
	PrimaryAction            Element = "primary-action"
	PrimaryActionFocused     Element = "primary-action-focused"
	QuietAction              Element = "quiet-action"
	QuietActionFocused       Element = "quiet-action-focused"
	DangerAction             Element = "danger-action"
	DangerActionFocused      Element = "danger-action-focused"
	UnavailableAction        Element = "unavailable-action"
	UnavailableActionFocused Element = "unavailable-action-focused"
	Input                    Element = "input"
	InputFocused             Element = "input-focused"
	InputPrompt              Element = "input-prompt"
	InputCursor              Element = "input-cursor"
	Warning                  Element = "warning"
	Error                    Element = "error"
	Notice                   Element = "notice"
	Success                  Element = "success"
	TableHeading             Element = "table-heading"
	TableValue               Element = "table-value"
	TableAlternate           Element = "table-alternate"
	EditorRow                Element = "editor-row"
	EditorRowFocused         Element = "editor-row-focused"
	Inherited                Element = "inherited"
	Override                 Element = "override"
)

// Paint is the complete color choice for one element/state. Geometry and
// emphasis belong to its view. Colors returns a value, so callers cannot
// mutate the shared table.
type Paint struct{ Text, Background, Border color.Color }

const (
	charcoal     = "#1b1d21"
	surface      = "#25282d"
	focusSurface = "#25282d"
	line         = "#4c515b"
	white        = "#eeece6"
	muted        = "#a5abb6"
	amber        = "#e0b15b"
	green        = "#4da653"
	blue         = "#5ca5ff"
	coral        = "#ff5f5f"
)

func paint(text, background, border string) Paint {
	return Paint{lipgloss.Color(text), lipgloss.Color(background), lipgloss.Color(border)}
}

var elements = map[Element]Paint{
	Canvas:                   paint(white, charcoal, line),
	Body:                     paint(white, charcoal, line),
	Muted:                    paint(muted, charcoal, line),
	Divider:                  paint(line, charcoal, line),
	NavigationLine:           paint("#43465a", charcoal, "#43465a"),
	Brand:                    paint(white, charcoal, line),
	Heading:                  paint(white, charcoal, line),
	SectionHeading:           paint(white, charcoal, line),
	Step:                     paint("#a3a7b7", charcoal, line),
	StepCurrent:              paint(amber, charcoal, amber),
	StepDone:                 paint(green, charcoal, line),
	Filter:                   paint(white, charcoal, line),
	FilterChosen:             paint(white, charcoal, line),
	FilterFocused:            paint(white, charcoal, white),
	Card:                     paint(white, charcoal, line),
	CardFocused:              paint(white, surface, white),
	CardTitle:                paint(white, charcoal, line),
	ModelName:                paint(white, charcoal, line),
	WeightsName:              paint(white, charcoal, line),
	EngineName:               paint(muted, charcoal, line),
	CardValue:                paint(white, charcoal, line),
	Link:                     paint(blue, charcoal, line),
	Selected:                 paint(white, charcoal, line),
	Toggle:                   paint(muted, charcoal, line),
	ToggleOn:                 paint(white, charcoal, line),
	ToggleFocused:            paint(white, focusSurface, white),
	ToggleOnFocused:          paint(white, focusSurface, white),
	Action:                   paint(white, surface, line),
	ActionFocused:            paint(surface, white, white),
	PrimaryAction:            paint(white, surface, line),
	PrimaryActionFocused:     paint(surface, white, white),
	QuietAction:              paint(muted, surface, line),
	QuietActionFocused:       paint(surface, muted, muted),
	DangerAction:             paint(coral, surface, coral),
	DangerActionFocused:      paint(surface, coral, coral),
	UnavailableAction:        paint(muted, charcoal, line),
	UnavailableActionFocused: paint(muted, charcoal, line),
	Input:                    paint(white, charcoal, line),
	InputFocused:             paint(white, charcoal, white),
	InputPrompt:              paint(muted, charcoal, line),
	InputCursor:              paint(white, charcoal, white),
	Warning:                  paint("#facc15", charcoal, "#facc15"),
	Error:                    paint(coral, charcoal, coral),
	Notice:                   paint(amber, charcoal, amber),
	Success:                  paint("#009a54", charcoal, "#009a54"),
	TableHeading:             paint(white, charcoal, line),
	TableValue:               paint(white, charcoal, line),
	TableAlternate:           paint(white, surface, line),
	EditorRow:                paint(white, charcoal, line),
	EditorRowFocused:         paint(white, surface, white),
	Inherited:                paint(muted, charcoal, line),
	Override:                 paint(amber, charcoal, line),
}

// Tool supplies identity and action colors; navigation and feedback are shared.
type Tool string

const (
	Wizard  Tool = "Guided setup"
	Layouts Tool = "Layouts"
	Presets Tool = "Presets"
)

func (tool Tool) accent() (string, string) {
	switch tool {
	case Wizard:
		return "#5ca5ff", "#20334d"
	case Layouts:
		return "#20d6f5", "#18343b"
	case Presets:
		return "#bc87ff", "#35234b"
	default:
		panic("unknown TUI tool: " + string(tool))
	}
}

func (tool Tool) Colors(element Element) Paint {
	accent, tinted := tool.accent()
	switch element {
	case Heading, FilterChosen, FilterFocused:
		return paint(accent, charcoal, white)
	case PrimaryAction:
		return paint(accent, tinted, accent)
	case PrimaryActionFocused:
		return paint(tinted, accent, accent)
	default:
		return Colors(element)
	}
}

func (tool Tool) Style(element Element) lipgloss.Style {
	p := tool.Colors(element)
	return lipgloss.NewStyle().Foreground(p.Text).Background(p.Background).BorderForeground(p.Border)
}

func Colors(element Element) Paint {
	p, ok := elements[element]
	if !ok {
		panic("unknown TUI element: " + string(element))
	}
	return p
}

func Style(element Element) lipgloss.Style {
	p := Colors(element)
	return lipgloss.NewStyle().Foreground(p.Text).Background(p.Background).BorderForeground(p.Border)
}
