package harness

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// mergeHermes edits YAML nodes so unrelated comments, scalar types, ordering
// and aliases survive. The comment is ownership metadata, not a Hermes option.
func mergeHermes(raw []byte, root, base string, models []Model) ([]byte, error) {
	doc, err := hermesDocument(raw)
	if err != nil {
		return nil, err
	}
	before, err := yamlBytes(doc)
	if err != nil {
		return nil, err
	}
	config := doc.Content[0]
	if err := plainYAMLMapping(config); err != nil {
		return nil, fmt.Errorf("Hermes configuration: %w", err)
	}
	if _, legacy := yamlEntry(config, "custom_providers"); legacy != nil {
		var entries []struct {
			Name string `yaml:"name"`
		}
		if err := legacy.Decode(&entries); err != nil {
			return nil, fmt.Errorf("custom_providers must be a list: %w", err)
		}
		for _, entry := range entries {
			if strings.EqualFold(strings.TrimSpace(entry.Name), providerID) {
				return nil, errors.New("legacy custom_providers already contains temper; resolve the name collision explicitly")
			}
		}
	}
	providers, err := yamlMapping(config, "providers")
	if err != nil {
		return nil, err
	}
	for i := 0; i < len(providers.Content); i += 2 {
		key := providers.Content[i].Value
		if key != providerID && strings.EqualFold(strings.TrimSpace(key), providerID) {
			return nil, fmt.Errorf("provider %q conflicts with temper; resolve the name collision explicitly", key)
		}
	}
	owner := "# temper-root: " + strconv.Quote(filepath.Clean(root))
	key, provider := yamlEntry(providers, providerID)
	if provider != nil {
		// This is a narrow marker attached to this entry, never a document-wide
		// search that could accidentally adopt another provider's comment.
		if key.LineComment != owner && provider.LineComment != owner {
			return nil, errors.New("provider temper is not owned by this Temper root; resolve the name collision explicitly")
		}
		if err := isolatedYAML(provider); err != nil {
			return nil, fmt.Errorf("provider temper: %w", err)
		}
	}
	provider, err = yamlMapping(providers, providerID)
	if err != nil {
		return nil, err
	}
	key, _ = yamlEntry(providers, providerID)
	key.LineComment = owner
	if provider.LineComment == owner {
		provider.LineComment = ""
	}
	// Inline key comments in flow mappings do not round-trip reliably through
	// yaml.v3. Use block mappings along the managed path, preserving all values.
	config.Style &^= yaml.FlowStyle
	providers.Style &^= yaml.FlowStyle
	provider.Style &^= yaml.FlowStyle
	yamlSet(provider, "name", "temper")
	yamlSet(provider, "api", base)
	yamlSet(provider, "transport", "chat_completions")
	yamlSet(provider, "discover_models", true)
	// Hermes accepts several spellings, some with precedence over these
	// canonical fields. Old spellings must not keep an obsolete listener/API.
	yamlRemove(provider, "base_url", "url", "baseUrl", "api_mode", "apiMode")
	previous, err := yamlMapping(provider, "models")
	if err != nil {
		return nil, err
	}
	// Reuse nodes for surviving models, including user fields and comments.
	var content []*yaml.Node
	for _, model := range models {
		key, metadata := yamlEntry(previous, model.ID)
		if metadata == nil {
			key, metadata = yamlString(model.ID), newYAMLMapping()
		}
		if err := plainYAMLMapping(metadata); err != nil {
			return nil, fmt.Errorf("model %s: %w", model.ID, err)
		}
		yamlSet(metadata, "context_length", model.ContextWindow)
		yamlSet(metadata, "supports_vision", slices.Contains(model.Input, "image"))
		content = append(content, key, metadata)
	}
	previous.Content = content
	after, err := yamlBytes(doc)
	if err != nil {
		return nil, err
	}
	if bytes.Equal(before, after) {
		return raw, nil
	}
	if _, err := hermesDocument(after); err != nil {
		return nil, fmt.Errorf("rendered Hermes configuration: %w", err)
	}
	return after, nil
}

func hermesDocument(raw []byte) (*yaml.Node, error) {
	var doc yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&doc); err == io.EOF {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{newYAMLMapping()}, HeadComment: strings.TrimSpace(string(raw))}
	} else if err != nil {
		return nil, err
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("expected exactly one YAML document")
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("expected a YAML mapping")
	}
	if err := uniqueYAML(&doc, 0); err != nil {
		return nil, err
	}
	return &doc, nil
}

// Do not decode unrelated values into Go maps: that would flatten aliases,
// discard comments and risk changing scalar representations.
func uniqueYAML(node *yaml.Node, depth int) error {
	if depth > 64 {
		return errors.New("YAML configuration is too deeply nested")
	}
	if node.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" && key.Tag != "!!merge" {
				return errors.New("configuration mappings require string keys")
			}
			if seen[key.Value] {
				return fmt.Errorf("duplicate YAML key %q", key.Value)
			}
			seen[key.Value] = true
		}
	}
	for _, child := range node.Content {
		if err := uniqueYAML(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func plainYAMLMapping(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode || node.Tag != "!!map" {
		return errors.New("expected a YAML mapping")
	}
	if node.Anchor != "" {
		return errors.New("managed mappings cannot have YAML anchors")
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Tag == "!!merge" {
			return errors.New("managed mappings cannot have YAML merge keys")
		}
	}
	return nil
}

// We own the entire model list. Refusing shared nodes inside our provider
// avoids changing aliases elsewhere, or deleting the anchor they reference.
func isolatedYAML(node *yaml.Node) error {
	if node.Anchor != "" || node.Kind == yaml.AliasNode || node.Tag == "!!merge" {
		return errors.New("managed provider cannot contain YAML anchors, aliases or merge keys")
	}
	for _, child := range node.Content {
		if err := isolatedYAML(child); err != nil {
			return err
		}
	}
	return nil
}

func newYAMLMapping() *yaml.Node { return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"} }

func yamlString(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func yamlEntry(mapping *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	for i := 0; i < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i], mapping.Content[i+1]
		}
	}
	return nil, nil
}

func yamlMapping(parent *yaml.Node, key string) (*yaml.Node, error) {
	_, value := yamlEntry(parent, key)
	if value == nil {
		value = newYAMLMapping()
		parent.Content = append(parent.Content, yamlString(key), value)
	}
	if err := plainYAMLMapping(value); err != nil {
		return nil, fmt.Errorf("%s: %w", key, err)
	}
	return value, nil
}

func yamlSet(mapping *yaml.Node, key string, value any) {
	var next yaml.Node
	// Callers supply only strings, integers and booleans.
	_ = next.Encode(value)
	if _, old := yamlEntry(mapping, key); old != nil {
		if old.Kind == next.Kind && old.Tag == next.Tag && old.Value == next.Value {
			return
		}
		next.HeadComment, next.LineComment, next.FootComment = old.HeadComment, old.LineComment, old.FootComment
		*old = next
	} else {
		mapping.Content = append(mapping.Content, yamlString(key), &next)
	}
}

func yamlRemove(mapping *yaml.Node, keys ...string) {
	for i := len(mapping.Content) - 2; i >= 0; i -= 2 {
		if slices.Contains(keys, mapping.Content[i].Value) {
			mapping.Content = slices.Delete(mapping.Content, i, i+2)
		}
	}
}

func yamlBytes(doc *yaml.Node) ([]byte, error) {
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
