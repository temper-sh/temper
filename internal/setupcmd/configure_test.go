package setupcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/setup"
)

func configureRun(c Command, args ...string) (int, string, string) {
	var out, err bytes.Buffer
	code := c.Configure(context.Background(), args, strings.NewReader(""), &out, &err)
	return code, out.String(), err.String()
}

func TestConfigureSavesPresetsOnceAndEditsLayoutsOffline(t *testing.T) {
	id := "nuextract3-q4km-llama"
	c := fixtureCommand(t)
	root := filepath.Join(t.TempDir(), "root")
	args := []string{"--root", root, "--preset", id, "--context", id + "=4096", "--compute", id + "=cpu", "--json"}
	if code, _, err := configureRun(c, append(args, "--dry-run")...); code != 0 {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("dry run created root")
	}
	if code, _, err := configureRun(c, args...); code != 0 {
		t.Fatal(err)
	}
	saved, rev, err := setup.ReadConfiguration(root)
	if err != nil || len(saved.Presets) != 1 || len(saved.Layouts) != 0 {
		t.Fatal("script selected unrequested layouts", err)
	}
	lock := saved.Presets[id].Lock
	if p := lock.Records.Presets[lock.Preset]; p.Compute != catalog.ComputeCPU || p.Window() != 4096 {
		t.Fatal("script lost explicit compute or context")
	}
	if got := saved.Presets[id].Choices; got != (setup.Choices{ContextWindow: 4096, Compute: catalog.ComputeCPU}) {
		t.Fatalf("choices were not recorded: %+v", got)
	}
	saved.Layouts["review"] = setup.Layout{Name: "Review", Presets: []string{id}, Startup: []string{}, IdleSeconds: 30}
	raw, err := saved.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "edit.json")
	if err = os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	c.Catalog = func(context.Context, string, string, bool) (catalog.View, error) {
		t.Fatal("offline layout edit read moving catalog")
		return catalog.View{}, errors.New("offline")
	}
	if code, _, err := configureRun(c, "--root", root, "--file", file, "--revision", rev, "--json"); code != 0 {
		t.Fatal(err)
	}
	if code, output, err := configureRun(c, "--root", root, "--resume", "--json"); code != 0 || !strings.Contains(output, `"compute":"cpu"`) && !strings.Contains(output, `"compute": "cpu"`) {
		t.Fatal("offline review lost compute", output, err)
	}
	if code, _, _ := configureRun(c, "--root", root, "--file", file, "--revision", rev); code == 0 {
		t.Fatal("stale editor overwrote layout")
	}
	saved, revision, err := setup.ReadConfiguration(root)
	if err != nil {
		t.Fatal(err)
	}
	if code, _, err := configureRun(c, "--root", root, "--remove-preset", id, "--revision", revision); code == 0 || !strings.Contains(err, "review") {
		t.Fatal("referenced removal did not identify layout", err)
	}
	delete(saved.Layouts, "review")
	raw, _ = json.Marshal(saved)
	if err = os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if code, _, err := configureRun(c, "--root", root, "--file", file, "--revision", revision); code != 0 {
		t.Fatal(err)
	}
	got, _, err := setup.ReadConfiguration(root)
	if err != nil || len(got.Presets) != 1 {
		t.Fatal("layout deletion discarded preset", err)
	}
}

func TestConfigureRejectsUndeclaredComputeWithoutWriting(t *testing.T) {
	id := "nuextract3-q4km-llama"
	for _, args := range [][]string{
		{"--preset", id, "--compute", id + "="},
		{"--preset", id, "--compute", id + "=tpu"},
		{"--preset", id, "--compute", "unknown=cpu"},
		{"--preset", id, "--compute", id + "=cpu", "--compute", id + "=gpu"},
		{"--preset", "qwen3.8-27b-q4xl-splash", "--compute", "qwen3.8-27b-q4xl-splash=cpu"},
		{"--preset", compactLayout, "--compute", compactLayout + "=cpu"},
		{"--compute", id + "=cpu"},
		{"--show", "--compute", id + "=cpu"},
		{"--resume", "--compute", id + "=cpu"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "root")
			if code, _, diagnostic := configureRun(fixtureCommand(t), append(args, "--root", root)...); code == 0 {
				t.Fatal("invalid selection accepted", diagnostic)
			}
			if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Fatal("rejected selection wrote configuration")
			}
		})
	}
}
