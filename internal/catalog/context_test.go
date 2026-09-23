package catalog_test

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/render"
	"github.com/temper-sh/temper/internal/software"
)

func TestGuidedContextsSeparateAuthoredWindowAndNativeCeiling(t *testing.T) {
	raw, err := os.ReadFile("../../catalog/guided-setup.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range []string{"qwen3.8-27b-q4xl-local", "qwen3.5-4b-local", "qwen3.5-4b-utility"} {
		t.Run(profile, func(t *testing.T) {
			id := d.Profiles[profile].Bindings[0].Layout
			original := catalog.Selection{Schema: catalog.SelectionSchema, Profile: profile}
			selected, err := catalog.ResolveSelection(d, original)
			if err != nil {
				t.Fatal(err)
			}
			if selected.ContextWindows[id] != d.Layouts[id].ContextWindowTokens || d.Layouts[id].ContextLimit() != 262144 || original.ContextWindows != nil {
				t.Fatalf("maximum default or input mutated: %+v", selected)
			}
			selected.ContextWindows[id] = 262144
			maximum, err := catalog.Compile(d, selected, software.Target{OS: "darwin", Arch: "arm64"})
			if err != nil {
				t.Fatal(err)
			}
			selected.ContextWindows[id] = 65536
			if maximum.Selection.ContextWindows[id] != 262144 {
				t.Fatal("editing a selection mutated an already compiled lock")
			}
			lower, err := catalog.Compile(d, selected, maximum.Target)
			if err != nil {
				t.Fatal(err)
			}
			if lower.Digests.Profile == maximum.Digests.Profile || d.Layouts[id].ContextLimit() != 262144 {
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
			if locked.Selection.ContextWindows[id] != 65536 || locked.Records.Layouts[id].ContextWindowTokens != 65536 {
				t.Fatal("override lost in exact lock")
			}
			p, err := locked.Projections()
			if err != nil {
				t.Fatal(err)
			}
			result, err := render.Build(render.Inputs{Manifest: p.Manifest, Lock: p.Artifacts, Mode: profile, Root: "/context-test"})
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
		})
	}
}

func TestContextChoicesRefuseUnknownUnselectedAndOutOfRangeWindows(t *testing.T) {
	raw, err := os.ReadFile("../../catalog/guided-setup.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	id := d.Profiles["qwen3.8-27b-q4xl-local"].Bindings[0].Layout
	other := d.Profiles["qwen3.5-4b-local"].Bindings[0].Layout
	for _, tc := range []struct {
		name, layout string
		tokens       int
		want         string
	}{
		{"unknown", "unknown", 65536, "unselected"},
		{"unselected", other, 65536, "unselected"},
		{"zero", id, 0, "context"},
		{"negative", id, -1, "context"},
		{"no input capacity", id, 4096, "context"},
		{"above native maximum", id, 262145, "context"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := catalog.Selection{Schema: catalog.SelectionSchema, Profile: "qwen3.8-27b-q4xl-local", ContextWindows: map[string]int{tc.layout: tc.tokens}}
			if _, err := catalog.Compile(d, s, software.Target{OS: "darwin", Arch: "arm64"}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("invalid context accepted: %v", err)
			}
		})
	}
}
