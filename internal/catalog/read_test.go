package catalog_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/buildinfo"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/software"
)

// tree is the fixture catalog as JSON values, for edits a newer publisher
// could make.
func tree(t *testing.T) map[string]any {
	t.Helper()
	d := document()
	second := d.Presets["qwen-32k"]
	second.Template = catalog.BuiltinTemplate
	d.Presets["qwen-builtin"] = second
	d.Presentation.PresetOrder = append(d.Presentation.PresetOrder, "qwen-builtin")
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func record(t *testing.T, value map[string]any, kind, id string) map[string]any {
	t.Helper()
	return value[kind].(map[string]any)[id].(map[string]any)
}

func read(t *testing.T, value map[string]any, client string) catalog.View {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	view, err := catalog.Read(raw, client)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func unavailable(view catalog.View, kind, id string) string {
	for _, u := range view.Unavailable {
		if u.Kind == kind && u.ID == id {
			return u.Reason
		}
	}
	return ""
}

func TestReaderHidesOnlyTheRecordsANewerCatalogChanged(t *testing.T) {
	for _, tc := range []struct {
		name            string
		edit            func(map[string]any)
		kind, id, why   string
		presetsHidden   []string
		presetsKept     []string
		othersAvailable bool
	}{
		{"unknown preset field", func(v map[string]any) { record(t, v, "presets", "qwen-32k")["new_setting"] = 1 },
			"preset", "qwen-32k", "needs a newer Temper", []string{"qwen-32k"}, []string{"qwen-builtin"}, true},
		{"new value in a preset", func(v map[string]any) { record(t, v, "presets", "qwen-32k")["interface"] = "embeddings" },
			"preset", "qwen-32k", "interface", []string{"qwen-32k"}, []string{"qwen-builtin"}, true},
		{"unused new adapter", func(v map[string]any) {
			e := map[string]any{}
			for k, x := range record(t, v, "engines", "llama-b10936") {
				e[k] = x
			}
			e["adapter"] = "future-engine/v1"
			v["engines"].(map[string]any)["future"] = e
		}, "engine", "future", "supported engine adapter", nil, []string{"qwen-32k", "qwen-builtin"}, true},
		{"unknown template transform", func(v map[string]any) { record(t, v, "templates", "template")["transform"] = "future-transform-v1" },
			"template", "template", "transform", []string{"qwen-32k"}, []string{"qwen-builtin"}, true},
		{"newer engine", func(v map[string]any) { record(t, v, "engines", "llama-b10936")["minimum_temper"] = "0.2.0" },
			"engine", "llama-b10936", "needs Temper 0.2.0 or newer (this is 0.1.0)", []string{"qwen-32k", "qwen-builtin"}, nil, true},
		{"artifact only on a later hub", func(v map[string]any) {
			record(t, v, "artifacts", "qwen-q4")["sources"] = []any{map[string]any{"hub": "future-hub", "repo": "example/Qwen", "revision": strings.Repeat("a", 40)}}
		}, "artifact", "qwen-q4", "Hugging Face", []string{"qwen-32k", "qwen-builtin"}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := tree(t)
			tc.edit(value)
			view := read(t, value, "0.1.0")
			if reason := unavailable(view, tc.kind, tc.id); !strings.Contains(reason, tc.why) {
				t.Fatalf("%s %s reason = %q, want %q; view %+v", tc.kind, tc.id, reason, tc.why, view.Unavailable)
			}
			for _, id := range tc.presetsHidden {
				if _, ok := view.Document.Presets[id]; ok || unavailable(view, "preset", id) == "" {
					t.Fatalf("preset %s stayed available", id)
				}
			}
			for _, id := range tc.presetsKept {
				if _, err := catalog.CompilePreset(view.Document, id, 0, 0, software.Target{OS: "darwin", Arch: "arm64"}); err != nil {
					t.Fatalf("preset %s was hidden: %v", id, err)
				}
			}
		})
	}
}

func TestReaderIgnoresWhatNothingUsesAndRefusesSharedProblems(t *testing.T) {
	value := tree(t)
	value["future_records"] = map[string]any{"a": 1}
	value["runtime"].(map[string]any)["future_component"] = map[string]any{"package": "x"}
	engine := record(t, value, "engines", "llama-b10936")
	engine["interfaces"] = []any{"chat-completions", "embeddings"}
	artifact := record(t, value, "artifacts", "qwen-q4")
	artifact["sources"] = append(artifact["sources"].([]any), map[string]any{"hub": "future-hub", "repo": "mirror/Qwen", "revision": strings.Repeat("b", 40), "region": "eu"})
	view := read(t, value, "0.1.0")
	if len(view.Unavailable) != 0 || len(view.Document.Presets) != 2 {
		t.Fatalf("unused additions hid records: %+v", view.Unavailable)
	}
	if got := view.Document.Engines["llama-b10936"].Interfaces; len(got) != 1 || got[0] != "chat-completions" {
		t.Fatalf("unknown capability was kept: %v", got)
	}
	if got := view.Document.Artifacts["qwen-q4"].Sources; len(got) != 1 || got[0].Hub != catalog.HubHuggingFace {
		t.Fatalf("unusable source was kept: %+v", got)
	}

	for name, edit := range map[string]func(map[string]any){
		"router field": func(v map[string]any) { v["runtime"].(map[string]any)["router"].(map[string]any)["future"] = true },
		"newer router": func(v map[string]any) {
			v["runtime"].(map[string]any)["router"].(map[string]any)["minimum_temper"] = "9.0.0"
		},
		"another schema": func(v map[string]any) { v["schema"] = "temper-catalog/v2" },
		"another platform": func(v map[string]any) {
			v["platform"] = "linux-cuda"
		},
		"no date": func(v map[string]any) { delete(v, "date") },
	} {
		t.Run(name, func(t *testing.T) {
			value := tree(t)
			edit(value)
			raw, _ := json.Marshal(value)
			if _, err := catalog.Read(raw, "0.1.0"); err == nil {
				t.Fatal("a problem every preset shares did not refuse the catalog")
			}
		})
	}
	newer := tree(t)
	newer["runtime"].(map[string]any)["router"].(map[string]any)["minimum_temper"] = "9.0.0"
	raw, _ := json.Marshal(newer)
	if _, err := catalog.Read(raw, buildinfo.DevVersion); err != nil {
		t.Fatalf("a development build did not satisfy minimum_temper: %v", err)
	}
	if _, err := catalog.Read(raw, "0.1.0"); err == nil || !strings.Contains(err.Error(), "needs Temper 9.0.0 or newer") {
		t.Fatalf("router minimum_temper message = %v", err)
	}
}

func TestProfilesSelectByMemoryAndSkipUnreadableEntries(t *testing.T) {
	value := tree(t)
	preset := record(t, value, "presets", "qwen-32k")
	small := preset["profiles"].([]any)[0].(map[string]any)
	large := map[string]any{}
	for k, x := range small {
		large[k] = x
	}
	large["min_memory_gib"], large["context_window_tokens"] = 64, 65536
	future := map[string]any{"min_memory_gib": 128, "context_window_tokens": 131072, "engine_config": small["engine_config"], "future_condition": "m6"}
	preset["profiles"] = []any{small, large, future}
	view := read(t, value, "0.1.0")
	if len(view.Unavailable) != 0 || len(view.Document.Presets["qwen-32k"].Profiles) != 2 {
		t.Fatalf("unreadable profile handling: %+v", view.Unavailable)
	}
	for memory, want := range map[int]int{0: 32768, 16: 32768, 32: 32768, 48: 32768, 64: 65536, 512: 65536} {
		l, err := catalog.CompilePreset(view.Document, "qwen-32k", memory, 0, software.Target{OS: "darwin", Arch: "arm64"})
		if err != nil {
			t.Fatal(err)
		}
		if got := l.Records.Presets["qwen-32k"]; len(got.Profiles) != 1 || got.Window() != want {
			t.Fatalf("%d GiB selected window %d, want %d", memory, got.Window(), want)
		}
	}
	preset["profiles"] = []any{future}
	if reason := unavailable(read(t, value, "0.1.0"), "preset", "qwen-32k"); !strings.Contains(reason, "no profile") {
		t.Fatalf("preset without a readable profile = %q", reason)
	}
}

func TestLockFromANewerTemperAsksForAnUpgrade(t *testing.T) {
	d := document()
	e := d.Engines["llama-b10936"]
	e.MinimumTemper = "0.3.0"
	d.Engines["llama-b10936"] = e
	l := compile(t, d)
	raw, err := catalog.MarshalLock(l)
	if err != nil {
		t.Fatal(err)
	}
	previous := buildinfo.Version
	buildinfo.Version = "0.1.0"
	defer func() { buildinfo.Version = previous }()
	if _, err := catalog.ParseLock(raw); err == nil || !strings.Contains(err.Error(), "needs Temper 0.3.0 or newer") {
		t.Fatalf("older Temper read a newer lock: %v", err)
	}
}

func TestRecordedReleaseMustComeFromItsSource(t *testing.T) {
	d := document()
	e := d.Engines["llama-b10936"]
	release := *e.Supply.Release
	release.Artifact.Locator = "https://mirror.example/llama.tar.gz"
	e.Supply.Release = &release
	d.Engines["llama-b10936"] = e
	if err := d.Validate(); err == nil || !strings.Contains(err.Error(), "must be downloaded from https://github.com/example/llama-cpp/releases/download/b10936/tool-b10936.tar.gz") {
		t.Fatalf("foreign download location accepted: %v", err)
	}
}
