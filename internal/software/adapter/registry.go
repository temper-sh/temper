// Package adapter owns the compiled installer-adapter family boundary.
// Catalog policy selects a key; this registry proves that the binary contains
// the matching implementation contract for the exact target.
package adapter

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/catalog"
)

var idPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)

// Descriptor is the pure, compiled identity of one adapter implementation.
// Effectful resolver, inspector, installer, and remover roles attach to this
// key in later slices; catalog data can never provide executable behavior.
type Descriptor struct {
	ID          string
	Method      string
	Protocol    string
	EffectModel string
	Targets     []software.Target
}

func (d Descriptor) Validate() error {
	if !idPattern.MatchString(d.ID) {
		return fmt.Errorf("adapter id %q is not a lowercase stable id", d.ID)
	}
	if !idPattern.MatchString(d.Method) {
		return fmt.Errorf("adapter %q method %q is not a lowercase stable id", d.ID, d.Method)
	}
	if d.Protocol != catalog.AdapterProtocolV1 {
		return fmt.Errorf("adapter %q protocol is %q, want %q", d.ID, d.Protocol, catalog.AdapterProtocolV1)
	}
	if d.EffectModel != "shared" && d.EffectModel != "isolated" {
		return fmt.Errorf("adapter %q effect model %q must be shared or isolated", d.ID, d.EffectModel)
	}
	if len(d.Targets) == 0 {
		return fmt.Errorf("adapter %q must support at least one target", d.ID)
	}
	for index, target := range d.Targets {
		if err := target.Validate(); err != nil {
			return fmt.Errorf("adapter %q target[%d]: %w", d.ID, index, err)
		}
	}
	return nil
}

func (d Descriptor) Supports(target software.Target) bool {
	for _, supported := range d.Targets {
		if supported.Matches(target) {
			return true
		}
	}
	return false
}

// Registry is an immutable keyed adapter family after construction.
type Registry struct {
	descriptors map[string]Descriptor
}

func NewRegistry(descriptors ...Descriptor) (Registry, error) {
	registry := Registry{descriptors: make(map[string]Descriptor, len(descriptors))}
	for _, descriptor := range descriptors {
		if err := descriptor.Validate(); err != nil {
			return Registry{}, err
		}
		if _, exists := registry.descriptors[descriptor.ID]; exists {
			return Registry{}, fmt.Errorf("adapter %q is registered more than once", descriptor.ID)
		}
		descriptor.Targets = append([]software.Target(nil), descriptor.Targets...)
		registry.descriptors[descriptor.ID] = descriptor
	}
	return registry, nil
}

// Resolve applies catalog target policy, then requires the compiled descriptor
// with exactly the contract the catalog declared. It never tries another key.
func (r Registry) Resolve(supply catalog.Document, method string, target software.Target) (Descriptor, error) {
	if err := supply.Validate(); err != nil {
		return Descriptor{}, err
	}
	adapterID, err := supply.AdapterFor(method, target)
	if err != nil {
		return Descriptor{}, err
	}
	declared := supply.Adapters[adapterID]
	descriptor, mismatches, err := r.compareDeclared(adapterID, declared)
	if err != nil {
		return Descriptor{}, err
	}
	if !descriptor.Supports(target) {
		mismatches = append(mismatches, "compiled target support does not include the selected target")
	}
	if len(mismatches) > 0 {
		return Descriptor{}, fmt.Errorf("adapter %q descriptor mismatch: %s", adapterID, strings.Join(mismatches, "; "))
	}
	descriptor.Targets = append([]software.Target(nil), descriptor.Targets...)
	return descriptor, nil
}

// ValidateCatalog proves that every adapter contract and target binding in a
// catalog can be honored by this binary. Catalog updates use it before making
// a snapshot active, rather than discovering an unsupported key at resolve.
func (r Registry) ValidateCatalog(supply catalog.Document) error {
	if err := supply.Validate(); err != nil {
		return err
	}
	ids := make([]string, 0, len(supply.Adapters))
	for id := range supply.Adapters {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		_, mismatches, err := r.compareDeclared(id, supply.Adapters[id])
		if err != nil {
			return fmt.Errorf("catalog adapter capability mismatch: %w", err)
		}
		if len(mismatches) > 0 {
			return fmt.Errorf("catalog adapter capability mismatch: adapter %q descriptor mismatch: %s", id, strings.Join(mismatches, "; "))
		}
	}
	for index, binding := range supply.TargetBindings {
		descriptor := r.descriptors[binding.Adapter]
		if !descriptor.Supports(binding.Target) {
			return fmt.Errorf("catalog adapter capability mismatch: target_bindings[%d] adapter %q does not support declared target", index, binding.Adapter)
		}
	}
	return nil
}

func (r Registry) compareDeclared(adapterID string, declared catalog.Adapter) (Descriptor, []string, error) {
	descriptor, ok := r.descriptors[adapterID]
	if !ok {
		return Descriptor{}, nil, fmt.Errorf("catalog-selected adapter %q is not compiled into this binary", adapterID)
	}
	var mismatches []string
	if descriptor.Method != declared.Method {
		mismatches = append(mismatches, fmt.Sprintf("method compiled=%q catalog=%q", descriptor.Method, declared.Method))
	}
	if descriptor.Protocol != declared.Protocol {
		mismatches = append(mismatches, fmt.Sprintf("protocol compiled=%q catalog=%q", descriptor.Protocol, declared.Protocol))
	}
	if descriptor.EffectModel != declared.EffectModel {
		mismatches = append(mismatches, fmt.Sprintf("effect_model compiled=%q catalog=%q", descriptor.EffectModel, declared.EffectModel))
	}
	return descriptor, mismatches, nil
}
