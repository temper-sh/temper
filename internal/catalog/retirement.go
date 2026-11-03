package catalog

import (
	"fmt"
	"strings"
	"time"
)

// Retirement explains withdrawal of a permanent catalog ID. It owns no runtime
// inputs; the saved lock and historical publication retain those.
type Retirement struct {
	RetiredOn string `yaml:"retired_on" json:"retired_on"`
	Reason    string `yaml:"reason" json:"reason"`
	Successor string `yaml:"successor,omitempty" json:"successor,omitempty"`
}

func (d Document) validateRetirement(id string, active func(string) bool) error {
	r := d.RetiredPresets[id]
	if !ValidPresetID(id) || active(id) {
		return fmt.Errorf("requires a valid retired ID absent from active presets")
	}
	if _, err := time.Parse(time.DateOnly, r.RetiredOn); err != nil || r.RetiredOn > d.Date {
		return fmt.Errorf("retired_on must be YYYY-MM-DD, no later than catalog date")
	}
	if strings.TrimSpace(r.Reason) == "" || strings.ContainsAny(r.Reason, "\x00\r\n\x1b") {
		return fmt.Errorf("reason must be nonempty single-line text")
	}
	seen := map[string]bool{id: true}
	for next := r.Successor; next != ""; {
		if !ValidPresetID(next) || seen[next] {
			return fmt.Errorf("successor contains an invalid ID or cycle")
		}
		seen[next] = true
		if active(next) {
			return nil
		}
		r, ok := d.RetiredPresets[next]
		if !ok {
			return fmt.Errorf("successor %q is neither active nor retired", next)
		}
		next = r.Successor
	}
	return nil
}

// Successor follows explicit recommendations to a usable active preset. An
// unavailable target or a retirement without a successor remains explainable.
func (d Document) Successor(id string) (string, error) {
	seen := map[string]bool{}
	for {
		if seen[id] {
			return "", fmt.Errorf("retirement successor cycle at %q", id)
		}
		seen[id] = true
		r, ok := d.RetiredPresets[id]
		if !ok {
			return "", fmt.Errorf("preset %q has no retirement record", id)
		}
		if r.Successor == "" {
			return "", fmt.Errorf("no recommended successor")
		}
		if _, ok := d.Presets[r.Successor]; ok {
			return r.Successor, nil
		}
		if _, ok := d.RetiredPresets[r.Successor]; !ok {
			return "", fmt.Errorf("recommended successor %q is unavailable on this Temper", r.Successor)
		}
		id = r.Successor
	}
}

// CheckRetirementHistory keeps the date of every retained notice and dates a
// new withdrawal of an offered preset at publication. Notices accumulate;
// removing one, including to offer its ID again, is a reviewed source edit.
func (d Document) CheckRetirementHistory(previous Document) error {
	for id, old := range previous.RetiredPresets {
		if r, ok := d.RetiredPresets[id]; ok && r.RetiredOn != old.RetiredOn {
			return fmt.Errorf("retirement %q must retain original retired_on %s", id, old.RetiredOn)
		}
	}
	for id, r := range d.RetiredPresets {
		_, wasOffered := previous.Presets[id]
		if _, exists := previous.RetiredPresets[id]; !exists && wasOffered && r.RetiredOn != d.Date {
			return fmt.Errorf("new retirement %q must use publication date %s", id, d.Date)
		}
	}
	return nil
}
