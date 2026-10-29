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
	"github.com/temper-sh/temper/internal/modelmemory"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/software"
)

func wizardType(m *PresetModel, value string) {
	m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m.Update(tea.PasteMsg{Content: value})
}
func setWizardContext(m *PresetModel, value string) {
	m.activateShortcut("c")
	wizardType(m, value)
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // Apply, retaining its new assessment.
}

func TestMeasuredCardFallsBackAfterContextEditAndRecoversAtTestedContext(t *testing.T) {
	m := presetEditor(t, "qwen3.8-27b-q4xl-splash")
	option := &m.input.Presets[0]
	observed := machine.Facts{Schema: machine.FactsSchemaV1, Target: software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "26.6"},
		HardwareModel: "MacTest,1", Chip: "Apple M5", OSBuild: "fixture", PhysicalMemoryBytes: 32 << 30,
		MetalDeviceMemoryMiB: 24 * 1024, MetalDeviceMemorySource: machine.MetalDeviceSourceLive,
		WiredLimitMiB: 24 * 1024, WiredLimitSource: budget.WiredSourceMetal}
	p := option.Lock.Records.Presets[option.Lock.Preset]
	window := p.Window()
	p.Profiles[0].Memory = &catalog.MemoryMeasurement{
		ObservedOn: "2026-10-20", Source: "https://example.com/assessment", Workload: "Fixture workload",
		Counter: "engine-physical-footprint", PeakBytes: 20 << 30, Limitations: "Fixture observation; loaded sample missing.",
		Machine: catalog.MemoryMachine{Chip: observed.Chip, PhysicalMemoryBytes: observed.PhysicalMemoryBytes,
			MetalMemoryMiB: observed.MetalDeviceMemoryMiB, OS: "macOS 26.6"},
		Execution: option.Lock.MemoryExecution(),
	}
	option.Lock.Records.Presets[option.Lock.Preset] = p
	m.input.Assess = func(l catalog.Lock) (setup.PresetPlan, error) { return setup.Assess(l, observed) }
	initial, err := m.input.Assess(option.Lock)
	if err != nil || len(initial.Refusals) != 0 {
		t.Fatal(initial.Refusals, err)
	}
	option.MemoryBytes, option.MemoryBasis = initial.MemoryBytes, initial.Memory.Basis
	m.group = option.Group
	editorKey(m, ' ')
	for _, step := range []struct {
		context int
		basis   string
		label   string
	}{
		{window / 2, "estimated", "Memory estimate"},
		{window, "measured", "Measured peak"},
	} {
		setWizardContext(m, fmt.Sprint(step.context))
		visible := m.visible()[0]
		if visible.MemoryBasis != step.basis || visible.Unavailable != "" {
			t.Fatal("card kept a stale basis or refusal", visible.MemoryBasis, visible.Unavailable, m.notice)
		}
		if view := ansi.Strip(m.View().Content); !strings.Contains(view, step.label) {
			t.Fatal("card did not explain its selected memory", view)
		}
	}
}

func TestContextEditRefreshesTheVisibleCacheEstimate(t *testing.T) {
	m := presetEditor(t, "qwen3.5-4b-q4km-off")
	option := &m.input.Presets[0]
	p := option.Lock.Records.Presets[option.Lock.Preset]
	a := option.Lock.Records.Artifacts[p.Artifact]
	a.ContextMemory = &modelmemory.Attention{Kind: "full-attention/v1", Architecture: "qwen3", Layers: 32, KVHeads: 8, KeyLength: 128, ValueLength: 128}
	option.Lock.Records.Artifacts[p.Artifact] = a
	observed := machine.Facts{Schema: machine.FactsSchemaV1, Target: software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "26.6"}, HardwareModel: "Test,1", Chip: "Apple M5", OSBuild: "test",
		PhysicalMemoryBytes: 64 << 30, MetalDeviceMemoryMiB: 48 * 1024, MetalDeviceMemorySource: machine.MetalDeviceSourceLive, WiredLimitMiB: 48 * 1024, WiredLimitSource: budget.WiredSourceMetal}
	m.input.Assess = func(l catalog.Lock) (setup.PresetPlan, error) { return setup.Assess(l, observed) }
	m.group = option.Group
	editorKey(m, ' ')
	setWizardContext(m, "4096")
	before := m.visible()[0].MemoryBytes
	setWizardContext(m, "32768")
	after := m.visible()[0].MemoryBytes
	if before == 0 || after <= before {
		t.Fatal("context edit left stale card memory", before, after, m.notice)
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, setup.Size(after)) || !strings.Contains(view, "Total fit unknown") {
		t.Fatal(view)
	}
}

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
	editorKey(m, ' ')
	editorKey(m, 'p')
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, cpu := presetRecord(m.draft.Presets[p.ID].Lock)
	if cpu.Compute != catalog.ComputeCPU || m.visible()[0].Unavailable != "" {
		t.Fatalf("CPU edit left a stale GPU refusal: %s", m.notice)
	}
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Compute CPU · Context 8,192 tokens") {
		t.Fatalf("picker did not place the selected device before Context on the same line:\n%s", view)
	}
	m.draft.Layouts["work"] = setup.Layout{Name: "Work", Presets: []string{p.ID}, Startup: []string{p.ID}, IdleSeconds: 60}
	setWizardContext(m, "4096")
	_, edited := presetRecord(m.draft.Presets[p.ID].Lock)
	if edited.Compute != catalog.ComputeCPU || edited.Window() != 4096 || len(m.draft.References(p.ID)) != 1 {
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
	editorKey(m, 'g')
	editorKey(m, 's')
	editorKey(m, 'p')
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, gpu := presetRecord(m.draft.Presets[p.ID].Lock)
	if gpu.Compute != catalog.ComputeGPU || gpu.Window() != 4096 || m.visible()[0].Unavailable == "" {
		t.Fatal("GPU round trip lost settings or memory refusal")
	}
	if !catalog.SameExecution(m.input.Presets[1].Lock, p.Lock) {
		t.Fatal("edit mutated the input preset")
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
				editorKey(m, ' ')
				editorKey(m, 'p')
				m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
				m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

				p, _ := m.selectedOption()
				_, chosen := presetRecord(m.draft.Presets[p.ID].Lock)
				if chosen.Compute != tc.next {
					t.Fatalf("keyboard Compute failed: %s", m.notice)
				}
				assertFrameFits(t, m.View().Content, width, 18)
				editorKey(m, 'p')
				clickPresetText(t, m, "○ "+strings.ToUpper(string(tc.before)))
				clickPresetText(t, m, "Apply")
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

func TestChangingCategoryKeepsFooterNavigationAligned(t *testing.T) {
	m := presetEditor(t, "gemma-4-31b-qat-ud-q4-k-xl-llama", "nuextract3-q4km-llama")
	editorKey(m, 's')
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight}) // Context.
	editorKey(m, 'g')                             // Keep the footer action aligned after changing category.
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
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	after, _ := m.draft.Bytes()
	if string(before) != string(after) || m.notice != "assessment unavailable" || m.field != "compute" {
		t.Fatal("failed assessment changed the selected execution")
	}
	m.input.Assess = nil
	clickPresetText(t, m, "Apply")
	p, _ := m.selectedOption()
	_, record := presetRecord(m.draft.Presets[p.ID].Lock)
	if record.Compute != catalog.ComputeCPU || m.field != "" {
		t.Fatal("retry did not apply the pending device")
	}
}

func TestComputePromptRequiresAnIncludedPreset(t *testing.T) {
	m := presetEditor(t, "nuextract3-q4km-llama")
	editorKey(m, 's')
	editorKey(m, 'p')
	if m.field != "" || len(m.draft.Presets) != 0 || !strings.Contains(m.notice, "Include the preset") {
		t.Fatal("Compute customized an unselected preset")
	}
	clickPresetText(t, m, "p Compute")
	if m.field != "" || len(m.draft.Presets) != 0 {
		t.Fatal("disabled Compute action selected a preset")
	}
}

func TestComputePromptStagesChangesAndCancelRestoresCurrentDevice(t *testing.T) {
	for _, cancel := range []string{"Escape", "Cancel"} {
		t.Run(cancel, func(t *testing.T) {
			m := presetEditor(t, "nuextract3-q4km-llama")
			editorKey(m, 's')
			editorKey(m, ' ')
			before, _ := m.draft.Bytes()
			clickPresetText(t, m, "p Compute")
			if view := ansi.Strip(m.View().Content); !strings.Contains(view, "● GPU") || !strings.Contains(view, "Apply") {
				t.Fatalf("Compute did not open at the current device:\n%s", view)
			}
			clickPresetText(t, m, "○ CPU")
			pending, _ := m.draft.Bytes()
			if string(before) != string(pending) || len(m.assessments) != 0 {
				t.Fatal("choosing a device changed the draft or memory assessment before Apply")
			}
			if cancel == "Escape" {
				m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
			} else {
				clickPresetText(t, m, "Cancel")
			}
			editorKey(m, 'p')
			if view := ansi.Strip(m.View().Content); !strings.Contains(view, "● GPU") || m.Decision.Action != "" {
				t.Fatalf("cancelled choice was retained or wizard exited:\n%s", view)
			}
			m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
			if view := ansi.Strip(m.View().Content); strings.Contains(view, "● GPU") || strings.Contains(view, "○ CPU") || !strings.Contains(view, "Compute GPU") {
				t.Fatalf("card kept inline choices or lost its device summary:\n%s", view)
			}
		})
	}
}

func TestComputePromptIsReachableWithArrowsInNarrowTerminal(t *testing.T) {
	m := presetEditor(t, "nuextract3-q4km-llama")
	m.Update(tea.WindowSizeMsg{Width: 28, Height: 12})
	editorKey(m, 's')
	editorKey(m, ' ')
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})  // Actions, directly after the card.
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight}) // Context.
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight}) // Compute.
	if view := ansi.Strip(m.View().Content); !strings.Contains(view, "Compute") {
		t.Fatalf("focused Compute action is not visible:\n%s", view)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.field != "compute" {
		t.Fatal("Compute action did not open its prompt")
	}
	assertFrameFits(t, m.View().Content, 28, 12)
	editorKey(m, ' ')                            // GPU -> CPU.
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // Apply.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	p, _ := m.selectedOption()
	_, record := presetRecord(m.draft.Presets[p.ID].Lock)
	if record.Compute != catalog.ComputeCPU || m.field != "" || m.Decision.Action != "" {
		t.Fatal("arrow Apply did not update only the revocable draft")
	}
	assertFrameFits(t, m.View().Content, 28, 12)
}
