package managed

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/setup"
)

type FileStore struct{ Root string }

func (s FileStore) Read() (State, error) {
	path := filepath.Join(s.Root, "managed", "activation.json")
	if _, err := setup.ExistingDirectory(filepath.Dir(path)); err != nil {
		return State{}, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{Schema: StateSchema}, nil
	}
	if err != nil {
		return State{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > 4<<20 {
		return State{}, errors.New("activation state must be a bounded regular file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}
	var state State
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err = d.Decode(&state); err != nil {
		return State{}, err
	}
	if err = d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return State{}, errors.New("activation has trailing data")
	}
	if state.Schema != StateSchema && state.Schema != "temper-activation/v1" {
		return State{}, errors.New("unknown activation schema")
	}
	state.Schema = StateSchema
	state.Selected = state.Selection()
	if err := validateSelection(state.Selected); err != nil {
		return State{}, err
	}
	for _, j := range known(state) {
		if err = j.Validate(s.Root); err != nil {
			return State{}, err
		}
	}
	return state, nil
}
func (s FileStore) Save(ctx context.Context, state State) error {
	if state.Schema != StateSchema {
		return errors.New("invalid activation schema")
	}
	if err := validateSelection(state.Selected); err != nil {
		return err
	}
	for _, j := range known(state) {
		if err := j.Validate(s.Root); err != nil {
			return err
		}
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(ctx, filepath.Join(s.Root, "managed", "activation.json"), append(raw, '\n'))
}

func validateSelection(s *Selection) error {
	if s == nil {
		return nil
	}
	host, port, err := net.SplitHostPort(s.Listen)
	n, e := strconv.Atoi(port)
	if !catalog.ValidPresetID(s.Layout) || err != nil || e != nil || host != "127.0.0.1" || n < 1024 || n > 65535 {
		return errors.New("invalid remembered layout or listener")
	}
	return nil
}

// SelectInitialDefault runs after the first configuration commit. It never
// replaces an existing choice and verifies that the first configuration is
// still the one whose wizard selected Default. Caller reports a failed write
// as saved configuration with incomplete selection, before any installation.
func SelectInitialDefault(ctx context.Context, root, revision string) error {
	unlock, err := setup.LockRoot(root)
	if err != nil {
		return err
	}
	defer unlock()
	c, current, err := setup.ReadConfiguration(root)
	if err != nil {
		return err
	}
	if current != revision {
		return errors.New("configuration changed before initial layout selection; choose with temper layout activate ID")
	}
	if _, ok := c.Layouts["default"]; !ok {
		return nil
	}
	store := FileStore{Root: root}
	state, err := store.Read()
	if err != nil {
		return err
	}
	if state.Selection() != nil {
		return nil
	}
	state.Selected = &Selection{Layout: "default", Listen: "127.0.0.1:8080"}
	return store.Save(ctx, state)
}

// publishLauncher copies the running binary to the job's launcher path once.
// A present copy must already have the job's digest.
func publishLauncher(ctx context.Context, rendered Rendered) error {
	path, digest := rendered.Job.Launcher, rendered.Job.LauncherSHA256
	if existing, err := executableDigest(path); err == nil {
		if existing != digest {
			return errors.New("managed launcher copy was edited")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if rendered.LauncherSource == "" {
		return errors.New("managed launcher copy is missing; activate the layout again")
	}
	raw, err := os.ReadFile(rendered.LauncherSource)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != digest {
		return errors.New("Temper executable changed during activation; activate again")
	}
	return writeAtomicMode(ctx, path, raw, 0o755)
}

func writeAtomic(ctx context.Context, path string, raw []byte) error {
	return writeAtomicMode(ctx, path, raw, 0o600)
}

func writeAtomicMode(ctx context.Context, path string, raw []byte, mode os.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	parent := filepath.Dir(path)
	if _, err := setup.ExistingDirectory(parent); err != nil {
		return err
	}
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return errors.New("refusing nonregular managed file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := os.CreateTemp(parent, ".pending-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Chmod(mode)
	}
	if err == nil {
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
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(parent)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func Publish(ctx context.Context, rendered Rendered) error {
	if err := publishLauncher(ctx, rendered); err != nil {
		return err
	}
	for _, p := range rendered.Job.Processes {
		if p.Record == "" {
			continue
		}
		raw, _ := launchBytes(p, rendered.Job.LauncherSHA256)
		path := filepath.Join(filepath.Dir(p.Record), "launch.json")
		if previous, err := os.ReadFile(path); err == nil {
			if !bytes.Equal(previous, raw) {
				return errors.New("managed launch command was edited")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		} else if err = writeAtomic(ctx, path, raw); err != nil {
			return err
		}
	}
	path := rendered.Job.Config
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("unsafe generation file")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Equal(raw, rendered.Config) {
			return errors.New("managed generation was edited; refusing replacement")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := writeAtomic(ctx, path, rendered.Config); err != nil {
		return fmt.Errorf("publish managed generation: %w", err)
	}
	return nil
}
