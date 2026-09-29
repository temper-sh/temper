package catalog

import "errors"

// ExecutionSettings are explicit per-run limits. Software, weights, templates,
// sampling and speculation remain exactly as recorded in the source lock.
type ExecutionSettings struct {
	ContextWindowTokens int   `json:"context_window_tokens"`
	MaxOutputTokens     int   `json:"max_output_tokens"`
	MaxMemoryBytes      int64 `json:"max_memory_bytes,omitempty"`
}

func ConfigureExecution(base Lock, id string, settings ExecutionSettings) (Lock, error) {
	if err := base.Validate(); err != nil {
		return Lock{}, err
	}
	if len(base.Records.Presets) != 1 {
		return Lock{}, errors.New("execution configuration requires one preset")
	}
	p, ok := base.Records.Presets[id]
	if !ok {
		return Lock{}, errors.New("preset does not match the source execution")
	}
	if settings.MaxOutputTokens <= 0 || settings.ContextWindowTokens <= settings.MaxOutputTokens || settings.ContextWindowTokens > p.ContextLimit() || settings.MaxMemoryBytes < 0 {
		return Lock{}, errors.New("execution settings are outside the preset's bounds")
	}
	// Canonicalization clones nested records: modifying a derived execution must
	// not alter the caller's retained source.
	d := canonicalDocument(base.Records)
	p = d.Presets[id]
	p.ContextWindowTokens = settings.ContextWindowTokens
	p.RequestDefaults.MaxOutputTokens = settings.MaxOutputTokens
	if settings.MaxMemoryBytes != 0 {
		if p.EngineConfig.Splash == nil {
			return Lock{}, errors.New("max-memory is supported only by Splash")
		}
		p.EngineConfig.Splash.MaxMemoryBytes = settings.MaxMemoryBytes
	}
	d.Presets[id] = p
	result, err := CompilePreset(d, id, "", 0, base.Target)
	if err != nil {
		return Lock{}, err
	}
	result.SourceSnapshotSHA256 = base.SourceSnapshotSHA256
	return result, nil
}
