package tui_test

import (
	"testing"

	"github.com/temper-sh/temper/internal/tui"
)

func TestFocusFollowsTheScreenAndSkipsEmptyContent(t *testing.T) {
	for _, tc := range []struct {
		name       string
		order      tui.FocusOrder
		area       tui.FocusArea
		row, delta int
		wantArea   tui.FocusArea
		wantRow    int
	}{
		{"next row", tui.FocusOrder{Rows: 2}, tui.ContentFocus, 0, 1, tui.ContentFocus, 1},
		{"end of list", tui.FocusOrder{Rows: 2}, tui.ContentFocus, 1, 1, tui.ActionsFocus, 1},
		{"return to last row", tui.FocusOrder{Rows: 2}, tui.ActionsFocus, 0, -1, tui.ContentFocus, 1},
		{"wrap to first row", tui.FocusOrder{Rows: 2}, tui.ActionsFocus, 1, 1, tui.ContentFocus, 0},
		{"content to secondary", tui.FocusOrder{Rows: 1, Primary: true, Secondary: true}, tui.ContentFocus, 0, -1, tui.SecondaryFocus, 0},
		{"footer to primary", tui.FocusOrder{Rows: 1, Primary: true, Secondary: true}, tui.ActionsFocus, 0, 1, tui.PrimaryFocus, 0},
		{"primary to footer", tui.FocusOrder{Rows: 1, Primary: true}, tui.PrimaryFocus, 0, -1, tui.ActionsFocus, 0},
		{"skip empty forward", tui.FocusOrder{Secondary: true}, tui.SecondaryFocus, 0, 1, tui.ActionsFocus, 0},
		{"skip empty backward", tui.FocusOrder{Secondary: true}, tui.ActionsFocus, 0, -1, tui.SecondaryFocus, 0},
		{"empty initial content", tui.FocusOrder{}, tui.ContentFocus, 0, -1, tui.ActionsFocus, 0},
		{"only actions", tui.FocusOrder{}, tui.ActionsFocus, 0, 1, tui.ActionsFocus, 0},
		{"input to actions", tui.FocusOrder{Rows: 1}, tui.ContentFocus, 0, -1, tui.ActionsFocus, 0},
		{"actions to input", tui.FocusOrder{Rows: 1}, tui.ActionsFocus, 0, 1, tui.ContentFocus, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			area, row := tc.order.Vertical(tc.area, tc.row, tc.delta)
			if area != tc.wantArea || row != tc.wantRow {
				t.Fatalf("focus = (%v, %d), want (%v, %d)", area, row, tc.wantArea, tc.wantRow)
			}
		})
	}
}

func TestTabChangesRegionsWithoutMovingThroughRows(t *testing.T) {
	order := tui.FocusOrder{Primary: true, Secondary: true, Rows: 12}
	area := tui.ContentFocus
	for _, want := range []tui.FocusArea{tui.ActionsFocus, tui.PrimaryFocus, tui.SecondaryFocus, tui.ContentFocus} {
		area = order.Cycle(area, 1)
		if area != want {
			t.Fatalf("Tab focus = %v, want %v", area, want)
		}
	}
	if got := order.Cycle(tui.ContentFocus, -1); got != tui.SecondaryFocus {
		t.Fatalf("Shift-Tab = %v, want secondary", got)
	}
}
