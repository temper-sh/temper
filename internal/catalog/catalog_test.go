package catalog_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"

	"github.com/temper-sh/temper/internal/render"
	"github.com/temper-sh/temper/internal/render/engine"
	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter/upstreamrelease"
)

func supply(pkg string) catalog.Supply {
	source := &upstreamrelease.GitHubSource{Repository: "example/" + pkg, Asset: "tool-{version}.tar.gz", ArchiveRoot: "bundle"}
	return catalog.Supply{Package: pkg, Target: software.Target{OS: "darwin", Arch: "arm64"}, Source: source,
		Release: &upstreamrelease.Release{Version: "b10936", Revision: strings.Repeat("b", 40), Artifact: software.Artifact{Locator: source.ReleaseURL("b10936"), SHA256: strings.Repeat("c", 64), Size: 101, UnpackedSize: 200, InstalledEntries: 2, Format: "tar.gz", ArchiveRoot: "bundle"}}}
}

func hf(repo string) []catalog.Source {
	return []catalog.Source{{Hub: catalog.HubHuggingFace, Repo: repo, Revision: strings.Repeat("a", 40)}}
}

func document() catalog.Document {
	return catalog.Document{Schema: catalog.Schema, Date: "2026-09-13", Platform: catalog.Platform, Runtime: catalog.Runtime{Router: catalog.Router{Supply: supply("llama-swap")}},
		Artifacts: map[string]catalog.Artifact{"qwen-q4": {Sources: hf("example/Qwen"), Format: "gguf", License: "Apache-2.0", Files: []catalog.File{{Path: "model.gguf", Bytes: 17, SHA256: strings.Repeat("d", 64)}}}},
		Templates: map[string]catalog.Template{"template": {Sources: hf("example/template"), License: "Apache-2.0", Files: []catalog.File{{Path: "chat.jinja", Bytes: 12, SHA256: strings.Repeat("e", 64)}}, CompatibleArtifacts: []string{"qwen-q4"}}},
		Engines:   map[string]catalog.Engine{"llama-b10936": {Adapter: "llama-server/v1", Supply: supply("llama-cpp"), Interfaces: []string{"chat-completions"}, Modalities: []string{"text"}}},
		Presets: map[string]catalog.Preset{"qwen-32k": {Artifact: "qwen-q4", Template: "template", Engine: "llama-b10936", Interface: "chat-completions", Modalities: []string{"text"},
			Compute: catalog.ComputeGPU, ComputeOptions: []catalog.ComputeDevice{catalog.ComputeGPU, catalog.ComputeCPU},
			RequestDefaults: catalog.RequestDefaults{MaxOutputTokens: 4096, Reasoning: "off", Sampling: engine.SamplingDefaults{Temperature: 0, TopK: 1, TopP: 1, MinP: 0, RepeatPenalty: 1, PresencePenalty: 0, Seed: seed(17)}},
			Speculation:     catalog.Speculation{Method: "mtp", Source: "embedded", MaxDraftTokens: 3},
			Profiles: []catalog.Profile{{MinMemoryGiB: 32, ContextWindowTokens: 32768, EngineConfig: catalog.EngineConfig{LlamaConfig: catalog.LlamaConfig{Kind: "llama-server/v1", Parallel: 1, KVCache: "q8", FlashAttention: "on", BatchTokens: 512, MicrobatchTokens: 512, ContextCheckpoints: 16, PromptCacheRAMMiB: 0, GPULayers: 99,
				Controls: engine.LlamaServerControls{CacheReuse: 16, ReasoningEffort: "medium", PreserveReasoning: true, ContextShift: false, CachePrompt: true, Fit: "off", Threads: 4, ThreadsBatch: 4, LoadMode: "mmap"}}}}},
			Presentation: catalog.PresetPresentation{DisplayName: "Qwen source work", Description: "A fixture preset.", Group: "other"}}},
		Presentation: &catalog.Presentation{Groups: []catalog.Group{{ID: "other", DisplayName: "Other", Description: "Fixture presets."}}, FallbackGroup: "other", PresetOrder: []string{"qwen-32k"}},
	}
}

func compile(t *testing.T, d catalog.Document) catalog.Lock {
	t.Helper()
	l, err := catalog.CompilePreset(d, "qwen-32k", 0, 0, software.Target{OS: "darwin", Arch: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestRuntimeSettingsReuseInstalledSoftware(t *testing.T) {
	baseline := compile(t, document())
	projection, err := baseline.Projections()
	if err != nil {
		t.Fatal(err)
	}
	want, err := projection.Software.SemanticDigest()
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct {
		name  string
		apply func(*catalog.Preset)
	}{
		{"batch size", func(l *catalog.Preset) { l.Profiles[0].EngineConfig.BatchTokens = 1024 }},
		{"context window", func(l *catalog.Preset) { l.Profiles[0].ContextWindowTokens = 65536 }},
		{"cache precision", func(l *catalog.Preset) { l.Profiles[0].EngineConfig.KVCache = "q4" }},
		{"speculation", func(l *catalog.Preset) { l.Speculation = catalog.Speculation{Method: "none", Source: "none"} }},
	} {
		t.Run(change.name, func(t *testing.T) {
			d := document()
			layout := d.Presets["qwen-32k"]
			change.apply(&layout)
			d.Presets["qwen-32k"] = layout
			candidate := compile(t, d)
			if catalog.SameExecution(candidate, baseline) {
				t.Fatal("execution configuration did not change")
			}
			p, err := candidate.Projections()
			if err != nil {
				t.Fatal(err)
			}
			got, err := p.Software.SemanticDigest()
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatal("runtime settings invalidated unchanged installed software")
			}
		})
	}
}

func TestTemplateIsPartOfThePresetAndItsClosure(t *testing.T) {
	d := document()
	alt := d.Templates["template"]
	alt.Files = []catalog.File{{Path: "alternative.jinja", Bytes: 14, SHA256: strings.Repeat("f", 64)}}
	d.Templates["alternative"] = alt
	for id, template := range map[string]string{"qwen-32k-alternative": "alternative", "qwen-32k-builtin": catalog.BuiltinTemplate} {
		p := d.Presets["qwen-32k"]
		p.Template = template
		d.Presets[id] = p
	}
	baseline := compile(t, d)
	for id, template := range map[string]string{"qwen-32k-alternative": "alternative", "qwen-32k-builtin": ""} {
		t.Run(id, func(t *testing.T) {
			selected, err := catalog.CompilePreset(d, id, 0, 0, software.Target{OS: "darwin", Arch: "arm64"})
			if err != nil {
				t.Fatal(err)
			}
			if catalog.SameExecution(selected, baseline) {
				t.Fatal("another template did not change what runs")
			}
			if !reflect.DeepEqual(selected.Records.Artifacts, baseline.Records.Artifacts) {
				t.Fatal("template choice copied or changed model weights")
			}
			if want := map[bool]int{true: 1, false: 0}[template != ""]; len(selected.Records.Templates) != want {
				t.Fatal("lock retained an unselected template")
			}
			projection, err := selected.Projections()
			if err != nil {
				t.Fatal(err)
			}
			if got := projection.Manifest.Layouts[id].ChatTemplate; got != template {
				t.Fatalf("manifest template = %q, want %q", got, template)
			}
			if got := len(projection.Artifacts.Entries[id].Patches); got != len(selected.Records.Templates) {
				t.Fatalf("artifact template count = %d", got)
			}
		})
	}
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
	if err = p.Manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	software := p.Software
	if len(software.Selections) != 2 || software.Target.OS != "darwin" || software.Target.Arch != "arm64" {
		t.Fatalf("incomplete portable software projection: %+v", software)
	}
	bundle, err := render.Build(render.Inputs{Manifest: p.Manifest, Lock: p.Artifacts, Mode: l.Preset, Root: "/isolated root"})
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
	layout := d.Presets["qwen-32k"]
	layout.Profiles[0].EngineConfig.KVCache = "q4"
	d.Presets["qwen-32k"] = layout
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
	parsed := projection.Manifest
	bundle, err := render.Build(render.Inputs{Manifest: parsed, Lock: projection.Artifacts, Mode: locked.Preset, Root: "/isolated root"})
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
			layout := d.Presets["qwen-32k"]
			layout.Profiles[0].EngineConfig.Controls.CheckpointMinStep = test.spacing
			d.Presets["qwen-32k"] = layout
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
			parsed := projection.Manifest
			got := parsed.Layouts["qwen-32k"].Llama.Controls.CheckpointMinStep
			if !reflect.DeepEqual(got, test.spacing) {
				t.Fatalf("checkpoint spacing lost or changed during export: %v", got)
			}
			bundle, err := render.Build(render.Inputs{Manifest: parsed, Lock: projection.Artifacts, Mode: locked.Preset, Root: "/isolated root"})
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
	layout := d.Presets["qwen-32k"]
	layout.Profiles[0].EngineConfig.Controls.CheckpointMinStep = &negative
	d.Presets["qwen-32k"] = layout
	_, err := catalog.CompilePreset(d, "qwen-32k", 0, 0, software.Target{OS: "darwin", Arch: "arm64"})
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
			p := d.Templates["template"]
			p.Files[0].SHA256 = strings.Repeat("f", 64)
			d.Templates["template"] = p
		}, true, true, true},
		{"sampling", func(d *catalog.Document) {
			l := d.Presets["qwen-32k"]
			l.RequestDefaults.Sampling.Temperature = .7
			d.Presets["qwen-32k"] = l
		}, false, true, true},
		{"checkpoint spacing", func(d *catalog.Document) {
			spacing := 1024
			l := d.Presets["qwen-32k"]
			l.Profiles[0].EngineConfig.Controls.CheckpointMinStep = &spacing
			d.Presets["qwen-32k"] = l
		}, false, true, true},
		{"display name", func(d *catalog.Document) {
			l := d.Presets["qwen-32k"]
			l.Presentation.DisplayName = "Updated description"
			d.Presets["qwen-32k"] = l
		}, false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := document()
			tt.change(&d)
			got := compile(t, d)
			if reflect.DeepEqual(got, base) {
				t.Error("lock records did not change")
			}
			if changed := !catalog.SameExecution(got, base); changed != tt.profile {
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
			l := d.Presets["qwen-32k"]
			l.Artifact = "absent"
			d.Presets["qwen-32k"] = l
		}},
		{"incompatible template", func(d *catalog.Document) {
			p := d.Templates["template"]
			p.CompatibleArtifacts = []string{"another-model"}
			d.Templates["template"] = p
		}},
		{"incomplete Rapid closure", func(d *catalog.Document) {
			e := d.Engines["llama-b10936"]
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
			a.Sources = []catalog.Source{{Hub: catalog.HubHuggingFace, Repo: "example/Qwen", Revision: "main"}}
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
			l := d.Presets["qwen-32k"]
			l.RequestDefaults.Sampling.TopP = 2
			d.Presets["qwen-32k"] = l
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := document()
			tt.change(&d)
			if _, err := catalog.CompilePreset(d, "qwen-32k", 0, 0, software.Target{OS: "darwin", Arch: "arm64"}); err == nil {
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
	raw, _ = json.Marshal(l)
	stored := bytes.Replace(raw, []byte(`"preset":`), []byte(`"execution_digest": "`+strings.Repeat("f", 64)+`", "preset":`), 1)
	if _, err := catalog.ParseLock(stored); err == nil {
		t.Fatal("accepted a stored execution fingerprint")
	}
	for id, artifact := range l.Records.Artifacts {
		l.Records.Artifacts[id+"-unselected"] = artifact
		break
	}
	raw, _ = json.Marshal(l)
	if _, err := catalog.ParseLock(raw); err == nil || !strings.Contains(err.Error(), "closure") {
		t.Fatalf("accepted an unselected record: %v", err)
	}
}

func TestUnselectedLayoutDoesNotEnterLock(t *testing.T) {
	d := document()
	l := d.Presets["qwen-32k"]
	l.Presentation.DisplayName = "Unselected alternative"
	d.Presets["alternative"] = l
	got := compile(t, d)
	if len(got.Records.Presets) != 1 {
		t.Fatal("lock retained unselected layout")
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
}

func seed(n int) *int { return &n }

// Without a catalog seed the engine picks a random one, so asking again gives
// a new answer; a seeded preset stays repeatable.
func TestSeedIsPassedOnlyWhenThePresetFixesOne(t *testing.T) {
	for _, tc := range []struct {
		seed *int
		want string
	}{{seed(17), "--seed 17"}, {nil, ""}} {
		d := document()
		p := d.Presets["qwen-32k"]
		p.RequestDefaults.Sampling.Seed = tc.seed
		d.Presets["qwen-32k"] = p
		l := compile(t, d)
		projection, err := l.Projections()
		if err != nil {
			t.Fatal(err)
		}
		bundle, err := render.Build(render.Inputs{Manifest: projection.Manifest, Lock: projection.Artifacts, Mode: l.Preset, Root: "/root"})
		if err != nil {
			t.Fatal(err)
		}
		var config string
		for _, a := range bundle.Artifacts {
			if a.Path == "llama-swap/config.yaml" {
				config = string(a.Data)
			}
		}
		if got := strings.Contains(config, "--seed"); got != (tc.want != "") || tc.want != "" && !strings.Contains(config, tc.want) {
			t.Errorf("seed %v: rendered command:\n%s", tc.seed, config)
		}
	}
}
