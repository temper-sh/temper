package setup

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/machine"
)

func (p Plan) MarshalJSON() ([]byte, error) {
	type wire Plan
	if p.Configuration == nil {
		return json.Marshal(wire(p))
	}
	raw, err := json.Marshal(wire(p))
	if err != nil {
		return nil, err
	}
	var result map[string]json.RawMessage
	if err = json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	assessments := map[string]any{}
	for id, selected := range p.Configuration.Presets {
		for _, m := range p.Presets {
			if catalog.SameExecution(m.Lock, selected.Lock) {
				assessments[id] = struct {
					Name                     string                `json:"name"`
					Compute                  catalog.ComputeDevice `json:"compute"`
					ModelBytes               int64                 `json:"model_bytes"`
					RuntimeDiskEstimateBytes int64                 `json:"runtime_disk_estimate_bytes,omitempty"`
					Memory                   budget.Prediction     `json:"memory_prediction"`
					WiredMemory              *WiredMemoryAdvice    `json:"wired_memory_advice,omitempty"`
					Refusals                 []string              `json:"refusals,omitempty"`
				}{selected.Name, selected.Lock.Records.Presets[selected.Lock.Preset].Compute, m.ModelBytes, m.RuntimeDiskEstimateBytes, m.Budget, m.WiredMemory, m.Refusals}
				break
			}
		}
	}
	result["presets"], err = json.Marshal(assessments)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

type LayoutPlan struct {
	ID                 string   `json:"id"`
	Layout             Layout   `json:"layout"`
	StartupBytes       int64    `json:"startup_bytes"`
	LargestPresetBytes int64    `json:"largest_preset_bytes"`
	AllFit             bool     `json:"all_fit"`
	Refusals           []string `json:"refusals,omitempty"`
}

func BuildConfiguration(root string, facts machine.Facts, free int64, c Configuration, material Material) (Plan, error) {
	if err := c.Validate(); err != nil {
		return Plan{}, err
	}
	var locks []catalog.Lock
	for _, id := range keys(c.Presets) {
		locks = append(locks, c.Presets[id].Lock)
	}
	p, err := build(root, facts, free, locks, material)
	if err != nil {
		return p, err
	}
	p.Configuration = &c
	for _, id := range keys(c.Layouts) {
		l := c.Layouts[id]
		assessment := LayoutPlan{ID: id, Layout: l}
		var total, gpuTotal, startupGPU int64
		for _, id := range l.Presets {
			lock := c.Presets[id].Lock
			one, err := Assess(lock, facts)
			if err != nil {
				return p, err
			}
			estimate := one.MemoryBytes
			if err = add(&total, estimate); err != nil {
				return p, err
			}
			assessment.LargestPresetBytes = max(assessment.LargestPresetBytes, estimate)
			gpu := false
			for _, x := range lock.Records.Presets {
				gpu = x.Compute == catalog.ComputeGPU
			}
			if gpu {
				if err = add(&gpuTotal, estimate); err != nil {
					return p, err
				}
			}
			if slices.Contains(l.Startup, id) {
				if err = add(&assessment.StartupBytes, estimate); err != nil {
					return p, err
				}
				if gpu {
					if err = add(&startupGPU, estimate); err != nil {
						return p, err
					}
				}
			}
		}
		// These conservative lower bounds never claim measured KV/runtime fit.
		physical := max(int64(0), facts.PhysicalMemoryBytes-budget.OSFloorMiB*MiB)
		wired := max(int64(0), facts.WiredLimitMiB*MiB-budget.OSFloorMiB*MiB)
		assessment.AllFit = total <= physical && gpuTotal <= wired
		if assessment.StartupBytes > physical || startupGPU > wired {
			assessment.Refusals = append(assessment.Refusals, "startup presets exceed the memory allowance (weights and declared engine caps); remove startup selections")
		}
		p.Layouts = append(p.Layouts, assessment)
		for _, reason := range assessment.Refusals {
			p.Refusals = append(p.Refusals, id+": "+reason)
		}
	}
	p.CanPrepare = len(p.Refusals) == 0
	return p, nil
}

func (p Plan) configurationSections() []Section {
	sections := []Section{{Title: "Selected presets", Lines: []string{"Configuration: " + p.Root, "Preset files are shared by all layouts. Saving does not activate or download."}}}
	for _, id := range keys(p.Configuration.Presets) {
		selected := p.Configuration.Presets[id]
		lines := []string{id + " · " + selected.Name}
		for _, x := range selected.Lock.Records.Presets {
			artifact := selected.Lock.Records.Artifacts[x.Artifact]
			lines = append(lines, "Model license: "+artifact.License+" · "+artifact.LicenseURL())
			lines = append(lines, fmt.Sprintf("Compute %s. Context %d tokens (input + output); output allowance %d.", strings.ToUpper(string(x.Compute)), x.Window(), x.RequestDefaults.MaxOutputTokens))
			if x.Presentation.Description != "" {
				lines = append(lines, x.Presentation.Description)
			}
			lines = append(lines, "Context fit is not measured; customization does not inherit a catalog performance claim.")
		}
		if refs := p.Configuration.References(id); len(refs) > 0 {
			lines = append(lines, "Used by: "+strings.Join(refs, ", "))
		}
		for _, m := range p.Presets {
			if catalog.SameExecution(m.Lock, selected.Lock) && m.RuntimeDiskEstimateBytes > 0 {
				lines = append(lines, "Splash first-start weight cache: roughly another "+Size(m.RuntimeDiskEstimateBytes)+", plus 2 GiB free; additional to installation totals. Splash checks exact space before conversion.")
			}
		}
		sections = append(sections, Section{Title: "Preset · " + id, Lines: lines})
	}
	for _, l := range p.Layouts {
		def := l.Layout.Default
		if def == "" {
			def = "none"
		}
		lines := []string{"Included: " + strings.Join(l.Layout.Presets, ", "), "Load on activation: " + strings.Join(l.Layout.Startup, ", "), "Default model: " + def, LayoutMemory(l) + " Counts weights and declared caps; KV cache and runtime overhead are not measured.", fmt.Sprintf("Idle unload after %d seconds; preload does not pin residency.", l.Layout.IdleSeconds)}
		if !l.AllFit {
			lines = append(lines, "Competing on-demand models use exclusive swapping; a request finishes before eviction.")
		}
		sections = append(sections, Section{Title: "Layout · " + l.Layout.Name, Lines: lines})
	}
	for _, m := range p.Presets {
		if m.WiredMemory != nil {
			sections = append(sections, m.WiredMemory.Section())
		}
	}
	var splash int64
	for _, m := range p.Presets {
		splash += m.RuntimeDiskEstimateBytes
	}
	disk := "Disk space still needed: " + DownloadSize(p.RemainingDiskBytes) + " to install"
	if splash > 0 {
		disk += ", plus about " + Size(splash) + " when Splash first starts"
	}
	disk += "; free: " + Size(p.FreeDiskBytes)
	downloads := []string{p.WeightSummary(), "Install downloads missing files and verifies cached weights; saving alone downloads nothing.", disk, "Files shared by several presets are counted once."}
	if p.HFCache != nil {
		downloads = append(downloads, "Hugging Face cache checked: "+p.HFCache.Root)
	}
	sections = append(sections, Section{Title: "Downloads", Lines: downloads, Downloads: p.Downloads})

	if len(p.Refusals) > 0 {
		sections = append(sections, Section{Title: "Installation is not possible yet", Lines: p.Refusals, Warning: true})
	}
	return sections
}

// LayoutMemory says what a layout needs at start and at most for one preset.
func LayoutMemory(l LayoutPlan) string {
	if l.StartupBytes == 0 {
		return "Memory: nothing loads at start; the largest preset needs at least " + Size(l.LargestPresetBytes) + " when used."
	}
	return "Memory: at least " + Size(l.StartupBytes) + " at start; the largest preset needs at least " + Size(l.LargestPresetBytes) + "."
}
