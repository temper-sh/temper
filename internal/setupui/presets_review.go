package setupui

import (
	"fmt"
	"image/color"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/setup"
)

func (m *PresetModel) reviewLayouts(body *screenBody, width int) {
	c := m.plan.Configuration
	if c == nil {
		return
	}
	var layoutIDs, unassigned []string
	included := map[string]bool{}
	for id := range c.Layouts {
		layoutIDs = append(layoutIDs, id)
	}
	slices.Sort(layoutIDs)
	if len(layoutIDs) == 0 && len(c.Presets) == 0 {
		body.add(lipgloss.NewStyle().Foreground(colorMuted).Width(width).Render("No presets or layouts selected."), false)
	}
	for _, id := range layoutIDs {
		layout := c.Layouts[id]
		for _, preset := range layout.Presets {
			included[preset] = true
		}
		title := lipgloss.NewStyle().Foreground(layoutColors.Name[0]).Bold(true).Width(width).Render(layout.Name)
		content := lipgloss.NewStyle().Foreground(colorMuted).Render("No presets included.")
		if len(layout.Presets) > 0 {
			content = reviewPresetTable(*c, layout.Presets, &layout, width)
		}
		details := []string{"Idle unload: " + idleDuration(layout.IdleSeconds)}
		for _, assessment := range m.plan.Layouts {
			if assessment.ID != id || len(layout.Presets) == 0 {
				continue
			}
			details = append(details, setup.LayoutMemory(assessment))
			if !assessment.AllFit {
				details = append(details, "Competing models swap on demand; active requests finish before eviction.")
			}
		}
		body.add(title+"\n"+content+"\n"+lipgloss.NewStyle().Foreground(colorMuted).Width(width).Render(strings.Join(details, " · ")), false)
	}
	for id := range c.Presets {
		if !included[id] {
			unassigned = append(unassigned, id)
		}
	}
	if len(unassigned) > 0 {
		slices.Sort(unassigned)
		title := lipgloss.NewStyle().Foreground(colorText).Bold(true).Width(width).Render("Presets outside layouts")
		body.add(title+"\n"+reviewPresetTable(*c, unassigned, nil, width), false)
	}
	var notes []string
	if len(c.Presets) > 0 {
		notes = append(notes, "Context / output: token limits; context includes input and output.",
			"Memory uses applicable catalog measurements, otherwise estimates. Observed peaks apply to the tested workload; estimates can omit runtime allocations.")
	}
	for _, mode := range m.plan.Presets {
		if mode.Memory.Basis != "" {
			name := mode.Lock.Records.Presets[mode.Lock.Preset].Presentation.DisplayName
			notes = append(notes, name+": "+mode.Memory.Lines()[0])
		}
		if mode.RuntimeDiskEstimateBytes > 0 {
			_, preset := presetRecord(mode.Lock)
			name := mode.Lock.Records.Artifacts[preset.Artifact].Presentation.ModelName
			if name == "" {
				name = preset.Presentation.DisplayName
			}
			notes = append(notes, fmt.Sprintf("%s: first-start Splash cache needs roughly another %s, plus 2 GiB free; additional to installation totals.", name, setup.Size(mode.RuntimeDiskEstimateBytes)))
		}
	}
	if m.plan.Root != "" {
		notes = append(notes, "Configuration: "+m.plan.Root)
	}
	if len(notes) > 0 {
		body.add(lipgloss.NewStyle().Foreground(colorMuted).Width(width).Render(strings.Join(notes, "\n")), false)
	}
}

func reviewPresetTable(c setup.Configuration, ids []string, layout *setup.Layout, width int) string {
	headers := []string{"Preset", "Context / out", "Load", "Default"}
	columns := []int{width - 48, 19, 15, 9}
	colors := []color.Color{layoutColors.Name[1], layoutColors.Accent, layoutColors.Check, layoutColors.Check}
	if width < 70 {
		headers = []string{"Preset", "Settings"}
		settingsWidth := 20
		if width < 40 {
			settingsWidth = 14
		}
		columns = []int{width - 3 - settingsWidth, settingsWidth}
		colors = []color.Color{layoutColors.Name[1], layoutColors.Accent}
	}
	rows := make([][]string, 0, len(ids))
	for _, id := range ids {
		selected := c.Presets[id]
		_, preset := presetRecord(selected.Lock)
		// One line per preset: the name the layout cards use, plus the device
		// when the preset card shows it.
		name := selected.Name
		if len(preset.ComputeOptions) > 1 {
			name += " · Compute " + strings.ToUpper(string(preset.Compute))
		} else if preset.Compute == catalog.ComputeCPU {
			name += " · CPU only"
		}
		context := fmt.Sprintf("%d", preset.Window())
		if preset.Interface == "chat-completions" {
			output := "remaining"
			if preset.RequestDefaults.MaxOutputTokens > 0 {
				output = fmt.Sprint(preset.RequestDefaults.MaxOutputTokens)
			}
			context += " / " + output
		}
		load, defaultRoute := "—", "—"
		if layout != nil {
			load = "On request"
			if slices.Contains(layout.Startup, id) {
				load = "On activation"
			}
			if layout.Default == id {
				defaultRoute = "Yes"
			}
		}
		row := []string{name, context, load, defaultRoute}
		if width < 70 {
			settings := "Context / out:\n" + context
			if layout != nil {
				settings += "\n" + load + "\nDefault: " + defaultRoute
			}
			row = []string{name, settings}
		}
		rows = append(rows, row)
	}
	return table.New().Headers(headers...).Rows(rows...).Width(width).Wrap(true).
		Border(lipgloss.NormalBorder()).BorderStyle(lipgloss.NewStyle().Foreground(colorBorder)).
		StyleFunc(func(row, col int) lipgloss.Style {
			style := lipgloss.NewStyle().Padding(0, 1).Width(columns[col]).Foreground(colors[col])
			if row == table.HeaderRow {
				return style.Bold(true)
			}
			if rows[row][col] == "—" || rows[row][col] == "On request" {
				style = style.Foreground(colorMuted)
			} else if col == 0 || rows[row][col] == "Yes" {
				style = style.Bold(true)
			}
			if row%2 == 0 {
				style = style.Background(colorSurface)
			}
			return style
		}).String()
}
