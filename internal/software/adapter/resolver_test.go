package adapter_test

import (
	"context"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/catalog"
)

type fakeResolver struct {
	descriptor adapter.Descriptor
}

func (f fakeResolver) Descriptor() adapter.Descriptor { return f.descriptor }

func (f fakeResolver) Candidates(context.Context, adapter.ResolveRequest) ([]software.Candidate, error) {
	return nil, nil
}

func TestResolverFamilyReturnsOnlyCatalogSelectedAdapter(t *testing.T) {
	supply := resolverCatalog(t)
	target := software.Target{OS: "darwin", Arch: "arm64"}
	homebrew := fakeResolver{descriptor: adapter.Descriptor{
		ID: "homebrew", Method: "system-package", Protocol: catalog.AdapterProtocolV1,
		EffectModel: "shared", Targets: []software.Target{target},
	}}
	uv := fakeResolver{descriptor: adapter.Descriptor{
		ID: "uv", Method: "python-environment", Protocol: catalog.AdapterProtocolV1,
		EffectModel: "isolated", Targets: []software.Target{target},
	}}
	family, err := adapter.NewResolverFamily(homebrew, uv)
	if err != nil {
		t.Fatal(err)
	}

	resolver, descriptor, err := family.For(supply, "system-package", target)
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.ID != "homebrew" || resolver.Descriptor().ID != "homebrew" {
		t.Fatalf("For() selected %q/%q, want homebrew", descriptor.ID, resolver.Descriptor().ID)
	}
}

func TestResolverFamilyDoesNotFallbackToAnotherMethod(t *testing.T) {
	supply := resolverCatalog(t)
	target := software.Target{OS: "darwin", Arch: "arm64"}
	uv := fakeResolver{descriptor: adapter.Descriptor{
		ID: "uv", Method: "python-environment", Protocol: catalog.AdapterProtocolV1,
		EffectModel: "isolated", Targets: []software.Target{target},
	}}
	family, err := adapter.NewResolverFamily(uv)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = family.For(supply, "system-package", target)
	if err == nil || !strings.Contains(err.Error(), `catalog-selected adapter "homebrew" is not compiled`) {
		t.Fatalf("For() error = %v, want exact adapter refusal", err)
	}
}

func resolverCatalog(t *testing.T) catalog.Document {
	t.Helper()
	document := catalog.Document{
		Schema: "temper-software-supply/v1", Sequence: 1, PublishedAt: "2026-08-20T00:00:00Z",
		Methods: map[string]catalog.Method{
			"system-package":     {Description: "system"},
			"python-environment": {Description: "python"},
		},
		Adapters: map[string]catalog.Adapter{
			"homebrew": {Method: "system-package", Protocol: catalog.AdapterProtocolV1, EffectModel: "shared"},
			"uv":       {Method: "python-environment", Protocol: catalog.AdapterProtocolV1, EffectModel: "isolated"},
		},
		TargetBindings: []catalog.TargetBinding{
			{Method: "system-package", Target: software.Target{OS: "darwin", Arch: "arm64"}, Adapter: "homebrew"},
			{Method: "python-environment", Target: software.Target{OS: "darwin", Arch: "arm64"}, Adapter: "uv"},
		},
		Packages: map[string]catalog.Package{
			"example": {
				Description: "example",
				Recipes: map[string]catalog.Recipe{
					"homebrew": validRecipe("system-package", "homebrew-formula"),
					"uv":       validRecipe("python-environment", "python-index"),
				},
			},
		},
	}
	if err := document.Validate(); err != nil {
		t.Fatal(err)
	}
	return document
}

func validRecipe(method, kind string) catalog.Recipe {
	source := catalog.Source{Kind: kind, Tap: "temper/tap", Formula: "example"}
	if kind == "python-index" {
		source = catalog.Source{Kind: kind, Index: "https://example.invalid", Distribution: "example"}
	}
	return catalog.Recipe{
		Method: method, RecipeRevision: "example/v1", Source: source, VersionScheme: "semver",
		Selection: catalog.Selection{Policy: "latest", MinimumCompatible: "1.0.0"},
		Tested: []catalog.Tested{{
			RootVersion: "1.0.0", ClosureDigest: strings.Repeat("a", 64),
			Target: software.Target{OS: "darwin", Arch: "arm64"}, Evidence: "test",
		}},
	}
}
