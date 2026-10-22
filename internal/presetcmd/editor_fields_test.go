package presetcmd_test

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/buildinfo"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/presetcmd"
	"github.com/temper-sh/temper/internal/userpreset"
)

func focusField(t *testing.T, m *presetcmd.Editor, label string) {
	t.Helper()
	for range 80 {
		key(m, tea.KeyUp)
	}
	for range 80 {
		if strings.Contains(ansi.Strip(m.View().Content), "▎ "+label) {
			return
		}
		key(m, tea.KeyDown)
	}
	t.Fatalf("could not focus %q:\n%s", label, ansi.Strip(m.View().Content))
}

func TestNewPresetNeedsOnlyANameAndNeverOverwritesACollision(t *testing.T) {
	c, root := command(t)
	c = singleSource(c)
	m := editor(t, c, root, "", false)
	key(m, 'n')
	key(m, 'f')
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "Preset name") || strings.Contains(frame, "New preset ID") {
		t.Fatalf("creation still asks for bookkeeping:\n%s", frame)
	}
	input(m, "Writing")
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("naming a draft created a root: %v", err)
	}
	key(m, 's')
	before, err := userpreset.Read(root, "writing")
	if err != nil {
		t.Fatal(err)
	}
	key(m, 'n')
	key(m, 'f')
	input(m, "Writing")
	key(m, 's')
	if read(t, root, "writing-2").Name != "Writing" {
		t.Fatal("collision handling changed the chosen name")
	}
	after, _ := userpreset.Read(root, "writing")
	if !bytes.Equal(before, after) {
		t.Fatal("generated ID overwrote an existing preset")
	}
	key(m, 'n')
	key(m, 'f')
	input(m, "写作")
	key(m, 's')
	if read(t, root, "preset").Name != "写作" {
		t.Fatal("non-Latin display name was lost")
	}
}

func TestSpeculationChoiceKeepsItsDependentValuesConsistent(t *testing.T) {
	for _, form := range []string{"fork", "copy"} {
		t.Run(form, func(t *testing.T) {
			c, root := command(t)
			if _, d, code := run(t, c, root, form, mtp, "mine"); code != 0 {
				t.Fatal(d)
			}
			m := editor(t, c, root, "mine", false)
			for range 3 {
				key(m, tea.KeyRight)
			}
			focusField(t, m, "Method")
			key(m, tea.KeyEnter) // MTP → Off is a complete configuration change.
			frame := ansi.Strip(m.View().Content)
			for _, absent := range []string{"Max draft tokens", "Draft artifact", "Source:"} {
				if strings.Contains(frame, absent) {
					t.Fatalf("inapplicable %q remains:\n%s", absent, frame)
				}
			}
			key(m, 's')
			if out, d, code := run(t, c, root, "check", "mine"); code != 0 {
				t.Fatalf("turning speculation off left incompatible fields: %s %s", out, d)
			}
			key(m, tea.KeyEnter)
			for range 3 {
				key(m, tea.KeyRight)
			}
			focusField(t, m, "Method")
			key(m, tea.KeyEnter)
			if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "Max draft tokens") {
				t.Fatalf("supported speculation cannot be restored:\n%s", frame)
			}
			key(m, 's')
			if out, d, code := run(t, c, root, "check", "mine"); code != 0 {
				t.Fatalf("restored speculation is invalid: %s %s", out, d)
			}
		})
	}
}

func TestFixedAndInapplicableControlsAreAbsent(t *testing.T) {
	c, root := command(t)
	if _, d, code := run(t, c, root, "copy", "ettin-reranker-150m-q8-crispembed", "rerank"); code != 0 {
		t.Fatal(d)
	}
	m := editor(t, c, root, "rerank", false)
	m.Update(tea.WindowSizeMsg{Width: 112, Height: 40})
	frame := ansi.Strip(m.View().Content)
	for _, absent := range []string{"Sampling", "Speculation", "Compute", "Context limit", "Template", "Reasoning"} {
		if strings.Contains(frame, absent) {
			t.Fatalf("reranker offers %q:\n%s", absent, frame)
		}
	}
	key(m, tea.KeyRight)
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "[Engine]") || !strings.Contains(frame, "Threads") {
		t.Fatalf("navigation did not skip empty sections:\n%s", frame)
	}
	if _, d, code := run(t, c, root, "fork", "qwen3.8-27b-q4xl-splash", "splash"); code != 0 {
		t.Fatal(d)
	}
	s := editor(t, c, root, "splash", false)
	s.Update(tea.WindowSizeMsg{Width: 112, Height: 40})
	key(s, tea.KeyRight)
	frame = ansi.Strip(s.View().Content)
	for _, absent := range []string{"Speculation", "Min p", "Repeat penalty", "Presence penalty"} {
		if strings.Contains(frame, absent) {
			t.Fatalf("Splash offers fixed %q:\n%s", absent, frame)
		}
	}
	key(s, tea.KeyRight)
	focusField(t, s, "Reasoning effort")
	key(s, tea.KeyEnter)
	input(s, "none")
	key(s, 's')
	if out, d, code := run(t, c, root, "check", "splash"); code != 0 {
		t.Fatalf("reasoning choice did not update its engine requirements: %s %s", out, d)
	}
}

func TestPythonCompatibilityMetadataSurvivesOrdinaryEditing(t *testing.T) {
	raw, err := os.ReadFile("../testfixture/testdata/study-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	view, err := catalog.Read(raw, buildinfo.DevVersion)
	if err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"rapid-mlx", "vllm-metal"} {
		t.Run(origin, func(t *testing.T) {
			c, root := command(t)
			c.Catalog = func(string) (catalog.View, error) { return view, nil }
			if _, d, code := run(t, c, root, "copy", origin, "mine"); code != 0 {
				t.Fatal(d)
			}
			before := read(t, root, "mine")
			m := editor(t, c, root, "mine", false)
			m.Update(tea.WindowSizeMsg{Width: 112, Height: 80})
			key(m, tea.KeyEnter)
			input(m, "Renamed")
			key(m, tea.KeyRight)
			key(m, tea.KeyRight)
			frame := ansi.Strip(m.View().Content)
			for _, absent := range []string{"Tool call parser", "Reasoning parser", "Block size", "Advanced"} {
				if strings.Contains(frame, absent) {
					t.Fatalf("compatibility field %q is editable:\n%s", absent, frame)
				}
			}
			key(m, 's')
			after := read(t, root, "mine")
			if !reflect.DeepEqual(before.Preset.Profiles, after.Preset.Profiles) || before.Preset.ContextLimitTokens != after.Preset.ContextLimitTokens {
				t.Fatal("hidden compatibility metadata changed during rename")
			}
			if origin == "rapid-mlx" {
				for _, reasoning := range []string{"off", "on"} {
					key(m, tea.KeyEnter) // Reopen after each Save.
					focusField(t, m, "Reasoning")
					key(m, tea.KeyEnter)
					input(m, reasoning)
					key(m, 's')
					if out, d, code := run(t, c, root, "check", "mine"); code != 0 {
						t.Fatalf("reasoning %s lost its parser contract: %s %s", reasoning, out, d)
					}
					preset := read(t, root, "mine").Preset
					parser := preset.Config().Python.RapidMLX.ReasoningParser
					if preset.RequestDefaults.Reasoning != reasoning || reasoning == "on" && parser != before.Preset.Config().Python.RapidMLX.ReasoningParser || reasoning == "off" && parser != "" {
						t.Fatalf("reasoning and hidden parser disagree: %+v, %q", preset.RequestDefaults, parser)
					}
				}
			}
		})
	}
}
