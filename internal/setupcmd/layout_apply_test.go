package setupcmd

import (
	"context"
	"errors"
	"maps"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/setup"
)

// Saving in the layout editor is enough to use a change: when the editor
// closes, the running layout restarts if the session changed what it serves.
// Activation then rewrites harness files. Nothing else starts or restarts.
func TestClosingTheLayoutEditorAppliesSavedChangesToTheRunningLayout(t *testing.T) {
	for _, scenario := range []string{"running layout changed", "nothing saved", "running layout renamed", "other layout changed", "stopped root", "running layout deleted", "restart refused"} {
		t.Run(scenario, func(t *testing.T) {
			var started []string
			var refusal error
			if scenario == "restart refused" {
				refusal = errors.New("active work prevents this transition; retry when requests finish")
			}
			c := installableCommand(t, &started, refusal)
			root := filepath.Join(t.TempDir(), "root")
			file := savedWithLayout(t, c, root)
			_, rev, _ := setup.ReadConfiguration(root)
			if code, _, err := configureRun(c, "--root", root, "--file", file, "--revision", rev); code != 0 {
				t.Fatal(err)
			}
			if scenario != "stopped root" {
				runningJob(t, root)
			}
			before, rev, err := setup.ReadConfiguration(root)
			if err != nil {
				t.Fatal(err)
			}
			after := before
			after.Layouts = maps.Clone(before.Layouts)
			switch scenario {
			case "running layout changed", "stopped root", "restart refused":
				layout := after.Layouts["default"]
				layout.IdleSeconds = 60
				after.Layouts["default"] = layout
			case "running layout renamed":
				layout := after.Layouts["default"]
				layout.Name = "Everyday"
				after.Layouts["default"] = layout
			case "other layout changed":
				after.Layouts["writing"] = setup.Layout{Name: "Writing", Presets: []string{extraction}, Startup: []string{}, IdleSeconds: 60}
			case "running layout deleted":
				delete(after.Layouts, "default")
			}
			if scenario != "nothing saved" {
				if _, _, err := setup.SaveConfiguration(context.Background(), root, after, rev, false); err != nil {
					t.Fatal(err)
				}
			}

			var out strings.Builder
			applied := c.applyEditedLayout(context.Background(), root, before, &out)
			restarts := scenario == "running layout changed" || scenario == "restart refused"
			if restarts != (len(started) == 1) || restarts && started[0] != "default" {
				t.Fatalf("restarted %v for %s:\n%s", started, scenario, out.String())
			}
			if applied != (scenario != "restart refused") {
				t.Fatalf("apply result %v for %s:\n%s", applied, scenario, out.String())
			}
			switch scenario {
			case "running layout changed":
				if !strings.Contains(out.String(), "Applying saved changes to the running layout Default.") || !strings.Contains(out.String(), "Default at http://127.0.0.1:8080/v1") {
					t.Fatalf("restart not reported:\n%s", out.String())
				}
			case "restart refused":
				if !strings.Contains(out.String(), "Apply later with: temper layout activate default") {
					t.Fatalf("refused restart gives no next step:\n%s", out.String())
				}
			default:
				if out.Len() != 0 {
					t.Fatalf("unaffected layout produced output:\n%s", out.String())
				}
			}
		})
	}
}
