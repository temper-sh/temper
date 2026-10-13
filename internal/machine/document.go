package machine

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// MarshalFacts validates and encodes one canonical machine-facts document.
func MarshalFacts(facts Facts) ([]byte, error) {
	if err := facts.Validate(); err != nil {
		return nil, err
	}
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(facts); err != nil {
		return nil, fmt.Errorf("encode machine facts: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("close machine facts encoder: %w", err)
	}
	return output.Bytes(), nil
}
