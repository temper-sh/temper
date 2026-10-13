package catalog

import (
	"errors"
	"fmt"
	"strings"
)

// DescribePreset replaces one preset's authored description and optional
// assessment link. It is the publisher's editing tool: the result is strict.
func DescribePreset(d Document, id, description string, assessmentURL *string, ifEmpty bool) (Document, error) {
	if err := d.Validate(); err != nil {
		return Document{}, err
	}
	p, ok := d.Presets[id]
	if !ok {
		return Document{}, fmt.Errorf("unknown preset %q", id)
	}
	if ifEmpty && strings.TrimSpace(p.Presentation.Description) != "" {
		return d, nil
	}
	if strings.TrimSpace(description) == "" {
		return Document{}, errors.New("every preset requires a manually authored description")
	}
	p.Presentation.Description = description
	if assessmentURL != nil {
		p.Presentation.AssessmentURL = *assessmentURL
	}
	d = canonicalDocument(d)
	d.Presets[id] = p
	return d, d.Validate()
}
