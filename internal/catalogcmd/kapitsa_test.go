package catalogcmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/catalogcmd"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/preset"
	"github.com/temper-sh/temper/internal/probecmd"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/testfixture"
)

func TestNativeInstallationReuseIsCreditedOnlyWhilePayloadMatches(t *testing.T) {
	ctx := context.Background()
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	w := newExecutionWorld(t, parent)
	raw, err := os.ReadFile(w.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := catalog.ParseLock(raw)
	if err != nil {
		t.Fatal(err)
	}
	sets, err := preset.Software(lock)
	if err != nil {
		t.Fatal(err)
	}
	for _, set := range sets {
		if err := w.effects.Software.Install(ctx, root, set.ID, set.Lock, io.Discard); err != nil {
			t.Fatal(err)
		}
		if ok, err := preset.Installed(ctx, root, set); err != nil || !ok {
			t.Fatal("native receipt not admitted", set.Package, err)
		}
	}
	material, err := setup.InspectModels(root, []catalog.Lock{lock})
	if err != nil {
		t.Fatal(err)
	}
	if err := material.InspectPresetSoftware(ctx, []catalog.Lock{lock}); err != nil {
		t.Fatal(err)
	}
	c := setup.EmptyConfiguration()
	c.Presets[lock.Preset] = setup.Preset{Name: "Fixture", Lock: lock}
	c.Layouts = setup.StarterLayouts([]string{lock.Preset})
	facts, err := w.effects.Facts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := setup.BuildConfiguration(root, facts, 1<<40, c, material)
	if err != nil {
		t.Fatal(err)
	}
	for _, download := range plan.RemainingDownloads {
		if download.Kind == "software" {
			t.Fatal("verified native software charged again", download)
		}
	}
	for _, set := range sets {
		if set.Package != "llama-cpp" {
			continue
		}
		executable, err := preset.Executable(root, lock, set.Package, "bin/llama-server")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(executable, []byte("edited payload"), 0755); err != nil {
			t.Fatal(err)
		}
		if ok, _ := preset.Installed(ctx, root, set); ok {
			t.Fatal("drifted native payload credited")
		}
	}
}

func TestKapitsaInstallsPlansAndServesOnlyVerifiedMaterialWithPreparedHome(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	w := executionWorldFor(t, parent, testfixture.KapitsaCatalog(t), "qwen3.8-27b-mlx4-kapitsa", "kapitsa")
	facts := w.effects.Facts
	w.effects.Facts = func(ctx context.Context) (machine.Facts, error) { f, e := facts(ctx); f.Chip = "Apple M5"; return f, e }
	base := []string{"--lock", w.lockPath, "--root", root, "--installation", "fixture", "--json"}
	run := func(operation string, extra ...string) (int, map[string]any, string) {
		t.Helper()
		var out, diag bytes.Buffer
		args := append([]string{operation}, base...)
		args = append(args, extra...)
		code := catalogcmd.Runtime(context.Background(), args, &out, &diag, w.effects)
		var result map[string]any
		if out.Len() > 0 {
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatal(err, out.String())
			}
		}
		return code, result, diag.String()
	}
	if code, _, d := run("prepare", "--dry-run"); code != 0 {
		t.Fatal(d)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) || len(w.operations) != 0 {
		t.Fatal("dry prepare performed effects", err)
	}
	code, result, d := run("prepare")
	if code != 0 {
		t.Fatal(d)
	}
	generation := result["generation"].(string)
	w.downloads = 0
	if code, again, d := run("prepare"); code != 0 || again["generation"] != generation || w.downloads != 0 {
		t.Fatal("unclean repeat", d)
	}
	code, paths, d := run("paths")
	if code != 0 {
		t.Fatal(d)
	}
	state := paths["engine_state"].(map[string]any)["qwen3.8-27b-mlx4-kapitsa"].(map[string]any)["path"].(string)
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatal("prepare or paths wrote calibration state", err)
	}
	if runtime.GOOS != "darwin" {
		return
	}
	called := false
	w.effects.Serve = func(_ context.Context, o probecmd.Options, _ probecmd.Supervision, _, _ io.Writer) error {
		called = true
		info, err := os.Stat(state)
		if err != nil || !info.IsDir() {
			t.Fatal("home not prepared", err)
		}
		inv, err := probecmd.Plan(o)
		if err != nil {
			return err
		}
		if filepath.Base(inv.EnginePath) != "kapitsa" || len(inv.EngineArguments) == 0 || inv.EngineArguments[0] != "serve" {
			t.Fatal("native process ownership missing", inv)
		}
		return nil
	}
	extra := []string{"--generation", generation, "--status-file", filepath.Join(parent, "status.json")}
	if code, _, d := run("serve", append(extra, "--dry-run")...); code != 0 || called {
		t.Fatal("dry serve effect", d)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatal("dry serve prepared state")
	}
	if code, _, d := run("serve", extra...); code != 0 || !called {
		t.Fatal("serve failed", d)
	}
	called = false
	model := paths["models"].(map[string]any)["qwen3.8-27b-mlx4-kapitsa"].(string)
	if err := os.WriteFile(filepath.Join(model, "._weights.safetensors"), []byte("metadata"), 0600); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := run("serve", extra...); code == 0 || called {
		t.Fatal("unverified model scan reached startup")
	}
}
