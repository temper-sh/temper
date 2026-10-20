package setupcmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/harness"
	"github.com/temper-sh/temper/internal/managed"
	"github.com/temper-sh/temper/internal/setup"
)

type activationStore struct {
	state  managed.State
	writes int
}

func (s *activationStore) Read() (managed.State, error) { return s.state, nil }
func (s *activationStore) Save(_ context.Context, v managed.State) error {
	s.state = v
	s.writes++
	return nil
}

type activationController struct {
	job           *managed.Job
	starts, stops int
	fail          bool
	onStart       func()
}

func (c *activationController) Recover(context.Context, []managed.Job) error { return nil }
func (c *activationController) Observe(context.Context, []managed.Job) (managed.Observation, error) {
	if c.job == nil {
		return managed.Observation{}, nil
	}
	return managed.Observation{Exists: true, Ready: !c.fail, PID: 123, Generation: c.job.Generation}, nil
}
func (c *activationController) Start(_ context.Context, job managed.Job) error {
	c.job = &job
	c.starts++
	if c.onStart != nil {
		c.onStart()
	}
	return nil
}
func (c *activationController) Stop(context.Context, managed.Job) error {
	c.job = nil
	c.stops++
	return nil
}

func TestActivationPublishesOnlyAfterReadinessAndRetryDoesNotRestart(t *testing.T) {
	for _, client := range []struct{ id, file string }{{"pi", "models.json"}, {"hermes", "config.yaml"}} {
		t.Run(client.id, func(t *testing.T) {
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, client.id, client.file)
			choices := []harness.Selection{{ID: client.id, ModelsFile: path}}
			prepare := func() harness.Plan {
				p, err := harness.Prepare(dir, choices, "127.0.0.1:8080", []harness.Model{{ID: "on-demand", Name: "On demand", ContextWindow: 8192, MaxTokens: 8192}})
				if err != nil {
					raw, _ := os.ReadFile(path)
					t.Fatalf("%v\n%s", err, raw)
				}
				return p
			}
			store := &activationStore{}
			controller := &activationController{fail: true}
			controller.onStart = func() {
				if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
					t.Fatal("harness changed before readiness")
				}
			}
			job := &managed.Job{Generation: "test", Layout: "work"}
			preview, err := reconcileLayout(context.Background(), store, controller, job, true, prepare())
			if err != nil || preview.Integrations[0].Status != "pending" || controller.starts != 0 || store.writes != 0 {
				t.Fatal("dry run caused effects", preview, err)
			}
			failed, err := reconcileLayout(context.Background(), store, controller, job, false, prepare())
			if err == nil || failed.Integrations[0].Status != "not-applied" {
				t.Fatal("unready service published harness", failed, err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("unready service created file")
			}
			controller.fail = false
			// A concurrent edit after preflight fails synchronization while service
			// recovery succeeds. The service's success must remain visible.
			plan := prepare()
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(`{"user":"edit"}`), 0600); err != nil {
				t.Fatal(err)
			}
			partial, err := reconcileLayout(context.Background(), store, controller, job, false, plan)
			if err == nil || !partial.Observed.Ready || partial.Integrations[0].Status != "failed" || !strings.Contains(err.Error(), "layout is running") {
				t.Fatal("partial success not reported", partial, err)
			}
			repaired, err := reconcileLayout(context.Background(), store, controller, job, false, prepare())
			if err != nil || repaired.Integrations[0].Status != "updated" || controller.starts != 1 || controller.stops != 0 {
				t.Fatal(repaired, err)
			}
			first, _ := os.Stat(path)
			clean, err := reconcileLayout(context.Background(), store, controller, job, false, prepare())
			after, _ := os.Stat(path)
			if err != nil || clean.Integrations[0].Status != "unchanged" || controller.starts != 1 || !first.ModTime().Equal(after.ModTime()) {
				t.Fatal("retry restarted or rewrote unchanged state", clean, err)
			}
		})
	}
}

func TestMalformedHarnessRefusesBeforeServiceOrMachineWork(t *testing.T) {
	for _, client := range []struct{ id, file string }{{"pi", "models.json"}, {"hermes", "config.yaml"}} {
		t.Run(client.id, func(t *testing.T) {
			c := fixtureCommand(t)
			root := filepath.Join(t.TempDir(), "root")
			file := savedWithLayout(t, c, root)
			_, revision, _ := setup.ReadConfiguration(root)
			if code, _, diagnostics := configureRun(c, "--root", root, "--file", file, "--revision", revision); code != 0 {
				t.Fatal(diagnostics)
			}
			saved, revision, _ := setup.ReadConfiguration(root)
			dir, _ := filepath.EvalSymlinks(t.TempDir())
			path := filepath.Join(dir, client.file)
			if err := os.WriteFile(path, []byte(`{"providers":null}`), 0600); err != nil {
				t.Fatal(err)
			}
			saved.Integrations = []harness.Selection{{ID: client.id, ModelsFile: path}}
			raw, _ := saved.Bytes()
			if err := os.WriteFile(file, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if code, _, diagnostics := configureRun(c, "--root", root, "--file", file, "--revision", revision); code != 0 {
				t.Fatal(diagnostics)
			}
			c.Controller = func(string, *managed.Job, managed.State) managed.Controller {
				t.Fatal("harness refusal reached controller")
				return nil
			}
			resolved, _ := setup.Root(root, "")
			_, status, err := c.activate(context.Background(), resolved, "default", "", false)
			if err == nil || status != nil || !strings.Contains(err.Error(), "harness preflight") {
				t.Fatal("malformed destination accepted", status, err)
			}
			if _, err := os.Stat(filepath.Join(root, "managed")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("preflight created managed service state")
			}
		})
	}
}
