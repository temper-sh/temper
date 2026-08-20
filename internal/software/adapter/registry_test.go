package adapter_test

import (
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/catalog"
)

func TestResolveUsesOnlyTheCatalogSelectedAdapter(t *testing.T) {
	supply := validCatalog(t)
	registry, err := adapter.NewRegistry(
		descriptor("homebrew", "system-package", "shared", software.Target{OS: "darwin", Arch: "arm64"}),
		descriptor("uv", "python-environment", "isolated", software.Target{OS: "darwin", Arch: "arm64"}),
	)
	if err != nil {
		t.Fatal(err)
	}

	got, err := registry.Resolve(supply, "system-package", software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos"})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.ID != "homebrew" {
		t.Errorf("Resolve() adapter = %q, want homebrew", got.ID)
	}
	got.Targets[0] = software.Target{OS: "linux", Arch: "arm64"}
	if _, err := registry.Resolve(supply, "system-package", software.Target{OS: "darwin", Arch: "arm64"}); err != nil {
		t.Fatalf("mutating returned descriptor changed registry: %v", err)
	}
}

func TestResolveRefusesAnUnbuiltKeyWithoutFallingBack(t *testing.T) {
	supply := validCatalog(t)
	registry, err := adapter.NewRegistry(
		descriptor("uv", "python-environment", "isolated", software.Target{OS: "darwin", Arch: "arm64"}),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = registry.Resolve(supply, "system-package", software.Target{OS: "darwin", Arch: "arm64"})
	if err == nil || !strings.Contains(err.Error(), `catalog-selected adapter "homebrew" is not compiled`) {
		t.Fatalf("Resolve() error = %v, want declared-but-unbuilt refusal", err)
	}
}

func TestResolveReportsEveryDescriptorMismatch(t *testing.T) {
	supply := validCatalog(t)
	registry, err := adapter.NewRegistry(adapter.Descriptor{
		ID:          "homebrew",
		Method:      "python-environment",
		Protocol:    catalog.AdapterProtocolV1,
		EffectModel: "isolated",
		Targets:     []software.Target{{OS: "linux", Arch: "arm64"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = registry.Resolve(supply, "system-package", software.Target{OS: "darwin", Arch: "arm64"})
	if err == nil {
		t.Fatal("Resolve() succeeded, want descriptor mismatch")
	}
	for _, want := range []string{"method compiled=", "effect_model compiled=", "target support"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Resolve() error does not contain %q: %v", want, err)
		}
	}
}

func TestNewRegistryRejectsDuplicateKeys(t *testing.T) {
	homebrew := descriptor("homebrew", "system-package", "shared", software.Target{OS: "darwin", Arch: "arm64"})
	_, err := adapter.NewRegistry(homebrew, homebrew)
	if err == nil || !strings.Contains(err.Error(), "registered more than once") {
		t.Fatalf("NewRegistry() error = %v, want duplicate refusal", err)
	}
}

func TestValidateCatalogChecksEveryDeclaredAdapterAndBinding(t *testing.T) {
	supply := validCatalog(t)

	t.Run("all capabilities present", func(t *testing.T) {
		registry, err := adapter.NewRegistry(
			descriptor("homebrew", "system-package", "shared", software.Target{OS: "darwin", Arch: "arm64"}),
			descriptor("uv", "python-environment", "isolated", software.Target{OS: "darwin", Arch: "arm64"}),
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := registry.ValidateCatalog(supply); err != nil {
			t.Fatalf("ValidateCatalog() error = %v", err)
		}
	})

	t.Run("declared adapter is not built", func(t *testing.T) {
		registry, err := adapter.NewRegistry(
			descriptor("homebrew", "system-package", "shared", software.Target{OS: "darwin", Arch: "arm64"}),
		)
		if err != nil {
			t.Fatal(err)
		}
		err = registry.ValidateCatalog(supply)
		if err == nil || !strings.Contains(err.Error(), `adapter "uv" is not compiled`) {
			t.Fatalf("ValidateCatalog() error = %v, want complete capability refusal", err)
		}
	})

	t.Run("binding target is unsupported", func(t *testing.T) {
		registry, err := adapter.NewRegistry(
			descriptor("homebrew", "system-package", "shared", software.Target{OS: "linux", Arch: "arm64"}),
			descriptor("uv", "python-environment", "isolated", software.Target{OS: "darwin", Arch: "arm64"}),
		)
		if err != nil {
			t.Fatal(err)
		}
		err = registry.ValidateCatalog(supply)
		if err == nil || !strings.Contains(err.Error(), "does not support declared target") {
			t.Fatalf("ValidateCatalog() error = %v, want target capability refusal", err)
		}
	})
}

func descriptor(id, method, effectModel string, target software.Target) adapter.Descriptor {
	return adapter.Descriptor{
		ID:          id,
		Method:      method,
		Protocol:    catalog.AdapterProtocolV1,
		EffectModel: effectModel,
		Targets:     []software.Target{target},
	}
}

func validCatalog(t *testing.T) catalog.Document {
	t.Helper()
	document := catalog.Document{
		Schema:      catalog.SchemaV1,
		Sequence:    1,
		PublishedAt: "2026-08-20T18:30:00Z",
		Methods: map[string]catalog.Method{
			"system-package":     {Description: "shared target package manager"},
			"python-environment": {Description: "Temper-owned Python environment"},
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
			"llama-swap": {
				Description: "router",
				Recipes: map[string]catalog.Recipe{
					"homebrew": {
						Method: "system-package", RecipeRevision: "llama-swap/v1",
						Source:        catalog.Source{Kind: "homebrew-formula", Tap: "temper-sh/tap", Formula: "llama-swap"},
						VersionScheme: "semver", Selection: catalog.Selection{Policy: "latest", MinimumCompatible: "1.0.0"},
						Tested: []catalog.Tested{{RootVersion: "1.0.0", ClosureDigest: strings.Repeat("a", 64), Target: software.Target{OS: "darwin", Arch: "arm64"}, Evidence: "results/router"}},
					},
				},
			},
		},
	}
	if err := document.Validate(); err != nil {
		t.Fatalf("fixture catalog invalid: %v", err)
	}
	return document
}
