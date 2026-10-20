package catalog_test

import (
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/render"
	"github.com/temper-sh/temper/internal/software"
)

func rapidSidecarCatalog(t *testing.T) catalog.Document {
	t.Helper()
	d := qwenStudy(t)
	d.Artifacts["qwen-mtp"] = catalog.Artifact{
		Sources: []catalog.Source{{Hub: catalog.HubHuggingFace, Repo: "example/Qwen-MTP", Revision: strings.Repeat("f", 40)}},
		Format:  "safetensors", License: "Apache-2.0",
		Files: []catalog.File{{Path: "mtp/model.safetensors", Bytes: 100, SHA256: strings.Repeat("a", 64)}},
	}
	p := d.Presets["rapid-mlx"]
	p.Speculation = catalog.Speculation{Method: "mtp", Source: "artifact", DraftArtifact: "qwen-mtp", MaxDraftTokens: 3}
	d.Presets["rapid-mlx"] = p
	return d
}

func TestRapidSidecarSurvivesLockRoundTripAndRendersExactLocalFile(t *testing.T) {
	d := rapidSidecarCatalog(t)
	before, err := catalog.CompilePreset(d, "rapid-mlx", 64, 0, software.Target{OS: "darwin", Arch: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := catalog.MarshalLock(before)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := catalog.ParseLock(raw)
	if err != nil {
		t.Fatal(err)
	}
	p, err := locked.Projections()
	if err != nil {
		t.Fatal(err)
	}
	draft := p.Artifacts.Entries["rapid-mlx"].Draft
	if draft == nil || draft.Revision != strings.Repeat("f", 40) || draft.Files[0].SHA256 != strings.Repeat("a", 64) {
		t.Fatal("independent pinned draft was lost")
	}
	bundle, err := render.Build(render.Inputs{StartPort: 10001, Manifest: p.Manifest, Lock: p.Artifacts,
		Mode: locked.Preset, Root: "/temper path"})
	if err != nil {
		t.Fatal(err)
	}
	var rendered string
	for _, file := range bundle.Artifacts {
		rendered += string(file.Data)
	}
	for _, want := range []string{`--speculative-config`, `"model":"/temper path/`, `/draft/mtp/model.safetensors`, `"num_speculative_tokens":3`} {
		if !strings.Contains(rendered, want) {
			t.Errorf("render missing %q", want)
		}
	}
	a := d.Artifacts["qwen-mtp"]
	a.Files[0].SHA256 = strings.Repeat("b", 64)
	d.Artifacts["qwen-mtp"] = a
	after, err := catalog.CompilePreset(d, "rapid-mlx", 64, 0, before.Target)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.SameExecution(before, after) {
		t.Fatal("changing the draft did not invalidate execution identity")
	}
}

func TestRapidSidecarRefusesIncompleteOrIncompatibleMaterial(t *testing.T) {
	for _, name := range []string{"missing", "gguf", "two-shards", "none", "zero-depth", "draft-as-target"} {
		t.Run(name, func(t *testing.T) {
			d := rapidSidecarCatalog(t)
			a, p := d.Artifacts["qwen-mtp"], d.Presets["rapid-mlx"]
			switch name {
			case "missing":
				p.Speculation.DraftArtifact = "absent"
			case "gguf":
				a.Format = "gguf"
			case "two-shards":
				a.Files = append(a.Files, catalog.File{Path: "second.safetensors", Bytes: 100, SHA256: strings.Repeat("c", 64)})
			case "none":
				p.Speculation.Method, p.Speculation.MaxDraftTokens = "none", 0
			case "zero-depth":
				p.Speculation.MaxDraftTokens = 0
			case "draft-as-target":
				p.Artifact = "qwen-mtp"
			}
			d.Artifacts["qwen-mtp"], d.Presets["rapid-mlx"] = a, p
			if _, err := catalog.CompilePreset(d, "rapid-mlx", 64, 0, software.Target{OS: "darwin", Arch: "arm64"}); err == nil {
				t.Fatal("invalid sidecar selection accepted")
			}
		})
	}
}
