package datadir

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// MarkerFile versions everything inside a Temper root at once, so internal
// files need no schema of their own.
const MarkerFile = "temper-root.json"

const markerSchema = "temper-root/v1"

// Format is the root layout this Temper reads and writes. A later Temper that
// changes it migrates older roots; an older Temper refuses newer ones.
const Format = 1

// owned lists the top-level entries a Temper root holds. Other files, such as
// a Field Kit session's, do not make a folder a Temper root.
var owned = []string{"configuration.json", ".setup.lock", "catalog", "software", "managed", "rendered", "runtime", "runtime-state", "prepared", "artifacts"}

type marker struct {
	Schema string `json:"schema"`
	Format int    `json:"format"`
}

// Check opens a root. A missing root, or one without Temper content, is fine;
// the same format continues; a newer format, or Temper content without a
// marker, is refused.
func Check(root string) error {
	raw, err := os.ReadFile(filepath.Join(root, MarkerFile))
	if errors.Is(err, fs.ErrNotExist) {
		for _, name := range owned {
			if _, err := os.Lstat(filepath.Join(root, name)); err == nil {
				return fmt.Errorf("%s holds files from a Temper before 0.1.0 (no %s); move it away or delete it, then run temper init", root, MarkerFile)
			} else if !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
		return nil
	}
	if err != nil {
		return err
	}
	var m marker
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil || m.Schema != markerSchema || m.Format < 1 {
		return fmt.Errorf("%s: %s is not a Temper root marker", root, MarkerFile)
	}
	if m.Format > Format {
		return fmt.Errorf("%s was written by a newer Temper (root format %d); upgrade Temper", root, m.Format)
	}
	// Format 1 is the first; migrations from older formats go here.
	return nil
}

// Ensure checks a root, creates it when missing and writes its marker. Every
// command that writes into a root calls it first.
func Ensure(root string) error {
	if err := Check(root); err != nil {
		return err
	}
	return Mark(root)
}

// Mark writes the marker into a root the caller checked before its first
// change, for a writer that marks the root only once that change commits.
func Mark(root string) error {
	path := filepath.Join(root, MarkerFile)
	if _, err := os.Lstat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(marker{Schema: markerSchema, Format: Format})
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(root, ".temper-root-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(raw, '\n')); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	return err
}

// Open resolves a root a command was given, checks it and, for a command
// that will write, ensures its marker.
func Open(path string, write bool) error {
	root, err := Resolve(path)
	if err != nil {
		return err
	}
	if write {
		return Ensure(root)
	}
	return Check(root)
}
