// Package adapter owns the compiled installer-adapter family boundary.
// An exact software lock names an adapter; this registry verifies that the
// binary contains the matching implementation for the target.
package adapter

import (
	"fmt"
	"regexp"

	"github.com/temper-sh/temper/internal/software"
)

const Protocol = "temper-installer-adapter/v1"

var idPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)

// Descriptor is the pure, compiled identity of one adapter implementation.
// Inspector, installer and remover roles attach to this key. Catalog data
// cannot provide executable behavior.
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
	if d.Protocol != Protocol {
		return fmt.Errorf("adapter %q protocol is %q, want %q", d.ID, d.Protocol, Protocol)
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

// Require returns one compiled adapter by exact key for an already-resolved
// software lock. Installation never consults catalog policy or falls back to
// another adapter.
func (r Registry) Require(adapterID string, target software.Target) (Descriptor, error) {
	if err := target.Validate(); err != nil {
		return Descriptor{}, fmt.Errorf("software target: %w", err)
	}
	descriptor, ok := r.descriptors[adapterID]
	if !ok {
		return Descriptor{}, fmt.Errorf("software lock adapter %q is not compiled into this binary", adapterID)
	}
	if !descriptor.Supports(target) {
		return Descriptor{}, fmt.Errorf("software lock adapter %q does not support target %s/%s", adapterID, target.OS, target.Arch)
	}
	descriptor.Targets = append([]software.Target(nil), descriptor.Targets...)
	return descriptor, nil
}
