// Package preset prepares shared exact material independently of user layouts.
package preset

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/temper-sh/temper/internal/artifactset"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/probecmd"
	"github.com/temper-sh/temper/internal/render"
	"github.com/temper-sh/temper/internal/render/engine"
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/adapter/upstreamrelease"
	"github.com/temper-sh/temper/internal/software/installplan"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/receiptstore"
	"github.com/temper-sh/temper/internal/splash"
)

type Installation struct {
	ID, Package string
	Lock        softwarelock.Document
}

// Software gives each independently reusable closure one immutable installation
// identity. Provenance is retained in the lock but never used as material identity.
func Software(lock catalog.Lock) ([]Installation, error) {
	p, err := lock.Projections()
	if err != nil {
		return nil, err
	}
	var result []Installation
	ids := make([]string, 0, len(p.Software.Selections))
	for id := range p.Software.Selections {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		s := p.Software
		s.Selections = map[string]softwarelock.Selection{id: p.Software.Selections[id]}
		s.Units = map[string]softwarelock.Unit{}
		var visit func(string)
		visit = func(key string) {
			if _, ok := s.Units[key]; ok {
				return
			}
			unit := p.Software.Units[key]
			s.Units[key] = unit
			for _, dep := range unit.Dependencies {
				visit(dep)
			}
		}
		visit(s.Selections[id].RootUnit)
		digest, err := s.SemanticDigest()
		if err != nil {
			return nil, err
		}
		result = append(result, Installation{ID: "preset-" + digest, Package: id, Lock: s})
	}
	return result, nil
}

func Executable(root string, lock catalog.Lock, packageID, relative string) (string, error) {
	sets, err := Software(lock)
	if err != nil {
		return "", err
	}
	for _, set := range sets {
		if set.Package == packageID {
			return probecmd.InstalledExecutableFromLock(root, set.ID, set.Lock, packageID, relative)
		}
	}
	return "", fmt.Errorf("preset has no %s software", packageID)
}

type Dispatch func(context.Context, []string, io.Writer, io.Writer) int

// Installed credits only an exact receipt and provider observation. The zero
// release inspector has no transport, so this admission cannot download.
func Installed(ctx context.Context, root string, set Installation) (bool, error) {
	snapshot, err := receiptstore.Read(root, set.ID)
	if err != nil || !snapshot.Exists() {
		return false, err
	}
	installation := installplan.Installation{ID: set.ID, Root: root}
	if err = snapshot.Document.ValidateAgainst(set.Lock, installation); err != nil {
		return false, err
	}
	var inspector upstreamrelease.InstallationAdapter
	for _, unit := range set.Lock.Units {
		if unit.Adapter != inspector.Descriptor().ID {
			return false, nil
		}
	}
	units, err := inspector.Inspect(ctx, adapter.InspectRequest{Target: set.Lock.Target, Installation: installation, Units: set.Lock.Units})
	if err != nil {
		return false, err
	}
	if err = snapshot.Document.VerifyObservation(installplan.Observation{Target: set.Lock.Target, Root: root, Units: units}); err != nil {
		return false, err
	}
	return true, nil
}

// Prepare uses the existing installers and content-addressed model sets. It
// starts no router or inference engine. Already verified closures are reused.
func Prepare(ctx context.Context, root string, lock catalog.Lock, dispatch Dispatch, out, diagnostics io.Writer) error {
	p, err := lock.Projections()
	if err != nil {
		return err
	}
	files, err := p.Files()
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "temper-preset-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(tmp, name), data, 0600); err != nil {
			return err
		}
	}
	run := func(args ...string) error {
		if code := dispatch(ctx, args, out, diagnostics); code != 0 {
			return fmt.Errorf("preset preparation failed at %s (exit %d)", args[0], code)
		}
		return nil
	}
	sets, err := Software(lock)
	if err != nil {
		return err
	}
	for _, set := range sets {
		raw, err := softwarelock.Marshal(set.Lock)
		if err != nil {
			return err
		}
		path := filepath.Join(tmp, set.Package+".yaml")
		if err = os.WriteFile(path, raw, 0600); err != nil {
			return err
		}
		if err = run("software", "install", "--root", root, "--installation", set.ID, "--lock", path); err != nil {
			return err
		}
		if err = run("software", "check", "--root", root, "--installation", set.ID, "--lock", path); err != nil {
			return err
		}
	}
	for id, layout := range p.Manifest.Layouts {
		if err = run("fetch", id, "--root", root, "--manifest", filepath.Join(tmp, "manifest.yaml"), "--lock", filepath.Join(tmp, "manifest.lock.yaml")); err != nil {
			return err
		}
		if layout.Splash != nil {
			material, err := splash.New(root, id, layout, p.Artifacts.Entries[id], p.Manifest.Patches)
			if err != nil {
				return err
			}
			python, err := Executable(root, lock, engine.Splash, engine.SplashPython)
			if err != nil {
				return err
			}
			if err = material.Prepare(ctx, python, splash.Derive); err != nil {
				return err
			}
		}
	}
	return nil
}

// Verify reads exact installed software and artifact receipts before activation.
func Verify(ctx context.Context, root string, lock catalog.Lock, dispatch Dispatch) error {
	p, err := lock.Projections()
	if err != nil {
		return err
	}
	sets, err := Software(lock)
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "temper-verify-preset-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	for _, set := range sets {
		raw, err := softwarelock.Marshal(set.Lock)
		if err != nil {
			return err
		}
		path := filepath.Join(tmp, set.Package+".yaml")
		if err = os.WriteFile(path, raw, 0600); err != nil {
			return err
		}
		var report bytes.Buffer
		if code := dispatch(ctx, []string{"software", "check", "--root", root, "--installation", set.ID, "--lock", path}, &report, &report); code != 0 {
			return fmt.Errorf("prepared software verification: %s", report.String())
		}
	}
	for id, layout := range p.Manifest.Layouts {
		set, err := artifactset.New(root, id, layout, p.Artifacts.Entries[id], p.Manifest.Patches)
		if err != nil {
			return err
		}
		if err = set.Verify(); err != nil {
			return err
		}
		if layout.Splash != nil {
			m, err := splash.New(root, id, layout, p.Artifacts.Entries[id], p.Manifest.Patches)
			if err != nil {
				return err
			}
			if err = m.Verify(); err != nil {
				return err
			}
		}
	}
	return nil
}

func Render(root string, lock catalog.Lock) (render.Bundle, error) {
	p, err := lock.Projections()
	if err != nil {
		return render.Bundle{}, err
	}
	return render.Build(render.Inputs{Root: root, Mode: lock.Preset, Manifest: p.Manifest, Lock: p.Artifacts})
}

func PrepareState(root string, lock catalog.Lock) error {
	p, err := lock.Projections()
	if err != nil {
		return err
	}
	for id, layout := range p.Manifest.Layouts {
		if layout.Splash != nil {
			m, err := splash.New(root, id, layout, p.Artifacts.Entries[id], p.Manifest.Patches)
			if err != nil {
				return err
			}
			if err = m.PrepareState(); err != nil {
				return err
			}
		}
	}
	return nil
}
