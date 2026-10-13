package catalog

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// View is what one Temper can use from a catalog: the available records only,
// and why each other record is unavailable.
type View struct {
	Document    Document
	Unavailable []Unavailable
}

// Unavailable names one record this Temper cannot use and why. For a preset
// it keeps what the tolerant presentation could read, so it can be shown.
type Unavailable struct {
	Kind         string             `json:"kind"`
	ID           string             `json:"id"`
	Reason       string             `json:"reason"`
	Presentation PresetPresentation `json:"-"`
}

// Read is every client's reader. A record it cannot fully read or use is
// unavailable with a reason, and so is every preset that depends on it. Only
// a problem shared by every preset (header, platform, router) is an error.
func Read(data []byte, client string) (View, error) {
	var raw struct {
		Schema       string               `yaml:"schema"`
		Date         string               `yaml:"date"`
		Platform     string               `yaml:"platform"`
		Runtime      yaml.Node            `yaml:"runtime"`
		Artifacts    map[string]yaml.Node `yaml:"artifacts"`
		Templates    map[string]yaml.Node `yaml:"templates"`
		Engines      map[string]yaml.Node `yaml:"engines"`
		Presets      map[string]yaml.Node `yaml:"presets"`
		Presentation *Presentation        `yaml:"presentation"`
	}
	// Containers ignore unknown keys; records below are strict.
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&raw); err != nil {
		return View{}, fmt.Errorf("decode catalog: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return View{}, errors.New("expected exactly one catalog document")
	}
	if raw.Schema != Schema {
		return View{}, fmt.Errorf("catalog schema is %q; this Temper reads %s", raw.Schema, Schema)
	}
	if _, err := time.Parse("2006-01-02", raw.Date); err != nil {
		return View{}, errors.New("catalog date must be YYYY-MM-DD")
	}
	if raw.Platform != Platform {
		return View{}, fmt.Errorf("catalog is for platform %q; this Temper reads %s catalogs", raw.Platform, Platform)
	}
	var runtime struct {
		Router yaml.Node `yaml:"router"`
	}
	if err := raw.Runtime.Decode(&runtime); err != nil {
		return View{}, fmt.Errorf("decode catalog runtime: %w", err)
	}
	var router Router
	if err := decodeNode(&runtime.Router, &router); err != nil {
		return View{}, fmt.Errorf("catalog router needs a newer Temper: %w", err)
	}
	if reason, err := needsTemper(router.MinimumTemper, client); err != nil || reason != "" {
		return View{}, fmt.Errorf("this catalog %s", orError(reason, err))
	}
	if err := validateRouter(router); err != nil {
		return View{}, fmt.Errorf("catalog router: %w", err)
	}
	d := Document{Schema: raw.Schema, Date: raw.Date, Platform: raw.Platform, Runtime: Runtime{Router: router},
		Artifacts: map[string]Artifact{}, Templates: map[string]Template{}, Engines: map[string]Engine{}, Presets: map[string]Preset{},
		Presentation: raw.Presentation}
	var view View
	unavailable := func(kind, id, reason string) {
		view.Unavailable = append(view.Unavailable, Unavailable{Kind: kind, ID: id, Reason: reason})
	}
	for id, node := range raw.Artifacts {
		var a Artifact
		if err := decodeNode(&node, &a); err != nil {
			unavailable("artifact", id, newerTemper(err))
			continue
		}
		a.Sources = usableSources(a.Sources)
		if err := validateArtifact(id, a); err != nil {
			unavailable("artifact", id, err.Error())
			continue
		}
		d.Artifacts[id] = a
	}
	for id, node := range raw.Templates {
		var t Template
		if err := decodeNode(&node, &t); err != nil {
			unavailable("template", id, newerTemper(err))
			continue
		}
		t.Sources = usableSources(t.Sources)
		if err := validateTemplate(id, t); err != nil {
			unavailable("template", id, err.Error())
			continue
		}
		d.Templates[id] = t
	}
	for id, node := range raw.Engines {
		var e Engine
		if err := decodeNode(&node, &e); err != nil {
			unavailable("engine", id, newerTemper(err))
			continue
		}
		if reason, err := needsTemper(e.MinimumTemper, client); err != nil || reason != "" {
			unavailable("engine", id, orError(reason, err))
			continue
		}
		e = e.knownCapabilities()
		if err := validateEngine(id, e); err != nil {
			unavailable("engine", id, err.Error())
			continue
		}
		d.Engines[id] = e
	}
	missing := func(kind string, ids map[string]bool, id string) string {
		if id == "" || ids[id] {
			return ""
		}
		for _, u := range view.Unavailable {
			if u.Kind == kind && u.ID == id {
				return fmt.Sprintf("its %s %q is unavailable: %s", kind, id, u.Reason)
			}
		}
		return ""
	}
	for id, node := range raw.Presets {
		var shown struct {
			Presentation PresetPresentation `yaml:"presentation"`
		}
		_ = node.Decode(&shown)
		unavailable := func(kind, id, reason string) {
			view.Unavailable = append(view.Unavailable, Unavailable{Kind: kind, ID: id, Reason: reason, Presentation: shown.Presentation})
		}
		var p Preset
		if err := decodeNode(&node, &p); err != nil {
			unavailable("preset", id, newerTemper(err))
			continue
		}
		var profiles []Profile
		for _, profile := range p.Profiles {
			if profile.unreadable == "" {
				profiles = append(profiles, profile)
			}
		}
		if len(p.Profiles) > 0 && len(profiles) == 0 {
			unavailable("preset", id, "no profile this Temper can read; needs a newer Temper")
			continue
		}
		p.Profiles = profiles
		if p.Speculation == (Speculation{}) {
			p.Speculation = Speculation{Method: "none", Source: "none"}
		}
		reason := ""
		for _, dependency := range []struct{ kind, id string }{{"artifact", p.Artifact}, {"artifact", p.ProjectorArtifact}, {"artifact", p.Speculation.DraftArtifact}, {"engine", p.Engine}, {"template", p.Template}} {
			present := map[string]bool{}
			switch dependency.kind {
			case "artifact":
				for k := range d.Artifacts {
					present[k] = true
				}
			case "engine":
				for k := range d.Engines {
					present[k] = true
				}
			case "template":
				for k := range d.Templates {
					present[k] = true
				}
			}
			if reason = missing(dependency.kind, present, dependency.id); reason != "" {
				break
			}
		}
		if reason == "" {
			if err := d.validatePreset(id, p); err != nil {
				reason = err.Error()
			}
		}
		if reason != "" {
			unavailable("preset", id, reason)
			continue
		}
		d.Presets[id] = p
	}
	slices.SortFunc(view.Unavailable, func(a, b Unavailable) int {
		if order := strings.Compare(a.Kind, b.Kind); order != 0 {
			return order
		}
		return strings.Compare(a.ID, b.ID)
	})
	view.Document = d
	return view, nil
}

func newerTemper(err error) string {
	return "needs a newer Temper: " + err.Error()
}

func orError(reason string, err error) string {
	if err != nil {
		return err.Error()
	}
	return reason
}
