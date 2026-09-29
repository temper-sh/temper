package catalog

import (
	"fmt"
	"slices"
	"strings"
)

// MemoryTier describes expected machine capacity, independently of model file
// size and of the machine checks for a selected execution configuration.
type MemoryTier string

var memoryTiers = []MemoryTier{"XS", "S", "M", "L", "XL", "XXL"}

func (t MemoryTier) Rank() int { return slices.Index(memoryTiers, t) }

func (t MemoryTier) Label() string {
	switch t {
	case "XS":
		return "XS · up to 16 GB"
	case "S":
		return "S · >16–32 GB"
	case "M":
		return "M · >32–64 GB"
	case "L":
		return "L · >64–128 GB"
	case "XL":
		return "XL · >128–256 GB"
	case "XXL":
		return "XXL · >256–512 GB"
	default:
		return "Memory tier unspecified"
	}
}

func (d Document) validatePresentation() error {
	seen := make(map[string]bool, len(d.PresetOrder))
	for _, id := range d.PresetOrder {
		if _, ok := d.Presets[id]; !ok || seen[id] {
			return fmt.Errorf("preset_order names an unknown or repeated layout %q", id)
		}
		seen[id] = true
	}
	for id, a := range d.Artifacts {
		if !plainText(a.ModelName) || !plainText(a.WeightsName) {
			return fmt.Errorf("artifact %q model_name and weights_name must be plain text", id)
		}
	}
	for id, e := range d.Engines {
		if !plainText(e.DisplayName) {
			return fmt.Errorf("engine %q display_name must be plain text", id)
		}
	}
	for id, l := range d.Presets {
		if !plainText(l.Description) || l.AssessmentURL != "" && !webURL(l.AssessmentURL) {
			return fmt.Errorf("preset %q description must be plain text and assessment_url an HTTP(S) URL", id)
		}
		if l.Recommended && strings.TrimSpace(l.Description) == "" {
			return fmt.Errorf("recommended preset %q requires a manually authored description", id)
		}
		if l.MemoryTier != "" && MemoryTier(l.MemoryTier).Rank() < 0 {
			return fmt.Errorf("layout %q has an unknown memory_tier %q", id, l.MemoryTier)
		}
	}
	return nil
}
