package catalogcmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/buildinfo"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/catalogcmd"
	"github.com/temper-sh/temper/internal/software"
)

func readExecution(t *testing.T, path string) catalog.Lock {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	l, err := catalog.ParseLock(raw)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestExecutionComputeCLIConfiguresAndInspectsExactPlacement(t *testing.T) {
	root := t.TempDir()
	source, output := filepath.Join(root, "gpu.json"), filepath.Join(root, "cpu.json")
	id := "nuextract3-q4km-llama"
	invoke(t, "catalog", "compile", "--catalog", "../testfixture/testdata/catalog.json", "--preset", id, "--target", "darwin/arm64", "--out", source)
	args := []string{"configure", "--lock", source, "--preset", id, "--context", "4096", "--max-output", "1024", "--out", output}
	for _, device := range []string{"", "auto", "tpu"} {
		var out, diagnostic bytes.Buffer
		if catalogcmd.Runtime(context.Background(), append(append([]string{}, args...), "--compute", device), &out, &diagnostic, catalogcmd.Effects{}) == 0 {
			t.Fatalf("invalid compute %q accepted", device)
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatal("invalid compute wrote output")
		}
	}
	args = append(args, "--compute", "cpu")
	for _, dry := range []bool{true, false, false} {
		var out, diagnostic bytes.Buffer
		command := append([]string{}, args...)
		if dry {
			command = append(command, "--dry-run")
		}
		if catalogcmd.Runtime(context.Background(), command, &out, &diagnostic, catalogcmd.Effects{}) != 0 {
			t.Fatal(diagnostic.String())
		}
		if dry {
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("dry compute edit wrote output")
			}
		}
	}
	configured := readExecution(t, output)
	if p := configured.Records.Presets[id]; p.Compute != catalog.ComputeCPU || p.Window() != 4096 || p.RequestDefaults.MaxOutputTokens != 1024 {
		t.Fatal("compute configuration lost explicit settings")
	}
	var out, diagnostic bytes.Buffer
	if catalogcmd.Runtime(context.Background(), []string{"inspect", "--lock", output}, &out, &diagnostic, catalogcmd.Effects{}) != 0 {
		t.Fatal(diagnostic.String())
	}
	if !strings.Contains(out.String(), `"compute": "cpu"`) {
		t.Fatalf("inspection omitted CPU selection: %s", &out)
	}
}

func TestPresetConfigurationPreservesFrozenMaterialsAndContextIdentity(t *testing.T) {
	root := t.TempDir()
	source, output := filepath.Join(root, "source.json"), filepath.Join(root, "configured.json")
	invoke(t, "catalog", "compile", "--catalog", "../testfixture/testdata/study-catalog.json",
		"--preset", "splash-q4", "--target", "darwin/arm64", "--out", source)
	before, _ := os.ReadFile(source)
	base := readExecution(t, source)
	args := []string{"configure", "--lock", source, "--preset", "splash-q4", "--context", "32768",
		"--max-output", "4096", "--max-memory", "38654705664", "--out", output}
	run := func(args []string) string {
		t.Helper()
		var out, diagnostics bytes.Buffer
		if code := catalogcmd.Runtime(context.Background(), args, &out, &diagnostics, catalogcmd.Effects{}); code != 0 {
			t.Fatalf("configuration: %s", diagnostics.String())
		}
		return out.String()
	}
	assertPublication := func(response string, changed, dry bool) {
		t.Helper()
		var result map[string]any
		if err := json.Unmarshal([]byte(response), &result); err != nil {
			t.Fatal(err)
		}
		_, digest := result["execution_digest"]
		_, context := result["context_execution_sha256"]
		if result["path"] != output || digest || context || result["changed"] != changed || result["dry_run"] != dry {
			t.Fatalf("unexpected publication result: %s", response)
		}
	}
	assertPublication(run(append(append([]string{}, args...), "--dry-run")), true, true)
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("dry-run wrote a lock")
	}
	response := run(args)
	assertPublication(response, true, false)
	configured := readExecution(t, output)
	if !reflect.DeepEqual(base.Records.Artifacts, configured.Records.Artifacts) ||
		!reflect.DeepEqual(base.Records.Templates, configured.Records.Templates) ||
		!reflect.DeepEqual(base.Records.Engines, configured.Records.Engines) ||
		!reflect.DeepEqual(base.Records.Runtime, configured.Records.Runtime) {
		t.Fatal("per-run settings changed frozen materials or software")
	}
	p := configured.Records.Presets["splash-q4"]
	if p.Window() != 32768 || p.RequestDefaults.MaxOutputTokens != 4096 || p.Config().Splash.MaxMemoryBytes != 36<<30 {
		t.Fatal("explicit settings were not applied")
	}
	want := base.Records.Presets["splash-q4"]
	p.Profiles[0].ContextWindowTokens = want.Window()
	p.RequestDefaults.MaxOutputTokens = want.RequestDefaults.MaxOutputTokens
	splash := *p.Config().Splash
	splash.MaxMemoryBytes = want.Config().Splash.MaxMemoryBytes
	p.Profiles[0].EngineConfig.Splash = &splash
	if !reflect.DeepEqual(p, want) {
		t.Fatal("unrequested preset setting changed")
	}
	after, _ := os.ReadFile(source)
	if !bytes.Equal(before, after) {
		t.Fatal("source lock changed")
	}
	first, _ := os.Stat(output)
	assertPublication(run(args), false, false)
	again, _ := os.Stat(output)
	if !os.SameFile(first, again) || !first.ModTime().Equal(again.ModTime()) {
		t.Fatal("replay rewrote output")
	}
	args[6] = "65536"
	var out, diagnostics bytes.Buffer
	if catalogcmd.Runtime(context.Background(), args, &out, &diagnostics, catalogcmd.Effects{}) == 0 {
		t.Fatal("replaced a different output")
	}
}

func TestExecutionConfigurationRejectsUnboundedAndWrongPresetChanges(t *testing.T) {
	raw, err := os.ReadFile("../testfixture/testdata/study-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	view, err := catalog.Read(raw, buildinfo.DevVersion)
	if err != nil {
		t.Fatal(err)
	}
	d := view.Document
	for _, id := range []string{"splash-q4", "llama-q5"} {
		base, err := catalog.CompilePreset(d, id, 0, 0, software.Target{OS: "darwin", Arch: "arm64"})
		if err != nil {
			t.Fatal(err)
		}
		before, _ := catalog.MarshalLock(base)
		valid := catalog.ExecutionSettings{ContextWindowTokens: 65536, MaxOutputTokens: 4096}
		if _, err := catalog.ConfigureExecution(base, "wrong-preset", valid); err == nil {
			t.Fatal("accepted another preset")
		}
		for _, invalid := range []catalog.ExecutionSettings{
			{ContextWindowTokens: 300000, MaxOutputTokens: 4096},
			{ContextWindowTokens: 4096, MaxOutputTokens: 4096},
			{ContextWindowTokens: 65536, MaxOutputTokens: 0},
			{ContextWindowTokens: 65536, MaxOutputTokens: 4096, MaxMemoryBytes: -1},
		} {
			if _, err := catalog.ConfigureExecution(base, id, invalid); err == nil {
				t.Fatal("accepted invalid bounds")
			}
		}
		valid.MaxMemoryBytes = 27 << 30
		_, err = catalog.ConfigureExecution(base, id, valid)
		if (id == "splash-q4") != (err == nil) {
			t.Fatalf("engine memory support: %v", err)
		}
		after, _ := catalog.MarshalLock(base)
		if !bytes.Equal(before, after) {
			t.Fatal("mutated source in memory")
		}
	}
}
