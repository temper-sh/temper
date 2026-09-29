package managed

import (
	"os"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/software"
	"gopkg.in/yaml.v3"
)

var fixtureLauncher = Launcher{Path: "/temper/bin/temper", SHA256: strings.Repeat("a", 64)}

func mixed(t *testing.T) setup.Configuration {
	t.Helper()
	raw, err := os.ReadFile("../../catalog/guided-setup.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	c := setup.EmptyConfiguration()
	ids := []string{"qwen3.8-27b-q4xl-splash", "qwen3.8-27b-q4xl-mtp", "gemma-4-31b-qat-ud-q4-k-xl-llama"}
	for _, id := range ids {
		l, err := catalog.CompilePreset(d, id, "", 0, software.Target{OS: "darwin", Arch: "arm64"})
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
	for _, want := range []string{"b11157/llama-server", "b11205/llama-server", "python/bin/python3.13", "engine/splash"} {
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
