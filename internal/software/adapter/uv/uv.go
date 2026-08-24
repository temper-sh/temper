// Package uv translates uv's version-matched managed-Python metadata and
// standardized PEP 751 lock output into provider-neutral exact candidates.
// Command execution and HTTPS reads are injected at the adapter boundary.
package uv

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/catalog"
	"github.com/temper-sh/temper/internal/software/version"
)

const (
	adapterID         = "uv"
	method            = "python-environment"
	pythonPlatform    = "aarch64-apple-darwin"
	productionIndex   = "https://pypi.org/simple"
	maxRequirementLen = 64 * 1024
)

var (
	uvVersionPattern    = regexp.MustCompile(`^uv (0\.12\.[0-9]+)(?: \([^\r\n]+\))?\r?\n?$`)
	distributionPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

type Command struct {
	Executable string
	Args       []string
	Stdin      []byte
}

type Output struct {
	Stdout []byte
	Stderr []byte
}

type CommandRunner interface {
	Run(context.Context, Command) (Output, error)
}

type MetadataReader interface {
	Read(context.Context, string, int64) ([]byte, error)
}

type Options struct {
	Executable string
	Timeout    time.Duration
}

type Resolver struct {
	runner     CommandRunner
	metadata   MetadataReader
	executable string
	timeout    time.Duration
}

func New(runner CommandRunner, metadata MetadataReader, options Options) (*Resolver, error) {
	if runner == nil {
		return nil, errors.New("uv command runner is required")
	}
	if metadata == nil {
		return nil, errors.New("uv Python metadata reader is required")
	}
	if options.Executable == "" || !filepath.IsAbs(options.Executable) {
		return nil, errors.New("uv executable must be an absolute path")
	}
	if options.Timeout <= 0 {
		return nil, errors.New("uv candidate timeout must be greater than zero")
	}
	return &Resolver{runner: runner, metadata: metadata, executable: options.Executable, timeout: options.Timeout}, nil
}

func Descriptor() adapter.Descriptor {
	return adapter.Descriptor{
		ID: adapterID, Method: method, Protocol: catalog.AdapterProtocolV1,
		EffectModel: "isolated",
		Targets:     []software.Target{{OS: "darwin", Arch: "arm64"}},
	}
}

func (r *Resolver) Descriptor() adapter.Descriptor { return Descriptor() }

// Candidates performs one bounded provider read. uv resolves the highest
// exact target closure allowed by the catalog constraints; Temper's shared
// pure selector subsequently revalidates the returned closure against policy.
func (r *Resolver) Candidates(ctx context.Context, request adapter.ResolveRequest) ([]software.Candidate, error) {
	input, err := buildInput(request)
	if err != nil {
		return nil, err
	}
	readContext, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	versionOutput, err := r.run(readContext, Command{Executable: r.executable, Args: []string{"--version"}})
	if err != nil {
		return nil, fmt.Errorf("read uv version: %w", err)
	}
	uvVersion, err := parseUVVersion(versionOutput.Stdout)
	if err != nil {
		return nil, err
	}
	metadataLocator := pythonMetadataLocator(uvVersion)
	metadataData, err := r.metadata.Read(readContext, metadataLocator, MaxPythonMetadataBytes)
	if err != nil {
		return nil, fmt.Errorf("read uv %s managed-Python metadata: %w", uvVersion, err)
	}
	runtime, err := selectPython(metadataData, input.runtimeRecipe, input.runtimeConstraints, request.Target)
	if err != nil {
		return nil, err
	}

	arguments := []string{
		"pip", "compile", "-",
		"--format", "pylock.toml",
		"--python-version", runtime.Version,
		"--python-platform", pythonPlatform,
		"--default-index", productionIndex,
		"--index-strategy", "first-index",
		"--resolution", "highest",
		"--prerelease", "disallow",
		"--only-binary", ":all:",
		"--no-build",
		"--no-config",
		"--no-cache",
		"--no-python-downloads",
		"--no-header",
	}
	for _, name := range input.prereleasePackages {
		arguments = append(arguments, "--prerelease-package", name+"=allow")
	}
	lockOutput, err := r.run(readContext, Command{Executable: r.executable, Args: arguments, Stdin: input.requirements})
	if err != nil {
		return nil, fmt.Errorf("resolve uv package closure: %w", err)
	}
	candidate, err := translatePylock(lockOutput.Stdout, request.Package, input, runtime)
	if err != nil {
		return nil, err
	}
	return []software.Candidate{candidate}, nil
}

func (r *Resolver) run(ctx context.Context, command Command) (Output, error) {
	if err := ctx.Err(); err != nil {
		return Output{}, err
	}
	output, err := r.runner.Run(ctx, command)
	if err != nil {
		return Output{}, err
	}
	if err := ctx.Err(); err != nil {
		return Output{}, err
	}
	return output, nil
}

type resolverInput struct {
	requirements       []byte
	rootNative         string
	runtimeNative      string
	runtimeRecipe      catalog.Recipe
	runtimeConstraints []string
	catalogEdges       map[string][]string
	prereleasePackages []string
}

func buildInput(request adapter.ResolveRequest) (resolverInput, error) {
	if request.Package == "" {
		return resolverInput{}, errors.New("uv candidate package is required")
	}
	if err := request.Target.Validate(); err != nil {
		return resolverInput{}, fmt.Errorf("uv candidate target: %w", err)
	}
	if request.Target.OS != "darwin" || request.Target.Arch != "arm64" {
		return resolverInput{}, fmt.Errorf("uv adapter does not support target %s/%s", request.Target.OS, request.Target.Arch)
	}
	if err := request.Supply.Validate(); err != nil {
		return resolverInput{}, err
	}
	rootPackage, ok := request.Supply.Packages[request.Package]
	if !ok {
		return resolverInput{}, fmt.Errorf("uv catalog package %q does not exist", request.Package)
	}
	rootRecipe, ok := rootPackage.Recipes[adapterID]
	if !ok || rootRecipe.Method != method || rootRecipe.Source.Kind != "python-index" {
		return resolverInput{}, errors.New("uv adapter requires a python-environment Python-index root recipe")
	}
	if !reflect.DeepEqual(request.Recipe, rootRecipe) {
		return resolverInput{}, errors.New("uv request recipe differs from the supplied catalog document")
	}

	type collected struct {
		recipe      catalog.Recipe
		constraints []string
	}
	packages := map[string]*collected{}
	edges := map[string][]string{}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var runtimePackage string
	var walk func(string, string) error
	walk = func(packageID, parentConstraint string) error {
		pkg := request.Supply.Packages[packageID]
		recipe, ok := pkg.Recipes[adapterID]
		if !ok {
			return fmt.Errorf("uv catalog package %q has no uv recipe", packageID)
		}
		if recipe.Method != method || recipe.VersionScheme != "pep440" {
			return fmt.Errorf("uv catalog package %q must use python-environment with PEP 440 versions", packageID)
		}
		native := recipe.Source.NativeName()
		if !canonicalDistribution(native) {
			return fmt.Errorf("uv catalog package %q native name %q is not canonical", packageID, native)
		}
		entry := packages[native]
		if entry == nil {
			entry = &collected{recipe: recipe}
			packages[native] = entry
		} else if !reflect.DeepEqual(entry.recipe, recipe) {
			return fmt.Errorf("uv native package %q has conflicting catalog recipes", native)
		}
		if parentConstraint != "" {
			entry.constraints = append(entry.constraints, parentConstraint)
		}
		if visited[packageID] {
			return nil
		}
		if visiting[packageID] {
			return fmt.Errorf("uv catalog dependency cycle at %q", packageID)
		}
		visiting[packageID] = true
		switch recipe.Source.Kind {
		case "python-runtime":
			if runtimePackage != "" && runtimePackage != packageID {
				return errors.New("uv catalog closure contains more than one Python runtime")
			}
			runtimePackage = packageID
		case "python-index":
			if recipe.Source.Index != "pypi" {
				return fmt.Errorf("uv catalog package %q index %q is not supported", packageID, recipe.Source.Index)
			}
		default:
			return fmt.Errorf("uv catalog package %q has unsupported source kind %q", packageID, recipe.Source.Kind)
		}
		for _, dependency := range recipe.Dependencies {
			dependencyRecipe := request.Supply.Packages[dependency.Package].Recipes[adapterID]
			dependencyNative := dependencyRecipe.Source.NativeName()
			edges[native] = append(edges[native], dependencyNative)
			if err := walk(dependency.Package, dependency.Constraint); err != nil {
				return err
			}
		}
		visiting[packageID] = false
		visited[packageID] = true
		return nil
	}
	if err := walk(request.Package, ""); err != nil {
		return resolverInput{}, err
	}
	if runtimePackage == "" {
		return resolverInput{}, errors.New("uv catalog closure must contain exactly one Python runtime dependency")
	}

	runtimeRecipe := request.Supply.Packages[runtimePackage].Recipes[adapterID]
	runtimeNative := runtimeRecipe.Source.NativeName()
	runtimeEntry := packages[runtimeNative]
	runtimeConstraints := append([]string(nil), runtimeEntry.constraints...)
	policyConstraint, err := recipeConstraint(runtimeRecipe)
	if err != nil {
		return resolverInput{}, err
	}
	if policyConstraint != "" {
		runtimeConstraints = append(runtimeConstraints, policyConstraint)
	}
	for _, excluded := range runtimeRecipe.Exclude {
		runtimeConstraints = append(runtimeConstraints, "!="+excluded)
	}

	names := make([]string, 0, len(packages)-1)
	for name := range packages {
		if name != runtimeNative {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var requirements strings.Builder
	var prereleasePackages []string
	for _, name := range names {
		entry := packages[name]
		constraints := append([]string(nil), entry.constraints...)
		policyConstraint, err := recipeConstraint(entry.recipe)
		if err != nil {
			return resolverInput{}, err
		}
		if policyConstraint != "" {
			constraints = append(constraints, policyConstraint)
		}
		for _, excluded := range entry.recipe.Exclude {
			constraints = append(constraints, "!="+excluded)
		}
		constraints = sortedUnique(constraints)
		requirements.WriteString(name)
		if len(constraints) > 0 {
			requirements.WriteString(strings.Join(constraints, ","))
		}
		requirements.WriteByte('\n')
		allowed, err := recipeAllowsPrerelease(entry.recipe)
		if err != nil {
			return resolverInput{}, err
		}
		if allowed {
			prereleasePackages = append(prereleasePackages, name)
		}
	}
	if requirements.Len() == 0 || requirements.Len() > maxRequirementLen {
		return resolverInput{}, errors.New("uv generated requirements are empty or exceed the size limit")
	}
	for name, dependencies := range edges {
		edges[name] = sortedUnique(dependencies)
	}
	return resolverInput{
		requirements: []byte(requirements.String()), rootNative: rootRecipe.Source.NativeName(),
		runtimeNative: runtimeNative, runtimeRecipe: runtimeRecipe, runtimeConstraints: sortedUnique(runtimeConstraints),
		catalogEdges: edges, prereleasePackages: prereleasePackages,
	}, nil
}

func parseUVVersion(data []byte) (string, error) {
	matches := uvVersionPattern.FindSubmatch(data)
	if matches == nil {
		return "", errors.New("uv version output is not a supported stable 0.12.x release")
	}
	return string(matches[1]), nil
}

func pythonMetadataLocator(uvVersion string) string {
	return "https://raw.githubusercontent.com/astral-sh/uv/" + uvVersion + "/crates/uv-python/download-metadata.json"
}

func recipeConstraint(recipe catalog.Recipe) (string, error) {
	switch recipe.Selection.Policy {
	case "latest":
		if recipe.Selection.MinimumCompatible == "" {
			return "", nil
		}
		return ">=" + recipe.Selection.MinimumCompatible, nil
	case "range":
		return recipe.Selection.Constraint, nil
	case "exact":
		return "==" + recipe.Selection.Exact, nil
	default:
		return "", fmt.Errorf("uv recipe selection policy %q is not supported", recipe.Selection.Policy)
	}
}

func recipeAllowsPrerelease(recipe catalog.Recipe) (bool, error) {
	if recipe.Selection.Policy == "exact" {
		return version.IsPrerelease("pep440", recipe.Selection.Exact)
	}
	if recipe.Selection.MinimumCompatible != "" {
		allowed, err := version.IsPrerelease("pep440", recipe.Selection.MinimumCompatible)
		if err != nil || allowed {
			return allowed, err
		}
	}
	if recipe.Selection.Constraint != "" {
		return version.ConstraintAllowsPrerelease("pep440", recipe.Selection.Constraint)
	}
	return false, nil
}

func canonicalDistribution(value string) bool {
	return distributionPattern.MatchString(value)
}

func sortedUnique(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
