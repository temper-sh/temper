package managed

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const StateSchema = "temper-activation/v2"

// ErrBusy matches every refusal caused by requests the router is answering.
// A restart never interrupts that work.
var ErrBusy = errors.New("router is answering requests")

type busyError string

func (e busyError) Error() string      { return string(e) }
func (busyError) Is(target error) bool { return target == ErrBusy }

// idlePoll is how often a waiting transition observes the router again.
var idlePoll = time.Second

type Selection struct {
	Layout string `json:"layout"`
	Listen string `json:"listen"`
}

type State struct {
	Schema   string     `json:"schema"`
	Desired  *Job       `json:"desired,omitempty"`
	Current  *Job       `json:"current,omitempty"`
	Selected *Selection `json:"selected,omitempty"`
}

// Selection survives stop and contains no process ownership authority.
func (s State) Selection() *Selection {
	if s.Selected != nil {
		return s.Selected
	}
	job := s.Desired
	if job == nil {
		job = s.Current
	}
	if job == nil {
		return nil
	}
	return &Selection{Layout: job.Layout, Listen: job.Listen}
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
// than trusting a stored PID or assuming the previous command failed. Active
// requests refuse the transition with ErrBusy.
func Reconcile(ctx context.Context, store Store, controller Controller, desired *Job, dry bool) (Status, error) {
	return reconcile(ctx, store, controller, desired, dry, nil)
}

// ReconcileWhenIdle is Reconcile that waits for active requests to finish
// instead of refusing, so a restart happens between requests. waiting runs
// once, when waiting starts. Cancelling ctx while waiting changes nothing.
func ReconcileWhenIdle(ctx context.Context, store Store, controller Controller, desired *Job, waiting func(Observation)) (Status, error) {
	if waiting == nil {
		waiting = func(Observation) {}
	}
	return reconcile(ctx, store, controller, desired, false, waiting)
}

func reconcile(ctx context.Context, store Store, controller Controller, desired *Job, dry bool, waiting func(Observation)) (Status, error) {
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
	announced := false
	// untilIdle observes again until the router has no active request.
	untilIdle := func() error {
		if !announced {
			waiting(o)
			announced = true
		}
		for o.Busy {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(idlePoll):
			}
			if o, err = controller.Observe(ctx, known(s)); err != nil {
				return err
			}
		}
		return nil
	}
	if o.Busy && (desired == nil || o.Generation != desired.Generation) {
		if waiting == nil {
			return Status{State: s, Observed: o}, busyError("active work prevents this transition; retry when requests finish")
		}
		if err := untilIdle(); err != nil {
			return Status{State: s, Observed: o}, err
		}
	}
	if dry {
		return Status{State: State{Schema: StateSchema, Desired: desired, Current: s.Current, Selected: s.Selection()}, Observed: o, Pending: !same(desired, s.Current)}, nil
	}
	if same(s.Desired, desired) && same(s.Current, desired) && ((desired == nil && !o.Exists) || (desired != nil && o.Ready && o.Generation == desired.Generation)) {
		return Status{State: s, Observed: o}, nil
	}
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	priorJobs := known(s)
	s.Schema = StateSchema
	s.Selected = s.Selection()
	if desired != nil {
		s.Selected = &Selection{Layout: desired.Layout, Listen: desired.Listen}
	}
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
		// A request can arrive between the last observation and Stop. Stop refuses
		// it with ErrBusy; a waiting transition waits again and retries.
		for {
			err := controller.Stop(ctx, *s.Current)
			if err == nil {
				break
			}
			if waiting == nil || !errors.Is(err, ErrBusy) {
				return fail(err)
			}
			o.Busy = true
			if err := untilIdle(); err != nil {
				return fail(err)
			}
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
