// Package adapter owns the keyed installer-adapter family. This file keeps
// provider inspection and installation as separate calls even when one member
// implements both roles.
package adapter

import (
	"context"
	"fmt"
	"sort"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/installplan"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/removeplan"
)

// InspectRequest asks one exact adapter to normalize provider state for only
// the lock units it owns. Absent units may carry an absolute InstallLocation.
type InspectRequest struct {
	Target       software.Target
	Installation installplan.Installation
	Units        map[string]softwarelock.Unit
}

// InstallRequest is one complete dependency-ordered adapter/scope group.
// Adapters execute this absolute plan and never resolve or reinterpret it.
type InstallRequest struct {
	Target       software.Target
	Installation installplan.Installation
	Group        installplan.Group
	Units        map[string]softwarelock.Unit
}

// RemoveRequest is one complete reverse-dependency-ordered adapter/scope
// group. The pure plan has already proved removal authority.
type RemoveRequest struct {
	Target       software.Target
	Installation installplan.Installation
	Group        removeplan.Group
	Units        map[string]softwarelock.Unit
}

type StateInspector interface {
	Descriptor() Descriptor
	Inspect(context.Context, InspectRequest) (map[string]installplan.ObservedUnit, error)
}

type GroupInstaller interface {
	Descriptor() Descriptor
	Install(context.Context, InstallRequest) error
}

type GroupRemover interface {
	Descriptor() Descriptor
	Remove(context.Context, RemoveRequest) error
}

// InstallationAdapter is one keyed member supplying both narrow roles. The
// family exposes those roles independently to orchestration.
type InstallationAdapter interface {
	StateInspector
	GroupInstaller
	GroupRemover
}

type InstallationFamily struct {
	registry Registry
	members  map[string]InstallationAdapter
}

func NewInstallationFamily(members ...InstallationAdapter) (InstallationFamily, error) {
	descriptors := make([]Descriptor, 0, len(members))
	byID := make(map[string]InstallationAdapter, len(members))
	for index, member := range members {
		if member == nil {
			return InstallationFamily{}, fmt.Errorf("installation adapter[%d] is nil", index)
		}
		descriptor := member.Descriptor()
		if _, exists := byID[descriptor.ID]; exists {
			return InstallationFamily{}, fmt.Errorf("installation adapter %q is registered more than once", descriptor.ID)
		}
		descriptors = append(descriptors, descriptor)
		byID[descriptor.ID] = member
	}
	registry, err := NewRegistry(descriptors...)
	if err != nil {
		return InstallationFamily{}, err
	}
	return InstallationFamily{registry: registry, members: byID}, nil
}

// EffectModels verifies every exact lock adapter for this target and returns
// the compiled semantics consumed by the pure planner.
func (f InstallationFamily) EffectModels(desired softwarelock.Document) (map[string]installplan.EffectModel, error) {
	if err := desired.Validate(); err != nil {
		return nil, err
	}
	models := map[string]installplan.EffectModel{}
	for _, adapterID := range adapterIDs(desired.Units) {
		descriptor, err := f.registry.Require(adapterID, desired.Target)
		if err != nil {
			return nil, err
		}
		models[adapterID] = installplan.EffectModel(descriptor.EffectModel)
	}
	return models, nil
}

// Inspect reads every adapter in deterministic key order and combines only
// provider-neutral observations.
func (f InstallationFamily) Inspect(ctx context.Context, target software.Target, installation installplan.Installation, units map[string]softwarelock.Unit) (installplan.Observation, error) {
	grouped := unitsByAdapter(units)
	observed := installplan.Observation{Target: target, Root: installation.Root, Units: make(map[string]installplan.ObservedUnit, len(units))}
	for _, adapterID := range sortedUnitGroupKeys(grouped) {
		if err := ctx.Err(); err != nil {
			return installplan.Observation{}, err
		}
		descriptor, err := f.registry.Require(adapterID, target)
		if err != nil {
			return installplan.Observation{}, err
		}
		member := f.members[descriptor.ID]
		got, err := member.Inspect(ctx, InspectRequest{Target: target, Installation: installation, Units: cloneUnits(grouped[adapterID])})
		if err != nil {
			return installplan.Observation{}, fmt.Errorf("inspect software with adapter %q: %w", adapterID, err)
		}
		for unitID := range grouped[adapterID] {
			unit, ok := got[unitID]
			if !ok {
				return installplan.Observation{}, fmt.Errorf("adapter %q inspection omitted unit %q", adapterID, unitID)
			}
			observed.Units[unitID] = unit
		}
		for unitID := range got {
			if _, ok := grouped[adapterID][unitID]; !ok {
				return installplan.Observation{}, fmt.Errorf("adapter %q inspection returned unexpected unit %q", adapterID, unitID)
			}
		}
	}
	return observed, nil
}

// Install executes one already-validated group through its exact lock adapter.
func (f InstallationFamily) Install(ctx context.Context, target software.Target, installation installplan.Installation, group installplan.Group, units map[string]softwarelock.Unit) error {
	descriptor, err := f.registry.Require(group.Adapter, target)
	if err != nil {
		return err
	}
	if descriptor.EffectModel != string(group.EffectModel) {
		return fmt.Errorf("installation group %q effect model %q differs from compiled adapter %q", group.ID, group.EffectModel, descriptor.EffectModel)
	}
	selected := make(map[string]softwarelock.Unit, len(group.Units))
	for _, planned := range group.Units {
		locked, ok := units[planned.ID]
		if !ok {
			return fmt.Errorf("installation group %q references unknown lock unit %q", group.ID, planned.ID)
		}
		if locked.Adapter != group.Adapter || locked.Scope != group.Scope {
			return fmt.Errorf("installation group %q unit %q differs from its adapter or scope", group.ID, planned.ID)
		}
		selected[planned.ID] = locked
	}
	if err := f.members[descriptor.ID].Install(ctx, InstallRequest{
		Target: target, Installation: installation, Group: group, Units: selected,
	}); err != nil {
		return fmt.Errorf("install software with adapter %q scope %q: %w", group.Adapter, group.Scope, err)
	}
	return nil
}

// Remove executes one already-validated provenance-guided group through its
// exact lock adapter.
func (f InstallationFamily) Remove(ctx context.Context, target software.Target, installation installplan.Installation, group removeplan.Group, units map[string]softwarelock.Unit) error {
	descriptor, err := f.registry.Require(group.Adapter, target)
	if err != nil {
		return err
	}
	if descriptor.EffectModel != string(group.EffectModel) {
		return fmt.Errorf("removal group %q effect model %q differs from compiled adapter %q", group.ID, group.EffectModel, descriptor.EffectModel)
	}
	if group.ID != group.Adapter+":"+group.Scope {
		return fmt.Errorf("removal group %q differs from its adapter or scope", group.ID)
	}
	selected := make(map[string]softwarelock.Unit, len(group.Units))
	for _, planned := range group.Units {
		if planned.Action != removeplan.ActionPreserve && planned.Action != removeplan.ActionRemove {
			return fmt.Errorf("removal group %q unit %q has invalid action %q", group.ID, planned.ID, planned.Action)
		}
		if planned.Execute && planned.Action != removeplan.ActionRemove {
			return fmt.Errorf("removal group %q unit %q executes a preserve action", group.ID, planned.ID)
		}
		locked, ok := units[planned.ID]
		if !ok {
			return fmt.Errorf("removal group %q references unknown lock unit %q", group.ID, planned.ID)
		}
		if locked.Adapter != group.Adapter || locked.Scope != group.Scope {
			return fmt.Errorf("removal group %q unit %q differs from its adapter or scope", group.ID, planned.ID)
		}
		selected[planned.ID] = locked
	}
	if err := f.members[descriptor.ID].Remove(ctx, RemoveRequest{
		Target: target, Installation: installation, Group: group, Units: selected,
	}); err != nil {
		return fmt.Errorf("remove software with adapter %q scope %q: %w", group.Adapter, group.Scope, err)
	}
	return nil
}

func unitsByAdapter(units map[string]softwarelock.Unit) map[string]map[string]softwarelock.Unit {
	grouped := map[string]map[string]softwarelock.Unit{}
	for unitID, unit := range units {
		if grouped[unit.Adapter] == nil {
			grouped[unit.Adapter] = map[string]softwarelock.Unit{}
		}
		grouped[unit.Adapter][unitID] = unit
	}
	return grouped
}

func adapterIDs(units map[string]softwarelock.Unit) []string {
	grouped := unitsByAdapter(units)
	return sortedUnitGroupKeys(grouped)
}

func sortedUnitGroupKeys(values map[string]map[string]softwarelock.Unit) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func cloneUnits(units map[string]softwarelock.Unit) map[string]softwarelock.Unit {
	result := make(map[string]softwarelock.Unit, len(units))
	for id, unit := range units {
		unit.Dependencies = append([]string(nil), unit.Dependencies...)
		unit.Artifacts = append([]software.Artifact(nil), unit.Artifacts...)
		result[id] = unit
	}
	return result
}
