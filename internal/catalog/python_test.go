package catalog_test

import (
	"os"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/buildinfo"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/render"
	"github.com/temper-sh/temper/internal/software"
)

func qwenStudy(t *testing.T) catalog.Document {
	t.Helper()
	raw, err := os.ReadFile("../testfixture/testdata/study-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	// The study catalog is an authoring input, not a publication: it is read
	// as clients read catalogs, without the publisher's editorial rules.
	view, err := catalog.Read(raw, buildinfo.DevVersion)
	if err != nil || len(view.Unavailable) != 0 {
		t.Fatal(err, view.Unavailable)
	}
	return view.Document
}

func TestStudyCompositionsCompileRenderAndKeepTheExactPythonClosure(t *testing.T) {
	document := qwenStudy(t)
	for id := range document.Presets {
		t.Run(id, func(t *testing.T) {
			lock, err := catalog.CompilePreset(document, id, 0, 0, software.Target{OS: "darwin", Arch: "arm64"})
			if err != nil {
				t.Fatal(err)
			}
			bytes, err := catalog.MarshalLock(lock)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := catalog.ParseLock(bytes)
			if err != nil {
				t.Fatal(err)
			}
			projection, err := parsed.Projections()
			if err != nil {
				t.Fatal(err)
			}
			if err := projection.Software.Validate(); err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			bundle, err := render.Build(render.Inputs{Manifest: projection.Manifest, Lock: projection.Artifacts, Mode: id, Root: root})
			if err != nil {
				t.Fatal(err)
			}
			rendered := ""
			for _, artifact := range bundle.Artifacts {
				rendered += string(artifact.Data)
			}
			if id == "rapid-mlx" || id == "vllm-metal" {
				for _, setting := range []string{"PYTHONDONTWRITEBYTECODE=1", "PYTHONNOUSERSITE=1", "HOME=" + root + "/runtime-state/" + id + "/home"} {
					if !strings.Contains(rendered, setting) {
						t.Fatalf("missing isolated Python setting %s", setting)
					}
				}
				if len(projection.Manifest.Layouts[id].Model.Files) < 4 {
					t.Fatal("MLX snapshot was truncated")
				}
				if !strings.Contains(rendered, "--tool-call-parser") {
					t.Fatal("tool parser was lost")
				}
			}
			if id == "rapid-mlx" && (!strings.Contains(rendered, "--prefill-step-size") || !strings.Contains(rendered, "--timeout")) {
				t.Fatal("Rapid request controls were lost")
			}
			if id == "vllm-metal" {
				for _, flag := range []string{"--reasoning-parser", "--language-model-only", "--enable-chunked-prefill", "--block-size"} {
					if !strings.Contains(rendered, flag) {
						t.Fatalf("missing %s", flag)
					}
				}
				source := projection.Software.Units["python:vllm-metal:mlx-lm"]
				if source.Revision != "9e6acca691e64d6d8bb808c328fcdea459099cca" || source.Artifacts[0].Format != "tar.gz" {
					t.Fatal("exact upstream MLX-LM dependency was replaced")
				}
			}
		})
	}
}

func TestPythonCatalogRefusesMixedOrIncompleteSupplies(t *testing.T) {
	for _, change := range []func(*catalog.Document){
		func(d *catalog.Document) {
			e := d.Engines["rapid-mlx"]
			e.Supply.Release = d.Runtime.Router.Release
			d.Engines["rapid-mlx"] = e
		},
		func(d *catalog.Document) { d.Engines["rapid-mlx"].Supply.Python.Root = "absent" },
		func(d *catalog.Document) {
			p := d.Engines["rapid-mlx"].Supply.Python
			p.Packages = append(p.Packages, p.Packages[0])
		},
		func(d *catalog.Document) {
			p := d.Engines["vllm-metal"].Supply.Python
			for i := range p.Packages {
				if p.Packages[i].Name == "mlx-lm" {
					p.Packages[i].Revision = strings.Repeat("a", 40)
				}
			}
		},
	} {
		document := qwenStudy(t)
		change(&document)
		if _, err := catalog.CompilePreset(document, "rapid-mlx", 0, 0, software.Target{OS: "darwin", Arch: "arm64"}); err == nil {
			t.Fatal("invalid Python closure was admitted")
		}
	}
}
