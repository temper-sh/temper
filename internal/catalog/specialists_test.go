package catalog_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/patch"
	"github.com/temper-sh/temper/internal/render"
	"github.com/temper-sh/temper/internal/render/engine"
)

func TestTemplateTransformSurvivesExecutionProjection(t *testing.T) {
	d := document()
	p := d.Templates["template"]
	p.Transform = patch.TranslateGemmaTextInputV1
	d.Templates["template"] = p
	lock := compile(t, d)
	projected, err := lock.Projections()
	if err != nil {
		t.Fatal(err)
	}
	source, err := patch.ParseSource(projected.Manifest.Patches["template"].Source)
	if err != nil || source.Transform != p.Transform || projected.Artifacts.Entries[lock.Preset].Patches[0].SHA256 != p.Files[0].SHA256 {
		t.Fatal("lost transform or transformed-file digest", source, err)
	}
	p.Transform = "unknown-transform"
	d.Templates["template"] = p
	if d.Validate() == nil {
		t.Fatal("accepted an unknown source transform")
	}
}

func imageCatalog() catalog.Document {
	d := document()
	d.Artifacts["projector"] = catalog.Artifact{Sources: []catalog.Source{{Hub: catalog.HubHuggingFace, Repo: "example/Vision", Revision: strings.Repeat("f", 40)}}, Format: "gguf", License: "Apache-2.0", Files: []catalog.File{{Path: "image/projector.gguf", Bytes: 31, SHA256: strings.Repeat("a", 64)}}}
	p := d.Presets["qwen-32k"]
	p.ProjectorArtifact, p.Modalities = "projector", []string{"text", "image"}
	p.Speculation = catalog.Speculation{Method: "none", Source: "none"}
	d.Presets["qwen-32k"] = p
	e := d.Engines[p.Engine]
	e.Modalities = p.Modalities
	d.Engines[p.Engine] = e
	return d
}

func TestProjectorIsPinnedRenderedAndInvalidatesOnlyChangedMaterial(t *testing.T) {
	d := imageCatalog()
	before := compile(t, d)
	p, err := before.Projections()
	if err != nil {
		t.Fatal(err)
	}
	entry := p.Artifacts.Entries[before.Preset]
	if len(before.Records.Artifacts) != 2 || entry.Projector == nil || entry.Projector.Files[0].Name != "image/projector.gguf" {
		t.Fatal("projector missing from selected material")
	}
	for _, device := range []catalog.ComputeDevice{catalog.ComputeGPU, catalog.ComputeCPU} {
		lock, err := catalog.ConfigureExecution(before, before.Preset, catalog.ExecutionSettings{ContextWindowTokens: 32768, MaxOutputTokens: 4096, Compute: device})
		if err != nil {
			t.Fatal(err)
		}
		p, err := lock.Projections()
		if err != nil {
			t.Fatal(err)
		}
		commands, err := render.Commands(render.Inputs{Root: "/temper path", Manifest: p.Manifest, Lock: p.Artifacts, Mode: lock.Preset})
		if err != nil {
			t.Fatal(err)
		}
		args := strings.Join(commands[lock.Preset].Arguments(), " ")
		if !strings.Contains(args, "--mmproj /temper path/") || !strings.Contains(args, "/projector/image/projector.gguf") || strings.Contains(args, "--no-mmproj ") {
			t.Fatalf("projector launch lost exact selected path: %s", args)
		}
		if strings.Contains(args, "--no-mmproj-offload") != (device == catalog.ComputeCPU) {
			t.Fatal("projector does not follow selected compute")
		}
	}
	a := d.Artifacts["projector"]
	a.Files = append([]catalog.File(nil), a.Files...)
	a.Files[0].SHA256 = strings.Repeat("b", 64)
	d.Artifacts["projector"] = a
	after := compile(t, d)
	q, err := after.Projections()
	if err != nil {
		t.Fatal(err)
	}
	if catalog.SameExecution(before, after) || entry.Digest() == q.Artifacts.Entries[after.Preset].Digest() || !reflect.DeepEqual(entry.Files, q.Artifacts.Entries[after.Preset].Files) {
		t.Fatal("projector change did not preserve target and invalidate material identity")
	}
	x, _ := p.Software.SemanticDigest()
	y, _ := q.Software.SemanticDigest()
	if x != y {
		t.Fatal("projector change invalidated unchanged software")
	}
}

func TestImageContractRefusesMissingAndIncompatibleProjectors(t *testing.T) {
	for _, change := range []func(*catalog.Document, *catalog.Preset){
		func(d *catalog.Document, p *catalog.Preset) { delete(d.Artifacts, "projector") },
		func(d *catalog.Document, p *catalog.Preset) { p.ProjectorArtifact = "" },
		func(d *catalog.Document, p *catalog.Preset) { p.Modalities = []string{"text"} },
		func(d *catalog.Document, p *catalog.Preset) {
			e := d.Engines[p.Engine]
			e.Modalities = []string{"text"}
			d.Engines[p.Engine] = e
		},
	} {
		d := imageCatalog()
		p := d.Presets["qwen-32k"]
		change(&d, &p)
		d.Presets["qwen-32k"] = p
		if d.Validate() == nil {
			t.Fatal("incomplete image contract accepted")
		}
	}
}

func rerankingCatalog(crisp bool) catalog.Document {
	d := document()
	p := d.Presets["qwen-32k"]
	p.Interface, p.Template, p.RequestDefaults = "reranking", "", catalog.RequestDefaults{}
	p.Speculation = catalog.Speculation{Method: "none", Source: "none"}
	p.Profiles[0].EngineConfig.KVCache = ""
	e := d.Engines[p.Engine]
	e.Interfaces = []string{"reranking"}
	if crisp {
		p.Profiles[0].EngineConfig = catalog.EngineConfig{LlamaConfig: catalog.LlamaConfig{Kind: "crispembed/v1"}, CrispEmbed: &catalog.CrispEmbedConfig{Kind: "crispembed/v1", CrispEmbedTuning: engine.CrispEmbedTuning{Threads: 4}}}
		p.Compute, p.ComputeOptions = catalog.ComputeCPU, []catalog.ComputeDevice{catalog.ComputeCPU}
		p.ContextLimitTokens = p.Window()
		e.Adapter, e.Supply = "crispembed/v1", supply("crispembed")
	}
	d.Presets["qwen-32k"], d.Engines[p.Engine] = p, e
	return d
}

func TestRerankersCompileWithoutGenerationAndRespectNativeControls(t *testing.T) {
	for _, crisp := range []bool{false, true} {
		d := rerankingCatalog(crisp)
		lock := compile(t, d)
		p := d.Presets[lock.Preset]
		configured, err := catalog.ConfigureExecution(lock, lock.Preset, catalog.ExecutionSettings{ContextWindowTokens: p.Window(), Compute: catalog.ComputeCPU})
		if err != nil {
			t.Fatal(err)
		}
		projection, err := configured.Projections()
		if err != nil {
			t.Fatal(err)
		}
		commands, err := render.Commands(render.Inputs{Root: "/temper", Manifest: projection.Manifest, Lock: projection.Artifacts, Mode: lock.Preset})
		if err != nil {
			t.Fatal(err)
		}
		command := commands[lock.Preset]
		args := strings.Join(command.Arguments(), " ")
		for _, unwanted := range []string{"--predict", "--temp", "--jinja", "--chat-template-file"} {
			if strings.Contains(args, unwanted) {
				t.Fatalf("reranker carries generation control %q", unwanted)
			}
		}
		if crisp {
			if command.Runtime().Requirement.Package != "crispembed" || !strings.Contains(args, "--gpu-backend cpu") || !strings.Contains(args, "-t 4") {
				t.Fatal("CrispEmbed did not retain CPU tuning")
			}
			_, err = catalog.ConfigureExecution(lock, lock.Preset, catalog.ExecutionSettings{ContextWindowTokens: 4096})
			if err == nil {
				t.Fatal("fixed encoder context silently overridden")
			}
		} else if !strings.Contains(args, "--reranking") || !strings.Contains(args, "--device none") {
			t.Fatal("llama CPU reranking controls missing")
		}
		// A 0.1.0 lock writes every default, so its reranker carries seed 0.
		p.RequestDefaults.Sampling.Seed = seed(0)
		d.Presets[lock.Preset] = p
		if err := d.Validate(); err != nil {
			t.Fatalf("0.1.0 reranker defaults refused: %v", err)
		}
		p.RequestDefaults.MaxOutputTokens = 1
		d.Presets[lock.Preset] = p
		if d.Validate() == nil {
			t.Fatal("generation defaults accepted for reranking")
		}
	}
}
