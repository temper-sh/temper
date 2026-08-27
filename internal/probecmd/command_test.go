package probecmd_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/temper-sh/temper/internal/probecmd"
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

	target := software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos"}
	artifact := software.Artifact{Locator: "https://example.invalid/archive.tar.gz", SHA256: strings.Repeat("c", 64), Size: 1}
	lock := softwarelock.Document{
		Schema: softwarelock.SchemaV1,
		Provenance: softwarelock.Provenance{Experiment: &softwarelock.ExperimentIdentity{
			Schema: "field-kit-baseline/v1", ID: "qwen38-dynamic", DefinitionSHA256: strings.Repeat("d", 64),
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
