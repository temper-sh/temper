package catalogcmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/catalogcmd"
	"github.com/temper-sh/temper/internal/machine"
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

func TestPresetConfigurationPreservesFrozenMaterialsAndContextIdentity(t *testing.T) {
	root := t.TempDir()
	source, output := filepath.Join(root, "source.json"), filepath.Join(root, "configured.json")
	invoke(t, "catalog", "compile", "--catalog", "../../catalog/experiments/qwen-study.json",
		"--preset", "splash-q4", "--target", "darwin/arm64", "--out", source)
	before, _ := os.ReadFile(source)
	base := readExecution(t, source)
	args := []string{"configure", "--lock", source, "--preset", "splash-q4", "--context", "32768",
		"--max-output", "4096", "--max-memory", "38654705664", "--out", output}
	run := func(args []string) string {
		t.Helper()
		var out, diagnostics bytes.Buffer
		if code := catalogcmd.Runtime(context.Background(), args, &out, &diagnostics, nil); code != 0 {
			t.Fatalf("configuration: %s", diagnostics.String())
		}
		return out.String()
	}
	assertPublication := func(response string, changed, dry bool) {
		t.Helper()
		var result struct {
			Path            string `json:"path"`
			ExecutionDigest string `json:"execution_digest"`
			Changed         bool   `json:"changed"`
			DryRun          bool   `json:"dry_run"`
		}
		if err := json.Unmarshal([]byte(response), &result); err != nil {
			t.Fatal(err)
		}
		if result.Path != output || len(result.ExecutionDigest) != 64 || result.Changed != changed || result.DryRun != dry {
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
		!reflect.DeepEqual(base.Records.Patches, configured.Records.Patches) ||
		!reflect.DeepEqual(base.Records.Engines, configured.Records.Engines) ||
		!reflect.DeepEqual(base.Records.Runtime, configured.Records.Runtime) {
		t.Fatal("per-run settings changed frozen materials or software")
	}
	p := configured.Records.Presets["splash-q4"]
	if p.ContextWindowTokens != 32768 || p.RequestDefaults.MaxOutputTokens != 4096 || p.EngineConfig.Splash.MaxMemoryBytes != 36<<30 {
		t.Fatal("explicit settings were not applied")
	}
	want := base.Records.Presets["splash-q4"]
	p.ContextWindowTokens = want.ContextWindowTokens
	p.RequestDefaults.MaxOutputTokens = want.RequestDefaults.MaxOutputTokens
	splash := *p.EngineConfig.Splash
	splash.MaxMemoryBytes = want.EngineConfig.Splash.MaxMemoryBytes
	p.EngineConfig.Splash = &splash
	if !reflect.DeepEqual(p, want) {
		t.Fatal("unrequested preset setting changed")
	}
	after, _ := os.ReadFile(source)
	if !bytes.Equal(before, after) {
		t.Fatal("source lock changed")
	}
	var result struct {
		ContextIdentity string `json:"context_execution_sha256"`
	}
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		t.Fatal(err)
	}
	p = configured.Records.Presets["splash-q4"]
	identity, err := catalog.ContextExecutionSHA256(configured.Records, "splash-q4", p.Patches[0], p.ContextWindowTokens)
	if err != nil || identity != result.ContextIdentity {
		t.Fatal("wizard context identity differs")
	}
	facts := machine.Facts{Schema: machine.FactsSchemaV1,
		Target: software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "26.6"},
		Chip:   "Apple M5 Max", HardwareModel: "Mac-fixture", OSBuild: "fixture", PhysicalMemoryBytes: 48 << 30,
		MetalDeviceMemoryMiB: 36 << 10, MetalDeviceMemorySource: machine.MetalDeviceSourceLive,
		WiredLimitMiB: 36 << 10, WiredLimitSource: budget.WiredSourceMetal}
	p.ContextFindings = []catalog.ContextFinding{{WindowTokens: 32768, MaxOutputTokens: 4096,
		ExecutionSHA256: result.ContextIdentity, EngineMemoryLimitBytes: 36 << 30, SwapGrowthLimitBytes: 512 << 20,
		Machine:  catalog.ContextMachine{Target: facts.Target, Chip: facts.Chip, PhysicalMemoryBytes: facts.PhysicalMemoryBytes, MinimumWiredLimitMiB: facts.WiredLimitMiB},
		Evidence: "https://example.test/reviewed-study", LatencyNote: "Synthetic contract check."}}
	configured.Records.Presets["splash-q4"] = p
	findings, err := catalog.MatchingContexts(configured.Records, "splash-q4", p.Patches[0], facts)
	if err != nil || len(findings) != 1 {
		t.Fatalf("wizard could not match the reviewed study: %v", err)
	}
	p.EngineConfig.Splash.MaxMemoryBytes = 35 << 30
	configured.Records.Presets["splash-q4"] = p
	findings, err = catalog.MatchingContexts(configured.Records, "splash-q4", p.Patches[0], facts)
	if err != nil || len(findings) != 0 {
		t.Fatalf("changed memory cap inherited a tested context: %v", err)
	}
	first, _ := os.Stat(output)
	assertPublication(run(args), false, false)
	again, _ := os.Stat(output)
	if !os.SameFile(first, again) || !first.ModTime().Equal(again.ModTime()) {
		t.Fatal("replay rewrote output")
	}
	args[6] = "65536"
	var out, diagnostics bytes.Buffer
	if catalogcmd.Runtime(context.Background(), args, &out, &diagnostics, nil) == 0 {
		t.Fatal("replaced a different output")
	}
}

func TestExecutionConfigurationRejectsUnboundedAndWrongPresetChanges(t *testing.T) {
	raw, err := os.ReadFile("../../catalog/experiments/qwen-study.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"splash-q4", "llama-q5"} {
		base, err := catalog.CompilePreset(d, id, "", 0, software.Target{OS: "darwin", Arch: "arm64"})
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
