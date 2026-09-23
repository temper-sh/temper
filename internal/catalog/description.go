package catalog

import (
	"errors"
	"fmt"
	"net/url"
	"unicode"
	"unicode/utf8"
)

func plainText(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' {
			return false
		}
	}
	return true
}

func webURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil && plainText(value)
}

func (a Artifact) validateDescription() error {
	if !plainText(a.Description) || a.AssessmentURL != "" && !webURL(a.AssessmentURL) {
		return errors.New("description must be plain text and assessment_url an HTTP(S) URL")
	}
	return nil
}

// Describe changes only editorial metadata in a copy. Suggestions preserve an
// existing description; deliberate edits replace it. A nil URL preserves the
// old link, while an explicit empty URL removes it.
func Describe(d Document, artifactID, description string, assessmentURL *string, ifEmpty bool) (Document, error) {
	if err := d.Validate(); err != nil {
		return Document{}, err
	}
	if d.Schema != Schema {
		return Document{}, errors.New("descriptions require a v2 authoring catalog")
	}
	a, ok := d.Artifacts[artifactID]
	if !ok {
		return Document{}, fmt.Errorf("unknown artifact %q", artifactID)
	}
	if ifEmpty && a.Description != "" {
		return d, nil
	}
	a.Description = description
	if assessmentURL != nil {
		a.AssessmentURL = *assessmentURL
	}
	if err := a.validateDescription(); err != nil {
		return Document{}, err
	}
	d = canonicalDocument(d)
	d.Artifacts[artifactID] = a
	return d, nil
}
