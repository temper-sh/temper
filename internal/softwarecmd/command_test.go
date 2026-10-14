package softwarecmd_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/adapter/upstreamrelease"

	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/softwarecmd"
)

var commandTarget = software.Target{
	OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6",
}

// report is the subset of the --json reports these tests read.
type report struct {
	Schema       string `json:"schema"`
	Status       string `json:"status"`
	Installation string `json:"installation"`
	Counts       struct {
		Packages, Units, Effects, Claims, Requirements, Problems int
	} `json:"counts"`
	Packages []struct {
		ID, Method, Adapter string
		RootUnit            string `json:"root_unit"`
	} `json:"packages"`
	Effects []struct {
		ID, Effect string
		Units      []struct{ ID, Action, Ownership, Claim string } `json:"units"`
	} `json:"effects"`
	Units    []struct{ ID, Status, Adapter, Scope, Location, Ownership, Claim string } `json:"units"`
	Problems []struct{ Code, Unit, Detail string }                                     `json:"problems"`
}

func decodeReport(t *testing.T, raw []byte) report {
	t.Helper()
	var r report
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&r); err != nil || decoder.More() {
		t.Fatalf("stdout is not one JSON document (%v):\n%s", err, raw)
	}
	return r
}

// oneUnit returns the single unit of a report with exactly one effect group.
func oneUnit(t *testing.T, r report) (string, struct{ ID, Action, Ownership, Claim string }) {
	t.Helper()
	if len(r.Effects) != 1 || len(r.Effects[0].Units) != 1 {
		t.Fatalf("want one effect with one unit: %+v", r.Effects)
	}
	return r.Effects[0].Effect, r.Effects[0].Units[0]
}

func TestCommandRunsTheReleaseInstallCheckRemoveRoundTripAndCleanSecondRuns(t *testing.T) {
	workspace := t.TempDir()
	root := filepath.Join(workspace, "temper-root")
	archive := makeReleaseArchive(t)
	lockPath := writeSoftwareLock(t, workspace, releaseLock(t, archive))
	reader := &memoryReader{content: map[string][]byte{archive.locator: archive.data}}
	command := releaseCommand(t, reader, commandTarget)
	arguments := []string{"--root", root, "--installation", "field-kit-base", "--lock", lockPath, "--json"}
	const unitID = "upstream-release:llama-cpp"

	var stdout, stderr bytes.Buffer
	exit := command.Run(context.Background(), append([]string{"install"}, append(arguments, "--dry-run")...), &stdout, &stderr)
	if exit != 0 || stderr.Len() != 0 {
		t.Fatalf("dry install exit = %d, stderr = %q", exit, stderr.String())
	}
	dry := decodeReport(t, stdout.Bytes())
	effect, unit := oneUnit(t, dry)
	if dry.Schema != "temper-software-install/v1" || dry.Status != "would-change" || dry.Installation != "field-kit-base" ||
		dry.Counts.Packages != 1 || dry.Counts.Units != 1 || dry.Counts.Effects != 1 || dry.Counts.Claims != 0 ||
		len(dry.Packages) != 1 || dry.Packages[0].ID != "llama-cpp" || dry.Packages[0].Method != "release-artifact" ||
		dry.Packages[0].Adapter != "upstream-release" || dry.Packages[0].RootUnit != unitID ||
		effect != "publish-isolated" || unit.ID != unitID || unit.Action != "add" || unit.Ownership != "temper-added" || unit.Claim != "none" {
		t.Fatalf("dry install report: %+v", dry)
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
	if exit != 0 || stderr.Len() != 0 || decodeReport(t, stdout.Bytes()).Status != "changed" {
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
	second := decodeReport(t, stdout.Bytes())
	effect, unit = oneUnit(t, second)
	if second.Status != "unchanged" || second.Counts.Effects != 0 || effect != "unchanged" || unit.Action != "preserve" || unit.Ownership != "temper-added" {
		t.Fatalf("second install report: %+v", second)
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
	check := decodeReport(t, stdout.Bytes())
	if check.Status != "exact" || check.Counts.Problems != 0 || len(check.Units) != 1 || check.Units[0].ID != unitID ||
		check.Units[0].Status != "exact" || check.Units[0].Scope != "llama-cpp" || !strings.HasPrefix(check.Units[0].Location, root) ||
		check.Units[0].Ownership != "temper-added" {
		t.Fatalf("check report: %+v", check)
	}

	stdout.Reset()
	stderr.Reset()
	exit = command.Run(context.Background(), []string{"check", "--root", root, "--installation", "field-kit-base", "--lock", lockPath}, &stdout, &stderr)
	if exit != 0 || stdout.String() != "Installation field-kit-base matches its lock (1 package).\n" {
		t.Fatalf("plain check exit = %d, stdout = %q", exit, stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	exit = command.Run(context.Background(), append([]string{"remove"}, arguments...), &stdout, &stderr)
	if exit != 0 || stderr.Len() != 0 {
		t.Fatalf("remove exit = %d, stderr = %q", exit, stderr.String())
	}
	removed := decodeReport(t, stdout.Bytes())
	effect, unit = oneUnit(t, removed)
	if removed.Schema != "temper-software-remove/v1" || removed.Status != "changed" || removed.Counts.Effects != 1 ||
		effect != "remove" || unit.Action != "remove" || unit.Ownership != "temper-added" || unit.Claim != "none" {
		t.Fatalf("remove report: %+v", removed)
	}

	stdout.Reset()
	stderr.Reset()
	exit = command.Run(context.Background(), []string{"remove", "--root", root, "--installation", "field-kit-base", "--lock", lockPath}, &stdout, &stderr)
	if exit != 0 || stderr.Len() != 0 || stdout.String() != "Nothing to remove from installation field-kit-base.\n" {
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
		"check", "--root", root, "--installation", "field-kit-base", "--lock", lockPath, "--json",
	}, &stdout, &stderr)
	if exit != 1 || stderr.Len() != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	r := decodeReport(t, stdout.Bytes())
	if r.Status != "findings" || r.Counts.Problems != 1 || len(r.Units) != 1 || r.Units[0].Status != "missing" ||
		r.Units[0].Location != "" || r.Units[0].Ownership != "unknown" || len(r.Problems) != 1 || r.Problems[0].Code != "provider-missing" {
		t.Fatalf("check findings report: %+v", r)
	}
	stdout.Reset()
	if exit = command.Run(context.Background(), []string{"check", "--root", root, "--installation", "field-kit-base", "--lock", lockPath}, &stdout, &stderr); exit != 1 ||
		!strings.HasPrefix(stdout.String(), "Installation field-kit-base differs from its lock (1 problem):\n  - ") {
		t.Fatalf("plain findings: %d %q", exit, stdout.String())
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
	}, func() (string, error) { return "test-run", nil })
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
	})
	if err != nil {
		t.Fatal(err)
	}
	return command
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
