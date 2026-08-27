package qualification

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
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

// Catalog is a validated qualification bundle containing only profile kinds whose complete
// typed validators have landed. Recommendation sets remain fail-closed.
type Catalog struct {
	Index          CatalogIndex
	MachineBuckets []MachineBucket
	ModelArtifacts []ModelArtifactProfile
	Engines        []EngineProfile
	ModelRuntimes  []ModelRuntimeProfile
	Tools          []ToolProfile
	Modes          []ModeProfile
	Activities     []ActivityProfile
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
		return Catalog{}, errors.New("load qualification catalog: recommendation sets require performance and applicability cross-document validation")
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
	engines := make([]EngineProfile, 0, len(index.Profiles))
	modelRuntimes := make([]ModelRuntimeProfile, 0, len(index.Profiles))
	toolProfiles := make([]ToolProfile, 0, len(index.Profiles))
	modeProfiles := make([]ModeProfile, 0, len(index.Profiles))
	activityProfiles := make([]ActivityProfile, 0, len(index.Profiles))
	type indexedProfile struct {
		path     string
		envelope ProfileEnvelope
	}
	indexedProfiles := make([]indexedProfile, 0, len(index.Profiles))
	profilesByReference := map[string]ProfileEnvelope{}
	modelArtifactsByReference := map[string]ModelArtifactProfile{}
	enginesByReference := map[string]EngineProfile{}
	modelRuntimesByReference := map[string]ModelRuntimeProfile{}
	toolsByReference := map[string]ToolProfile{}
	modesByReference := map[string]ModeProfile{}
	type indexedRuntime struct {
		indexed IndexedDocument
		profile ModelRuntimeProfile
	}
	indexedRuntimes := make([]indexedRuntime, 0, len(index.Profiles))
	type indexedMode struct {
		indexed IndexedDocument
		profile ModeProfile
	}
	indexedModes := make([]indexedMode, 0, len(index.Profiles))
	type indexedActivity struct {
		indexed IndexedDocument
		profile ActivityProfile
	}
	indexedActivities := make([]indexedActivity, 0, len(index.Profiles))
	for _, indexed := range index.Profiles {
		if indexed.Document.Schema != ModelArtifactSchemaV1 && indexed.Document.Schema != EngineSchemaV1 && indexed.Document.Schema != ModelRuntimeSchemaV1 && indexed.Document.Schema != ToolSchemaV1 && indexed.Document.Schema != ModeSchemaV1 && indexed.Document.Schema != ActivitySchemaV1 {
			return Catalog{}, fmt.Errorf("load qualification catalog: profile schema %q is not implemented", indexed.Document.Schema)
		}
		data, ok := files[indexed.Path]
		if !ok {
			return Catalog{}, fmt.Errorf("load qualification catalog: indexed document %q is missing", indexed.Path)
		}
		if digest := Digest(data); digest != indexed.Document.SHA256 {
			return Catalog{}, fmt.Errorf("load qualification catalog: indexed document %q sha256 is %s, want %s", indexed.Path, digest, indexed.Document.SHA256)
		}

		switch indexed.Document.Schema {
		case ModelArtifactSchemaV1:
			profile, err := ParseModelArtifactProfile(data)
			if err != nil {
				return Catalog{}, fmt.Errorf("load qualification catalog: indexed document %q: %w", indexed.Path, err)
			}
			if err := verifyIndexedProfile(indexed, profile.ProfileEnvelope, bucketReferences); err != nil {
				return Catalog{}, err
			}
			modelArtifacts = append(modelArtifacts, profile)
			modelArtifactsByReference[referenceExactIdentity(indexed.Document)] = profile
			indexedProfiles = append(indexedProfiles, indexedProfile{path: indexed.Path, envelope: profile.ProfileEnvelope})
			profilesByReference[referenceExactIdentity(indexed.Document)] = profile.ProfileEnvelope
		case EngineSchemaV1:
			profile, err := ParseEngineProfile(data)
			if err != nil {
				return Catalog{}, fmt.Errorf("load qualification catalog: indexed document %q: %w", indexed.Path, err)
			}
			if err := verifyIndexedProfile(indexed, profile.ProfileEnvelope, bucketReferences); err != nil {
				return Catalog{}, err
			}
			engines = append(engines, profile)
			enginesByReference[referenceExactIdentity(indexed.Document)] = profile
			indexedProfiles = append(indexedProfiles, indexedProfile{path: indexed.Path, envelope: profile.ProfileEnvelope})
			profilesByReference[referenceExactIdentity(indexed.Document)] = profile.ProfileEnvelope
		case ModelRuntimeSchemaV1:
			profile, err := ParseModelRuntimeProfile(data)
			if err != nil {
				return Catalog{}, fmt.Errorf("load qualification catalog: indexed document %q: %w", indexed.Path, err)
			}
			if err := verifyIndexedProfile(indexed, profile.ProfileEnvelope, bucketReferences); err != nil {
				return Catalog{}, err
			}
			modelRuntimes = append(modelRuntimes, profile)
			modelRuntimesByReference[referenceExactIdentity(indexed.Document)] = profile
			indexedRuntimes = append(indexedRuntimes, indexedRuntime{indexed: indexed, profile: profile})
			indexedProfiles = append(indexedProfiles, indexedProfile{path: indexed.Path, envelope: profile.ProfileEnvelope})
			profilesByReference[referenceExactIdentity(indexed.Document)] = profile.ProfileEnvelope
		case ToolSchemaV1:
			profile, err := ParseToolProfile(data)
			if err != nil {
				return Catalog{}, fmt.Errorf("load qualification catalog: indexed document %q: %w", indexed.Path, err)
			}
			if err := verifyIndexedProfile(indexed, profile.ProfileEnvelope, bucketReferences); err != nil {
				return Catalog{}, err
			}
			toolProfiles = append(toolProfiles, profile)
			toolsByReference[referenceExactIdentity(indexed.Document)] = profile
			indexedProfiles = append(indexedProfiles, indexedProfile{path: indexed.Path, envelope: profile.ProfileEnvelope})
			profilesByReference[referenceExactIdentity(indexed.Document)] = profile.ProfileEnvelope
		case ModeSchemaV1:
			profile, err := ParseModeProfile(data)
			if err != nil {
				return Catalog{}, fmt.Errorf("load qualification catalog: indexed document %q: %w", indexed.Path, err)
			}
			if err := verifyIndexedProfile(indexed, profile.ProfileEnvelope, bucketReferences); err != nil {
				return Catalog{}, err
			}
			modeProfiles = append(modeProfiles, profile)
			modesByReference[referenceExactIdentity(indexed.Document)] = profile
			indexedModes = append(indexedModes, indexedMode{indexed: indexed, profile: profile})
			indexedProfiles = append(indexedProfiles, indexedProfile{path: indexed.Path, envelope: profile.ProfileEnvelope})
			profilesByReference[referenceExactIdentity(indexed.Document)] = profile.ProfileEnvelope
		case ActivitySchemaV1:
			profile, err := ParseActivityProfile(data)
			if err != nil {
				return Catalog{}, fmt.Errorf("load qualification catalog: indexed document %q: %w", indexed.Path, err)
			}
			if err := verifyIndexedProfile(indexed, profile.ProfileEnvelope, bucketReferences); err != nil {
				return Catalog{}, err
			}
			activityProfiles = append(activityProfiles, profile)
			indexedActivities = append(indexedActivities, indexedActivity{indexed: indexed, profile: profile})
			indexedProfiles = append(indexedProfiles, indexedProfile{path: indexed.Path, envelope: profile.ProfileEnvelope})
			profilesByReference[referenceExactIdentity(indexed.Document)] = profile.ProfileEnvelope
		}
	}
	for _, profile := range indexedProfiles {
		if err := verifyIndexedEvidenceReferences(profile.path, profile.envelope, profilesByReference, bucketReferences); err != nil {
			return Catalog{}, err
		}
		for _, dependencyReference := range profile.envelope.Dependencies {
			dependency, ok := profilesByReference[referenceExactIdentity(dependencyReference.Profile)]
			if !ok {
				continue
			}
			if err := validateDependencyDisposition(profile.envelope, dependency); err != nil {
				return Catalog{}, fmt.Errorf("load qualification catalog: indexed document %q %w", profile.path, err)
			}
		}
	}

	for _, runtime := range indexedRuntimes {
		artifact, ok := modelArtifactsByReference[referenceExactIdentity(runtime.profile.Spec.ArtifactProfile)]
		if !ok {
			return Catalog{}, fmt.Errorf("load qualification catalog: indexed document %q references model artifact %s/%s@%d absent from the index", runtime.indexed.Path, runtime.profile.Spec.ArtifactProfile.Schema, runtime.profile.Spec.ArtifactProfile.ID, runtime.profile.Spec.ArtifactProfile.Revision)
		}
		engine, ok := enginesByReference[referenceExactIdentity(runtime.profile.Spec.EngineProfile)]
		if !ok {
			return Catalog{}, fmt.Errorf("load qualification catalog: indexed document %q references engine %s/%s@%d absent from the index", runtime.indexed.Path, runtime.profile.Spec.EngineProfile.Schema, runtime.profile.Spec.EngineProfile.ID, runtime.profile.Spec.EngineProfile.Revision)
		}
		if err := verifyRuntimeComposition(runtime.indexed.Path, runtime.profile, artifact, engine); err != nil {
			return Catalog{}, err
		}
	}
	for _, mode := range indexedModes {
		if err := verifyModeComposition(mode.indexed.Path, mode.profile, modelRuntimesByReference, toolsByReference); err != nil {
			return Catalog{}, err
		}
	}
	for _, activity := range indexedActivities {
		mode, ok := modesByReference[referenceExactIdentity(activity.profile.Spec.ModeProfile)]
		if !ok {
			return Catalog{}, fmt.Errorf("load qualification catalog: indexed document %q references mode %s/%s@%d absent from the index", activity.indexed.Path, activity.profile.Spec.ModeProfile.Schema, activity.profile.Spec.ModeProfile.ID, activity.profile.Spec.ModeProfile.Revision)
		}
		if err := verifyActivityComposition(activity.indexed.Path, activity.profile, mode, modelRuntimesByReference, toolsByReference); err != nil {
			return Catalog{}, err
		}
	}

	return Catalog{
		Index: index, MachineBuckets: buckets, ModelArtifacts: modelArtifacts, Engines: engines, ModelRuntimes: modelRuntimes, Tools: toolProfiles, Modes: modeProfiles, Activities: activityProfiles,
	}, nil
}

func verifyModeComposition(path string, mode ModeProfile, runtimes map[string]ModelRuntimeProfile, tools map[string]ToolProfile) error {
	for _, binding := range mode.Spec.Bindings {
		runtime, ok := runtimes[referenceExactIdentity(binding.RuntimeProfile)]
		if !ok {
			return fmt.Errorf("load qualification catalog: indexed document %q references runtime %s/%s@%d absent from the index", path, binding.RuntimeProfile.Schema, binding.RuntimeProfile.ID, binding.RuntimeProfile.Revision)
		}
		if binding.Role != runtime.Spec.Layout.Role {
			return fmt.Errorf("load qualification catalog: indexed document %q binding %q role %q does not match runtime role %q", path, binding.ID, binding.Role, runtime.Spec.Layout.Role)
		}
		if !contains(runtime.Applicability.Foregrounds, mode.Spec.Foreground) {
			return fmt.Errorf("load qualification catalog: indexed document %q foreground %q is absent from runtime %s@%d applicability", path, mode.Spec.Foreground, runtime.ID, runtime.Revision)
		}
	}

	harnesses := map[string]ModeHarness{}
	for _, harness := range mode.Spec.Harnesses {
		harnesses[harness.ID] = harness
	}
	activeTools := make([]Reference, 0, len(mode.Spec.Tools))
	for _, modeTool := range mode.Spec.Tools {
		tool, ok := tools[referenceExactIdentity(modeTool.Profile)]
		if !ok {
			return fmt.Errorf("load qualification catalog: indexed document %q references tool %s/%s@%d absent from the index", path, modeTool.Profile.Schema, modeTool.Profile.ID, modeTool.Profile.Revision)
		}
		for _, role := range tool.Spec.Backend.RequiredRoles {
			if _, ok := mode.Spec.RoleBindings[role]; !ok {
				return fmt.Errorf("load qualification catalog: indexed document %q tool %s@%d requires missing role %q", path, tool.ID, tool.Revision, role)
			}
		}
		if modeTool.Active {
			if !contains(tool.Applicability.Foregrounds, mode.Spec.Foreground) {
				return fmt.Errorf("load qualification catalog: indexed document %q foreground %q is absent from active tool %s@%d applicability", path, mode.Spec.Foreground, tool.ID, tool.Revision)
			}
			compatible := false
			for _, transport := range tool.Spec.Transports {
				harness, ok := harnesses[transport.Harness]
				if ok && harness.IntegrationRevision == transport.IntegrationRevision {
					compatible = true
					break
				}
			}
			if !compatible {
				return fmt.Errorf("load qualification catalog: indexed document %q active tool %s@%d has no exact harness transport", path, tool.ID, tool.Revision)
			}
			activeTools = append(activeTools, modeTool.Profile)
		}
	}
	composition, err := composeModeDataBoundary(path, mode.Spec.Bindings, activeTools, runtimes, tools)
	if err != nil {
		return err
	}
	return verifyComposedDataBoundary(path, mode.DataBoundary, composition)
}

func verifyActivityComposition(path string, activity ActivityProfile, mode ModeProfile, runtimes map[string]ModelRuntimeProfile, tools map[string]ToolProfile) error {
	if !equalStrings(activity.Roles, mode.Roles) {
		return fmt.Errorf("load qualification catalog: indexed document %q roles must exactly match mode %s@%d", path, mode.ID, mode.Revision)
	}
	if !stringSetIsSubset(activity.Applicability.Foregrounds, mode.Applicability.Foregrounds) {
		return fmt.Errorf("load qualification catalog: indexed document %q foreground applicability widens mode %s@%d", path, mode.ID, mode.Revision)
	}
	if !stringSetIsSubset(activity.Applicability.Harnesses, mode.Applicability.Harnesses) {
		return fmt.Errorf("load qualification catalog: indexed document %q harness applicability widens mode %s@%d", path, mode.ID, mode.Revision)
	}
	for _, bucket := range activity.Applicability.MachineBuckets {
		if !referenceSetContains(mode.Applicability.MachineBuckets, bucket) {
			return fmt.Errorf("load qualification catalog: indexed document %q machine-bucket applicability widens mode %s@%d", path, mode.ID, mode.Revision)
		}
	}
	if activity.QualificationStatus == QualificationStatusQualified {
		for _, harnessID := range activity.Applicability.Harnesses {
			for _, harness := range mode.Spec.Harnesses {
				if harness.ID == harnessID && !evidenceHasHarness(activity.Evidence, harness.ID, harness.IntegrationRevision) {
					return fmt.Errorf("load qualification catalog: indexed document %q qualified activity harness %s@%s has no exact evidence witness", path, harness.ID, harness.IntegrationRevision)
				}
			}
		}
	}

	modeActiveTools := map[string]bool{}
	for _, tool := range mode.Spec.Tools {
		if tool.Active {
			modeActiveTools[referenceExactIdentity(tool.Profile)] = true
		}
	}
	for _, tool := range activity.Spec.ActiveTools {
		if !modeActiveTools[referenceExactIdentity(tool)] {
			return fmt.Errorf("load qualification catalog: indexed document %q active tool %s/%s@%d is not active in mode %s@%d", path, tool.Schema, tool.ID, tool.Revision, mode.ID, mode.Revision)
		}
	}
	if len(activity.Spec.ActiveTools) >= len(modeActiveTools) {
		return fmt.Errorf("load qualification catalog: indexed document %q active tools must be a strict subset of mode %s@%d active tools", path, mode.ID, mode.Revision)
	}
	if activity.DataBoundary.Inference != mode.DataBoundary.Inference || activity.DataBoundary.Credentials != mode.DataBoundary.Credentials {
		return fmt.Errorf("load qualification catalog: indexed document %q inference and credentials must exactly match mode %s@%d", path, mode.ID, mode.Revision)
	}

	composition, err := composeModeDataBoundary(path, mode.Spec.Bindings, activity.Spec.ActiveTools, runtimes, tools)
	if err != nil {
		return err
	}
	return verifyComposedDataBoundary(path, activity.DataBoundary, composition)
}

type composedDataBoundary struct {
	reads   []string
	writes  []string
	network []ProfileNetworkUse
}

func composeModeDataBoundary(path string, bindings []ModeBinding, activeTools []Reference, runtimes map[string]ModelRuntimeProfile, tools map[string]ToolProfile) (composedDataBoundary, error) {
	reads := map[string]bool{}
	writes := map[string]bool{}
	network := map[string]ProfileNetworkUse{}
	add := func(boundary ProfileDataBoundary) {
		for _, value := range boundary.Reads {
			reads[value] = true
		}
		for _, value := range boundary.Writes {
			writes[value] = true
		}
		for _, use := range boundary.Network {
			identity := use.Purpose + "\x00" + use.Destination + "\x00" + use.Timing
			network[identity] = use
		}
	}
	for _, binding := range bindings {
		runtime, ok := runtimes[referenceExactIdentity(binding.RuntimeProfile)]
		if !ok {
			return composedDataBoundary{}, fmt.Errorf("load qualification catalog: indexed document %q references runtime %s/%s@%d absent from the index", path, binding.RuntimeProfile.Schema, binding.RuntimeProfile.ID, binding.RuntimeProfile.Revision)
		}
		add(runtime.DataBoundary)
	}
	for _, reference := range activeTools {
		tool, ok := tools[referenceExactIdentity(reference)]
		if !ok {
			return composedDataBoundary{}, fmt.Errorf("load qualification catalog: indexed document %q references tool %s/%s@%d absent from the index", path, reference.Schema, reference.ID, reference.Revision)
		}
		add(tool.DataBoundary)
	}

	networkUses := make([]ProfileNetworkUse, 0, len(network))
	for _, use := range network {
		networkUses = append(networkUses, use)
	}
	sort.Slice(networkUses, func(left, right int) bool {
		return networkUses[left].Purpose+"\x00"+networkUses[left].Destination+"\x00"+networkUses[left].Timing < networkUses[right].Purpose+"\x00"+networkUses[right].Destination+"\x00"+networkUses[right].Timing
	})
	return composedDataBoundary{reads: sortedSetKeys(reads), writes: sortedSetKeys(writes), network: networkUses}, nil
}

func verifyComposedDataBoundary(path string, boundary ProfileDataBoundary, composition composedDataBoundary) error {
	if !equalStrings(boundary.Reads, composition.reads) || !equalStrings(boundary.Writes, composition.writes) {
		return fmt.Errorf("load qualification catalog: indexed document %q data boundary reads/writes do not match the active composition", path)
	}
	if len(composition.network) != len(boundary.Network) {
		return fmt.Errorf("load qualification catalog: indexed document %q data boundary network does not match the active composition", path)
	}
	for index := range composition.network {
		if composition.network[index] != boundary.Network[index] {
			return fmt.Errorf("load qualification catalog: indexed document %q data boundary network does not match the active composition", path)
		}
	}
	return nil
}

func stringSetIsSubset(subset, superset []string) bool {
	for _, value := range subset {
		if !contains(superset, value) {
			return false
		}
	}
	return true
}

func sortedSetKeys(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func verifyIndexedProfile(indexed IndexedDocument, envelope ProfileEnvelope, bucketReferences map[string]bool) error {
	if envelope.Schema != indexed.Document.Schema || envelope.ID != indexed.Document.ID || envelope.Revision != indexed.Document.Revision {
		return fmt.Errorf("load qualification catalog: indexed document %q identity is %s/%s@%d, want %s/%s@%d", indexed.Path, envelope.Schema, envelope.ID, envelope.Revision, indexed.Document.Schema, indexed.Document.ID, indexed.Document.Revision)
	}
	for _, bucket := range envelope.Applicability.MachineBuckets {
		if !bucketReferences[referenceExactIdentity(bucket)] {
			return fmt.Errorf("load qualification catalog: indexed document %q references machine bucket %s/%s@%d absent from the index", indexed.Path, bucket.Schema, bucket.ID, bucket.Revision)
		}
	}
	return nil
}

func verifyIndexedEvidenceReferences(path string, envelope ProfileEnvelope, profiles map[string]ProfileEnvelope, buckets map[string]bool) error {
	for _, evidence := range envelope.Evidence {
		for _, scope := range []*ScopeReference{
			evidence.Scope.ArtifactProfile,
			evidence.Scope.EngineProfile,
			evidence.Scope.RuntimeProfile,
			evidence.Scope.ToolProfile,
			evidence.Scope.ModeProfile,
			evidence.Scope.ActivityProfile,
		} {
			if scope == nil || (scope.Schema == envelope.Schema && scope.ID == envelope.ID && scope.Revision == envelope.Revision) {
				continue
			}
			reference := Reference{Schema: scope.Schema, ID: scope.ID, Revision: scope.Revision, SHA256: scope.SHA256}
			if _, ok := profiles[referenceExactIdentity(reference)]; !ok {
				return fmt.Errorf("load qualification catalog: indexed document %q evidence %q references profile %s/%s@%d absent from the index", path, evidence.ID, reference.Schema, reference.ID, reference.Revision)
			}
		}
		if evidence.Scope.MachineBucket != nil && !buckets[referenceExactIdentity(*evidence.Scope.MachineBucket)] {
			bucket := evidence.Scope.MachineBucket
			return fmt.Errorf("load qualification catalog: indexed document %q evidence %q references machine bucket %s@%d absent from the index", path, evidence.ID, bucket.ID, bucket.Revision)
		}
		for _, resident := range evidence.Scope.CoResidents {
			if _, ok := profiles[referenceExactIdentity(resident.RuntimeProfile)]; !ok {
				return fmt.Errorf("load qualification catalog: indexed document %q evidence %q references co-resident runtime %s@%d absent from the index", path, evidence.ID, resident.RuntimeProfile.ID, resident.RuntimeProfile.Revision)
			}
		}
	}
	return nil
}

func verifyRuntimeComposition(path string, runtime ModelRuntimeProfile, artifact ModelArtifactProfile, engine EngineProfile) error {
	role := runtime.Spec.Layout.Role
	if !contains(artifact.Roles, role) {
		return fmt.Errorf("load qualification catalog: indexed document %q role %q is absent from model artifact %s@%d", path, role, artifact.ID, artifact.Revision)
	}
	if !contains(engine.Roles, role) {
		return fmt.Errorf("load qualification catalog: indexed document %q role %q is absent from engine %s@%d", path, role, engine.ID, engine.Revision)
	}

	wantCapability := "chat-completions"
	if role == "rerank" {
		wantCapability = "rerank"
	}
	if !contains(engine.Spec.Capabilities, wantCapability) {
		return fmt.Errorf("load qualification catalog: indexed document %q role %q requires engine capability %q", path, role, wantCapability)
	}
	if role == "coder" && artifact.Spec.Template.State != "file" {
		return fmt.Errorf("load qualification catalog: indexed document %q coder layout requires an artifact-owned template file", path)
	}

	switch runtime.Spec.Layout.Speculation.State {
	case "drafter":
		if !contains(artifact.Spec.Sidecars, runtime.Spec.Layout.Speculation.Sidecar) {
			return fmt.Errorf("load qualification catalog: indexed document %q drafter sidecar %q is absent from the model artifact", path, runtime.Spec.Layout.Speculation.Sidecar)
		}
		if !contains(engine.Spec.Capabilities, "drafter-speculation") {
			return fmt.Errorf("load qualification catalog: indexed document %q drafter layout requires engine capability %q", path, "drafter-speculation")
		}
	case "mtp":
		if !contains(engine.Spec.Capabilities, "mtp-speculation") {
			return fmt.Errorf("load qualification catalog: indexed document %q mtp layout requires engine capability %q", path, "mtp-speculation")
		}
	}
	return nil
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
