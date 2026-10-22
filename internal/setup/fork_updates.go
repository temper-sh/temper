package setup

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"github.com/temper-sh/temper/internal/catalog"
)

// ForkBaseline retains the origin's values for explicitly overridden fields
// at the last full save. An absent map key means untracked; JSON null means
// the catalog field was unset. It never supplies execution settings.
type ForkBaseline struct {
	Origin string                     `json:"origin"`
	Values map[string]json.RawMessage `json:"values"`
}

// CaptureForkBaseline snapshots the catalog values hidden by a fork's
// overrides, before applying either authored changes or configuration choices.
// Copies and unavailable origins have no baseline.
func CaptureForkBaseline(file catalog.UserPreset, d catalog.Document, memoryGiB int) *ForkBaseline {
	p, ok := d.Presets[file.Origin]
	if !file.Fork() || !ok {
		return nil
	}
	p.Profiles = []catalog.Profile{p.SelectProfile(memoryGiB)}
	values := catalog.Values(p)
	baseline := &ForkBaseline{Origin: file.Origin, Values: map[string]json.RawMessage{}}
	for _, field := range catalog.Fields(p.Config().Kind) {
		if _, overridden := forkOverride(file, field.Path); overridden {
			baseline.Values[field.Path] = jsonValue(values[field.Path])
		}
	}
	return baseline
}

// OverrideChange is an upstream change masked by an authored override.
// Override describes the file, even if its rebuild is later refused.
type OverrideChange struct {
	Path     string          `json:"path"`
	From     json.RawMessage `json:"from"`
	To       json.RawMessage `json:"to"`
	Override json.RawMessage `json:"override"`
}

// CompareForkOverrides reports changed defaults and paths whose former value
// is unknown. Only still-overridden fields from the same origin are comparable.
// Neither input is changed; callers advance the baseline at the full-save commit.
func CompareForkOverrides(previous, next *ForkBaseline, file catalog.UserPreset) (changes []OverrideChange, untracked []string) {
	if next == nil {
		return nil, nil
	}
	for _, path := range slices.Sorted(maps.Keys(next.Values)) {
		var before json.RawMessage
		if previous != nil && previous.Origin == next.Origin {
			before = previous.Values[path]
		}
		if len(before) == 0 {
			untracked = append(untracked, path)
			continue
		}
		after := next.Values[path]
		if !bytes.Equal(before, after) {
			override, _ := forkOverride(file, path)
			changes = append(changes, OverrideChange{Path: path, From: before, To: after, Override: jsonValue(override)})
		}
	}
	return changes, untracked
}

func forkOverride(file catalog.UserPreset, path string) (any, bool) {
	var value any = file.Changes
	for _, part := range strings.Split(path, ".") {
		group, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok = group[part]
		if !ok {
			return nil, false
		}
	}
	return value, true
}

// Values come from typed catalog fields or a parsed preset's scalar overrides.
func jsonValue(value any) json.RawMessage {
	raw, _ := json.Marshal(value)
	return raw
}
