// Package receipt owns the canonical C6 per-installation observed-history
// document. It contains no desired policy and is never removal authority.
package receipt

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/installplan"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"gopkg.in/yaml.v3"
)

const SchemaV1 = "temper-software-installation/v1"

var (
	idPattern       = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	unitIDPattern   = regexp.MustCompile(`^[a-z0-9]+(?:[._:-][a-z0-9]+)*$`)
	revisionPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._/+:-][a-z0-9]+)*$`)
	sha256Pattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Document struct {
	Schema             string               `yaml:"schema"`
	Installation       string               `yaml:"installation"`
	SoftwareLockDigest string               `yaml:"software_lock_digest"`
	Target             software.Target      `yaml:"target"`
	Root               string               `yaml:"root"`
	ObservedAt         string               `yaml:"observed_at"`
	Requirements       []Requirement        `yaml:"requirements"`
	Selections         map[string]Selection `yaml:"selections"`
	Units              map[string]Unit      `yaml:"units"`
}

type Requirement struct {
	SoftwareLockDigest string `yaml:"software_lock_digest"`
	Installation       string `yaml:"installation"`
	ReceiptSHA256      string `yaml:"receipt_sha256"`
}

type Selection struct {
	Provenance     string `yaml:"provenance"`
	Method         string `yaml:"method"`
	Adapter        string `yaml:"adapter"`
	RecipeRevision string `yaml:"recipe_revision"`
	RootUnit       string `yaml:"root_unit"`
}

type Unit struct {
	Adapter      string                `yaml:"adapter"`
	Scope        string                `yaml:"scope"`
	NativeName   string                `yaml:"native_name"`
	Version      string                `yaml:"version"`
	Revision     string                `yaml:"revision,omitempty"`
	Dependencies []string              `yaml:"dependencies"`
	Artifacts    []software.Artifact   `yaml:"artifacts,omitempty"`
	Location     string                `yaml:"location"`
	Ownership    installplan.Ownership `yaml:"ownership"`
	SharedClaim  string                `yaml:"shared_claim,omitempty"`
}

type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "software installation receipt invalid: " + strings.Join(e.Problems, "; ")
}

// Parse accepts only the canonical bytes produced by Marshal. This makes
// aliases, comments, implicit coercion, map/list reordering, and missing final
// newlines refusals rather than alternate spellings of receipt identity.
func Parse(data []byte) (Document, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var document Document
	if err := decoder.Decode(&document); err != nil {
		return Document{}, fmt.Errorf("decode software installation receipt: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Document{}, errors.New("decode software installation receipt: multiple YAML documents are not allowed")
		}
		return Document{}, fmt.Errorf("decode software installation receipt: %w", err)
	}
	canonical, err := Marshal(document)
	if err != nil {
		return Document{}, err
	}
	if !bytes.Equal(data, canonical) {
		return Document{}, errors.New("software installation receipt bytes are not canonical")
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
		return nil, fmt.Errorf("encode software installation receipt: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("close software installation receipt encoder: %w", err)
	}
	return output.Bytes(), nil
}

func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (d Document) Validate() error {
	var problems []string
	problem := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if d.Schema != SchemaV1 {
		problem("schema is %q, want %q", d.Schema, SchemaV1)
	}
	if !idPattern.MatchString(d.Installation) {
		problem("installation %q is not a lowercase stable id", d.Installation)
	}
	if !sha256Pattern.MatchString(d.SoftwareLockDigest) {
		problem("software_lock_digest must be 64 lowercase hexadecimal characters")
	}
	if err := d.Target.Validate(); err != nil {
		problem("target: %v", err)
	}
	if err := validateAbsolutePath(d.Root); err != nil {
		problem("root: %v", err)
	}
	if err := validateCanonicalInstant(d.ObservedAt); err != nil {
		problem("observed_at: %v", err)
	}
	previousRequirement := ""
	for index, requirement := range d.Requirements {
		if !sha256Pattern.MatchString(requirement.SoftwareLockDigest) {
			problem("requirements[%d].software_lock_digest must be 64 lowercase hexadecimal characters", index)
		}
		if !idPattern.MatchString(requirement.Installation) {
			problem("requirements[%d].installation %q is not a lowercase stable id", index, requirement.Installation)
		}
		if !sha256Pattern.MatchString(requirement.ReceiptSHA256) {
			problem("requirements[%d].receipt_sha256 must be 64 lowercase hexadecimal characters", index)
		}
		if previousRequirement != "" && requirement.SoftwareLockDigest <= previousRequirement {
			problem("requirements must be unique and sorted by software_lock_digest")
		}
		previousRequirement = requirement.SoftwareLockDigest
	}
	if len(d.Selections) == 0 {
		problem("selections must not be empty")
	}
	if len(d.Units) == 0 {
		problem("units must not be empty")
	}
	for _, packageID := range sortedKeys(d.Selections) {
		selection := d.Selections[packageID]
		if !idPattern.MatchString(packageID) {
			problem("selection id %q is not a lowercase stable id", packageID)
		}
		if selection.Provenance != softwarelock.ProvenanceCatalog && selection.Provenance != softwarelock.ProvenanceExperiment {
			problem("selection %q provenance %q must be catalog or experiment", packageID, selection.Provenance)
		}
		if !idPattern.MatchString(selection.Method) || !idPattern.MatchString(selection.Adapter) {
			problem("selection %q method and adapter must be lowercase stable ids", packageID)
		}
		if !revisionPattern.MatchString(selection.RecipeRevision) {
			problem("selection %q recipe_revision %q is not a stable revision", packageID, selection.RecipeRevision)
		}
		root, ok := d.Units[selection.RootUnit]
		if !unitIDPattern.MatchString(selection.RootUnit) || !ok {
			problem("selection %q references unknown or invalid root unit %q", packageID, selection.RootUnit)
		} else if root.Adapter != selection.Adapter {
			problem("selection %q adapter differs from root unit %q", packageID, selection.RootUnit)
		}
	}
	installationRoot := filepath.Join(d.Root, "software", "installations", d.Installation)
	for _, unitID := range sortedKeys(d.Units) {
		unit := d.Units[unitID]
		if !unitIDPattern.MatchString(unitID) {
			problem("unit id %q is not a stable unit id", unitID)
		}
		if !idPattern.MatchString(unit.Adapter) || !idPattern.MatchString(unit.Scope) {
			problem("unit %q adapter and scope must be lowercase stable ids", unitID)
		}
		if strings.TrimSpace(unit.NativeName) == "" || strings.TrimSpace(unit.Version) == "" {
			problem("unit %q native_name and version are required", unitID)
		}
		if unit.Revision != "" && !revisionPattern.MatchString(unit.Revision) {
			problem("unit %q revision %q is not a stable revision", unitID, unit.Revision)
		}
		if len(unit.Artifacts) == 0 && unit.Revision == "" {
			problem("unit %q requires an exact revision or at least one hashed artifact", unitID)
		}
		if !strictlySorted(unit.Dependencies) {
			problem("unit %q dependencies must be unique and sorted", unitID)
		}
		for _, dependencyID := range unit.Dependencies {
			dependency, ok := d.Units[dependencyID]
			if !ok {
				problem("unit %q references unknown dependency %q", unitID, dependencyID)
			} else if dependency.Adapter != unit.Adapter || dependency.Scope != unit.Scope {
				problem("unit %q dependency %q crosses adapter or scope", unitID, dependencyID)
			}
		}
		if !artifactsCanonical(unit.Artifacts) {
			problem("unit %q artifacts must have nonempty locators, lowercase SHA-256 values, and canonical order", unitID)
		}
		if err := validateAbsolutePath(unit.Location); err != nil {
			problem("unit %q location: %v", unitID, err)
		}
		if unit.Ownership != installplan.OwnershipTemperAdded && unit.Ownership != installplan.OwnershipPreExisting {
			problem("unit %q ownership %q must be temper-added or pre-existing", unitID, unit.Ownership)
		}
		if unit.SharedClaim != "" {
			want := installplan.SharedUnitKey(unit.Adapter, unit.Scope, unit.NativeName)
			if unit.SharedClaim != want {
				problem("unit %q shared_claim does not match its provider identity", unitID)
			}
		} else if !strictlyBelow(installationRoot, unit.Location) {
			problem("isolated unit %q location is outside its named installation", unitID)
		}
	}
	if !acyclicAndReachable(d.Selections, d.Units) {
		problem("units must be acyclic and reachable from selections")
	}
	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

// Build records exact post-install observation using ownership decisions from
// the already-validated plan.
func Build(desired softwarelock.Document, plan installplan.Plan, observed installplan.Observation, observedAt time.Time) (Document, error) {
	if err := desired.Validate(); err != nil {
		return Document{}, err
	}
	if plan.Target != desired.Target || observed.Target != desired.Target || plan.Installation.Root != observed.Root {
		return Document{}, errors.New("software receipt inputs disagree on target or root")
	}
	digest, err := desired.SemanticDigest()
	if err != nil {
		return Document{}, err
	}
	if plan.LockDigest != digest {
		return Document{}, errors.New("software installation plan belongs to another lock")
	}
	ownership := map[string]installplan.Unit{}
	for _, group := range plan.Groups {
		for _, unit := range group.Units {
			ownership[unit.ID] = unit
		}
	}
	document := Document{
		Schema: SchemaV1, Installation: plan.Installation.ID, SoftwareLockDigest: digest,
		Target: desired.Target, Root: plan.Installation.Root, ObservedAt: observedAt.UTC().Format(time.RFC3339Nano),
		Requirements: make([]Requirement, 0, len(plan.Requirements)),
		Selections:   make(map[string]Selection, len(desired.Selections)),
		Units:        make(map[string]Unit, len(desired.Units)),
	}
	for _, requirement := range plan.Requirements {
		document.Requirements = append(document.Requirements, Requirement{
			SoftwareLockDigest: requirement.SoftwareLockDigest,
			Installation:       requirement.InstallationID,
			ReceiptSHA256:      requirement.ReceiptSHA256,
		})
	}
	sort.Slice(document.Requirements, func(i, j int) bool {
		return document.Requirements[i].SoftwareLockDigest < document.Requirements[j].SoftwareLockDigest
	})
	for packageID, selection := range desired.Selections {
		document.Selections[packageID] = Selection{
			Provenance: selection.Provenance, Method: selection.Method, Adapter: selection.Adapter,
			RecipeRevision: selection.RecipeRevision, RootUnit: selection.RootUnit,
		}
	}
	for unitID, locked := range desired.Units {
		actual, ok := observed.Units[unitID]
		planned, plannedOK := ownership[unitID]
		if !ok || !plannedOK || !actual.Present || !installplan.MatchesLock(locked, actual) {
			return Document{}, fmt.Errorf("software post-state unit %q is not exact", unitID)
		}
		dependencies := append([]string(nil), locked.Dependencies...)
		sort.Strings(dependencies)
		artifacts := append([]software.Artifact(nil), locked.Artifacts...)
		sort.Slice(artifacts, func(i, j int) bool {
			if artifacts[i].Locator == artifacts[j].Locator {
				return artifacts[i].SHA256 < artifacts[j].SHA256
			}
			return artifacts[i].Locator < artifacts[j].Locator
		})
		document.Units[unitID] = Unit{
			Adapter: locked.Adapter, Scope: locked.Scope, NativeName: locked.NativeName,
			Version: locked.Version, Revision: locked.Revision,
			Dependencies: dependencies, Artifacts: artifacts,
			Location: actual.Location, Ownership: planned.Ownership, SharedClaim: planned.SharedClaim,
		}
	}
	if len(observed.Units) != len(desired.Units) {
		return Document{}, errors.New("software post-state contains unexpected units")
	}
	if err := document.ValidateAgainst(desired, plan.Installation); err != nil {
		return Document{}, err
	}
	return document, nil
}

func (d Document) ValidateAgainst(desired softwarelock.Document, installation installplan.Installation) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if err := desired.Validate(); err != nil {
		return err
	}
	digest, err := desired.SemanticDigest()
	if err != nil {
		return err
	}
	if d.SoftwareLockDigest != digest || d.Target != desired.Target {
		return errors.New("software installation receipt belongs to another lock or target")
	}
	if d.Installation != installation.ID || d.Root != installation.Root {
		return errors.New("software installation receipt belongs to another installation or root")
	}
	if len(d.Selections) != len(desired.Selections) || len(d.Units) != len(desired.Units) {
		return errors.New("software installation receipt closure differs from its lock")
	}
	for packageID, locked := range desired.Selections {
		got, ok := d.Selections[packageID]
		if !ok || got != (Selection{Provenance: locked.Provenance, Method: locked.Method, Adapter: locked.Adapter, RecipeRevision: locked.RecipeRevision, RootUnit: locked.RootUnit}) {
			return fmt.Errorf("software installation receipt selection %q differs from its lock", packageID)
		}
	}
	for unitID, locked := range desired.Units {
		got, ok := d.Units[unitID]
		if !ok || !sameReceiptLockUnit(got, locked) {
			return fmt.Errorf("software installation receipt unit %q differs from its lock", unitID)
		}
	}
	if len(d.Requirements) != len(desired.Requires) {
		return errors.New("software installation receipt requirements differ from its lock")
	}
	wanted := make([]string, len(desired.Requires))
	for index, requirement := range desired.Requires {
		wanted[index] = requirement.SoftwareLockDigest
	}
	sort.Strings(wanted)
	for index, digest := range wanted {
		if d.Requirements[index].SoftwareLockDigest != digest {
			return errors.New("software installation receipt requirements differ from its lock")
		}
	}
	return nil
}

func (d Document) VerifyObservation(observed installplan.Observation) error {
	if observed.Target != d.Target || observed.Root != d.Root || len(observed.Units) != len(d.Units) {
		return errors.New("software receipt provider observation differs in target, root, or closure")
	}
	for unitID, receipted := range d.Units {
		actual, ok := observed.Units[unitID]
		locked := softwarelock.Unit{
			Adapter: receipted.Adapter, Scope: receipted.Scope, NativeName: receipted.NativeName,
			Version: receipted.Version, Revision: receipted.Revision,
			Dependencies: receipted.Dependencies, Artifacts: receipted.Artifacts,
		}
		if !ok || !actual.Present || !installplan.MatchesLock(locked, actual) || actual.Location != receipted.Location {
			return fmt.Errorf("software receipt unit %q has provider drift", unitID)
		}
	}
	return nil
}

func (d Document) Previous() installplan.Previous {
	units := make(map[string]installplan.PreviousUnit, len(d.Units))
	for unitID, unit := range d.Units {
		units[unitID] = installplan.PreviousUnit{Ownership: unit.Ownership, SharedClaim: unit.SharedClaim}
	}
	return installplan.Previous{
		LockDigest: d.SoftwareLockDigest, Target: d.Target,
		Installation: installplan.Installation{ID: d.Installation, Root: d.Root}, Units: units,
	}
}

func (d Document) DesiredUnits() map[string]softwarelock.Unit {
	units := make(map[string]softwarelock.Unit, len(d.Units))
	for unitID, unit := range d.Units {
		units[unitID] = softwarelock.Unit{
			Adapter: unit.Adapter, Scope: unit.Scope, NativeName: unit.NativeName,
			Version: unit.Version, Revision: unit.Revision,
			Dependencies: append([]string(nil), unit.Dependencies...), Artifacts: append([]software.Artifact(nil), unit.Artifacts...),
		}
	}
	return units
}

func sameReceiptLockUnit(receipted Unit, locked softwarelock.Unit) bool {
	return receipted.Adapter == locked.Adapter && receipted.Scope == locked.Scope && receipted.NativeName == locked.NativeName &&
		receipted.Version == locked.Version && receipted.Revision == locked.Revision &&
		sameStrings(receipted.Dependencies, locked.Dependencies) && sameArtifacts(receipted.Artifacts, locked.Artifacts)
}

func sameStrings(left, right []string) bool {
	left = append([]string(nil), left...)
	right = append([]string(nil), right...)
	sort.Strings(left)
	sort.Strings(right)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sameArtifacts(left, right []software.Artifact) bool {
	left = append([]software.Artifact(nil), left...)
	right = append([]software.Artifact(nil), right...)
	sort.Slice(left, func(i, j int) bool {
		if left[i].Locator == left[j].Locator {
			return left[i].SHA256 < left[j].SHA256
		}
		return left[i].Locator < left[j].Locator
	})
	sort.Slice(right, func(i, j int) bool {
		if right[i].Locator == right[j].Locator {
			return right[i].SHA256 < right[j].SHA256
		}
		return right[i].Locator < right[j].Locator
	})
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func validateAbsolutePath(path string) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Dir(path) == path {
		return fmt.Errorf("path %q must be absolute, clean, and narrower than a filesystem root", path)
	}
	return nil
}

func validateCanonicalInstant(value string) error {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return errors.New("must be an RFC 3339 UTC instant")
	}
	if parsed.Location() != time.UTC || parsed.Format(time.RFC3339Nano) != value {
		return errors.New("must use canonical RFC 3339 UTC form")
	}
	return nil
}

func strictlyBelow(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != "." && relative != ".." && !filepath.IsAbs(relative) && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func strictlySorted(values []string) bool {
	for index := 1; index < len(values); index++ {
		if values[index] <= values[index-1] {
			return false
		}
	}
	return true
}

func artifactsCanonical(values []software.Artifact) bool {
	for index, artifact := range values {
		if strings.TrimSpace(artifact.Locator) == "" || !sha256Pattern.MatchString(artifact.SHA256) || artifact.Size < 0 || artifact.UnpackedSize < 0 || artifact.InstalledEntries < 0 || ((artifact.Format == "") != (artifact.ArchiveRoot == "")) || (artifact.Format == "" && (artifact.UnpackedSize != 0 || artifact.InstalledEntries != 0)) {
			return false
		}
		if index > 0 {
			previous := values[index-1]
			if artifact.Locator < previous.Locator || (artifact.Locator == previous.Locator && artifact.SHA256 <= previous.SHA256) {
				return false
			}
		}
	}
	return true
}

func acyclicAndReachable(selections map[string]Selection, units map[string]Unit) bool {
	visiting := map[string]bool{}
	visited := map[string]bool{}
	valid := true
	var visit func(string)
	visit = func(unitID string) {
		if visiting[unitID] {
			valid = false
			return
		}
		if visited[unitID] {
			return
		}
		unit, ok := units[unitID]
		if !ok {
			valid = false
			return
		}
		visiting[unitID] = true
		for _, dependencyID := range unit.Dependencies {
			visit(dependencyID)
		}
		delete(visiting, unitID)
		visited[unitID] = true
	}
	for _, selection := range selections {
		visit(selection.RootUnit)
	}
	return valid && len(visited) == len(units)
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
