package tui

import "slices"

// FocusOrder describes the visible regions of a terminal screen. Tools own
// activation, scrolling and draft state; this value only chooses the next focus.
type FocusOrder struct {
	Primary, Secondary bool
	Rows               int
}

type FocusArea int

const (
	ContentFocus FocusArea = iota
	PrimaryFocus
	SecondaryFocus
	ActionsFocus
)

func (o FocusOrder) areas() []FocusArea {
	var areas []FocusArea
	if o.Primary {
		areas = append(areas, PrimaryFocus)
	}
	if o.Secondary {
		areas = append(areas, SecondaryFocus)
	}
	return append(areas, ContentFocus, ActionsFocus)
}

// Cycle moves between regions in visual order, skipping empty content. It also
// handles an empty screen that initially has content focus.
func (o FocusOrder) Cycle(area FocusArea, delta int) FocusArea {
	areas := o.areas()
	index := CycleChoice(slices.Index(areas, area), len(areas), delta)
	if areas[index] == ContentFocus && o.Rows == 0 {
		index = CycleChoice(index, len(areas), delta)
	}
	return areas[index]
}

// Vertical moves through rows before crossing regions. Read-only content uses
// one row; its tool calls this only after reaching the viewport's scroll edge.
// A modal input uses one row and excludes primary/secondary navigation.
func (o FocusOrder) Vertical(area FocusArea, row, delta int) (FocusArea, int) {
	if area == ContentFocus && row+delta >= 0 && row+delta < o.Rows {
		return area, row + delta
	}
	next := o.Cycle(area, delta)
	if next == ContentFocus {
		row = 0
		if delta < 0 {
			row = o.Rows - 1
		}
	}
	return next, row
}

// CycleChoice wraps horizontal action and secondary-navigation choices.
// Primary navigation uses bounded movement because it represents a path.
func CycleChoice(current, count, delta int) int {
	if count <= 0 {
		return 0
	}
	return (current + delta + count) % count
}

func FocusHints(area FocusArea) []Hint {
	choice := "actions"
	if area == PrimaryFocus {
		choice = "navigation"
	} else if area == SecondaryFocus {
		choice = "sections"
	}
	return []Hint{{Key: "←→", Action: choice}, {Key: "enter", Action: "activate"}, {Key: "↑↓", Action: "focus"}}
}

func InputHints(actions bool) []Hint {
	if actions {
		return []Hint{{Key: "←→", Action: "choose"}, {Key: "enter", Action: "activate"}, {Key: "↑↓", Action: "input"}, {Key: "esc", Action: "cancel"}}
	}
	return []Hint{{Key: "enter", Action: "apply"}, {Key: "esc", Action: "cancel"}, {Key: "↑↓", Action: "actions"}}
}
