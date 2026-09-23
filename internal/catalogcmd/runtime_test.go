package catalogcmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	checkverb "github.com/temper-sh/temper/internal/check"
	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/render"
	"github.com/temper-sh/temper/internal/software"
)

func TestUtilityExecutionKeepsHelperAvailableWithoutLocalForeground(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	raw, err := os.ReadFile("../../catalog/guided-setup.json")
	if err != nil {
		t.Fatal(err)
	}
	document, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	const profile = "qwen3.5-4b-utility"
	const helper = "qwen3.5-4b-q4km-off"
	selection, err := catalog.ResolveSelection(document, catalog.Selection{Schema: catalog.SelectionSchema, Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	locked, err := catalog.Compile(document, selection, software.Target{OS: "darwin", Arch: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	lockData, err := catalog.MarshalLock(locked)
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(parent, "utility.lock.json")
	if err := os.WriteFile(lockPath, lockData, 0o600); err != nil {
		t.Fatal(err)
	}
	projection, err := locked.Projections()
	if err != nil {
		t.Fatal(err)
	}
	mode := projection.Manifest.Modes[profile]
	if !mode.ExternalForeground || mode.Foreground != "" || len(mode.Members.Resident) != 0 || len(mode.Members.OnDemand) != 1 || mode.Members.OnDemand[0].Layout != helper {
		t.Fatalf("utility projection selected a local foreground: %+v", mode)
	}
	prediction, err := checkverb.PredictBudget(projection.Manifest, mode,
		budget.Machine{PhysicalMiB: 32768, DeviceMiB: 24576, WiredLimitMiB: 24576, WiredSource: budget.WiredSourcePredicted}, map[string]int64{})
	if err != nil || prediction.Status != budget.StatusNotApplicable || prediction.Holder != "" {
		t.Fatalf("helper-only resident budget: %+v %v", prediction, err)
	}
	piMode := mode
	piMode.Harnesses = []string{"pi"}
	piManifest := projection.Manifest
	piManifest.Modes = map[string]manifest.Mode{profile: piMode}
	piBase := []byte(`{"defaultModel":"provider/main","compaction":{"enabled":true,"reserveTokens":77},"theme":"dark"}`)
	piBundle, err := render.Build(render.Inputs{Manifest: piManifest, Lock: projection.Artifacts, Mode: profile, Root: root, PiSettingsBase: piBase})
	if err != nil {
		t.Fatal(err)
	}
	piSettingsFound := false
	for _, artifact := range piBundle.Artifacts {
		if artifact.Path == "pi/settings.json" {
			piSettingsFound = true
			var original, rendered map[string]any
			if err := json.Unmarshal(piBase, &original); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(artifact.Data, &rendered); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(rendered, original) {
				t.Fatalf("utility Pi settings changed provider foreground: %v", rendered)
			}
		}
	}
	if !piSettingsFound {
		t.Fatal("selected Pi integration produced no settings projection")
	}

	var operations []string
	var generation string
	dispatch := func(_ context.Context, args []string, out, _ io.Writer) int {
		operation := args[0]
		if operation == "software" || operation == "field-kit" || operation == "probe" {
			operation += " " + args[1]
		}
		operations = append(operations, operation)
		value := func(flag string) string {
			for index, arg := range args {
				if arg == flag && index+1 < len(args) {
					return args[index+1]
				}
			}
			return ""
		}
		switch operation {
		case "fetch":
			if args[1] != helper {
				t.Fatalf("runtime fetched non-helper layout %q", args[1])
			}
		case "apply", "check":
			manifestData, err := os.ReadFile(value("--manifest"))
			if err != nil {
				t.Fatal(err)
			}
			projected, err := manifest.Parse(manifestData)
			if err != nil || !projected.Modes[profile].ExternalForeground || projected.Modes[profile].Foreground != "" {
				t.Fatalf("temporary manifest lost external foreground: %v", err)
			}
			if operation == "apply" {
				artifactData, err := os.ReadFile(value("--lock"))
				if err != nil {
					t.Fatal(err)
				}
				artifacts, err := lockfile.Parse(artifactData)
				if err != nil {
					t.Fatal(err)
				}
				bundle, err := render.Build(render.Inputs{Manifest: projected, Lock: artifacts, Mode: profile, Root: root})
				if err != nil {
					t.Fatal(err)
				}
				config := ""
				for _, artifact := range bundle.Artifacts {
					if artifact.Path == "llama-swap/config.yaml" {
						config = string(artifact.Data)
					}
				}
				if !strings.Contains(config, `"`+helper+`"`) || strings.Contains(config, "routing:") {
					t.Fatalf("helper route leaked a local default: %s", config)
				}
				generation = bundle.Digest()
				for _, artifact := range bundle.Artifacts {
					path := filepath.Join(root, "rendered", "generations", generation, artifact.Path)
					if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, artifact.Data, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				fmt.Fprintln(out, "RESULT apply changed mode="+profile+" generation="+generation)
			}
		case "field-kit bind":
			if _, err := os.ReadFile(value("--manifest-lock")); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintln(out, "schema: temper-field-kit-binding/v1")
		case "probe serve":
			if value("--generation") != generation {
				t.Fatal("serve lost utility generation")
			}
		}
		return 0
	}
	var output, diagnostics bytes.Buffer
	base := []string{"--lock", lockPath, "--root", root, "--installation", "fixture"}
	if code := catalogcmd.Runtime(context.Background(), append([]string{"prepare"}, base...), &output, &diagnostics, dispatch); code != 0 {
		t.Fatalf("utility prepare failed: %s", &diagnostics)
	}
	if want := []string{"software install", "fetch", "software check", "apply", "check", "field-kit bind"}; !reflect.DeepEqual(operations, want) {
		t.Fatalf("prepare effects = %v, want %v", operations, want)
	}
	operations = nil
	output.Reset()
	if code := catalogcmd.Runtime(context.Background(), append([]string{"render"}, base...), &output, &diagnostics, dispatch); code != 0 {
		t.Fatalf("utility render failed: %s", &diagnostics)
	}
	if want := []string{"software check", "apply", "check", "field-kit bind"}; !reflect.DeepEqual(operations, want) {
		t.Fatalf("render effects = %v, want %v", operations, want)
	}
	if runtime.GOOS == "darwin" {
		operations = nil
		output.Reset()
		args := append(append([]string{"serve"}, base...), "--generation", generation, "--status-file", filepath.Join(parent, "status.json"))
		if code := catalogcmd.Runtime(context.Background(), args, &output, &diagnostics, dispatch); code != 0 || !reflect.DeepEqual(operations, []string{"probe serve"}) {
			t.Fatalf("utility serve failed: %s (effects %v)", &diagnostics, operations)
		}
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
	bundle, err := render.Build(render.Inputs{Manifest: projections.Manifest, Lock: projections.Artifacts, Mode: locked.Selection.Profile, Root: root})
	if err != nil {
		t.Fatal(err)
	}
	base := []string{"serve", "--lock", lock, "--root", root, "--installation", "fixture", "--status-file", filepath.Join(parent, "status.json"), "--generation"}
	called := false
	dispatch := func(_ context.Context, args []string, _ io.Writer, _ io.Writer) int { called = true; return 0 }
	var out, diagnostics bytes.Buffer
	if code := catalogcmd.Runtime(context.Background(), append(base, strings.Repeat("a", 64)), &out, &diagnostics, dispatch); code == 0 || called {
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
	if code := catalogcmd.Runtime(context.Background(), args, &out, &diagnostics, dispatch); code != 0 || !called {
		t.Fatal("valid generation refused", &diagnostics)
	}
	called = false
	path := filepath.Join(root, "rendered", "generations", bundle.Digest(), bundle.Artifacts[0].Path)
	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := catalogcmd.Runtime(context.Background(), args, &out, &diagnostics, dispatch); code == 0 || called {
		t.Fatal("changed generation admitted")
	}
}

func TestExecutionInspectionAndDryRunDoNotWriteOrInvokeEffects(t *testing.T) {
	parent := t.TempDir()
	lock := filepath.Join(parent, "execution.lock.json")
	invoke(t, compileArgs(lock)...)
	for _, args := range [][]string{
		{"inspect", "--lock", lock},
		{"prepare", "--lock", lock, "--root", filepath.Join(parent, "installation"), "--installation", "fixture", "--dry-run"},
	} {
		var out, diagnostics bytes.Buffer
		code := catalogcmd.Runtime(context.Background(), args, &out, &diagnostics, func(context.Context, []string, io.Writer, io.Writer) int {
			t.Fatal("read-only operation invoked an effect")
			return 1
		})
		if code != 0 || !json.Valid(out.Bytes()) {
			t.Fatalf("exit %d: %s %s", code, &out, &diagnostics)
		}
		entries, _ := os.ReadDir(parent)
		if len(entries) != 1 {
			t.Fatal("read-only operation wrote files")
		}
	}
}

func TestExecutionPreparationOwnsLegacyInputsAndReturnsBoundMaterial(t *testing.T) {
	parent := t.TempDir()
	lock := filepath.Join(parent, "execution.lock.json")
	invoke(t, compileArgs(lock)...)
	var operations, projections []string
	dispatch := func(_ context.Context, args []string, out, diagnostics io.Writer) int {
		operation := args[0]
		if operation == "software" || operation == "field-kit" {
			operation += " " + args[1]
		}
		operations = append(operations, operation)
		for index, arg := range args {
			if arg == "--manifest" || arg == "--manifest-lock" || arg == "--lock" {
				path := args[index+1]
				if _, err := os.ReadFile(path); err != nil {
					t.Fatal(err)
				}
				projections = append(projections, path)
			}
		}
		switch operation {
		case "apply":
			fmt.Fprintln(out, "RESULT apply changed mode=fixture generation="+strings.Repeat("b", 64))
		case "field-kit bind":
			fmt.Fprintln(out, "schema: temper-field-kit-binding/v1")
		}
		return 0
	}
	var out, diagnostics bytes.Buffer
	args := []string{"prepare", "--lock", lock, "--root", filepath.Join(parent, "root"), "--installation", "fixture"}
	if code := catalogcmd.Runtime(context.Background(), args, &out, &diagnostics, dispatch); code != 0 {
		t.Fatalf("exit %d: %s", code, &diagnostics)
	}
	want := []string{"software install", "fetch", "software check", "apply", "check", "field-kit bind"}
	if !reflect.DeepEqual(operations, want) {
		t.Fatalf("%v", operations)
	}
	for _, path := range projections {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("temporary inputs survived")
		}
	}
	var material map[string]any
	if err := json.Unmarshal(out.Bytes(), &material); err != nil {
		t.Fatal(err)
	}
	if material["generation"] != strings.Repeat("b", 64) || material["binding"] != "schema: temper-field-kit-binding/v1\n" {
		t.Fatalf("%v", material)
	}
	// Rendering a candidate never repeats installation or fetch.
	operations = nil
	args[0] = "render"
	out.Reset()
	if code := catalogcmd.Runtime(context.Background(), args, &out, &diagnostics, dispatch); code != 0 {
		t.Fatal(diagnostics.String())
	}
	if !reflect.DeepEqual(operations, want[2:]) {
		t.Fatalf("render effects: %v", operations)
	}
}

func TestExecutionFailureStopsSubsequentEffectsAndRemovesTemporaryInputs(t *testing.T) {
	parent := t.TempDir()
	lock := filepath.Join(parent, "execution.lock.json")
	invoke(t, compileArgs(lock)...)
	calls := 0
	temporary := ""
	dispatch := func(_ context.Context, args []string, out, diagnostics io.Writer) int {
		calls++
		for index, arg := range args {
			if arg == "--lock" {
				temporary = filepath.Dir(args[index+1])
			}
		}
		fmt.Fprintln(diagnostics, "injected install failure")
		return 7
	}
	var out, diagnostics bytes.Buffer
	code := catalogcmd.Runtime(context.Background(), []string{"prepare", "--lock", lock, "--root", filepath.Join(parent, "root"), "--installation", "fixture"}, &out, &diagnostics, dispatch)
	if code == 0 || calls != 1 || !strings.Contains(diagnostics.String(), "injected install failure") {
		t.Fatal(code, calls, &diagnostics)
	}
	if _, err := os.Lstat(temporary); !os.IsNotExist(err) {
		t.Fatal("temporary inputs survived failure")
	}
}
