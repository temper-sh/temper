package presetcmd_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/presetcmd"
	"github.com/temper-sh/temper/internal/software"
)

func TestCatalogOverviewStartsTheChosenForkOrCopyOnlyOnExplicitSave(t *testing.T) {
	for _, width := range []int{40, 80, 112} {
		for _, action := range []string{"Fork", "Copy"} {
			t.Run(fmt.Sprintf("%d/%s", width, action), func(t *testing.T) {
				c, root := command(t)
				v, _ := c.Catalog(root)
				p := v.Document.Presets[mtp]
				p.Presentation.DisplayName = "Chosen preset"
				p.Presentation.Description = "A coding preset with MTP."
				other := p
				other.Presentation.Description = "A different source."
				v.Document.Presets = map[string]catalog.Preset{"first": other, "chosen": p}
				c.Catalog = func(string) (catalog.View, error) { return v, nil }
				m := editor(t, c, root, "", false)
				m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
				key(m, 'n') // "chosen" sorts before "first".
				editorClick(t, m, "Open")
				frame := ansi.Strip(m.View().Content)
				for _, want := range []string{"Chosen preset", "A coding preset with MTP.", "Configuration", "Fork", "Copy"} {
					if !strings.Contains(frame, want) {
						t.Fatalf("overview missing %q:\n%s", want, frame)
					}
				}
				editorClick(t, m, action)
				editorClick(t, m, "Cancel")
				if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "A coding preset with MTP.") {
					t.Fatalf("Cancel did not return to the same preset:\n%s", frame)
				}
				for range 12 {
					key(m, tea.KeyDown)
					m.View()
				}
				assertEditorFrame(t, m, width, 24)
				if width == 80 {
					key(m, tea.KeyTab)
					key(m, tea.KeyHome)
					key(m, tea.KeyRight) // Fork.
					if action == "Copy" {
						key(m, tea.KeyRight)
					}
					key(m, tea.KeyEnter)
				} else {
					editorClick(t, m, action)
				}
				input(m, "Custom")
				if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("overview or name input published authoring state: %v", err)
				}
				saveEditor(m)
				u := read(t, root, "custom")
				if action == "Fork" {
					if !u.Fork() || u.Origin != "chosen" {
						t.Fatalf("fork lost the viewed source: %+v", u)
					}
				} else if u.Fork() || u.Preset.Artifact != p.Artifact || u.Preset.Presentation.Description != p.Presentation.Description {
					t.Fatalf("copy lost the viewed source: %+v", u)
				}
			})
		}
	}
}

func TestUnavailableCatalogOverviewExplainsWhyAndBlocksAuthoring(t *testing.T) {
	c, root := command(t)
	v, _ := c.Catalog(root)
	v.Document.Presets = map[string]catalog.Preset{}
	v.Unavailable = []catalog.Unavailable{{Kind: "preset", ID: "future", Reason: "Requires a newer Temper.",
		Presentation: catalog.PresetPresentation{DisplayName: "Future preset", Description: "A newer engine configuration."}}}
	c.Catalog = func(string) (catalog.View, error) { return v, nil }
	m := editor(t, c, root, "", false)
	key(m, 'n')
	key(m, tea.KeyEnter)
	for _, action := range []string{"Fork", "Copy"} {
		editorClick(t, m, action)
	}
	key(m, 'f')
	key(m, 'c')
	frame := ansi.Strip(m.View().Content)
	if !strings.Contains(frame, "Requires a newer Temper.") || strings.Contains(frame, "Preset name") {
		t.Fatalf("unavailable overview lost its reason or opened authoring:\n%s", frame)
	}
	editorClick(t, m, "Presets")
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "[Catalog]") {
		t.Fatalf("breadcrumb did not return to catalog:\n%s", frame)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unavailable preset created state: %v", err)
	}
}

func TestOverviewMemoryRefusalDoesNotBlockAValidCopy(t *testing.T) {
	c, root := command(t)
	c = singleSource(c)
	facts := c.Facts
	c.Facts = func(ctx context.Context) (machine.Facts, error) {
		f, err := facts(ctx)
		f.PhysicalMemoryBytes = 8 << 30
		f.MetalDeviceMemoryMiB, f.WiredLimitMiB = 4<<10, 4<<10
		return f, err
	}
	m, err := c.NewEditor(context.Background(), presetcmd.EditorOptions{Root: root, MemoryGiB: 32})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	m.Update(tea.WindowSizeMsg{Width: 112, Height: 100})
	key(m, 'n')
	key(m, tea.KeyEnter)
	frame := strings.Join(strings.Fields(ansi.Strip(m.View().Content)), " ")
	for _, want := range []string{"Configuration", "Memory", "This Mac", "Total runtime fit is unknown", "not resident memory"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("overview hides assessment context %q:\n%s", want, frame)
		}
	}
	editorClick(t, m, "Copy")
	input(m, "For another Mac")
	saveEditor(m)
	if read(t, root, "for-another-mac").Fork() {
		t.Fatal("memory refusal changed copy semantics")
	}
}

func TestOverviewPreservesTheMeaningOfMeasuredMemory(t *testing.T) {
	for _, sampled := range []bool{false, true} {
		t.Run(fmt.Sprint(sampled), func(t *testing.T) {
			c, root := command(t)
			v, _ := c.Catalog(root)
			facts, _ := c.Facts(context.Background())
			lock, err := catalog.CompilePreset(v.Document, mtp, 32, 0, software.Target{OS: "darwin", Arch: "arm64"})
			if err != nil {
				t.Fatal(err)
			}
			p := lock.Records.Presets[mtp]
			measurement := &catalog.MemoryMeasurement{
				ObservedOn: "2026-10-22", Source: "https://example.test/assessment", Counter: "engine-physical-footprint",
				PeakBytes: 18 << 30, Workload: "Fixture workload with long prompts.", Limitations: "Engine only; excludes other processes.",
				Machine:   catalog.MemoryMachine{Chip: facts.Chip, PhysicalMemoryBytes: facts.PhysicalMemoryBytes, MetalMemoryMiB: facts.MetalDeviceMemoryMiB, OS: "macOS"},
				Execution: lock.MemoryExecution(),
			}
			loaded := "Not sampled"
			if sampled {
				bytes := int64(16 << 30)
				measurement.LoadedBytes, loaded = &bytes, "16.00 GiB"
			}
			p.Profiles[0].Memory = measurement
			v.Document.Presets = map[string]catalog.Preset{mtp: p}
			c.Catalog = func(string) (catalog.View, error) { return v, nil }
			m := editor(t, c, root, "", false)
			m.Update(tea.WindowSizeMsg{Width: 112, Height: 100})
			key(m, 'n')
			key(m, tea.KeyEnter)
			frame := strings.Join(strings.Fields(ansi.Strip(m.View().Content)), " ")
			for _, want := range []string{"Memory · measured", "18.00 GiB", loaded, "65536 tokens", measurement.Counter,
				measurement.Workload, measurement.Limitations, measurement.ObservedOn, measurement.Source, "not a worst-case memory guarantee"} {
				if !strings.Contains(frame, want) {
					t.Fatalf("measured memory lost %q:\n%s", want, frame)
				}
			}
		})
	}
}

func TestOverviewDoesNotInventMemoryWhenMachineFactsAreMissing(t *testing.T) {
	c, root := command(t)
	c = singleSource(c)
	c.Facts = func(context.Context) (machine.Facts, error) { return machine.Facts{}, errors.New("unavailable") }
	m, err := c.NewEditor(context.Background(), presetcmd.EditorOptions{Root: root, MemoryGiB: 32})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	m.Update(tea.WindowSizeMsg{Width: 112, Height: 100})
	key(m, 'n')
	key(m, tea.KeyEnter)
	frame := ansi.Strip(m.View().Content)
	if !strings.Contains(frame, "Machine memory assessment is unavailable.") || strings.Contains(frame, "0.00 GiB") {
		t.Fatalf("missing machine facts were presented as an estimate:\n%s", frame)
	}
	editorClick(t, m, "Fork")
	editorClick(t, m, "Cancel")
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("viewing or cancelling wrote state: %v", err)
	}
}
