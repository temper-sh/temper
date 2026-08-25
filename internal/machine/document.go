package machine

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// ParseFacts accepts only the canonical YAML bytes produced by MarshalFacts.
func ParseFacts(data []byte) (Facts, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var facts Facts
	if err := decoder.Decode(&facts); err != nil {
		return Facts{}, fmt.Errorf("decode machine facts: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Facts{}, errors.New("decode machine facts: multiple YAML documents are not allowed")
		}
		return Facts{}, fmt.Errorf("decode machine facts: %w", err)
	}
	canonical, err := MarshalFacts(facts)
	if err != nil {
		return Facts{}, err
	}
	if !bytes.Equal(data, canonical) {
		return Facts{}, errors.New("machine facts bytes are not canonical")
	}
	return facts, nil
}

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
