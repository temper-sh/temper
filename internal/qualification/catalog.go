package qualification

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	CatalogSchemaV1       = "temper-qualification-catalog/v1"
	ModelArtifactSchemaV1 = "temper-qualification-model-artifact/v1"
	EngineSchemaV1        = "temper-qualification-engine/v1"
	ModelRuntimeSchemaV1  = "temper-qualification-model-runtime/v1"
	ToolSchemaV1          = "temper-qualification-tool/v1"
	ModeSchemaV1          = "temper-qualification-mode/v1"
	ActivitySchemaV1      = "temper-qualification-activity/v1"
)

var profileKinds = map[string]string{
	ModelArtifactSchemaV1: "model-artifact",
	EngineSchemaV1:        "engine",
	ModelRuntimeSchemaV1:  "model-runtime",
	ToolSchemaV1:          "tool",
	ModeSchemaV1:          "mode",
	ActivitySchemaV1:      "activity",
}

// Reference binds a semantic document identity to its exact canonical bytes.
type Reference struct {
	Schema   string `yaml:"schema"`
	ID       string `yaml:"id"`
	Revision uint64 `yaml:"revision"`
	SHA256   string `yaml:"sha256"`
}

// IndexedDocument binds an exact qualification document to its release path.
type IndexedDocument struct {
	Document Reference `yaml:"document"`
	Path     string    `yaml:"path"`
}

// CatalogIndex is the exact current projection shipped by one Temper release.
// It contains no user selection or moving-Labs state.
type CatalogIndex struct {
	Schema             string              `yaml:"schema"`
	Revision           uint64              `yaml:"revision"`
	PublishedAt        string              `yaml:"published_at"`
	MachineBuckets     []IndexedDocument   `yaml:"machine_buckets"`
	Profiles           []IndexedDocument   `yaml:"profiles"`
	RecommendationSets []RecommendationSet `yaml:"recommendation_sets"`
}

// RecommendationSet presents qualified runtime tradeoffs without ordering or
// selecting its members.
type RecommendationSet struct {
	ID            string                      `yaml:"id"`
	Applicability RecommendationApplicability `yaml:"applicability"`
	Explanation   string                      `yaml:"explanation"`
	Members       []RecommendationMember      `yaml:"members"`
}

// RecommendationApplicability is the complete hard scope of a comparison
// group.
type RecommendationApplicability struct {
	MachineBuckets []Reference `yaml:"machine_buckets"`
	Foreground     string      `yaml:"foreground"`
	Role           string      `yaml:"role"`
}

// RecommendationMember explains one exact runtime option without giving it a
// rank or default status.
type RecommendationMember struct {
	RuntimeProfile Reference `yaml:"runtime_profile"`
	Reason         string    `yaml:"reason"`
	Strengths      []string  `yaml:"strengths"`
	Costs          []string  `yaml:"costs"`
}

// Catalog is a validated C7 bundle containing only profile kinds whose complete
// typed validators have landed. Recommendation sets remain fail-closed.
type Catalog struct {
	Index          CatalogIndex
	MachineBuckets []MachineBucket
	ModelArtifacts []ModelArtifactProfile
}

// ParseCatalogIndex accepts only the canonical YAML bytes produced by
// MarshalCatalogIndex.
func ParseCatalogIndex(data []byte) (CatalogIndex, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	var index CatalogIndex
	if err := decoder.Decode(&index); err != nil {
		return CatalogIndex{}, fmt.Errorf("decode qualification catalog index: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return CatalogIndex{}, errors.New("decode qualification catalog index: multiple YAML documents are not allowed")
		}
		return CatalogIndex{}, fmt.Errorf("decode qualification catalog index: %w", err)
	}

	canonical, err := MarshalCatalogIndex(index)
	if err != nil {
		return CatalogIndex{}, err
	}
	if !bytes.Equal(data, canonical) {
		return CatalogIndex{}, errors.New("qualification catalog index bytes are not canonical")
	}
	return index, nil
}

// MarshalCatalogIndex validates an index and returns its canonical YAML.
func MarshalCatalogIndex(index CatalogIndex) ([]byte, error) {
	if err := index.Validate(); err != nil {
		return nil, err
	}

	var root yaml.Node
	if err := root.Encode(index); err != nil {
		return nil, fmt.Errorf("encode qualification catalog index: %w", err)
	}
	sortMappingKeys(&root)

	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(&root); err != nil {
		return nil, fmt.Errorf("encode qualification catalog index: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("close qualification catalog index encoder: %w", err)
	}
	return output.Bytes(), nil
}

// LoadCatalog validates an in-memory release bundle. Unindexed files are
// ignored so a bundle may retain immutable historical documents.
func LoadCatalog(indexData []byte, files map[string][]byte) (Catalog, error) {
	index, err := ParseCatalogIndex(indexData)
	if err != nil {
		return Catalog{}, err
	}
	if len(index.RecommendationSets) > 0 {
		return Catalog{}, errors.New("load qualification catalog: recommendation sets require implemented profile documents")
	}

	buckets := make([]MachineBucket, 0, len(index.MachineBuckets))
	bucketReferences := map[string]bool{}
	for _, indexed := range index.MachineBuckets {
		data, ok := files[indexed.Path]
		if !ok {
			return Catalog{}, fmt.Errorf("load qualification catalog: indexed document %q is missing", indexed.Path)
		}
		if digest := Digest(data); digest != indexed.Document.SHA256 {
			return Catalog{}, fmt.Errorf("load qualification catalog: indexed document %q sha256 is %s, want %s", indexed.Path, digest, indexed.Document.SHA256)
		}
		bucket, err := ParseMachineBucket(data)
		if err != nil {
			return Catalog{}, fmt.Errorf("load qualification catalog: indexed document %q: %w", indexed.Path, err)
		}
		if bucket.Schema != indexed.Document.Schema || bucket.ID != indexed.Document.ID || bucket.Revision != indexed.Document.Revision {
			return Catalog{}, fmt.Errorf("load qualification catalog: indexed document %q identity is %s/%s@%d, want %s/%s@%d", indexed.Path, bucket.Schema, bucket.ID, bucket.Revision, indexed.Document.Schema, indexed.Document.ID, indexed.Document.Revision)
		}
		buckets = append(buckets, bucket)
		bucketReferences[referenceExactIdentity(indexed.Document)] = true
	}

	modelArtifacts := make([]ModelArtifactProfile, 0, len(index.Profiles))
	for _, indexed := range index.Profiles {
		if indexed.Document.Schema != ModelArtifactSchemaV1 {
			return Catalog{}, fmt.Errorf("load qualification catalog: profile schema %q is not implemented", indexed.Document.Schema)
		}
		data, ok := files[indexed.Path]
		if !ok {
			return Catalog{}, fmt.Errorf("load qualification catalog: indexed document %q is missing", indexed.Path)
		}
		if digest := Digest(data); digest != indexed.Document.SHA256 {
			return Catalog{}, fmt.Errorf("load qualification catalog: indexed document %q sha256 is %s, want %s", indexed.Path, digest, indexed.Document.SHA256)
		}

		profile, err := ParseModelArtifactProfile(data)
		if err != nil {
			return Catalog{}, fmt.Errorf("load qualification catalog: indexed document %q: %w", indexed.Path, err)
		}
		if profile.ID != indexed.Document.ID || profile.Revision != indexed.Document.Revision {
			return Catalog{}, fmt.Errorf("load qualification catalog: indexed document %q identity is %s/%s@%d, want %s/%s@%d", indexed.Path, profile.Schema, profile.ID, profile.Revision, indexed.Document.Schema, indexed.Document.ID, indexed.Document.Revision)
		}
		for _, bucket := range profile.Applicability.MachineBuckets {
			if !bucketReferences[referenceExactIdentity(bucket)] {
				return Catalog{}, fmt.Errorf("load qualification catalog: indexed document %q references machine bucket %s/%s@%d absent from the index", indexed.Path, bucket.Schema, bucket.ID, bucket.Revision)
			}
		}
		modelArtifacts = append(modelArtifacts, profile)
	}

	return Catalog{Index: index, MachineBuckets: buckets, ModelArtifacts: modelArtifacts}, nil
}

// Validate enforces exact reference identity without resolving a document.
func (r Reference) Validate() error {
	var problems []string
	validateReference("reference", r, func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	})
	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

// Validate enforces the v1 catalog-index structure without loading documents.
func (c CatalogIndex) Validate() error {
	var problems []string
	problem := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	if c.Schema != CatalogSchemaV1 {
		problem("schema is %q, want %q", c.Schema, CatalogSchemaV1)
	}
	if c.Revision == 0 {
		problem("revision must be greater than zero")
	}
	if _, err := time.Parse(time.RFC3339, c.PublishedAt); err != nil {
		problem("published_at %q must be RFC 3339", c.PublishedAt)
	}
	if len(c.MachineBuckets) == 0 {
		problem("machine_buckets must not be empty")
	}
	validateIndexedDocuments("machine_buckets", c.MachineBuckets, MachineBucketSchemaV1, problem)
	validateIndexedDocuments("profiles", c.Profiles, "profile", problem)
	validateRecommendationSets(c.RecommendationSets, problem)

	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

func validateIndexedDocuments(location string, documents []IndexedDocument, expected string, problem func(string, ...any)) {
	previous := ""
	semanticIdentities := map[string]bool{}
	paths := map[string]bool{}
	for index, indexed := range documents {
		itemLocation := fmt.Sprintf("%s[%d]", location, index)
		validateReference(itemLocation+".document", indexed.Document, problem)

		kind, isProfile := profileKinds[indexed.Document.Schema]
		switch expected {
		case MachineBucketSchemaV1:
			if indexed.Document.Schema != MachineBucketSchemaV1 {
				problem("%s.document schema %q must be %q", itemLocation, indexed.Document.Schema, MachineBucketSchemaV1)
			}
		case "profile":
			if !isProfile {
				problem("%s.document schema %q is not a profile schema", itemLocation, indexed.Document.Schema)
			}
		}

		wantPath := ""
		if expected == MachineBucketSchemaV1 {
			wantPath = "machine-buckets/" + indexed.Document.ID + "/" + strconv.FormatUint(indexed.Document.Revision, 10) + ".yaml"
		} else if isProfile {
			wantPath = "profiles/" + kind + "/" + indexed.Document.ID + "/" + strconv.FormatUint(indexed.Document.Revision, 10) + ".yaml"
		}
		if wantPath != "" && indexed.Path != wantPath {
			problem("%s.path is %q, want %q", itemLocation, indexed.Path, wantPath)
		}
		if paths[indexed.Path] {
			problem("%s.path repeats %q", itemLocation, indexed.Path)
		}
		paths[indexed.Path] = true

		semanticIdentity := referenceSemanticIdentity(indexed.Document)
		if semanticIdentities[semanticIdentity] {
			problem("%s repeats document identity %s/%s@%d", location, indexed.Document.Schema, indexed.Document.ID, indexed.Document.Revision)
		}
		semanticIdentities[semanticIdentity] = true

		exactIdentity := referenceExactIdentity(indexed.Document)
		if previous != "" && exactIdentity <= previous {
			problem("%s must be unique and sorted by exact document identity", location)
		}
		previous = exactIdentity
	}
}

func validateRecommendationSets(sets []RecommendationSet, problem func(string, ...any)) {
	previousSet := ""
	for index, set := range sets {
		location := fmt.Sprintf("recommendation_sets[%d]", index)
		if !stableIDPattern.MatchString(set.ID) {
			problem("%s.id %q is not a lowercase stable id", location, set.ID)
		}
		if index > 0 && set.ID <= previousSet {
			problem("recommendation_sets must be unique and sorted by id")
		}
		previousSet = set.ID

		validateLine(location+".explanation", set.Explanation, problem)
		if len(set.Applicability.MachineBuckets) == 0 {
			problem("%s.applicability.machine_buckets must not be empty", location)
		}
		validateReferenceSet(location+".applicability.machine_buckets", set.Applicability.MachineBuckets, MachineBucketSchemaV1, problem)
		if set.Applicability.Foreground != "local" && set.Applicability.Foreground != "harness" {
			problem("%s.applicability.foreground %q must be local or harness", location, set.Applicability.Foreground)
		}
		if !stableIDPattern.MatchString(set.Applicability.Role) {
			problem("%s.applicability.role %q is not a lowercase stable id", location, set.Applicability.Role)
		}

		if len(set.Members) == 0 {
			problem("%s.members must not be empty", location)
		}
		previousMember := ""
		semanticMembers := map[string]bool{}
		for memberIndex, member := range set.Members {
			memberLocation := fmt.Sprintf("%s.members[%d]", location, memberIndex)
			validateReference(memberLocation+".runtime_profile", member.RuntimeProfile, problem)
			if member.RuntimeProfile.Schema != ModelRuntimeSchemaV1 {
				problem("%s.runtime_profile schema %q must be %q", memberLocation, member.RuntimeProfile.Schema, ModelRuntimeSchemaV1)
			}
			validateLine(memberLocation+".reason", member.Reason, problem)
			validateSortedLines(memberLocation+".strengths", member.Strengths, problem)
			validateSortedLines(memberLocation+".costs", member.Costs, problem)

			semanticIdentity := referenceSemanticIdentity(member.RuntimeProfile)
			if semanticMembers[semanticIdentity] {
				problem("%s.members repeats runtime profile identity %s/%s@%d", location, member.RuntimeProfile.Schema, member.RuntimeProfile.ID, member.RuntimeProfile.Revision)
			}
			semanticMembers[semanticIdentity] = true
			exactIdentity := referenceExactIdentity(member.RuntimeProfile)
			if previousMember != "" && exactIdentity <= previousMember {
				problem("%s.members must be unique and sorted by exact runtime profile identity", location)
			}
			previousMember = exactIdentity
		}
	}
}

func validateReferenceSet(location string, references []Reference, schema string, problem func(string, ...any)) {
	previous := ""
	semanticIdentities := map[string]bool{}
	for index, reference := range references {
		itemLocation := fmt.Sprintf("%s[%d]", location, index)
		validateReference(itemLocation, reference, problem)
		if reference.Schema != schema {
			problem("%s schema %q must be %q", itemLocation, reference.Schema, schema)
		}

		semanticIdentity := referenceSemanticIdentity(reference)
		if semanticIdentities[semanticIdentity] {
			problem("%s repeats identity %s/%s@%d", location, reference.Schema, reference.ID, reference.Revision)
		}
		semanticIdentities[semanticIdentity] = true
		exactIdentity := referenceExactIdentity(reference)
		if previous != "" && exactIdentity <= previous {
			problem("%s must be unique and sorted by exact identity", location)
		}
		previous = exactIdentity
	}
}

func validateReference(location string, reference Reference, problem func(string, ...any)) {
	if reference.Schema != MachineBucketSchemaV1 {
		if _, ok := profileKinds[reference.Schema]; !ok {
			problem("%s.schema %q is not a qualification document schema", location, reference.Schema)
		}
	}
	if !stableIDPattern.MatchString(reference.ID) {
		problem("%s.id %q is not a lowercase stable id", location, reference.ID)
	}
	if reference.Revision == 0 {
		problem("%s.revision must be greater than zero", location)
	}
	if !sha256Pattern.MatchString(reference.SHA256) {
		problem("%s.sha256 must be 64 lowercase hexadecimal characters", location)
	}
}

func referenceSemanticIdentity(reference Reference) string {
	return strings.Join([]string{reference.Schema, reference.ID, strconv.FormatUint(reference.Revision, 10)}, "\x00")
}

func referenceExactIdentity(reference Reference) string {
	return strings.Join([]string{
		reference.Schema,
		reference.ID,
		fmt.Sprintf("%020d", reference.Revision),
		reference.SHA256,
	}, "\x00")
}
