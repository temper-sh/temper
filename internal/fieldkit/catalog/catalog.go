// Package catalog verifies an immutable Labs-promoted Field Kit snapshot and
// deterministically evaluates hard machine applicability. It never reads Labs
// or chooses an experiment for the user.
package catalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	CatalogSchemaV1 = "field-kit-catalog/v1"
	PackageSchemaV1 = "field-kit-experiment/v1"
	MachineSchemaV1 = "temper-machine-facts/v1"
)

var (
	idPattern     = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Document struct {
	Schema      string      `json:"schema"`
	Revision    int         `json:"revision"`
	PromotedAt  string      `json:"promoted_at"`
	Experiments []Reference `json:"experiments"`
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
	Schema        string       `json:"schema"`
	ID            string       `json:"id"`
	Revision      int          `json:"revision"`
	Origin        Origin       `json:"origin"`
	Kind          string       `json:"kind"`
	Title         string       `json:"title"`
	Question      string       `json:"question"`
	Decision      string       `json:"decision"`
	Applicability Predicate    `json:"applicability"`
	Relevance     []Signal     `json:"relevance"`
	Cost          Cost         `json:"cost"`
	Consent       Consent      `json:"consent"`
	Parameters    []Parameter  `json:"parameters"`
	Bounds        Bounds       `json:"bounds"`
	StopRules     []StopRule   `json:"stop_rules"`
	Mechanics     Mechanics    `json:"mechanics"`
	Report        Report       `json:"report"`
	Prompt        FileIdentity `json:"prompt"`
	Invalidation  []string     `json:"invalidation_triggers"`
}

type Origin struct {
	ExperimentID       string `json:"experiment_id"`
	ExperimentRevision int    `json:"experiment_revision"`
	ExperimentSHA256   string `json:"experiment_sha256"`
	PromotionID        string `json:"promotion_id"`
	PromotionRevision  int    `json:"promotion_revision"`
	PromotionSHA256    string `json:"promotion_sha256"`
}

type Predicate struct {
	OS                   string   `json:"os"`
	Arch                 string   `json:"arch"`
	Distribution         string   `json:"distribution"`
	MinPhysicalMemoryMiB int64    `json:"min_physical_memory_mib"`
	MaxPhysicalMemoryMiB int64    `json:"max_physical_memory_mib"`
	MinWiredLimitMiB     int64    `json:"min_wired_limit_mib"`
	ChipPrefixes         []string `json:"chip_prefixes"`
}

type Signal struct {
	ID     string    `json:"id"`
	When   Predicate `json:"when"`
	Reason string    `json:"reason"`
}

type Cost struct {
	FixedRuntimeMinutes int64  `json:"fixed_runtime_minutes"`
	SetupMinutesMin     int64  `json:"setup_minutes_min"`
	SetupMinutesMax     int64  `json:"setup_minutes_max"`
	NetworkBytesMax     int64  `json:"network_bytes_max"`
	TemporaryDiskBytes  int64  `json:"temporary_disk_bytes_max"`
	RetainedDiskBytes   int64  `json:"retained_disk_bytes_max"`
	MemoryPressure      string `json:"memory_pressure"`
	ServiceDisruption   string `json:"service_disruption"`
	PaidProvider        string `json:"paid_provider_exposure"`
	IdleRequired        bool   `json:"idle_required"`
}

type Consent struct {
	Choices             []string `json:"choices"`
	Reads               []string `json:"reads"`
	Writes              []string `json:"writes"`
	NetworkDestinations []string `json:"network_destinations"`
	LocalOutput         string   `json:"local_output"`
	Cleanup             string   `json:"cleanup"`
	RenewedConsent      []string `json:"renewed_consent_conditions"`
}

type Parameter struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Fixed    string   `json:"fixed"`
	Minimum  int64    `json:"minimum"`
	Maximum  int64    `json:"maximum"`
	Values   []string `json:"values"`
	Required bool     `json:"required"`
}

type Bounds struct {
	MaximumAttempts       int64 `json:"maximum_attempts"`
	MaximumRuntimeMinutes int64 `json:"maximum_runtime_minutes"`
	MaximumNetworkBytes   int64 `json:"maximum_network_bytes"`
}

type StopRule struct {
	ID          string `json:"id"`
	Observation string `json:"observation"`
	Action      string `json:"action"`
}

type Mechanics struct {
	TemperProtocol string         `json:"temper_protocol"`
	Plan           FileIdentity   `json:"plan"`
	ExternalInputs []FileIdentity `json:"external_inputs"`
	Resume         string         `json:"resume"`
	Interruption   string         `json:"interruption"`
}

type Report struct {
	Schema             string   `json:"schema"`
	RequiredConditions []string `json:"required_conditions"`
	Sensitivity        string   `json:"sensitivity"`
	Submission         string   `json:"submission"`
}

type FileIdentity struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type MachineFacts struct {
	Schema                  string `yaml:"schema"`
	Target                  Target `yaml:"target"`
	HardwareModel           string `yaml:"hardware_model"`
	Chip                    string `yaml:"chip"`
	OSBuild                 string `yaml:"os_build"`
	PhysicalMemoryBytes     int64  `yaml:"physical_memory_bytes"`
	MetalDeviceMemoryMiB    int64  `yaml:"metal_device_memory_mib"`
	MetalDeviceMemorySource string `yaml:"metal_device_memory_source"`
	WiredLimitMiB           int64  `yaml:"wired_limit_mib"`
	WiredLimitSource        string `yaml:"wired_limit_source"`
}

type Target struct {
	OS                  string `yaml:"os"`
	Arch                string `yaml:"arch"`
	Distribution        string `yaml:"distribution"`
	DistributionVersion string `yaml:"distribution_version"`
}

type Snapshot struct {
	Document Document
	SHA256   string
	Root     string
	Entries  []Entry
}

type Entry struct {
	Reference Reference
	Package   Package
	Relevance []Signal
}

type Applicability struct {
	Applicable bool
	Reasons    []string
}

// Load verifies canonical catalog and package bytes plus every referenced
// prompt, plan, and external input beneath the snapshot root.
func Load(path string) (Snapshot, error) {
	data, err := readRegular(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read catalog: %w", err)
	}
	var document Document
	if err := parseCanonicalJSON(data, &document); err != nil {
		return Snapshot{}, fmt.Errorf("parse catalog: %w", err)
	}
	if err := document.Validate(); err != nil {
		return Snapshot{}, err
	}
	root := filepath.Dir(path)
	snapshot := Snapshot{Document: document, SHA256: Digest(data), Root: root}
	for _, reference := range document.Experiments {
		packageData, packagePath, err := readReferenced(root, reference.PackagePath)
		if err != nil {
			return Snapshot{}, fmt.Errorf("experiment %s@%d package: %w", reference.ID, reference.Revision, err)
		}
		if Digest(packageData) != reference.PackageSHA256 {
			return Snapshot{}, fmt.Errorf("experiment %s@%d package hash mismatch", reference.ID, reference.Revision)
		}
		var promoted Package
		if err := parseCanonicalJSON(packageData, &promoted); err != nil {
			return Snapshot{}, fmt.Errorf("experiment %s@%d package: %w", reference.ID, reference.Revision, err)
		}
		if err := promoted.Validate(); err != nil {
			return Snapshot{}, fmt.Errorf("experiment %s@%d package: %w", reference.ID, reference.Revision, err)
		}
		if promoted.ID != reference.ID || promoted.Revision != reference.Revision {
			return Snapshot{}, fmt.Errorf("experiment %s@%d reference differs from package identity", reference.ID, reference.Revision)
		}
		packageRoot := filepath.Dir(packagePath)
		identities := append([]FileIdentity{promoted.Prompt, promoted.Mechanics.Plan}, promoted.Mechanics.ExternalInputs...)
		for _, identity := range identities {
			identityData, _, err := readReferenced(packageRoot, identity.Path)
			if err != nil {
				return Snapshot{}, fmt.Errorf("experiment %s@%d file %s: %w", promoted.ID, promoted.Revision, identity.Path, err)
			}
			if Digest(identityData) != identity.SHA256 {
				return Snapshot{}, fmt.Errorf("experiment %s@%d file %s hash mismatch", promoted.ID, promoted.Revision, identity.Path)
			}
		}
		snapshot.Entries = append(snapshot.Entries, Entry{Reference: reference, Package: promoted})
	}
	return snapshot, nil
}

func (d Document) Validate() error {
	if d.Schema != CatalogSchemaV1 {
		return fmt.Errorf("catalog schema is %q, want %q", d.Schema, CatalogSchemaV1)
	}
	if d.Revision <= 0 || strings.TrimSpace(d.PromotedAt) == "" {
		return errors.New("catalog revision and promoted_at are required")
	}
	previous := ""
	active := map[string]bool{}
	for _, reference := range d.Experiments {
		key := fmt.Sprintf("%s@%09d", reference.ID, reference.Revision)
		if !idPattern.MatchString(reference.ID) || reference.Revision <= 0 || !safeRelative(reference.PackagePath) || !sha256Pattern.MatchString(reference.PackageSHA256) {
			return fmt.Errorf("catalog experiment %q has invalid identity, path, or hash", key)
		}
		switch reference.Availability {
		case "active":
			if active[reference.ID] {
				return fmt.Errorf("catalog has more than one active revision for experiment %q", reference.ID)
			}
			active[reference.ID] = true
		case "paused", "retired":
		default:
			return fmt.Errorf("catalog experiment %q has unsupported availability %q", key, reference.Availability)
		}
		if strings.TrimSpace(reference.AvailabilityReason) == "" {
			return fmt.Errorf("catalog experiment %q has no availability reason", key)
		}
		if previous != "" && key <= previous {
			return errors.New("catalog experiments must be unique and sorted by id and revision")
		}
		previous = key
	}
	return nil
}

func (p Package) Validate() error {
	if p.Schema != PackageSchemaV1 || !idPattern.MatchString(p.ID) || p.Revision <= 0 {
		return errors.New("package schema, id, or revision is invalid")
	}
	if p.Kind != "fixed" && p.Kind != "bounded-adaptive" {
		return fmt.Errorf("package kind %q is not supported", p.Kind)
	}
	if strings.TrimSpace(p.Title) == "" || strings.TrimSpace(p.Question) == "" || strings.TrimSpace(p.Decision) == "" {
		return errors.New("package title, question, and decision are required")
	}
	for _, value := range []struct{ name, value string }{
		{"origin experiment id", p.Origin.ExperimentID}, {"origin promotion id", p.Origin.PromotionID},
	} {
		if !idPattern.MatchString(value.value) {
			return fmt.Errorf("%s is invalid", value.name)
		}
	}
	if p.Origin.ExperimentRevision <= 0 || p.Origin.PromotionRevision <= 0 ||
		!sha256Pattern.MatchString(p.Origin.ExperimentSHA256) || !sha256Pattern.MatchString(p.Origin.PromotionSHA256) {
		return errors.New("package origin revisions and hashes are required")
	}
	if err := p.Applicability.Validate(); err != nil {
		return fmt.Errorf("package applicability: %w", err)
	}
	previousSignal := ""
	for _, signal := range p.Relevance {
		if !idPattern.MatchString(signal.ID) || signal.ID <= previousSignal || strings.TrimSpace(signal.Reason) == "" {
			return errors.New("package relevance signals must have unique sorted ids and reasons")
		}
		if err := signal.When.Validate(); err != nil {
			return fmt.Errorf("package relevance signal %q: %w", signal.ID, err)
		}
		previousSignal = signal.ID
	}
	if err := p.Cost.Validate(); err != nil {
		return fmt.Errorf("package cost: %w", err)
	}
	if len(p.Consent.Choices) == 0 || p.Consent.LocalOutput != "local-only" || p.Consent.Cleanup == "" {
		return errors.New("package consent must declare choices, local-only output, and cleanup")
	}
	for name, values := range map[string][]string{
		"choices": p.Consent.Choices, "reads": p.Consent.Reads, "writes": p.Consent.Writes,
		"network destinations": p.Consent.NetworkDestinations, "renewed consent": p.Consent.RenewedConsent,
	} {
		if !sortedUnique(values) {
			return fmt.Errorf("package consent %s must be unique and sorted", name)
		}
	}
	if p.Bounds.MaximumAttempts <= 0 || p.Bounds.MaximumRuntimeMinutes <= 0 || p.Bounds.MaximumNetworkBytes < 0 {
		return errors.New("package bounds must declare positive attempts/runtime and nonnegative network bytes")
	}
	if p.Kind == "fixed" && p.Bounds.MaximumAttempts != 1 {
		return errors.New("fixed package must allow exactly one attempt")
	}
	if p.Kind == "bounded-adaptive" && p.Bounds.MaximumAttempts < 2 {
		return errors.New("bounded-adaptive package must allow at least two attempts")
	}
	if p.Bounds.MaximumNetworkBytes != p.Cost.NetworkBytesMax {
		return errors.New("package network cost and execution bound must be identical")
	}
	if len(p.StopRules) == 0 || len(p.Invalidation) == 0 {
		return errors.New("package stop rules and invalidation triggers must not be empty")
	}
	previousRule := ""
	for _, rule := range p.StopRules {
		if !idPattern.MatchString(rule.ID) || rule.ID <= previousRule || strings.TrimSpace(rule.Observation) == "" || rule.Action != "stop" {
			return errors.New("package stop rules must have unique sorted ids, observations, and stop actions")
		}
		previousRule = rule.ID
	}
	if !sortedUnique(p.Report.RequiredConditions) || !sortedUnique(p.Invalidation) {
		return errors.New("package report conditions and invalidation triggers must be unique and sorted")
	}
	if p.Mechanics.TemperProtocol == "" || p.Mechanics.Resume == "" || p.Mechanics.Interruption == "" {
		return errors.New("package mechanics protocol, resume, and interruption are required")
	}
	if p.Report.Schema == "" || len(p.Report.RequiredConditions) == 0 || p.Report.Submission != "explicit-export-only" {
		return errors.New("package report schema, conditions, and explicit export policy are required")
	}
	for _, identity := range append([]FileIdentity{p.Prompt, p.Mechanics.Plan}, p.Mechanics.ExternalInputs...) {
		if !safeRelative(identity.Path) || !sha256Pattern.MatchString(identity.SHA256) {
			return fmt.Errorf("package file identity %q is invalid", identity.Path)
		}
	}
	previous := ""
	for _, parameter := range p.Parameters {
		if !idPattern.MatchString(parameter.ID) || parameter.ID <= previous {
			return errors.New("package parameters must have unique sorted stable ids")
		}
		previous = parameter.ID
		switch parameter.Kind {
		case "fixed":
			if parameter.Fixed == "" {
				return fmt.Errorf("fixed parameter %q has no value", parameter.ID)
			}
		case "integer":
			if parameter.Minimum > parameter.Maximum {
				return fmt.Errorf("integer parameter %q has inverted bounds", parameter.ID)
			}
		case "enum":
			if len(parameter.Values) == 0 || !sortedUnique(parameter.Values) {
				return fmt.Errorf("enum parameter %q values must be unique and sorted", parameter.ID)
			}
		default:
			return fmt.Errorf("parameter %q kind %q is unsupported", parameter.ID, parameter.Kind)
		}
	}
	return nil
}

func (p Predicate) Validate() error {
	if p.OS == "" || p.Arch == "" || p.Distribution == "" || p.MinPhysicalMemoryMiB <= 0 || p.MinWiredLimitMiB <= 0 {
		return errors.New("target and positive minimum memory values are required")
	}
	if p.MaxPhysicalMemoryMiB != 0 && p.MaxPhysicalMemoryMiB < p.MinPhysicalMemoryMiB {
		return errors.New("maximum physical memory is below minimum")
	}
	if !sortedUnique(p.ChipPrefixes) {
		return errors.New("chip prefixes must be unique and sorted")
	}
	return nil
}

func (c Cost) Validate() error {
	if c.FixedRuntimeMinutes < 0 || c.SetupMinutesMin < 0 || c.SetupMinutesMax < c.SetupMinutesMin || c.NetworkBytesMax < 0 || c.TemporaryDiskBytes < 0 || c.RetainedDiskBytes < 0 {
		return errors.New("cost values are negative or setup range is inverted")
	}
	if c.MemoryPressure == "" || c.ServiceDisruption == "" || c.PaidProvider == "" {
		return errors.New("memory, service, and provider cost labels are required")
	}
	return nil
}

// Evaluate applies only hard promoted predicates. Advisory relevance is
// returned separately and never changes eligibility.
func Evaluate(promoted Package, facts MachineFacts) (Applicability, []Signal) {
	result := evaluate(promoted.Applicability, facts)
	var relevance []Signal
	if result.Applicable {
		for _, signal := range promoted.Relevance {
			if evaluate(signal.When, facts).Applicable {
				relevance = append(relevance, signal)
			}
		}
	}
	return result, relevance
}

// EvaluatePredicate exposes the one shared hard-machine predicate evaluator
// to the adjacent baseline catalog. Advisory policy remains with each caller.
func EvaluatePredicate(predicate Predicate, facts MachineFacts) Applicability {
	return evaluate(predicate, facts)
}

func evaluate(predicate Predicate, facts MachineFacts) Applicability {
	physicalMiB := facts.PhysicalMemoryBytes / (1024 * 1024)
	checks := []struct {
		ok     bool
		reason string
	}{
		{facts.Schema == MachineSchemaV1, "machine facts schema is not temper-machine-facts/v1"},
		{facts.Target.OS == predicate.OS, fmt.Sprintf("requires os=%s", predicate.OS)},
		{facts.Target.Arch == predicate.Arch, fmt.Sprintf("requires arch=%s", predicate.Arch)},
		{facts.Target.Distribution == predicate.Distribution, fmt.Sprintf("requires distribution=%s", predicate.Distribution)},
		{physicalMiB >= predicate.MinPhysicalMemoryMiB, fmt.Sprintf("requires physical-memory-mib>=%d", predicate.MinPhysicalMemoryMiB)},
		{predicate.MaxPhysicalMemoryMiB == 0 || physicalMiB <= predicate.MaxPhysicalMemoryMiB, fmt.Sprintf("requires physical-memory-mib<=%d", predicate.MaxPhysicalMemoryMiB)},
		{facts.WiredLimitMiB >= predicate.MinWiredLimitMiB, fmt.Sprintf("requires wired-limit-mib>=%d", predicate.MinWiredLimitMiB)},
	}
	if len(predicate.ChipPrefixes) > 0 {
		matched := false
		for _, prefix := range predicate.ChipPrefixes {
			matched = matched || strings.HasPrefix(facts.Chip, prefix)
		}
		checks = append(checks, struct {
			ok     bool
			reason string
		}{matched, "chip is outside promoted prefixes"})
	}
	var failures []string
	for _, check := range checks {
		if !check.ok {
			failures = append(failures, check.reason)
		}
	}
	if len(failures) > 0 {
		return Applicability{Reasons: failures}
	}
	return Applicability{Applicable: true, Reasons: []string{"all hard applicability predicates passed"}}
}

func ParseMachineFacts(data []byte) (MachineFacts, error) {
	// The canonical byte check belongs to Temper. Field Kit keeps a strict
	// known-field parser so it cannot reinterpret a future schema silently.
	var facts MachineFacts
	decoder := newYAMLDecoder(data)
	if err := decoder.Decode(&facts); err != nil {
		return MachineFacts{}, fmt.Errorf("decode machine facts: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return MachineFacts{}, errors.New("decode machine facts: expected exactly one document")
	}
	if facts.Schema != MachineSchemaV1 || facts.PhysicalMemoryBytes <= 0 || facts.WiredLimitMiB <= 0 {
		return MachineFacts{}, errors.New("machine facts identity or memory values are invalid")
	}
	return facts, nil
}

func newYAMLDecoder(data []byte) *yaml.Decoder {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	return decoder
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
	canonical = append(canonical, '\n')
	if !bytes.Equal(data, canonical) {
		return errors.New("bytes are not canonical JSON")
	}
	return nil
}

func readReferenced(root, relative string) ([]byte, string, error) {
	if !safeRelative(relative) {
		return nil, "", errors.New("reference must be a clean relative path beneath its owner")
	}
	path := filepath.Join(root, filepath.FromSlash(relative))
	resolvedRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, "", err
	}
	resolvedPath, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}
	if resolvedPath == resolvedRoot || !strings.HasPrefix(resolvedPath, resolvedRoot+string(filepath.Separator)) {
		return nil, "", errors.New("reference escapes its owner")
	}
	data, err := readRegular(resolvedPath)
	return data, resolvedPath, err
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

func safeRelative(path string) bool {
	return path != "" && filepath.IsLocal(path) && filepath.ToSlash(filepath.Clean(path)) == path && path != "."
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
