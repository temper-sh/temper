package catalogcmd

import (
	"bytes"
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// descriptionSource edits validated authoring, not the normalized execution
// document. Untouched fields keep their values and presence, including nulls.
func descriptionSource(raw []byte, id, description string, assessmentURL *string) ([]byte, error) {
	var document map[string]any
	isJSON := json.Valid(raw)
	if isJSON {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&document); err != nil {
			return nil, err
		}
	} else {
		var source yaml.Node
		if err := yaml.Unmarshal(raw, &source); err != nil {
			return nil, err
		}
		// Catalog dates are strings. Generic YAML decoding otherwise turns an
		// unquoted date into time.Time and writes back a different scalar value.
		preserveDateText(&source)
		if err := source.Decode(&document); err != nil {
			return nil, err
		}
	}
	presentation := document
	for _, key := range []string{"presets", id, "presentation"} {
		next, ok := presentation[key].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("description edit requires a mapping at %q", key)
		}
		presentation = next
	}
	presentation["description"] = description
	if assessmentURL != nil {
		presentation["assessment_url"] = *assessmentURL
	}
	if isJSON {
		data, err := json.MarshalIndent(document, "", "  ")
		return append(data, '\n'), err
	}
	return yaml.Marshal(document)
}

func preserveDateText(node *yaml.Node) {
	if node.Tag == "!!timestamp" {
		node.Tag = "!!str"
	}
	for _, child := range node.Content {
		preserveDateText(child)
	}
}
