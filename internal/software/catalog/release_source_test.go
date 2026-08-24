package catalog_test

import (
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/catalog"
)

func TestReleaseArchiveSourceFreezesTargetArtifactAndNativeIdentity(t *testing.T) {
	document := releaseCatalog(t)
	if err := document.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	source := document.Packages["llama-cpp"].Recipes["upstream-release"].Source
	if source.NativeName() != "llama-cpp" {
		t.Fatalf("NativeName() = %q, want llama-cpp", source.NativeName())
	}
	artifact, err := source.ReleaseArtifactFor(software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "26.0"})
	if err != nil || artifact.Size != 123 || artifact.UnpackedSize != 456 || artifact.InstalledEntries != 20 || artifact.ArchiveRoot != "llama-b10566" {
		t.Fatalf("ReleaseArtifactFor() = %#v, %v", artifact, err)
	}
}

func TestReleaseArchiveSourceRejectsMovingIncompleteOrAmbiguousInputs(t *testing.T) {
	tests := []struct {
		name string
		edit func(*catalog.Recipe)
		want string
	}{
		{name: "moving selection", edit: func(recipe *catalog.Recipe) { recipe.Selection = catalog.Selection{Policy: "latest"} }, want: "selection must be exact"},
		{name: "missing unpacked size", edit: func(recipe *catalog.Recipe) { recipe.Source.Artifacts[0].UnpackedSize = 0 }, want: "unpacked_size must be greater than zero"},
		{name: "missing installed entry count", edit: func(recipe *catalog.Recipe) { recipe.Source.Artifacts[0].InstalledEntries = 0 }, want: "installed_entries must be greater than zero"},
		{name: "unsafe root", edit: func(recipe *catalog.Recipe) { recipe.Source.Artifacts[0].ArchiveRoot = "../payload" }, want: "archive_root"},
		{name: "non-https locator", edit: func(recipe *catalog.Recipe) { recipe.Source.Artifacts[0].Locator = "file:///tmp/archive" }, want: "absolute https URL"},
		{name: "overlapping assets", edit: func(recipe *catalog.Recipe) {
			recipe.Source.Artifacts = append(recipe.Source.Artifacts, recipe.Source.Artifacts[0])
		}, want: "target overlaps"},
		{name: "mixed provider fields", edit: func(recipe *catalog.Recipe) { recipe.Source.Formula = "llama.cpp" }, want: "cannot declare package-manager fields"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := releaseCatalog(t)
			pkg := document.Packages["llama-cpp"]
			recipe := pkg.Recipes["upstream-release"]
			recipe.Source.Artifacts = append([]catalog.ReleaseArtifact(nil), recipe.Source.Artifacts...)
			test.edit(&recipe)
			pkg.Recipes["upstream-release"] = recipe
			document.Packages["llama-cpp"] = pkg
			err := document.Validate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want %q", err, test.want)
			}
		})
	}
}

func releaseCatalog(t *testing.T) catalog.Document {
	t.Helper()
	document, err := catalog.Parse(validCatalog())
	if err != nil {
		t.Fatal(err)
	}
	document.Methods["release-artifact"] = catalog.Method{Description: "Verified isolated release archive"}
	document.Adapters["upstream-release"] = catalog.Adapter{Method: "release-artifact", Protocol: catalog.AdapterProtocolV1, EffectModel: "isolated"}
	document.TargetBindings = append(document.TargetBindings, catalog.TargetBinding{Method: "release-artifact", Target: software.Target{OS: "darwin", Arch: "arm64"}, Adapter: "upstream-release"})
	document.Packages["llama-cpp"] = catalog.Package{
		Description: "Field Kit inference server",
		Recipes: map[string]catalog.Recipe{"upstream-release": {
			Method: "release-artifact", RecipeRevision: "llama-cpp-upstream-release/v1",
			Source: catalog.Source{
				Kind: "release-archive", Name: "llama-cpp", Repository: "ggml-org/llama.cpp", Revision: "bb4caa7540188872173c44d161602d9271386413",
				Artifacts: []catalog.ReleaseArtifact{{
					Target: software.Target{OS: "darwin", Arch: "arm64"}, Locator: "https://example.invalid/llama-b10566-bin-macos-arm64.tar.gz",
					SHA256: strings.Repeat("a", 64), Size: 123, UnpackedSize: 456, InstalledEntries: 20, Format: "tar.gz", ArchiveRoot: "llama-b10566",
				}},
			},
			VersionScheme: "opaque", Selection: catalog.Selection{Policy: "exact", Exact: "b10566"},
			Dependencies: []catalog.Dependency{}, Exclude: []string{}, Gates: []string{"runtime-smoke.v1"},
			Tested: []catalog.Tested{{RootVersion: "b10566", ClosureDigest: strings.Repeat("b", 64), Target: software.Target{OS: "darwin", Arch: "arm64"}, Evidence: "results/software/llama-cpp-b10566"}},
		}},
	}
	return document
}
