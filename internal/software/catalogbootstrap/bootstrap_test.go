package catalogbootstrap

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/adapter/upstreamrelease"
	"github.com/temper-sh/temper/internal/software/catalog"
	publication "github.com/temper-sh/temper/internal/software/catalogpublication"
	"github.com/temper-sh/temper/internal/software/catalogreader"
	"github.com/temper-sh/temper/internal/software/catalogtrust"
)

const productionCatalogDigest = "3b00bb311ed694b5771e146788e5e19dcd6e54aebb522d82621ff4b469487a44"

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

func TestStablePagesPublicationJoinsTheEmbeddedBootstrap(t *testing.T) {
	repositoryRoot := filepath.Join("..", "..", "..")
	publicationRoot := filepath.Join(repositoryRoot, "docs", "catalog")
	snapshotRoot := filepath.Join(publicationRoot, "snapshots", productionCatalogDigest)
	publishedCatalog := readPublicationFile(t, filepath.Join(snapshotRoot, "catalog.yaml"))
	publishedCatalogSignature := readPublicationFile(t, filepath.Join(snapshotRoot, "catalog.signature.yaml"))
	if !bytes.Equal(publishedCatalog, catalogData) || !bytes.Equal(publishedCatalogSignature, signatureData) {
		t.Fatal("published sequence-1 snapshot differs from the embedded bootstrap publication")
	}

	channelRoot := filepath.Join(publicationRoot, "channels", "stable")
	channelData := readPublicationFile(t, filepath.Join(channelRoot, "channel.yaml"))
	channelSignature := readPublicationFile(t, filepath.Join(channelRoot, "channel.signature.yaml"))
	trust, err := catalogtrust.Production()
	if err != nil {
		t.Fatal(err)
	}
	verifiedChannel, err := publication.VerifyChannel("stable", channelData, channelSignature, trust)
	if err != nil {
		t.Fatal(err)
	}
	reference := verifiedChannel.Document.Catalog
	wantLocator := "https://temper-sh.github.io/temper/catalog/snapshots/" + productionCatalogDigest + "/"
	if reference.SHA256 != productionCatalogDigest || reference.Sequence != 1 || reference.Locator != wantLocator {
		t.Fatalf("stable channel reference = %#v", reference)
	}
	verifiedCatalog, err := publication.VerifyCatalog(reference, publishedCatalog, publishedCatalogSignature, trust)
	if err != nil {
		t.Fatal(err)
	}
	if verifiedCatalog.Snapshot.SHA256 != productionCatalogDigest {
		t.Fatalf("verified snapshot digest = %q", verifiedCatalog.Snapshot.SHA256)
	}
}

func readPublicationFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
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
