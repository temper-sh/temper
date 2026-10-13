package managed

import (
	"context"
	"errors"
	"fmt"
)

const StateSchema = "temper-activation/v1"

type State struct {
	Schema  string `json:"schema"`
	Desired *Job   `json:"desired,omitempty"`
	Current *Job   `json:"current,omitempty"`
}
type Observation struct {
	Exists     bool              `json:"exists"`
	Ready      bool              `json:"ready"`
	Busy       bool              `json:"busy"`
	PID        int               `json:"pid,omitempty"`
	Generation string            `json:"generation,omitempty"`
	Models     map[string]string `json:"models,omitempty"`
}
type Status struct {
	State    State       `json:"state"`
	Observed Observation `json:"observed"`
	Pending  bool        `json:"pending"`
	Error    string      `json:"error,omitempty"`
}

type Controller interface {
	Recover(context.Context, []Job) error
	Observe(context.Context, []Job) (Observation, error)
	Start(context.Context, Job) error
	Stop(context.Context, Job) error
}
type Store interface {
	Read() (State, error)
	Save(context.Context, State) error
}

func known(s State) []Job {
	var jobs []Job
	if s.Current != nil {
		jobs = append(jobs, *s.Current)
	}
	if s.Desired != nil && (s.Current == nil || s.Current.Generation != s.Desired.Generation) {
		jobs = append(jobs, *s.Desired)
	}
	return jobs
}
func same(a, b *Job) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Generation == b.Generation && a.Layout == b.Layout && a.Name == b.Name
}

func Inspect(ctx context.Context, store Store, controller Controller) (Status, error) {
	s, err := store.Read()
	if err != nil {
		return Status{}, err
	}
	o, err := controller.Observe(ctx, known(s))
	status := Status{State: s, Observed: o, Pending: !same(s.Desired, s.Current) || s.Desired != nil && !o.Ready || s.Desired == nil && o.Exists}
	if err != nil {
		status.Error = err.Error()
	}
	return status, err
}

// Reconcile runs under the caller's root lock. Desired state commits before job
// effects. A restart observes both sides of an interrupted transition rather
// than trusting a stored PID or assuming the previous command failed.
func Reconcile(ctx context.Context, store Store, controller Controller, desired *Job, dry bool) (Status, error) {
	s, err := store.Read()
	if err != nil {
		return Status{}, err
	}
	if !dry {
		if err := controller.Recover(ctx, known(s)); err != nil {
			return Status{State: s, Pending: true, Error: err.Error()}, err
		}
	}
	o, err := controller.Observe(ctx, known(s))
	if err != nil {
		return Status{State: s, Error: err.Error()}, err
	}
	if o.Busy && (desired == nil || o.Generation != desired.Generation) {
		return Status{State: s, Observed: o}, errors.New("active work prevents this transition; retry when requests finish")
	}
	if dry {
		return Status{State: State{Schema: StateSchema, Desired: desired, Current: s.Current}, Observed: o, Pending: !same(desired, s.Current)}, nil
	}
	if same(s.Desired, desired) && same(s.Current, desired) && ((desired == nil && !o.Exists) || (desired != nil && o.Ready && o.Generation == desired.Generation)) {
		return Status{State: s, Observed: o}, nil
	}
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	priorJobs := known(s)
	s.Schema = StateSchema
	s.Desired = desired
	// Bind a just-started job observed after an interrupted prior Start.
	for _, job := range priorJobs {
		if o.Exists && job.Generation == o.Generation {
			s.Current = &job
			break
		}
	}
	if err := store.Save(ctx, s); err != nil {
		return Status{}, err
	}
	fail := func(err error) (Status, error) {
		return Status{State: s, Observed: o, Pending: true, Error: err.Error()}, err
	}
	if o.Exists && (desired == nil || desired.Generation != o.Generation || o.PID == 0) {
		if s.Current == nil || s.Current.Generation != o.Generation {
			return fail(errors.New("observed job has no matching owned generation"))
		}
		if err := controller.Stop(ctx, *s.Current); err != nil {
			return fail(err)
		}
		o, err = controller.Observe(ctx, known(s))
		if err != nil {
			return fail(err)
		}
		if o.Exists {
			return fail(errors.New("old job or descendants remain; new layout was not started"))
		}
		s.Current = nil
		if err := store.Save(ctx, s); err != nil {
			return fail(err)
		}
	}
	if desired != nil && (!o.Exists || o.PID == 0) {
		if err := controller.Start(ctx, *desired); err != nil {
			return fail(err)
		}
		o, err = controller.Observe(ctx, known(s))
		if err != nil {
			return fail(err)
		}
		if !o.Exists || o.Generation != desired.Generation {
			return fail(fmt.Errorf("activation pending: requested router is not observed"))
		}
	}
	if desired != nil && !o.Ready {
		return fail(errors.New("activation pending: router is not ready; retry activation or inspect status"))
	}
	s.Current = desired
	if err := store.Save(ctx, s); err != nil {
		return fail(err)
	}
	return Status{State: s, Observed: o}, nil
}
