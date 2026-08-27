// Package baseline owns immutable Labs-sourced setup baselines. A baseline is
// separate from an experiment: it materializes and verifies one exact local
// profile but does not make a comparative product or quality claim.
package baseline

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/temper-sh/temper/internal/fieldkit/catalog"
)

const (
	CatalogSchemaV1 = "field-kit-baseline-catalog/v1"
	PackageSchemaV1 = "field-kit-baseline/v1"
	PackageSchemaV2 = "field-kit-baseline/v2"
	PackageSchemaV3 = "field-kit-baseline/v3"

	OrchestrationTemperMultiStageV1 = "temper-multi-stage/v1"
)

var (
	idPattern       = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	revisionPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._/+:-][a-z0-9]+)*$`)
	sha256Pattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Document struct {
	Schema     string      `json:"schema"`
	Revision   int         `json:"revision"`
	CompiledAt string      `json:"compiled_at"`
	Baselines  []Reference `json:"baselines"`
}

type Reference struct {
	ID                 string `json:"id"`
	Revision           int    `json:"revision"`
	Availability       string `json:"availability"`
	AvailabilityReason string `json:"availability_reason"`
	PackagePath        string `json:"package_path"`
	PackageSHA256      string `json:"package_sha256"`
}

type Package struct {
	Schema        string            `json:"schema"`
	ID            string            `json:"id"`
	Revision      int               `json:"revision"`
	Origin        Origin            `json:"origin"`
	Title         string            `json:"title"`
	Summary       string            `json:"summary"`
	EvidenceScope string            `json:"evidence_scope"`
	Applicability catalog.Predicate `json:"applicability"`
	Relevance     []catalog.Signal  `json:"relevance"`
	Cost          catalog.Cost      `json:"cost"`
	Consent       catalog.Consent   `json:"consent"`
	Profile       Profile           `json:"profile"`
	Software      SoftwareClosure   `json:"software"`
	Mechanics     Mechanics         `json:"mechanics"`
	Report        Report            `json:"report"`
	Invalidation  []string          `json:"invalidation_triggers"`
}

type Origin struct {
	SourceID     string `json:"source_id"`
	SourceSchema string `json:"source_schema"`
	SourceSHA256 string `json:"source_sha256"`
	ProfileID    string `json:"profile_id"`
}

type Profile struct {
	Layout              string `json:"layout"`
	ModelRepository     string `json:"model_repository"`
	ModelRevision       string `json:"model_revision"`
	ModelFile           string `json:"model_file"`
	ModelBytes          int64  `json:"model_bytes"`
	ModelSHA256         string `json:"model_sha256"`
	EngineVersion       string `json:"engine_version"`
	EngineRevision      string `json:"engine_revision"`
	RouterVersion       string `json:"router_version"`
	RouterRevision      string `json:"router_revision"`
	TemplateRevision    string `json:"template_revision"`
	TemplateSHA256      string `json:"template_sha256"`
	ContextTokens       int    `json:"context_tokens"`
	MaximumOutputTokens int    `json:"maximum_output_tokens"`
	ParallelSlots       int    `json:"parallel_slots"`
	KV                  string `json:"kv"`
	FlashAttention      string `json:"flash_attention"`
	BatchTokens         int    `json:"batch_tokens"`
	MicrobatchTokens    int    `json:"microbatch_tokens"`
	Reasoning           string `json:"reasoning"`
	Speculation         string `json:"speculation"`
}

type SoftwareClosure struct {
	DefinitionSchema string            `json:"definition_schema"`
	DefinitionID     string            `json:"definition_id"`
	DefinitionSHA256 string            `json:"definition_sha256"`
	Resolved         string            `json:"resolved"`
	Packages         []SoftwarePackage `json:"packages"`
}

type SoftwarePackage struct {
	ID               string `json:"id"`
	Scope            string `json:"scope"`
	NativeName       string `json:"native_name"`
	Version          string `json:"version"`
	Revision         string `json:"revision"`
	RecipeRevision   string `json:"recipe_revision"`
	Locator          string `json:"locator"`
	SHA256           string `json:"sha256"`
	Bytes            int64  `json:"bytes"`
	UnpackedBytes    int64  `json:"unpacked_bytes"`
	InstalledEntries int    `json:"installed_entries"`
	ArchiveRoot      string `json:"archive_root"`
}

type Mechanics struct {
	Installation  string                 `json:"installation"`
	Mode          string                 `json:"mode"`
	Manifest      catalog.FileIdentity   `json:"manifest"`
	ManifestLock  catalog.FileIdentity   `json:"manifest_lock"`
	Runner        *catalog.FileIdentity  `json:"runner,omitempty"`
	Protocol      *ProtocolIdentity      `json:"runtime_protocol,omitempty"`
	Orchestration string                 `json:"orchestration,omitempty"`
	Prompt        catalog.FileIdentity   `json:"prompt"`
	Protocols     []catalog.FileIdentity `json:"protocols"`
	Stages        []Stage                `json:"stages"`
	Resume        string                 `json:"resume"`
	Interruption  string                 `json:"interruption"`
}

type ProtocolIdentity struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
	Schema   string `json:"schema"`
}

type Stage struct {
	ID        string `json:"id"`
	Operation string `json:"operation"`
	Summary   string `json:"summary"`
}

type Report struct {
	Schema             string   `json:"schema"`
	RequiredConditions []string `json:"required_conditions"`
	Sensitivity        string   `json:"sensitivity"`
	Submission         string   `json:"submission"`
}

type Snapshot struct {
	Document Document
	SHA256   string
	Root     string
	Entries  []Entry
}

type Entry struct {
	Reference   Reference
	Package     Package
	PackageData []byte
	Root        string
	Files       map[string][]byte
}

func Load(path string) (Snapshot, error) {
	data, err := readRegular(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read baseline catalog: %w", err)
	}
	var document Document
	if err := parseCanonicalJSON(data, &document); err != nil {
		return Snapshot{}, fmt.Errorf("parse baseline catalog: %w", err)
	}
	if err := document.Validate(); err != nil {
		return Snapshot{}, err
	}
	root := filepath.Dir(path)
	snapshot := Snapshot{Document: document, SHA256: Digest(data), Root: root}
	for _, reference := range document.Baselines {
		packageData, packagePath, err := readReferenced(root, reference.PackagePath)
		if err != nil {
			return Snapshot{}, fmt.Errorf("baseline %s@%d package: %w", reference.ID, reference.Revision, err)
		}
		if Digest(packageData) != reference.PackageSHA256 {
			return Snapshot{}, fmt.Errorf("baseline %s@%d package hash mismatch", reference.ID, reference.Revision)
		}
		var compiled Package
		if err := parseCanonicalJSON(packageData, &compiled); err != nil {
			return Snapshot{}, fmt.Errorf("baseline %s@%d package: %w", reference.ID, reference.Revision, err)
		}
		if err := compiled.Validate(); err != nil {
			return Snapshot{}, fmt.Errorf("baseline %s@%d package: %w", reference.ID, reference.Revision, err)
		}
		if compiled.ID != reference.ID || compiled.Revision != reference.Revision {
			return Snapshot{}, fmt.Errorf("baseline %s@%d reference differs from package identity", reference.ID, reference.Revision)
		}
		packageRoot := filepath.Dir(packagePath)
		identities := []catalog.FileIdentity{compiled.Mechanics.Manifest, compiled.Mechanics.ManifestLock, compiled.Mechanics.Prompt}
		if compiled.Mechanics.Runner != nil {
			identities = append(identities, *compiled.Mechanics.Runner)
		}
		identities = append(identities, compiled.Mechanics.Protocols...)
		files := make(map[string][]byte, len(identities))
		for _, identity := range identities {
			input, _, err := readReferenced(packageRoot, identity.Path)
			if err != nil || Digest(input) != identity.SHA256 {
				if err != nil {
					return Snapshot{}, fmt.Errorf("baseline %s@%d file %s: %w", compiled.ID, compiled.Revision, identity.Path, err)
				}
				return Snapshot{}, fmt.Errorf("baseline %s@%d file %s hash mismatch", compiled.ID, compiled.Revision, identity.Path)
			}
			files[identity.Path] = append([]byte(nil), input...)
		}
		snapshot.Entries = append(snapshot.Entries, Entry{
			Reference: reference, Package: compiled, PackageData: append([]byte(nil), packageData...),
			Root: packageRoot, Files: files,
		})
	}
	return snapshot, nil
}

func (d Document) Validate() error {
	if d.Schema != CatalogSchemaV1 || d.Revision <= 0 || strings.TrimSpace(d.CompiledAt) == "" {
		return errors.New("baseline catalog schema, revision, and compiled_at are required")
	}
	previous := ""
	active := map[string]bool{}
	for _, reference := range d.Baselines {
		key := fmt.Sprintf("%s@%09d", reference.ID, reference.Revision)
		if !idPattern.MatchString(reference.ID) || reference.Revision <= 0 || !safeRelative(reference.PackagePath) || !sha256Pattern.MatchString(reference.PackageSHA256) {
			return fmt.Errorf("baseline catalog reference %q is invalid", key)
		}
		switch reference.Availability {
		case "active":
			if active[reference.ID] {
				return fmt.Errorf("baseline catalog repeats active id %q", reference.ID)
			}
			active[reference.ID] = true
		case "paused", "retired":
		default:
			return fmt.Errorf("baseline %q has unsupported availability %q", key, reference.Availability)
		}
		if strings.TrimSpace(reference.AvailabilityReason) == "" || (previous != "" && key <= previous) {
			return errors.New("baseline references require reasons and unique sorted identities")
		}
		previous = key
	}
	return nil
}

func (p Package) Validate() error {
	if (p.Schema != PackageSchemaV1 && p.Schema != PackageSchemaV2 && p.Schema != PackageSchemaV3) || !idPattern.MatchString(p.ID) || p.Revision <= 0 {
		return errors.New("baseline package schema, id, or revision is invalid")
	}
	if strings.TrimSpace(p.Title) == "" || strings.TrimSpace(p.Summary) == "" || strings.TrimSpace(p.EvidenceScope) == "" {
		return errors.New("baseline title, summary, and evidence scope are required")
	}
	if !idPattern.MatchString(p.Origin.SourceID) || !revisionPattern.MatchString(p.Origin.SourceSchema) || !sha256Pattern.MatchString(p.Origin.SourceSHA256) || !idPattern.MatchString(p.Origin.ProfileID) {
		return errors.New("baseline origin is invalid")
	}
	if err := p.Applicability.Validate(); err != nil {
		return fmt.Errorf("baseline applicability: %w", err)
	}
	previousSignal := ""
	for _, signal := range p.Relevance {
		if !idPattern.MatchString(signal.ID) || signal.ID <= previousSignal || strings.TrimSpace(signal.Reason) == "" {
			return errors.New("baseline relevance signals must have sorted stable ids and reasons")
		}
		if err := signal.When.Validate(); err != nil {
			return fmt.Errorf("baseline relevance %q: %w", signal.ID, err)
		}
		previousSignal = signal.ID
	}
	if err := p.Cost.Validate(); err != nil {
		return fmt.Errorf("baseline cost: %w", err)
	}
	if len(p.Consent.Choices) < 2 || p.Consent.LocalOutput != "local-only" || p.Consent.Cleanup == "" {
		return errors.New("baseline consent requires choices, local-only output, and cleanup")
	}
	for name, values := range map[string][]string{"choices": p.Consent.Choices, "reads": p.Consent.Reads, "writes": p.Consent.Writes, "network destinations": p.Consent.NetworkDestinations, "renewed consent": p.Consent.RenewedConsent} {
		if !sortedUnique(values) {
			return fmt.Errorf("baseline consent %s must be unique and sorted", name)
		}
	}
	if err := p.Profile.Validate(); err != nil {
		return err
	}
	if err := p.Software.Validate(); err != nil {
		return err
	}
	if !idPattern.MatchString(p.Mechanics.Installation) || !idPattern.MatchString(p.Mechanics.Mode) || p.Mechanics.Resume == "" || p.Mechanics.Interruption == "" {
		return errors.New("baseline mechanics identity and recovery text are required")
	}
	identities := []catalog.FileIdentity{p.Mechanics.Manifest, p.Mechanics.ManifestLock, p.Mechanics.Prompt}
	switch p.Schema {
	case PackageSchemaV1:
		if p.Mechanics.Runner == nil || p.Mechanics.Runner.Path == "" || p.Mechanics.Protocol != nil || p.Mechanics.Orchestration != "" {
			return errors.New("baseline v1 mechanics requires a runner and no Temper runtime protocol")
		}
		identities = append(identities, *p.Mechanics.Runner)
	case PackageSchemaV2:
		if p.Mechanics.Runner != nil || p.Mechanics.Protocol == nil || !idPattern.MatchString(p.Mechanics.Protocol.ID) || p.Mechanics.Protocol.Revision <= 0 || !revisionPattern.MatchString(p.Mechanics.Protocol.Schema) || p.Mechanics.Orchestration != "" {
			return errors.New("baseline v2 mechanics requires one supported Temper runtime protocol and no external runner")
		}
	case PackageSchemaV3:
		if p.Mechanics.Runner != nil || p.Mechanics.Protocol == nil || !idPattern.MatchString(p.Mechanics.Protocol.ID) || p.Mechanics.Protocol.Revision <= 0 || !revisionPattern.MatchString(p.Mechanics.Protocol.Schema) || p.Mechanics.Orchestration != OrchestrationTemperMultiStageV1 {
			return errors.New("baseline v3 mechanics requires one supported Temper runtime protocol and Temper multi-stage orchestration")
		}
	}
	identities = append(identities, p.Mechanics.Protocols...)
	seenPaths := map[string]bool{}
	for _, identity := range identities {
		if !safeRelative(identity.Path) || !sha256Pattern.MatchString(identity.SHA256) || seenPaths[identity.Path] {
			return errors.New("baseline mechanics file identity is invalid")
		}
		seenPaths[identity.Path] = true
	}
	previousProtocol := ""
	for _, identity := range p.Mechanics.Protocols {
		if identity.Path <= previousProtocol {
			return errors.New("baseline protocol identities must be unique and sorted by path")
		}
		previousProtocol = identity.Path
	}
	wantOperations := []string{"software-install", "model-fetch", "config-apply", "software-check", "artifact-check", "material-bind", "live-protocol", "outcome"}
	if len(p.Mechanics.Stages) != len(wantOperations) {
		return errors.New("baseline mechanics has an incomplete stage sequence")
	}
	seenStages := map[string]bool{}
	for index, stage := range p.Mechanics.Stages {
		if !idPattern.MatchString(stage.ID) || seenStages[stage.ID] || stage.Operation != wantOperations[index] || strings.TrimSpace(stage.Summary) == "" {
			return errors.New("baseline mechanics stages are invalid or out of canonical order")
		}
		seenStages[stage.ID] = true
	}
	if p.Report.Schema == "" || !sortedUnique(p.Report.RequiredConditions) || len(p.Report.RequiredConditions) == 0 || p.Report.Submission != "explicit-export-only" {
		return errors.New("baseline report contract is invalid")
	}
	if !sortedUnique(p.Invalidation) || len(p.Invalidation) == 0 {
		return errors.New("baseline invalidation triggers are required and sorted")
	}
	return nil
}

// Material returns an immutable copy of one package-bound file.
func (e Entry) Material(name string) ([]byte, error) {
	data, ok := e.Files[name]
	if !ok {
		return nil, fmt.Errorf("baseline package material %q is absent", name)
	}
	return append([]byte(nil), data...), nil
}

func (p Profile) Validate() error {
	if !idPattern.MatchString(p.Layout) || !strings.Contains(p.ModelRepository, "/") || !revisionPattern.MatchString(p.ModelRevision) || !safeRelative(p.ModelFile) || p.ModelBytes <= 0 || !sha256Pattern.MatchString(p.ModelSHA256) {
		return errors.New("baseline profile model identity is invalid")
	}
	if p.EngineVersion == "" || !revisionPattern.MatchString(p.EngineRevision) || p.RouterVersion == "" || !revisionPattern.MatchString(p.RouterRevision) || !revisionPattern.MatchString(p.TemplateRevision) || !sha256Pattern.MatchString(p.TemplateSHA256) {
		return errors.New("baseline profile runtime identity is invalid")
	}
	if p.ContextTokens <= 0 || p.MaximumOutputTokens <= 0 || p.MaximumOutputTokens >= p.ContextTokens || p.ParallelSlots <= 0 || p.KV != "q8_0" || p.FlashAttention != "on" || p.BatchTokens <= 0 || p.MicrobatchTokens <= 0 || p.Reasoning != "off" || p.Speculation != "embedded-mtp-maximum-3" {
		return errors.New("baseline profile runtime tuning is incomplete or unsupported")
	}
	return nil
}

func (s SoftwareClosure) Validate() error {
	if !revisionPattern.MatchString(s.DefinitionSchema) || !idPattern.MatchString(s.DefinitionID) || !sha256Pattern.MatchString(s.DefinitionSHA256) {
		return errors.New("baseline software definition identity is invalid")
	}
	if _, err := time.Parse("2006-01-02", s.Resolved); err != nil {
		return errors.New("baseline software resolved date must be YYYY-MM-DD")
	}
	if len(s.Packages) != 2 || s.Packages[0].ID != "llama-cpp" || s.Packages[1].ID != "llama-swap" {
		return errors.New("baseline software closure must contain sorted llama-cpp and llama-swap packages")
	}
	for _, item := range s.Packages {
		parsed, err := url.Parse(item.Locator)
		if !idPattern.MatchString(item.ID) || !idPattern.MatchString(item.Scope) || item.NativeName == "" || item.Version == "" || !revisionPattern.MatchString(item.Revision) || !revisionPattern.MatchString(item.RecipeRevision) || err != nil || parsed.Scheme != "https" || parsed.Host == "" || !sha256Pattern.MatchString(item.SHA256) || item.Bytes <= 0 || item.UnpackedBytes <= 0 || item.InstalledEntries <= 0 || (item.ArchiveRoot != "." && !safeRelative(item.ArchiveRoot)) {
			return fmt.Errorf("baseline software package %q is invalid", item.ID)
		}
	}
	return nil
}

func Find(snapshot Snapshot, selector string) (Entry, error) {
	id, revisionText, found := strings.Cut(selector, "@")
	if !found || id == "" || revisionText == "" {
		return Entry{}, fmt.Errorf("baseline selector %q must be exact id@revision", selector)
	}
	var revision int
	if _, err := fmt.Sscanf(revisionText, "%d", &revision); err != nil || revision <= 0 || fmt.Sprintf("%d", revision) != revisionText {
		return Entry{}, fmt.Errorf("baseline selector %q has an invalid revision", selector)
	}
	for _, entry := range snapshot.Entries {
		if entry.Package.ID == id && entry.Package.Revision == revision {
			return entry, nil
		}
	}
	return Entry{}, fmt.Errorf("baseline %q is not in the immutable catalog", selector)
}

func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func parseCanonicalJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("expected exactly one JSON value")
	}
	canonical, err := json.MarshalIndent(target, "", "  ")
	if err != nil {
		return err
	}
	if !bytes.Equal(data, append(canonical, '\n')) {
		return errors.New("bytes are not canonical JSON")
	}
	return nil
}

func readReferenced(root, relative string) ([]byte, string, error) {
	if !safeRelative(relative) {
		return nil, "", errors.New("reference must be a clean relative path beneath its owner")
	}
	path := filepath.Join(root, filepath.FromSlash(relative))
	absoluteRoot, _ := filepath.Abs(root)
	absolutePath, _ := filepath.Abs(path)
	if absolutePath == absoluteRoot || !strings.HasPrefix(absolutePath, absoluteRoot+string(filepath.Separator)) {
		return nil, "", errors.New("reference escapes its owner")
	}
	data, err := readRegular(absolutePath)
	return data, absolutePath, err
}

func loadFS(source fs.FS, catalogPath, sourceName string) (Snapshot, error) {
	if !fs.ValidPath(catalogPath) {
		return Snapshot{}, errors.New("baseline catalog path is invalid")
	}
	data, err := fs.ReadFile(source, catalogPath)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read baseline catalog: %w", err)
	}
	var document Document
	if err := parseCanonicalJSON(data, &document); err != nil {
		return Snapshot{}, fmt.Errorf("parse baseline catalog: %w", err)
	}
	if err := document.Validate(); err != nil {
		return Snapshot{}, err
	}
	root := path.Dir(catalogPath)
	if root == "." {
		root = ""
	}
	snapshot := Snapshot{Document: document, SHA256: Digest(data), Root: sourceName}
	for _, reference := range document.Baselines {
		packagePath := path.Join(root, reference.PackagePath)
		packageData, err := readReferencedFS(source, root, reference.PackagePath)
		if err != nil {
			return Snapshot{}, fmt.Errorf("baseline %s@%d package: %w", reference.ID, reference.Revision, err)
		}
		if Digest(packageData) != reference.PackageSHA256 {
			return Snapshot{}, fmt.Errorf("baseline %s@%d package hash mismatch", reference.ID, reference.Revision)
		}
		var compiled Package
		if err := parseCanonicalJSON(packageData, &compiled); err != nil {
			return Snapshot{}, fmt.Errorf("baseline %s@%d package: %w", reference.ID, reference.Revision, err)
		}
		if err := compiled.Validate(); err != nil {
			return Snapshot{}, fmt.Errorf("baseline %s@%d package: %w", reference.ID, reference.Revision, err)
		}
		if compiled.ID != reference.ID || compiled.Revision != reference.Revision {
			return Snapshot{}, fmt.Errorf("baseline %s@%d reference differs from package identity", reference.ID, reference.Revision)
		}
		packageRoot := path.Dir(packagePath)
		identities := []catalog.FileIdentity{compiled.Mechanics.Manifest, compiled.Mechanics.ManifestLock, compiled.Mechanics.Prompt}
		if compiled.Mechanics.Runner != nil {
			identities = append(identities, *compiled.Mechanics.Runner)
		}
		identities = append(identities, compiled.Mechanics.Protocols...)
		files := make(map[string][]byte, len(identities))
		for _, identity := range identities {
			input, err := readReferencedFS(source, packageRoot, identity.Path)
			if err != nil {
				return Snapshot{}, fmt.Errorf("baseline %s@%d file %s: %w", compiled.ID, compiled.Revision, identity.Path, err)
			}
			if Digest(input) != identity.SHA256 {
				return Snapshot{}, fmt.Errorf("baseline %s@%d file %s hash mismatch", compiled.ID, compiled.Revision, identity.Path)
			}
			files[identity.Path] = append([]byte(nil), input...)
		}
		snapshot.Entries = append(snapshot.Entries, Entry{
			Reference: reference, Package: compiled, PackageData: append([]byte(nil), packageData...),
			Root: sourceName, Files: files,
		})
	}
	return snapshot, nil
}

func readReferencedFS(source fs.FS, root, relative string) ([]byte, error) {
	if !safeRelative(relative) {
		return nil, errors.New("reference must be a clean relative path beneath its owner")
	}
	joined := path.Join(root, relative)
	if !fs.ValidPath(joined) || (root != "" && joined != root && !strings.HasPrefix(joined, root+"/")) {
		return nil, errors.New("reference escapes its owner")
	}
	data, err := fs.ReadFile(source, joined)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func readRegular(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&fs.ModeSymlink != 0 {
		return nil, errors.New("expected a regular file without symlink indirection")
	}
	return os.ReadFile(path)
}

func safeRelative(value string) bool {
	return value != "" && filepath.IsLocal(value) && filepath.ToSlash(filepath.Clean(value)) == value && value != "."
}

func sortedUnique(values []string) bool {
	if !sort.StringsAreSorted(values) {
		return false
	}
	for index := 1; index < len(values); index++ {
		if values[index] == values[index-1] {
			return false
		}
	}
	return true
}
