package setupui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/software"
)

func TestComputeEditRefreshesMemoryAndSurvivesOtherEdits(t *testing.T) {
	m := presetEditor(t, "qwen3.8-27b-q4xl-splash", "nuextract3-q4km-llama")
	facts := machine.Facts{Schema: machine.FactsSchemaV1, Target: software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "26.6"}, HardwareModel: "MacTest,1", Chip: "Apple M5", OSBuild: "fixture", PhysicalMemoryBytes: 32 << 30, MetalDeviceMemoryMiB: 2048, MetalDeviceMemorySource: machine.MetalDeviceSourceLive, WiredLimitMiB: 2048, WiredLimitSource: budget.WiredSourceMetal}
	m.input.Assess = func(lock catalog.Lock) (setup.PresetPlan, error) { return setup.Assess(lock, facts) }
	editorKey(m, 's')
	p, _ := m.selectedOption()
	base, err := setup.Assess(p.Lock, facts)
	if err != nil || len(base.Refusals) == 0 {
		t.Fatalf("fixture must exceed GPU allowance: %v", err)
	}
	for i := range m.input.Presets {
		if m.input.Presets[i].ID == p.ID {
			m.input.Presets[i].Unavailable = strings.Join(base.Refusals, "; ")
		}
	}
	editorKey(m, 'p')
	_, cpu := presetRecord(m.draft.Presets[p.ID].Lock)
	if cpu.Compute != catalog.ComputeCPU || m.visible()[0].Unavailable != "" {
		t.Fatalf("CPU edit left a stale GPU refusal: %s", m.notice)
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Compute CPU · Context 8,192 tokens") {
		t.Fatalf("picker did not place the selected device before Context on the same line:\n%s", view)
	}
	m.draft.Layouts["work"] = setup.Layout{Name: "Work", Presets: []string{p.ID}, Startup: []string{p.ID}, IdleSeconds: 60}
	editorKey(m, 'c')
	m.text.SetValue("4096")
	m.acceptEdit()
	_, edited := presetRecord(m.draft.Presets[p.ID].Lock)
	if edited.Compute != catalog.ComputeCPU || edited.Window() != 4096 || !strings.Contains(m.notice, "work") {
		t.Fatalf("context edit reset compute or lost layout references: %s", m.notice)
	}
	if got := m.draft.Presets[p.ID].Choices; got != (setup.Choices{ContextWindow: 4096, Compute: catalog.ComputeCPU}) {
		t.Fatalf("edits were not recorded as choices for temper update: %+v", got)
	}
	before, _ := m.draft.Bytes()
	editorKey(m, 't') // The template belongs to the preset; the wizard cannot change it.
	after, _ := m.draft.Bytes()
	if string(before) != string(after) || m.notice != "" {
		t.Fatal("a Template key changed the selected preset")
	}
	for _, width := range []int{120, 80, 28} {
		view := ansi.Strip(reviewPresetTable(m.draft, []string{p.ID}, nil, width))
		if !strings.Contains(view, "CPU") {
			t.Fatalf("review omitted device at %d columns:\n%s", width, view)
		}
	}
	editorKey(m, 'g')
	editorKey(m, 's')
	editorKey(m, 'p')
	_, gpu := presetRecord(m.draft.Presets[p.ID].Lock)
	if gpu.Compute != catalog.ComputeGPU || gpu.Window() != 4096 || m.visible()[0].Unavailable == "" {
		t.Fatal("GPU round trip lost settings or memory refusal")
	}
	if !catalog.SameExecution(m.input.Presets[1].Lock, p.Lock) {
		t.Fatal("edit mutated the input preset")
	}
}

func TestReviewShowsRemainingContextForAnOmittedOutputLimit(t *testing.T) {
	m := presetEditor(t, "qwen3.8-27b-q4xl-mtp")
	editorKey(m, ' ')
	p, _ := m.selectedOption()
	selected := m.draft.Presets[p.ID]
	_, preset := presetRecord(selected.Lock)
	var err error
	selected.Lock, err = catalog.ConfigureExecution(selected.Lock, selected.Lock.Preset, catalog.ExecutionSettings{ContextWindowTokens: preset.Window()})
	if err != nil {
		t.Fatal(err)
	}
	m.draft.Presets[p.ID] = selected
	for _, width := range []int{120, 80, 40} {
		view := ansi.Strip(reviewPresetTable(m.draft, []string{p.ID}, nil, width))
		if !strings.Contains(view, "remaining") || strings.Contains(view, " / 0") {
			t.Fatalf("review misrepresents the omitted cap at %d columns:\n%s", width, view)
		}
	}
}

func TestComputeControlUsesKeyboardAndMouseAtNarrowWidths(t *testing.T) {
	for _, tc := range []struct {
		id           string
		before, next catalog.ComputeDevice
	}{
		{"nuextract3-q4km-llama", catalog.ComputeGPU, catalog.ComputeCPU},
		{"qwen3-reranker-0.6b-q8-llama", catalog.ComputeCPU, catalog.ComputeGPU},
	} {
		for _, width := range []int{100, 28} {
			t.Run(fmt.Sprintf("%s/%d", tc.id, width), func(t *testing.T) {
				m := presetEditor(t, tc.id)
				m.Update(tea.WindowSizeMsg{Width: width, Height: 18})
				editorKey(m, 's')
				m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
				for i := 0; i < 2; i++ {
					m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
				}
				if m.footerButtons()[m.action].target.key != "p" {
					t.Fatal("Compute is not reachable from the footer")
				}
				if view := strings.ToLower(ansi.Strip(m.View().Content)); strings.Contains(view, "template") {
					t.Fatalf("a preset with its model's own template shows a Template control:\n%s", view)
				}
				m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				p, _ := m.selectedOption()
				_, chosen := presetRecord(m.draft.Presets[p.ID].Lock)
				if chosen.Compute != tc.next {
					t.Fatalf("keyboard Compute failed: %s", m.notice)
				}
				assertFrameFits(t, m.View().Content, width, 18)
				clickPresetText(t, m, "Compute")
				_, chosen = presetRecord(m.draft.Presets[p.ID].Lock)
				if chosen.Compute != tc.before {
					t.Fatal("mouse Compute failed")
				}
			})
		}
	}
}

func TestFixedComputeCardsOmitControlAndIgnoreShortcut(t *testing.T) {
	for _, tc := range []struct {
		id, label string
		category  rune
		steps     int
	}{
		{"qwen3.5-4b-q4km-off", "Context", 'o', 2},
		{"ettin-reranker-150m-q8-crispembed", "CPU only", 's', 2},
	} {
		for _, width := range []int{100, 28} {
			t.Run(fmt.Sprintf("%s/%d", tc.id, width), func(t *testing.T) {
				m := presetEditor(t, tc.id)
				m.Update(tea.WindowSizeMsg{Width: width, Height: 35})
				editorKey(m, tc.category)
				view := ansi.Strip(m.View().Content)
				if !strings.Contains(view, tc.label) || strings.Contains(view, "Compute") || strings.Contains(view, "GPU") || strings.Contains(view, " · Reranking") {
					t.Fatalf("fixed-device card contains redundant metadata or lost its CPU-only label:\n%s", view)
				}
				assertFrameFits(t, m.View().Content, width, 35)
				editorKey(m, 'p')
				if len(m.draft.Presets) != 0 || m.notice != "" {
					t.Fatal("hidden Compute shortcut acted on a fixed-device preset")
				}
				m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
				for i := 0; i < tc.steps; i++ {
					m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
				}
				m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
				if m.Decision.Action != "cancel" || len(m.draft.Presets) != 0 {
					t.Fatal("footer navigation retained a hidden Compute control")
				}
			})
		}
	}
}

func TestRemovingComputeControlKeepsFooterNavigationAligned(t *testing.T) {
	m := presetEditor(t, "gemma-4-31b-qat-ud-q4-k-xl-llama", "nuextract3-q4km-llama")
	editorKey(m, 's')
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	for i := 0; i < 2; i++ {
		m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	}
	editorKey(m, 'g') // The focused Compute button disappears.
	if view := ansi.Strip(m.View().Content); strings.Contains(view, "Compute") {
		t.Fatalf("fixed-device footer retained Compute:\n%s", view)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.Decision.Action != "cancel" || m.page != 0 {
		t.Fatal("keyboard focus disagreed with the displayed footer after removing Compute")
	}
}

func TestComputeAssessmentFailureLeavesDraftUnchanged(t *testing.T) {
	m := presetEditor(t, "nuextract3-q4km-llama")
	editorKey(m, 's')
	editorKey(m, ' ')
	before, _ := m.draft.Bytes()
	m.input.Assess = func(catalog.Lock) (setup.PresetPlan, error) {
		return setup.PresetPlan{}, errors.New("assessment unavailable")
	}
	editorKey(m, 'p')
	after, _ := m.draft.Bytes()
	if string(before) != string(after) || m.notice != "assessment unavailable" {
		t.Fatal("failed assessment changed the selected execution")
	}
}
