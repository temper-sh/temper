package probecmd_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/temper-sh/temper/internal/datadir"
	"github.com/temper-sh/temper/internal/probecmd"
	"github.com/temper-sh/temper/internal/runtimeconfig"
	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/installplan"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/receipt"
	"github.com/temper-sh/temper/internal/software/receiptstore"
)

type recordingRunner struct {
	called     bool
	invocation probecmd.Invocation
}

func (r *recordingRunner) Run(_ context.Context, invocation probecmd.Invocation, _, _ io.Writer) error {
	r.called = true
	r.invocation = invocation
	return nil
}

func TestPlanBindsTheReceiptedRouterToItsRenderedGeneration(t *testing.T) {
	fixture := materialize(t)
	invocation, err := probecmd.Plan(probecmd.Options{
		Root: fixture.root, Installation: "field-kit-qwen", SoftwareLock: softwareLock(t, fixture.lockPath),
		Generation: fixture.generation, Listen: "127.0.0.1:18080",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := invocation.Arguments; strings.Join(got, " ") != "--config "+fixture.config+" --listen 127.0.0.1:18080" {
		t.Fatalf("arguments = %q", got)
	}
	if invocation.Path != fixture.router || len(invocation.Environment) != 1 || !strings.HasPrefix(invocation.Environment[0], "PATH="+filepath.Dir(fixture.engine)) {
		t.Fatalf("invocation = %#v", invocation)
	}
}

func TestPlanRefusesAGenerationEngineAbsentFromTheExactReceipt(t *testing.T) {
	fixture := materialize(t)
	data, err := runtimeconfig.Marshal(runtimeconfig.Document{
		Schema: runtimeconfig.SchemaV1,
		Requirements: []runtimeconfig.Requirement{
			{Package: "llama-swap", RelativeExecutable: "llama-swap"},
			{Package: "rapid-mlx", RelativeExecutable: "bin/rapid-mlx"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(fixture.root, "rendered", "generations", fixture.generation, "runtime", "requirements.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = probecmd.Plan(probecmd.Options{
		Root: fixture.root, Installation: "field-kit-qwen", SoftwareLock: softwareLock(t, fixture.lockPath),
		Generation: fixture.generation, Listen: "127.0.0.1:8080",
	})
	if err == nil || !strings.Contains(err.Error(), `no "rapid-mlx" selection`) {
		t.Fatalf("Plan() error = %v", err)
	}
}

func TestPlanUsesReceiptedUVEngineFromItsExactEnvironment(t *testing.T) {
	fixture := materialize(t)
	lockData, err := os.ReadFile(fixture.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := softwarelock.Parse(lockData)
	if err != nil {
		t.Fatal(err)
	}

	const (
		pythonUnit = "python:rapid-mlx:cpython"
		engineUnit = "python:rapid-mlx:rapid-mlx"
	)
	pythonArtifact := software.Artifact{Locator: "https://example.invalid/cpython.tar.gz", SHA256: strings.Repeat("1", 64), Size: 1}
	wheelArtifact := software.Artifact{Locator: "https://example.invalid/rapid_mlx.whl", SHA256: strings.Repeat("2", 64), Size: 1}
	lock.Selections["rapid-mlx"] = softwarelock.Selection{
		Provenance: softwarelock.ProvenanceExperiment, Method: "python-environment", Adapter: "python",
		RecipeRevision: "field-kit/v1", RootUnit: engineUnit,
	}
	lock.Units[pythonUnit] = softwarelock.Unit{
		Adapter: "python", Scope: "rapid-mlx", NativeName: "cpython", Version: "3.13.7", Revision: "20260814",
		Dependencies: []string{}, Artifacts: []software.Artifact{pythonArtifact},
	}
	lock.Units[engineUnit] = softwarelock.Unit{
		Adapter: "python", Scope: "rapid-mlx", NativeName: "rapid-mlx", Version: "0.13.3",
		Dependencies: []string{pythonUnit}, Artifacts: []software.Artifact{wheelArtifact},
	}
	lockData, err = softwarelock.Marshal(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.lockPath, lockData, 0o644); err != nil {
		t.Fatal(err)
	}

	scopeRoot := filepath.Join(fixture.root, "software", "installations", "field-kit-qwen", "python", "rapid-mlx")
	generationRoot := filepath.Join(scopeRoot, "generations", "fixture")
	environment := filepath.Join(generationRoot, "environment")
	engine := filepath.Join(environment, "bin", "rapid-mlx")
	if err := os.MkdirAll(filepath.Dir(engine), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(engine, []byte("fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	python := filepath.Join(environment, "bin", "python3")
	if err := os.WriteFile(python, []byte("fixture interpreter"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("generations", "fixture"), filepath.Join(scopeRoot, "current")); err != nil {
		t.Fatal(err)
	}

	store, err := receiptstore.Read(fixture.root, "field-kit-qwen")
	if err != nil {
		t.Fatal(err)
	}
	receiptDocument := store.Document
	digest, err := lock.SemanticDigest()
	if err != nil {
		t.Fatal(err)
	}
	receiptDocument.SoftwareLockDigest = digest
	receiptDocument.Selections["rapid-mlx"] = receipt.Selection{
		Provenance: softwarelock.ProvenanceExperiment, Method: "python-environment", Adapter: "python",
		RecipeRevision: "field-kit/v1", RootUnit: engineUnit,
	}
	receiptDocument.Units[pythonUnit] = receipt.Unit{
		Adapter: "python", Scope: "rapid-mlx", NativeName: "cpython", Version: "3.13.7", Revision: "20260814",
		Dependencies: []string{}, Artifacts: []software.Artifact{pythonArtifact}, Location: environment, Ownership: installplan.OwnershipTemperAdded,
	}
	receiptDocument.Units[engineUnit] = receipt.Unit{
		Adapter: "python", Scope: "rapid-mlx", NativeName: "rapid-mlx", Version: "0.13.3",
		Dependencies: []string{pythonUnit}, Artifacts: []software.Artifact{wheelArtifact}, Location: environment, Ownership: installplan.OwnershipTemperAdded,
	}
	if err := store.Commit(context.Background(), receiptDocument); err != nil {
		t.Fatal(err)
	}

	requirements, err := runtimeconfig.Marshal(runtimeconfig.Document{
		Schema: runtimeconfig.SchemaV1,
		Requirements: []runtimeconfig.Requirement{
			{Package: "llama-swap", RelativeExecutable: "llama-swap"},
			{Package: "rapid-mlx", RelativeExecutable: "bin/rapid-mlx", Role: "engine", Arguments: []string{"--no-telemetry", "serve", "--port", "${PORT}"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	requirementsPath := filepath.Join(fixture.root, "rendered", "generations", fixture.generation, "runtime", "requirements.json")
	if err := os.WriteFile(requirementsPath, requirements, 0o644); err != nil {
		t.Fatal(err)
	}

	invocation, err := probecmd.Plan(probecmd.Options{
		Root: fixture.root, Installation: "field-kit-qwen", SoftwareLock: softwareLock(t, fixture.lockPath),
		Generation: fixture.generation, Listen: "127.0.0.1:8080",
	})
	if err != nil {
		t.Fatal(err)
	}
	resolvedEngine, err := filepath.EvalSymlinks(engine)
	if err != nil {
		t.Fatal(err)
	}
	if got := invocation.Environment; len(got) != 1 || !strings.HasPrefix(got[0], "PATH="+filepath.Dir(resolvedEngine)+string(os.PathListSeparator)) {
		t.Fatalf("environment = %q", got)
	}
	resolvedPython, err := filepath.EvalSymlinks(python)
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(datadir.StartPort(fixture.root))
	if invocation.EnginePath != resolvedPython || strings.Join(invocation.EngineArguments, "|") != resolvedEngine+"|--no-telemetry|serve|--port|"+port || strconv.Itoa(invocation.EnginePort) != port {
		t.Fatalf("Python process identity = %q %q", invocation.EnginePath, invocation.EngineArguments)
	}
	if invocation.PythonCrashLogSHA256 != "8b69ee2f2cebdf3943155e195d5f31a06dbc5b14818e7b40aee9ba90206e3435" {
		t.Fatal("Rapid crash-log helper is not bound to its reviewed command")
	}
}

func TestPlanRefusesDriftAndNonLoopback(t *testing.T) {
	fixture := materialize(t)
	tests := []struct {
		name string
		edit func(*probecmd.Options)
		want string
	}{
		{name: "non-loopback", edit: func(o *probecmd.Options) { o.Listen = "0.0.0.0:8080" }, want: "IPv4 loopback"},
		{name: "wrong generation", edit: func(o *probecmd.Options) { o.Generation = strings.Repeat("a", 64) }, want: "does not exist"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := probecmd.Options{Root: fixture.root, Installation: "field-kit-qwen", SoftwareLock: softwareLock(t, fixture.lockPath), Generation: fixture.generation, Listen: "127.0.0.1:8080"}
			test.edit(&options)
			if _, err := probecmd.Plan(options); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Plan() error = %v, want %q", err, test.want)
			}
		})
	}
}

type fixture struct {
	root, lockPath, generation, config, router, engine string
}

func materialize(t *testing.T) fixture {
	t.Helper()
	workspace := t.TempDir()
	root := filepath.Join(workspace, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	generation := strings.Repeat("b", 64)
	config := filepath.Join(root, "rendered", "generations", generation, "llama-swap", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("models: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	requirements, err := runtimeconfig.Marshal(runtimeconfig.Document{
		Schema: runtimeconfig.SchemaV1,
		Requirements: []runtimeconfig.Requirement{
			{Package: "llama-cpp", RelativeExecutable: "llama-server"},
			{Package: "llama-swap", RelativeExecutable: "llama-swap"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	requirementsPath := filepath.Join(root, "rendered", "generations", generation, "runtime", "requirements.json")
	if err := os.MkdirAll(filepath.Dir(requirementsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(requirementsPath, requirements, 0o644); err != nil {
		t.Fatal(err)
	}

	target := software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos"}
	artifact := software.Artifact{Locator: "https://example.invalid/archive.tar.gz", SHA256: strings.Repeat("c", 64), Size: 1}
	lock := softwarelock.Document{
		Schema: softwarelock.SchemaV1,
		Provenance: softwarelock.Provenance{Experiment: &softwarelock.ExperimentIdentity{
			Schema: "field-kit-question-package/v1", ID: "qwen38-dynamic", DefinitionSHA256: strings.Repeat("d", 64),
		}},
		Target: target, Resolved: "2026-08-27",
		Selections: map[string]softwarelock.Selection{
			"llama-cpp":  {Provenance: softwarelock.ProvenanceExperiment, Method: "release-artifact", Adapter: "upstream-release", RecipeRevision: "field-kit/v1", RootUnit: "upstream-release:engine:llama-cpp"},
			"llama-swap": {Provenance: softwarelock.ProvenanceExperiment, Method: "release-artifact", Adapter: "upstream-release", RecipeRevision: "field-kit/v1", RootUnit: "upstream-release:router:llama-swap"},
		},
		Units: map[string]softwarelock.Unit{
			"upstream-release:engine:llama-cpp":  {Adapter: "upstream-release", Scope: "engine", NativeName: "llama-cpp", Version: "b10636", Revision: strings.Repeat("e", 40), Dependencies: []string{}, Artifacts: []software.Artifact{artifact}},
			"upstream-release:router:llama-swap": {Adapter: "upstream-release", Scope: "router", NativeName: "llama-swap", Version: "v251", Revision: strings.Repeat("f", 40), Dependencies: []string{}, Artifacts: []software.Artifact{artifact}},
		},
	}
	lockData, err := softwarelock.Marshal(lock)
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(workspace, "software.lock.yaml")
	if err := os.WriteFile(lockPath, lockData, 0o644); err != nil {
		t.Fatal(err)
	}
	digest, _ := lock.SemanticDigest()
	receiptDocument := receipt.Document{
		Schema: receipt.SchemaV1, Installation: "field-kit-qwen", SoftwareLockDigest: digest,
		Target: target, Root: root, ObservedAt: time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
		Requirements: []receipt.Requirement{},
		Selections: map[string]receipt.Selection{
			"llama-cpp":  {Provenance: softwarelock.ProvenanceExperiment, Method: "release-artifact", Adapter: "upstream-release", RecipeRevision: "field-kit/v1", RootUnit: "upstream-release:engine:llama-cpp"},
			"llama-swap": {Provenance: softwarelock.ProvenanceExperiment, Method: "release-artifact", Adapter: "upstream-release", RecipeRevision: "field-kit/v1", RootUnit: "upstream-release:router:llama-swap"},
		},
		Units: map[string]receipt.Unit{},
	}
	locations := map[string]string{
		"upstream-release:engine:llama-cpp":  filepath.Join(root, "software", "installations", "field-kit-qwen", "upstream-release", "engine", "current", "payload"),
		"upstream-release:router:llama-swap": filepath.Join(root, "software", "installations", "field-kit-qwen", "upstream-release", "router", "current", "payload"),
	}
	for unitID, unit := range lock.Units {
		location := locations[unitID]
		generationRoot := filepath.Join(filepath.Dir(filepath.Dir(location)), "generations", "fixture")
		payload := filepath.Join(generationRoot, "payload")
		if err := os.MkdirAll(payload, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join("generations", "fixture"), filepath.Join(filepath.Dir(filepath.Dir(location)), "current")); err != nil {
			t.Fatal(err)
		}
		binary := "llama-server"
		if unitID == "upstream-release:router:llama-swap" {
			binary = "llama-swap"
		}
		if err := os.WriteFile(filepath.Join(payload, binary), []byte("fixture"), 0o755); err != nil {
			t.Fatal(err)
		}
		receiptDocument.Units[unitID] = receipt.Unit{
			Adapter: unit.Adapter, Scope: unit.Scope, NativeName: unit.NativeName, Version: unit.Version, Revision: unit.Revision,
			Dependencies: []string{}, Artifacts: unit.Artifacts, Location: location, Ownership: installplan.OwnershipTemperAdded,
		}
	}
	store, err := receiptstore.Read(root, "field-kit-qwen")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Commit(context.Background(), receiptDocument); err != nil {
		t.Fatal(err)
	}
	engine, _ := filepath.EvalSymlinks(filepath.Join(locations["upstream-release:engine:llama-cpp"], "llama-server"))
	router, _ := filepath.EvalSymlinks(filepath.Join(locations["upstream-release:router:llama-swap"], "llama-swap"))
	return fixture{root: root, lockPath: lockPath, generation: generation, config: config, router: router, engine: engine}
}

func TestSplashPlanResolvesBothProcessesFromOneExactRelease(t *testing.T) {
	f := materialize(t)
	raw, err := os.ReadFile(f.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := softwarelock.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	selection := locked.Selections["llama-cpp"]
	locked.Selections["splash"] = selection
	delete(locked.Selections, "llama-cpp")
	raw, err = softwarelock.Marshal(locked)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.lockPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	stored, err := receiptstore.Read(f.root, "field-kit-qwen")
	if err != nil {
		t.Fatal(err)
	}
	rec := stored.Document
	rec.Selections["splash"] = rec.Selections["llama-cpp"]
	delete(rec.Selections, "llama-cpp")
	rec.SoftwareLockDigest, err = locked.SemanticDigest()
	if err != nil {
		t.Fatal(err)
	}
	if err := stored.Commit(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	payload := rec.Units[selection.RootUnit].Location
	for _, path := range []string{"engine/splash", "python/bin/python3.13"} {
		full := filepath.Join(payload, path)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("fixture"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	requirements := runtimeconfig.Document{Schema: runtimeconfig.SchemaV1, Requirements: []runtimeconfig.Requirement{
		{Package: "llama-swap", RelativeExecutable: "llama-swap"},
		{Package: "splash", RelativeExecutable: "engine/splash", Role: "engine", Arguments: []string{"serve-native", "/target", "/draft", "32768", "25769803776"}},
		{Package: "splash", RelativeExecutable: "python/bin/python3.13", Role: "frontend", Arguments: []string{"-I", "-B", "-c", "bootstrap", "--port", "${PORT}"}},
	}}
	raw, err = runtimeconfig.Marshal(requirements)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "rendered", "generations", f.generation, "runtime", "requirements.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	inv, err := probecmd.Plan(probecmd.Options{Root: f.root, Installation: "field-kit-qwen", SoftwareLock: softwareLock(t, f.lockPath), Generation: f.generation, Listen: "127.0.0.1:18080"})
	if err != nil {
		t.Fatal(err)
	}
	native, _ := filepath.EvalSymlinks(filepath.Join(payload, "engine/splash"))
	frontend, _ := filepath.EvalSymlinks(filepath.Join(payload, "python/bin/python3.13"))
	port := strconv.Itoa(datadir.StartPort(f.root))
	if inv.EnginePath != native || inv.FrontendPath != frontend || strings.Join(inv.FrontendArguments, " ") != "-I -B -c bootstrap --port "+port || len(inv.EngineArguments) != 5 {
		t.Fatalf("lost exact runtime identity: %+v", inv)
	}
	preparedPython, err := probecmd.InstalledExecutable(f.root, "field-kit-qwen", softwareLock(t, f.lockPath), "splash", "python/bin/python3.13")
	if err != nil || preparedPython != frontend {
		t.Fatal(preparedPython, err)
	}
}

// softwareLock reads the fixture lock at call time; tests edit it in place.
func softwareLock(t *testing.T, path string) softwarelock.Document {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	document, err := softwarelock.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return document
}
