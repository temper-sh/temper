package catalogcmd_test

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
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/catalogcmd"
	"github.com/temper-sh/temper/internal/datadir"
	"github.com/temper-sh/temper/internal/fetch"
	"github.com/temper-sh/temper/internal/hfcache"
	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/probecmd"
	"github.com/temper-sh/temper/internal/render"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/adapter/upstreamrelease"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/softwarecmd"
)

const utilityPreset = "qwen3.5-4b-q4km-off"

func TestUtilityExecutionKeepsHelperAvailableWithoutLocalForeground(t *testing.T) {
	raw, err := os.ReadFile("../testfixture/testdata/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	document, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := catalog.CompilePreset(document, utilityPreset, 0, 0, software.Target{OS: "darwin", Arch: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	projection, err := locked.Projections()
	if err != nil {
		t.Fatal(err)
	}
	mode := projection.Manifest.Modes[utilityPreset]
	if !mode.ExternalForeground || mode.Foreground != "" || len(mode.Members.Resident) != 0 || len(mode.Members.OnDemand) != 1 || mode.Members.OnDemand[0].Layout != utilityPreset {
		t.Fatalf("utility projection selected a local foreground: %+v", mode)
	}
	prediction, err := setup.PredictBudget(projection.Manifest, mode,
		budget.Machine{PhysicalMiB: 32768, DeviceMiB: 24576, WiredLimitMiB: 24576, WiredSource: budget.WiredSourceMetal}, map[string]int64{})
	if err != nil || prediction.Status != budget.StatusNotApplicable || prediction.Holder != "" {
		t.Fatalf("helper-only resident budget: %+v %v", prediction, err)
	}
}

// TestExecutionPreparesRendersAndBindsExactMaterial runs every primitive for
// real against synthetic release archives and model bytes; only the network
// transports, machine facts and binary identity are fixtures.
func TestExecutionPreparesRendersAndBindsExactMaterial(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	world := newExecutionWorld(t, parent)
	base := []string{"--lock", world.lockPath, "--root", root, "--installation", "fixture", "--json"}

	var output, diagnostics bytes.Buffer
	if code := catalogcmd.Runtime(context.Background(), append([]string{"prepare"}, base...), &output, &diagnostics, world.effects); code != 0 {
		t.Fatalf("prepare failed: %s", &diagnostics)
	}
	if want := []string{"software install", "fetch " + utilityPreset, "software check"}; !reflect.DeepEqual(world.operations, want) {
		t.Fatalf("prepare effects = %v, want %v", world.operations, want)
	}
	var material map[string]any
	if err := json.Unmarshal(output.Bytes(), &material); err != nil {
		t.Fatalf("prepare output is not one JSON document: %v\n%s", err, &output)
	}
	generation, _ := material["generation"].(string)
	binding, _ := material["binding"].(string)
	if material["schema"] != "temper-execution-material/v1" || len(generation) != 64 || !strings.HasPrefix(binding, "schema: temper-field-kit-binding/v1\n") {
		t.Fatalf("material = %v", material)
	}
	config, err := os.ReadFile(filepath.Join(root, "rendered", "generations", generation, "llama-swap", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), `"`+utilityPreset+`"`) || strings.Contains(string(config), "routing:") {
		t.Fatalf("helper route leaked a local default: %s", config)
	}

	// Rendering a candidate never repeats installation or fetch.
	world.operations, world.downloads = nil, 0
	output.Reset()
	if code := catalogcmd.Runtime(context.Background(), append([]string{"render"}, base...), &output, &diagnostics, world.effects); code != 0 {
		t.Fatalf("render failed: %s", &diagnostics)
	}
	if want := []string{"software check"}; !reflect.DeepEqual(world.operations, want) || world.downloads != 0 {
		t.Fatalf("render effects = %v, downloads = %d", world.operations, world.downloads)
	}
	if err := json.Unmarshal(output.Bytes(), &material); err != nil || material["generation"] != generation {
		t.Fatalf("render changed the generation: %v %v", material, err)
	}

	if runtime.GOOS == "darwin" {
		output.Reset()
		args := append(append([]string{"serve"}, base...), "--generation", generation, "--status-file", filepath.Join(parent, "status.json"))
		if code := catalogcmd.Runtime(context.Background(), args, &output, &diagnostics, world.effects); code != 0 {
			t.Fatalf("serve failed: %s", &diagnostics)
		}
		if world.served == nil || world.served.Generation != generation || world.served.Installation != "fixture" || !reflect.DeepEqual(world.served.SoftwareLock, world.software) {
			t.Fatalf("serve lost its exact identities: %+v", world.served)
		}
	}

	output.Reset()
	if code := catalogcmd.Runtime(context.Background(), append([]string{"remove"}, base...), &output, &diagnostics, world.effects); code != 0 {
		t.Fatalf("remove failed: %s", &diagnostics)
	}
	var removal map[string]any
	if err := json.Unmarshal(output.Bytes(), &removal); err != nil || removal["schema"] != "temper-execution-removal/v1" || removal["installation"] != "fixture" {
		t.Fatalf("remove output = %q", &output)
	}
}

func TestExecutionServeRejectsWrongGenerationAndChangedBytesBeforeEffects(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("supervised serving currently requires macOS")
	}
	parent := t.TempDir()
	lock := filepath.Join(parent, "execution.lock.json")
	invoke(t, compileArgs(lock)...)
	raw, err := os.ReadFile(lock)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := catalog.ParseLock(raw)
	if err != nil {
		t.Fatal(err)
	}
	projections, err := locked.Projections()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "root")
	bundle, err := render.Build(render.Inputs{StartPort: datadir.StartPort(root), Manifest: projections.Manifest, Lock: projections.Artifacts, Mode: locked.Preset, Root: root})
	if err != nil {
		t.Fatal(err)
	}
	base := []string{"serve", "--lock", lock, "--root", root, "--installation", "fixture", "--status-file", filepath.Join(parent, "status.json"), "--generation"}
	called := false
	effects := catalogcmd.Effects{Serve: func(context.Context, probecmd.Options, probecmd.Supervision, io.Writer, io.Writer) error {
		called = true
		return nil
	}}
	var out, diagnostics bytes.Buffer
	if code := catalogcmd.Runtime(context.Background(), append(base, strings.Repeat("a", 64)), &out, &diagnostics, effects); code == 0 || called {
		t.Fatal("wrong generation admitted")
	}
	for _, artifact := range bundle.Artifacts {
		path := filepath.Join(root, "rendered", "generations", bundle.Digest(), artifact.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, artifact.Data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	args := append(base, bundle.Digest())
	if code := catalogcmd.Runtime(context.Background(), args, &out, &diagnostics, effects); code != 0 || !called {
		t.Fatal("valid generation refused", &diagnostics)
	}
	called = false
	path := filepath.Join(root, "rendered", "generations", bundle.Digest(), bundle.Artifacts[0].Path)
	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := catalogcmd.Runtime(context.Background(), args, &out, &diagnostics, effects); code == 0 || called {
		t.Fatal("changed generation admitted")
	}
}

func TestExecutionInspectionAndDryRunDoNotWriteOrInvokeEffects(t *testing.T) {
	parent := t.TempDir()
	lock := filepath.Join(parent, "execution.lock.json")
	invoke(t, compileArgs(lock)...)
	refuse := refusingSoftware{t}
	effects := catalogcmd.Effects{
		Software: refuse,
		Fetch: func(context.Context, string, string, manifest.Document, lockfile.Document) error {
			t.Fatal("read-only operation fetched")
			return nil
		},
		Serve: func(context.Context, probecmd.Options, probecmd.Supervision, io.Writer, io.Writer) error {
			t.Fatal("read-only operation served")
			return nil
		},
	}
	for _, args := range [][]string{
		{"inspect", "--lock", lock, "--json"},
		{"paths", "--lock", lock, "--root", filepath.Join(parent, "installation"), "--installation", "fixture", "--dry-run", "--json"},
		{"prepare", "--lock", lock, "--root", filepath.Join(parent, "installation"), "--installation", "fixture", "--dry-run", "--json"},
	} {
		var out, diagnostics bytes.Buffer
		code := catalogcmd.Runtime(context.Background(), args, &out, &diagnostics, effects)
		if code != 0 || !json.Valid(out.Bytes()) {
			t.Fatalf("exit %d: %s %s", code, &out, &diagnostics)
		}
		if args[0] == "inspect" {
			var result map[string]any
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result["preset"] != "qwen3.8-27b-q4xl-mtp" || !reflect.DeepEqual(result["presets"], []any{"qwen3.8-27b-q4xl-mtp"}) {
				t.Fatalf("inspection lost preset identity: %v", result)
			}
			for _, retired := range []string{"profile", "layouts"} {
				if _, exists := result[retired]; exists {
					t.Fatalf("inspection retains retired field %s", retired)
				}
			}
		}
		entries, _ := os.ReadDir(parent)
		if len(entries) != 1 {
			t.Fatal("read-only operation wrote files")
		}
	}
}

func TestExecutionFailureStopsSubsequentEffectsAndReportsTheStep(t *testing.T) {
	parent := t.TempDir()
	lock := filepath.Join(parent, "execution.lock.json")
	invoke(t, compileArgs(lock)...)
	fetched := false
	effects := catalogcmd.Effects{
		Software: failingInstall{},
		Fetch: func(context.Context, string, string, manifest.Document, lockfile.Document) error {
			fetched = true
			return nil
		},
	}
	var out, diagnostics bytes.Buffer
	code := catalogcmd.Runtime(context.Background(), []string{"prepare", "--lock", lock, "--root", filepath.Join(parent, "root"), "--installation", "fixture"}, &out, &diagnostics, effects)
	if code == 0 || fetched || out.Len() != 0 || !strings.Contains(diagnostics.String(), "injected install report") || !strings.Contains(diagnostics.String(), "failed at software install") {
		t.Fatalf("exit %d, fetched %v, stdout %q, stderr %q", code, fetched, &out, &diagnostics)
	}
	if _, err := os.Lstat(filepath.Join(parent, "root")); !os.IsNotExist(err) {
		t.Fatalf("failed preparation created the root: %v", err)
	}
}

// executionWorld is one hermetic Temper host: a catalog whose router, engine
// and model bytes are synthetic, a real software command over an in-memory
// release transport, and a real fetch over a fake HF client.
type executionWorld struct {
	lockPath   string
	software   softwarelock.Document
	effects    catalogcmd.Effects
	operations []string
	downloads  int
	served     *probecmd.Options
}

func newExecutionWorld(t *testing.T, parent string) *executionWorld {
	t.Helper()
	raw, err := os.ReadFile("../testfixture/testdata/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	document, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return executionWorldFor(t, parent, document, utilityPreset, "bin/llama-server")
}

func executionWorldFor(t *testing.T, parent string, document catalog.Document, id, executable string) *executionWorld {
	t.Helper()
	transport := releaseTransport{}
	router := document.Runtime.Router
	router.Release = transport.release(t, *router.Source, *router.Release, "llama-swap", "llama-swap")
	document.Runtime.Router = router
	selected := document.Presets[id]
	for i := range selected.Profiles {
		selected.Profiles[i].Memory = nil
	}
	document.Presets[id] = selected
	engine := document.Engines[selected.Engine]
	engine.Supply.Release = transport.release(t, *engine.Supply.Source, *engine.Supply.Release, engine.Supply.Package, executable)
	document.Engines[selected.Engine] = engine
	weights := []byte("utility weights")
	sum := sha256.Sum256(weights)
	for key, artifact := range document.Artifacts {
		if key != selected.Artifact && key != selected.Speculation.DraftArtifact {
			continue
		}
		for i := range artifact.Files {
			artifact.Files[i].Bytes = int64(len(weights))
			artifact.Files[i].SHA256 = hex.EncodeToString(sum[:])
		}
		document.Artifacts[key] = artifact
	}
	locked, err := catalog.CompilePreset(document, id, 0, 0, software.Target{OS: "darwin", Arch: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := catalog.MarshalLock(locked)
	if err != nil {
		t.Fatal(err)
	}
	world := &executionWorld{lockPath: filepath.Join(parent, "execution.lock.json")}
	if err := os.WriteFile(world.lockPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	projection, err := locked.Projections()
	if err != nil {
		t.Fatal(err)
	}
	world.software = projection.Software

	member, err := upstreamrelease.NewInstallationAdapter(transport)
	if err != nil {
		t.Fatal(err)
	}
	family, err := adapter.NewInstallationFamily(member)
	if err != nil {
		t.Fatal(err)
	}
	invocations := 0
	command, err := softwarecmd.New(family, func(context.Context) (software.Target, error) {
		return software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "26.0"}, nil
	}, func() (string, error) {
		invocations++
		return "software-test-" + strings.Repeat("a", invocations), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(parent, "hf")
	download := func(_ context.Context, e hfcache.Entry, cache string) error {
		world.downloads++
		blob := filepath.Join(cache, "models--"+strings.ReplaceAll(e.Repo, "/", "--"), "blobs", e.SHA256)
		if err := os.MkdirAll(filepath.Dir(blob), 0o755); err != nil {
			return err
		}
		return os.WriteFile(blob, weights, 0o644)
	}
	world.effects = catalogcmd.Effects{
		Software: recordingSoftware{command: command, operations: &world.operations},
		Fetch: func(ctx context.Context, root, layout string, document manifest.Document, locked lockfile.Document) error {
			world.operations = append(world.operations, "fetch "+layout)
			_, err := fetch.Run(ctx, fetch.Options{Root: root, Layout: layout, Manifest: document, Lock: locked, HFCache: cache, DownloadModel: download}, nil)
			return err
		},
		Serve: func(_ context.Context, options probecmd.Options, _ probecmd.Supervision, _, _ io.Writer) error {
			world.served = &options
			return nil
		},
		Facts: func(context.Context) (machine.Facts, error) {
			return machine.Facts{
				Schema: machine.FactsSchemaV1, Target: software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "26.0"},
				HardwareModel: "MacTest,1", Chip: "Apple test chip", OSBuild: "25A1",
				PhysicalMemoryBytes: 32 << 30, MetalDeviceMemoryMiB: 24576, MetalDeviceMemorySource: machine.MetalDeviceSourceLive,
				WiredLimitMiB: 24576, WiredLimitSource: budget.WiredSourceMetal,
			}, nil
		},
		Binary: func() ([]byte, error) { return []byte("temper test binary"), nil },
	}
	return world
}

// releaseTransport serves synthetic release archives by locator.
type releaseTransport map[string][]byte

func (r releaseTransport) Open(ctx context.Context, locator string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, ok := r[locator]
	if !ok {
		return nil, errors.New("unknown test artifact")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// release replaces a catalog release artifact with a tiny archive holding one
// executable below archiveRoot.
func (r releaseTransport) release(t *testing.T, source upstreamrelease.GitHubSource, original upstreamrelease.Release, archiveRoot, executable string) *upstreamrelease.Release {
	t.Helper()
	const payload = "synthetic executable\n"
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gzipWriter)
	headers := []tar.Header{{Name: archiveRoot + "/", Mode: 0o755, Typeflag: tar.TypeDir}}
	if dir := filepath.Dir(executable); dir != "." {
		headers = append(headers, tar.Header{Name: archiveRoot + "/" + dir + "/", Mode: 0o755, Typeflag: tar.TypeDir})
	}
	headers = append(headers, tar.Header{Name: archiveRoot + "/" + executable, Mode: 0o755, Typeflag: tar.TypeReg, Size: int64(len(payload))})
	for index := range headers {
		if err := tarWriter.WriteHeader(&headers[index]); err != nil {
			t.Fatal(err)
		}
		if headers[index].Typeflag == tar.TypeReg {
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
	release := original
	release.Artifact = software.Artifact{
		Locator: source.ReleaseURL(original.Version), SHA256: hex.EncodeToString(sum[:]),
		Size: int64(len(data)), UnpackedSize: int64(len(payload)), InstalledEntries: len(headers) - 1,
		Format: "tar.gz", ArchiveRoot: archiveRoot,
	}
	r[release.Artifact.Locator] = data
	return &release
}

type recordingSoftware struct {
	command    softwarecmd.Command
	operations *[]string
}

func (r recordingSoftware) Install(ctx context.Context, root, installation string, lock softwarelock.Document, out io.Writer) error {
	*r.operations = append(*r.operations, "software install")
	return r.command.Install(ctx, root, installation, lock, out)
}

func (r recordingSoftware) Check(ctx context.Context, root, installation string, lock softwarelock.Document, out io.Writer) error {
	*r.operations = append(*r.operations, "software check")
	return r.command.Check(ctx, root, installation, lock, out)
}

func (r recordingSoftware) Remove(ctx context.Context, root, installation string, lock softwarelock.Document, out io.Writer) error {
	*r.operations = append(*r.operations, "software remove")
	return r.command.Remove(ctx, root, installation, lock, out)
}

type refusingSoftware struct{ t *testing.T }

func (r refusingSoftware) Install(context.Context, string, string, softwarelock.Document, io.Writer) error {
	r.t.Fatal("read-only operation installed software")
	return nil
}

func (r refusingSoftware) Check(context.Context, string, string, softwarelock.Document, io.Writer) error {
	r.t.Fatal("read-only operation checked software")
	return nil
}

func (r refusingSoftware) Remove(context.Context, string, string, softwarelock.Document, io.Writer) error {
	r.t.Fatal("read-only operation removed software")
	return nil
}

type failingInstall struct{ refusingSoftware }

func (failingInstall) Install(_ context.Context, _, _ string, _ softwarelock.Document, out io.Writer) error {
	_, _ = io.WriteString(out, "injected install report\n")
	return errors.New("injected install failure")
}
