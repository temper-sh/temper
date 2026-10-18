// Package preset prepares shared exact material independently of user layouts.
package preset

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"slices"

	"github.com/temper-sh/temper/internal/artifactset"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/datadir"
	"github.com/temper-sh/temper/internal/fetch"
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
			return probecmd.InstalledExecutable(root, set.ID, set.Lock, packageID, relative)
		}
	}
	return "", fmt.Errorf("preset has no %s software", packageID)
}

// Installer installs and audits one exact software lock under a named
// installation, writing its report lines to out.
type Installer interface {
	Install(ctx context.Context, root, installation string, lock softwarelock.Document, out io.Writer) error
	Check(ctx context.Context, root, installation string, lock softwarelock.Document, out io.Writer) error
}

// Effects are the machine primitives preparation composes.
type Effects struct {
	Software Installer
	Fetch    fetch.Func
}

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

// Prepare installs and checks each exact software closure, then materializes
// every layout's model files and Splash conversion. It starts no router or
// inference engine. Already verified closures and sets are reused. Progress
// is one plain line per step; the software report stays with temper software.
func Prepare(ctx context.Context, root string, lock catalog.Lock, effects Effects, progress io.Writer) error {
	p, err := lock.Projections()
	if err != nil {
		return err
	}
	sets, err := Software(lock)
	if err != nil {
		return err
	}
	for _, set := range sets {
		name := set.Package
		if unit, ok := set.Lock.Units[set.Lock.Selections[set.Package].RootUnit]; ok && unit.Version != "" {
			name += " " + unit.Version
		}
		if installed, err := Installed(ctx, root, set); err == nil && installed {
			fmt.Fprintf(progress, "%s is already installed\n", name)
		} else {
			fmt.Fprintf(progress, "Installing %s\n", name)
		}
		if err := effects.Software.Install(ctx, root, set.ID, set.Lock, io.Discard); err != nil {
			return fmt.Errorf("install %s: %w", set.Package, err)
		}
		if err := effects.Software.Check(ctx, root, set.ID, set.Lock, io.Discard); err != nil {
			return fmt.Errorf("check %s: %w", set.Package, err)
		}
	}
	name := DisplayName(lock)
	for _, id := range slices.Sorted(maps.Keys(p.Manifest.Layouts)) {
		layout := p.Manifest.Layouts[id]
		fmt.Fprintf(progress, "Getting model files for %s\n", name)
		if err := effects.Fetch(ctx, root, id, p.Manifest, p.Artifacts); err != nil {
			return fmt.Errorf("fetch %s: %w", id, err)
		}
		if layout.Splash != nil {
			fmt.Fprintf(progress, "Preparing the Splash files for %s\n", name)
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

// DisplayName is the preset's catalog name, or its ID when it has none.
func DisplayName(lock catalog.Lock) string {
	if p, ok := lock.Records.Presets[lock.Preset]; ok && p.Presentation.DisplayName != "" {
		return p.Presentation.DisplayName
	}
	return lock.Preset
}

// Verify reads exact installed software and artifact receipts before activation.
func Verify(ctx context.Context, root string, lock catalog.Lock, software Installer) error {
	p, err := lock.Projections()
	if err != nil {
		return err
	}
	sets, err := Software(lock)
	if err != nil {
		return err
	}
	for _, set := range sets {
		var report bytes.Buffer
		if err := software.Check(ctx, root, set.ID, set.Lock, &report); err != nil {
			return fmt.Errorf("prepared software verification: %w\n%s", err, report.String())
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
	return render.Build(render.Inputs{Root: root, StartPort: datadir.StartPort(root), Mode: lock.Preset, Manifest: p.Manifest, Lock: p.Artifacts})
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
