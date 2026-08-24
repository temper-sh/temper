package softwarecmd_test

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
	"time"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/adapter/upstreamrelease"
	"github.com/temper-sh/temper/internal/software/catalogupdate"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/softwarecmd"
)

var commandTarget = software.Target{
	OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6",
}

func TestCommandRunsTheReleaseInstallCheckRemoveRoundTripAndCleanSecondRuns(t *testing.T) {
	workspace := t.TempDir()
	root := filepath.Join(workspace, "temper-root")
	archive := makeReleaseArchive(t)
	lockPath := writeSoftwareLock(t, workspace, releaseLock(t, archive))
	reader := &memoryReader{content: map[string][]byte{archive.locator: archive.data}}
	command := releaseCommand(t, reader, commandTarget)
	arguments := []string{"--root", root, "--installation", "field-kit-base", "--lock", lockPath}

	var stdout, stderr bytes.Buffer
	exit := command.Run(context.Background(), append([]string{"install"}, append(arguments, "--dry-run")...), &stdout, &stderr)
	if exit != 0 || stderr.Len() != 0 {
		t.Fatalf("dry install exit = %d, stderr = %q", exit, stderr.String())
	}
	wantDry := "RESULT software-install would-change installation=field-kit-base packages=1 units=1 effects=1 claims=0\n" +
		"PACKAGE llama-cpp method=release-artifact adapter=upstream-release root-unit=upstream-release:llama-cpp\n" +
		"EFFECT upstream-release:llama-cpp publish-isolated units=1\n" +
		"UNIT upstream-release:llama-cpp add ownership=temper-added claim=none\n"
	if stdout.String() != wantDry {
		t.Fatalf("dry install stdout:\n%s\nwant:\n%s", stdout.String(), wantDry)
	}
	if reader.opens != 0 {
		t.Fatalf("dry install opened %d artifacts", reader.opens)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("dry install created the root: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	exit = command.Run(context.Background(), append([]string{"install"}, arguments...), &stdout, &stderr)
	if exit != 0 || stderr.Len() != 0 || !strings.HasPrefix(stdout.String(), "RESULT software-install changed ") {
		t.Fatalf("install exit = %d, stdout = %q, stderr = %q", exit, stdout.String(), stderr.String())
	}
	if reader.opens != 1 {
		t.Fatalf("install opened %d artifacts, want 1", reader.opens)
	}

	stdout.Reset()
	stderr.Reset()
	exit = command.Run(context.Background(), append([]string{"install"}, arguments...), &stdout, &stderr)
	if exit != 0 || stderr.Len() != 0 {
		t.Fatalf("second install exit = %d, stderr = %q", exit, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "RESULT software-install unchanged installation=field-kit-base packages=1 units=1 effects=0 claims=0\n") ||
		!strings.Contains(stdout.String(), "EFFECT upstream-release:llama-cpp unchanged units=1\n") ||
		!strings.Contains(stdout.String(), "UNIT upstream-release:llama-cpp preserve ownership=temper-added claim=none\n") {
		t.Fatalf("second install stdout:\n%s", stdout.String())
	}
	if reader.opens != 1 {
		t.Fatalf("second install reopened an artifact: %d", reader.opens)
	}

	stdout.Reset()
	stderr.Reset()
	exit = command.Run(context.Background(), append([]string{"check"}, arguments...), &stdout, &stderr)
	if exit != 0 || stderr.Len() != 0 {
		t.Fatalf("check exit = %d, stderr = %q", exit, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "RESULT software-check exact installation=field-kit-base packages=1 units=1 requirements=0 problems=0 receipt=") ||
		!strings.Contains(stdout.String(), "UNIT upstream-release:llama-cpp exact adapter=upstream-release scope=llama-cpp location="+root) ||
		!strings.Contains(stdout.String(), " ownership=temper-added claim=none\n") {
		t.Fatalf("check stdout:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	exit = command.Run(context.Background(), append([]string{"remove"}, arguments...), &stdout, &stderr)
	if exit != 0 || stderr.Len() != 0 {
		t.Fatalf("remove exit = %d, stderr = %q", exit, stderr.String())
	}
	wantRemove := "RESULT software-remove changed installation=field-kit-base packages=1 units=1 effects=1 claims=0\n" +
		"EFFECT upstream-release:llama-cpp remove units=1\n" +
		"UNIT upstream-release:llama-cpp remove ownership=temper-added claim=none\n"
	if stdout.String() != wantRemove {
		t.Fatalf("remove stdout:\n%s\nwant:\n%s", stdout.String(), wantRemove)
	}

	stdout.Reset()
	stderr.Reset()
	exit = command.Run(context.Background(), append([]string{"remove"}, arguments...), &stdout, &stderr)
	if exit != 0 || stderr.Len() != 0 || stdout.String() != "RESULT software-remove unchanged installation=field-kit-base packages=1 units=1 effects=0 claims=0\n" {
		t.Fatalf("second remove exit = %d, stdout = %q, stderr = %q", exit, stdout.String(), stderr.String())
	}
}

func TestCommandReportsCheckFindingsOnStdoutAndExitOneWithoutCreatingARoot(t *testing.T) {
	workspace := t.TempDir()
	root := filepath.Join(workspace, "absent-root")
	archive := makeReleaseArchive(t)
	lockPath := writeSoftwareLock(t, workspace, releaseLock(t, archive))
	command := releaseCommand(t, &memoryReader{content: map[string][]byte{}}, commandTarget)
	var stdout, stderr bytes.Buffer

	exit := command.Run(context.Background(), []string{
		"check", "--root", root, "--installation", "field-kit-base", "--lock", lockPath,
	}, &stdout, &stderr)
	if exit != 1 || stderr.Len() != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "RESULT software-check findings installation=field-kit-base packages=1 units=1 requirements=0 problems=1 receipt=none\n") ||
		!strings.Contains(stdout.String(), "UNIT upstream-release:llama-cpp missing adapter=upstream-release scope=llama-cpp location=none ownership=unknown claim=none\n") ||
		!strings.Contains(stdout.String(), "PROBLEM code=provider-missing") {
		t.Fatalf("check findings stdout:\n%s", stdout.String())
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("check created the root: %v", err)
	}
}

func TestCommandRefusesAHostTargetMismatchWithoutAResult(t *testing.T) {
	workspace := t.TempDir()
	archive := makeReleaseArchive(t)
	lockPath := writeSoftwareLock(t, workspace, releaseLock(t, archive))
	host := commandTarget
	host.DistributionVersion = "15.7"
	command := releaseCommand(t, &memoryReader{content: map[string][]byte{}}, host)
	var stdout, stderr bytes.Buffer

	exit := command.Run(context.Background(), []string{
		"install", "--root", filepath.Join(workspace, "root"), "--installation", "field-kit-base", "--lock", lockPath, "--dry-run",
	}, &stdout, &stderr)
	if exit != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "does not match host target") {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", exit, stdout.String(), stderr.String())
	}
}

func TestCommandRefusesAnUncompiledLockedAdapterWithoutFallback(t *testing.T) {
	workspace := t.TempDir()
	archive := makeReleaseArchive(t)
	document := releaseLock(t, archive)
	unit := document.Units["upstream-release:llama-cpp"]
	delete(document.Units, "upstream-release:llama-cpp")
	unit.Adapter = "homebrew"
	unit.Scope = "system"
	document.Units["homebrew:llama-cpp"] = unit
	selection := document.Selections["llama-cpp"]
	selection.Method = "system-package"
	selection.Adapter = "homebrew"
	selection.RootUnit = "homebrew:llama-cpp"
	document.Selections["llama-cpp"] = selection
	lockPath := writeSoftwareLock(t, workspace, document)
	command := releaseCommand(t, &memoryReader{content: map[string][]byte{}}, commandTarget)
	var stdout, stderr bytes.Buffer

	exit := command.Run(context.Background(), []string{
		"install", "--root", filepath.Join(workspace, "root"), "--installation", "field-kit-base", "--lock", lockPath, "--dry-run",
	}, &stdout, &stderr)
	if exit != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), `software lock adapter "homebrew" is not compiled into this binary`) {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", exit, stdout.String(), stderr.String())
	}
}

func TestCommandRefusesUsageBeforeReadingTheHost(t *testing.T) {
	family, err := adapter.NewInstallationFamily()
	if err != nil {
		t.Fatal(err)
	}
	detectCalls := 0
	command, err := softwarecmd.New(family, func(context.Context) (software.Target, error) {
		detectCalls++
		return software.Target{}, errors.New("unexpected detection")
	}, func() (string, error) { return "test-run", nil }, unexpectedCatalogUpdate)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer

	exit := command.Run(context.Background(), []string{"install", "--dry-run"}, &stdout, &stderr)
	if exit != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "--root and --installation are required") {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", exit, stdout.String(), stderr.String())
	}
	if detectCalls != 0 {
		t.Fatalf("invalid usage read the host %d times", detectCalls)
	}
}

func TestCommandRunsTheBoundedCatalogUpdateSurface(t *testing.T) {
	family, err := adapter.NewInstallationFamily()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "temper-root")
	var gotOptions catalogupdate.Options
	command, err := softwarecmd.New(
		family,
		func(context.Context) (software.Target, error) {
			return software.Target{}, errors.New("catalog update must not detect the host")
		},
		func() (string, error) { return "unused", errors.New("catalog update must not create an invocation id") },
		func(ctx context.Context, options catalogupdate.Options) (catalogupdate.Result, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 31*time.Second {
				t.Fatalf("catalog update deadline = %v, present %v", deadline, ok)
			}
			gotOptions = options
			return catalogupdate.Result{
				Changed: true, DryRun: options.DryRun, Channel: options.Channel, Sequence: 7,
				SHA256: strings.Repeat("a", 64), ChannelKeyID: "temper-catalog-2026-01", CatalogKeyID: "temper-catalog-2026-01",
			}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	exit := command.Run(context.Background(), []string{
		"catalog", "update", "--root", root, "--dry-run",
	}, &stdout, &stderr)
	if exit != 0 || stderr.Len() != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	if gotOptions != (catalogupdate.Options{Root: root, Channel: "stable", DryRun: true}) {
		t.Fatalf("catalog options = %#v", gotOptions)
	}
	want := "RESULT software-catalog-update would-change channel=stable sequence=7 sha256=" + strings.Repeat("a", 64) + " channel-key=temper-catalog-2026-01 catalog-key=temper-catalog-2026-01\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestCommandRefusesCatalogUsageBeforeCallingTheUpdater(t *testing.T) {
	family, err := adapter.NewInstallationFamily()
	if err != nil {
		t.Fatal(err)
	}
	updateCalls := 0
	command, err := softwarecmd.New(
		family,
		func(context.Context) (software.Target, error) { return software.Target{}, errors.New("unused") },
		func() (string, error) { return "unused", errors.New("unused") },
		func(context.Context, catalogupdate.Options) (catalogupdate.Result, error) {
			updateCalls++
			return catalogupdate.Result{}, errors.New("unexpected catalog update")
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	exit := command.Run(context.Background(), []string{"catalog", "update", "--dry-run"}, &stdout, &stderr)
	if exit != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "--root is required") {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", exit, stdout.String(), stderr.String())
	}
	if updateCalls != 0 {
		t.Fatalf("invalid usage called updater %d times", updateCalls)
	}
}

func releaseCommand(t *testing.T, reader *memoryReader, target software.Target) softwarecmd.Command {
	t.Helper()
	member, err := upstreamrelease.NewInstallationAdapter(reader)
	if err != nil {
		t.Fatal(err)
	}
	family, err := adapter.NewInstallationFamily(member)
	if err != nil {
		t.Fatal(err)
	}
	invocation := 0
	command, err := softwarecmd.New(family, func(context.Context) (software.Target, error) {
		return target, nil
	}, func() (string, error) {
		invocation++
		return fmtInvocation(invocation), nil
	}, unexpectedCatalogUpdate)
	if err != nil {
		t.Fatal(err)
	}
	return command
}

func unexpectedCatalogUpdate(context.Context, catalogupdate.Options) (catalogupdate.Result, error) {
	return catalogupdate.Result{}, errors.New("unexpected catalog update")
}

func fmtInvocation(index int) string {
	const digits = "0123456789"
	if index < 0 || index >= len(digits) {
		panic("test invocation index outside one digit")
	}
	return "software-test-" + string(digits[index])
}

type archiveFixture struct {
	data     []byte
	locator  string
	sha256   string
	size     int64
	unpacked int64
	entries  int
}

func makeReleaseArchive(t *testing.T) archiveFixture {
	t.Helper()
	const payload = "release-binary\n"
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gzipWriter)
	entries := []tar.Header{
		{Name: "bundle/", Mode: 0o755, Typeflag: tar.TypeDir},
		{Name: "bundle/bin/", Mode: 0o755, Typeflag: tar.TypeDir},
		{Name: "bundle/bin/llama-server", Mode: 0o755, Typeflag: tar.TypeReg, Size: int64(len(payload))},
	}
	for index := range entries {
		if err := tarWriter.WriteHeader(&entries[index]); err != nil {
			t.Fatal(err)
		}
		if entries[index].Typeflag == tar.TypeReg {
			if _, err := io.WriteString(tarWriter, payload); err != nil {
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
	data := append([]byte(nil), output.Bytes()...)
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	return archiveFixture{
		data: data, locator: "https://example.invalid/releases/" + digest[:12] + ".tar.gz", sha256: digest,
		size: int64(len(data)), unpacked: int64(len(payload)), entries: 2,
	}
}

func releaseLock(t *testing.T, archive archiveFixture) softwarelock.Document {
	t.Helper()
	const unitID = "upstream-release:llama-cpp"
	document := softwarelock.Document{
		Schema: softwarelock.SchemaV1,
		Provenance: softwarelock.Provenance{Experiment: &softwarelock.ExperimentIdentity{
			Schema: "field-kit-experiment/v1", ID: "release-command", DefinitionSHA256: strings.Repeat("a", 64),
		}},
		Target: commandTarget, Resolved: "2026-08-24",
		Selections: map[string]softwarelock.Selection{
			"llama-cpp": {
				Provenance: softwarelock.ProvenanceExperiment, Method: "release-artifact", Adapter: "upstream-release",
				RecipeRevision: "llama-cpp-release/v1", RootUnit: unitID,
			},
		},
		Units: map[string]softwarelock.Unit{
			unitID: {
				Adapter: "upstream-release", Scope: "llama-cpp", NativeName: "llama-cpp", Version: "b10566", Revision: "revision-1",
				Dependencies: []string{}, Artifacts: []software.Artifact{{
					Locator: archive.locator, SHA256: archive.sha256, Size: archive.size, UnpackedSize: archive.unpacked,
					InstalledEntries: archive.entries, Format: "tar.gz", ArchiveRoot: "bundle",
				}},
			},
		},
	}
	if err := document.Validate(); err != nil {
		t.Fatalf("release lock invalid: %v", err)
	}
	return document
}

func writeSoftwareLock(t *testing.T, directory string, document softwarelock.Document) string {
	t.Helper()
	data, err := softwarelock.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "software.lock.yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
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
