package testfixture

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
)

// CatalogPath is a frozen copy of the guided catalog in the v1 shape. Tests
// use it so a later catalog publication never changes a test's input.
func CatalogPath() string { return filepath.Join(here(), "testdata", "catalog.json") }

func KapitsaCatalog(t testing.TB) catalog.Document {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(here(), "testdata", "kapitsa-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func here() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}

// MixedEngineCatalog retains the real preset shapes and explicitly supplies
// two synthetic engine releases, independent of catalog publication revisions.
func MixedEngineCatalog(t testing.TB) catalog.Document {
	t.Helper()
	raw, err := os.ReadFile(CatalogPath())
	if err != nil {
		t.Fatal(err)
	}
	d, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	e := d.Engines["llama-cpp"]
	first := *e.Supply.Release
	first.Version = "b1"
	first.Artifact.Locator = e.Supply.Source.ReleaseURL(first.Version)
	first.Artifact.SHA256 = strings.Repeat("a", 64)
	e.Supply.Release = &first
	d.Engines["llama-cpp"] = e
	second := first
	second.Version = "b2"
	second.Artifact.Locator = e.Supply.Source.ReleaseURL(second.Version)
	second.Artifact.SHA256 = strings.Repeat("b", 64)
	e.Supply.Release = &second
	d.Engines["second-llama-cpp"] = e
	for _, id := range []string{"gemma-4-31b-qat-ud-q4-k-xl-llama", "muse-glimmer-30b-q4xl-llama"} {
		p := d.Presets[id]
		p.Engine = "second-llama-cpp"
		d.Presets[id] = p
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	return d
}
