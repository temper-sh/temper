package probecmd_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/temper-sh/temper/internal/artifactset"
	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/manifest"
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

type tokenizerRunner struct {
	called     bool
	invocation probecmd.Invocation
	output     string
}

func (r *tokenizerRunner) Run(_ context.Context, invocation probecmd.Invocation, stdout, _ io.Writer) error {
	r.called = true
	r.invocation = invocation
	_, _ = io.WriteString(stdout, r.output)
	return nil
}

func (r *recordingRunner) Run(_ context.Context, invocation probecmd.Invocation, _, _ io.Writer) error {
	r.called = true
	r.invocation = invocation
	return nil
}

func TestServeValidatesExactInputsBeforeRunningForeground(t *testing.T) {
	fixture := materialize(t)
	runner := &recordingRunner{}
	command, err := probecmd.New(runner)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	exit := command.Run(context.Background(), []string{
		"serve", "--root", fixture.root, "--installation", "field-kit-qwen",
		"--software-lock", fixture.lockPath, "--generation", fixture.generation,
		"--listen", "127.0.0.1:18080",
	}, &stdout, &stderr)
	if exit != 0 || stderr.Len() != 0 || !runner.called {
		t.Fatalf("exit=%d called=%v stdout=%q stderr=%q", exit, runner.called, stdout.String(), stderr.String())
	}
	if got := runner.invocation.Arguments; strings.Join(got, " ") != "--config "+fixture.config+" --listen 127.0.0.1:18080" {
		t.Fatalf("arguments = %q", got)
	}
	if runner.invocation.Path != fixture.router || len(runner.invocation.Environment) != 1 || !strings.HasPrefix(runner.invocation.Environment[0], "PATH="+filepath.Dir(fixture.engine)) {
		t.Fatalf("invocation = %#v", runner.invocation)
	}
	if !strings.Contains(stdout.String(), "RESULT probe-serve starting") || !strings.Contains(stdout.String(), "RESULT probe-serve stopped") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestServeDryRunHasNoProcessEffect(t *testing.T) {
	fixture := materialize(t)
	runner := &recordingRunner{}
	command, _ := probecmd.New(runner)
	var stdout, stderr bytes.Buffer
	exit := command.Run(context.Background(), []string{
		"serve", "--root", fixture.root, "--installation", "field-kit-qwen",
		"--software-lock", fixture.lockPath, "--generation", fixture.generation, "--dry-run",
	}, &stdout, &stderr)
	if exit != 0 || runner.called || stderr.Len() != 0 || !strings.Contains(stdout.String(), "ready-to-start") {
		t.Fatalf("exit=%d called=%v stdout=%q stderr=%q", exit, runner.called, stdout.String(), stderr.String())
	}
}

func TestTokenizeUsesReceiptedBinaryAndLockedGGUF(t *testing.T) {
	fixture := materialize(t)
	model := materializeTokenizerInputs(t, fixture)
	runner := &tokenizerRunner{output: "[101,202]\n"}
	command, err := probecmd.NewWithInput(runner, bytes.NewReader([]byte("exact rendered prompt")))
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	exit := command.Run(context.Background(), []string{
		"tokenize", "--root", fixture.root, "--installation", "field-kit-qwen",
		"--software-lock", fixture.lockPath, "--manifest", model.manifestPath,
		"--lock", model.lockPath, "--layout", "qwen-exact",
	}, &stdout, &stderr)
	if exit != 0 || stderr.Len() != 0 || !runner.called {
		t.Fatalf("exit=%d called=%v stdout=%q stderr=%q", exit, runner.called, stdout.String(), stderr.String())
	}
	if stdout.String() != "[101,202]\n" || string(runner.invocation.Input) != "exact rendered prompt" {
		t.Fatalf("stdout=%q input=%q", stdout.String(), runner.invocation.Input)
	}
	if runner.invocation.Path != model.tokenizer {
		t.Fatalf("tokenizer path = %q, want %q", runner.invocation.Path, model.tokenizer)
	}
	wantArguments := "-m " + model.model + " --stdin --ids --no-bos --offline --log-disable"
	if strings.Join(runner.invocation.Arguments, " ") != wantArguments {
		t.Fatalf("arguments = %q, want %q", runner.invocation.Arguments, wantArguments)
	}
}

func TestTokenizeRefusesNonJSONArrayOutput(t *testing.T) {
	fixture := materialize(t)
	model := materializeTokenizerInputs(t, fixture)
	runner := &tokenizerRunner{output: "token 101\n"}
	command, _ := probecmd.NewWithInput(runner, strings.NewReader("prompt"))
	var stdout, stderr bytes.Buffer
	exit := command.Run(context.Background(), []string{
		"tokenize", "--root", fixture.root, "--installation", "field-kit-qwen",
		"--software-lock", fixture.lockPath, "--manifest", model.manifestPath,
		"--lock", model.lockPath, "--layout", "qwen-exact",
	}, &stdout, &stderr)
	if exit != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "invalid token IDs") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
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
		Root: fixture.root, Installation: "field-kit-qwen", SoftwareLockPath: fixture.lockPath,
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
		pythonUnit = "uv:rapid-mlx:cpython"
		engineUnit = "uv:rapid-mlx:rapid-mlx"
	)
	pythonArtifact := software.Artifact{Locator: "https://example.invalid/cpython.tar.gz", SHA256: strings.Repeat("1", 64), Size: 1}
	wheelArtifact := software.Artifact{Locator: "https://example.invalid/rapid_mlx.whl", SHA256: strings.Repeat("2", 64), Size: 1}
	lock.Selections["rapid-mlx"] = softwarelock.Selection{
		Provenance: softwarelock.ProvenanceExperiment, Method: "python-environment", Adapter: "uv",
		RecipeRevision: "field-kit/v1", RootUnit: engineUnit,
	}
	lock.Units[pythonUnit] = softwarelock.Unit{
		Adapter: "uv", Scope: "rapid-mlx", NativeName: "cpython", Version: "3.13.7", Revision: "20260814",
		Dependencies: []string{}, Artifacts: []software.Artifact{pythonArtifact},
	}
	lock.Units[engineUnit] = softwarelock.Unit{
		Adapter: "uv", Scope: "rapid-mlx", NativeName: "rapid-mlx", Version: "0.13.3",
		Dependencies: []string{pythonUnit}, Artifacts: []software.Artifact{wheelArtifact},
	}
	lockData, err = softwarelock.Marshal(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.lockPath, lockData, 0o644); err != nil {
		t.Fatal(err)
	}

	scopeRoot := filepath.Join(fixture.root, "software", "installations", "field-kit-qwen", "uv", "rapid-mlx")
	generationRoot := filepath.Join(scopeRoot, "generations", "fixture")
	environment := filepath.Join(generationRoot, "environment")
	engine := filepath.Join(environment, "bin", "rapid-mlx")
	if err := os.MkdirAll(filepath.Dir(engine), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(engine, []byte("fixture"), 0o755); err != nil {
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
		Provenance: softwarelock.ProvenanceExperiment, Method: "python-environment", Adapter: "uv",
		RecipeRevision: "field-kit/v1", RootUnit: engineUnit,
	}
	receiptDocument.Units[pythonUnit] = receipt.Unit{
		Adapter: "uv", Scope: "rapid-mlx", NativeName: "cpython", Version: "3.13.7", Revision: "20260814",
		Dependencies: []string{}, Artifacts: []software.Artifact{pythonArtifact}, Location: environment, Ownership: installplan.OwnershipTemperAdded,
	}
	receiptDocument.Units[engineUnit] = receipt.Unit{
		Adapter: "uv", Scope: "rapid-mlx", NativeName: "rapid-mlx", Version: "0.13.3",
		Dependencies: []string{pythonUnit}, Artifacts: []software.Artifact{wheelArtifact}, Location: environment, Ownership: installplan.OwnershipTemperAdded,
	}
	if err := store.Commit(context.Background(), receiptDocument); err != nil {
		t.Fatal(err)
	}

	requirements, err := runtimeconfig.Marshal(runtimeconfig.Document{
		Schema: runtimeconfig.SchemaV1,
		Requirements: []runtimeconfig.Requirement{
			{Package: "llama-swap", RelativeExecutable: "llama-swap"},
			{Package: "rapid-mlx", RelativeExecutable: "bin/rapid-mlx"},
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
		Root: fixture.root, Installation: "field-kit-qwen", SoftwareLockPath: fixture.lockPath,
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
}

func TestServeRefusesDriftAndNonLoopbackBeforeProcessEffect(t *testing.T) {
	fixture := materialize(t)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "non-loopback", args: []string{"--listen", "0.0.0.0:8080"}, want: "IPv4 loopback"},
		{name: "wrong generation", args: []string{"--generation", strings.Repeat("a", 64)}, want: "does not exist"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &recordingRunner{}
			command, _ := probecmd.New(runner)
			arguments := []string{"serve", "--root", fixture.root, "--installation", "field-kit-qwen", "--software-lock", fixture.lockPath, "--generation", fixture.generation}
			arguments = append(arguments, test.args...)
			var stdout, stderr bytes.Buffer
			exit := command.Run(context.Background(), arguments, &stdout, &stderr)
			if exit != 1 || runner.called || !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("exit=%d called=%v stderr=%q", exit, runner.called, stderr.String())
			}
		})
	}
}

type fixture struct {
	root, lockPath, generation, config, router, engine string
}

type tokenizerFixture struct {
	manifestPath, lockPath, model, tokenizer string
}

func materializeTokenizerInputs(t *testing.T, fixture fixture) tokenizerFixture {
	t.Helper()
	tokenizer := filepath.Join(filepath.Dir(fixture.engine), "llama-tokenize")
	if err := os.WriteFile(tokenizer, []byte("fixture tokenizer"), 0o755); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Dir(fixture.lockPath)
	manifestPath := filepath.Join(workspace, "manifest.yaml")
	manifestData := []byte(`schema: temper-manifest/v1
defaults:
  ttl: 1800
  gpu_memory_utilization: 0.85
layouts:
  qwen-exact:
    display_name: Exact Qwen fixture
    model:
      repo: example/Qwen
      file: model.gguf
    engine: llama-server
    role: coder
    window: 4096
    max_tokens: 512
    kv: q8
    thinking: off
    llama:
      parallel: 1
      flash_attention: on
      batch: 512
      ubatch: 512
modes:
  field-kit:
    foreground: local
    tools: []
    harnesses: []
    members:
      resident:
        - layout: qwen-exact
          preferred: true
      on_demand: []
`)
	if err := os.WriteFile(manifestPath, manifestData, 0o644); err != nil {
		t.Fatal(err)
	}
	document, err := manifest.Parse(manifestData)
	if err != nil {
		t.Fatal(err)
	}
	modelData := []byte("fixture model")
	modelHash := fmt.Sprintf("%x", sha256.Sum256(modelData))
	entry := lockfile.Entry{
		Repo: "example/Qwen", Revision: strings.Repeat("1", 40), Resolved: "2026-09-02",
		Files: []lockfile.File{{Name: "model.gguf", SHA256: modelHash}},
	}
	modelLock := lockfile.Document{Schema: lockfile.SchemaV1, Entries: map[string]lockfile.Entry{"qwen-exact": entry}}
	lockData, err := lockfile.Marshal(modelLock)
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(workspace, "manifest.lock.yaml")
	if err := os.WriteFile(lockPath, lockData, 0o644); err != nil {
		t.Fatal(err)
	}
	set, err := artifactset.New(fixture.root, "qwen-exact", document.Layouts["qwen-exact"], entry, document.Patches)
	if err != nil {
		t.Fatal(err)
	}
	model := set.ModelPath()
	if err := os.MkdirAll(filepath.Dir(model), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(model, modelData, 0o644); err != nil {
		t.Fatal(err)
	}
	receiptData, err := set.Receipt([]artifactset.Record{{Path: "model/model.gguf", SHA256: modelHash, Size: int64(len(modelData))}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(set.Path(), "receipt.json"), receiptData, 0o644); err != nil {
		t.Fatal(err)
	}
	return tokenizerFixture{manifestPath: manifestPath, lockPath: lockPath, model: model, tokenizer: tokenizer}
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
