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
	delete(result, "modes")
	delete(result, "default_profile")
	assessments := map[string]any{}
	for id, selected := range p.Configuration.Presets {
		for _, m := range p.Modes {
			if m.Lock.Digests.Profile == selected.Lock.Digests.Profile {
				assessments[id] = struct {
					Name                     string                       `json:"name"`
					ModelBytes               int64                        `json:"model_bytes"`
					RuntimeDiskEstimateBytes int64                        `json:"runtime_disk_estimate_bytes,omitempty"`
					Memory                   budget.Prediction            `json:"memory_prediction"`
					WiredMemory              *WiredMemoryAdvice           `json:"wired_memory_advice,omitempty"`
					Contexts                 map[string]ContextAssessment `json:"contexts"`
					Refusals                 []string                     `json:"refusals,omitempty"`
				}{selected.Name, m.ModelBytes, m.RuntimeDiskEstimateBytes, m.Budget, m.WiredMemory, m.Contexts, m.Refusals}
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
	p, err := build(root, facts, free, locks, material, "", true)
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
			estimate := one.ModelBytes
			if one.Budget.Status == budget.StatusFits || one.Budget.Status == budget.StatusExceeded {
				estimate = max(estimate, (one.Budget.RequiredMiB-budget.OSFloorMiB)*MiB)
			}
			for _, x := range lock.Records.Layouts {
				if x.EngineConfig.Splash != nil {
					estimate = max(estimate, x.EngineConfig.Splash.MaxMemoryBytes)
				}
			}
			for _, context := range one.Contexts {
				if context.Finding != nil {
					estimate = max(estimate, context.Finding.EngineMemoryLimitBytes)
				}
			}
			if err = add(&total, estimate); err != nil {
				return p, err
			}
			assessment.LargestPresetBytes = max(assessment.LargestPresetBytes, estimate)
			gpu := false
			for _, x := range lock.Records.Layouts {
				gpu = x.EngineConfig.Splash != nil || x.EngineConfig.GPULayers > 0
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
		for _, x := range selected.Lock.Records.Layouts {
			lines = append(lines, fmt.Sprintf("Context %d tokens (input + output); output allowance %d.", x.ContextWindowTokens, x.RequestDefaults.MaxOutputTokens))
			if x.Description != "" {
				lines = append(lines, x.Description)
			}
			lines = append(lines, "Context fit is unknown unless exact applicable evidence exists; customization does not inherit a catalog performance claim.")
		}
		if refs := p.Configuration.References(id); len(refs) > 0 {
			lines = append(lines, "Used by: "+strings.Join(refs, ", "))
		}
		for _, m := range p.Modes {
			if m.Lock.Digests.Profile == selected.Lock.Digests.Profile && m.RuntimeDiskEstimateBytes > 0 {
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
		lines := []string{"Included: " + strings.Join(l.Layout.Presets, ", "), "Load on activation: " + strings.Join(l.Layout.Startup, ", "), "Default model: " + def, fmt.Sprintf("Startup memory allowance: %s; largest on-demand preset: %s. Uses weights and declared caps; unmeasured KV and runtime overhead remain unknown.", Size(l.StartupBytes), Size(l.LargestPresetBytes)), fmt.Sprintf("Idle unload after %d seconds; preload does not pin residency.", l.Layout.IdleSeconds)}
		if !l.AllFit {
			lines = append(lines, "Competing on-demand models use exclusive swapping; a request finishes before eviction.")
		}
		sections = append(sections, Section{Title: "Layout · " + l.Layout.Name, Lines: lines})
	}
	for _, m := range p.Modes {
		if m.WiredMemory != nil {
			sections = append(sections, m.WiredMemory.Section())
		}
	}
	sections = append(sections, Section{Title: "Downloads", Lines: []string{p.WeightSummary(), "Remaining installation allowance: " + DownloadSize(p.RemainingDiskBytes) + "; free: " + Size(p.FreeDiskBytes), "Software closures and weights count once across selected presets."}, Downloads: p.Downloads})
	if len(p.Refusals) > 0 {
		sections = append(sections, Section{Title: "Preparation unavailable", Lines: p.Refusals, Warning: true})
	}
	return sections
}
