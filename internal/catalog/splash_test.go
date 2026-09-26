package catalog_test

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/render"
	"gopkg.in/yaml.v3"
)

const splashLayout = "qwen3.8-27b-q4xl-splash"

func splashCatalog(t *testing.T) catalog.Document {
	t.Helper()
	raw, err := os.ReadFile("../../catalog/guided-setup.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func compileSplash(t *testing.T, doc catalog.Document) catalog.Lock {
	t.Helper()
	lock, err := catalog.Compile(doc, catalog.Selection{Schema: catalog.SelectionSchema, Profile: "qwen3.8-27b-splash-local", ContextWindows: map[string]int{splashLayout: 32768}}, doc.Runtime.Router.Target)
	if err != nil {
		t.Fatal(err)
	}
	return lock
}

func TestSplashSelectionCarriesExactSidecarAndOfflineRuntime(t *testing.T) {
	doc := splashCatalog(t)
	locked := compileSplash(t, doc)
	if len(locked.Records.Artifacts) != 2 || len(locked.Records.Engines) != 1 || doc.LayoutOrder[0] != splashLayout {
		t.Fatal("incomplete or misordered Splash composition")
	}
	raw, err := json.Marshal(locked)
	if err != nil {
		t.Fatal(err)
	}
	// Public locks use canonical encoding; inspect the derived projection through
	// both serializers to catch omissions in the tagged engine configuration.
	var round catalog.Lock
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatal(err)
	}
	if err := round.Validate(); err != nil {
		t.Fatal(err)
	}
	projection, err := round.Projections()
	if err != nil {
		t.Fatal(err)
	}
	entry := projection.Artifacts.Entries[splashLayout]
	if entry.Draft == nil || entry.Draft.Repo != "incoai/Qwen3.8-27B-DFlash2" || len(entry.Draft.Files) != 2 {
		t.Fatal(entry)
	}
	encoded, err := yaml.Marshal(projection.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manifest.Parse(encoded); err != nil {
		t.Fatal(err)
	}
	bundle, err := render.Build(render.Inputs{Manifest: projection.Manifest, Lock: projection.Artifacts, Mode: locked.Selection.Profile, Root: "/temper path"})
	if err != nil {
		t.Fatal(err)
	}
	var config, requirements string
	for _, file := range bundle.Artifacts {
		if file.Path == "llama-swap/config.yaml" {
			config = string(file.Data)
		}
		if file.Path == "runtime/requirements.json" {
			requirements = string(file.Data)
		}
	}
	for _, expected := range []string{"python3.13", "HF_HUB_OFFLINE=1", "SPLASH_WEIGHT_CACHE=", "--no-webui", `"seed?": 17`, `"top_k?": 20`, "healthCheckTimeout: 660"} {
		if !strings.Contains(config, expected) {
			t.Errorf("missing %q in config", expected)
		}
	}
	for _, expected := range []string{`"role": "frontend"`, `"role": "engine"`, `"relative_executable": "engine/splash"`, "serve-native"} {
		if !strings.Contains(requirements, expected) {
			t.Errorf("missing %q in requirements", expected)
		}
	}
	if strings.Contains(config, "llama-server") || strings.Contains(config, "install/launcher") || strings.Contains(config, "--draft-model") {
		t.Fatal("render used a mutable resolver or another engine")
	}
}

func TestSplashDraftChangesInvalidateCompositionButLeaveTargetIdentity(t *testing.T) {
	doc := splashCatalog(t)
	before := compileSplash(t, doc)
	draft := doc.Artifacts["qwen3.8-27b-dflash2"]
	draft.Files = slices.Clone(draft.Files)
	draft.Files[1].SHA256 = strings.Repeat("a", 64)
	doc.Artifacts["qwen3.8-27b-dflash2"] = draft
	after := compileSplash(t, doc)
	if before.Digests.Profile == after.Digests.Profile {
		t.Fatal("draft update did not invalidate execution")
	}
	p1, _ := before.Projections()
	p2, _ := after.Projections()
	e1, e2 := p1.Artifacts.Entries[splashLayout], p2.Artifacts.Entries[splashLayout]
	if e1.Digest() == e2.Digest() || e1.Files[0] != e2.Files[0] {
		t.Fatal("draft identity was lost or target identity changed")
	}
}

func TestSplashRejectsMixedEngineFieldsAndMissingDraft(t *testing.T) {
	for _, mutate := range []func(*catalog.Document){
		func(d *catalog.Document) {
			l := d.Layouts[splashLayout]
			l.EngineConfig.Parallel = 1
			d.Layouts[splashLayout] = l
		},
		func(d *catalog.Document) {
			l := d.Layouts[splashLayout]
			l.Speculation.DraftArtifact = ""
			d.Layouts[splashLayout] = l
		},
		func(d *catalog.Document) {
			l := d.Layouts[splashLayout]
			l.RequestDefaults.Sampling.TopK = 33
			d.Layouts[splashLayout] = l
		},
		func(d *catalog.Document) {
			l := d.Layouts[splashLayout]
			l.Speculation.MaxDraftTokens = 8
			d.Layouts[splashLayout] = l
		},
	} {
		doc := splashCatalog(t)
		mutate(&doc)
		if err := doc.Validate(); err == nil {
			t.Fatal("invalid Splash composition accepted")
		}
	}
	raw, _ := os.ReadFile("../../catalog/guided-setup.json")
	raw = []byte(strings.Replace(string(raw), `"kind": "splash/v1",`, `"kind": "splash/v1", "gpu_layers": 99,`, 1))
	if _, err := catalog.Parse(raw); err == nil {
		t.Fatal("unknown Splash config field accepted")
	}
}
