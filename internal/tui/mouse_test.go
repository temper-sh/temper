package tui_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/tui"
)

func TestMouseTargetsMatchOnlyVisibleFrameCells(t *testing.T) {
	for _, size := range [][2]int{{28, 10}, {40, 18}, {80, 24}, {112, 32}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			width, height := size[0], size[1]
			rows := func(prefix string, n int) ([]string, []tui.Target) {
				var lines []string
				var targets []tui.Target
				for i := range n {
					lines = append(lines, fmt.Sprintf("%s%d", prefix, i))
					targets = append(targets, tui.Target{Index: i, Y: i, Width: tui.ContentWidth(width), Height: 1})
				}
				return lines, targets
			}
			header, primary := rows("H", 7)
			body, content := rows("B", 50)
			footer, actions := rows("F", 12)
			frame := tui.Frame{Width: width, Height: height, Header: strings.Join(header, "\n"), Footer: strings.Join(footer, "\n")}
			var hits tui.HitMap
			hits.Add(0, frame.HeaderTargets(primary))
			hits.Add(1, frame.BodyTargets(content, 5))
			hits.Add(2, frame.FooterTargets(actions))
			view := ansi.Strip(frame.Render(strings.Join(body[5:], "\n")))
			for y, line := range strings.Split(view, "\n") {
				label := strings.TrimSpace(line)
				if label == "" {
					if _, _, ok := hits.At(width/2, y); ok {
						t.Fatal("blank outer margin has a click target")
					}
					continue
				}
				var prefix rune
				var index int
				if _, err := fmt.Sscanf(label, "%c%d", &prefix, &index); err != nil {
					t.Fatal(err)
				}
				area, got, ok := hits.At(strings.Index(line, label), y)
				if !ok || area != strings.IndexRune("HBF", prefix) || got != index {
					t.Fatalf("rendered %s maps to area %d, index %d, found %v", label, area, got, ok)
				}
				if _, _, ok := hits.At(0, y); ok {
					t.Fatal("left margin activates a control")
				}
			}
		})
	}
}
