package setupui

import (
	"fmt"
	"image/color"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
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
		body.add(lipgloss.NewStyle().Foreground(nightMuted).Width(width).Render("No presets or layouts selected."), false)
	}
	for _, id := range layoutIDs {
		layout := c.Layouts[id]
		for _, preset := range layout.Presets {
			included[preset] = true
		}
		title := lipgloss.NewStyle().Foreground(nightCyan).Bold(true).Width(width).Render(layout.Name)
		content := lipgloss.NewStyle().Foreground(nightMuted).Render("No presets included.")
		if len(layout.Presets) > 0 {
			content = reviewPresetTable(*c, layout.Presets, &layout, m.plan.Modes, width)
		}
		details := []string{"Idle unload: " + idleDuration(layout.IdleSeconds)}
		for _, assessment := range m.plan.Layouts {
			if assessment.ID != id || len(layout.Presets) == 0 {
				continue
			}
			details = append(details, fmt.Sprintf("Est. memory: %s at activation · %s largest preset", setup.Size(assessment.StartupBytes), setup.Size(assessment.LargestPresetBytes)))
			if !assessment.AllFit {
				details = append(details, "Competing models swap on demand; active requests finish before eviction.")
			}
		}
		body.add(title+"\n"+content+"\n"+lipgloss.NewStyle().Foreground(nightMuted).Width(width).Render(strings.Join(details, " · ")), false)
	}
	for id := range c.Presets {
		if !included[id] {
			unassigned = append(unassigned, id)
		}
	}
	if len(unassigned) > 0 {
		slices.Sort(unassigned)
		title := lipgloss.NewStyle().Foreground(nightCyan).Bold(true).Width(width).Render("Presets outside layouts")
		body.add(title+"\n"+reviewPresetTable(*c, unassigned, nil, m.plan.Modes, width), false)
	}
	var notes []string
	if len(c.Presets) > 0 {
		notes = append(notes, "Context / output: token limits; context includes input and output. Fit is unknown unless marked tested.",
			"Memory estimates use weights and declared caps; KV and runtime overhead remain unmeasured.")
	}
	for _, mode := range m.plan.Modes {
		if mode.RuntimeDiskEstimateBytes > 0 {
			_, preset := presetRecord(mode.Lock)
			name := mode.Lock.Records.Artifacts[preset.Artifact].ModelName
			if name == "" {
				name = preset.DisplayName
			}
			notes = append(notes, fmt.Sprintf("%s: first-start Splash cache needs roughly another %s, plus 2 GiB free; additional to installation totals.", name, setup.Size(mode.RuntimeDiskEstimateBytes)))
		}
	}
	if m.plan.Root != "" {
		notes = append(notes, "Configuration: "+m.plan.Root)
	}
	if len(notes) > 0 {
		body.add(lipgloss.NewStyle().Foreground(nightMuted).Width(width).Render(strings.Join(notes, "\n")), false)
	}
}

func reviewPresetTable(c setup.Configuration, ids []string, layout *setup.Layout, modes []setup.ModePlan, width int) string {
	headers := []string{"Model", "Weights", "Engine", "Context / out", "Load", "Default"}
	columns := []int{width - 85, 22, 17, 15, 15, 9}
	colors := []color.Color{nightBlue, nightCyan, nightPurple, nightCyan, nightGreen, nightPurple}
	if width < 100 {
		headers = []string{"Preset", "Context / out", "Load", "Default"}
		columns = []int{width - 44, 15, 15, 9}
		colors = []color.Color{nightBlue, nightCyan, nightGreen, nightPurple}
	}
	if width < 70 {
		headers = []string{"Preset", "Settings"}
		settingsWidth := 20
		if width < 40 {
			settingsWidth = 14
		}
		columns = []int{width - 3 - settingsWidth, settingsWidth}
		colors = []color.Color{nightBlue, nightCyan}
	}
	rows := make([][]string, 0, len(ids))
	for _, id := range ids {
		selected := c.Presets[id]
		presetID, preset := presetRecord(selected.Lock)
		artifact, engine := selected.Lock.Records.Artifacts[preset.Artifact], selected.Lock.Records.Engines[preset.Engine]
		model, weights, engineName := artifact.ModelName, weightsLabel(artifact.WeightsName), engine.DisplayName
		if model == "" {
			model = selected.Name
		}
		if weights == "" {
			weights = preset.Artifact
		}
		if engineName == "" {
			engineName = preset.Engine
		}
		context := fmt.Sprintf("%d / %d", preset.ContextWindowTokens, preset.RequestDefaults.MaxOutputTokens)
		for _, mode := range modes {
			if mode.Lock.Digests.Profile == selected.Lock.Digests.Profile && mode.Contexts[presetID].Finding != nil {
				context += "\ntested"
				break
			}
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
		row := []string{model, weights, engineName, context, load, defaultRoute}
		identity := strings.Join([]string{model, weights, engineName}, "\n")
		if width < 100 {
			row = []string{identity, context, load, defaultRoute}
		}
		if width < 70 {
			settings := "Context / out:\n" + context
			if layout != nil {
				settings += "\n" + load + "\nDefault: " + defaultRoute
			}
			row = []string{identity, settings}
		}
		rows = append(rows, row)
	}
	return table.New().Headers(headers...).Rows(rows...).Width(width).Wrap(true).
		Border(lipgloss.NormalBorder()).BorderStyle(lipgloss.NewStyle().Foreground(nightBorder)).
		StyleFunc(func(row, col int) lipgloss.Style {
			style := lipgloss.NewStyle().Padding(0, 1).Width(columns[col]).Foreground(colors[col])
			if row == table.HeaderRow {
				return style.Bold(true)
			}
			if rows[row][col] == "—" || rows[row][col] == "On request" {
				style = style.Foreground(nightMuted)
			} else if col == 0 || rows[row][col] == "Yes" {
				style = style.Bold(true)
			}
			if row%2 == 0 {
				style = style.Background(nightSurface)
			}
			return style
		}).String()
}
