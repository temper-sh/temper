// Package qualification validates the immutable documents that make up the
// qualification catalog. It is a pure boundary: callers supply bytes or
// already-read machine facts, and the package performs no reads or effects.
package qualification

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/software"
	"gopkg.in/yaml.v3"
)

const MachineBucketSchemaV1 = "temper-qualification-machine-bucket/v1"

var (
	stableIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	sha256Pattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// MachineBucket is immutable matching vocabulary. A match means only that
// canonical machine facts satisfy its predicate; it is not qualification
// evidence for any profile.
type MachineBucket struct {
	Schema               string                  `yaml:"schema"`
	ID                   string                  `yaml:"id"`
	Revision             uint64                  `yaml:"revision"`
	Title                string                  `yaml:"title"`
	FactsSchema          string                  `yaml:"facts_schema"`
	Predicate            MachineBucketPredicate  `yaml:"predicate"`
	AxisLabels           MachineBucketAxisLabels `yaml:"axis_labels"`
	Evidence             []MachineBucketEvidence `yaml:"evidence"`
	InvalidationTriggers []string                `yaml:"invalidation_triggers"`
}

type MachineBucketPredicate struct {
	Target              software.Target    `yaml:"target"`
	HardwareModels      []string           `yaml:"hardware_models"`
	Chips               []string           `yaml:"chips"`
	PhysicalMemoryBytes InclusiveByteRange `yaml:"physical_memory_bytes"`
}

type InclusiveByteRange struct {
	Minimum int64 `yaml:"minimum"`
	Maximum int64 `yaml:"maximum"`
}

type MachineBucketAxisLabels struct {
	Memory          string `yaml:"memory"`
	ChipGeneration  string `yaml:"chip_generation"`
	MemoryBandwidth string `yaml:"memory_bandwidth"`
}

type MachineBucketEvidence struct {
	Kind     string `yaml:"kind"`
	ID       string `yaml:"id"`
	Revision uint64 `yaml:"revision"`
	SHA256   string `yaml:"sha256"`
}

type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "qualification document invalid: " + strings.Join(e.Problems, "; ")
}

// ParseMachineBucket accepts only the canonical YAML bytes produced by
// MarshalMachineBucket.
func ParseMachineBucket(data []byte) (MachineBucket, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	var bucket MachineBucket
	if err := decoder.Decode(&bucket); err != nil {
		return MachineBucket{}, fmt.Errorf("decode qualification machine bucket: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return MachineBucket{}, errors.New("decode qualification machine bucket: multiple YAML documents are not allowed")
		}
		return MachineBucket{}, fmt.Errorf("decode qualification machine bucket: %w", err)
	}

	canonical, err := MarshalMachineBucket(bucket)
	if err != nil {
		return MachineBucket{}, err
	}
	if !bytes.Equal(data, canonical) {
		return MachineBucket{}, errors.New("qualification machine bucket bytes are not canonical")
	}
	return bucket, nil
}

// MarshalMachineBucket validates a bucket and returns its canonical YAML.
func MarshalMachineBucket(bucket MachineBucket) ([]byte, error) {
	if err := bucket.Validate(); err != nil {
		return nil, err
	}

	var root yaml.Node
	if err := root.Encode(bucket); err != nil {
		return nil, fmt.Errorf("encode qualification machine bucket: %w", err)
	}
	sortMappingKeys(&root)

	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(&root); err != nil {
		return nil, fmt.Errorf("encode qualification machine bucket: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("close qualification machine bucket encoder: %w", err)
	}
	return output.Bytes(), nil
}

// Digest returns the material identity of exact canonical document bytes.
func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Validate enforces the v1 machine-bucket structure without reading a host.
func (b MachineBucket) Validate() error {
	var problems []string
	problem := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	if b.Schema != MachineBucketSchemaV1 {
		problem("schema is %q, want %q", b.Schema, MachineBucketSchemaV1)
	}
	if !stableIDPattern.MatchString(b.ID) {
		problem("id %q is not a lowercase stable id", b.ID)
	}
	if b.Revision == 0 {
		problem("revision must be greater than zero")
	}
	validateLine("title", b.Title, problem)
	if b.FactsSchema != machine.FactsSchemaV1 {
		problem("facts_schema is %q, want %q", b.FactsSchema, machine.FactsSchemaV1)
	}

	if err := b.Predicate.Target.Validate(); err != nil {
		problem("predicate.target: %v", err)
	}
	wantTarget := software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos"}
	if b.Predicate.Target != wantTarget {
		problem("predicate.target must identify darwin/arm64 macOS without a version")
	}
	validateSortedLines("predicate.hardware_models", b.Predicate.HardwareModels, problem)
	validateSortedLines("predicate.chips", b.Predicate.Chips, problem)
	if b.Predicate.PhysicalMemoryBytes.Minimum <= 0 {
		problem("predicate.physical_memory_bytes.minimum must be positive")
	}
	if b.Predicate.PhysicalMemoryBytes.Maximum <= 0 {
		problem("predicate.physical_memory_bytes.maximum must be positive")
	}
	if b.Predicate.PhysicalMemoryBytes.Maximum < b.Predicate.PhysicalMemoryBytes.Minimum {
		problem("predicate.physical_memory_bytes maximum must be greater than or equal to minimum")
	}

	validateLine("axis_labels.memory", b.AxisLabels.Memory, problem)
	validateLine("axis_labels.chip_generation", b.AxisLabels.ChipGeneration, problem)
	validateLine("axis_labels.memory_bandwidth", b.AxisLabels.MemoryBandwidth, problem)

	if len(b.Evidence) == 0 {
		problem("evidence must not be empty")
	}
	previousEvidence := ""
	seenEvidence := map[string]bool{}
	for index, evidence := range b.Evidence {
		location := fmt.Sprintf("evidence[%d]", index)
		if evidence.Kind != "results-record" && evidence.Kind != "release-review" {
			problem("%s.kind %q is not supported", location, evidence.Kind)
		}
		if !stableIDPattern.MatchString(evidence.ID) {
			problem("%s.id %q is not a lowercase stable id", location, evidence.ID)
		}
		if evidence.Revision == 0 {
			problem("%s.revision must be greater than zero", location)
		}
		if !sha256Pattern.MatchString(evidence.SHA256) {
			problem("%s.sha256 must be 64 lowercase hexadecimal characters", location)
		}

		semanticIdentity := evidence.Kind + "\x00" + evidence.ID + "\x00" + strconv.FormatUint(evidence.Revision, 10)
		if seenEvidence[semanticIdentity] {
			problem("evidence repeats identity %s/%s@%d", evidence.Kind, evidence.ID, evidence.Revision)
		}
		seenEvidence[semanticIdentity] = true
		exactIdentity := evidence.Kind + "\x00" + evidence.ID + "\x00" + fmt.Sprintf("%020d", evidence.Revision) + "\x00" + evidence.SHA256
		if previousEvidence != "" && exactIdentity <= previousEvidence {
			problem("evidence must be unique and sorted by exact identity")
		}
		previousEvidence = exactIdentity
	}

	validateSortedLines("invalidation_triggers", b.InvalidationTriggers, problem)

	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

// Matches reports whether canonical machine facts satisfy the complete hard
// predicate. Labels and evidence never participate in membership.
func (b MachineBucket) Matches(facts machine.Facts) (bool, error) {
	if err := b.Validate(); err != nil {
		return false, err
	}
	if err := facts.Validate(); err != nil {
		return false, fmt.Errorf("qualification machine bucket facts: %w", err)
	}
	if facts.Schema != b.FactsSchema || !b.Predicate.Target.Matches(facts.Target) {
		return false, nil
	}
	if !contains(b.Predicate.HardwareModels, facts.HardwareModel) || !contains(b.Predicate.Chips, facts.Chip) {
		return false, nil
	}
	return facts.PhysicalMemoryBytes >= b.Predicate.PhysicalMemoryBytes.Minimum &&
		facts.PhysicalMemoryBytes <= b.Predicate.PhysicalMemoryBytes.Maximum, nil
}

func validateLine(location, value string, problem func(string, ...any)) {
	if value == "" || strings.TrimSpace(value) != value {
		problem("%s must be nonempty and trimmed", location)
		return
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			problem("%s must not contain control characters", location)
			return
		}
	}
}

func validateSortedLines(location string, values []string, problem func(string, ...any)) {
	if len(values) == 0 {
		problem("%s must not be empty", location)
		return
	}
	previous := ""
	for index, value := range values {
		validateLine(fmt.Sprintf("%s[%d]", location, index), value, problem)
		if index > 0 && value <= previous {
			problem("%s must be unique and sorted", location)
		}
		previous = value
	}
}

func contains(values []string, wanted string) bool {
	index := sort.SearchStrings(values, wanted)
	return index < len(values) && values[index] == wanted
}

func sortMappingKeys(node *yaml.Node) {
	for _, child := range node.Content {
		sortMappingKeys(child)
	}
	if node.Kind != yaml.MappingNode {
		return
	}
	type pair struct {
		key   *yaml.Node
		value *yaml.Node
	}
	pairs := make([]pair, 0, len(node.Content)/2)
	for index := 0; index < len(node.Content); index += 2 {
		pairs = append(pairs, pair{key: node.Content[index], value: node.Content[index+1]})
	}
	sort.Slice(pairs, func(left, right int) bool {
		return pairs[left].key.Value < pairs[right].key.Value
	})
	node.Content = node.Content[:0]
	for _, item := range pairs {
		node.Content = append(node.Content, item.key, item.value)
	}
}
