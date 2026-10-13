package catalog_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
)

func TestLicenseLinksUseTheDeclaredLicenseAndRetainCustomSources(t *testing.T) {
	for _, tc := range []struct{ license, want string }{
		{"Apache-2.0", "https://choosealicense.com/licenses/apache-2.0/"},
		{"Gemma", "https://ai.google.dev/gemma/terms"},
		{"CC-BY-NC-4.0", "https://creativecommons.org/licenses/by-nc/4.0/"},
		{"LicenseRef-Custom", "https://huggingface.co/example/Model/blob/" + strings.Repeat("a", 40) + "/README.md"},
	} {
		t.Run(tc.license, func(t *testing.T) {
			a := catalog.Artifact{Sources: []catalog.Source{{Hub: catalog.HubHuggingFace, Repo: "example/Model", Revision: strings.Repeat("a", 40)}}, License: tc.license}
			if got := a.LicenseURL(); got != tc.want {
				t.Fatalf("license link = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCatalogRefusesTerminalControlsInVisibleLicenses(t *testing.T) {
	for _, license := range []string{"Apache-2.0\x1b[31m", "Gemma\x00", string([]byte{0xff})} {
		d := document()
		a := d.Artifacts["qwen-q4"]
		a.License = license
		d.Artifacts["qwen-q4"] = a
		if err := d.Validate(); err == nil || !strings.Contains(err.Error(), "license must be plain text") {
			t.Fatalf("accepted unsafe license %q: %v", license, err)
		}
	}
}

// specialistDocument places the fixture preset under a group with sections.
func specialistDocument() catalog.Document {
	d := document()
	d.Presentation.Groups = append(d.Presentation.Groups, catalog.Group{ID: "specialist", DisplayName: "Specialist", Description: "Specialists by task.",
		Sections: []catalog.Section{{ID: "extraction", DisplayName: "Extraction", Description: "Turn source text into structured records."}}})
	p := d.Presets["qwen-32k"]
	p.Presentation.Group, p.Presentation.Section = "specialist", "extraction"
	d.Presets["qwen-32k"] = p
	return d
}

func setPresentation(d *catalog.Document, edit func(*catalog.PresetPresentation)) {
	p := d.Presets["qwen-32k"]
	edit(&p.Presentation)
	d.Presets["qwen-32k"] = p
}

func TestPublisherRequiresCompleteGroupsAndPlacement(t *testing.T) {
	if err := specialistDocument().Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*catalog.Document){
		"unknown group": func(d *catalog.Document) {
			setPresentation(d, func(p *catalog.PresetPresentation) { p.Group = "missing" })
		},
		"section of another group": func(d *catalog.Document) {
			setPresentation(d, func(p *catalog.PresetPresentation) { p.Group = "other" })
		},
		"missing section": func(d *catalog.Document) { setPresentation(d, func(p *catalog.PresetPresentation) { p.Section = "" }) },
		"missing description": func(d *catalog.Document) {
			setPresentation(d, func(p *catalog.PresetPresentation) { p.Description = " \n " })
		},
		"missing display name": func(d *catalog.Document) {
			setPresentation(d, func(p *catalog.PresetPresentation) { p.DisplayName = "" })
		},
		"group without description": func(d *catalog.Document) { d.Presentation.Groups[1].Description = "" },
		"repeated group": func(d *catalog.Document) {
			d.Presentation.Groups = append(d.Presentation.Groups, d.Presentation.Groups[0])
		},
		"unknown fallback group": func(d *catalog.Document) { d.Presentation.FallbackGroup = "missing" },
		"incomplete order":       func(d *catalog.Document) { d.Presentation.PresetOrder = nil },
		"repeated order":         func(d *catalog.Document) { d.Presentation.PresetOrder = []string{"qwen-32k", "qwen-32k"} },
		"no presentation":        func(d *catalog.Document) { d.Presentation = nil },
		"chat preset without template": func(d *catalog.Document) {
			p := d.Presets["qwen-32k"]
			p.Template = ""
			d.Presets["qwen-32k"] = p
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := specialistDocument()
			change(&d)
			if err := d.Validate(); err == nil {
				t.Fatal("publisher accepted incomplete presentation")
			}
		})
	}
}

func TestReaderToleratesPresentationThePublisherRefuses(t *testing.T) {
	raw, err := json.Marshal(specialistDocument())
	if err != nil {
		t.Fatal(err)
	}
	var tree map[string]any
	if err := json.Unmarshal(raw, &tree); err != nil {
		t.Fatal(err)
	}
	preset := tree["presets"].(map[string]any)["qwen-32k"].(map[string]any)
	preset["presentation"] = map[string]any{"display_name": 7, "description": "Line one\x1b[31m red", "assessment_url": "javascript:alert(1)",
		"group": "future-group", "section": "future-section", "memory_tier": "XXXL", "future_badge": "new"}
	tree["presentation"].(map[string]any)["future_layout"] = true
	tree["future_records"] = map[string]any{"x": 1}
	raw, err = json.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Parse(raw); err == nil {
		t.Fatal("publisher accepted unknown keys and invalid presentation")
	}
	view, err := catalog.Read(raw, "0.1.0")
	if err != nil || len(view.Unavailable) != 0 {
		t.Fatalf("reader refused presentation: %v %+v", err, view.Unavailable)
	}
	p := view.Document.Presets["qwen-32k"].Presentation
	if p.DisplayName != "" || p.Description != "Line one[31m red" || p.AssessmentURL != "" || p.MemoryTier != "" {
		t.Fatalf("invalid values did not fall back: %+v", p)
	}
	group, section := view.Document.Placement(p)
	if group.ID != "other" || section != nil {
		t.Fatalf("unknown group or section did not fall back: %+v %+v", group, section)
	}
	if _, err := catalog.CompilePreset(view.Document, "qwen-32k", 0, 0, compile(t, document()).Target); err != nil {
		t.Fatal(err)
	}
}

func TestGroupsFallBackToABuiltInOther(t *testing.T) {
	d := document()
	d.Presentation = nil
	groups := d.Groups()
	if len(groups) != 1 || groups[0].ID != catalog.OtherGroup.ID {
		t.Fatalf("groups = %+v", groups)
	}
	if group, section := d.Placement(catalog.PresetPresentation{Group: "general"}); group.ID != catalog.OtherGroup.ID || section != nil {
		t.Fatalf("placement = %+v %+v", group, section)
	}
	d = specialistDocument()
	d.Presentation.Groups = d.Presentation.Groups[1:]
	if groups := d.Groups(); groups[len(groups)-1].ID != catalog.OtherGroup.ID {
		t.Fatalf("a missing fallback group did not add the built-in Other: %+v", groups)
	}
}

func TestPresentationChangesPreserveWhatRuns(t *testing.T) {
	d := specialistDocument()
	before := compile(t, d)
	a := d.Artifacts["qwen-q4"]
	a.Presentation.ModelName, a.Presentation.WeightsName = "Qwen", "Unsloth Q4 GGUF"
	d.Artifacts["qwen-q4"] = a
	e := d.Engines["llama-b10936"]
	e.Presentation.DisplayName = "llama.cpp"
	d.Engines["llama-b10936"] = e
	setPresentation(&d, func(p *catalog.PresetPresentation) {
		p.MemoryTier, p.Group, p.Section, p.Description = "S", "other", "", "Reworded."
	})
	d.Presentation.Groups[1].Description = "Reworded tab."
	after := compile(t, d)
	if !catalog.SameExecution(before, after) {
		t.Fatal("presentation changed what runs")
	}
	if reflect.DeepEqual(before, after) {
		t.Fatal("lock records did not keep the preset's presentation")
	}
	if after.Records.Presentation != nil {
		t.Fatal("execution lock retained catalog navigation")
	}
	raw, err := catalog.MarshalLock(after)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ParseLock(raw); err != nil {
		t.Fatal(err)
	}
}
