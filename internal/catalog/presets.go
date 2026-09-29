package catalog

import (
	"errors"
	"fmt"
	"strings"

	"github.com/temper-sh/temper/internal/software"
)

const PresetCatalogSchema = "temper-catalog/v3"

// Preset is the current name for the exact configuration carried by issued
// Layout records. Keep the old wire contract inside execution locks.
type Preset = Layout

// PresetCatalog is the authoring surface. User compositions are not catalog
// records: they live with the user's selected exact locks.
type PresetCatalog struct {
	Schema      string              `yaml:"schema" json:"schema"`
	Date        string              `yaml:"date" json:"date"`
	Runtime     Runtime             `yaml:"runtime" json:"runtime"`
	Artifacts   map[string]Artifact `yaml:"artifacts" json:"artifacts"`
	Patches     map[string]Patch    `yaml:"patches" json:"patches"`
	Engines     map[string]Engine   `yaml:"engines" json:"engines"`
	Presets     map[string]Preset   `yaml:"presets" json:"presets"`
	PresetOrder []string            `yaml:"preset_order,omitempty" json:"preset_order,omitempty"`
}

func parsePresets(data []byte) (Document, error) {
	var source PresetCatalog
	if err := decode(data, &source); err != nil {
		return Document{}, err
	}
	d := Document{Schema: Schema, Date: source.Date, Runtime: source.Runtime,
		Artifacts: source.Artifacts, Patches: source.Patches, Engines: source.Engines,
		Layouts: source.Presets, LayoutOrder: source.PresetOrder, Profiles: map[string]Profile{}}
	for id := range d.Layouts {
		d.Profiles[id] = presetProfile(id)
	}
	return d, d.Validate()
}

func presetProfile(id string) Profile {
	// The compatibility projection describes one demand-loadable configuration.
	// User layouts own preload and coexistence; no preset reserves a historical
	// profile-wide fraction of the device simply by being selected.
	return Profile{Foreground: "external", GPUMemoryUtilization: .85, Bindings: []Binding{{Layout: id, Route: "available", Residency: "on-demand", IdleTTLSeconds: 1800}}}
}

// Authoring projects normalized inputs back into the current public vocabulary.
func Authoring(d Document) PresetCatalog {
	return PresetCatalog{Schema: PresetCatalogSchema, Date: d.Date, Runtime: d.Runtime,
		Artifacts: d.Artifacts, Patches: d.Patches, Engines: d.Engines, Presets: d.Layouts, PresetOrder: d.LayoutOrder}
}

func PresetSelection(d Document, id, template string, window int) (Document, Selection, error) {
	if _, ok := d.Layouts[id]; !ok {
		return Document{}, Selection{}, fmt.Errorf("unknown preset %q", id)
	}
	d = canonicalDocument(d)
	d.Profiles = map[string]Profile{id: presetProfile(id)}
	s := Selection{Schema: SelectionSchema, Profile: id, Templates: map[string]string{}, ContextWindows: map[string]int{}}
	if template != "" {
		if template == "builtin" {
			template = ""
		}
		s.Templates[id] = template
	}
	if window > 0 {
		s.ContextWindows[id] = window
	}
	return d, s, nil
}

func CompilePreset(d Document, id, template string, window int, target software.Target) (Lock, error) {
	d, s, err := PresetSelection(d, id, template, window)
	if err != nil {
		return Lock{}, err
	}
	return Compile(d, s, target)
}

func DescribePreset(d Document, id, description string, assessmentURL *string, ifEmpty bool) (Document, error) {
	if err := d.Validate(); err != nil {
		return Document{}, err
	}
	p, ok := d.Layouts[id]
	if !ok {
		return Document{}, fmt.Errorf("unknown preset %q", id)
	}
	if ifEmpty && strings.TrimSpace(p.Description) != "" {
		return d, nil
	}
	p.Description = description
	if assessmentURL != nil {
		p.AssessmentURL = *assessmentURL
	}
	if p.Recommended && strings.TrimSpace(description) == "" {
		return Document{}, errors.New("recommended presets require a manually authored description")
	}
	d = canonicalDocument(d)
	d.Layouts[id] = p
	return d, d.Validate()
}
