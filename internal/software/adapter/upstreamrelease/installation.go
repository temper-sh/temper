package upstreamrelease

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/installplan"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/removeplan"
)

const markerSchema = "temper-upstream-release-installation/v1"

// InstallationAdapter owns exact archive transport, isolated publication,
// inspection, and removal. It never chooses a version or target asset.
type InstallationAdapter struct{ reader ArtifactReader }

var _ adapter.InstallationAdapter = (*InstallationAdapter)(nil)

func NewInstallationAdapter(reader ArtifactReader) (*InstallationAdapter, error) {
	if reader == nil {
		return nil, errors.New("release artifact reader is required")
	}
	return &InstallationAdapter{reader: reader}, nil
}

func (a *InstallationAdapter) Descriptor() adapter.Descriptor { return Descriptor() }

func (a *InstallationAdapter) Inspect(ctx context.Context, request adapter.InspectRequest) (map[string]installplan.ObservedUnit, error) {
	if err := validateTargetInstallation(request.Target, request.Installation); err != nil {
		return nil, err
	}
	result := make(map[string]installplan.ObservedUnit, len(request.Units))
	seenScopes := map[string]string{}
	for _, unitID := range sortedUnitIDs(request.Units) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		unit := request.Units[unitID]
		if prior := seenScopes[unit.Scope]; prior != "" {
			return nil, fmt.Errorf("release units %q and %q share isolated scope %q", prior, unitID, unit.Scope)
		}
		seenScopes[unit.Scope] = unitID
		artifact, err := validateLockedUnit(unitID, unit)
		if err != nil {
			return nil, err
		}
		location := installLocation(request.Installation, unit.Scope)
		observed, err := inspectUnit(ctx, unitID, unit, artifact, location, request.Installation.Root)
		if err != nil {
			return nil, fmt.Errorf("inspect release unit %q: %w", unitID, err)
		}
		result[unitID] = observed
	}
	return result, nil
}

func (a *InstallationAdapter) Install(ctx context.Context, request adapter.InstallRequest) error {
	unitID, unit, artifact, action, err := validateInstallRequest(request)
	if err != nil {
		return err
	}
	if action == installplan.ActionPreserve {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	scopeRoot := groupRoot(request.Installation, unit.Scope)
	_, scopeStatErr := os.Lstat(scopeRoot)
	scopeWasAbsent := errors.Is(scopeStatErr, fs.ErrNotExist)
	if scopeStatErr != nil && !scopeWasAbsent {
		return fmt.Errorf("inspect release group before installation: %w", scopeStatErr)
	}
	if action == installplan.ActionAdd && !scopeWasAbsent {
		return errors.New("release add destination appeared after planning")
	}
	if action == installplan.ActionReplace && scopeWasAbsent {
		return errors.New("release replacement destination disappeared after planning")
	}
	if err := prepareScope(request.Installation, scopeRoot); err != nil {
		return err
	}
	currentPath := filepath.Join(scopeRoot, "current")
	if info, statErr := os.Lstat(currentPath); statErr == nil && info.Mode()&os.ModeSymlink == 0 {
		return errors.New("release current pointer exists and is not a symbolic link")
	} else if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
		return fmt.Errorf("inspect release current pointer: %w", statErr)
	}
	stage, err := os.MkdirTemp(scopeRoot, ".stage-")
	if err != nil {
		return fmt.Errorf("create release stage: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(stage)
			_ = os.Remove(filepath.Join(scopeRoot, "generations"))
			_ = removeEmptyScope(scopeRoot)
		}
	}()

	metadataPath := filepath.Join(stage, ".temper")
	if err := os.Mkdir(metadataPath, 0o755); err != nil {
		return fmt.Errorf("create release metadata stage: %w", err)
	}
	archivePath := filepath.Join(metadataPath, "archive.tar.gz")
	if err := a.download(ctx, artifact, archivePath); err != nil {
		return err
	}
	entries, err := readArchiveManifest(ctx, archivePath, artifact)
	if err != nil {
		return err
	}
	payloadPath := filepath.Join(stage, "payload")
	if err := extractArchive(ctx, archivePath, artifact, payloadPath, entries); err != nil {
		return err
	}
	if err := verifyFileIdentity(ctx, archivePath, artifact.Size, artifact.SHA256); err != nil {
		return fmt.Errorf("reverify staged release archive: %w", err)
	}
	actualEntries, err := scanPayload(ctx, payloadPath)
	if err != nil || !reflect.DeepEqual(actualEntries, markerEntries(entries)) {
		return errors.New("staged release payload differs from validated archive manifest")
	}
	marker := installationMarker{
		Schema: markerSchema, UnitID: unitID, Adapter: unit.Adapter, Scope: unit.Scope,
		NativeName: unit.NativeName, Version: unit.Version, Revision: unit.Revision,
		Artifacts: append([]software.Artifact(nil), unit.Artifacts...), Entries: markerEntries(entries),
	}
	markerData, err := marshalMarker(marker)
	if err != nil {
		return err
	}
	markerPath := filepath.Join(metadataPath, "unit.json")
	if err := writeNewFile(markerPath, markerData, 0o644); err != nil {
		return err
	}
	if err := syncTree(stage); err != nil {
		return fmt.Errorf("sync release stage: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	generationsPath := filepath.Join(scopeRoot, "generations")
	if err := ensureChildDirectory(scopeRoot, generationsPath); err != nil {
		return err
	}
	generationName := artifact.SHA256[:16] + "-" + filepath.Base(stage)[len(".stage-"):]
	generationPath := filepath.Join(generationsPath, generationName)
	if err := os.Rename(stage, generationPath); err != nil {
		return fmt.Errorf("publish release generation: %w", err)
	}
	stage = generationPath
	if err := syncDirectory(generationsPath); err != nil {
		return fmt.Errorf("sync release generations: %w", err)
	}

	if info, statErr := os.Lstat(currentPath); statErr == nil && info.Mode()&os.ModeSymlink == 0 {
		return errors.New("release current pointer exists and is not a symbolic link")
	} else if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
		return fmt.Errorf("inspect release current pointer: %w", statErr)
	}
	temporaryPointer := filepath.Join(scopeRoot, ".current-"+generationName)
	if err := os.Symlink(filepath.ToSlash(filepath.Join("generations", generationName)), temporaryPointer); err != nil {
		return fmt.Errorf("stage release current pointer: %w", err)
	}
	if err := os.Rename(temporaryPointer, currentPath); err != nil {
		_ = os.Remove(temporaryPointer)
		return fmt.Errorf("commit release current pointer: %w", err)
	}
	committed = true
	if err := syncDirectory(scopeRoot); err != nil {
		return fmt.Errorf("sync committed release pointer: %w", err)
	}
	if err := cleanPublishedGroup(scopeRoot, generationName); err != nil {
		return fmt.Errorf("clean previous release generation: %w", err)
	}
	return nil
}

func (a *InstallationAdapter) Remove(ctx context.Context, request adapter.RemoveRequest) error {
	_, unit, execute, err := validateRemoveRequest(request)
	if err != nil {
		return err
	}
	if !execute {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	scopeRoot := groupRoot(request.Installation, unit.Scope)
	exists, safe, err := inspectRealGroupPath(request.Installation.Root, scopeRoot)
	if err != nil {
		return fmt.Errorf("inspect release group for removal: %w", err)
	}
	if !exists {
		return nil
	}
	if !safe {
		return errors.New("release group removal target is not a real directory")
	}
	if err := os.RemoveAll(scopeRoot); err != nil {
		return fmt.Errorf("remove release group: %w", err)
	}
	return syncDirectory(filepath.Dir(scopeRoot))
}

func (a *InstallationAdapter) download(ctx context.Context, artifact software.Artifact, destination string) error {
	source, err := a.reader.Open(ctx, artifact.Locator)
	if err != nil {
		return fmt.Errorf("open release artifact: %w", err)
	}
	destinationFile, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		_ = source.Close()
		return fmt.Errorf("create staged release artifact: %w", err)
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(destinationFile, hash), io.LimitReader(source, artifact.Size+1))
	closeSourceErr := source.Close()
	syncErr := destinationFile.Sync()
	closeDestinationErr := destinationFile.Close()
	if copyErr != nil {
		return fmt.Errorf("download release artifact: %w", copyErr)
	}
	if closeSourceErr != nil {
		return fmt.Errorf("close release artifact source: %w", closeSourceErr)
	}
	if syncErr != nil || closeDestinationErr != nil {
		return errors.New("persist staged release artifact")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if written != artifact.Size {
		return fmt.Errorf("release artifact size is %d, want %d", written, artifact.Size)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != artifact.SHA256 {
		return fmt.Errorf("release artifact sha256 is %s, want %s", got, artifact.SHA256)
	}
	return nil
}

func inspectUnit(ctx context.Context, unitID string, locked softwarelock.Unit, artifact software.Artifact, location, root string) (installplan.ObservedUnit, error) {
	scopeRoot := filepath.Dir(filepath.Dir(location))
	exists, safe, err := inspectRealGroupPath(root, scopeRoot)
	if err != nil {
		return installplan.ObservedUnit{}, err
	}
	if !exists {
		return installplan.ObservedUnit{InstallLocation: location}, nil
	}
	nonExact := func() installplan.ObservedUnit {
		return installplan.ObservedUnit{Present: true, Adapter: adapterID, Scope: locked.Scope, NativeName: locked.NativeName, Version: locked.Version, Location: location, InstallLocation: location}
	}
	if !safe {
		return nonExact(), nil
	}
	currentPath := filepath.Join(scopeRoot, "current")
	target, err := os.Readlink(currentPath)
	if err != nil || !safeCurrentTarget(target) {
		return nonExact(), nil
	}
	generationPath := filepath.Join(scopeRoot, filepath.FromSlash(target))
	if !strictlyBelow(scopeRoot, generationPath) {
		return nonExact(), nil
	}
	for _, directory := range []string{filepath.Join(scopeRoot, "generations"), generationPath, filepath.Join(generationPath, ".temper"), filepath.Join(generationPath, "payload")} {
		if !realDirectory(directory) {
			return nonExact(), nil
		}
	}
	markerPath := filepath.Join(generationPath, ".temper", "unit.json")
	if !regularFile(markerPath) {
		return nonExact(), nil
	}
	markerData, err := os.ReadFile(markerPath)
	if err != nil {
		return nonExact(), nil
	}
	marker, err := parseMarker(markerData)
	if err != nil || !markerMatches(marker, unitID, locked) {
		return nonExact(), nil
	}
	archivePath := filepath.Join(generationPath, ".temper", "archive.tar.gz")
	if err := verifyFileIdentity(ctx, archivePath, artifact.Size, artifact.SHA256); err != nil {
		return nonExact(), nil
	}
	entries, err := readArchiveManifest(ctx, archivePath, artifact)
	if err != nil || !reflect.DeepEqual(marker.Entries, markerEntries(entries)) {
		return nonExact(), nil
	}
	actual, err := scanPayload(ctx, filepath.Join(generationPath, "payload"))
	if err != nil || !reflect.DeepEqual(actual, marker.Entries) {
		return nonExact(), nil
	}
	if err := exactGroupLayout(scopeRoot, filepath.Base(generationPath)); err != nil {
		return nonExact(), nil
	}
	return installplan.ObservedUnit{
		Present: true, Adapter: locked.Adapter, Scope: locked.Scope, NativeName: locked.NativeName,
		Version: locked.Version, Revision: locked.Revision,
		Dependencies: append([]string(nil), locked.Dependencies...), Artifacts: append([]software.Artifact(nil), locked.Artifacts...),
		Location: location, InstallLocation: location,
	}, nil
}

type installationMarker struct {
	Schema     string              `json:"schema"`
	UnitID     string              `json:"unit_id"`
	Adapter    string              `json:"adapter"`
	Scope      string              `json:"scope"`
	NativeName string              `json:"native_name"`
	Version    string              `json:"version"`
	Revision   string              `json:"revision"`
	Artifacts  []software.Artifact `json:"artifacts"`
	Entries    []installedEntry    `json:"entries"`
}

type installedEntry struct {
	Path   string `json:"path"`
	Type   string `json:"type"`
	Mode   uint32 `json:"mode"`
	Size   int64  `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	Target string `json:"target,omitempty"`
}

type archiveEntry struct {
	installedEntry
	source string
}

func readArchiveManifest(ctx context.Context, archivePath string, artifact software.Artifact) ([]archiveEntry, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open staged release archive: %w", err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("open release gzip stream: %w", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	entries := map[string]archiveEntry{}
	var unpacked int64
	headers := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read release tar stream: %w", err)
		}
		headers++
		if headers > artifact.InstalledEntries+1 {
			return nil, errors.New("release archive exceeds locked manifest bounds")
		}
		if header.Mode < 0 || header.Mode&0o7000 != 0 {
			return nil, fmt.Errorf("release archive path %q has privileged mode bits", header.Name)
		}
		relative, included, err := archiveRelative(header.Name, artifact.ArchiveRoot, header.Typeflag == tar.TypeDir)
		if err != nil {
			return nil, err
		}
		if !included {
			continue
		}
		if _, exists := entries[relative]; exists {
			return nil, fmt.Errorf("release archive repeats path %q", relative)
		}
		if len(entries) >= artifact.InstalledEntries {
			return nil, errors.New("release archive exceeds locked installed entry count")
		}
		entry := archiveEntry{source: header.Name}
		entry.Path = relative
		switch header.Typeflag {
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || unpacked > artifact.UnpackedSize-header.Size {
				return nil, errors.New("release archive exceeds locked unpacked size")
			}
			unpacked += header.Size
			hash := sha256.New()
			written, err := io.Copy(hash, tarReader)
			if err != nil || written != header.Size {
				return nil, fmt.Errorf("read release archive file %q", relative)
			}
			entry.Type, entry.Mode, entry.Size, entry.SHA256 = "file", normalizedFileMode(header.Mode), header.Size, hex.EncodeToString(hash.Sum(nil))
		case tar.TypeDir:
			if header.Size != 0 {
				return nil, fmt.Errorf("release archive directory %q has file content", relative)
			}
			entry.Type, entry.Mode = "directory", 0o755
		case tar.TypeSymlink:
			if header.Size != 0 {
				return nil, fmt.Errorf("release archive symlink %q has file content", relative)
			}
			if !safeSymlink(relative, header.Linkname) {
				return nil, fmt.Errorf("release archive symlink %q has unsafe target %q", relative, header.Linkname)
			}
			entry.Type, entry.Mode, entry.Target = "symlink", 0o777, header.Linkname
		default:
			return nil, fmt.Errorf("release archive path %q has unsupported tar type %d", relative, header.Typeflag)
		}
		entries[relative] = entry
	}
	if unpacked != artifact.UnpackedSize {
		return nil, fmt.Errorf("release archive unpacked size is %d, want %d", unpacked, artifact.UnpackedSize)
	}
	if len(entries) == 0 {
		return nil, errors.New("release archive payload is empty")
	}
	var trailing [1]byte
	if count, trailingErr := gzipReader.Read(trailing[:]); count != 0 || !errors.Is(trailingErr, io.EOF) {
		if trailingErr != nil && !errors.Is(trailingErr, io.EOF) {
			return nil, fmt.Errorf("finish release gzip stream: %w", trailingErr)
		}
		return nil, errors.New("release archive has trailing decompressed content")
	}
	addImplicitDirectories(entries)
	if len(entries) != artifact.InstalledEntries {
		return nil, fmt.Errorf("release archive installed entry count is %d, want %d", len(entries), artifact.InstalledEntries)
	}
	if err := validateArchiveParents(entries); err != nil {
		return nil, err
	}
	result := make([]archiveEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}

func extractArchive(ctx context.Context, archivePath string, artifact software.Artifact, payloadPath string, expected []archiveEntry) error {
	if err := os.Mkdir(payloadPath, 0o755); err != nil {
		return fmt.Errorf("create release payload stage: %w", err)
	}
	byPath := make(map[string]archiveEntry, len(expected))
	for _, entry := range expected {
		byPath[entry.Path] = entry
		if entry.Type == "directory" {
			if err := os.MkdirAll(filepath.Join(payloadPath, filepath.FromSlash(entry.Path)), 0o755); err != nil {
				return fmt.Errorf("create release archive directory %q: %w", entry.Path, err)
			}
		}
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	var symlinks []archiveEntry
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		relative, included, err := archiveRelative(header.Name, artifact.ArchiveRoot, header.Typeflag == tar.TypeDir)
		if err != nil || !included {
			if err != nil {
				return err
			}
			continue
		}
		entry := byPath[relative]
		switch entry.Type {
		case "file":
			destination := filepath.Join(payloadPath, filepath.FromSlash(relative))
			if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
				return err
			}
			output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fs.FileMode(entry.Mode))
			if err != nil {
				return err
			}
			hash := sha256.New()
			written, copyErr := io.Copy(io.MultiWriter(output, hash), tarReader)
			syncErr, closeErr := output.Sync(), output.Close()
			if copyErr != nil || syncErr != nil || closeErr != nil || written != entry.Size || hex.EncodeToString(hash.Sum(nil)) != entry.SHA256 {
				return fmt.Errorf("extract release archive file %q", relative)
			}
		case "symlink":
			symlinks = append(symlinks, entry)
		}
	}
	for _, entry := range symlinks {
		destination := filepath.Join(payloadPath, filepath.FromSlash(entry.Path))
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		if err := os.Symlink(entry.Target, destination); err != nil {
			return fmt.Errorf("create release archive symlink %q: %w", entry.Path, err)
		}
	}
	return nil
}

func scanPayload(ctx context.Context, payloadPath string) ([]installedEntry, error) {
	var result []installedEntry
	err := filepath.WalkDir(payloadPath, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if current == payloadPath {
			return nil
		}
		relative, err := filepath.Rel(payloadPath, current)
		if err != nil {
			return err
		}
		item := installedEntry{Path: filepath.ToSlash(relative)}
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		switch {
		case info.Mode().IsRegular():
			item.Type, item.Mode, item.Size = "file", uint32(info.Mode().Perm()), info.Size()
			hash, err := hashFile(ctx, current)
			if err != nil {
				return err
			}
			item.SHA256 = hash
		case info.IsDir():
			item.Type, item.Mode = "directory", uint32(info.Mode().Perm())
		case info.Mode()&os.ModeSymlink != 0:
			item.Type, item.Mode = "symlink", 0o777
			item.Target, err = os.Readlink(current)
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("installed release path %q has unsupported file type", item.Path)
		}
		result = append(result, item)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}

func archiveRelative(name, root string, directory bool) (string, bool, error) {
	cleanedName := strings.TrimSuffix(name, "/")
	for strings.HasPrefix(cleanedName, "./") {
		cleanedName = strings.TrimPrefix(cleanedName, "./")
	}
	if cleanedName == "." && root == "." && directory {
		return "", false, nil
	}
	if !safeArchivePath(cleanedName, false) {
		return "", false, fmt.Errorf("release archive path %q is unsafe", name)
	}
	if root == "." {
		return cleanedName, true, nil
	}
	if cleanedName == root {
		if !directory {
			return "", false, fmt.Errorf("release archive root %q is not a directory", root)
		}
		return "", false, nil
	}
	prefix := root + "/"
	if !strings.HasPrefix(cleanedName, prefix) {
		return "", false, fmt.Errorf("release archive path %q is outside archive root %q", name, root)
	}
	return strings.TrimPrefix(cleanedName, prefix), true, nil
}

func safeArchivePath(value string, allowDot bool) bool {
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, "\\") || strings.ContainsRune(value, '\x00') || path.Clean(value) != value {
		return false
	}
	if value == "." {
		return allowDot
	}
	return value != ".." && !strings.HasPrefix(value, "../")
}

func safeSymlink(relative, target string) bool {
	if target == "" || strings.HasPrefix(target, "/") || strings.Contains(target, "\\") || strings.ContainsRune(target, '\x00') {
		return false
	}
	resolved := path.Clean(path.Join(path.Dir(relative), target))
	return resolved != ".." && !strings.HasPrefix(resolved, "../") && !strings.HasPrefix(resolved, "/")
}

func normalizedFileMode(mode int64) uint32 {
	if mode&0o111 != 0 {
		return 0o755
	}
	return 0o644
}

func addImplicitDirectories(entries map[string]archiveEntry) {
	paths := make([]string, 0, len(entries))
	for value := range entries {
		paths = append(paths, value)
	}
	for _, value := range paths {
		for parent := path.Dir(value); parent != "."; parent = path.Dir(parent) {
			if _, exists := entries[parent]; !exists {
				entries[parent] = archiveEntry{installedEntry: installedEntry{Path: parent, Type: "directory", Mode: 0o755}}
			}
		}
	}
}

func validateArchiveParents(entries map[string]archiveEntry) error {
	for value, entry := range entries {
		for parent := path.Dir(value); parent != "."; parent = path.Dir(parent) {
			if entries[parent].Type != "directory" {
				return fmt.Errorf("release archive path %q has non-directory parent %q", value, parent)
			}
		}
		if entry.Type == "symlink" {
			target := path.Clean(path.Join(path.Dir(value), entry.Target))
			if _, exists := entries[target]; !exists {
				return fmt.Errorf("release archive symlink %q targets missing path %q", value, target)
			}
			seen := map[string]bool{value: true}
			for entries[target].Type == "symlink" {
				if seen[target] {
					return fmt.Errorf("release archive symlink %q belongs to a cycle", value)
				}
				seen[target] = true
				target = path.Clean(path.Join(path.Dir(target), entries[target].Target))
				if _, exists := entries[target]; !exists {
					return fmt.Errorf("release archive symlink %q resolves through missing path %q", value, target)
				}
			}
		}
	}
	return nil
}

func markerEntries(entries []archiveEntry) []installedEntry {
	result := make([]installedEntry, len(entries))
	for index, entry := range entries {
		result[index] = entry.installedEntry
	}
	return result
}

func marshalMarker(marker installationMarker) ([]byte, error) {
	data, err := json.Marshal(marker)
	if err != nil {
		return nil, fmt.Errorf("encode release installation marker: %w", err)
	}
	return append(data, '\n'), nil
}

func parseMarker(data []byte) (installationMarker, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var marker installationMarker
	if err := decoder.Decode(&marker); err != nil {
		return installationMarker{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return installationMarker{}, errors.New("release installation marker has trailing data")
	}
	canonical, err := marshalMarker(marker)
	if err != nil || string(canonical) != string(data) {
		return installationMarker{}, errors.New("release installation marker is not canonical")
	}
	return marker, nil
}

func markerMatches(marker installationMarker, unitID string, locked softwarelock.Unit) bool {
	return marker.Schema == markerSchema && marker.UnitID == unitID && marker.Adapter == locked.Adapter && marker.Scope == locked.Scope && marker.NativeName == locked.NativeName && marker.Version == locked.Version && marker.Revision == locked.Revision && reflect.DeepEqual(marker.Artifacts, locked.Artifacts)
}

func validateLockedUnit(unitID string, unit softwarelock.Unit) (software.Artifact, error) {
	if unit.Adapter != adapterID || !stableIDPattern.MatchString(unit.Scope) || strings.TrimSpace(unit.NativeName) == "" || strings.TrimSpace(unit.Version) == "" || !revisionPattern.MatchString(unit.Revision) {
		return software.Artifact{}, fmt.Errorf("release unit %q has invalid exact identity", unitID)
	}
	if len(unit.Dependencies) != 0 || len(unit.Artifacts) != 1 {
		return software.Artifact{}, fmt.Errorf("release unit %q must own one archive and no provider dependencies", unitID)
	}
	artifact := unit.Artifacts[0]
	if !validHTTPSLocator(artifact.Locator) || !sha256Pattern.MatchString(artifact.SHA256) || artifact.Size <= 0 || artifact.UnpackedSize <= 0 || artifact.InstalledEntries <= 0 || artifact.Format != "tar.gz" || !safeArchivePath(artifact.ArchiveRoot, true) {
		return software.Artifact{}, fmt.Errorf("release unit %q archive identity is invalid", unitID)
	}
	return artifact, nil
}

func validateInstallRequest(request adapter.InstallRequest) (string, softwarelock.Unit, software.Artifact, installplan.Action, error) {
	if err := validateTargetInstallation(request.Target, request.Installation); err != nil {
		return "", softwarelock.Unit{}, software.Artifact{}, "", err
	}
	if request.Group.ID != request.Group.Adapter+":"+request.Group.Scope || request.Group.Adapter != adapterID || request.Group.EffectModel != installplan.EffectIsolated || len(request.Group.Units) != 1 || len(request.Units) != 1 {
		return "", softwarelock.Unit{}, software.Artifact{}, "", errors.New("release install requires one complete isolated adapter/scope group")
	}
	planned := request.Group.Units[0]
	unit, ok := request.Units[planned.ID]
	if !ok || unit.Scope != request.Group.Scope {
		return "", softwarelock.Unit{}, software.Artifact{}, "", errors.New("release install group differs from locked unit")
	}
	artifact, err := validateLockedUnit(planned.ID, unit)
	if err != nil {
		return "", softwarelock.Unit{}, software.Artifact{}, "", err
	}
	if planned.Location != installLocation(request.Installation, unit.Scope) {
		return "", softwarelock.Unit{}, software.Artifact{}, "", errors.New("release install location differs from its isolated group destination")
	}
	if planned.Action != installplan.ActionAdd && planned.Action != installplan.ActionReplace && planned.Action != installplan.ActionPreserve {
		return "", softwarelock.Unit{}, software.Artifact{}, "", errors.New("release install action is invalid")
	}
	return planned.ID, unit, artifact, planned.Action, nil
}

func validateRemoveRequest(request adapter.RemoveRequest) (string, softwarelock.Unit, bool, error) {
	if err := validateTargetInstallation(request.Target, request.Installation); err != nil {
		return "", softwarelock.Unit{}, false, err
	}
	if request.Group.ID != request.Group.Adapter+":"+request.Group.Scope || request.Group.Adapter != adapterID || request.Group.EffectModel != installplan.EffectIsolated || len(request.Group.Units) != 1 || len(request.Units) != 1 {
		return "", softwarelock.Unit{}, false, errors.New("release removal requires one complete isolated adapter/scope group")
	}
	planned := request.Group.Units[0]
	unit, ok := request.Units[planned.ID]
	if !ok || unit.Scope != request.Group.Scope {
		return "", softwarelock.Unit{}, false, errors.New("release removal group differs from locked unit")
	}
	if _, err := validateLockedUnit(planned.ID, unit); err != nil {
		return "", softwarelock.Unit{}, false, err
	}
	if planned.Location != installLocation(request.Installation, unit.Scope) {
		return "", softwarelock.Unit{}, false, errors.New("release removal location differs from its isolated group destination")
	}
	if planned.Action != removeplan.ActionPreserve && planned.Action != removeplan.ActionRemove {
		return "", softwarelock.Unit{}, false, errors.New("release removal action is invalid")
	}
	if planned.Execute && planned.Action != removeplan.ActionRemove {
		return "", softwarelock.Unit{}, false, errors.New("release removal cannot execute a preserve action")
	}
	return planned.ID, unit, planned.Execute, nil
}

func validateTargetInstallation(target software.Target, installation installplan.Installation) error {
	if err := target.Validate(); err != nil {
		return err
	}
	if target.OS != "darwin" || target.Arch != "arm64" {
		return fmt.Errorf("upstream release adapter does not support target %s/%s", target.OS, target.Arch)
	}
	if !stableIDPattern.MatchString(installation.ID) || !filepath.IsAbs(installation.Root) || filepath.Clean(installation.Root) != installation.Root {
		return errors.New("release installation requires a stable id and absolute clean root")
	}
	return nil
}

func groupRoot(installation installplan.Installation, scope string) string {
	return filepath.Join(installplan.InstallationRoot(installation), adapterID, scope)
}

func installLocation(installation installplan.Installation, scope string) string {
	return filepath.Join(groupRoot(installation, scope), "current", "payload")
}

func prepareScope(installation installplan.Installation, scopeRoot string) error {
	info, err := os.Lstat(installation.Root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("release installation root must already exist as a real directory")
	}
	current := installation.Root
	relative, _ := filepath.Rel(installation.Root, scopeRoot)
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		if err := ensureChildDirectory(filepath.Dir(current), current); err != nil {
			return err
		}
	}
	return nil
}

func inspectRealGroupPath(root, group string) (bool, bool, error) {
	if !strictlyBelow(root, group) {
		return false, false, errors.New("release group is outside installation root")
	}
	info, err := os.Lstat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return false, true, nil
	}
	if err != nil {
		return false, false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return true, false, nil
	}
	relative, err := filepath.Rel(root, group)
	if err != nil {
		return false, false, err
	}
	current := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err = os.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			return false, true, nil
		}
		if err != nil {
			return false, false, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return true, false, nil
		}
	}
	return true, true, nil
}

func realDirectory(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

func regularFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

func ensureChildDirectory(parent, child string) error {
	info, err := os.Lstat(child)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.Mkdir(child, 0o755); err != nil {
			return fmt.Errorf("create release directory below %q: %w", parent, err)
		}
		return syncDirectory(parent)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("release path %q is not a real directory", child)
	}
	return nil
}

func cleanPublishedGroup(scopeRoot, currentGeneration string) error {
	generationsPath := filepath.Join(scopeRoot, "generations")
	entries, err := os.ReadDir(generationsPath)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == currentGeneration {
			continue
		}
		if err := os.RemoveAll(filepath.Join(generationsPath, entry.Name())); err != nil {
			return err
		}
	}
	scopeEntries, err := os.ReadDir(scopeRoot)
	if err != nil {
		return err
	}
	for _, entry := range scopeEntries {
		if entry.Name() == "current" || entry.Name() == "generations" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(scopeRoot, entry.Name())); err != nil {
			return err
		}
	}
	return syncDirectory(scopeRoot)
}

func exactGroupLayout(scopeRoot, currentGeneration string) error {
	entries, err := os.ReadDir(scopeRoot)
	if err != nil || len(entries) != 2 || entries[0].Name() != "current" || entries[1].Name() != "generations" {
		return errors.New("release group contains unexpected paths")
	}
	generations, err := os.ReadDir(filepath.Join(scopeRoot, "generations"))
	if err != nil || len(generations) != 1 || generations[0].Name() != currentGeneration || !generations[0].IsDir() {
		return errors.New("release group generations are not exact")
	}
	return nil
}

func safeCurrentTarget(target string) bool {
	if filepath.ToSlash(target) != target || !strings.HasPrefix(target, "generations/") || path.Clean(target) != target {
		return false
	}
	return strings.Count(target, "/") == 1 && safeArchivePath(strings.TrimPrefix(target, "generations/"), false)
}

func verifyFileIdentity(ctx context.Context, path string, size int64, wantHash string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != size {
		return errors.New("release artifact file identity differs")
	}
	got, err := hashFile(ctx, path)
	if err != nil || got != wantHash {
		return errors.New("release artifact file hash differs")
	}
	return nil
}

func hashFile(ctx context.Context, path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	buffer := make([]byte, 128*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		count, readErr := file.Read(buffer)
		if count > 0 {
			_, _ = hash.Write(buffer[:count])
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func writeNewFile(path string, data []byte, mode fs.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func syncTree(root string) error {
	var directories []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			directories = append(directories, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(directories, func(i, j int) bool { return len(directories[i]) > len(directories[j]) })
	for _, directory := range directories {
		if err := syncDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func removeEmptyScope(scopeRoot string) error {
	current := scopeRoot
	for current != filepath.Dir(scopeRoot) {
		if err := os.Remove(current); err != nil {
			return nil
		}
		current = filepath.Dir(current)
	}
	return nil
}

func strictlyBelow(root, value string) bool {
	relative, err := filepath.Rel(root, value)
	return err == nil && relative != "." && relative != ".." && !filepath.IsAbs(relative) && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func sortedUnitIDs(values map[string]softwarelock.Unit) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
