package catalog_test

import (
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/render"
	"gopkg.in/yaml.v3"
)

func externalDraftCatalog(method string) catalog.Document {
	d := document()
	d.Artifacts["assistant"] = catalog.Artifact{Repo: "example/Assistant", Revision: strings.Repeat("f", 40), Format: "gguf", License: "Apache-2.0", Files: []catalog.File{{Path: "draft/model.gguf", Bytes: 11, SHA256: strings.Repeat("a", 64)}}}
	l := d.Layouts["qwen-32k"]
	l.Speculation = catalog.Speculation{Method: method, Source: "artifact", DraftArtifact: "assistant", MaxDraftTokens: 3}
	d.Layouts["qwen-32k"] = l
	return d
}

func TestLlamaExternalDraftSurvivesCompilationAndRendersTheLockedFile(t *testing.T) {
	for _, method := range []string{"mtp", "dflash", "dflash2"} {
		t.Run(method, func(t *testing.T) {
			locked := compile(t, externalDraftCatalog(method))
			if len(locked.Records.Artifacts) != 2 {
				t.Fatal("selected draft missing from execution closure")
			}
			p, err := locked.Projections()
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := yaml.Marshal(p.Manifest)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := manifest.Parse(encoded); err != nil {
				t.Fatal(err)
			}
			draft := p.Artifacts.Entries["qwen-32k"].Draft
			if draft == nil || draft.Repo != "example/Assistant" || draft.Files[0].Name != "draft/model.gguf" {
				t.Fatal("draft identity lost in artifact projection")
			}
			bundle, err := render.Build(render.Inputs{Manifest: p.Manifest, Lock: p.Artifacts, Mode: locked.Selection.Profile, Root: "/temper path"})
			if err != nil {
				t.Fatal(err)
			}
			var config string
			for _, file := range bundle.Artifacts {
				if file.Path == "llama-swap/config.yaml" {
					config = string(file.Data)
				}
			}
			native := "draft-dflash"
			if method == "mtp" {
				native = "draft-mtp"
			}
			for _, want := range []string{"--spec-type " + native, "--model-draft '/temper path/", "/draft/draft/model.gguf'", "--gpu-layers-draft 99", "--cache-type-k-draft f16", "--cache-type-v-draft f16"} {
				if !strings.Contains(config, want) {
					t.Errorf("render missing %q", want)
				}
			}
		})
	}
}

func TestExternalDraftUpdateChangesExecutionWithoutChangingTargetOrSoftware(t *testing.T) {
	d := externalDraftCatalog("dflash2")
	before := compile(t, d)
	a := d.Artifacts["assistant"]
	a.Files = []catalog.File{{Path: "draft/model.gguf", Bytes: 11, SHA256: strings.Repeat("b", 64)}}
	d.Artifacts["assistant"] = a
	after := compile(t, d)
	p1, err := before.Projections()
	if err != nil {
		t.Fatal(err)
	}
	p2, err := after.Projections()
	if err != nil {
		t.Fatal(err)
	}
	e1, e2 := p1.Artifacts.Entries["qwen-32k"], p2.Artifacts.Entries["qwen-32k"]
	if before.Digests.Profile == after.Digests.Profile || e1.Digest() == e2.Digest() || e1.Files[0] != e2.Files[0] {
		t.Fatal("draft identity was lost or target changed")
	}
	s1, _ := p1.Software.SemanticDigest()
	s2, _ := p2.Software.SemanticDigest()
	if s1 != s2 {
		t.Fatal("draft invalidated unchanged software")
	}
}

func TestExternalDraftRefusesMissingOrIncompatibleMaterial(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*catalog.Document, *catalog.Layout)
	}{
		{"missing draft", func(d *catalog.Document, l *catalog.Layout) { delete(d.Artifacts, "assistant") }},
		{"missing selection", func(d *catalog.Document, l *catalog.Layout) { l.Speculation.DraftArtifact = "" }},
		{"embedded dflash", func(d *catalog.Document, l *catalog.Layout) { l.Speculation.Source = "embedded" }},
		{"none with draft", func(d *catalog.Document, l *catalog.Layout) { l.Speculation.Method = "none" }},
		{"no block", func(d *catalog.Document, l *catalog.Layout) { l.Speculation.MaxDraftTokens = 0 }},
		{"oversized block", func(d *catalog.Document, l *catalog.Layout) { l.Speculation.MaxDraftTokens = 16 }},
		{"wrong format", func(d *catalog.Document, l *catalog.Layout) {
			d.Artifacts["assistant"] = catalog.Artifact{Repo: "example/Assistant", Revision: strings.Repeat("f", 40), Format: "safetensors", License: "Apache-2.0", Files: []catalog.File{{Path: "config.json", Bytes: 1, SHA256: strings.Repeat("a", 64)}, {Path: "model.safetensors", Bytes: 11, SHA256: strings.Repeat("b", 64)}}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := externalDraftCatalog("dflash2")
			l := d.Layouts["qwen-32k"]
			tc.change(&d, &l)
			d.Layouts["qwen-32k"] = l
			if err := d.Validate(); err == nil {
				t.Fatal("invalid draft composition accepted")
			}
		})
	}
}
