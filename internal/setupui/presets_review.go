package setupui

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/temper-sh/temper/internal/setup"
)

// Review confirms choices and cost. Preset tuning and layout settings belong
// to their editors; the wizard does not repeat those forms as review tables.
func (m *PresetModel) reviewSummary(body *screenBody, width int) {
	c := m.plan.Configuration
	if c == nil {
		return
	}
	var names, unassigned []string
	for _, id := range slices.Sorted(maps.Keys(c.Presets)) {
		name := c.Presets[id].Name
		names = append(names, name)
		if len(c.References(id)) == 0 {
			unassigned = append(unassigned, name)
		}
	}
	models := "None selected"
	if len(names) > 0 {
		models = strings.Join(names, ", ")
	}
	lines := []string{"Models: " + models}
	switch {
	case m.input.NewConfiguration:
		lines = append(lines, "Default layout · models load on demand")
	case len(c.Layouts) == 0:
		lines = append(lines, "No layout · create one with temper layout")
	default:
		var layouts []string
		for _, id := range slices.Sorted(maps.Keys(c.Layouts)) {
			layouts = append(layouts, c.Layouts[id].Name)
		}
		lines = append(lines, "Layouts: "+strings.Join(layouts, ", ")+" · kept unchanged")
	}
	var harnesses []string
	options := m.harnessOptions()
	for _, selected := range c.Integrations {
		name := selected.ID
		for _, option := range options {
			if option.ID == selected.ID {
				name = option.Name
				break
			}
		}
		harnesses = append(harnesses, name)
	}
	harnessNames := "None"
	if len(harnesses) > 0 {
		harnessNames = strings.Join(harnesses, ", ")
	}
	lines = append(lines, "Harnesses: "+harnessNames)
	body.add(lipgloss.NewStyle().Foreground(colorText).Width(width).Render(strings.Join(lines, "\n")), false)
	if len(unassigned) > 0 {
		body.add(lipgloss.NewStyle().Foreground(colorWarn).Width(width).Render("Outside layouts: "+strings.Join(unassigned, ", ")+". Add with temper layout."), false)
	}
}

func (m *PresetModel) reviewMemory(body *screenBody, width int) {
	if len(m.plan.Presets) == 0 {
		if m.plan.Configuration != nil && len(m.plan.Configuration.Presets) > 0 {
			body.add(lipgloss.NewStyle().Foreground(colorMuted).Render("Memory: not assessed."), false)
		}
		return
	}
	largest := m.plan.Presets[0]
	for _, preset := range m.plan.Presets[1:] {
		if preset.MemoryBytes > largest.MemoryBytes {
			largest = preset
		}
	}
	var lines []string
	if len(m.plan.Presets) > 1 {
		_, preset := presetRecord(largest.Lock)
		lines = append(lines, "Largest memory allowance: "+preset.Presentation.DisplayName)
	}
	if largest.Memory.Basis == "measured" && largest.Memory.Measurement != nil {
		// A measured value needs its counter, workload and limits to remain
		// meaningful. Reuse the assessment's wording rather than imply total RAM.
		lines = append(lines, largest.Memory.Lines()[:3]...)
	} else {
		_, preset := presetRecord(largest.Lock)
		lines = append(lines, fmt.Sprintf("Memory estimate: %s accounted at %s context tokens.", setup.Size(largest.MemoryBytes), tokenCount(preset.Window())),
			"Total runtime fit unknown; some allocations remain unmeasured.")
	}
	for _, layout := range m.plan.Layouts {
		if layout.StartupBytes > 0 {
			lines = append(lines, layout.Layout.Name+": "+setup.Size(layout.StartupBytes)+" accounted at startup.")
		}
		if !layout.AllFit && len(layout.Layout.Presets) > 1 {
			lines = append(lines, layout.Layout.Name+": models swap on demand.")
		}
	}
	body.add(lipgloss.NewStyle().Foreground(colorMuted).Width(width).Render(strings.Join(lines, "\n")), false)
}

func (m *PresetModel) reviewDownloads(body *screenBody, width int) {
	plan := m.plan
	title := "Downloads · " + setup.DownloadSize(plan.RemainingDownloadBytes)
	lines := []string{"Disk: " + setup.DownloadSize(plan.RemainingDiskBytes) + " needed · " + setup.Size(plan.FreeDiskBytes) + " free"}
	if cache := plan.HFCache; cache != nil && !cache.SharedFilesystem {
		lines = append(lines, "Model cache: "+setup.DownloadSize(cache.RemainingDiskBytes)+" needed · "+setup.Size(cache.FreeDiskBytes)+" free")
	}
	var firstStart int64
	for _, preset := range plan.Presets {
		firstStart += preset.RuntimeDiskEstimateBytes
	}
	if firstStart > 0 {
		lines = append(lines, "Splash first-start cache: about "+setup.Size(firstStart)+" extra disk; keep another 2 GiB free.")
	}
	var files []Download
	for _, file := range plan.Downloads {
		files = append(files, Download{File: file.Name, Size: setup.DownloadSize(file.Bytes), Status: file.Action()})
	}
	content := lipgloss.NewStyle().Foreground(colorText).Bold(true).Width(width).Render(title) + "\n" +
		lipgloss.NewStyle().Foreground(colorText).Width(width).Render(strings.Join(lines, "\n"))
	if len(files) > 0 {
		content += "\n" + downloadTable(files, width)
	}
	body.add(content, false)
}
