package presetcmd

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/tui"
)

// A catalog overview retains the source being viewed independently of list
// selection and scroll position. Its assessment never grants a write.
type presetOverview struct {
	source                 managerPreset
	description            string
	lock                   *catalog.Lock
	assessment             *setup.PresetPlan
	problem, memoryProblem string
}

func (o *presetOverview) name() string {
	if o.source.name != "" {
		return o.source.name
	}
	return o.source.id
}

func (m *Editor) inspectCatalog(source managerPreset) {
	o := &presetOverview{source: source, description: m.view.Document.Presets[source.id].Presentation.Description,
		memoryProblem: "Machine memory assessment is unavailable."}
	lock, err := catalog.CompilePreset(m.view.Document, source.id, m.memory, 0, target)
	if err != nil {
		o.problem = err.Error()
		for _, unavailable := range m.view.Unavailable {
			if unavailable.Kind == "preset" && unavailable.ID == source.id {
				o.problem, o.description = unavailable.Reason, unavailable.Presentation.Description
				break
			}
		}
	} else {
		o.lock = &lock
		if m.facts != nil {
			assessment, err := setup.Assess(lock, *m.facts)
			if err != nil {
				o.memoryProblem += " " + err.Error()
			} else {
				o.assessment = &assessment
			}
		}
	}
	m.overview, m.mode = o, "overview"
	m.viewport.GotoTop()
}

func (m *Editor) overviewKey(key string) tea.Cmd {
	switch key {
	case "esc", "q":
		m.overview, m.mode = nil, "manager"
		m.viewport.GotoTop()
	case "f":
		return m.beginNew(false)
	case "c":
		return m.beginNew(true)
	}
	return nil
}

func overviewText(text string, tone tui.Element, width int) string {
	return tui.Style(tone).Width(width).Render(ansi.Wrap(terminalText(text), width, ""))
}

func overviewSection(title string, rows []tui.FieldRow, width int) string {
	parts := []string{tui.Style(tui.Heading).Bold(true).Render(title)}
	for _, row := range rows {
		row.Value, row.Detail = terminalText(row.Value), terminalText(row.Detail)
		parts = append(parts, row.Render(width))
	}
	return strings.Join(parts, "\n\n")
}

func (o *presetOverview) render(width int) string {
	parts := []string{tui.Style(tui.Heading).Bold(true).Width(width).Render(ansi.Wrap(terminalText(o.name()), width, ""))}
	if o.description != "" {
		parts = append(parts, overviewText(o.description, tui.Muted, width))
	}
	if o.lock == nil {
		parts = append(parts, overviewSection("Unavailable", nil, width), overviewText(o.problem, tui.Error, width))
		return strings.Join(parts, "\n\n")
	}
	p := o.lock.Records.Presets[o.source.id]
	a := o.lock.Records.Artifacts[p.Artifact]
	e := o.lock.Records.Engines[p.Engine]
	engine := e.Presentation.DisplayName
	if engine == "" {
		engine = p.Engine
	}
	if e.Supply.Release != nil {
		engine += " · " + e.Supply.Release.Version
	}
	weights := a.Presentation.WeightsName
	if weights == "" {
		weights = a.Format
		if len(a.Files) > 0 {
			weights = a.Files[0].Path
		}
	}
	rows := []tui.FieldRow{{Label: "Engine", Value: engine}, {Label: "Weights", Value: weights}}
	if p.Interface == "chat-completions" {
		rows = append(rows, tui.FieldRow{Label: "Context", Value: fmt.Sprintf("%d tokens", p.Window())})
		if p.RequestDefaults.Reasoning != "" {
			rows = append(rows, tui.FieldRow{Label: "Reasoning", Value: p.RequestDefaults.Reasoning})
		}
		if p.Speculation.Method != "" && p.Speculation.Method != "none" {
			rows = append(rows, tui.FieldRow{Label: "Speculation", Value: strings.ToUpper(p.Speculation.Method)})
		}
	}
	parts = append(parts, overviewSection("Configuration", rows, width))
	if o.assessment == nil {
		parts = append(parts, overviewSection("Memory", nil, width), overviewText(o.memoryProblem, tui.Muted, width))
	} else {
		parts = append(parts, overviewMemory(o.assessment.Memory, width))
		if len(o.assessment.Refusals) > 0 {
			parts = append(parts, overviewSection("This Mac", nil, width))
			for _, refusal := range o.assessment.Refusals {
				parts = append(parts, overviewText(refusal, tui.Error, width))
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

func overviewMemory(memory setup.MemoryEstimate, width int) string {
	if measured := memory.Measurement; memory.Basis == "measured" && measured != nil {
		loaded := "Not sampled"
		if measured.LoadedBytes != nil {
			loaded = setup.Size(*measured.LoadedBytes)
		}
		rows := []tui.FieldRow{
			{Label: "Peak", Value: setup.Size(measured.PeakBytes)},
			{Label: "Loaded", Value: loaded},
			{Label: "Counter", Value: measured.Counter},
			{Label: "Context", Value: fmt.Sprintf("%d tokens", memory.ContextTokens)},
			{Label: "Workload", Value: measured.Workload},
			{Label: "Measured on", Value: measured.Machine.Chip + " · " + setup.Size(measured.Machine.PhysicalMemoryBytes) + " RAM", Detail: measured.ObservedOn},
		}
		return strings.Join([]string{overviewSection("Memory · measured", rows, width),
			overviewText(measured.Limitations+" Observed peak is not a worst-case memory guarantee.", tui.Muted, width),
			overviewText("Assessment: "+measured.Source, tui.Muted, width)}, "\n\n")
	}
	cache := "Unknown"
	if memory.KVCacheBytes != nil {
		cache = setup.Size(*memory.KVCacheBytes) + " estimated payload"
	}
	format := memory.CacheFormat
	if format == "" {
		format = "not modeled"
	}
	rows := []tui.FieldRow{
		{Label: "Accounted", Value: setup.Size(memory.AccountedBytes), Detail: "Total runtime fit is unknown; some allocations remain unmeasured."},
		{Label: "Weight files", Value: setup.Size(memory.WeightFileBytes), Detail: "Storage proxy, not resident memory."},
		{Label: "KV/context cache", Value: cache, Detail: fmt.Sprintf("%d tokens · %d parallel slot(s) · %s", memory.ContextTokens, memory.ParallelSlots, format)},
	}
	if memory.PromptCacheReserveBytes > 0 {
		rows = append(rows, tui.FieldRow{Label: "Prompt cache", Value: setup.Size(memory.PromptCacheReserveBytes), Detail: "Configured RAM cap, not observed allocation."})
	}
	if memory.EngineEnvelopeBytes > 0 {
		rows = append(rows, tui.FieldRow{Label: "Engine envelope", Value: setup.Size(memory.EngineEnvelopeBytes), Detail: "Includes weights/cache; not added to them."})
	}
	if memory.EngineLimitBytes > 0 {
		rows = append(rows, tui.FieldRow{Label: "Engine limit", Value: setup.Size(memory.EngineLimitBytes), Detail: "A ceiling, not a required allocation."})
	}
	parts := []string{overviewSection("Memory · estimate", rows, width)}
	if memory.FallbackReason != "" {
		parts = append(parts, overviewText("Using an estimate: "+memory.FallbackReason+".", tui.Muted, width))
	}
	if len(memory.Unknown) > 0 {
		parts = append(parts, overviewText("Not measured:\n\n• "+strings.Join(memory.Unknown, "\n\n• "), tui.Muted, width))
	}
	return strings.Join(parts, "\n\n")
}
