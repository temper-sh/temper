package setup

import (
	"encoding/json"
	"fmt"
	"reflect"
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
			if reflect.DeepEqual(m.Lock, selected.Lock) {
				assessments[id] = struct {
					Name                     string                `json:"name"`
					Compute                  catalog.ComputeDevice `json:"compute"`
					ModelBytes               int64                 `json:"model_bytes"`
					RuntimeDiskEstimateBytes int64                 `json:"runtime_disk_estimate_bytes,omitempty"`
					Memory                   budget.Prediction     `json:"memory_prediction"`
					Estimate                 MemoryEstimate        `json:"memory_estimate"`
					MemoryBytes              int64                 `json:"memory_bytes"`
					WiredMemory              *WiredMemoryAdvice    `json:"wired_memory_advice,omitempty"`
					Refusals                 []string              `json:"refusals,omitempty"`
				}{selected.Name, selected.Lock.Records.Presets[selected.Lock.Preset].Compute, m.ModelBytes, m.RuntimeDiskEstimateBytes, m.Budget, m.Memory, m.MemoryBytes, m.WiredMemory, m.Refusals}
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
	MemoryIncomplete   bool     `json:"memory_incomplete"`
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
		assessment, err := AssessLayout(c, id, facts)
		if err != nil {
			return p, err
		}
		p.Layouts = append(p.Layouts, assessment)
		for _, reason := range assessment.Refusals {
			p.Refusals = append(p.Refusals, id+": "+reason)
		}
	}
	p.CanPrepare = len(p.Refusals) == 0
	return p, nil
}

// AssessLayout computes the same memory allowance used by activation without
// inspecting downloads, installing material, or rebuilding any exact lock.
func AssessLayout(c Configuration, id string, facts machine.Facts) (LayoutPlan, error) {
	if err := c.Validate(); err != nil {
		return LayoutPlan{}, err
	}
	if _, ok := c.Layouts[id]; !ok {
		return LayoutPlan{}, fmt.Errorf("unknown layout %q", id)
	}
	l := c.Layouts[id]
	assessment := LayoutPlan{ID: id, Layout: l}
	var total, gpuTotal, startupGPU int64
	gpuUnknown := false
	for _, id := range l.Presets {
		lock := c.Presets[id].Lock
		one, err := Assess(lock, facts)
		if err != nil {
			return LayoutPlan{}, err
		}
		estimate := one.MemoryBytes
		assessment.MemoryIncomplete = assessment.MemoryIncomplete || len(one.Memory.Unknown) > 0
		if err = add(&total, estimate); err != nil {
			return LayoutPlan{}, err
		}
		assessment.LargestPresetBytes = max(assessment.LargestPresetBytes, estimate)
		gpu := false
		for _, x := range lock.Records.Presets {
			gpu = x.Compute == catalog.ComputeGPU && predictableGPUFootprint(x)
			gpuUnknown = gpuUnknown || x.Compute == catalog.ComputeGPU && !predictableGPUFootprint(x)
		}
		if gpu {
			if err = add(&gpuTotal, estimate-one.Memory.PromptCacheReserveBytes); err != nil {
				return LayoutPlan{}, err
			}
		}
		if slices.Contains(l.Startup, id) {
			if err = add(&assessment.StartupBytes, estimate); err != nil {
				return LayoutPlan{}, err
			}
			if gpu {
				if err = add(&startupGPU, estimate-one.Memory.PromptCacheReserveBytes); err != nil {
					return LayoutPlan{}, err
				}
			}
		}
	}
	// This is a scheduling allowance, not a measured simultaneous-fit claim.
	physical := max(int64(0), facts.PhysicalMemoryBytes-budget.OSFloorMiB*MiB)
	wired := max(int64(0), facts.WiredLimitMiB*MiB-budget.OSFloorMiB*MiB)
	assessment.AllFit = !gpuUnknown && total <= physical && gpuTotal <= wired
	assessment.MemoryIncomplete = assessment.MemoryIncomplete || gpuUnknown
	if assessment.StartupBytes > physical || startupGPU > wired {
		assessment.Refusals = append(assessment.Refusals, "startup presets exceed the memory allowance (measured peaks or fallback estimates); remove startup selections")
	}
	return assessment, nil
}

func (p Plan) configurationSections() []Section {
	sections := []Section{{Title: "Selected presets", Lines: []string{"Configuration: " + p.Root, "Preset files are shared by all layouts. Saving does not activate or download."}}}
	for _, id := range keys(p.Configuration.Presets) {
		selected := p.Configuration.Presets[id]
		lines := []string{id + " · " + selected.Name}
		for _, x := range selected.Lock.Records.Presets {
			artifact := selected.Lock.Records.Artifacts[x.Artifact]
			lines = append(lines, "Model license: "+artifact.License+" · "+artifact.LicenseURL())
			context := fmt.Sprintf("Compute %s. Context %d tokens", strings.ToUpper(string(x.Compute)), x.Window())
			if x.Interface == "chat-completions" {
				context += " (input + output); "
				if x.RequestDefaults.MaxOutputTokens > 0 {
					context += fmt.Sprintf("output default %d", x.RequestDefaults.MaxOutputTokens)
				} else {
					context += "output uses remaining context"
				}
			}
			lines = append(lines, context+".")
			if x.Presentation.Description != "" {
				lines = append(lines, x.Presentation.Description)
			}
			lines = append(lines, "Context capacity beyond the recorded workload is unverified; customization does not inherit a catalog performance claim.")
		}
		if refs := p.Configuration.References(id); len(refs) > 0 {
			lines = append(lines, "Used by: "+strings.Join(refs, ", "))
		}
		for _, m := range p.Presets {
			if reflect.DeepEqual(m.Lock, selected.Lock) {
				lines = append(lines, m.Memory.Lines()...)
				if m.RuntimeDiskEstimateBytes > 0 {
					lines = append(lines, "Splash first-start weight cache: roughly another "+Size(m.RuntimeDiskEstimateBytes)+", plus 2 GiB free; additional to installation totals. Splash checks exact space before conversion.")
				}
				break
			}
		}
		sections = append(sections, Section{Title: "Preset · " + id, Lines: lines})
	}
	for _, l := range p.Layouts {
		def := l.Layout.Default
		if def == "" {
			def = "none"
		}
		lines := []string{"Included: " + strings.Join(l.Layout.Presets, ", "), "Load on activation: " + strings.Join(l.Layout.Startup, ", "), "Default model: " + def, LayoutMemory(l), fmt.Sprintf("Idle unload after %d seconds; preload does not pin residency.", l.Layout.IdleSeconds)}
		if l.MemoryIncomplete {
			lines = append(lines, "Total runtime fit remains unknown; see each preset's measurement limits or estimated and unknown components.")
		}
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
	harnesses := []string{"None selected. Harness files will not be changed."}
	if len(p.Configuration.Integrations) > 0 {
		harnesses = []string{"Layout activation synchronizes the included chat models. Saving alone does not change these files."}
		for _, integration := range p.Configuration.Integrations {
			harnesses = append(harnesses, integration.ID+" · "+integration.ModelsFile)
		}
	}
	sections = append(sections, Section{Title: "Harnesses", Lines: harnesses})
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
		return "Memory allowance: nothing loads at start; the largest preset accounts for " + Size(l.LargestPresetBytes) + " when used."
	}
	return "Memory allowance: " + Size(l.StartupBytes) + " accounted at start; the largest preset accounts for " + Size(l.LargestPresetBytes) + "."
}
