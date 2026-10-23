package presetcmd

import (
	tea "charm.land/bubbletea/v2"
	"github.com/temper-sh/temper/internal/tui"
)

const choiceArea = tui.ActionsFocus + 1 // Pointer targets; keyboard focus stays in content.

func (m *Editor) focusInput() tea.Cmd { return m.text.Focus() }
