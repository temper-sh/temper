package catalog

import (
	"fmt"
	"slices"
)

// ResolveSelection freezes template and context defaults for each selected
// layout. An empty template explicitly selects the model's embedded template.
// It never changes the caller's selection or catalog.
func ResolveSelection(d Document, s Selection) (Selection, error) {
	if err := d.Validate(); err != nil {
		return Selection{}, err
	}
	if err := s.Validate(); err != nil {
		return Selection{}, err
	}
	if (d.Schema == Schema) != (s.Schema == SelectionSchema) {
		return Selection{}, fmt.Errorf("catalog and selection schema versions must match")
	}
	profile, ok := d.Profiles[s.Profile]
	if !ok {
		return Selection{}, fmt.Errorf("unknown selected profile %q", s.Profile)
	}
	if s.Schema == legacySelectionSchema {
		return s, nil
	}
	selected := make(map[string]bool, len(profile.Bindings))
	for _, binding := range profile.Bindings {
		selected[binding.Layout] = true
	}
	for layoutID, patchID := range s.Templates {
		if !selected[layoutID] {
			return Selection{}, fmt.Errorf("template choice names unselected or unknown layout %q", layoutID)
		}
		if patchID == "" {
			continue
		}
		layout := d.Layouts[layoutID]
		patch, ok := d.Patches[patchID]
		if !ok || !slices.Contains(patch.CompatibleArtifacts, layout.Artifact) {
			return Selection{}, fmt.Errorf("layout %q template patch %q is absent or incompatible", layoutID, patchID)
		}
	}
	for layoutID, tokens := range s.ContextWindows {
		if !selected[layoutID] {
			return Selection{}, fmt.Errorf("context choice names unselected or unknown layout %q", layoutID)
		}
		layout := d.Layouts[layoutID]
		if tokens <= layout.RequestDefaults.MaxOutputTokens || tokens > layout.ContextLimit() {
			return Selection{}, fmt.Errorf("layout %q context must be between %d and %d tokens (total input and output)", layoutID, layout.RequestDefaults.MaxOutputTokens+1, layout.ContextLimit())
		}
	}
	resolved := s
	resolved.Templates = make(map[string]string, len(profile.Bindings))
	resolved.ContextWindows = make(map[string]int, len(profile.Bindings))
	for _, binding := range profile.Bindings {
		choice, ok := s.Templates[binding.Layout]
		if !ok {
			patches := d.Layouts[binding.Layout].Patches
			if len(patches) != 0 {
				choice = patches[0]
			}
		}
		resolved.Templates[binding.Layout] = choice
		window, ok := s.ContextWindows[binding.Layout]
		if !ok {
			window = d.Layouts[binding.Layout].ContextWindowTokens
		}
		resolved.ContextWindows[binding.Layout] = window
	}
	return resolved, nil
}
