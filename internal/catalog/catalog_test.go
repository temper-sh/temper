package catalog_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/render"
	"github.com/temper-sh/temper/internal/render/engine"
	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter/upstreamrelease"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
)

func supply(pkg string) catalog.Supply {
	return catalog.Supply{Package: pkg, Target: software.Target{OS: "darwin", Arch: "arm64"},
		Source:  &upstreamrelease.GitHubSource{Repository: "example/" + pkg, Asset: "tool-{version}.tar.gz", ArchiveRoot: "bundle"},
		Release: &upstreamrelease.Release{Version: "b10936", Revision: strings.Repeat("b", 40), Artifact: software.Artifact{Locator: "https://example.test/" + pkg + ".tar.gz", SHA256: strings.Repeat("c", 64), Size: 101, UnpackedSize: 200, InstalledEntries: 2, Format: "tar.gz", ArchiveRoot: "bundle"}}}
}

func document() catalog.Document {
	return catalog.Document{Schema: catalog.Schema, Date: "2026-09-13", Runtime: catalog.Runtime{Router: supply("llama-swap")},
		Artifacts: map[string]catalog.Artifact{"qwen-q4": {Repo: "example/Qwen", Revision: strings.Repeat("a", 40), Format: "gguf", License: "Apache-2.0", Files: []catalog.File{{Path: "model.gguf", Bytes: 17, SHA256: strings.Repeat("d", 64)}}}},
		Patches:   map[string]catalog.Patch{"template": {Repo: "example/template", Revision: strings.Repeat("a", 40), License: "Apache-2.0", Files: []catalog.File{{Path: "chat.jinja", Bytes: 12, SHA256: strings.Repeat("e", 64)}}, CompatibleArtifacts: []string{"qwen-q4"}}},
		Engines:   map[string]catalog.Engine{"llama-b10936": {Family: "llama-server", Adapter: "llama-server/v2", Supply: supply("llama-cpp"), Interfaces: []string{"chat-completions"}, Modalities: []string{"text"}}},
		Layouts: map[string]catalog.Layout{"qwen-32k": {DisplayName: "Qwen source work", Artifact: "qwen-q4", Patches: []string{"template"}, Engine: "llama-b10936", Interface: "chat-completions", Modalities: []string{"text"}, ContextWindowTokens: 32768,
			RequestDefaults: catalog.RequestDefaults{MaxOutputTokens: 4096, Reasoning: "off", Sampling: engine.SamplingDefaults{Temperature: 0, TopK: 1, TopP: 1, MinP: 0, RepeatPenalty: 1, PresencePenalty: 0, Seed: 17}},
			Speculation:     catalog.Speculation{Method: "mtp", Source: "embedded", MaxDraftTokens: 3},
			EngineConfig: catalog.LlamaConfig{Kind: "llama-server/v2", Parallel: 1, KVCache: "q8", FlashAttention: "on", BatchTokens: 512, MicrobatchTokens: 512, ContextCheckpoints: 16, PromptCacheRAMMiB: 0, GPULayers: 99,
				Controls: engine.LlamaServerControls{CacheReuse: 16, ReasoningEffort: "medium", PreserveReasoning: true, ContextShift: false, CachePrompt: true, Fit: "off", Threads: 4, ThreadsBatch: 4, LoadMode: "mmap"}}}},
		Profiles: map[string]catalog.Profile{"local-qwen": {GPUMemoryUtilization: .85, Bindings: []catalog.Binding{{Layout: "qwen-32k", Route: "default", Residency: "resident", IdleTTLSeconds: 1800}}}}}
}

func compile(t *testing.T, d catalog.Document) catalog.Lock {
	t.Helper()
	l, err := catalog.Compile(d, catalog.Selection{Schema: catalog.SelectionSchema, Profile: "local-qwen"}, software.Target{OS: "darwin", Arch: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestCompiledLockRunsWithoutSourceCatalog(t *testing.T) {
	d := document()
	before, _ := json.Marshal(d)
	l := compile(t, d)
	after, _ := json.Marshal(d)
	if !bytes.Equal(before, after) {
		t.Fatal("compiler mutated authored catalog")
	}
	raw, err := catalog.MarshalLock(l)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := catalog.ParseLock(raw)
	if err != nil {
		t.Fatal(err)
	}
	p, err := restored.Projections()
	if err != nil {
		t.Fatal(err)
	}
	files, err := p.Files()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manifest.Parse(files["manifest.yaml"]); err != nil {
		t.Fatal(err)
	}
	software, err := softwarelock.Parse(files["software.lock.yaml"])
	if err != nil {
		t.Fatal(err)
	}
	if len(software.Selections) != 2 || software.TargetMode != "compatible" {
		t.Fatalf("incomplete portable software projection: %+v", software)
	}
	bundle, err := render.Build(render.Inputs{Manifest: p.Manifest, Lock: p.Artifacts, Mode: l.Selection.Profile, Root: "/isolated root"})
	if err != nil {
		t.Fatal(err)
	}
	var output string
	for _, a := range bundle.Artifacts {
		if a.Path == "llama-swap/config.yaml" {
			output = string(a.Data)
		}
	}
	for _, want := range []string{"--ctx-checkpoints 16", "--cache-ram 0", "--reasoning off", "--load-mode mmap", "--no-context-shift", "--reasoning-preserve", "--spec-type draft-mtp", "--spec-draft-n-max 3", "--temp 0", "--seed 17", "--predict 4096"} {
		if !strings.Contains(output, want) {
			t.Errorf("render missing %q", want)
		}
	}
	if strings.Contains(string(raw), "/isolated root") {
		t.Fatal("local root entered portable lock")
	}
	if p.RequestDefaults["qwen-32k"].MaxOutputTokens != 4096 {
		t.Fatal("output reserve lost")
	}
}

func TestDerivedManifestRejectsInvalidControlsBeforeRuntimeEffects(t *testing.T) {
	p, err := compile(t, document()).Projections()
	if err != nil {
		t.Fatal(err)
	}
	layout := p.Manifest.Layouts["qwen-32k"]
	layout.Sampling.TopP = 2
	if err := p.Manifest.Validate(); err == nil || !strings.Contains(err.Error(), "sampling") {
		t.Fatalf("manifest accepted invalid API sampling: %v", err)
	}
	layout.Sampling.TopP = 1
	layout.Llama.Controls.LoadMode = "unknown"
	if err := p.Manifest.Validate(); err == nil || !strings.Contains(err.Error(), "load mode") {
		t.Fatalf("manifest accepted invalid loading controls: %v", err)
	}
}

func TestQ4KVCacheSurvivesLockAndManifestRoundTrip(t *testing.T) {
	d := document()
	layout := d.Layouts["qwen-32k"]
	layout.EngineConfig.KVCache = "q4"
	d.Layouts["qwen-32k"] = layout
	locked := compile(t, d)
	raw, err := catalog.MarshalLock(locked)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := catalog.ParseLock(raw)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := restored.Projections()
	if err != nil {
		t.Fatal(err)
	}
	files, err := projection.Files()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := manifest.Parse(files["manifest.yaml"])
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := render.Build(render.Inputs{Manifest: parsed, Lock: projection.Artifacts, Mode: locked.Selection.Profile, Root: "/isolated root"})
	if err != nil {
		t.Fatal(err)
	}
	for _, artifact := range bundle.Artifacts {
		if artifact.Path == "llama-swap/config.yaml" {
			if !strings.Contains(string(artifact.Data), "-ctk q4_0 -ctv q4_0") {
				t.Fatalf("Q4 KV selection lost in rendered command: %s", artifact.Data)
			}
			return
		}
	}
	t.Fatal("no rendered router configuration")
}

func TestCheckpointSpacingSurvivesLockExportAndRendering(t *testing.T) {
	zero, spaced := 0, 1024
	for _, test := range []struct {
		name    string
		spacing *int
	}{
		{"engine default", nil},
		{"no minimum", &zero},
		{"explicit spacing", &spaced},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := document()
			layout := d.Layouts["qwen-32k"]
			layout.EngineConfig.Controls.CheckpointMinStep = test.spacing
			d.Layouts["qwen-32k"] = layout
			raw, err := catalog.MarshalLock(compile(t, d))
			if err != nil {
				t.Fatal(err)
			}
			locked, err := catalog.ParseLock(raw)
			if err != nil {
				t.Fatal(err)
			}
			projection, err := locked.Projections()
			if err != nil {
				t.Fatal(err)
			}
			files, err := projection.Files()
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := manifest.Parse(files["manifest.yaml"])
			if err != nil {
				t.Fatal(err)
			}
			got := parsed.Layouts["qwen-32k"].Llama.Controls.CheckpointMinStep
			if !reflect.DeepEqual(got, test.spacing) {
				t.Fatalf("checkpoint spacing lost or changed during export: %v", got)
			}
			bundle, err := render.Build(render.Inputs{Manifest: parsed, Lock: projection.Artifacts, Mode: locked.Selection.Profile, Root: "/isolated root"})
			if err != nil {
				t.Fatal(err)
			}
			var command string
			for _, artifact := range bundle.Artifacts {
				if artifact.Path == "llama-swap/config.yaml" {
					command = string(artifact.Data)
				}
			}
			if test.spacing == nil {
				if strings.Contains(command, "--checkpoint-min-step") {
					t.Fatal("omitted spacing overrode the engine default")
				}
			} else if !strings.Contains(command, "--checkpoint-min-step "+strconv.Itoa(*test.spacing)) {
				t.Fatalf("explicit checkpoint spacing missing from command: %s", command)
			}
		})
	}
}

func TestNegativeCheckpointSpacingIsRejectedBeforeEffects(t *testing.T) {
	negative := -1
	d := document()
	layout := d.Layouts["qwen-32k"]
	layout.EngineConfig.Controls.CheckpointMinStep = &negative
	d.Layouts["qwen-32k"] = layout
	_, err := catalog.Compile(d, catalog.Selection{Schema: catalog.SelectionSchema, Profile: "local-qwen"}, software.Target{OS: "darwin", Arch: "arm64"})
	if err == nil || !strings.Contains(err.Error(), "checkpoint spacing") {
		t.Fatalf("catalog accepted negative checkpoint spacing: %v", err)
	}
	projection, err := compile(t, document()).Projections()
	if err != nil {
		t.Fatal(err)
	}
	projection.Manifest.Layouts["qwen-32k"].Llama.Controls.CheckpointMinStep = &negative
	if err := projection.Manifest.Validate(); err == nil || !strings.Contains(err.Error(), "checkpoint spacing") {
		t.Fatalf("manifest accepted negative checkpoint spacing: %v", err)
	}
}

func TestDigestChangesFollowOwnedFacts(t *testing.T) {
	base := compile(t, document())
	tests := []struct {
		name                      string
		change                    func(*catalog.Document)
		material, layout, profile bool
	}{
		{"license metadata", func(d *catalog.Document) {
			a := d.Artifacts["qwen-q4"]
			a.License = "LicenseRef-reviewed"
			d.Artifacts["qwen-q4"] = a
		}, false, false, false},
		{"model bytes", func(d *catalog.Document) {
			a := d.Artifacts["qwen-q4"]
			a.Files[0].SHA256 = strings.Repeat("f", 64)
			d.Artifacts["qwen-q4"] = a
		}, true, true, true},
		{"template bytes", func(d *catalog.Document) {
			p := d.Patches["template"]
			p.Files[0].SHA256 = strings.Repeat("f", 64)
			d.Patches["template"] = p
		}, true, true, true},
		{"sampling", func(d *catalog.Document) {
			l := d.Layouts["qwen-32k"]
			l.RequestDefaults.Sampling.Temperature = .7
			d.Layouts["qwen-32k"] = l
		}, false, true, true},
		{"checkpoint spacing", func(d *catalog.Document) {
			spacing := 1024
			l := d.Layouts["qwen-32k"]
			l.EngineConfig.Controls.CheckpointMinStep = &spacing
			d.Layouts["qwen-32k"] = l
		}, false, true, true},
		{"profile TTL", func(d *catalog.Document) {
			p := d.Profiles["local-qwen"]
			p.Bindings[0].IdleTTLSeconds = 60
			d.Profiles["local-qwen"] = p
		}, false, false, true},
		{"display name", func(d *catalog.Document) {
			l := d.Layouts["qwen-32k"]
			l.DisplayName = "Updated description"
			d.Layouts["qwen-32k"] = l
		}, false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := document()
			tt.change(&d)
			got := compile(t, d)
			if got.SourceSnapshotSHA256 == base.SourceSnapshotSHA256 {
				t.Error("record identity did not change")
			}
			if changed := got.Digests.Profile != base.Digests.Profile; changed != tt.profile {
				t.Errorf("profile changed=%v", changed)
			}
		})
	}
}

func TestRejectsUnexecutableGraphs(t *testing.T) {
	tests := []struct {
		name   string
		change func(*catalog.Document)
	}{
		{"unknown artifact", func(d *catalog.Document) {
			l := d.Layouts["qwen-32k"]
			l.Artifact = "absent"
			d.Layouts["qwen-32k"] = l
		}},
		{"incompatible patch", func(d *catalog.Document) {
			p := d.Patches["template"]
			p.CompatibleArtifacts = []string{"another-model"}
			d.Patches["template"] = p
		}},
		{"incomplete Rapid closure", func(d *catalog.Document) {
			e := d.Engines["llama-b10936"]
			e.Family = "rapid-mlx"
			e.Supply.Release = nil
			d.Engines["llama-b10936"] = e
		}},
		{"unknown adapter", func(d *catalog.Document) {
			e := d.Engines["llama-b10936"]
			e.Adapter = "llama-server/future"
			d.Engines["llama-b10936"] = e
		}},
		{"moving model revision", func(d *catalog.Document) {
			a := d.Artifacts["qwen-q4"]
			a.Revision = "main"
			d.Artifacts["qwen-q4"] = a
		}},
		{"missing library archive", func(d *catalog.Document) {
			e := d.Engines["llama-b10936"]
			e.Supply.Release.Artifact.SHA256 = ""
			d.Engines["llama-b10936"] = e
		}},
		{"unsafe file", func(d *catalog.Document) {
			a := d.Artifacts["qwen-q4"]
			a.Files[0].Path = "../model.gguf"
			d.Artifacts["qwen-q4"] = a
		}},
		{"invalid sampling", func(d *catalog.Document) {
			l := d.Layouts["qwen-32k"]
			l.RequestDefaults.Sampling.TopP = 2
			d.Layouts["qwen-32k"] = l
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := document()
			tt.change(&d)
			if _, err := catalog.Compile(d, catalog.Selection{Schema: catalog.SelectionSchema, Profile: "local-qwen"}, software.Target{OS: "darwin", Arch: "arm64"}); err == nil {
				t.Fatal("accepted unexecutable graph")
			}
		})
	}
}

func TestStrictDecodingAndLockTamper(t *testing.T) {
	raw, _ := json.Marshal(document())
	raw = bytes.Replace(raw, []byte(`"date":`), []byte(`"raw_flags": ["--anything"], "date":`), 1)
	if _, err := catalog.Parse(raw); err == nil {
		t.Fatal("accepted raw flags")
	}
	l := compile(t, document())
	l.Digests.Profile = strings.Repeat("f", 64)
	raw, _ = json.Marshal(l)
	if _, err := catalog.ParseLock(raw); err == nil {
		t.Fatal("accepted altered execution digest")
	}
	if _, err := catalog.ParseSelection([]byte("schema: temper-selection/v1\nprofile: local-qwen\nprofile: overwritten\n")); err == nil {
		t.Fatal("accepted duplicate selection key")
	}
	if _, err := catalog.ParseSelection([]byte("schema: temper-selection/v1\nprofile: local-qwen\ntools: [unrequested]\n")); err == nil {
		t.Fatal("accepted unsupported optional tool")
	}
}

func TestUnselectedLayoutDoesNotEnterLock(t *testing.T) {
	d := document()
	l := d.Layouts["qwen-32k"]
	l.DisplayName = "Unselected alternative"
	d.Layouts["alternative"] = l
	got := compile(t, d)
	if len(got.Records.Layouts) != 1 {
		t.Fatal("lock retained unselected layout")
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
}
