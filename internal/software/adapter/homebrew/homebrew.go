// Package homebrew translates Homebrew's machine-readable formula metadata
// into provider-neutral exact software candidates. Command execution is
// injected: this package owns the Homebrew protocol, not process lifecycle.
package homebrew

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/catalog"
)

const (
	adapterID = "homebrew"
	method    = "system-package"
	scope     = "system"
)

var (
	formulaReferencePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9@+._-]*(?:/[a-z0-9][a-z0-9@+._-]*){0,2}$`)
	sha256Pattern           = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Command is one non-shell Homebrew invocation requested by the adapter.
type Command struct {
	Executable string
	Args       []string
}

// Output is the captured result of a successful command invocation.
type Output struct {
	Stdout []byte
	Stderr []byte
}

// CommandRunner is the external read boundary consumed by this adapter. The
// composition root supplies the process implementation; tests supply records.
type CommandRunner interface {
	Run(context.Context, Command) (Output, error)
}

// Options contains process facts and the total budget for one provider read.
type Options struct {
	Executable string
	Timeout    time.Duration
}

// Resolver reads the one current stable Homebrew closure for a formula.
type Resolver struct {
	runner     CommandRunner
	executable string
	timeout    time.Duration
}

// New constructs a resolver without discovering executables or host policy.
func New(runner CommandRunner, options Options) (*Resolver, error) {
	if runner == nil {
		return nil, errors.New("Homebrew command runner is required")
	}
	if options.Executable == "" || !filepath.IsAbs(options.Executable) {
		return nil, errors.New("Homebrew executable must be an absolute path")
	}
	if options.Timeout <= 0 {
		return nil, errors.New("Homebrew candidate timeout must be greater than zero")
	}
	return &Resolver{runner: runner, executable: options.Executable, timeout: options.Timeout}, nil
}

// Descriptor declares the compiled adapter contract. Exact macOS release
// support is checked when choosing the provider's bottle tag.
func (r *Resolver) Descriptor() adapter.Descriptor {
	return adapter.Descriptor{
		ID: adapterID, Method: method, Protocol: catalog.AdapterProtocolV1,
		EffectModel: "shared",
		Targets:     []software.Target{{OS: "darwin", Arch: "arm64"}},
	}
}

// Candidates reads and translates Homebrew's current stable formula closure.
// It owns no retry and makes no policy choice among candidates.
func (r *Resolver) Candidates(ctx context.Context, request adapter.ResolveRequest) ([]software.Candidate, error) {
	if err := validateRequest(request); err != nil {
		return nil, err
	}
	bottleTag, err := bottleTag(request.Target)
	if err != nil {
		return nil, err
	}

	readContext, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	rootReference := request.Recipe.Source.Tap + "/" + request.Recipe.Source.Formula
	dependenciesOutput, err := r.run(readContext, []string{
		"deps", "--formula", "--full-name", "--topological",
		"--os=macos", "--arch=arm64", rootReference,
	})
	if err != nil {
		return nil, fmt.Errorf("read Homebrew dependency closure: %w", err)
	}
	dependencyReferences, err := parseDependencyReferences(dependenciesOutput.Stdout, rootReference)
	if err != nil {
		return nil, err
	}

	formulaReferences := append([]string{rootReference}, dependencyReferences...)
	sort.Strings(formulaReferences[1:])
	arguments := append([]string{"info", "--json=v1", "--variations", "--formula"}, formulaReferences...)
	metadataOutput, err := r.run(readContext, arguments)
	if err != nil {
		return nil, fmt.Errorf("read Homebrew formula metadata: %w", err)
	}

	candidate, err := translate(metadataOutput.Stdout, rootReference, request.Recipe.Source.Formula, formulaReferences, bottleTag)
	if err != nil {
		return nil, err
	}
	return []software.Candidate{candidate}, nil
}

func (r *Resolver) run(ctx context.Context, arguments []string) (Output, error) {
	if err := ctx.Err(); err != nil {
		return Output{}, err
	}
	command := Command{Executable: r.executable, Args: append([]string(nil), arguments...)}
	output, err := r.runner.Run(ctx, command)
	if err != nil {
		return Output{}, err
	}
	if err := ctx.Err(); err != nil {
		return Output{}, err
	}
	return output, nil
}

func validateRequest(request adapter.ResolveRequest) error {
	if request.Package == "" {
		return errors.New("Homebrew candidate package is required")
	}
	if err := request.Target.Validate(); err != nil {
		return fmt.Errorf("Homebrew candidate target: %w", err)
	}
	if request.Target.OS != "darwin" || request.Target.Arch != "arm64" {
		return fmt.Errorf("Homebrew adapter does not support target %s/%s", request.Target.OS, request.Target.Arch)
	}
	if request.Recipe.Method != method || request.Recipe.Source.Kind != "homebrew-formula" {
		return errors.New("Homebrew adapter requires a system-package Homebrew formula recipe")
	}
	if !formulaReferencePattern.MatchString(request.Recipe.Source.Tap) || strings.Count(request.Recipe.Source.Tap, "/") != 1 {
		return fmt.Errorf("Homebrew tap %q is not a canonical owner/tap reference", request.Recipe.Source.Tap)
	}
	if !formulaReferencePattern.MatchString(request.Recipe.Source.Formula) || strings.Contains(request.Recipe.Source.Formula, "/") {
		return fmt.Errorf("Homebrew formula %q is not a canonical formula name", request.Recipe.Source.Formula)
	}
	return nil
}

func bottleTag(target software.Target) (string, error) {
	if target.Distribution != "macos" || target.DistributionVersion == "" {
		return "", errors.New("Homebrew bottle resolution requires an exact macos distribution version")
	}
	majorText := strings.Split(target.DistributionVersion, ".")[0]
	major, err := strconv.Atoi(majorText)
	if err != nil {
		return "", fmt.Errorf("Homebrew macos version %q has no numeric major version", target.DistributionVersion)
	}
	names := map[int]string{
		11: "big_sur",
		12: "monterey",
		13: "ventura",
		14: "sonoma",
		15: "sequoia",
		26: "tahoe",
	}
	name, ok := names[major]
	if !ok {
		return "", fmt.Errorf("Homebrew adapter has no reviewed bottle tag for macos %d", major)
	}
	return "arm64_" + name, nil
}

func parseDependencyReferences(data []byte, rootReference string) ([]string, error) {
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 1 && strings.TrimSpace(lines[0]) == "" {
		return nil, nil
	}
	seen := map[string]bool{}
	references := make([]string, 0, len(lines))
	for index, line := range lines {
		reference := strings.TrimSpace(line)
		if reference != line || !formulaReferencePattern.MatchString(reference) {
			return nil, fmt.Errorf("Homebrew dependency output line %d is not a canonical formula reference", index+1)
		}
		if strings.Count(reference, "/") == 0 {
			reference = "homebrew/core/" + reference
		}
		if strings.Count(reference, "/") != 2 {
			return nil, fmt.Errorf("Homebrew dependency output line %d is not a full formula reference", index+1)
		}
		if reference == rootReference {
			return nil, errors.New("Homebrew dependency closure contains its root formula")
		}
		if seen[reference] {
			return nil, fmt.Errorf("Homebrew dependency closure repeats %q", reference)
		}
		seen[reference] = true
		references = append(references, reference)
	}
	return references, nil
}

type formulaMetadata struct {
	Name                    string                     `json:"name"`
	Tap                     string                     `json:"tap"`
	Revision                int                        `json:"revision"`
	VersionScheme           int                        `json:"version_scheme"`
	Dependencies            []string                   `json:"dependencies"`
	RecommendedDependencies []string                   `json:"recommended_dependencies"`
	Disabled                bool                       `json:"disabled"`
	Variations              map[string]json.RawMessage `json:"variations"`
	Versions                struct {
		Stable string `json:"stable"`
		Bottle bool   `json:"bottle"`
	} `json:"versions"`
	Bottle struct {
		Stable *struct {
			Rebuild int                       `json:"rebuild"`
			Files   map[string]bottleMetadata `json:"files"`
		} `json:"stable"`
	} `json:"bottle"`
}

type bottleMetadata struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

func translate(data []byte, rootReference, rootName string, expectedReferences []string, tag string) (software.Candidate, error) {
	var formulae []formulaMetadata
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&formulae); err != nil {
		return software.Candidate{}, fmt.Errorf("decode Homebrew formula JSON v1: %w", err)
	}
	if err := requireJSONEnd(decoder); err != nil {
		return software.Candidate{}, err
	}
	if len(formulae) == 0 {
		return software.Candidate{}, errors.New("Homebrew formula JSON v1 returned no formulae")
	}

	expected := make(map[string]bool, len(expectedReferences))
	for _, reference := range expectedReferences {
		expected[reference] = true
	}
	byReference := make(map[string]formulaMetadata, len(formulae))
	byName := make(map[string][]string, len(formulae))
	for index, formula := range formulae {
		if !validFormulaComponent(formula.Name) || !formulaReferencePattern.MatchString(formula.Tap) || strings.Count(formula.Tap, "/") != 1 {
			return software.Candidate{}, fmt.Errorf("Homebrew formula JSON v1 formula[%d] has invalid name or tap", index)
		}
		reference := formula.Tap + "/" + formula.Name
		if !expected[reference] {
			return software.Candidate{}, fmt.Errorf("Homebrew formula JSON v1 returned unexpected formula %q", reference)
		}
		if _, duplicate := byReference[reference]; duplicate {
			return software.Candidate{}, fmt.Errorf("Homebrew formula JSON v1 repeats formula %q", reference)
		}
		byReference[reference] = formula
		byName[formula.Name] = append(byName[formula.Name], reference)
	}
	for reference := range expected {
		if _, ok := byReference[reference]; !ok {
			return software.Candidate{}, fmt.Errorf("Homebrew formula JSON v1 omitted formula %q", reference)
		}
	}
	if byReference[rootReference].Name != rootName {
		return software.Candidate{}, fmt.Errorf("Homebrew root formula identity does not match catalog formula %q", rootName)
	}

	units := make(map[string]software.ResolvedUnit, len(byReference))
	for reference, formula := range byReference {
		unit, err := translateFormula(formula, reference, tag, byReference, byName)
		if err != nil {
			return software.Candidate{}, err
		}
		units[unitID(reference)] = unit
	}
	rootUnit := unitID(rootReference)
	if err := validateClosure(rootUnit, units); err != nil {
		return software.Candidate{}, err
	}
	return software.Candidate{RootUnit: rootUnit, Units: units, Current: true}, nil
}

func translateFormula(formula formulaMetadata, reference, tag string, byReference map[string]formulaMetadata, byName map[string][]string) (software.ResolvedUnit, error) {
	if formula.Disabled {
		return software.ResolvedUnit{}, fmt.Errorf("Homebrew formula %q is disabled", reference)
	}
	if formula.Versions.Stable == "" {
		return software.ResolvedUnit{}, fmt.Errorf("Homebrew formula %q has no stable version", reference)
	}
	if formula.Revision < 0 || formula.VersionScheme < 0 {
		return software.ResolvedUnit{}, fmt.Errorf("Homebrew formula %q has a negative provider revision", reference)
	}
	if _, varies := formula.Variations[tag]; varies {
		return software.ResolvedUnit{}, fmt.Errorf("Homebrew formula %q has an unsupported metadata variation for %q", reference, tag)
	}
	if _, varies := formula.Variations["all"]; varies {
		return software.ResolvedUnit{}, fmt.Errorf("Homebrew formula %q has an unsupported metadata variation for all targets", reference)
	}
	if !formula.Versions.Bottle || formula.Bottle.Stable == nil {
		return software.ResolvedUnit{}, fmt.Errorf("Homebrew formula %q has no stable bottle", reference)
	}
	if formula.Bottle.Stable.Rebuild < 0 {
		return software.ResolvedUnit{}, fmt.Errorf("Homebrew formula %q has a negative bottle rebuild", reference)
	}
	bottle, ok := formula.Bottle.Stable.Files[tag]
	if !ok {
		bottle, ok = formula.Bottle.Stable.Files["all"]
	}
	if !ok {
		return software.ResolvedUnit{}, fmt.Errorf("Homebrew formula %q has no bottle for %q", reference, tag)
	}
	if err := validateBottle(reference, bottle); err != nil {
		return software.ResolvedUnit{}, err
	}

	dependencyNames := append([]string(nil), formula.Dependencies...)
	dependencyNames = append(dependencyNames, formula.RecommendedDependencies...)
	seenDependencies := map[string]bool{}
	dependencies := make([]string, 0, len(dependencyNames))
	for _, dependencyName := range dependencyNames {
		dependencyReference, err := matchDependency(dependencyName, byReference, byName)
		if err != nil {
			return software.ResolvedUnit{}, fmt.Errorf("Homebrew formula %q: %w", reference, err)
		}
		dependencyID := unitID(dependencyReference)
		if seenDependencies[dependencyID] {
			return software.ResolvedUnit{}, fmt.Errorf("Homebrew formula %q repeats dependency %q", reference, dependencyName)
		}
		seenDependencies[dependencyID] = true
		dependencies = append(dependencies, dependencyID)
	}
	sort.Strings(dependencies)

	revision := fmt.Sprintf("formula:%d+scheme:%d+bottle:%d", formula.Revision, formula.VersionScheme, formula.Bottle.Stable.Rebuild)
	return software.ResolvedUnit{
		Scope: scope, NativeName: formula.Name, Version: formula.Versions.Stable, Revision: revision,
		Dependencies: dependencies,
		Artifacts:    []software.Artifact{{Locator: bottle.URL, SHA256: bottle.SHA256}},
	}, nil
}

func matchDependency(name string, byReference map[string]formulaMetadata, byName map[string][]string) (string, error) {
	if !formulaReferencePattern.MatchString(name) {
		return "", fmt.Errorf("dependency %q is not a canonical formula name", name)
	}
	if strings.Contains(name, "/") {
		if _, ok := byReference[name]; !ok {
			return "", fmt.Errorf("dependency %q is absent from the resolved closure", name)
		}
		return name, nil
	}
	matches := byName[name]
	if len(matches) == 0 {
		return "", fmt.Errorf("dependency %q is absent from the resolved closure", name)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("dependency %q is ambiguous in the resolved closure", name)
	}
	return matches[0], nil
}

func validateBottle(reference string, bottle bottleMetadata) error {
	parsed, err := url.Parse(bottle.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("Homebrew formula %q bottle locator must be an absolute https URL", reference)
	}
	if !sha256Pattern.MatchString(bottle.SHA256) {
		return fmt.Errorf("Homebrew formula %q bottle sha256 must be 64 lowercase hexadecimal characters", reference)
	}
	return nil
}

func validateClosure(root string, units map[string]software.ResolvedUnit) error {
	state := map[string]uint8{}
	reachable := map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		switch state[id] {
		case 1:
			return fmt.Errorf("Homebrew dependency closure contains a cycle at %q", id)
		case 2:
			return nil
		}
		unit, ok := units[id]
		if !ok {
			return fmt.Errorf("Homebrew dependency closure references missing unit %q", id)
		}
		state[id] = 1
		reachable[id] = true
		for _, dependency := range unit.Dependencies {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		state[id] = 2
		return nil
	}
	if err := visit(root); err != nil {
		return err
	}
	if len(reachable) != len(units) {
		return errors.New("Homebrew dependency output contains a formula not reachable from the root")
	}
	return nil
}

func unitID(reference string) string {
	if strings.ContainsAny(reference, "@+") {
		// Unsafe Homebrew separators are represented by an injective escape
		// branch. No accepted formula reference contains a colon, so this cannot
		// collide with either readable branch below.
		return adapterID + ":" + scope + ":encoded:" + hex.EncodeToString([]byte(reference))
	}
	parts := strings.Split(reference, "/")
	if len(parts) == 3 && parts[0] == "homebrew" && parts[1] == "core" {
		return adapterID + ":" + scope + ":" + parts[2]
	}
	return adapterID + ":" + scope + ":tap:" + strings.Join(parts, ":")
}

func validFormulaComponent(value string) bool {
	return formulaReferencePattern.MatchString(value) && !strings.Contains(value, "/")
}

func requireJSONEnd(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if err == nil {
		return errors.New("decode Homebrew formula JSON v1: trailing JSON value")
	}
	if !errors.Is(err, io.EOF) {
		return fmt.Errorf("decode Homebrew formula JSON v1: %w", err)
	}
	return nil
}
