package catalog_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/testfixture"
)

const mtp = "qwen3.8-27b-q4xl-mtp"

var target = software.Target{OS: "darwin", Arch: "arm64"}

func userPreset(t *testing.T, id, text string) catalog.UserPreset {
	t.Helper()
	u, err := catalog.ParseUserPreset(id, []byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func compileUser(t *testing.T, d catalog.Document, id string, u catalog.UserPreset) (catalog.Lock, []string) {
	t.Helper()
	closure, warnings, err := catalog.ResolveUserPreset(id, u, catalog.View{Document: d})
	if err != nil {
		t.Fatal(err)
	}
	l, err := catalog.CompilePreset(closure, id, 0, 0, target)
	if err != nil {
		t.Fatal(err)
	}
	return l, warnings
}

// leaves flattens a record to dotted paths; a preset's one profile is
// flattened to the top, as a fork's changes name it.
func leaves(value any, prefix string, into map[string]any) {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			leaves(item, path, into)
		}
	case []any:
		if prefix == "profiles" && len(v) == 1 {
			leaves(v[0], "", into)
			return
		}
		raw, _ := json.Marshal(v)
		into[prefix] = string(raw)
	default:
		into[prefix] = v
	}
}

func presetLeaves(t *testing.T, p catalog.Preset) map[string]any {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	result := map[string]any{}
	leaves(value, "", result)
	return result
}

func differences(a, b map[string]any) []string {
	var paths []string
	for path, value := range a {
		if !reflect.DeepEqual(value, b[path]) {
			paths = append(paths, path)
		}
	}
	for path := range b {
		if _, ok := a[path]; !ok {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths
}

func withoutPreset(l catalog.Lock) catalog.Document {
	d := l.Records
	d.Presets = nil
	return d
}

func TestForkLockDiffersFromItsOriginOnlyInItsChangesAndFollowsTheOrigin(t *testing.T) {
	d := testfixture.MixedEngineCatalog(t)
	fork := userPreset(t, "qwen-cool", `{"schema": "temper-preset/v1", "origin": "qwen3.8-27b-q4xl-mtp", "name": "Qwen, cooler",
	  "changes": {"request_defaults": {"sampling": {"temperature": 0.7, "seed": 23}},
	              "engine_config": {"batch_tokens": 1024, "controls": {"threads": 6}}}}`)
	origin, err := catalog.CompilePreset(d, mtp, 0, 0, target)
	if err != nil {
		t.Fatal(err)
	}
	forked, _ := compileUser(t, d, "qwen-cool", fork)
	if !reflect.DeepEqual(withoutPreset(origin), withoutPreset(forked)) {
		t.Fatal("a settings fork changed records other than its preset")
	}
	got := differences(presetLeaves(t, origin.Records.Presets[mtp]), presetLeaves(t, forked.Records.Presets["qwen-cool"]))
	want := []string{"engine_config.batch_tokens", "engine_config.controls.threads", "presentation.display_name", "request_defaults.sampling.seed", "request_defaults.sampling.temperature"}
	if !slices.Equal(got, want) {
		t.Fatalf("fork changed %v, want %v", got, want)
	}
	p := forked.Records.Presets["qwen-cool"]
	if p.RequestDefaults.Sampling.Temperature != 0.7 || *p.RequestDefaults.Sampling.Seed != 23 || p.Config().BatchTokens != 1024 || p.Config().Controls.Threads != 6 || p.Presentation.DisplayName != "Qwen, cooler" {
		t.Fatalf("fork lost a change: %+v", p)
	}

	// The catalog moves on: the fork follows the origin's other changes, and
	// its own value wins where both changed.
	moved := d
	moved.Presets = map[string]catalog.Preset{}
	for id, preset := range d.Presets {
		moved.Presets[id] = preset
	}
	next := moved.Presets[mtp]
	next.RequestDefaults.Sampling.TopK = 40
	next.Profiles = slices.Clone(next.Profiles)
	next.Profiles[0].EngineConfig.BatchTokens = 768
	moved.Presets[mtp] = next
	followed, _ := compileUser(t, moved, "qwen-cool", fork)
	p = followed.Records.Presets["qwen-cool"]
	if p.RequestDefaults.Sampling.TopK != 40 || p.Config().BatchTokens != 1024 {
		t.Fatalf("fork stayed on the old origin or lost its value: top_k %d, batch %d", p.RequestDefaults.Sampling.TopK, p.Config().BatchTokens)
	}
}

func TestCopyKeepsItsValuesWhenTheCatalogRemovesItsRecords(t *testing.T) {
	d := testfixture.MixedEngineCatalog(t)
	origin, err := catalog.CompilePreset(d, mtp, 0, 0, target)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := catalog.MarshalUserPreset(catalog.CopyOf(origin, ""))
	if err != nil {
		t.Fatal(err)
	}
	copied := userPreset(t, "my-qwen", string(raw))
	first, _ := compileUser(t, d, "my-qwen", copied)
	got := differences(presetLeaves(t, origin.Records.Presets[mtp]), presetLeaves(t, first.Records.Presets["my-qwen"]))
	if !slices.Equal(got, []string{"presentation.display_name"}) || first.Records.Presets["my-qwen"].Presentation.DisplayName != origin.Records.Presets[mtp].Presentation.DisplayName+" (yours)" {
		t.Fatalf("copy differs from its source in %v", got)
	}

	// A later catalog drops the preset, its weights and its template, and
	// ships a new engine build: only the engine build changes.
	later := d
	later.Presets = map[string]catalog.Preset{"gemma-4-e2b-q4km-off": d.Presets["gemma-4-e2b-q4km-off"]}
	later.Artifacts = map[string]catalog.Artifact{"gemma-4-e2b-q4km": d.Artifacts["gemma-4-e2b-q4km"]}
	later.Templates = map[string]catalog.Template{}
	later.Engines = map[string]catalog.Engine{"llama-cpp": d.Engines["second-llama-cpp"]}
	second, _ := compileUser(t, later, "my-qwen", copied)
	if !reflect.DeepEqual(first.Records.Presets, second.Records.Presets) || !reflect.DeepEqual(first.Records.Artifacts, second.Records.Artifacts) || !reflect.DeepEqual(first.Records.Templates, second.Records.Templates) {
		t.Fatal("copy followed the catalog")
	}
	if first.Records.Engines["llama-cpp"].Supply.Release.Version == second.Records.Engines["llama-cpp"].Supply.Release.Version {
		t.Fatal("copy did not take the new engine build")
	}
}

func TestForkOfOtherWeightsAndTemplatesUsesTheAcceptanceRule(t *testing.T) {
	d := testfixture.MixedEngineCatalog(t)
	q4 := d.Artifacts["qwen3.8-27b-q4xl"]
	q6, err := json.Marshal(catalog.UserArtifact{Artifact: catalog.Artifact{Sources: q4.Sources, Format: "gguf", License: q4.License,
		Files: []catalog.File{{Path: "Qwen3.8-27B-Q6_K.gguf", Bytes: 22 << 30, SHA256: strings.Repeat("6", 64)}}}, SameModelAs: "qwen3.8-27b-q4xl"})
	if err != nil {
		t.Fatal(err)
	}
	weights := userPreset(t, "qwen-q6", `{"schema": "temper-preset/v1", "origin": "qwen3.8-27b-q4xl-mtp",
	  "changes": {"artifact": "qwen3.8-27b-q6k"}, "artifacts": {"qwen3.8-27b-q6k": `+string(q6)+`}}`)
	l, _ := compileUser(t, d, "qwen-q6", weights)
	template := l.Records.Templates["froggeric-qwen38"]
	if l.Records.Presets["qwen-q6"].Artifact != "qwen3.8-27b-q6k" || !slices.Contains(template.CompatibleArtifacts, "qwen3.8-27b-q6k") {
		t.Fatal("the lock does not record that the template accepted the other weights")
	}
	builtin := userPreset(t, "qwen-own-template", `{"schema": "temper-preset/v1", "origin": "qwen3.8-27b-q4xl-mtp", "changes": {"template": "builtin"}}`)
	if l, _ := compileUser(t, d, "qwen-own-template", builtin); len(l.Records.Templates) != 0 || l.Records.Presets["qwen-own-template"].TemplateID() != catalog.BuiltinTemplate {
		t.Fatal("builtin template fork kept a template record")
	}
	next := d.Templates["froggeric-qwen38"]
	record, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	newer := userPreset(t, "qwen-next-template", `{"schema": "temper-preset/v1", "origin": "qwen3.8-27b-q4xl-mtp",
	  "changes": {"template": "froggeric-qwen38"}, "templates": {"froggeric-qwen38": `+strings.Replace(string(record), next.Files[0].SHA256, strings.Repeat("e", 64), 1)+`}}`)
	l, warnings := compileUser(t, d, "qwen-next-template", newer)
	if l.Records.Templates["froggeric-qwen38"].Files[0].SHA256 != strings.Repeat("e", 64) || len(warnings) != 1 || !strings.Contains(warnings[0], `templates["froggeric-qwen38"]`) {
		t.Fatalf("file template did not shadow the catalog's with a warning: %v", warnings)
	}
}

func TestUserPresetRefusalsNameTheProblem(t *testing.T) {
	d := testfixture.MixedEngineCatalog(t)
	for name, test := range map[string]struct{ text, want string }{
		"both forms":      {`{"schema": "temper-preset/v1", "origin": "qwen3.8-27b-q4xl-mtp", "preset": {}}`, "exactly one of origin"},
		"no form":         {`{"schema": "temper-preset/v1"}`, "exactly one of origin"},
		"unknown field":   {`{"schema": "temper-preset/v1", "origin": "qwen3.8-27b-q4xl-mtp", "colour": "blue"}`, "colour"},
		"wrong schema":    {`{"schema": "temper-preset/v2", "origin": "qwen3.8-27b-q4xl-mtp"}`, "temper-preset/v1"},
		"closed set":      {`{"schema": "temper-preset/v1", "origin": "qwen3.8-27b-q4xl-mtp", "changes": {"engine": "splash"}}`, "changes.engine is not a setting"},
		"other engine":    {`{"schema": "temper-preset/v1", "origin": "qwen3.8-27b-q4xl-mtp", "changes": {"engine_config": {"max_memory_bytes": 1}}}`, "changes.engine_config.max_memory_bytes is not a setting"},
		"type":            {`{"schema": "temper-preset/v1", "origin": "qwen3.8-27b-q4xl-mtp", "changes": {"engine_config": {"batch_tokens": "big"}}}`, "must be a whole number"},
		"choice":          {`{"schema": "temper-preset/v1", "origin": "qwen3.8-27b-q4xl-mtp", "changes": {"engine_config": {"kv_cache": "q2"}}}`, "must be one of q4, q8, f16"},
		"removal":         {`{"schema": "temper-preset/v1", "origin": "qwen3.8-27b-q4xl-mtp", "changes": {"request_defaults": {"sampling": {"seed": null}}}}`, "cannot be removed"},
		"engine rule":     {`{"schema": "temper-preset/v1", "origin": "qwen3.8-27b-q4xl-mtp", "changes": {"engine_config": {"microbatch_tokens": 99999}}}`, "ubatch must not exceed batch"},
		"another model":   {`{"schema": "temper-preset/v1", "origin": "qwen3.8-27b-q4xl-mtp", "changes": {"artifact": "gemma-4-e2b-q4km"}}`, "a fork keeps its origin's model"},
		"unknown origin":  {`{"schema": "temper-preset/v1", "origin": "qwen-9"}`, `origin "qwen-9" is not in this catalog`},
		"copy references": {`{"schema": "temper-preset/v1", "preset": ` + presetJSON(t, d, mtp) + `}`, `add artifacts["qwen3.8-27b-q4xl"]`},
	} {
		t.Run(name, func(t *testing.T) {
			u, err := catalog.ParseUserPreset("mine", []byte(test.text))
			if err == nil {
				_, _, err = catalog.ResolveUserPreset("mine", u, catalog.View{Document: d})
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want %q", err, test.want)
			}
			if strings.Contains(fmt.Sprint(err), "catalog.") {
				t.Fatalf("error names a Go type: %v", err)
			}
		})
	}
}

func presetJSON(t *testing.T, d catalog.Document, id string) string {
	t.Helper()
	raw, err := json.Marshal(d.Presets[id])
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// engineSettings are the leaf settings each engine_config kind decodes, from
// the types the compiler reads.
func engineSettings(kind string) map[string]reflect.Type {
	types := map[string]reflect.Type{
		"llama-server/v1": reflect.TypeOf(catalog.LlamaConfig{}),
		"splash/v2":       reflect.TypeOf(catalog.SplashConfig{}),
		"crispembed/v1":   reflect.TypeOf(catalog.CrispEmbedConfig{}),
		"rapid-mlx/v1": reflect.TypeOf(struct {
			RapidMLX manifest.RapidMLXTuning `yaml:"rapid_mlx"`
		}{}),
		"vllm-metal/v1": reflect.TypeOf(struct {
			VLLMMetal manifest.VLLMMetalTuning `yaml:"vllm_metal"`
		}{}),
	}
	result := map[string]reflect.Type{}
	var walk func(reflect.Type, string)
	walk = func(t reflect.Type, prefix string) {
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			switch {
			case strings.Contains(f.Tag.Get("yaml"), "inline"):
				walk(ft, prefix)
			case name == "" || name == "-" || name == "kind":
			case ft.Kind() == reflect.Struct:
				walk(ft, prefix+name+".")
			default:
				result[prefix+name] = ft
			}
		}
	}
	walk(types[kind], "engine_config.")
	return result
}

func TestFieldTableNamesExactlyTheEngineSettingsTheCompilerReads(t *testing.T) {
	kinds := map[reflect.Kind][]string{reflect.Int: {catalog.FieldInteger}, reflect.Int64: {catalog.FieldInteger}, reflect.Float64: {catalog.FieldNumber},
		reflect.Bool: {catalog.FieldSwitch}, reflect.String: {catalog.FieldText, catalog.FieldChoice}}
	for _, kind := range []string{"llama-server/v1", "splash/v2", "crispembed/v1", "rapid-mlx/v1", "vllm-metal/v1"} {
		settings := engineSettings(kind)
		rows := map[string]catalog.Field{}
		for _, f := range catalog.Fields(kind) {
			if strings.HasPrefix(f.Path, "engine_config.") {
				rows[f.Path] = f
			}
		}
		for path, typ := range settings {
			row, ok := rows[path]
			if !ok {
				t.Errorf("%s: the compiler reads %s but no fork can change it", kind, path)
			} else if !slices.Contains(kinds[typ.Kind()], row.Type) {
				t.Errorf("%s: %s is a %s setting but its row says %s", kind, path, typ.Kind(), row.Type)
			}
		}
		for path := range rows {
			if _, ok := settings[path]; !ok {
				t.Errorf("%s: row %s names no setting the compiler reads", kind, path)
			}
		}
	}
	// Preset rows name leaves of a real preset record.
	seed := 1
	p := testfixture.MixedEngineCatalog(t).Presets[mtp]
	p.RequestDefaults.Sampling.Seed, p.RequestDefaults.MaxOutputTokens, p.Speculation.DraftArtifact = &seed, 1, "draft"
	known := presetLeaves(t, p)
	for _, f := range catalog.Fields("llama-server/v1") {
		if _, ok := known[f.Path]; !ok && !strings.HasPrefix(f.Path, "engine_config.") {
			t.Errorf("preset row %s names no field of a preset record", f.Path)
		}
	}
}

func TestEveryListedEngineChoiceCompilesAndOthersAreRefused(t *testing.T) {
	mixed, study := testfixture.MixedEngineCatalog(t), qwenStudy(t)
	for _, origin := range []struct {
		d  catalog.Document
		id string
	}{{mixed, mtp}, {mixed, "qwen3.8-27b-q4xl-splash"}, {mixed, "ettin-reranker-150m-q8-crispembed"}, {study, "rapid-mlx"}, {study, "vllm-metal"}} {
		p := origin.d.Presets[origin.id]
		for _, f := range catalog.Fields(p.Config().Kind) {
			if f.Type != catalog.FieldChoice || !strings.HasPrefix(f.Path, "engine_config.") {
				continue
			}
			for _, choice := range f.Choices {
				t.Run(origin.id+"/"+f.Path+"="+choice, func(t *testing.T) {
					changes := nested(f.Path, `"`+choice+`"`)
					if p.Config().Splash != nil && f.Path == "engine_config.reasoning_effort" {
						// Splash ties reasoning off to effort none.
						changes = `{"request_defaults": {"reasoning": "` + map[bool]string{true: "off", false: "on"}[choice == "none"] + `"}, ` + strings.TrimPrefix(changes, "{")
					}
					compileUser(t, origin.d, "mine", userPreset(t, "mine", `{"schema": "temper-preset/v1", "origin": "`+origin.id+`", "changes": `+changes+`}`))
				})
			}
			// The compiler, not only the table, refuses a value outside the choices.
			copied := origin.d
			copied.Presets = map[string]catalog.Preset{"mine": withSetting(t, p, f.Path, "not-a-choice")}
			if _, err := catalog.CompilePreset(copied, "mine", 0, 0, target); err == nil {
				t.Errorf("%s: the compiler accepts %s outside the table's choices", origin.id, f.Path)
			}
		}
	}
}

func nested(path, value string) string {
	parts := strings.Split(path, ".")
	text := value
	for i := len(parts) - 1; i >= 0; i-- {
		text = `{"` + parts[i] + `": ` + text + `}`
	}
	return text
}

func withSetting(t *testing.T, p catalog.Preset, path, value string) catalog.Preset {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	config := record["profiles"].([]any)[0].(map[string]any)["engine_config"].(map[string]any)
	parts := strings.Split(strings.TrimPrefix(path, "engine_config."), ".")
	for _, part := range parts[:len(parts)-1] {
		config = config[part].(map[string]any)
	}
	config[parts[len(parts)-1]] = value
	raw, err = json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var result catalog.Preset
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
