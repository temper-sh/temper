package adapter

import (
	"context"
	"fmt"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/catalog"
)

// ResolveRequest contains policy context needed by a provider read. Adapters
// return provider-neutral exact candidates and never choose catalog policy.
type ResolveRequest struct {
	Package string
	Recipe  catalog.Recipe
	Supply  catalog.Document
	Target  software.Target
}

// CandidateResolver is the read-only role of an installer adapter.
type CandidateResolver interface {
	Descriptor() Descriptor
	Candidates(context.Context, ResolveRequest) ([]software.Candidate, error)
}

// ResolverFamily is an immutable keyed family. Catalog selection chooses the
// exact key; absence or descriptor drift is an error and never a fallback.
type ResolverFamily struct {
	registry  Registry
	resolvers map[string]CandidateResolver
}

func NewResolverFamily(resolvers ...CandidateResolver) (ResolverFamily, error) {
	descriptors := make([]Descriptor, 0, len(resolvers))
	byID := make(map[string]CandidateResolver, len(resolvers))
	for index, resolver := range resolvers {
		if resolver == nil {
			return ResolverFamily{}, fmt.Errorf("resolver[%d] is nil", index)
		}
		descriptor := resolver.Descriptor()
		if _, exists := byID[descriptor.ID]; exists {
			return ResolverFamily{}, fmt.Errorf("adapter resolver %q is registered more than once", descriptor.ID)
		}
		descriptors = append(descriptors, descriptor)
		byID[descriptor.ID] = resolver
	}
	registry, err := NewRegistry(descriptors...)
	if err != nil {
		return ResolverFamily{}, err
	}
	return ResolverFamily{registry: registry, resolvers: byID}, nil
}

func (f ResolverFamily) For(supply catalog.Document, method string, target software.Target) (CandidateResolver, Descriptor, error) {
	descriptor, err := f.registry.Resolve(supply, method, target)
	if err != nil {
		return nil, Descriptor{}, err
	}
	resolver, ok := f.resolvers[descriptor.ID]
	if !ok {
		return nil, Descriptor{}, fmt.Errorf("catalog-selected adapter %q has no candidate resolver", descriptor.ID)
	}
	return resolver, descriptor, nil
}
