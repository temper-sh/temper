package presetcmd_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/buildinfo"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/userpreset"
)

func TestKVCachePickerUsesTheEnginesChoicesAndSavesTheSelection(t *testing.T) {
	for _, tc := range []struct {
		fixture, origin, field string
		choices                []string
	}{
		{"catalog.json", mtp, "KV precision", []string{"q4", "q8", "f16"}},
		{"catalog.json", "qwen3.8-27b-q4xl-splash", "KV precision", []string{"int8", "bf16"}},
		{"study-catalog.json", "rapid-mlx", "KV cache dtype", []string{"bf16", "int8", "int4"}},
		{"study-catalog.json", "vllm-metal", "KV cache dtype", []string{"auto", "bfloat16", "float16"}},
	} {
		t.Run(tc.origin, func(t *testing.T) {
			c, root := command(t)
			raw, err := os.ReadFile("../testfixture/testdata/" + tc.fixture)
			if err != nil {
				t.Fatal(err)
			}
			view, err := catalog.Read(raw, buildinfo.DevVersion)
			if err != nil {
				t.Fatal(err)
			}
			c.Catalog = func(string) (catalog.View, error) { return view, nil }
			if _, diagnostic, code := run(t, c, root, "copy", tc.origin, "mine"); code != 0 {
				t.Fatal(diagnostic)
			}
			m := editor(t, c, root, "mine", false)
			editorClick(t, m, "Engine")
			focusField(t, m, tc.field)
			key(m, tea.KeyHome)
			for _, choice := range tc.choices {
				frame := ansi.Strip(m.View().Content)
				if !strings.Contains(frame, "● "+choiceLabel(choice)) {
					t.Fatalf("engine choice %q cannot be selected:\n%s", choice, frame)
				}
				key(m, tea.KeyRight)
			}
			key(m, tea.KeyEnd)
			saveEditor(m)
			p := read(t, root, "mine").Preset
			path := "engine_config.kv_cache"
			if tc.origin == "rapid-mlx" {
				path = "engine_config.rapid_mlx.kv_cache_dtype"
			} else if tc.origin == "vllm-metal" {
				path = "engine_config.vllm_metal.kv_cache_dtype"
			}
			if got := catalog.Values(*p)[path]; got != tc.choices[len(tc.choices)-1] {
				t.Fatalf("saved %v, want the selected cache format", got)
			}
		})
	}
}

func choiceLabel(value string) string {
	switch value {
	case "q4", "q8", "f16", "bf16":
		return strings.ToUpper(value)
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func TestChoiceDraftSurvivesResizeAndRequiresExplicitSave(t *testing.T) {
	c, root := command(t)
	if _, d, code := run(t, c, root, "fork", mtp, "mine"); code != 0 {
		t.Fatal(d)
	}
	before, _ := userpreset.Read(root, "mine")
	m := editor(t, c, root, "mine", false)
	editorClick(t, m, "Engine")
	focusField(t, m, "KV precision")
	key(m, tea.KeyEnd)
	m.Update(tea.PasteMsg{Content: "invalid-cache"})
	key(m, 'x')
	for _, width := range []int{112, 40, 80, 28} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		assertEditorFrame(t, m, width, 24)
		frame := ansi.Strip(m.View().Content)
		if !strings.Contains(frame, "● F16") || strings.Contains(frame, "invalid-cache") {
			t.Fatalf("choice changed: %s", frame)
		}
	}
	after, _ := userpreset.Read(root, "mine")
	if !bytes.Equal(before, after) {
		t.Fatal("choice saved implicitly")
	}
	editorClick(t, m, "Discard")
	key(m, tea.KeyEnter) // Keep editing.
	saveEditor(m)
	if got := read(t, root, "mine").Changes["engine_config"].(map[string]any)["kv_cache"]; got != "f16" {
		t.Fatalf("lost choice: %v", got)
	}
}

func TestBooleanTogglePreservesFalseAndSavesABoolean(t *testing.T) {
	c, root := command(t)
	if _, d, code := run(t, c, root, "fork", mtp, "mine"); code != 0 {
		t.Fatal(d)
	}
	m := editor(t, c, root, "mine", false)
	editorClick(t, m, "Engine")
	focusField(t, m, "Drop oldest context when full") // Click toggles false to true.
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "[✓] Drop oldest") {
		t.Fatalf("toggle lost: %s", frame)
	}
	saveEditor(m)
	controls := read(t, root, "mine").Changes["engine_config"].(map[string]any)["controls"].(map[string]any)
	if controls["context_shift"] != true {
		t.Fatalf("wrong boolean: %v", controls)
	}
}

func TestOmittedBooleanStaysOffUntilExplicitlyToggled(t *testing.T) {
	c, root := command(t)
	raw, err := os.ReadFile("../testfixture/testdata/study-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	view, err := catalog.Read(raw, buildinfo.DevVersion)
	if err != nil {
		t.Fatal(err)
	}
	p := view.Document.Presets["vllm-metal"]
	p.Profiles[0].EngineConfig.Python.VLLMMetal.LanguageModelOnly = false
	view.Document.Presets["vllm-metal"] = p
	c.Catalog = func(string) (catalog.View, error) { return view, nil }
	if _, d, code := run(t, c, root, "copy", "vllm-metal", "mine"); code != 0 {
		t.Fatal(d)
	}
	before, _ := userpreset.Read(root, "mine")
	m := editor(t, c, root, "mine", false)
	editorClick(t, m, "Engine")
	for range 80 {
		frame := ansi.Strip(m.View().Content)
		if strings.Contains(frame, "[ ] Language model only") {
			break
		}
		key(m, tea.KeyDown)
	}
	after, _ := userpreset.Read(root, "mine")
	if !bytes.Equal(before, after) {
		t.Fatal("opening a toggle changed the file")
	}
	editorClick(t, m, "Language model only")
	saveEditor(m)
	if !read(t, root, "mine").Preset.Profiles[0].EngineConfig.Python.VLLMMetal.LanguageModelOnly {
		t.Fatal("explicit toggle not saved")
	}
}
