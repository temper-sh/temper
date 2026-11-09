package setupui

import (
	"fmt"
	"path"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/temper-sh/temper/internal/setup"
)

// Review answers what will be installed and whether it fits on disk. Model
// choice, memory evidence and layout composition belong to their own screens;
// refusals and memory problems still appear as warnings above this content.

// reviewDefaultChoice asks whether newly selected models join Default. It is
// asked only when other layouts exist; with Default alone they join without a
// question, because the wizard has no layout concept to explain.
func (m *PresetModel) reviewDefaultChoice(body *screenBody, width int) {
	if !m.defaultChoiceShown() {
		return
	}
	additions := m.defaultAdditions()
	noun := "model"
	if len(additions) > 1 {
		noun = "models"
	}
	mark := "[x]"
	if m.keepDefault {
		mark = "[ ]"
	}
	name := m.defaultName()
	focused := m.focus == presetContentFocus
	card := optionCard(Option{
		Name:        fmt.Sprintf("Add the new %s to layout “%s”", noun, name),
		Description: m.presetNames(additions),
		Details:     fmt.Sprintf("Apps that use “%s” can use it after installation. Your other layouts stay as they are. Press Space to change.", name),
	}, mark, focused, width, layoutColors)
	top := body.addCard(card, focused)
	body.targets = append(body.targets, hitTarget{kind: hitShortcut, key: "default-additions", y: top, width: width, height: lipgloss.Height(card)})
}

// defaultChoiceShown is true on a rerun that adds models while other layouts
// exist besides Default.
func (m *PresetModel) defaultChoiceShown() bool {
	return len(m.defaultAdditions()) > 0 && len(m.draft.Layouts) > 1
}

// reviewFacts lists what this screen decides as aligned rows.
func (m *PresetModel) reviewFacts(body *screenBody, width int) {
	plan := m.plan
	var harnesses []string
	options := m.harnessOptions()
	if c := plan.Configuration; c != nil {
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
	}
	harnessNames := "None"
	if len(harnesses) > 0 {
		harnessNames = strings.Join(harnesses, ", ")
	}
	rows := [][2]string{
		{"Harnesses", harnessNames},
		{"Download", setup.DownloadSize(plan.RemainingDownloadBytes)},
		{"Disk", setup.DownloadSize(plan.RemainingDiskBytes) + " needed · " + setup.Size(plan.FreeDiskBytes) + " free"},
	}
	if cache := plan.HFCache; cache != nil && !cache.SharedFilesystem {
		rows = append(rows, [2]string{"Model cache", setup.DownloadSize(cache.RemainingDiskBytes) + " needed · " + setup.Size(cache.FreeDiskBytes) + " free"})
	}
	var firstStart int64
	for _, preset := range plan.Presets {
		firstStart += preset.RuntimeDiskEstimateBytes
	}
	if firstStart > 0 {
		rows = append(rows, [2]string{"First start", "Splash builds a cache of about " + setup.Size(firstStart) + "; keep another 2 GiB free."})
	}
	body.add(factRows(rows, width), false)
}

// factRows renders label/value pairs with the values in one aligned column.
func factRows(rows [][2]string, width int) string {
	labelWidth := 0
	for _, row := range rows {
		labelWidth = max(labelWidth, lipgloss.Width(row[0]))
	}
	labelWidth += 2
	label := lipgloss.NewStyle().Foreground(colorMuted).Width(labelWidth)
	value := lipgloss.NewStyle().Foreground(colorText).Width(max(1, width-labelWidth))
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top, label.Render(row[0]), value.Render(row[1])))
	}
	return strings.Join(lines, "\n")
}

// reviewRetirements states each staged retirement action and, before deletion
// consent, the shared files it could delete.
func (m *PresetModel) reviewRetirements(body *screenBody, width int) {
	if len(m.retirements) == 0 {
		return
	}
	var lines []string
	for _, change := range m.retirements {
		line := "Remove " + change.Preset
		if change.Successor != "" {
			line = "Replace " + change.Preset + " → " + change.Successor
		}
		lines = append(lines, line)
	}
	lines = append(lines, "Prepare replacement before switching; clean newly unused managed files after activation.")
	lines = append(lines, m.retiredFileLines()...)
	body.add(infoBlock("Retired presets", lines, width), false)
}

// reviewFiles shows every file the installation needs and whether it is
// already present. Software rows use their engine name and version.
func (m *PresetModel) reviewFiles(body *screenBody, width int) {
	var files []Download
	for _, file := range m.plan.Downloads {
		name := file.Name
		if file.Kind == "software" {
			name = path.Base(name)
		}
		files = append(files, Download{File: name, Size: setup.DownloadSize(file.Bytes), Status: file.Action()})
	}
	if len(files) > 0 {
		body.add(downloadTable(files, width), false)
	}
}

// reviewLayoutGap is the one layout note Review keeps: without a Default
// layout, newly selected models would otherwise be unusable without notice.
func (m *PresetModel) reviewLayoutGap(body *screenBody, width int) {
	if m.input.NewConfiguration {
		return
	}
	if _, ok := m.draft.Layouts[defaultLayout]; ok {
		return
	}
	var outside []string
	for _, id := range m.presetIDs() {
		if _, saved := m.input.Configuration.Presets[id]; !saved && len(m.draft.References(id)) == 0 {
			outside = append(outside, id)
		}
	}
	if len(outside) > 0 {
		body.add(infoBlock("Not in a layout yet", []string{m.presetNames(outside) + ". Add new models to a layout with temper layout to use them."}, width), false)
	}
}

// retiredFileLines names each shared file before deletion consent, because
// that deletion cannot be undone and may affect applications outside Temper.
func (m *PresetModel) retiredFileLines() []string {
	switch {
	case m.loading:
		return []string{"Checking shared HF files…"}
	case m.retiredFilesErr != nil:
		return []string{"Shared HF files kept: cannot list them (" + m.retiredFilesErr.Error() + ")."}
	case len(m.retiredFiles) == 0:
		return []string{"No unused shared HF files to delete."}
	}
	var total int64
	for _, f := range m.retiredFiles {
		total += f.Bytes
	}
	lines := []string{fmt.Sprintf("Unused shared HF files (%s); other applications may use them:", setup.Size(total))}
	for _, f := range m.retiredFiles {
		lines = append(lines, "  "+f.Name+" · "+setup.Size(f.Bytes))
	}
	if m.deleteRetiredWeights {
		return append(lines, "DELETE these files across cached revisions (w to keep).")
	}
	return append(lines, "Keep these files (w to delete).")
}
