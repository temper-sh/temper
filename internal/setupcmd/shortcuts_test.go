package setupcmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/managed"
	"github.com/temper-sh/temper/internal/setup"
)

func TestStartRefusesUnknownOrDeletedSelectionWithoutEffects(t *testing.T) {
	ctx := context.Background()
	for _, selected := range []string{"", "removed"} {
		t.Run(selected, func(t *testing.T) {
			root := t.TempDir()
			configuration := layoutFixture(t)
			configuration.Layouts = setup.StarterLayouts(nil)
			configuration.Layouts["another"] = setup.Layout{Name: "Another", IdleSeconds: 1800}
			if _, _, err := setup.SaveConfiguration(ctx, root, configuration, "", false); err != nil {
				t.Fatal(err)
			}
			if selected != "" {
				if err := (managed.FileStore{Root: root}).Save(ctx, managed.State{Schema: managed.StateSchema, Selected: &managed.Selection{Layout: selected, Listen: "127.0.0.1:8080"}}); err != nil {
					t.Fatal(err)
				}
			}
			c := fixtureCommand(t)
			c.Controller = func(string, *managed.Job, managed.State) managed.Controller {
				t.Fatal("refusal reached process effects")
				return nil
			}
			var out, diagnostics bytes.Buffer
			if code := c.Layout(ctx, []string{"start", "--root", root}, &out, &diagnostics); code != 1 || !strings.Contains(diagnostics.String(), "temper layout activate ID") {
				t.Fatal(code, out.String(), diagnostics.String())
			}
			state, err := (managed.FileStore{Root: root}).Read()
			if err != nil || selected == "" && state.Selected != nil || selected != "" && state.Selected.Layout != selected {
				t.Fatal("refusal changed selection", state, err)
			}
		})
	}
}

func TestFirstConfigurationSelectsDefaultAndDryRunDoesNot(t *testing.T) {
	configuration := layoutFixture(t)
	configuration.Layouts = setup.StarterLayouts(nil)
	raw, err := configuration.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "configuration.json")
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, dry := range []bool{true, false} {
		root := filepath.Join(t.TempDir(), "new-root")
		args := []string{"--root", root, "--file", file}
		if dry {
			args = append(args, "--dry-run")
		}
		if code, out, diagnostics := configureRun(fixtureCommand(t), args...); code != 0 {
			t.Fatal(code, out, diagnostics)
		}
		if dry {
			if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Fatal("dry run created root", err)
			}
		} else {
			state, err := (managed.FileStore{Root: root}).Read()
			if err != nil || state.Selected == nil || state.Selected.Layout != "default" {
				t.Fatal(state, err)
			}
		}
	}
}
