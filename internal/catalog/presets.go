package catalog

import (
	"errors"
	"fmt"
	"strings"
)

func DescribePreset(d Document, id, description string, assessmentURL *string, ifEmpty bool) (Document, error) {
	if err := d.Validate(); err != nil {
		return Document{}, err
	}
	p, ok := d.Presets[id]
	if !ok {
		return Document{}, fmt.Errorf("unknown preset %q", id)
	}
	if ifEmpty && strings.TrimSpace(p.Description) != "" {
		return d, nil
	}
	p.Description = description
	if assessmentURL != nil {
		p.AssessmentURL = *assessmentURL
	}
	if p.Recommended && strings.TrimSpace(description) == "" {
		return Document{}, errors.New("recommended presets require a manually authored description")
	}
	d = canonicalDocument(d)
	d.Presets[id] = p
	return d, d.Validate()
}
