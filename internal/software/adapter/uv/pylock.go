package uv

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/version"
)

type pylockFile struct {
	LockVersion           string           `toml:"lock-version"`
	CreatedBy             string           `toml:"created-by"`
	RequiresPython        string           `toml:"requires-python"`
	Environments          []string         `toml:"environments"`
	Extras                []string         `toml:"extras"`
	DependencyGroups      []string         `toml:"dependency-groups"`
	DefaultGroups         []string         `toml:"default-groups"`
	Packages              []pylockPackage  `toml:"packages"`
	AttestationIdentities []map[string]any `toml:"attestation-identities"`
}

type pylockPackage struct {
	Name           string           `toml:"name"`
	Version        string           `toml:"version"`
	Index          string           `toml:"index"`
	Marker         string           `toml:"marker"`
	RequiresPython string           `toml:"requires-python"`
	Dependencies   []map[string]any `toml:"dependencies"`
	VCS            map[string]any   `toml:"vcs"`
	Directory      map[string]any   `toml:"directory"`
	Archive        map[string]any   `toml:"archive"`
	Sdist          map[string]any   `toml:"sdist"`
	Wheels         []pylockWheel    `toml:"wheels"`
}

type pylockWheel struct {
	Name   string            `toml:"name"`
	URL    string            `toml:"url"`
	Path   string            `toml:"path"`
	Size   *int64            `toml:"size"`
	Hashes map[string]string `toml:"hashes"`
}

var (
	wheelBuildTagPattern    = regexp.MustCompile(`^[0-9][0-9a-zA-Z_.]*$`)
	wheelPythonTagPattern   = regexp.MustCompile(`^(cp|py)([0-9]+)$`)
	wheelMacPlatformPattern = regexp.MustCompile(`^macosx_[0-9]+_[0-9]+_(arm64|universal2)$`)
	pythonReleasePattern    = regexp.MustCompile(`^([0-9]+)\.([0-9]+)`)
)

// translatePylock is deliberately narrower than the full PEP 751 data model.
// The command above requests one exact target from PyPI as wheels only. Any
// marker, local/VCS/archive source, sdist, or uv protocol expansion is refused
// instead of being silently projected into a weaker Temper lock.
func translatePylock(data []byte, scope string, input resolverInput, runtime pythonRuntime) (software.Candidate, error) {
	var lock pylockFile
	if err := toml.Unmarshal(data, &lock); err != nil {
		return software.Candidate{}, fmt.Errorf("decode uv pylock.toml: %w", err)
	}
	if lock.LockVersion != "1.0" || lock.CreatedBy != "uv" {
		return software.Candidate{}, fmt.Errorf("uv pylock.toml identity is %q/%q, want 1.0/uv", lock.LockVersion, lock.CreatedBy)
	}
	if lock.RequiresPython == "" {
		return software.Candidate{}, errors.New("uv pylock.toml requires-python is required")
	}
	matched, err := version.Satisfies("pep440", runtime.Version, lock.RequiresPython)
	if err != nil {
		return software.Candidate{}, fmt.Errorf("uv pylock.toml requires-python: %w", err)
	}
	if !matched {
		return software.Candidate{}, fmt.Errorf("uv pylock.toml requires-python %q excludes managed Python %s", lock.RequiresPython, runtime.Version)
	}
	if len(lock.Environments) != 0 || len(lock.Extras) != 0 || len(lock.DependencyGroups) != 0 || len(lock.DefaultGroups) != 0 || len(lock.AttestationIdentities) != 0 {
		return software.Candidate{}, errors.New("uv pylock.toml contains unsupported multi-environment, extras, groups, or attestation data")
	}
	if len(lock.Packages) == 0 {
		return software.Candidate{}, errors.New("uv pylock.toml package closure is empty")
	}

	units := make(map[string]software.ResolvedUnit, len(lock.Packages)+1)
	runtimeID := uvUnitID(scope, input.runtimeNative)
	units[runtimeID] = software.ResolvedUnit{
		Scope: scope, NativeName: input.runtimeNative, Version: runtime.Version,
		Revision: runtime.Revision, Artifacts: []software.Artifact{runtime.Artifact},
	}
	packageIDs := make(map[string]string, len(lock.Packages))
	for index, pkg := range lock.Packages {
		if !canonicalDistribution(pkg.Name) {
			return software.Candidate{}, fmt.Errorf("uv pylock.toml packages[%d] name %q is not canonical", index, pkg.Name)
		}
		if pkg.Name == input.runtimeNative {
			return software.Candidate{}, fmt.Errorf("uv pylock.toml package %q conflicts with the managed runtime", pkg.Name)
		}
		if _, exists := packageIDs[pkg.Name]; exists {
			return software.Candidate{}, fmt.Errorf("uv pylock.toml repeats package %q", pkg.Name)
		}
		if err := version.Validate("pep440", pkg.Version); err != nil {
			return software.Candidate{}, fmt.Errorf("uv pylock.toml package %q version: %w", pkg.Name, err)
		}
		if pkg.Marker != "" {
			return software.Candidate{}, fmt.Errorf("uv pylock.toml package %q retains an unevaluated environment marker", pkg.Name)
		}
		if pkg.RequiresPython != "" {
			matched, err := version.Satisfies("pep440", runtime.Version, pkg.RequiresPython)
			if err != nil || !matched {
				if err != nil {
					return software.Candidate{}, fmt.Errorf("uv pylock.toml package %q requires-python: %w", pkg.Name, err)
				}
				return software.Candidate{}, fmt.Errorf("uv pylock.toml package %q excludes managed Python %s", pkg.Name, runtime.Version)
			}
		}
		if len(pkg.Dependencies) != 0 {
			return software.Candidate{}, fmt.Errorf("uv pylock.toml package %q uses an unsupported dependency extension", pkg.Name)
		}
		if pkg.VCS != nil || pkg.Directory != nil || pkg.Archive != nil || pkg.Sdist != nil {
			return software.Candidate{}, fmt.Errorf("uv pylock.toml package %q is not a wheel-only registry package", pkg.Name)
		}
		if !productionPyPIIndex(pkg.Index) {
			return software.Candidate{}, fmt.Errorf("uv pylock.toml package %q index %q is not PyPI", pkg.Name, pkg.Index)
		}
		if len(pkg.Wheels) == 0 {
			return software.Candidate{}, fmt.Errorf("uv pylock.toml package %q has no target-compatible wheels", pkg.Name)
		}
		artifacts := make([]software.Artifact, 0, len(pkg.Wheels))
		seenLocators := map[string]bool{}
		for wheelIndex, wheel := range pkg.Wheels {
			artifact, err := translateWheel(pkg.Name, pkg.Version, runtime.Version, wheel)
			if err != nil {
				return software.Candidate{}, fmt.Errorf("uv pylock.toml package %q wheels[%d]: %w", pkg.Name, wheelIndex, err)
			}
			if seenLocators[artifact.Locator] {
				return software.Candidate{}, fmt.Errorf("uv pylock.toml package %q repeats wheel %q", pkg.Name, artifact.Locator)
			}
			seenLocators[artifact.Locator] = true
			artifacts = append(artifacts, artifact)
		}
		sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Locator < artifacts[j].Locator })
		unitID := uvUnitID(scope, pkg.Name)
		packageIDs[pkg.Name] = unitID
		units[unitID] = software.ResolvedUnit{
			Scope: scope, NativeName: pkg.Name, Version: pkg.Version,
			Dependencies: []string{runtimeID}, Artifacts: artifacts,
		}
	}

	rootID, ok := packageIDs[input.rootNative]
	if !ok {
		return software.Candidate{}, fmt.Errorf("uv pylock.toml omits root package %q", input.rootNative)
	}
	parents := make([]string, 0, len(input.catalogEdges))
	for parent := range input.catalogEdges {
		parents = append(parents, parent)
	}
	sort.Strings(parents)
	for _, parent := range parents {
		dependencies := input.catalogEdges[parent]
		if parent == input.runtimeNative {
			continue
		}
		parentID, exists := packageIDs[parent]
		if !exists {
			return software.Candidate{}, fmt.Errorf("uv pylock.toml omits catalog package %q", parent)
		}
		unit := units[parentID]
		for _, dependency := range dependencies {
			dependencyID := runtimeID
			if dependency != input.runtimeNative {
				var present bool
				dependencyID, present = packageIDs[dependency]
				if !present {
					return software.Candidate{}, fmt.Errorf("uv pylock.toml omits catalog dependency %q", dependency)
				}
			}
			unit.Dependencies = append(unit.Dependencies, dependencyID)
		}
		unit.Dependencies = sortedUnique(unit.Dependencies)
		units[parentID] = unit
	}

	// uv 0.12 emits PEP 751 as a flattened install set and leaves the optional
	// dependency extension empty. Bind every selected wheel below the root so
	// the exact closure is reachable and cannot be dropped from the lock.
	root := units[rootID]
	for _, packageID := range packageIDs {
		if packageID != rootID {
			root.Dependencies = append(root.Dependencies, packageID)
		}
	}
	root.Dependencies = sortedUnique(root.Dependencies)
	units[rootID] = root

	return software.Candidate{RootUnit: rootID, Units: units}, nil
}

func translateWheel(packageName, packageVersion, runtimeVersion string, wheel pylockWheel) (software.Artifact, error) {
	if wheel.Path != "" || wheel.URL == "" {
		return software.Artifact{}, errors.New("wheel must use a remote URL and no local path")
	}
	parsed, err := url.Parse(wheel.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "files.pythonhosted.org" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" {
		return software.Artifact{}, errors.New("wheel URL must be credential-free files.pythonhosted.org HTTPS")
	}
	filename, err := url.PathUnescape(path.Base(parsed.Path))
	if err != nil || filename == "." || filename == "/" || !strings.HasSuffix(strings.ToLower(filename), ".whl") {
		return software.Artifact{}, errors.New("wheel URL does not end in a .whl filename")
	}
	if wheel.Name != "" && wheel.Name != filename {
		return software.Artifact{}, fmt.Errorf("wheel name %q does not match URL filename %q", wheel.Name, filename)
	}
	if err := validateWheelFilename(filename, packageName, packageVersion, runtimeVersion); err != nil {
		return software.Artifact{}, err
	}
	if wheel.Size == nil || *wheel.Size <= 0 {
		return software.Artifact{}, errors.New("wheel size must be greater than zero")
	}
	if len(wheel.Hashes) != 1 || !sha256Pattern.MatchString(wheel.Hashes["sha256"]) {
		return software.Artifact{}, errors.New("wheel must contain exactly one lowercase SHA-256 hash")
	}
	return software.Artifact{Locator: wheel.URL, SHA256: wheel.Hashes["sha256"], Size: *wheel.Size}, nil
}

func productionPyPIIndex(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host == "pypi.org" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" && parsed.RawPath == "" && (parsed.Path == "/simple" || parsed.Path == "/simple/")
}

func validateWheelFilename(filename, packageName, packageVersion, runtimeVersion string) error {
	base := strings.TrimSuffix(filename, ".whl")
	parts := strings.Split(base, "-")
	if len(parts) != 5 && len(parts) != 6 {
		return fmt.Errorf("wheel filename %q does not have a supported tag shape", filename)
	}
	distribution := strings.ToLower(strings.ReplaceAll(parts[0], "_", "-"))
	if distribution != packageName {
		return fmt.Errorf("wheel filename %q does not name package %q", filename, packageName)
	}
	order, err := version.Compare("pep440", parts[1], packageVersion)
	if err != nil || order != 0 {
		return fmt.Errorf("wheel filename %q does not name package version %q", filename, packageVersion)
	}
	if len(parts) == 6 && !wheelBuildTagPattern.MatchString(parts[2]) {
		return fmt.Errorf("wheel filename %q build tag is invalid", filename)
	}
	pythonTag, abiTag, platformTag := parts[len(parts)-3], parts[len(parts)-2], parts[len(parts)-1]
	if !wheelTagsCompatible(pythonTag, abiTag, platformTag, runtimeVersion) {
		return fmt.Errorf("wheel filename %q is not compatible with CPython %s on darwin/arm64", filename, runtimeVersion)
	}
	return nil
}

func wheelTagsCompatible(pythonTags, abiTags, platformTags, runtimeVersion string) bool {
	release := pythonReleasePattern.FindStringSubmatch(runtimeVersion)
	if release == nil {
		return false
	}
	major, majorErr := strconv.Atoi(release[1])
	minor, minorErr := strconv.Atoi(release[2])
	if majorErr != nil || minorErr != nil {
		return false
	}
	for _, platformTag := range strings.Split(platformTags, ".") {
		platformAny := platformTag == "any"
		if !platformAny && !wheelMacPlatformPattern.MatchString(platformTag) {
			continue
		}
		for _, pythonTag := range strings.Split(pythonTags, ".") {
			for _, abiTag := range strings.Split(abiTags, ".") {
				if platformAny && abiTag != "none" {
					continue
				}
				if pythonABICompatible(pythonTag, abiTag, major, minor) {
					return true
				}
			}
		}
	}
	return false
}

func pythonABICompatible(pythonTag, abiTag string, runtimeMajor, runtimeMinor int) bool {
	match := wheelPythonTagPattern.FindStringSubmatch(pythonTag)
	if match == nil {
		return false
	}
	digits := match[2]
	if match[1] == "py" {
		if abiTag != "none" {
			return false
		}
		if digits == strconv.Itoa(runtimeMajor) {
			return true
		}
		return digits == strconv.Itoa(runtimeMajor)+strconv.Itoa(runtimeMinor)
	}
	if len(digits) < 2 || digits[0] != '3' || runtimeMajor != 3 {
		return false
	}
	tagMinor, err := strconv.Atoi(digits[1:])
	if err != nil {
		return false
	}
	if abiTag == "abi3" {
		return tagMinor <= runtimeMinor
	}
	return tagMinor == runtimeMinor && (abiTag == pythonTag || abiTag == "none")
}

func uvUnitID(scope, nativeName string) string {
	return adapterID + ":" + scope + ":" + nativeName
}
