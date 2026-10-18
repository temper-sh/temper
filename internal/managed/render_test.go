package managed

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/testfixture"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/preset"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/software"
	"gopkg.in/yaml.v3"
)

var fixtureLauncher = Launcher{Path: "/temper/bin/temper", SHA256: strings.Repeat("a", 64)}

func mixed(t *testing.T) setup.Configuration {
	t.Helper()
	d := testfixture.MixedEngineCatalog(t)
	c := setup.EmptyConfiguration()
	ids := []string{"qwen3.8-27b-q4xl-splash", "qwen3.8-27b-q4xl-mtp", "gemma-4-31b-qat-ud-q4-k-xl-llama"}
	for _, id := range ids {
		l, err := catalog.CompilePreset(d, id, 0, 0, software.Target{OS: "darwin", Arch: "arm64"})
		if err != nil {
			t.Fatal(err)
		}
		c.Presets[id] = setup.Preset{Name: id, Lock: l}
	}
	c.Layouts["work"] = setup.Layout{Name: "Work", Presets: ids, Startup: ids[:1], Default: ids[1], IdleSeconds: 60}
	return c
}

func TestMixedEnginesRenderExactPathsAndIndependentStartupDefault(t *testing.T) {
	c := mixed(t)
	l := setup.LayoutPlan{ID: "work", Layout: c.Layouts["work"], AllFit: false}
	resolve := func(lock catalog.Lock, pkg, relative string) (string, error) {
		version := lock.Records.Runtime.Router.Release.Version
		for _, e := range lock.Records.Engines {
			if e.Supply.Package == pkg {
				version = e.Supply.Release.Version
			}
		}
		return "/software/" + pkg + "/" + version + "/" + relative, nil
	}
	got, err := Render("/private/temper", "127.0.0.1:18080", c, l, resolve, fixtureLauncher)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got.Config)
	for _, want := range []string{"ttl: 60", "persistent: false", "aliases:", "internal-managed-exec"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s in %s", want, text)
		}
	}
	var paths []string
	for _, p := range got.Job.Processes {
		paths = append(paths, p.Path)
	}
	for _, want := range []string{"b1/llama-server", "b2/llama-server", "python/bin/python3.13", "engine/splash"} {
		if !strings.Contains(strings.Join(paths, "\n"), want) {
			t.Fatalf("exact engine closure missing: %s", want)
		}
	}
	var doc map[string]any
	if err = yaml.Unmarshal(got.Config, &doc); err != nil {
		t.Fatal(err)
	}
	models := doc["models"].(map[string]any)
	if len(models) != 3 {
		t.Fatal("mixed engines collapsed")
	}
	preload := doc["hooks"].(map[string]any)["on_startup"].(map[string]any)["preload"].([]any)
	if len(preload) != 1 || preload[0] != l.Layout.Startup[0] {
		t.Fatal("default implied startup")
	}
	if strings.Contains(text, "persistent: true") {
		t.Fatal("preload became permanent residency")
	}
}

func TestEmptyLayoutDoesNotStartAndNoDefaultNeedsNoAlias(t *testing.T) {
	c := mixed(t)
	l := c.Layouts["work"]
	l.Default = ""
	c.Layouts["work"] = l
	resolve := func(catalog.Lock, string, string) (string, error) { return "/exact/executable", nil }
	got, err := Render("/private/temper", "127.0.0.1:18080", c, setup.LayoutPlan{ID: "work", Layout: l, AllFit: true}, resolve, fixtureLauncher)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got.Config), "aliases:") {
		t.Fatal("helper-only layout acquired default")
	}
	l.Presets = nil
	l.Startup = nil
	c.Layouts["work"] = l
	if _, err = Render("/private/temper", "127.0.0.1:18080", c, setup.LayoutPlan{ID: "work", Layout: l}, resolve, fixtureLauncher); err == nil {
		t.Fatal("empty layout started a service")
	}
}

func TestLayoutPreservesIndependentCPUAndGPUCommands(t *testing.T) {
	c := mixed(t)
	const cpuID = "qwen3.8-27b-q4xl-mtp"
	selected := c.Presets[cpuID]
	p := selected.Lock.Records.Presets[cpuID]
	p.ComputeOptions = []catalog.ComputeDevice{catalog.ComputeGPU, catalog.ComputeCPU}
	selected.Lock.Records.Presets[cpuID] = p
	var err error
	selected.Lock, err = catalog.ConfigureExecution(selected.Lock, cpuID, catalog.ExecutionSettings{ContextWindowTokens: p.Window(), MaxOutputTokens: p.RequestDefaults.MaxOutputTokens, Compute: catalog.ComputeCPU})
	if err != nil {
		t.Fatal(err)
	}
	c.Presets[cpuID] = selected
	resolve := func(_ catalog.Lock, pkg, relative string) (string, error) {
		return "/exact/" + pkg + "/" + relative, nil
	}
	got, err := Render("/private/temper", "127.0.0.1:18080", c, setup.LayoutPlan{ID: "work", Layout: c.Layouts["work"], AllFit: true}, resolve, fixtureLauncher)
	if err != nil {
		t.Fatal(err)
	}
	cpuFound, gpuFound := false, false
	for _, process := range got.Job.Processes {
		args := strings.Join(process.Arguments, " ")
		if process.Role == cpuID {
			cpuFound = true
			for _, flag := range []string{"-ngl 0", "--device none", "--no-kv-offload", "--no-op-offload"} {
				if !strings.Contains(args, flag) {
					t.Fatalf("managed CPU launch omitted %s: %s", flag, args)
				}
			}
		}
		if process.Role == "gemma-4-31b-qat-ud-q4-k-xl-llama" {
			gpuFound = true
			if !strings.Contains(args, "-ngl 99") || strings.Contains(args, "--device none") {
				t.Fatal("CPU choice leaked to a GPU layout member")
			}
		}
	}
	if !cpuFound || !gpuFound {
		t.Fatal("mixed-device layout lost a preset")
	}
}

func TestMovedRootNamesItsJobAndRouterLogsLeaveTheGeneration(t *testing.T) {
	c := mixed(t)
	l := setup.LayoutPlan{ID: "work", Layout: c.Layouts["work"]}
	resolve := func(catalog.Lock, string, string) (string, error) { return "/software/bin", nil }
	got, err := Render("/private/temper", "127.0.0.1:18080", c, l, resolve, fixtureLauncher)
	if err != nil {
		t.Fatal(err)
	}
	if err := got.Job.Validate("/private/temper"); err != nil {
		t.Fatal(err)
	}
	err = got.Job.Validate("/private/moved")
	if err == nil || !strings.Contains(err.Error(), "launchctl bootout gui/") || !strings.Contains(err.Error(), got.Job.Label) {
		t.Fatalf("moved root = %v", err)
	}
	if path := got.Job.logPath(); path != "/private/temper/managed/logs/router.log" {
		t.Fatalf("router log = %s", path)
	}
	if plist := string(plistBytes(got.Job)); !strings.Contains(plist, "/private/temper/managed/logs/router.log") || strings.Contains(plist, "generations/"+got.Job.Generation+"/router.log") {
		t.Fatalf("plist log path:\n%s", plist)
	}
}

func TestEachStartEmptiesTheRouterLog(t *testing.T) {
	root := t.TempDir()
	j := Job{Config: filepath.Join(root, "managed", "generations", strings.Repeat("a", 64), "config.yaml")}
	if err := emptyLog(j); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(j.logPath(), []byte("earlier start\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := emptyLog(j); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(j.logPath()); err != nil || len(raw) != 0 {
		t.Fatalf("log kept %q, %v", raw, err)
	}
	if err := os.Remove(j.logPath()); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "elsewhere"), j.logPath()); err != nil {
		t.Fatal(err)
	}
	if err := emptyLog(j); err == nil {
		t.Fatal("followed a symlinked log")
	}
}

func TestEachRootRendersItsOwnStableEnginePorts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	c := mixed(t)
	l := setup.LayoutPlan{ID: "work", Layout: c.Layouts["work"], AllFit: false}
	resolve := func(catalog.Lock, string, string) (string, error) { return "/software/bin", nil }
	render := func(root string) (Rendered, int, string) {
		t.Helper()
		got, err := Render(root, "127.0.0.1:18080", c, l, resolve, fixtureLauncher)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			StartPort int `yaml:"startPort"`
		}
		if err = yaml.Unmarshal(got.Config, &doc); err != nil {
			t.Fatal(err)
		}
		bundle, err := preset.Render(root, c.Presets["qwen3.8-27b-q4xl-mtp"].Lock)
		if err != nil {
			t.Fatal(err)
		}
		return got, doc.StartPort, string(bundle.Artifacts[0].Data)
	}
	main, mainPort, mainExecution := render(filepath.Join(home, ".temper"))
	again, _, _ := render(filepath.Join(home, ".temper"))
	run, runPort, runExecution := render(filepath.Join(home, "field-kit", "runs", "one", "temper"))
	// The default root keeps its port and so its generation across upgrades.
	if mainPort != 10001 || !strings.Contains(mainExecution, "startPort: 10001\n") || main.Job.Generation != again.Job.Generation {
		t.Fatalf("default root moved or is unstable: port %d, generations %s %s", mainPort, main.Job.Generation, again.Job.Generation)
	}
	if runPort == mainPort || !strings.Contains(runExecution, fmt.Sprintf("startPort: %d\n", runPort)) || run.Job.Generation == main.Job.Generation {
		t.Fatalf("two roots share engine ports: %d and %d", mainPort, runPort)
	}
}
