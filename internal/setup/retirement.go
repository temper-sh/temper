package setup

import (
	"fmt"
	"slices"

	"github.com/temper-sh/temper/internal/catalog"
)

// RetirementChange is explicit consent to remove one saved preset, optionally
// replacing every layout reference with the named selected successor.
type RetirementChange struct {
	Preset    string `json:"preset"`
	Successor string `json:"successor,omitempty"`
}

// ChangeRetired edits a private configuration copy. The successor uses its
// reviewed defaults, or a selection that already exists; it never overwrites it.
func ChangeRetired(current Configuration, id, successor string, replacement Preset) (Configuration, error) {
	if _, ok := current.Presets[id]; !ok {
		return Configuration{}, fmt.Errorf("saved preset %q does not exist", id)
	}
	if id == successor {
		return Configuration{}, fmt.Errorf("a replacement must use a different saved ID")
	}
	raw, err := current.Bytes()
	if err != nil {
		return Configuration{}, err
	}
	next, err := ParseConfiguration(raw)
	if err != nil {
		return Configuration{}, err
	}
	if successor != "" {
		if _, selected := next.Presets[successor]; !selected {
			next.Presets[successor] = replacement
		}
	}
	delete(next.Presets, id)
	if successor != "" {
		// A root uses one router. Adopt only the successor's shared router;
		// unrelated models, engine releases and settings retain their locks.
		router := next.Presets[successor].Lock.Records.Runtime.Router
		for key, p := range next.Presets {
			d := p.Lock.Records
			d.Runtime.Router = router
			lock, err := catalog.CompilePreset(d, p.Lock.Preset, 0, 0, p.Lock.Target)
			if err != nil {
				return Configuration{}, err
			}
			p.Lock = lock
			next.Presets[key] = p
		}
	}
	replace := func(ids []string) []string {
		out := []string{}
		for _, item := range ids {
			if item == id {
				item = successor
			}
			if item != "" && !slices.Contains(out, item) {
				out = append(out, item)
			}
		}
		return out
	}
	for key, layout := range next.Layouts {
		layout.Presets, layout.Startup = replace(layout.Presets), replace(layout.Startup)
		if layout.Default == id {
			layout.Default = successor
		}
		next.Layouts[key] = layout
	}
	return next, next.Validate()
}

// RetirementOrigin derives only explicit provenance, never similarity of bytes.
func RetirementOrigin(saved Preset) string {
	if saved.UserPreset == "" {
		return saved.Lock.Preset
	}
	if saved.ForkBaseline != nil {
		return saved.ForkBaseline.Origin
	}
	return ""
}

// RetirementPreset constructs an ordinary saved selection from a compiled
// successor. Authored overrides and files stay with the old user preset.
func RetirementPreset(lock catalog.Lock, name string) Preset { return Preset{Name: name, Lock: lock} }
