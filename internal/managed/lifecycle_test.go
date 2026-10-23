package managed

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type memoryStore struct {
	state          State
	writes, failAt int
}

func (s *memoryStore) Read() (State, error) { return s.state, nil }
func (s *memoryStore) Save(_ context.Context, state State) error {
	s.writes++
	if s.writes == s.failAt {
		return errors.New("interrupted save")
	}
	s.state = state
	return nil
}

type jobController struct {
	job                          *Job
	busy                         bool
	starts, stops, recoveries    int
	failStart, failStop, unknown bool
}

func (c *jobController) Recover(context.Context, []Job) error { c.recoveries++; return nil }
func (c *jobController) Observe(_ context.Context, known []Job) (Observation, error) {
	if c.unknown {
		return Observation{}, errors.New("unrelated job")
	}
	if c.job == nil {
		return Observation{}, nil
	}
	for _, j := range known {
		if j.Generation == c.job.Generation {
			return Observation{Exists: true, Ready: true, PID: 123, Generation: j.Generation, Busy: c.busy}, nil
		}
	}
	return Observation{}, errors.New("unrecognized generation")
}
func (c *jobController) Start(_ context.Context, j Job) error {
	c.starts++
	c.job = &j
	if c.failStart {
		c.failStart = false
		return errors.New("CLI lost start response")
	}
	return nil
}
func (c *jobController) Stop(_ context.Context, j Job) error {
	c.stops++
	c.job = nil
	if c.failStop {
		c.failStop = false
		return errors.New("CLI lost stop response")
	}
	return nil
}

func TestActivationRecoversEveryDurableBoundary(t *testing.T) {
	for _, point := range []string{"intent", "start-response", "final-save", "stop-response", "stopped-save"} {
		t.Run(point, func(t *testing.T) {
			old := Job{Layout: "old", Generation: "old"}
			next := Job{Layout: "new", Generation: "new"}
			s := &memoryStore{state: State{Schema: StateSchema}}
			c := &jobController{}
			switch point {
			case "intent":
				s.failAt = 1
			case "start-response":
				c.failStart = true
			case "final-save":
				s.failAt = 2
			case "stop-response":
				s.state.Current = &old
				s.state.Desired = &old
				c.job = &old
				c.failStop = true
			case "stopped-save":
				s.state.Current = &old
				s.state.Desired = &old
				c.job = &old
				s.failAt = 2
			}
			if _, err := Reconcile(context.Background(), s, c, &next, false); err == nil {
				t.Fatal("expected injected interruption")
			}
			s.failAt = 0
			got, err := Reconcile(context.Background(), s, c, &next, false)
			if err != nil || got.Pending || got.State.Current.Generation != "new" {
				t.Fatal("retry did not converge", got, err)
			}
			starts := c.starts
			stops := c.stops
			if _, err := Reconcile(context.Background(), s, c, &next, false); err != nil {
				t.Fatal(err)
			}
			if c.starts != starts || c.stops != stops {
				t.Fatal("clean repeat restarted a reusable running preset")
			}
		})
	}
}

func TestBusyUnknownAndDryTransitionsHaveNoJobEffects(t *testing.T) {
	old := Job{Layout: "old", Generation: "old"}
	next := Job{Layout: "new", Generation: "new"}
	for _, condition := range []string{"busy", "unknown", "dry"} {
		t.Run(condition, func(t *testing.T) {
			s := &memoryStore{state: State{Schema: StateSchema, Desired: &old, Current: &old}}
			c := &jobController{job: &old, busy: condition == "busy", unknown: condition == "unknown"}
			_, err := Reconcile(context.Background(), s, c, &next, condition == "dry")
			if condition != "dry" && err == nil {
				t.Fatal("unsafe transition accepted")
			}
			if s.writes != 0 || c.starts+c.stops != 0 {
				t.Fatal("refusal/dry transition changed state")
			}
			if condition == "dry" && c.recoveries != 0 {
				t.Fatal("dry run performed recovery")
			}
		})
	}
}

func TestStopConvergesAfterJobEffectAndLayoutEditsDoNotMutateActivation(t *testing.T) {
	old := Job{Layout: "writing", Name: "Writing", Generation: "exact"}
	s := &memoryStore{state: State{Schema: StateSchema, Desired: &old, Current: &old}}
	c := &jobController{job: &old, failStop: true}
	if _, err := Reconcile(context.Background(), s, c, nil, false); err == nil {
		t.Fatal("missing stop interruption")
	}
	if s.state.Desired != nil {
		t.Fatal("stop did not commit desired off before its effect")
	}
	status, err := Reconcile(context.Background(), s, c, nil, false)
	if err != nil || status.Pending || status.State.Current != nil {
		t.Fatal(status, err)
	}
	if _, err := Reconcile(context.Background(), s, c, nil, false); err != nil || c.stops != 1 {
		t.Fatal("repeat stop repeated effect", err)
	}
	if s.state.Selected == nil || s.state.Selected.Layout != "writing" {
		t.Fatal("stop lost the selected layout")
	}
}

func TestSelectionCommitsWithIntentAndSurvivesStopRestart(t *testing.T) {
	ctx := context.Background()
	first := Job{Layout: "writing", Generation: "one", Listen: "127.0.0.1:9090"}
	second := Job{Layout: "coding", Generation: "two", Listen: first.Listen}
	s := &memoryStore{state: State{Schema: StateSchema}}
	c := &jobController{}
	if _, err := Reconcile(ctx, s, c, &first, false); err != nil {
		t.Fatal(err)
	}
	for _, dry := range []bool{true, false} {
		before := s.state
		c.busy = !dry
		_, err := Reconcile(ctx, s, c, &second, dry)
		if !dry && err == nil {
			t.Fatal("busy switch accepted")
		}
		if !reflect.DeepEqual(before, s.state) {
			t.Fatal("dry/busy switch changed remembered choice")
		}
	}
	c.busy = false
	c.failStart = true
	if _, err := Reconcile(ctx, s, c, &second, false); err == nil {
		t.Fatal("expected lost response")
	}
	if s.state.Selected.Layout != "coding" {
		t.Fatal("accepted switch did not remember intent")
	}
	if _, err := Reconcile(ctx, s, c, &second, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Reconcile(ctx, s, c, nil, false); err != nil {
		t.Fatal(err)
	}
	// A new CLI process receives only persisted state, not an in-memory choice.
	b, err := json.Marshal(s.state)
	if err != nil {
		t.Fatal(err)
	}
	var reopened State
	if err := json.Unmarshal(b, &reopened); err != nil {
		t.Fatal(err)
	}
	if reopened.Selected.Layout != "coding" || reopened.Selected.Listen != first.Listen || reopened.Current != nil || reopened.Desired != nil {
		t.Fatalf("stopped selection: %+v", reopened)
	}
	s = &memoryStore{state: reopened}
	if _, err := Reconcile(ctx, s, c, &second, false); err != nil {
		t.Fatal(err)
	}
	starts := c.starts
	if _, err := Reconcile(ctx, s, c, &second, false); err != nil || c.starts != starts {
		t.Fatal("repeated start was not clean", err)
	}
}

func TestStoppedLegacySelectionMigrationIsReadOnlyAndNeverGuesses(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "managed"), 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "managed", "activation.json")
	raw := []byte("{\"schema\":\"temper-activation/v1\"}\n")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := (FileStore{Root: root}).Read()
	if err != nil || s.Selected != nil || s.Schema != StateSchema {
		t.Fatal(s, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(raw) {
		t.Fatal("read migrated bytes", err)
	}
	old := Job{Layout: "writing", Listen: "127.0.0.1:9090"}
	next := Job{Layout: "coding", Listen: old.Listen}
	if selected := (State{Current: &old, Desired: &next}).Selection(); selected.Layout != "coding" {
		t.Fatal("pending activation lost its selected intent")
	}
	if selected := (State{Current: &old}).Selection(); selected.Layout != "writing" {
		t.Fatal("pending stop lost its previous layout")
	}
}
