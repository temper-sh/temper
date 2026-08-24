// Package lockfile parses and validates exact desired software closures. It
// records resolution, never update policy or installed state.
package lockfile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/catalog"
	"github.com/temper-sh/temper/internal/software/policy"
	"gopkg.in/yaml.v3"
)

const SchemaV1 = "temper-software-lock/v1"

const (
	ProvenanceCatalog    = "catalog"
	ProvenanceExperiment = "experiment"
)

var (
	idPattern       = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	unitIDPattern   = regexp.MustCompile(`^[a-z0-9]+(?:[._:-][a-z0-9]+)*$`)
	revisionPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._/+:-][a-z0-9]+)*$`)
	sha256Pattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Document struct {
	Schema     string                    `yaml:"schema"`
	Provenance Provenance                `yaml:"provenance"`
	Requires   []InstallationRequirement `yaml:"requires"`
	Target     software.Target           `yaml:"target"`
	Resolved   string                    `yaml:"resolved"`
	Selections map[string]Selection      `yaml:"selections"`
	Units      map[string]Unit           `yaml:"units"`
}

// Provenance records immutable inputs that authorized resolution. Catalog and
// experiment are independent: a reviewed experiment may use a catalog and add
// fresh software, while a direct experiment lock may have no catalog at all.
type Provenance struct {
	Catalog    *CatalogIdentity    `yaml:"catalog,omitempty" json:"catalog,omitempty"`
	Experiment *ExperimentIdentity `yaml:"experiment,omitempty" json:"experiment,omitempty"`
}

type CatalogIdentity struct {
	Schema   string `yaml:"schema" json:"schema"`
	Sequence uint64 `yaml:"sequence" json:"sequence"`
	SHA256   string `yaml:"sha256" json:"sha256"`
}

type ExperimentIdentity struct {
	Schema           string `yaml:"schema" json:"schema"`
	ID               string `yaml:"id" json:"id"`
	DefinitionSHA256 string `yaml:"definition_sha256" json:"definition_sha256"`
}

type InstallationRequirement struct {
	SoftwareLockDigest string `yaml:"software_lock_digest" json:"software_lock_digest"`
}

type Selection struct {
	Provenance     string `yaml:"provenance" json:"provenance"`
	Method         string `yaml:"method" json:"method"`
	Adapter        string `yaml:"adapter" json:"adapter"`
	RecipeRevision string `yaml:"recipe_revision" json:"recipe_revision"`
	RootUnit       string `yaml:"root_unit" json:"root_unit"`
}

type Unit struct {
	Adapter      string     `yaml:"adapter" json:"adapter"`
	Scope        string     `yaml:"scope" json:"scope"`
	NativeName   string     `yaml:"native_name" json:"native_name"`
	Version      string     `yaml:"version" json:"version"`
	Revision     string     `yaml:"revision,omitempty" json:"revision,omitempty"`
	Dependencies []string   `yaml:"dependencies" json:"dependencies"`
	Artifacts    []Artifact `yaml:"artifacts,omitempty" json:"artifacts,omitempty"`
}

type Artifact = software.Artifact

type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "software lock invalid: " + strings.Join(e.Problems, "; ")
}

func Parse(data []byte) (Document, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	var document Document
	if err := decoder.Decode(&document); err != nil {
		return Document{}, fmt.Errorf("decode software lock: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Document{}, errors.New("decode software lock: multiple YAML documents are not allowed")
		}
		return Document{}, fmt.Errorf("decode software lock: %w", err)
	}
	if err := document.Validate(); err != nil {
		return Document{}, err
	}
	return document, nil
}

func Marshal(document Document) ([]byte, error) {
	if err := document.Validate(); err != nil {
		return nil, err
	}
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(document); err != nil {
		return nil, fmt.Errorf("encode software lock: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("close software lock encoder: %w", err)
	}
	return output.Bytes(), nil
}

func (d Document) Validate() error {
	var problems []string
	problem := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	if d.Schema != SchemaV1 {
		problem("schema is %q, want %q", d.Schema, SchemaV1)
	}
	if d.Provenance.Catalog == nil && d.Provenance.Experiment == nil {
		problem("provenance must contain catalog, experiment, or both")
	}
	if identity := d.Provenance.Catalog; identity != nil {
		if identity.Schema != catalog.SchemaV1 {
			problem("provenance.catalog.schema is %q, want %q", identity.Schema, catalog.SchemaV1)
		}
		if identity.Sequence == 0 {
			problem("provenance.catalog.sequence must be greater than zero")
		}
		if !sha256Pattern.MatchString(identity.SHA256) {
			problem("provenance.catalog.sha256 must be 64 lowercase hexadecimal characters")
		}
	}
	if identity := d.Provenance.Experiment; identity != nil {
		if !revisionPattern.MatchString(identity.Schema) {
			problem("provenance.experiment.schema %q is not a stable schema id", identity.Schema)
		}
		if !idPattern.MatchString(identity.ID) {
			problem("provenance.experiment.id %q is not a lowercase stable id", identity.ID)
		}
		if !sha256Pattern.MatchString(identity.DefinitionSHA256) {
			problem("provenance.experiment.definition_sha256 must be 64 lowercase hexadecimal characters")
		}
	}
	seenRequirements := map[string]bool{}
	for index, requirement := range d.Requires {
		if !sha256Pattern.MatchString(requirement.SoftwareLockDigest) {
			problem("requires[%d].software_lock_digest must be 64 lowercase hexadecimal characters", index)
		}
		if seenRequirements[requirement.SoftwareLockDigest] {
			problem("requires repeats software lock digest %q", requirement.SoftwareLockDigest)
		}
		seenRequirements[requirement.SoftwareLockDigest] = true
	}
	if err := d.Target.Validate(); err != nil {
		problem("target: %v", err)
	}
	if _, err := time.Parse("2006-01-02", d.Resolved); err != nil {
		problem("resolved %q must be YYYY-MM-DD", d.Resolved)
	}
	if len(d.Selections) == 0 {
		problem("selections must not be empty")
	}
	if len(d.Units) == 0 {
		problem("units must not be empty")
	}

	for _, id := range sortedKeys(d.Selections) {
		selection := d.Selections[id]
		if !idPattern.MatchString(id) {
			problem("selection id %q is not a lowercase stable id", id)
		}
		switch selection.Provenance {
		case ProvenanceCatalog:
			if d.Provenance.Catalog == nil {
				problem("selection %q has catalog provenance but the lock has no catalog identity", id)
			}
		case ProvenanceExperiment:
			if d.Provenance.Experiment == nil {
				problem("selection %q has experiment provenance but the lock has no experiment identity", id)
			}
		default:
			problem("selection %q provenance %q must be catalog or experiment", id, selection.Provenance)
		}
		if !idPattern.MatchString(selection.Method) {
			problem("selection %q method %q is not a lowercase stable id", id, selection.Method)
		}
		if !idPattern.MatchString(selection.Adapter) {
			problem("selection %q adapter %q is not a lowercase stable id", id, selection.Adapter)
		}
		if !revisionPattern.MatchString(selection.RecipeRevision) {
			problem("selection %q recipe_revision %q is not a stable revision", id, selection.RecipeRevision)
		}
		if !unitIDPattern.MatchString(selection.RootUnit) {
			problem("selection %q root_unit %q is not a stable unit id", id, selection.RootUnit)
			continue
		}
		root, ok := d.Units[selection.RootUnit]
		if !ok {
			problem("selection %q references unknown root unit %q", id, selection.RootUnit)
		} else if root.Adapter != selection.Adapter {
			problem("selection %q adapter %q does not match root unit adapter %q", id, selection.Adapter, root.Adapter)
		}
	}

	for _, id := range sortedKeys(d.Units) {
		unit := d.Units[id]
		if !unitIDPattern.MatchString(id) {
			problem("unit id %q is not a stable unit id", id)
		}
		if !idPattern.MatchString(unit.Adapter) {
			problem("unit %q adapter %q is not a lowercase stable id", id, unit.Adapter)
		}
		if !idPattern.MatchString(unit.Scope) {
			problem("unit %q scope %q is not a lowercase stable id", id, unit.Scope)
		}
		if strings.TrimSpace(unit.NativeName) == "" {
			problem("unit %q native_name is required", id)
		}
		if strings.TrimSpace(unit.Version) == "" {
			problem("unit %q version is required", id)
		}
		if unit.Revision != "" && !revisionPattern.MatchString(unit.Revision) {
			problem("unit %q revision %q is not a stable revision", id, unit.Revision)
		}
		if len(unit.Artifacts) == 0 && unit.Revision == "" {
			problem("unit %q requires an exact revision or at least one hashed artifact", id)
		}
		if duplicate := firstDuplicate(unit.Dependencies); duplicate != "" {
			problem("unit %q repeats dependency %q", id, duplicate)
		}
		for index, dependencyID := range unit.Dependencies {
			if !unitIDPattern.MatchString(dependencyID) {
				problem("unit %q dependencies[%d] %q is not a stable unit id", id, index, dependencyID)
			} else if _, ok := d.Units[dependencyID]; !ok {
				problem("unit %q references unknown dependency %q", id, dependencyID)
			}
		}
		seenArtifacts := map[string]bool{}
		for index, artifact := range unit.Artifacts {
			location := fmt.Sprintf("unit %q artifacts[%d]", id, index)
			if strings.TrimSpace(artifact.Locator) == "" {
				problem("%s locator is required", location)
			}
			if seenArtifacts[artifact.Locator] {
				problem("unit %q repeats artifact locator %q", id, artifact.Locator)
			}
			seenArtifacts[artifact.Locator] = true
			if !sha256Pattern.MatchString(artifact.SHA256) {
				problem("%s sha256 must be 64 lowercase hexadecimal characters", location)
			}
			if artifact.Size < 0 {
				problem("%s size must not be negative", location)
			}
			if artifact.UnpackedSize < 0 {
				problem("%s unpacked_size must not be negative", location)
			}
			if artifact.InstalledEntries < 0 {
				problem("%s installed_entries must not be negative", location)
			}
			if (artifact.Format == "") != (artifact.ArchiveRoot == "") {
				problem("%s format and archive_root must be declared together", location)
			}
			if artifact.Format == "" && (artifact.UnpackedSize != 0 || artifact.InstalledEntries != 0) {
				problem("%s unpacked_size and installed_entries require an archive format", location)
			}
		}
	}

	reachable := map[string]bool{}
	states := map[string]uint8{}
	var stack []string
	var walk func(string, string, string)
	walk = func(id, adapter, scope string) {
		unit, ok := d.Units[id]
		if !ok {
			return
		}
		if unit.Adapter != adapter {
			problem("unit %q crosses adapter boundary %q -> %q", id, adapter, unit.Adapter)
		}
		if unit.Scope != scope {
			problem("unit %q crosses scope boundary %q -> %q", id, scope, unit.Scope)
		}
		switch states[id] {
		case 1:
			start := 0
			for index, stackID := range stack {
				if stackID == id {
					start = index
					break
				}
			}
			cycle := append([]string(nil), stack[start:]...)
			cycle = append(cycle, id)
			problem("dependency cycle: %s", strings.Join(cycle, " -> "))
			return
		case 2:
			reachable[id] = true
			return
		}
		states[id] = 1
		reachable[id] = true
		stack = append(stack, id)
		dependencies := append([]string(nil), unit.Dependencies...)
		sort.Strings(dependencies)
		for _, dependencyID := range dependencies {
			walk(dependencyID, adapter, scope)
		}
		stack = stack[:len(stack)-1]
		states[id] = 2
	}

	for _, selectionID := range sortedKeys(d.Selections) {
		selection := d.Selections[selectionID]
		if root, ok := d.Units[selection.RootUnit]; ok {
			// Each root is an independently reconcilable adapter/scope effect.
			states = map[string]uint8{}
			stack = nil
			walk(selection.RootUnit, selection.Adapter, root.Scope)
		}
	}
	for _, unitID := range sortedKeys(d.Units) {
		if !reachable[unitID] {
			problem("unit %q is not reachable from any selection", unitID)
		}
	}

	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

// ValidateAgainst checks the lock's historical catalog identity and selection
// references against the exact snapshot bytes supplied by the caller.
func (d Document) ValidateAgainst(supply catalog.Document, snapshotDigest string) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if err := supply.Validate(); err != nil {
		return err
	}
	var problems []string
	problem := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	identity := d.Provenance.Catalog
	if identity == nil {
		return errors.New("software lock has no catalog provenance to validate against")
	}
	if identity.Schema != supply.Schema {
		problem("catalog schema mismatch: lock has %q, snapshot has %q", identity.Schema, supply.Schema)
	}
	if identity.Sequence != supply.Sequence {
		problem("catalog sequence mismatch: lock has %d, snapshot has %d", identity.Sequence, supply.Sequence)
	}
	if identity.SHA256 != snapshotDigest {
		problem("catalog digest mismatch: lock has %q, snapshot has %q", identity.SHA256, snapshotDigest)
	}
	resolvedUnits := make(map[string]software.ResolvedUnit, len(d.Units))
	for unitID, unit := range d.Units {
		resolvedUnits[unitID] = software.ResolvedUnit{
			Scope: unit.Scope, NativeName: unit.NativeName, Version: unit.Version, Revision: unit.Revision,
			Dependencies: append([]string(nil), unit.Dependencies...), Artifacts: append([]software.Artifact(nil), unit.Artifacts...),
		}
	}
	for _, packageID := range sortedKeys(d.Selections) {
		selection := d.Selections[packageID]
		if selection.Provenance == ProvenanceExperiment {
			continue
		}
		adapterID, err := supply.AdapterFor(selection.Method, d.Target)
		if err != nil {
			problem("selection %q target adapter: %v", packageID, err)
		} else if adapterID != selection.Adapter {
			problem("selection %q adapter is %q, catalog selects %q", packageID, selection.Adapter, adapterID)
		}
		pkg, ok := supply.Packages[packageID]
		if !ok {
			problem("selection %q references unknown catalog package", packageID)
			continue
		}
		recipe, ok := pkg.Recipes[selection.Adapter]
		if !ok {
			problem("selection %q has no catalog recipe for adapter %q", packageID, selection.Adapter)
			continue
		}
		if recipe.Method != selection.Method {
			problem("selection %q method is %q, catalog recipe has %q", packageID, selection.Method, recipe.Method)
		}
		if recipe.RecipeRevision != selection.RecipeRevision {
			problem("selection %q recipe_revision is %q, catalog has %q", packageID, selection.RecipeRevision, recipe.RecipeRevision)
		}
		if root, ok := d.Units[selection.RootUnit]; ok {
			if root.NativeName != recipe.Source.NativeName() {
				problem("selection %q root native_name is %q, catalog recipe has %q", packageID, root.NativeName, recipe.Source.NativeName())
			} else if eligible, policyErr := policy.ClosureEligible(supply, selection.Adapter, packageID, selection.RootUnit, resolvedUnits, nil); policyErr != nil {
				problem("selection %q catalog policy: %v", packageID, policyErr)
			} else if !eligible {
				problem("selection %q closure does not satisfy catalog policy", packageID)
			}
		}
	}
	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

func (d Document) SemanticDigest() (string, error) {
	if err := d.Validate(); err != nil {
		return "", err
	}
	projection := digestDocument{
		Schema:     d.Schema,
		Provenance: d.Provenance,
		Requires:   canonicalRequirements(d.Requires),
		Target:     d.Target,
		Selections: cloneSelections(d.Selections),
		Units:      canonicalUnits(d.Units),
	}
	return digestJSON(projection)
}

func (d Document) ClosureDigest(selectionID string) (string, error) {
	if err := d.Validate(); err != nil {
		return "", err
	}
	selection, ok := d.Selections[selectionID]
	if !ok {
		return "", fmt.Errorf("selection %q does not exist", selectionID)
	}
	reachable := map[string]bool{}
	var walk func(string)
	walk = func(id string) {
		if reachable[id] {
			return
		}
		reachable[id] = true
		for _, dependency := range d.Units[id].Dependencies {
			walk(dependency)
		}
	}
	walk(selection.RootUnit)
	units := make(map[string]Unit, len(reachable))
	for id := range reachable {
		units[id] = d.Units[id]
	}
	projection := closureDigestDocument{
		SelectionID: selectionID,
		Selection:   selection,
		Units:       canonicalUnits(units),
	}
	return digestJSON(projection)
}

type digestDocument struct {
	Schema     string                    `json:"schema"`
	Provenance Provenance                `json:"provenance"`
	Requires   []InstallationRequirement `json:"requires"`
	Target     software.Target           `json:"target"`
	Selections map[string]Selection      `json:"selections"`
	Units      map[string]Unit           `json:"units"`
}

type closureDigestDocument struct {
	SelectionID string          `json:"selection_id"`
	Selection   Selection       `json:"selection"`
	Units       map[string]Unit `json:"units"`
}

func digestJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode software lock digest: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func cloneSelections(values map[string]Selection) map[string]Selection {
	cloned := make(map[string]Selection, len(values))
	for id, selection := range values {
		cloned[id] = selection
	}
	return cloned
}

func canonicalUnits(values map[string]Unit) map[string]Unit {
	cloned := make(map[string]Unit, len(values))
	for id, unit := range values {
		unit.Dependencies = append([]string(nil), unit.Dependencies...)
		sort.Strings(unit.Dependencies)
		unit.Artifacts = append([]Artifact(nil), unit.Artifacts...)
		sort.Slice(unit.Artifacts, func(i, j int) bool {
			if unit.Artifacts[i].Locator == unit.Artifacts[j].Locator {
				return unit.Artifacts[i].SHA256 < unit.Artifacts[j].SHA256
			}
			return unit.Artifacts[i].Locator < unit.Artifacts[j].Locator
		})
		cloned[id] = unit
	}
	return cloned
}

func canonicalRequirements(values []InstallationRequirement) []InstallationRequirement {
	cloned := make([]InstallationRequirement, len(values))
	copy(cloned, values)
	sort.Slice(cloned, func(i, j int) bool {
		return cloned[i].SoftwareLockDigest < cloned[j].SoftwareLockDigest
	})
	return cloned
}

func firstDuplicate(values []string) string {
	seen := map[string]bool{}
	for _, value := range values {
		if seen[value] {
			return value
		}
		seen[value] = true
	}
	return ""
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
