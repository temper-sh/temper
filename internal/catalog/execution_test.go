package catalog_test

import (
	"os"
	"slices"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/software"
)

func guidedCatalog(t *testing.T) catalog.Document {
	t.Helper()
	raw, err := os.ReadFile("../testfixture/testdata/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func compileGuided(t *testing.T, d catalog.Document, id string) catalog.Lock {
	t.Helper()
	l, err := catalog.CompilePreset(d, id, 0, 0, software.Target{OS: "darwin", Arch: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// renameRecords gives every record the preset selects a new ID and keeps the
// references consistent. IDs name records; they are not what runs.
func renameRecords(d catalog.Document, preset string) (catalog.Document, string) {
	p := d.Presets[preset]
	artifact, engine, template := p.Artifact+"-renamed", p.Engine+"-renamed", p.Template+"-renamed"
	d.Artifacts[artifact] = d.Artifacts[p.Artifact]
	delete(d.Artifacts, p.Artifact)
	d.Engines[engine] = d.Engines[p.Engine]
	t := d.Templates[p.Template]
	t.CompatibleArtifacts = append(slices.Clone(t.CompatibleArtifacts), artifact)
	d.Templates[template] = t
	for id, other := range d.Presets {
		if other.Artifact == p.Artifact {
			other.Artifact = artifact
		}
		if other.Engine == p.Engine {
			other.Engine = engine
		}
		d.Presets[id] = other
	}
	delete(d.Engines, p.Engine)
	renamed := preset + "-renamed"
	p = d.Presets[preset]
	p.Template = template
	d.Presets[renamed] = p
	delete(d.Presets, preset)
	return d, renamed
}

func TestSameExecutionIgnoresPresentationPolicySourcesAndIDs(t *testing.T) {
	const id = "translategemma-4b-q5km-llama"
	base := compileGuided(t, guidedCatalog(t), id)
	for _, change := range []struct {
		name string
		edit func(catalog.Document) (catalog.Document, string)
	}{
		{"presentation", func(d catalog.Document) (catalog.Document, string) {
			p := d.Presets[id]
			p.Presentation.DisplayName, p.Presentation.Description, p.Presentation.MemoryTier, p.Presentation.AssessmentURL = "Renamed", "Reworded.", "XL", "https://example.test/review"
			d.Presets[id] = p
			a := d.Artifacts[p.Artifact]
			a.Presentation.ModelName, a.Presentation.WeightsName = "Other name", "Other weights"
			d.Artifacts[p.Artifact] = a
			e := d.Engines[p.Engine]
			e.Presentation.DisplayName = "Other engine"
			d.Engines[p.Engine] = e
			return d, id
		}},
		{"version policy", func(d catalog.Document) (catalog.Document, string) {
			p := d.Presets[id]
			p.ExcludeEngineVersions = []catalog.VersionExclusion{{Range: "< b1", Reason: "an old build"}}
			d.Presets[id] = p
			e := d.Engines[p.Engine]
			e.ExcludeVersions = []catalog.VersionExclusion{{Range: "= b2"}}
			e.MinimumTemper = "0.1.0"
			d.Engines[p.Engine] = e
			d.Runtime.Router.ExcludeVersions = nil
			return d, id
		}},
		{"sources", func(d catalog.Document) (catalog.Document, string) {
			p := d.Presets[id]
			a := d.Artifacts[p.Artifact]
			a.Sources = append(a.Sources, catalog.Source{Hub: catalog.HubModelScope, Repo: "mirror/model", Revision: "0000000000000000000000000000000000000000"})
			d.Artifacts[p.Artifact] = a
			e := d.Engines[p.Engine]
			source := *e.Supply.Source
			source.Repository = "mirror/llama.cpp"
			release := *e.Supply.Release
			release.Artifact.Locator = source.ReleaseURL(release.Version)
			e.Supply.Source, e.Supply.Release = &source, &release
			d.Engines[p.Engine] = e
			return d, id
		}},
		{"context ceiling", func(d catalog.Document) (catalog.Document, string) {
			p := d.Presets[id]
			p.ContextLimitTokens = 2 * p.ContextLimit()
			d.Presets[id] = p
			return d, id
		}},
		{"record IDs", func(d catalog.Document) (catalog.Document, string) { return renameRecords(d, id) }},
	} {
		t.Run(change.name, func(t *testing.T) {
			d, preset := change.edit(guidedCatalog(t))
			if changed := compileGuided(t, d, preset); !catalog.SameExecution(base, changed) {
				t.Fatal("a change that runs nothing differently changed the comparison")
			}
		})
	}
}

func TestSameExecutionSeesTemplateTransformWindowAndFiles(t *testing.T) {
	const id = "translategemma-4b-q5km-llama"
	base := compileGuided(t, guidedCatalog(t), id)
	for _, change := range []struct {
		name string
		edit func(*catalog.Document)
	}{
		{"template transform", func(d *catalog.Document) {
			template := d.Templates[d.Presets[id].Template]
			template.Transform = ""
			d.Templates[d.Presets[id].Template] = template
		}},
		{"context window", func(d *catalog.Document) {
			p := d.Presets[id]
			p.Profiles[0].ContextWindowTokens = p.RequestDefaults.MaxOutputTokens + 1024
			d.Presets[id] = p
		}},
		{"model bytes", func(d *catalog.Document) {
			a := d.Artifacts[d.Presets[id].Artifact]
			a.Files = slices.Clone(a.Files)
			a.Files[0].SHA256 = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
			d.Artifacts[d.Presets[id].Artifact] = a
		}},
	} {
		t.Run(change.name, func(t *testing.T) {
			d := guidedCatalog(t)
			change.edit(&d)
			if catalog.SameExecution(base, compileGuided(t, d, id)) {
				t.Fatal("a change to what runs kept the comparison")
			}
		})
	}
}
