package catalog

import (
	"errors"
)

// ContextLimit is the model ceiling, or the selected window when no ceiling
// is known.
func (l Preset) ContextLimit() int {
	if l.ContextLimitTokens != 0 {
		return l.ContextLimitTokens
	}
	return l.Window()
}

// ContextConfigurable distinguishes launch-time windows from an encoder's
// fixed artifact-owned sequence limit.
func (l Preset) ContextConfigurable() bool { return l.Config().Kind != "crispembed/v1" }

func (l Preset) validateContext() error {
	for _, profile := range l.Profiles {
		if !l.ContextConfigurable() && (l.ContextLimitTokens <= 0 || profile.ContextWindowTokens != l.ContextLimitTokens) {
			return errors.New("CrispEmbed uses the model's fixed sequence limit; context cannot be overridden")
		}
		if l.ContextLimitTokens < 0 || l.ContextLimitTokens != 0 && l.ContextLimitTokens < profile.ContextWindowTokens {
			return errors.New("context limit must be at least every profile's context window")
		}
	}
	return nil
}
