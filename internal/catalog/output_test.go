package catalog_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/render"
	"github.com/temper-sh/temper/internal/software"
	"gopkg.in/yaml.v3"
)

func TestOmittedOutputLimitSurvivesCatalogLockAndManifest(t *testing.T) {
	for _, tc := range []struct{ preset, command string }{
		{"qwen3.8-27b-q4xl-mtp", "--predict -1"},
		{"qwen3.8-27b-q4xl-splash", "'--max-new-tokens' '118000'"},
		{"nuextract3-q4km-llama", "--predict -1"},
		{"translategemma-4b-q5km-llama", "--predict -1"},
	} {
		t.Run(tc.preset, func(t *testing.T) {
			d := guidedCatalog(t)
			p := d.Presets[tc.preset]
			p.RequestDefaults.MaxOutputTokens = 0
			d.Presets[tc.preset] = p
			raw, err := json.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			d, err = catalog.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			lock := compileGuided(t, d, tc.preset)
			raw, err = catalog.MarshalLock(lock)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(raw, []byte(`"max_output_tokens"`)) {
				t.Fatal("lock invented an output limit")
			}
			lock, err = catalog.ParseLock(raw)
			if err != nil {
				t.Fatal(err)
			}
			projection, err := lock.Projections()
			if err != nil {
				t.Fatal(err)
			}
			raw, err = yaml.Marshal(projection.Manifest)
			if err != nil {
				t.Fatal(err)
			}
			projection.Manifest, err = manifest.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			bundle, err := render.Build(render.Inputs{StartPort: 10001, Manifest: projection.Manifest, Lock: projection.Artifacts, Mode: lock.Preset, Root: "/output-test"})
			if err != nil {
				t.Fatal(err)
			}
			for _, artifact := range bundle.Artifacts {
				if artifact.Path == "llama-swap/config.yaml" {
					var config struct {
						Models map[string]struct{ Cmd string }
					}
					if err := yaml.Unmarshal(artifact.Data, &config); err != nil {
						t.Fatal(err)
					}
					if strings.Contains(config.Models[tc.preset].Cmd, tc.command) {
						return
					}
					t.Fatalf("rendered runtime did not use %s: %s", tc.command, config.Models[tc.preset].Cmd)
				}
			}
			t.Fatalf("rendered runtime did not use %s", tc.command)
		})
	}
}

func TestContextAndOutputDefaultsAreIndependent(t *testing.T) {
	for _, id := range []string{"qwen3.8-27b-q4xl-mtp", "qwen3.8-27b-q4xl-splash"} {
		t.Run(id, func(t *testing.T) {
			d := guidedCatalog(t)
			base := compileGuided(t, d, id)
			before, _ := catalog.MarshalLock(base)
			// A context edit need not reserve the whole output default in advance.
			if _, err := catalog.CompilePreset(d, id, 0, 2048, software.Target{OS: "darwin", Arch: "arm64"}); err != nil {
				t.Fatal(err)
			}
			for _, output := range []int{0, 1024, 2048, 4096} {
				changed, err := catalog.ConfigureExecution(base, id, catalog.ExecutionSettings{ContextWindowTokens: 2048, MaxOutputTokens: output})
				if err != nil {
					t.Fatal(err)
				}
				if p := changed.Records.Presets[id]; p.Window() != 2048 || p.RequestDefaults.MaxOutputTokens != output {
					t.Fatal("configuration lost the requested window or output default")
				}
			}
			after, _ := catalog.MarshalLock(base)
			if !bytes.Equal(before, after) {
				t.Fatal("configuration mutated the original lock")
			}
			if _, err := catalog.ConfigureExecution(base, id, catalog.ExecutionSettings{ContextWindowTokens: 2048, MaxOutputTokens: -1}); err == nil {
				t.Fatal("accepted a negative output limit")
			}
		})
	}
}
