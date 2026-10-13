package render

import "github.com/temper-sh/temper/internal/render/engine"

// Commands reuses the exact launch builders for composed managed operation.
// Artifact paths and preset controls have the same owner as ordinary rendering.
func Commands(inputs Inputs) (map[string]engine.Command, error) {
	if err := inputs.Manifest.Validate(); err != nil {
		return nil, err
	}
	mode, err := inputs.Manifest.Mode(inputs.Mode)
	if err != nil {
		return nil, err
	}
	members, err := resolveMembers(inputs.Manifest, inputs.Lock, mode, inputs.Root)
	if err != nil {
		return nil, err
	}
	result := map[string]engine.Command{}
	for _, member := range members {
		c, err := engine.Build(engineRequest(member))
		if err != nil {
			return nil, err
		}
		result[member.ID] = c
	}
	return result, nil
}
