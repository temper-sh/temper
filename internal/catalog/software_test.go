package catalog_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/software"
)

func TestVersionExclusionsAreAUnionScopedToTheSelectedPreset(t *testing.T) {
	d := document()
	l := d.Presets["qwen-32k"]
	l.ExcludeEngineVersions = []catalog.VersionExclusion{{Range: "< b10900", Reason: "needs the fixture feature"}}
	d.Presets["qwen-32k"] = l
	// The recorded release is allowed; exclusions are policy, not what runs.
	if !catalog.SameExecution(compile(t, d), compile(t, document())) {
		t.Fatal("version policy changed what runs")
	}
	for name, change := range map[string]func(*catalog.Document){
		"preset": func(d *catalog.Document) {
			p := d.Presets["qwen-32k"]
			p.ExcludeEngineVersions = []catalog.VersionExclusion{{Range: "< b10937", Reason: "needs the fixture feature"}}
			d.Presets["qwen-32k"] = p
		},
		"engine": func(d *catalog.Document) {
			e := d.Engines["llama-b10936"]
			e.ExcludeVersions = []catalog.VersionExclusion{{Range: ">= b10930, < b10940", Reason: "needs the fixture feature"}}
			d.Engines["llama-b10936"] = e
		},
		"router": func(d *catalog.Document) {
			d.Runtime.Router.ExcludeVersions = []catalog.VersionExclusion{{Range: "= b10936", Reason: "needs the fixture feature"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := document()
			change(&d)
			if _, err := catalog.CompilePreset(d, "qwen-32k", 0, 0, software.Target{OS: "darwin", Arch: "arm64"}); err == nil || !strings.Contains(err.Error(), "needs the fixture feature") {
				t.Fatalf("excluded recorded release = %v", err)
			}
		})
	}
	// Through the client reader an unselected preset's exclusion hides only
	// that preset.
	future := document()
	p := future.Presets["qwen-32k"]
	p.ExcludeEngineVersions = []catalog.VersionExclusion{{Range: "< b20000"}}
	future.Presets["future-model"] = p
	future.Presentation.PresetOrder = append(future.Presentation.PresetOrder, "future-model")
	raw, err := json.Marshal(future)
	if err != nil {
		t.Fatal(err)
	}
	view, err := catalog.Read(raw, "0.1.0")
	if err != nil || len(view.Unavailable) != 1 || view.Unavailable[0].ID != "future-model" {
		t.Fatalf("view = %+v, %v", view.Unavailable, err)
	}
	compile(t, view.Document)
}

func TestVersionRangeGrammar(t *testing.T) {
	d := document()
	for _, tc := range []struct {
		rule  string
		valid bool
	}{
		{"< b10937", true}, {"<= b10936", true}, {"> b10900", true}, {">= b10900, < b10950", true}, {"= b10935", true},
		{"b10936", false}, {"== b10936", false}, {"> b1, > b2", false}, {"= b1, < b2", false}, {"> b20, < b10", false},
		{"> b10, < b10", false}, {"< b1, >= b1, > b2", false}, {"< v1.2", false},
	} {
		t.Run(tc.rule, func(t *testing.T) {
			p := d.Presets["qwen-32k"]
			p.ExcludeEngineVersions = []catalog.VersionExclusion{{Range: tc.rule}}
			d.Presets["qwen-32k"] = p
			err := d.Validate()
			if tc.valid && err != nil && !strings.Contains(err.Error(), "excluded") || !tc.valid && err == nil {
				t.Fatalf("Validate() = %v, valid %v", err, tc.valid)
			}
		})
	}
}

func TestSoftwareChoiceIsStableOrLatest(t *testing.T) {
	d := document()
	resolved, used, err := catalog.ResolveSoftware(context.Background(), d, "qwen-32k", catalog.SoftwareStable, nil, false)
	if err != nil || len(used) != 0 {
		t.Fatal(err, used)
	} // Exact retained inputs need no network.
	compile(t, resolved)
	for _, retired := range []string{"recorded", "tested", ""} {
		if _, _, err := catalog.ResolveSoftware(context.Background(), d, "qwen-32k", retired, nil, true); err == nil {
			t.Fatalf("accepted software choice %q", retired)
		}
	}
}

// failingReleases counts lookups and fails each one.
type failingReleases struct{ opened *int }

func (r failingReleases) Open(context.Context, string) (io.ReadCloser, error) {
	*r.opened++
	return nil, errors.New("upstream offline")
}

func TestLatestFallsBackPerItemVisiblyAndToolsStayStrict(t *testing.T) {
	d := document()
	opened := 0
	latest := &catalog.LatestReleases{Reader: failingReleases{&opened}}
	if _, _, err := catalog.ResolveSoftware(context.Background(), d, "qwen-32k", catalog.SoftwareLatest, latest, false); err == nil || !strings.Contains(err.Error(), "upstream offline") {
		t.Fatalf("a tool quietly used the stable release: %v", err)
	}
	resolved, used, err := catalog.ResolveSoftware(context.Background(), d, "qwen-32k", catalog.SoftwareLatest, latest, true)
	if err != nil {
		t.Fatal(err)
	}
	if !catalog.SameExecution(compile(t, resolved), compile(t, d)) {
		t.Fatal("fallback did not keep the stable releases")
	}
	if len(used) != 2 || used[0].Package != "llama-swap" || used[1].Package != "llama-cpp" {
		t.Fatalf("resolutions = %+v", used)
	}
	for _, r := range used {
		if r.Requested != catalog.SoftwareLatest || r.Used != "b10936" || !strings.Contains(r.Reason, "upstream offline") {
			t.Fatalf("fallback was not visible: %+v", r)
		}
	}
	// One lookup per source for the whole command run.
	before := opened
	if _, _, err := catalog.ResolveSoftware(context.Background(), d, "qwen-32k", catalog.SoftwareLatest, latest, true); err != nil || opened != before {
		t.Fatalf("source looked up again: %d → %d, %v", before, opened, err)
	}
}

func TestLockOmitsInstallerAuthoringAndRedundantIdentities(t *testing.T) {
	l := compile(t, document())
	raw, err := catalog.MarshalLock(l)
	if err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{`"tools"`, `"integrations"`, `"id":`, `"units":`, `"recipe_revision"`, `"experiment"`, `"materials"`} {
		if strings.Contains(string(raw), unwanted) {
			t.Errorf("lock contains %s", unwanted)
		}
	}
	var decoded map[string]json.RawMessage
	_ = json.Unmarshal(raw, &decoded)
	if _, ok := decoded["digests"]; ok {
		t.Fatal("redundant digest map")
	}
	for _, stored := range []string{"execution_digest", "source_snapshot_sha256"} {
		if _, ok := decoded[stored]; ok {
			t.Fatalf("lock stores fingerprint %s", stored)
		}
	}
	p, err := l.Projections()
	if err != nil {
		t.Fatal(err)
	}
	if p.Software.Provenance.Experiment != nil || p.Software.Provenance.Execution == nil {
		t.Fatal("catalog installation fabricated experiment provenance")
	}
	if p.Software.Resolved != "" {
		t.Fatal("catalog date was presented as a resolution observation")
	}
}
