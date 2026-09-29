package catalog_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/render"
	"github.com/temper-sh/temper/internal/software"
)

func TestContextOverridePreservesWeightsAndUsesItsOwnExecutionIdentity(t *testing.T) {
	d := document()
	const id, ceiling = "qwen-32k", 131072
	layout := d.Presets[id]
	layout.ContextLimitTokens = ceiling
	d.Presets[id] = layout
	maximum, err := catalog.CompilePreset(d, id, "", ceiling, software.Target{OS: "darwin", Arch: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	lower, err := catalog.CompilePreset(d, id, "", 65536, maximum.Target)
	if err != nil {
		t.Fatal(err)
	}
	if lower.ExecutionDigest == maximum.ExecutionDigest || d.Presets[id].ContextLimit() != ceiling {
		t.Fatal("context change did not get its own identity or mutated the catalog")
	}
	if !reflect.DeepEqual(lower.Records.Artifacts, maximum.Records.Artifacts) {
		t.Fatal("context override changed weight identity")
	}
	bytes, err := catalog.MarshalLock(lower)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := catalog.ParseLock(bytes)
	if err != nil {
		t.Fatal(err)
	}
	if locked.Records.Presets[id].ContextWindowTokens != 65536 {
		t.Fatal("override lost in exact lock")
	}
	p, err := locked.Projections()
	if err != nil {
		t.Fatal(err)
	}
	result, err := render.Build(render.Inputs{Manifest: p.Manifest, Lock: p.Artifacts, Mode: locked.Preset, Root: "/context-test"})
	if err != nil {
		t.Fatal(err)
	}
	// The renderer's public artifact must actually pass the chosen window.
	found := false
	for _, artifact := range result.Artifacts {
		if strings.Contains(string(artifact.Data), "-c 65536") {
			found = true
		}
	}
	if !found {
		t.Fatal("selected context did not reach rendered engine command")
	}
}

func TestContextChoicesRefuseUnknownAndOutOfRangeWindows(t *testing.T) {
	d := document()
	const id, other = "qwen-32k", "unselected"
	layout := d.Presets[id]
	layout.ContextLimitTokens = 131072
	d.Presets[id], d.Presets[other] = layout, layout
	for _, tc := range []struct {
		name, layout string
		tokens       int
		want         string
	}{
		{"unknown", "unknown", 65536, "unknown preset"},
		{"negative", id, -1, "context"},
		{"no input capacity", id, 4096, "context"},
		{"above native maximum", id, layout.ContextLimitTokens + 1, "context"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := catalog.CompilePreset(d, tc.layout, "", tc.tokens, software.Target{OS: "darwin", Arch: "arm64"}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("invalid context accepted: %v", err)
			}
		})
	}
}
