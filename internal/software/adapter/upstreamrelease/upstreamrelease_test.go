package upstreamrelease

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/catalog"
	"github.com/temper-sh/temper/internal/software/installplan"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/removeplan"
)

func TestResolverCopiesOneReviewedTargetArchiveIntoExactCandidate(t *testing.T) {
	archive := makeArchive(t, []tarEntry{
		{name: "bundle/bin/server", body: "binary", mode: 0o755, kind: tar.TypeReg},
	})
	request := resolveRequest(archive)

	candidates, err := NewResolver().Candidates(context.Background(), request)
	if err != nil {
		t.Fatalf("Candidates() error = %v", err)
	}
	if len(candidates) != 1 || candidates[0].RootUnit != "upstream-release:llama-cpp" {
		t.Fatalf("Candidates() = %#v, want one deterministic root", candidates)
	}
	unit := candidates[0].Units[candidates[0].RootUnit]
	if unit.Scope != "llama-cpp" || unit.NativeName != "llama-cpp" || unit.Version != "b10566" || unit.Revision != "bb4caa7540188872173c44d161602d9271386413" {
		t.Fatalf("resolved unit = %#v, want reviewed source identity", unit)
	}
	if len(unit.Artifacts) != 1 || unit.Artifacts[0].SHA256 != archive.sha256 || unit.Artifacts[0].UnpackedSize != 6 || unit.Artifacts[0].InstalledEntries != archive.installedEntries || candidates[0].Current {
		t.Fatalf("resolved artifacts = %#v, want exact catalog artifact without moving-current claim", unit.Artifacts)
	}
}

func TestResolverRejectsMovingOrMissingTargetPolicy(t *testing.T) {
	archive := makeArchive(t, []tarEntry{{name: "bundle/server", body: "ok", mode: 0o755, kind: tar.TypeReg}})
	tests := []struct {
		name string
		edit func(*adapter.ResolveRequest)
		want string
	}{
		{name: "moving selection", edit: func(request *adapter.ResolveRequest) { request.Recipe.Selection = catalog.Selection{Policy: "latest"} }, want: "requires one exact"},
		{name: "other target", edit: func(request *adapter.ResolveRequest) { request.Target.Arch = "amd64" }, want: "does not support"},
		{name: "missing target asset", edit: func(request *adapter.ResolveRequest) { request.Recipe.Source.Artifacts[0].Target.Arch = "amd64" }, want: "no release artifact matches"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := resolveRequest(archive)
			test.edit(&request)
			_, err := NewResolver().Candidates(context.Background(), request)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Candidates() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestInstallationRoundTripDetectsDriftRepairsAtomicallyAndRemovesOnlyGroup(t *testing.T) {
	archive := makeArchive(t, []tarEntry{
		{name: "bundle/bin/server", body: "server-binary", mode: 0o755, kind: tar.TypeReg},
		{name: "bundle/lib/runtime.dylib", body: "runtime", mode: 0o644, kind: tar.TypeReg},
		{name: "bundle/bin/server-link", link: "server", mode: 0o777, kind: tar.TypeSymlink},
	})
	reader := &memoryReader{content: map[string][]byte{archive.locator: archive.data}}
	installer, err := NewInstallationAdapter(reader)
	if err != nil {
		t.Fatal(err)
	}
	installation := installplan.Installation{ID: "field-kit", Root: t.TempDir()}
	target := software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "26.0"}
	unitID := "upstream-release:llama-cpp"
	unit := lockedUnit(archive)
	location := filepath.Join(installplan.InstallationRoot(installation), adapterID, unit.Scope, "current", "payload")
	units := map[string]softwarelock.Unit{unitID: unit}
	add := installplan.Group{
		ID: adapterID + ":" + unit.Scope, Adapter: adapterID, Scope: unit.Scope, EffectModel: installplan.EffectIsolated,
		Units: []installplan.Unit{{ID: unitID, Action: installplan.ActionAdd, Ownership: installplan.OwnershipTemperAdded, Location: location}},
	}
	if err := installer.Install(context.Background(), adapter.InstallRequest{Target: target, Installation: installation, Group: add, Units: units}); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if reader.opens != 1 {
		t.Fatalf("artifact opens = %d, want 1", reader.opens)
	}
	server := filepath.Join(location, "bin", "server")
	info, err := os.Stat(server)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("installed server stat = %v, %v", info, err)
	}
	link, err := os.Readlink(filepath.Join(location, "bin", "server-link"))
	if err != nil || link != "server" {
		t.Fatalf("installed symlink = %q, %v", link, err)
	}

	observed := inspectOne(t, installer, target, installation, unitID, unit)
	if !installplan.MatchesLock(unit, observed) || observed.Location != location {
		t.Fatalf("exact observation = %#v", observed)
	}
	preserve := add
	preserve.Units[0].Action = installplan.ActionPreserve
	if err := installer.Install(context.Background(), adapter.InstallRequest{Target: target, Installation: installation, Group: preserve, Units: units}); err != nil {
		t.Fatalf("second Install() error = %v", err)
	}
	if reader.opens != 1 {
		t.Fatalf("second clean run opened artifact; opens = %d", reader.opens)
	}

	if err := os.WriteFile(server, []byte("drift"), 0o755); err != nil {
		t.Fatal(err)
	}
	drifted := inspectOne(t, installer, target, installation, unitID, unit)
	if !drifted.Present || installplan.MatchesLock(unit, drifted) {
		t.Fatalf("drift observation = %#v, want present non-exact", drifted)
	}
	replace := add
	replace.Units[0].Action = installplan.ActionReplace
	if err := installer.Install(context.Background(), adapter.InstallRequest{Target: target, Installation: installation, Group: replace, Units: units}); err != nil {
		t.Fatalf("repair Install() error = %v", err)
	}
	if repaired := inspectOne(t, installer, target, installation, unitID, unit); !installplan.MatchesLock(unit, repaired) {
		t.Fatalf("repaired observation = %#v, want exact", repaired)
	}

	sibling := filepath.Join(installplan.InstallationRoot(installation), adapterID, "other-scope", "keep")
	if err := os.MkdirAll(filepath.Dir(sibling), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sibling, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	remove := removeplan.Group{
		ID: adapterID + ":" + unit.Scope, Adapter: adapterID, Scope: unit.Scope, EffectModel: installplan.EffectIsolated,
		Units: []removeplan.Unit{{ID: unitID, Action: removeplan.ActionRemove, Execute: true, Ownership: installplan.OwnershipTemperAdded, Location: location}},
	}
	request := adapter.RemoveRequest{Target: target, Installation: installation, Group: remove, Units: units}
	if err := installer.Remove(context.Background(), request); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := installer.Remove(context.Background(), request); err != nil {
		t.Fatalf("second Remove() error = %v", err)
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Fatalf("sibling was removed: %v", err)
	}
	if removed := inspectOne(t, installer, target, installation, unitID, unit); removed.Present || removed.InstallLocation != location {
		t.Fatalf("removed observation = %#v", removed)
	}
}

func TestInstallFailureLeavesPublishedGenerationUnchanged(t *testing.T) {
	good := makeArchive(t, []tarEntry{{name: "bundle/server", body: "good", mode: 0o755, kind: tar.TypeReg}})
	bad := makeArchive(t, []tarEntry{{name: "../escape", body: "bad", mode: 0o644, kind: tar.TypeReg}})
	reader := &memoryReader{content: map[string][]byte{good.locator: good.data, bad.locator: bad.data}}
	installer, _ := NewInstallationAdapter(reader)
	installation := installplan.Installation{ID: "field-kit", Root: t.TempDir()}
	target := software.Target{OS: "darwin", Arch: "arm64"}
	unitID := "upstream-release:llama-cpp"
	goodUnit := lockedUnit(good)
	location := filepath.Join(installplan.InstallationRoot(installation), adapterID, goodUnit.Scope, "current", "payload")
	group := installplan.Group{ID: adapterID + ":" + goodUnit.Scope, Adapter: adapterID, Scope: goodUnit.Scope, EffectModel: installplan.EffectIsolated, Units: []installplan.Unit{{ID: unitID, Action: installplan.ActionAdd, Ownership: installplan.OwnershipTemperAdded, Location: location}}}
	if err := installer.Install(context.Background(), adapter.InstallRequest{Target: target, Installation: installation, Group: group, Units: map[string]softwarelock.Unit{unitID: goodUnit}}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(location, "server"))
	if err != nil {
		t.Fatal(err)
	}
	badUnit := lockedUnit(bad)
	group.Units[0].Action = installplan.ActionReplace
	err = installer.Install(context.Background(), adapter.InstallRequest{Target: target, Installation: installation, Group: group, Units: map[string]softwarelock.Unit{unitID: badUnit}})
	if err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("replacement error = %v, want unsafe path refusal", err)
	}
	after, err := os.ReadFile(filepath.Join(location, "server"))
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("published generation changed after failed replacement: %q, %v", after, err)
	}
	if observed := inspectOne(t, installer, target, installation, unitID, goodUnit); !installplan.MatchesLock(goodUnit, observed) {
		t.Fatalf("old generation is no longer exact: %#v", observed)
	}
}

func TestInstallRejectsUnsafeArchivesBeforePublication(t *testing.T) {
	tests := []struct {
		name    string
		entries []tarEntry
		want    string
	}{
		{name: "parent traversal", entries: []tarEntry{{name: "../outside", body: "x", kind: tar.TypeReg}}, want: "unsafe"},
		{name: "absolute path", entries: []tarEntry{{name: "/outside", body: "x", kind: tar.TypeReg}}, want: "unsafe"},
		{name: "escaping symlink", entries: []tarEntry{{name: "bundle/file", body: "x", kind: tar.TypeReg}, {name: "bundle/link", link: "../../outside", kind: tar.TypeSymlink}}, want: "unsafe target"},
		{name: "symlink cycle", entries: []tarEntry{{name: "bundle/file", body: "x", kind: tar.TypeReg}, {name: "bundle/a", link: "b", kind: tar.TypeSymlink}, {name: "bundle/b", link: "a", kind: tar.TypeSymlink}}, want: "belongs to a cycle"},
		{name: "hard link", entries: []tarEntry{{name: "bundle/file", body: "x", kind: tar.TypeReg}, {name: "bundle/hard", link: "bundle/file", kind: tar.TypeLink}}, want: "unsupported tar type"},
		{name: "duplicate", entries: []tarEntry{{name: "bundle/file", body: "a", kind: tar.TypeReg}, {name: "bundle/file", body: "b", kind: tar.TypeReg}}, want: "repeats path"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			archive := makeArchive(t, test.entries)
			reader := &memoryReader{content: map[string][]byte{archive.locator: archive.data}}
			installer, _ := NewInstallationAdapter(reader)
			installation := installplan.Installation{ID: "test", Root: t.TempDir()}
			unitID := "upstream-release:llama-cpp"
			unit := lockedUnit(archive)
			location := filepath.Join(installplan.InstallationRoot(installation), adapterID, unit.Scope, "current", "payload")
			group := installplan.Group{ID: adapterID + ":" + unit.Scope, Adapter: adapterID, Scope: unit.Scope, EffectModel: installplan.EffectIsolated, Units: []installplan.Unit{{ID: unitID, Action: installplan.ActionAdd, Ownership: installplan.OwnershipTemperAdded, Location: location}}}
			err := installer.Install(context.Background(), adapter.InstallRequest{Target: software.Target{OS: "darwin", Arch: "arm64"}, Installation: installation, Group: group, Units: map[string]softwarelock.Unit{unitID: unit}})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Install() error = %v, want %q", err, test.want)
			}
			if _, err := os.Lstat(filepath.Join(filepath.Dir(filepath.Dir(location)), "current")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unsafe archive published current pointer: %v", err)
			}
			if _, err := os.Stat(filepath.Join(installation.Root, "outside")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unsafe archive wrote outside stage: %v", err)
			}
		})
	}
}

func TestInstallRejectsSizeAndHashMismatchWithoutPublishing(t *testing.T) {
	archive := makeArchive(t, []tarEntry{{name: "bundle/file", body: "payload", kind: tar.TypeReg}})
	tests := []struct {
		name string
		edit func(*softwarelock.Unit)
		want string
	}{
		{name: "size", edit: func(unit *softwarelock.Unit) { unit.Artifacts[0].Size-- }, want: "size is"},
		{name: "hash", edit: func(unit *softwarelock.Unit) { unit.Artifacts[0].SHA256 = strings.Repeat("0", 64) }, want: "sha256 is"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			installer, _ := NewInstallationAdapter(&memoryReader{content: map[string][]byte{archive.locator: archive.data}})
			installation := installplan.Installation{ID: "test", Root: t.TempDir()}
			unitID := "upstream-release:llama-cpp"
			unit := lockedUnit(archive)
			test.edit(&unit)
			location := filepath.Join(installplan.InstallationRoot(installation), adapterID, unit.Scope, "current", "payload")
			group := installplan.Group{ID: adapterID + ":" + unit.Scope, Adapter: adapterID, Scope: unit.Scope, EffectModel: installplan.EffectIsolated, Units: []installplan.Unit{{ID: unitID, Action: installplan.ActionAdd, Location: location}}}
			err := installer.Install(context.Background(), adapter.InstallRequest{Target: software.Target{OS: "darwin", Arch: "arm64"}, Installation: installation, Group: group, Units: map[string]softwarelock.Unit{unitID: unit}})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Install() error = %v, want %q", err, test.want)
			}
			if _, err := os.Lstat(filepath.Join(filepath.Dir(filepath.Dir(location)), "current")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("mismatched artifact published current pointer: %v", err)
			}
		})
	}
}

func TestInspectionAndRemovalRefuseSymlinkedGroupAncestors(t *testing.T) {
	archive := makeArchive(t, []tarEntry{{name: "bundle/file", body: "payload", kind: tar.TypeReg}})
	installer, _ := NewInstallationAdapter(&memoryReader{content: map[string][]byte{archive.locator: archive.data}})
	installation := installplan.Installation{ID: "field-kit", Root: t.TempDir()}
	target := software.Target{OS: "darwin", Arch: "arm64"}
	unitID, unit := "upstream-release:llama-cpp", lockedUnit(archive)
	installationRoot := installplan.InstallationRoot(installation)
	if err := os.MkdirAll(installationRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	outsideGroup := filepath.Join(outside, unit.Scope)
	if err := os.MkdirAll(outsideGroup, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(outsideGroup, "sentinel")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(installationRoot, adapterID)); err != nil {
		t.Fatal(err)
	}
	observed := inspectOne(t, installer, target, installation, unitID, unit)
	if !observed.Present || installplan.MatchesLock(unit, observed) {
		t.Fatalf("symlinked group observation = %#v, want present non-exact without traversal", observed)
	}
	location := filepath.Join(installationRoot, adapterID, unit.Scope, "current", "payload")
	group := removeplan.Group{
		ID: adapterID + ":" + unit.Scope, Adapter: adapterID, Scope: unit.Scope, EffectModel: installplan.EffectIsolated,
		Units: []removeplan.Unit{{ID: unitID, Action: removeplan.ActionRemove, Execute: true, Ownership: installplan.OwnershipTemperAdded, Location: location}},
	}
	err := installer.Remove(context.Background(), adapter.RemoveRequest{Target: target, Installation: installation, Group: group, Units: map[string]softwarelock.Unit{unitID: unit}})
	if err == nil || !strings.Contains(err.Error(), "not a real directory") {
		t.Fatalf("Remove() error = %v, want symlink ancestor refusal", err)
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "keep" {
		t.Fatalf("outside sentinel changed: %q, %v", data, err)
	}
}

func inspectOne(t *testing.T, installer *InstallationAdapter, target software.Target, installation installplan.Installation, unitID string, unit softwarelock.Unit) installplan.ObservedUnit {
	t.Helper()
	got, err := installer.Inspect(context.Background(), adapter.InspectRequest{Target: target, Installation: installation, Units: map[string]softwarelock.Unit{unitID: unit}})
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	return got[unitID]
}

type archiveFixture struct {
	data             []byte
	locator          string
	sha256           string
	size             int64
	unpackedSize     int64
	installedEntries int
}

type tarEntry struct {
	name string
	body string
	link string
	mode int64
	kind byte
}

func makeArchive(t *testing.T, entries []tarEntry) archiveFixture {
	t.Helper()
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gzipWriter)
	var unpacked int64
	for _, entry := range entries {
		kind := entry.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		mode := entry.mode
		if mode == 0 {
			mode = 0o644
		}
		header := &tar.Header{Name: entry.name, Mode: mode, Typeflag: kind, Linkname: entry.link}
		if kind == tar.TypeReg || kind == tar.TypeRegA {
			header.Size = int64(len(entry.body))
			unpacked += header.Size
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Size > 0 {
			if _, err := io.WriteString(tarWriter, entry.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	data := output.Bytes()
	hash := softwareHash(data)
	return archiveFixture{data: append([]byte(nil), data...), locator: "https://example.invalid/release/" + hash[:12] + ".tar.gz", sha256: hash, size: int64(len(data)), unpackedSize: unpacked, installedEntries: installedEntryCount(entries)}
}

func installedEntryCount(entries []tarEntry) int {
	paths := map[string]bool{}
	for _, entry := range entries {
		name := strings.TrimSuffix(strings.TrimPrefix(entry.name, "./"), "/")
		name = strings.TrimPrefix(name, "bundle/")
		if name == "" || name == "bundle" {
			continue
		}
		paths[name] = true
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			continue
		}
		for parent := filepath.ToSlash(filepath.Dir(name)); parent != "."; parent = filepath.ToSlash(filepath.Dir(parent)) {
			paths[parent] = true
		}
	}
	if len(paths) == 0 {
		return 1
	}
	return len(paths)
}

func softwareHash(data []byte) string {
	return sha256Bytes(data)
}

func sha256Bytes(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func resolveRequest(archive archiveFixture) adapter.ResolveRequest {
	return adapter.ResolveRequest{
		Package: "llama-cpp", Target: software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "26.0"},
		Recipe: catalog.Recipe{
			Method: method, RecipeRevision: "llama-cpp-release/v1", VersionScheme: "opaque", Selection: catalog.Selection{Policy: "exact", Exact: "b10566"},
			Source: catalog.Source{Kind: "release-archive", Name: "llama-cpp", Repository: "ggml-org/llama.cpp", Revision: "bb4caa7540188872173c44d161602d9271386413", Artifacts: []catalog.ReleaseArtifact{{Target: software.Target{OS: "darwin", Arch: "arm64"}, Locator: archive.locator, SHA256: archive.sha256, Size: archive.size, UnpackedSize: archive.unpackedSize, InstalledEntries: archive.installedEntries, Format: "tar.gz", ArchiveRoot: "bundle"}}},
		},
	}
}

func lockedUnit(archive archiveFixture) softwarelock.Unit {
	return softwarelock.Unit{
		Adapter: adapterID, Scope: "llama-cpp", NativeName: "llama-cpp", Version: "b10566", Revision: "bb4caa7540188872173c44d161602d9271386413", Dependencies: []string{},
		Artifacts: []software.Artifact{{Locator: archive.locator, SHA256: archive.sha256, Size: archive.size, UnpackedSize: archive.unpackedSize, InstalledEntries: archive.installedEntries, Format: "tar.gz", ArchiveRoot: "bundle"}},
	}
}

type memoryReader struct {
	content map[string][]byte
	opens   int
}

func (r *memoryReader) Open(ctx context.Context, locator string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, ok := r.content[locator]
	if !ok {
		return nil, errors.New("unknown test artifact")
	}
	r.opens++
	return io.NopCloser(bytes.NewReader(data)), nil
}
