package managed

import (
	"context"
	"errors"
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
}
