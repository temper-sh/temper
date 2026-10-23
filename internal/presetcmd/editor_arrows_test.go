package presetcmd_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/userpreset"
)

func TestArrowsReachOverviewActionsAndSaveTheChosenPreset(t *testing.T) {
	for _, width := range []int{40, 112} {
		for _, action := range []string{"Fork", "Copy"} {
			t.Run(fmt.Sprintf("%d/%s", width, action), func(t *testing.T) {
				c, root := command(t)
				m := editor(t, singleSource(c), root, "", false)
				m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
				key(m, tea.KeyUp) // Empty Yours → secondary navigation.
				key(m, tea.KeyRight)
				key(m, tea.KeyDown) // Catalog → first preset.
				key(m, tea.KeyEnter)
				frame := ansi.Strip(m.View().Content)
				if !strings.Contains(frame, "Configuration") || !strings.Contains(frame, "Fork") {
					t.Fatalf("arrows did not open the catalog preset:\n%s", frame)
				}
				for steps := 0; steps < 200 && !strings.Contains(frame, "←→ actions"); steps++ {
					key(m, tea.KeyDown)
					frame = ansi.Strip(m.View().Content)
				}
				if !strings.Contains(frame, "←→ actions") {
					t.Fatalf("Down never reached the overview actions:\n%s", frame)
				}
				// Return to content without losing the bottom of the assessment.
				key(m, tea.KeyUp)
				content := ansi.Strip(m.View().Content)
				if !strings.Contains(content, "↑↓ scroll") || strings.Contains(content, "Configuration") {
					t.Fatalf("returning to content lost scroll position:\n%s", content)
				}
				key(m, tea.KeyDown) // At the bottom, immediately return to Fork.
				if action == "Copy" {
					key(m, tea.KeyRight)
				}
				key(m, tea.KeyEnter)
				if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "Preset name") {
					t.Fatalf("arrows did not activate %s:\n%s", action, frame)
				}
				m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
				m.Update(tea.PasteMsg{Content: "Arrow preset"})
				key(m, tea.KeyDown) // Name → Apply.
				key(m, tea.KeyEnter)
				if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("name Apply published the preset: %v", err)
				}
				// First field → sections → breadcrumb → footer, without Tab.
				key(m, tea.KeyUp)
				key(m, tea.KeyUp)
				key(m, tea.KeyUp)
				key(m, tea.KeyEnter)
				u := read(t, root, "arrow-preset")
				if u.Fork() != (action == "Fork") || u.Fork() && u.Origin != mtp {
					t.Fatalf("arrow navigation changed the chosen source/action: %+v", u)
				}
				assertEditorFrame(t, m, width, 24)
			})
		}
	}
}

func TestArrowInputFocusRetainsTextAndRequiresSave(t *testing.T) {
	c, root := command(t)
	if _, d, code := run(t, c, root, "fork", mtp, "mine"); code != 0 {
		t.Fatal(d)
	}
	before, _ := userpreset.Read(root, "mine")
	m := editor(t, c, root, "mine", false)
	key(m, tea.KeyUp)
	key(m, tea.KeyRight)
	key(m, tea.KeyDown) // Sampling.
	typeText(m, "bad")
	key(m, tea.KeyDown)
	key(m, tea.KeyUp)
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "bad") || !strings.Contains(frame, "number") {
		t.Fatalf("navigation lost invalid text: %s", frame)
	}
	typeText(m, "0.4")
	key(m, tea.KeyDown)
	key(m, tea.KeyUp)
	key(m, tea.KeyLeft)
	m.Update(tea.PasteMsg{Content: "5"})
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "0.54") || !strings.Contains(frame, "[Sampling]") {
		t.Fatalf("text cursor changed section: %s", frame)
	}
	after, _ := userpreset.Read(root, "mine")
	if !bytes.Equal(before, after) {
		t.Fatal("navigation saved")
	}
	key(m, tea.KeyUp)
	key(m, tea.KeyUp)
	key(m, tea.KeyEnter)
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "Unsaved changes. Choose") {
		t.Fatalf("breadcrumb bypassed guard: %s", frame)
	}
	key(m, tea.KeyEnter)
	saveEditor(m)
	u := read(t, root, "mine")
	if u.Changes["request_defaults"].(map[string]any)["sampling"].(map[string]any)["temperature"] != 0.54 {
		t.Fatal("lost draft")
	}
}

func TestArrowsReachActionsAfterOnlyTheVisibleTemplateFields(t *testing.T) {
	for _, source := range []struct {
		name            string
		choices, fields int
	}{{"builtin", 0, 1}, {"local", 1, 2}, {"huggingface", 2, 4}} {
		t.Run(source.name, func(t *testing.T) {
			c, root := command(t)
			m := editor(t, singleSource(c), root, "", false)
			key(m, 'n')
			key(m, 'f')
			input(m, "Template draft")
			focusField(t, m, "Template")
			editorClick(t, m, "From model")
			for range source.choices {
				key(m, tea.KeyRight)
			}
			for range source.fields { // Visible controls, then footer.
				key(m, tea.KeyDown)
				m.View()
			}
			if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "←→ actions") {
				t.Fatalf("template fields trapped arrow focus:\n%s", frame)
			}
			key(m, tea.KeyLeft)
			key(m, tea.KeyEnter) // Back, without submitting the form.
			if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "[General]") || !strings.Contains(strings.ToLower(frame), "unsaved") {
				t.Fatalf("arrow Back lost the draft:\n%s", frame)
			}
			if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("navigating a source form wrote authoring state: %v", err)
			}
		})
	}
}

func TestArrowsReachFileActionsAndReturnToTheLastFile(t *testing.T) {
	c, root := withHub(t)
	m := editor(t, singleSource(c), root, "", false)
	key(m, 'n')
	key(m, 'f')
	input(m, "Weight draft")
	focusField(t, m, "Weights")
	key(m, tea.KeyDown) // Revision.
	key(m, tea.KeyDown) // Footer lookup.
	completeRead(t, m, key(m, tea.KeyEnter))
	key(m, tea.KeyDown) // Second file.
	key(m, tea.KeyDown) // Use files.
	key(m, tea.KeyEnter)
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "Select at least one file") {
		t.Fatalf("footer bypassed the file-selection guard:\n%s", frame)
	}
	key(m, tea.KeyDown) // Return to footer after refusal.
	key(m, tea.KeyUp)   // Last file.
	key(m, tea.KeyUp)   // First file.
	key(m, ' ')
	key(m, tea.KeyDown)
	key(m, tea.KeyDown)
	completeRead(t, m, key(m, tea.KeyEnter))
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "Weights changed in the draft") {
		t.Fatalf("arrow Use files did not update the draft:\n%s", frame)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file navigation published before Save: %v", err)
	}
	saveEditor(m)
	u := read(t, root, "weight-draft")
	a := u.Artifacts[u.Changes["artifact"].(string)]
	if len(a.Files) != 1 || a.Files[0].Path != "Qwen3.8-27B-Q6_K.gguf" {
		t.Fatalf("footer Up returned to the wrong file: %+v", a.Files)
	}
}

func TestArrowsSkipEmptyContentAndCannotActivateUnavailableSources(t *testing.T) {
	c, root := command(t)
	v, _ := c.Catalog(root)
	v.Document.Presets = map[string]catalog.Preset{}
	v.Unavailable = []catalog.Unavailable{{Kind: "preset", ID: "future", Reason: "Requires a newer Temper."}}
	c.Catalog = func(string) (catalog.View, error) { return v, nil }
	m := editor(t, c, root, "", false)
	key(m, tea.KeyDown) // Empty Yours → New, not disabled Open.
	key(m, tea.KeyEnter)
	key(m, tea.KeyEnter) // Inspect unavailable source.
	m.View()
	key(m, tea.KeyDown) // Short overview → Back, not disabled Fork.
	key(m, tea.KeyRight)
	key(m, tea.KeyEnter) // Disabled Fork.
	key(m, tea.KeyRight)
	key(m, tea.KeyEnter) // Disabled Copy.
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "Requires a newer Temper.") || strings.Contains(frame, "Preset name") {
		t.Fatalf("arrow activation bypassed the unavailable-source guard:\n%s", frame)
	}
	key(m, tea.KeyRight) // Wrap to Back.
	key(m, tea.KeyEnter)
	key(m, tea.KeyUp) // Catalog selector.
	key(m, tea.KeyLeft)
	key(m, tea.KeyDown)  // Skip empty Yours.
	key(m, tea.KeyEnter) // New.
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "[Catalog]") {
		t.Fatalf("empty list trapped arrow navigation:\n%s", frame)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unavailable or empty navigation wrote authoring state: %v", err)
	}
}

func TestPresetReviewRequiresAnExplicitActionAndKeepsScrollPosition(t *testing.T) {
	c, root := command(t)
	m := editor(t, singleSource(c), root, "", false)
	key(m, 'n')
	key(m, 'f')
	input(m, "Review draft")
	editorClick(t, m, "Review")
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	m.View()
	key(m, tea.KeyEnter) // Read-only content has no action to activate.
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Enter on read-only Review saved the draft: %v", err)
	}
	frame := ansi.Strip(m.View().Content)
	for steps := 0; steps < 200 && !strings.Contains(frame, "←→ actions"); steps++ {
		key(m, tea.KeyDown)
		frame = ansi.Strip(m.View().Content)
	}
	if !strings.Contains(frame, "←→ actions") {
		t.Fatalf("Review never reached actions:\n%s", frame)
	}
	key(m, tea.KeyUp)
	m.View()
	key(m, tea.KeyDown)
	key(m, tea.KeyEnter)
	if read(t, root, "review-draft").Name != "Review draft" {
		t.Fatal("explicit Save lost the draft")
	}
}

func TestPresetConfirmationArrowsStayWithinTheGuard(t *testing.T) {
	c, root := command(t)
	m := editor(t, singleSource(c), root, "", false)
	key(m, 'n')
	key(m, 'f')
	input(m, "Kept draft")
	key(m, tea.KeyEsc)
	key(m, tea.KeyDown) // Discard.
	key(m, tea.KeyDown) // Save.
	key(m, tea.KeyDown) // Confirmation footer.
	key(m, tea.KeyLeft) // Back, not Confirm.
	key(m, tea.KeyEnter)
	if frame := ansi.Strip(m.View().Content); strings.Contains(frame, "Unsaved changes. Choose") || !strings.Contains(frame, "Kept draft") {
		t.Fatalf("confirmation arrows did not return to the draft:\n%s", frame)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("moving through confirmation choices saved the draft: %v", err)
	}
	key(m, tea.KeyEsc)
	key(m, tea.KeyUp)    // Footer with Keep editing still selected.
	key(m, tea.KeyEnter) // Confirm the safe default.
	saveEditor(m)
	if read(t, root, "kept-draft").Name != "Kept draft" {
		t.Fatal("confirmation navigation lost the draft")
	}
}
