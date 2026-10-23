package catalog_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/render"
)

func TestCPUMoEPlacementRoundTripsAndChangesOnlyExecution(t *testing.T) {
	d := guidedCatalog(t)
	for _, count := range []int{0, 4} {
		p := d.Presets[mtp]
		p.Profiles[0].EngineConfig.GPULayers = 7
		d.Presets[mtp] = p
		baseline := compileGuided(t, d, mtp)
		p.Profiles[0].EngineConfig.Controls.CPUMoELayers = count
		d.Presets[mtp] = p
		raw, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := catalog.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		lock := compileGuided(t, parsed, mtp)
		raw, err = catalog.MarshalLock(lock)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte("cpu_moe_layers")) != (count > 0) {
			t.Fatal("zero changed the existing wire shape")
		}
		lock, err = catalog.ParseLock(raw)
		if err != nil {
			t.Fatal(err)
		}
		if lock.Records.Presets[mtp].Config().Controls.CPUMoELayers != count {
			t.Fatal("placement lost")
		}
		projection, err := lock.Projections()
		if err != nil {
			t.Fatal(err)
		}
		bundle, err := render.Build(render.Inputs{StartPort: 10001, Manifest: projection.Manifest, Lock: projection.Artifacts, Mode: mtp, Root: "/test"})
		if err != nil {
			t.Fatal(err)
		}
		var output string
		for _, a := range bundle.Artifacts {
			if a.Path == "llama-swap/config.yaml" {
				output = string(a.Data)
			}
		}
		if strings.Contains(output, "--n-cpu-moe 4") != (count > 0) || !strings.Contains(output, "-ngl 7") {
			t.Fatal("placement lost or changed GPU layers", output)
		}
		if count > 0 && catalog.SameExecution(baseline, lock) {
			t.Fatal("placement missing from execution identity")
		}
	}
}

func TestCPUMoEAuthoringRejectsInvalidCountsAndUnsupportedEngines(t *testing.T) {
	for _, value := range []any{-1, 1.5, "4", 2147483648} {
		d := guidedCatalog(t)
		u := catalog.UserPreset{Schema: catalog.UserPresetSchema, Origin: mtp, Changes: map[string]any{"engine_config": map[string]any{"controls": map[string]any{"cpu_moe_layers": value}}}}
		if resolved, _, err := catalog.ResolveUserPreset("mine", u, catalog.View{Document: d}); err == nil {
			if _, err := catalog.CompilePreset(resolved, "mine", 32, 0, d.Runtime.Router.Target); err == nil {
				t.Fatalf("invalid placement accepted: %v", value)
			}
		}
	}
	d := qwenStudy(t)
	for _, origin := range []string{"rapid-mlx", "vllm-metal", "splash-q4"} {
		u := catalog.UserPreset{Schema: catalog.UserPresetSchema, Origin: origin, Changes: map[string]any{"engine_config": map[string]any{"controls": map[string]any{"cpu_moe_layers": 4}}}}
		if _, _, err := catalog.ResolveUserPreset("mine", u, catalog.View{Document: d}); err == nil {
			t.Fatal("unsupported engine accepted MoE placement", origin)
		}
	}
}
