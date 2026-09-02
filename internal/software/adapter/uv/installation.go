package uv

import (
	"bytes"
	"context"
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
	"reflect"
	"sort"
	"strings"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
	softwarearchive "github.com/temper-sh/temper/internal/software/archive"
	"github.com/temper-sh/temper/internal/software/installplan"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/removeplan"
	"github.com/temper-sh/temper/internal/software/version"
)

const (
	installationMarkerSchema = "temper-uv-installation/v1"
	maxRuntimeArchiveBytes   = 512 << 20
	maxRuntimeEntries        = 100_000
	maxRuntimeUnpackedBytes  = 2 << 30
)

// ArtifactReader is the uv installation transport. Resolver reads and
// installation effects remain separate; this edge receives only exact locked
// artifact locators and never performs package or version discovery.
type ArtifactReader interface {
	Open(context.Context, string) (io.ReadCloser, error)
}

// EnvironmentInstaller installs an already downloaded, exact wheelhouse into
// the locked managed Python runtime. It never resolves or downloads packages.
type EnvironmentInstaller interface {
	Install(context.Context, EnvironmentInstallRequest) error
}

type EnvironmentInstallRequest struct {
	PythonPath       string
	EnvironmentPath  string
	WheelhousePath   string
	RequirementsPath string
}

// InstallationAdapter owns isolated publication, inspection, repair, and
// removal for one uv-resolved managed-Python closure.
type InstallationAdapter struct {
	reader    ArtifactReader
	installer EnvironmentInstaller
}

var _ adapter.InstallationAdapter = (*InstallationAdapter)(nil)

func NewInstallationAdapter(reader ArtifactReader, installer EnvironmentInstaller) (*InstallationAdapter, error) {
	if reader == nil {
		return nil, errors.New("uv installation artifact reader is required")
	}
	if installer == nil {
		return nil, errors.New("uv environment installer is required")
	}
	return &InstallationAdapter{reader: reader, installer: installer}, nil
}

func (a *InstallationAdapter) Descriptor() adapter.Descriptor { return Descriptor() }

func (a *InstallationAdapter) Inspect(ctx context.Context, request adapter.InspectRequest) (map[string]installplan.ObservedUnit, error) {
	if err := validateUVTargetInstallation(request.Target, request.Installation); err != nil {
		return nil, err
	}
	if len(request.Units) == 0 {
		return nil, errors.New("uv inspection requires at least one locked unit")
	}
	byScope := map[string]map[string]softwarelock.Unit{}
	for id, unit := range request.Units {
		if byScope[unit.Scope] == nil {
			byScope[unit.Scope] = map[string]softwarelock.Unit{}
		}
		byScope[unit.Scope][id] = unit
	}
	scopes := make([]string, 0, len(byScope))
	for scope := range byScope {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	result := make(map[string]installplan.ObservedUnit, len(request.Units))
	for _, scope := range scopes {
		observed, err := a.inspectGroup(ctx, request.Target, request.Installation, byScope[scope])
		if err != nil {
			return nil, fmt.Errorf("inspect uv scope %q: %w", scope, err)
		}
		for id, unit := range observed {
			result[id] = unit
		}
	}
	return result, nil
}

func (a *InstallationAdapter) inspectGroup(ctx context.Context, target software.Target, installation installplan.Installation, units map[string]softwarelock.Unit) (map[string]installplan.ObservedUnit, error) {
	group, err := validateLockedGroup(target, installation, units)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	location := environmentLocation(installation, group.scope)
	absent := func() map[string]installplan.ObservedUnit {
		result := make(map[string]installplan.ObservedUnit, len(units))
		for id := range units {
			result[id] = installplan.ObservedUnit{InstallLocation: location}
		}
		return result
	}
	nonExact := func() map[string]installplan.ObservedUnit {
		result := make(map[string]installplan.ObservedUnit, len(units))
		for id, unit := range units {
			result[id] = installplan.ObservedUnit{
				Present: true, Adapter: adapterID, Scope: unit.Scope,
				NativeName: unit.NativeName, Version: unit.Version,
				Location: location, InstallLocation: location,
			}
		}
		return result
	}

	exists, safe, err := inspectRealUVGroupPath(installation.Root, uvGroupRoot(installation, group.scope))
	if err != nil {
		return nil, err
	}
	if !exists {
		return absent(), nil
	}
	if !safe {
		return nonExact(), nil
	}
	generation, generationName, err := currentGeneration(installation, group.scope)
	if err != nil {
		return nonExact(), nil
	}
	for _, directory := range []string{
		generation,
		filepath.Join(generation, ".temper"),
		filepath.Join(generation, ".temper", "artifacts"),
		filepath.Join(generation, "environment"),
	} {
		if !realUVDirectory(directory) {
			return nonExact(), nil
		}
	}
	markerData, err := os.ReadFile(filepath.Join(generation, ".temper", "unit.json"))
	if err != nil {
		return nonExact(), nil
	}
	marker, err := parseInstallationMarker(markerData)
	if err != nil || !markerMatchesLockedGroup(marker, group) {
		return nonExact(), nil
	}
	if err := verifyInstalledArtifacts(ctx, generation, marker.Artifacts, group); err != nil {
		return nonExact(), nil
	}
	entries, err := scanEnvironment(ctx, filepath.Join(generation, "environment"))
	if err != nil || !reflect.DeepEqual(entries, marker.Entries) {
		return nonExact(), nil
	}
	if err := exactUVGroupLayout(uvGroupRoot(installation, group.scope), generationName); err != nil {
		return nonExact(), nil
	}

	result := make(map[string]installplan.ObservedUnit, len(units))
	for id, unit := range units {
		result[id] = installplan.ObservedUnit{
			Present: true, Adapter: unit.Adapter, Scope: unit.Scope,
			NativeName: unit.NativeName, Version: unit.Version, Revision: unit.Revision,
			Dependencies: append([]string(nil), unit.Dependencies...),
			Artifacts:    append([]software.Artifact(nil), unit.Artifacts...),
			Location:     location, InstallLocation: location,
		}
	}
	return result, nil
}

func (a *InstallationAdapter) Install(ctx context.Context, request adapter.InstallRequest) error {
	group, change, err := validateUVInstallRequest(request)
	if err != nil {
		return err
	}
	if !change {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	scopeRoot := uvGroupRoot(request.Installation, group.scope)
	if err := prepareUVScope(request.Installation, scopeRoot); err != nil {
		return err
	}
	currentPath := filepath.Join(scopeRoot, "current")
	if info, statErr := os.Lstat(currentPath); statErr == nil && info.Mode()&os.ModeSymlink == 0 {
		return errors.New("uv current pointer exists and is not a symbolic link")
	} else if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
		return fmt.Errorf("inspect uv current pointer: %w", statErr)
	}
	generationsPath := filepath.Join(scopeRoot, "generations")
	if err := ensureUVChildDirectory(scopeRoot, generationsPath); err != nil {
		return err
	}
	generation, err := os.MkdirTemp(generationsPath, "generation-")
	if err != nil {
		return fmt.Errorf("create uv generation: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(generation)
		}
	}()

	metadataPath := filepath.Join(generation, ".temper")
	wheelhousePath := filepath.Join(metadataPath, "artifacts")
	environmentPath := filepath.Join(generation, "environment")
	for _, directory := range []string{metadataPath, wheelhousePath, environmentPath} {
		if err := os.Mkdir(directory, 0o755); err != nil {
			return fmt.Errorf("create uv generation directory: %w", err)
		}
	}
	installedArtifacts, runtimeArchive, err := a.downloadLockedArtifacts(ctx, generation, group)
	if err != nil {
		return err
	}
	runtimeSpec := softwarearchive.TarGzSpec{
		Root: "python", MaxEntries: maxRuntimeEntries, MaxUnpackedBytes: maxRuntimeUnpackedBytes,
		Label: "managed Python archive",
	}
	runtimeEntries, err := softwarearchive.InspectTarGz(ctx, runtimeArchive, runtimeSpec)
	if err != nil {
		return fmt.Errorf("inspect managed Python archive: %w", err)
	}
	if err := softwarearchive.ExtractTarGz(ctx, runtimeArchive, environmentPath, runtimeSpec, runtimeEntries); err != nil {
		return fmt.Errorf("extract managed Python: %w", err)
	}
	requirementsData := lockedRequirements(group)
	requirementsPath := filepath.Join(metadataPath, "requirements.txt")
	if err := writeUVFile(requirementsPath, requirementsData, 0o644); err != nil {
		return err
	}
	pythonPath := filepath.Join(environmentPath, "bin", "python3")
	if !regularUVExecutable(pythonPath) {
		return errors.New("managed Python archive has no executable bin/python3")
	}
	if err := a.installer.Install(ctx, EnvironmentInstallRequest{
		PythonPath: pythonPath, EnvironmentPath: environmentPath,
		WheelhousePath: wheelhousePath, RequirementsPath: requirementsPath,
	}); err != nil {
		return fmt.Errorf("install exact wheel closure: %w", err)
	}
	entries, err := scanEnvironment(ctx, environmentPath)
	if err != nil {
		return fmt.Errorf("scan installed uv environment: %w", err)
	}
	marker := installationMarker{
		Schema: installationMarkerSchema, Adapter: adapterID, Scope: group.scope,
		Units: group.units, Artifacts: installedArtifacts, Entries: entries,
	}
	markerData, err := marshalInstallationMarker(marker)
	if err != nil {
		return err
	}
	if err := writeUVFile(filepath.Join(metadataPath, "unit.json"), markerData, 0o644); err != nil {
		return err
	}
	if err := syncUVTree(generation); err != nil {
		return fmt.Errorf("sync uv generation: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	generationName := filepath.Base(generation)
	temporaryPointer := filepath.Join(scopeRoot, ".current-"+generationName)
	if err := os.Symlink(filepath.ToSlash(filepath.Join("generations", generationName)), temporaryPointer); err != nil {
		return fmt.Errorf("stage uv current pointer: %w", err)
	}
	if err := os.Rename(temporaryPointer, currentPath); err != nil {
		_ = os.Remove(temporaryPointer)
		return fmt.Errorf("commit uv current pointer: %w", err)
	}
	committed = true
	if err := syncUVDirectory(scopeRoot); err != nil {
		return fmt.Errorf("sync committed uv pointer: %w", err)
	}
	if err := cleanUVGenerations(scopeRoot, generationName); err != nil {
		return fmt.Errorf("clean previous uv generation: %w", err)
	}
	return nil
}

func (a *InstallationAdapter) Remove(ctx context.Context, request adapter.RemoveRequest) error {
	group, execute, err := validateUVRemoveRequest(request)
	if err != nil {
		return err
	}
	if !execute {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	target := uvGroupRoot(request.Installation, group.scope)
	exists, safe, err := inspectRealUVGroupPath(request.Installation.Root, target)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if !safe {
		return errors.New("uv group removal target is not a real directory")
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("remove uv group: %w", err)
	}
	return syncUVDirectory(filepath.Dir(target))
}

type lockedGroup struct {
	scope          string
	runtimeID      string
	runtimeVersion string
	units          []markerUnit
	artifacts      []lockedArtifact
	packages       []markerUnit
}

type markerUnit struct {
	ID           string              `json:"id"`
	NativeName   string              `json:"native_name"`
	Version      string              `json:"version"`
	Revision     string              `json:"revision,omitempty"`
	Dependencies []string            `json:"dependencies"`
	Artifacts    []software.Artifact `json:"artifacts"`
}

type lockedArtifact struct {
	unitID   string
	artifact software.Artifact
	path     string
	runtime  bool
}

func validateLockedGroup(target software.Target, installation installplan.Installation, units map[string]softwarelock.Unit) (lockedGroup, error) {
	if err := validateUVTargetInstallation(target, installation); err != nil {
		return lockedGroup{}, err
	}
	if len(units) < 2 {
		return lockedGroup{}, errors.New("uv group requires one managed Python runtime and at least one wheel package")
	}
	ids := make([]string, 0, len(units))
	for id := range units {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	group := lockedGroup{}
	seenNative := map[string]bool{}
	for _, id := range ids {
		unit := units[id]
		if unit.Adapter != adapterID || !distributionPattern.MatchString(unit.Scope) || !canonicalDistribution(unit.NativeName) || id != uvUnitID(unit.Scope, unit.NativeName) {
			return lockedGroup{}, fmt.Errorf("uv unit %q has an invalid adapter, scope, or native identity", id)
		}
		if group.scope == "" {
			group.scope = unit.Scope
		} else if group.scope != unit.Scope {
			return lockedGroup{}, errors.New("uv installation group crosses isolated scopes")
		}
		if seenNative[unit.NativeName] {
			return lockedGroup{}, fmt.Errorf("uv group repeats native package %q", unit.NativeName)
		}
		seenNative[unit.NativeName] = true
		if err := version.Validate("pep440", unit.Version); err != nil {
			return lockedGroup{}, fmt.Errorf("uv unit %q version: %w", id, err)
		}
		dependencies := append([]string(nil), unit.Dependencies...)
		sort.Strings(dependencies)
		artifacts := append([]software.Artifact(nil), unit.Artifacts...)
		sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Locator < artifacts[j].Locator })
		marker := markerUnit{ID: id, NativeName: unit.NativeName, Version: unit.Version, Revision: unit.Revision, Dependencies: dependencies, Artifacts: artifacts}
		group.units = append(group.units, marker)
		if unit.NativeName == "cpython" {
			if group.runtimeID != "" {
				return lockedGroup{}, errors.New("uv group contains more than one managed Python runtime")
			}
			if err := validateRuntimeUnit(id, unit); err != nil {
				return lockedGroup{}, err
			}
			group.runtimeID, group.runtimeVersion = id, unit.Version
		} else {
			group.packages = append(group.packages, marker)
		}
	}
	if group.runtimeID == "" {
		return lockedGroup{}, errors.New("uv group has no managed CPython runtime")
	}
	for _, id := range ids {
		unit := units[id]
		for _, dependency := range unit.Dependencies {
			if _, ok := units[dependency]; !ok {
				return lockedGroup{}, fmt.Errorf("uv unit %q dependency %q leaves its isolated group", id, dependency)
			}
		}
		if id != group.runtimeID {
			if len(unit.Artifacts) == 0 {
				return lockedGroup{}, fmt.Errorf("uv wheel unit %q has no artifacts", id)
			}
			if !containsDependency(unit.Dependencies, group.runtimeID) {
				return lockedGroup{}, fmt.Errorf("uv wheel unit %q does not depend on managed Python", id)
			}
			for _, artifact := range unit.Artifacts {
				if err := validateLockedWheel(unit, group.runtimeVersion, artifact); err != nil {
					return lockedGroup{}, fmt.Errorf("uv wheel unit %q: %w", id, err)
				}
			}
		}
	}

	seenArtifactPaths := map[string]bool{}
	for _, marker := range group.units {
		for _, artifact := range marker.Artifacts {
			runtime := marker.ID == group.runtimeID
			name := "managed-python.tar.gz"
			if !runtime {
				parsed, _ := url.Parse(artifact.Locator)
				name, _ = url.PathUnescape(path.Base(parsed.Path))
			}
			relative := filepath.ToSlash(filepath.Join(".temper", "artifacts", name))
			if seenArtifactPaths[relative] {
				return lockedGroup{}, fmt.Errorf("uv group repeats artifact filename %q", name)
			}
			seenArtifactPaths[relative] = true
			group.artifacts = append(group.artifacts, lockedArtifact{unitID: marker.ID, artifact: artifact, path: relative, runtime: runtime})
		}
	}
	return group, nil
}

func validateRuntimeUnit(id string, unit softwarelock.Unit) error {
	if len(unit.Dependencies) != 0 || len(unit.Artifacts) != 1 || unit.Revision == "" {
		return fmt.Errorf("uv managed Python unit %q requires one runtime archive, a revision, and no dependencies", id)
	}
	if !strings.HasPrefix(unit.Revision, "python-build-standalone:") || len(strings.TrimPrefix(unit.Revision, "python-build-standalone:")) != 8 {
		return fmt.Errorf("uv managed Python unit %q revision is invalid", id)
	}
	artifact := unit.Artifacts[0]
	parsed, err := url.Parse(artifact.Locator)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Host != "github.com" && parsed.Host != "releases.astral.sh") || !strings.HasSuffix(parsed.Path, "-install_only_stripped.tar.gz") {
		return fmt.Errorf("uv managed Python unit %q archive locator is invalid", id)
	}
	build := strings.TrimPrefix(unit.Revision, "python-build-standalone:")
	wantName := "cpython-" + unit.Version + "+" + build + "-aarch64-apple-darwin-install_only_stripped.tar.gz"
	if !buildPattern.MatchString(build) || path.Base(parsed.Path) != wantName || !strings.Contains(parsed.Path, "/python-build-standalone/releases/download/"+build+"/") || !sha256Pattern.MatchString(artifact.SHA256) || artifact.Size < 0 || artifact.Size > maxRuntimeArchiveBytes || artifact.Format != "" || artifact.ArchiveRoot != "" || artifact.UnpackedSize != 0 || artifact.InstalledEntries != 0 {
		return fmt.Errorf("uv managed Python unit %q archive identity is invalid", id)
	}
	return nil
}

func validateLockedWheel(unit softwarelock.Unit, runtimeVersion string, artifact software.Artifact) error {
	parsed, err := url.Parse(artifact.Locator)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "files.pythonhosted.org" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" {
		return errors.New("wheel locator must be credential-free files.pythonhosted.org HTTPS")
	}
	filename, err := url.PathUnescape(path.Base(parsed.Path))
	if err != nil {
		return errors.New("wheel filename is not valid URL text")
	}
	if err := validateWheelFilename(filename, unit.NativeName, unit.Version, runtimeVersion); err != nil {
		return err
	}
	if !sha256Pattern.MatchString(artifact.SHA256) || artifact.Size <= 0 || artifact.Format != "" || artifact.ArchiveRoot != "" || artifact.UnpackedSize != 0 || artifact.InstalledEntries != 0 {
		return errors.New("wheel artifact identity is incomplete")
	}
	return nil
}

func validateUVInstallRequest(request adapter.InstallRequest) (lockedGroup, bool, error) {
	group, err := validateLockedGroup(request.Target, request.Installation, request.Units)
	if err != nil {
		return lockedGroup{}, false, err
	}
	if request.Group.ID != adapterID+":"+group.scope || request.Group.Adapter != adapterID || request.Group.Scope != group.scope || request.Group.EffectModel != installplan.EffectIsolated || len(request.Group.Units) != len(request.Units) {
		return lockedGroup{}, false, errors.New("uv install requires one complete isolated adapter/scope group")
	}
	seen := map[string]bool{}
	change := false
	preserves := 0
	location := environmentLocation(request.Installation, group.scope)
	for _, planned := range request.Group.Units {
		if _, ok := request.Units[planned.ID]; !ok || seen[planned.ID] {
			return lockedGroup{}, false, errors.New("uv install plan differs from its locked group")
		}
		seen[planned.ID] = true
		if planned.Location != location {
			return lockedGroup{}, false, errors.New("uv install location differs from its isolated group destination")
		}
		switch planned.Action {
		case installplan.ActionPreserve:
			preserves++
		case installplan.ActionAdd, installplan.ActionReplace:
			change = true
		default:
			return lockedGroup{}, false, errors.New("uv install action is invalid")
		}
	}
	if change && preserves != 0 {
		return lockedGroup{}, false, errors.New("uv mutable install must replace the complete isolated group")
	}
	return group, change, nil
}

func validateUVRemoveRequest(request adapter.RemoveRequest) (lockedGroup, bool, error) {
	group, err := validateLockedGroup(request.Target, request.Installation, request.Units)
	if err != nil {
		return lockedGroup{}, false, err
	}
	if request.Group.ID != adapterID+":"+group.scope || request.Group.Adapter != adapterID || request.Group.Scope != group.scope || request.Group.EffectModel != installplan.EffectIsolated || len(request.Group.Units) != len(request.Units) {
		return lockedGroup{}, false, errors.New("uv removal requires one complete isolated adapter/scope group")
	}
	execute := false
	preserves := 0
	seen := map[string]bool{}
	location := environmentLocation(request.Installation, group.scope)
	for _, planned := range request.Group.Units {
		if _, ok := request.Units[planned.ID]; !ok || seen[planned.ID] || planned.Location != location {
			return lockedGroup{}, false, errors.New("uv removal plan differs from its locked group")
		}
		seen[planned.ID] = true
		if planned.Action != removeplan.ActionPreserve && planned.Action != removeplan.ActionRemove {
			return lockedGroup{}, false, errors.New("uv removal action is invalid")
		}
		if planned.Action == removeplan.ActionPreserve {
			preserves++
		}
		if planned.Execute && planned.Action != removeplan.ActionRemove {
			return lockedGroup{}, false, errors.New("uv removal cannot execute a preserve action")
		}
		execute = execute || planned.Execute
	}
	if execute && preserves != 0 {
		return lockedGroup{}, false, errors.New("uv removal cannot delete a partially preserved isolated group")
	}
	return group, execute, nil
}

func (a *InstallationAdapter) downloadLockedArtifacts(ctx context.Context, generation string, group lockedGroup) ([]installedArtifact, string, error) {
	records := make([]installedArtifact, 0, len(group.artifacts))
	runtimePath := ""
	for _, locked := range group.artifacts {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		reader, err := a.reader.Open(ctx, locked.artifact.Locator)
		if err != nil {
			return nil, "", fmt.Errorf("open uv artifact for %q: %w", locked.unitID, err)
		}
		limit := locked.artifact.Size
		if locked.runtime {
			limit = maxRuntimeArchiveBytes
		}
		path := filepath.Join(generation, filepath.FromSlash(locked.path))
		size, writeErr := writeHashedUVArtifact(ctx, path, reader, limit, locked.artifact.Size, locked.artifact.SHA256)
		closeErr := reader.Close()
		if writeErr != nil {
			return nil, "", fmt.Errorf("download uv artifact for %q: %w", locked.unitID, writeErr)
		}
		if closeErr != nil {
			return nil, "", fmt.Errorf("close uv artifact for %q: %w", locked.unitID, closeErr)
		}
		records = append(records, installedArtifact{UnitID: locked.unitID, Locator: locked.artifact.Locator, Path: locked.path, Size: size})
		if locked.runtime {
			runtimePath = path
		}
	}
	return records, runtimePath, nil
}

func writeHashedUVArtifact(ctx context.Context, destination string, source io.Reader, limit, exactSize int64, expectedHash string) (int64, error) {
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 0, err
	}
	hash := sha256.New()
	written, copyErr := copyUVWithContext(ctx, io.MultiWriter(file, hash), io.LimitReader(source, limit+1))
	if copyErr == nil && written > limit {
		copyErr = fmt.Errorf("artifact exceeds %d bytes", limit)
	}
	if copyErr == nil && exactSize > 0 && written != exactSize {
		copyErr = fmt.Errorf("artifact size is %d, want %d", written, exactSize)
	}
	if copyErr == nil {
		actual := hex.EncodeToString(hash.Sum(nil))
		if actual != expectedHash {
			copyErr = fmt.Errorf("artifact sha256 is %s, want %s", actual, expectedHash)
		}
	}
	if copyErr == nil {
		copyErr = file.Sync()
	}
	closeErr := file.Close()
	if copyErr != nil {
		return 0, copyErr
	}
	if closeErr != nil {
		return 0, closeErr
	}
	return written, nil
}

func lockedRequirements(group lockedGroup) []byte {
	var output strings.Builder
	for _, unit := range group.packages {
		fmt.Fprintf(&output, "%s==%s", unit.NativeName, unit.Version)
		for _, artifact := range unit.Artifacts {
			fmt.Fprintf(&output, " --hash=sha256:%s", artifact.SHA256)
		}
		output.WriteByte('\n')
	}
	return []byte(output.String())
}

type installationMarker struct {
	Schema    string              `json:"schema"`
	Adapter   string              `json:"adapter"`
	Scope     string              `json:"scope"`
	Units     []markerUnit        `json:"units"`
	Artifacts []installedArtifact `json:"artifacts"`
	Entries   []environmentEntry  `json:"entries"`
}

type installedArtifact struct {
	UnitID  string `json:"unit_id"`
	Locator string `json:"locator"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
}

func marshalInstallationMarker(marker installationMarker) ([]byte, error) {
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode uv installation marker: %w", err)
	}
	return append(data, '\n'), nil
}

func parseInstallationMarker(data []byte) (installationMarker, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var marker installationMarker
	if err := decoder.Decode(&marker); err != nil {
		return installationMarker{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return installationMarker{}, errors.New("uv installation marker has trailing data")
	}
	canonical, err := marshalInstallationMarker(marker)
	if err != nil || !bytes.Equal(canonical, data) {
		return installationMarker{}, errors.New("uv installation marker is not canonical")
	}
	return marker, nil
}

func markerMatchesLockedGroup(marker installationMarker, group lockedGroup) bool {
	if marker.Schema != installationMarkerSchema || marker.Adapter != adapterID || marker.Scope != group.scope || !reflect.DeepEqual(marker.Units, group.units) || len(marker.Entries) == 0 || len(marker.Artifacts) != len(group.artifacts) {
		return false
	}
	for index, installed := range marker.Artifacts {
		locked := group.artifacts[index]
		if installed.UnitID != locked.unitID || installed.Locator != locked.artifact.Locator || installed.Path != locked.path || installed.Size <= 0 || (locked.artifact.Size > 0 && installed.Size != locked.artifact.Size) {
			return false
		}
	}
	return true
}

func verifyInstalledArtifacts(ctx context.Context, generation string, installed []installedArtifact, group lockedGroup) error {
	wanted := map[string]bool{}
	for index, record := range installed {
		if err := ctx.Err(); err != nil {
			return err
		}
		locked := group.artifacts[index]
		path := filepath.Join(generation, filepath.FromSlash(record.Path))
		if err := verifyRegularUVFile(ctx, path, record.Size, locked.artifact.SHA256); err != nil {
			return err
		}
		wanted[filepath.Base(path)] = true
	}
	entries, err := os.ReadDir(filepath.Join(generation, ".temper", "artifacts"))
	if err != nil || len(entries) != len(wanted) {
		return errors.New("uv artifact cache has an unexpected shape")
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !wanted[entry.Name()] {
			return errors.New("uv artifact cache has an unexpected entry")
		}
	}
	return nil
}

func verifyRegularUVFile(ctx context.Context, path string, size int64, expectedHash string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != size {
		return errors.New("uv artifact is absent or has changed shape")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, copyErr := copyUVWithContext(ctx, hash, file)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if hex.EncodeToString(hash.Sum(nil)) != expectedHash {
		return errors.New("uv artifact content differs from the lock")
	}
	return nil
}

func validateUVTargetInstallation(target software.Target, installation installplan.Installation) error {
	if err := target.Validate(); err != nil {
		return err
	}
	if target.OS != "darwin" || target.Arch != "arm64" {
		return fmt.Errorf("uv installation adapter does not support target %s/%s", target.OS, target.Arch)
	}
	if !distributionPattern.MatchString(installation.ID) || !filepath.IsAbs(installation.Root) || filepath.Clean(installation.Root) != installation.Root || filepath.Dir(installation.Root) == installation.Root {
		return errors.New("uv installation requires a stable id and narrow absolute clean root")
	}
	return nil
}

func uvGroupRoot(installation installplan.Installation, scope string) string {
	return filepath.Join(installplan.InstallationRoot(installation), adapterID, scope)
}

func environmentLocation(installation installplan.Installation, scope string) string {
	return filepath.Join(uvGroupRoot(installation, scope), "current", "environment")
}

func containsDependency(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
