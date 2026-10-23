package presetcmd

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/tui"
	"github.com/temper-sh/temper/internal/userpreset"
)

// Browsing and selection only read. The selected byte snapshot enters the
// authoring draft at Use template and is copied to presets.d only on Save.
func (m *Editor) browseTemplate() {
	dir := m.directory
	if dir == "" {
		dir, _ = os.Getwd()
	}
	if err := m.readDirectory(dir); err != nil {
		m.notice = err.Error()
		return
	}
	m.mode, m.cursor, m.area = "local-files", 0, contentArea
}

func (m *Editor) readDirectory(path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	slices.SortStableFunc(entries, func(a, b os.DirEntry) int {
		if a.IsDir() != b.IsDir() {
			if a.IsDir() {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name()), strings.ToLower(b.Name()))
	})
	m.directory, m.directoryEntries, m.cursor = path, entries, 0
	m.viewport.GotoTop()
	return nil
}

func (m *Editor) localFileKey(key string) tea.Cmd {
	switch key {
	case "esc", "q":
		m.mode, m.cursor, m.controlKey = "form", 1, ""
	case "location":
		return m.prompt("browse-path", m.directory)
	case "enter", "space", " ":
		path := filepath.Dir(m.directory)
		if m.cursor > 0 && m.cursor <= len(m.directoryEntries) {
			entry := m.directoryEntries[m.cursor-1]
			path = filepath.Join(m.directory, entry.Name())
			if !entry.IsDir() {
				return m.job("local-template", func(context.Context) editorJob {
					selected, err := userpreset.ReadTemplateFile(path)
					return editorJob{template: &selected, err: err}
				})
			}
		}
		if err := m.readDirectory(path); err != nil {
			m.notice = err.Error()
		}
	}
	return nil
}

func (m *Editor) localFilesBody(width int) (string, int, int, []tui.Target, []tui.Target) {
	labels := []string{".. / Parent directory"}
	for _, entry := range m.directoryEntries {
		name := terminalText(entry.Name())
		if entry.IsDir() {
			name += "/"
		}
		labels = append(labels, name)
	}
	lines := append(strings.Split(tui.Style(tui.Muted).Render(ansi.Wrap(terminalText(m.directory), width, "")), "\n"), "")
	var targets []tui.Target
	start, end := 0, 0
	for i, label := range labels {
		y := len(lines)
		block, _ := tui.ActionsWithTargets(tui.Presets, []tui.Button{{Label: label, Kind: tui.Action, Focused: i == m.cursor && m.area == contentArea}}, width)
		lines = append(lines, strings.Split(block, "\n")...)
		targets = append(targets, tui.Target{Index: i, Y: y, Width: width, Height: len(lines) - y})
		if i == m.cursor {
			start, end = y, len(lines)-1
		}
	}
	return strings.Join(lines, "\n"), start, end, targets, nil
}
