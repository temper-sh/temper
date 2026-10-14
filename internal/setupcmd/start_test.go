package setupcmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/managed"
	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/preset"
	"github.com/temper-sh/temper/internal/setup"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
)

type acceptingSoftware struct{}

func (acceptingSoftware) Install(context.Context, string, string, softwarelock.Document, io.Writer) error {
	return nil
}
func (acceptingSoftware) Check(context.Context, string, string, softwarelock.Document, io.Writer) error {
	return nil
}

// installableCommand prepares without effects and records which layout an
// install starts instead of reaching launchd.
func installableCommand(t *testing.T, started *[]string, startErr error) Command {
	t.Helper()
	c := fixtureCommand(t)
	c.Effects = preset.Effects{Software: acceptingSoftware{}, Fetch: func(context.Context, string, string, manifest.Document, lockfile.Document) error { return nil }}
	c.Start = func(_ context.Context, _ string, layout string) (*managed.Job, *managed.Status, error) {
		*started = append(*started, layout)
		if startErr != nil {
			return nil, nil, startErr
		}
		job := &managed.Job{Layout: layout, Name: "Default", Generation: "g1", Listen: "127.0.0.1:8080", Presets: []string{extraction}}
		return nil, &managed.Status{State: managed.State{Current: job, Desired: job}, Observed: managed.Observation{Exists: true, Ready: true, Models: map[string]string{extraction: "available on demand"}}}, nil
	}
	return c
}

// savedWithLayout saves one preset in the starter layout and returns the
// configuration file a scripted install can apply.
func savedWithLayout(t *testing.T, c Command, root string) string {
	t.Helper()
	if code, _, err := configureRun(c, "--root", root, "--preset", extraction); code != 0 {
		t.Fatal(err)
	}
	saved, _, _ := setup.ReadConfiguration(root)
	saved.Layouts = setup.StarterLayouts([]string{extraction})
	raw, err := saved.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "configuration.json")
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestInstallStartsTheDefaultLayout(t *testing.T) {
	var started []string
	c := installableCommand(t, &started, nil)
	root := filepath.Join(t.TempDir(), "root")
	file := savedWithLayout(t, c, root)
	_, rev, _ := setup.ReadConfiguration(root)
	code, output, diagnostics := configureRun(c, "--root", root, "--file", file, "--revision", rev, "--prepare")
	if code != 0 || len(started) != 1 || started[0] != "default" {
		t.Fatalf("%d %v\n%s\n%s", code, started, output, diagnostics)
	}
	for _, want := range []string{"Installed 1 preset.", "Started layout Default at http://127.0.0.1:8080/v1", extraction, "temper layout stop"} {
		if !strings.Contains(output, want) {
			t.Errorf("missing %q in:\n%s", want, output)
		}
	}
	if !strings.Contains(diagnostics, "Getting model files for") {
		t.Errorf("preparation progress missing:\n%s", diagnostics)
	}
}

func TestInstallSaysHowToRetryWhenTheLayoutDoesNotStart(t *testing.T) {
	var started []string
	c := installableCommand(t, &started, errors.New("port 8080 is in use"))
	root := filepath.Join(t.TempDir(), "root")
	file := savedWithLayout(t, c, root)
	_, rev, _ := setup.ReadConfiguration(root)
	code, output, _ := configureRun(c, "--root", root, "--file", file, "--revision", rev, "--prepare")
	if code != 1 || !strings.Contains(output, "Installed 1 preset.") ||
		!strings.Contains(output, "Layout default did not start: port 8080 is in use") ||
		!strings.Contains(output, "temper layout activate default") {
		t.Fatalf("%d\n%s", code, output)
	}
}

func TestInstallWithoutALayoutStartsNothing(t *testing.T) {
	var started []string
	c := installableCommand(t, &started, nil)
	root := filepath.Join(t.TempDir(), "root")
	code, output, diagnostics := configureRun(c, "--root", root, "--preset", extraction, "--prepare")
	if code != 0 || len(started) != 0 || !strings.Contains(output, "Installed 1 preset.") {
		t.Fatalf("%d %v\n%s\n%s", code, started, output, diagnostics)
	}
}

// runningJob records a valid current job, as layout activate leaves it.
func runningJob(t *testing.T, root string) {
	t.Helper()
	resolved, err := setup.Root(root, "")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(resolved))
	generation := strings.Repeat("b", 64)
	job := managed.Job{Layout: "default", Name: "Default", Generation: generation, Label: "sh.temper." + hex.EncodeToString(sum[:12]),
		Listen: "127.0.0.1:8080", Router: "/opt/llama-swap", Config: filepath.Join(resolved, "managed", "generations", generation, "config.yaml"),
		Presets: []string{extraction}, Launcher: "/opt/temper", LauncherSHA256: strings.Repeat("a", 64)}
	if err := (managed.FileStore{Root: resolved}).Save(context.Background(), managed.State{Schema: managed.StateSchema, Current: &job, Desired: &job}); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateRestartsTheRunningLayout(t *testing.T) {
	var started []string
	c := installableCommand(t, &started, nil)
	root := filepath.Join(t.TempDir(), "root")
	file := savedWithLayout(t, c, root)
	_, rev, _ := setup.ReadConfiguration(root)
	if code, _, err := configureRun(c, "--root", root, "--file", file, "--revision", rev); code != 0 {
		t.Fatal(err)
	}
	runningJob(t, root)
	c.Newest = newest(newerRouter(t))
	code, output, diagnostics := updateRun(c, "--root", root)
	if code != 0 || len(started) != 1 || started[0] != "default" ||
		!strings.Contains(output, "Installed the update for 1 preset.") || !strings.Contains(output, "Restarted layout Default at http://127.0.0.1:8080/v1") {
		t.Fatalf("%d %v\n%s\n%s", code, started, output, diagnostics)
	}

	// Nothing new: no install and no restart.
	started = nil
	if code, output, _ = updateRun(c, "--root", root); code != 0 || len(started) != 0 || !strings.Contains(output, "Everything is up to date.") {
		t.Fatalf("second update: %d %v\n%s", code, started, output)
	}
}
