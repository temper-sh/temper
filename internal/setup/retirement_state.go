package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/temper-sh/temper/internal/datadir"
)

const RetirementFile = "retirement.json"
const RetirementSchema = "temper-retirement/v1"

// RetirementState retains the reviewed selection and cleanup consent across a
// crash. Retired locks identify only material this operation may release.
type RetirementState struct {
	Schema         string             `json:"schema"`
	BeforeRevision string             `json:"before_revision"`
	AfterRevision  string             `json:"after_revision"`
	Configuration  Configuration      `json:"configuration"`
	Retired        map[string]Preset  `json:"retired"`
	Changes        []RetirementChange `json:"changes"`
	DeleteWeights  bool               `json:"delete_weights"`
	Activated      bool               `json:"activated"`
	Revision       string             `json:"-"`
	PreviousState  json.RawMessage    `json:"previous_managed_state,omitempty"`
	HFCache        string             `json:"hf_cache,omitempty"`
}

func (s RetirementState) Validate() error {
	if s.Schema != RetirementSchema || s.BeforeRevision == "" || s.AfterRevision == "" || len(s.Retired) == 0 || len(s.Changes) == 0 {
		return errors.New("invalid retirement intent")
	}
	if s.HFCache != "" && !filepath.IsAbs(s.HFCache) || s.DeleteWeights && s.HFCache == "" {
		return errors.New("retirement weight deletion requires its reviewed absolute HF cache path")
	}
	if len(s.PreviousState) > 0 && !json.Valid(s.PreviousState) {
		return errors.New("invalid previous managed state")
	}
	raw, err := s.Configuration.Bytes()
	if err != nil {
		return err
	}
	if revision(raw) != s.AfterRevision {
		return errors.New("retirement configuration does not match its reviewed revision")
	}
	for id, p := range s.Retired {
		if !choiceID.MatchString(id) {
			return errors.New("invalid retired saved ID")
		}
		if _, ok := s.Configuration.Presets[id]; ok {
			return errors.New("retired selection is still present")
		}
		if err := p.Lock.Validate(); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, change := range s.Changes {
		if _, ok := s.Retired[change.Preset]; !ok || seen[change.Preset] {
			return errors.New("retirement changes must name each removed selection once")
		}
		seen[change.Preset] = true
		if change.Successor != "" {
			if _, ok := s.Configuration.Presets[change.Successor]; !ok {
				return errors.New("retirement successor is not selected")
			}
		}
	}
	if len(seen) != len(s.Retired) {
		return errors.New("unreviewed removed selection in retirement intent")
	}
	return nil
}

func ReadRetirement(root string) (*RetirementState, error) {
	if _, err := ExistingDirectory(root); err != nil {
		return nil, err
	}
	if err := datadir.Check(root); err != nil {
		return nil, err
	}
	raw, err := readRegular(filepath.Join(root, RetirementFile), 64<<20)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, unreadableRetirement(err)
	}
	var state RetirementState
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err = d.Decode(&state); err != nil {
		return nil, unreadableRetirement(err)
	}
	if err = d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, unreadableRetirement(errors.New("retirement intent requires one JSON document"))
	}
	if err = state.Validate(); err != nil {
		return nil, unreadableRetirement(err)
	}
	state.Revision = revision(raw)
	return &state, nil
}

// An intent this Temper cannot trust still blocks configuration changes, but
// names the explicit way out instead of leaving a manual file deletion.
func unreadableRetirement(err error) error {
	return fmt.Errorf("unreadable retirement intent: %w; temper configure --discard-retirement keeps the current selection and files", err)
}

// DiscardRetirement removes pending intent without interpreting it, so even an
// unreadable intent can be discarded. Saved selections and files stay as they
// are. A dry run only reports whether intent exists.
func DiscardRetirement(root string, dry bool) (bool, error) {
	path := filepath.Join(root, RetirementFile)
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if err := datadir.Check(root); err != nil || dry {
		return err == nil, err
	}
	unlock, err := LockRoot(root)
	if err != nil {
		return false, err
	}
	defer unlock()
	if err = os.Remove(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, syncDir(root)
}

// SaveRetirement publishes one guarded, synced checkpoint. It never replaces a
// concurrent retirement operation, including one created while preparing files.
func SaveRetirement(ctx context.Context, root string, state *RetirementState) error {
	if err := state.Validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := datadir.Ensure(root); err != nil {
		return err
	}
	unlock, err := LockRoot(root)
	if err != nil {
		return err
	}
	defer unlock()
	old, err := ReadRetirement(root)
	if err != nil {
		return err
	}
	got := ""
	if old != nil {
		got = old.Revision
	}
	if got != state.Revision {
		return errors.New("retirement intent changed; resume the existing operation")
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	f, err := os.CreateTemp(root, ".retirement-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), filepath.Join(root, RetirementFile)); err != nil {
		return err
	}
	state.Revision = revision(raw)
	return syncDir(root)
}

func ClearRetirement(root string, state *RetirementState) error {
	// Caller holds the root lock through cleanup and clearing the intent.
	current, err := ReadRetirement(root)
	if err != nil {
		return err
	}
	if current == nil {
		return nil
	}
	if current.Revision != state.Revision {
		return fmt.Errorf("retirement intent changed during cleanup")
	}
	if err := os.Remove(filepath.Join(root, RetirementFile)); err != nil {
		return err
	}
	return syncDir(root)
}
