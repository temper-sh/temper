package catalogbootstrap

import (
	"testing"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/adapter/upstreamrelease"
	"github.com/temper-sh/temper/internal/software/catalog"
	"github.com/temper-sh/temper/internal/software/catalogreader"
	"github.com/temper-sh/temper/internal/software/catalogtrust"
)

func TestCatalogIsValidAndSupportedByTheReleaseAdapter(t *testing.T) {
	snapshot, err := catalog.ParseSnapshot(catalogData)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := adapter.NewRegistry(upstreamrelease.Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.ValidateCatalog(snapshot.Document); err != nil {
		t.Fatal(err)
	}
	if snapshot.Document.Sequence != 1 {
		t.Fatalf("sequence = %d, want 1", snapshot.Document.Sequence)
	}

	target := software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "26.6.1"}
	assertRecipe(t, snapshot.Document, target, "llama-swap", "v251", "4ec317589b21f58b64802c2b3371a179b9fdaa53", "b438acbfbe588b4a2e9ffe11f08eb22c5d7955b9b304cab0e668a8686edfccdc", 12871496, 23027581, 3, ".")
	assertRecipe(t, snapshot.Document, target, "llama-cpp", "b10566", "bb4caa7540188872173c44d161602d9271386413", "533f546dab2ce2f8e29ce3070f26acc55acc59528e177f2cd0d52b7f69b44f50", 11095544, 27555366, 61, "llama-b10566")
}

func TestProductionBootstrapVerifiesThroughTheReadBoundary(t *testing.T) {
	trust, err := catalogtrust.Production()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := adapter.NewRegistry(upstreamrelease.Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	result, err := catalogreader.Read(t.TempDir(), trust, Production(), registry)
	if err != nil {
		t.Fatal(err)
	}
	if result.Origin != catalogreader.OriginBootstrap || result.KeyID != catalogtrust.ProductionKeyID || result.Catalog.Document.Sequence != 1 {
		t.Fatalf("bootstrap result = %#v", result)
	}
}

func TestProductionReturnsIndependentBytes(t *testing.T) {
	first := Production()
	first.CatalogData[0] ^= 0xff
	first.SignatureData[0] ^= 0xff
	second := Production()
	if second.CatalogData[0] != catalogData[0] || second.SignatureData[0] != signatureData[0] {
		t.Fatal("Production() exposed mutable embedded bytes")
	}
}

func assertRecipe(t *testing.T, document catalog.Document, target software.Target, packageID, version, revision, digest string, size, unpackedSize int64, installedEntries int, archiveRoot string) {
	t.Helper()
	pkg, ok := document.Packages[packageID]
	if !ok {
		t.Fatalf("package %q is absent", packageID)
	}
	recipe, ok := pkg.Recipes["upstream-release"]
	if !ok {
		t.Fatalf("package %q has no upstream-release recipe", packageID)
	}
	if recipe.Selection.Exact != version || recipe.Source.Revision != revision {
		t.Fatalf("package %q identity = version %q revision %q", packageID, recipe.Selection.Exact, recipe.Source.Revision)
	}
	if len(recipe.Tested) != 0 {
		t.Fatalf("package %q has %d tested rows, want none", packageID, len(recipe.Tested))
	}
	artifact, err := recipe.Source.ReleaseArtifactFor(target)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.SHA256 != digest || artifact.Size != size || artifact.UnpackedSize != unpackedSize || artifact.InstalledEntries != installedEntries || artifact.ArchiveRoot != archiveRoot {
		t.Fatalf("package %q artifact = %#v", packageID, artifact)
	}
}
