package setup

import (
	"fmt"
	"reflect"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/software"
)

// BuildPreset compiles one catalog preset with the profile this Mac's memory
// selects and the user's explicit choices.
func BuildPreset(d catalog.Document, origin string, memoryGiB int, choices Choices) (catalog.Lock, error) {
	lock, err := catalog.CompilePreset(d, origin, memoryGiB, choices.ContextWindow, software.Target{OS: "darwin", Arch: "arm64"})
	if err != nil || choices.Compute == "" {
		return lock, err
	}
	p := lock.Records.Presets[origin]
	return catalog.ConfigureExecution(lock, origin, catalog.ExecutionSettings{
		ContextWindowTokens: p.Window(), MaxOutputTokens: p.RequestDefaults.MaxOutputTokens, Compute: choices.Compute})
}

// WithNewestSoftware keeps a saved preset's models and settings and takes the
// router and its engine record from the newest catalog. An engine that left
// the catalog keeps its saved record.
func WithNewestSoftware(saved catalog.Lock, newest catalog.Document) catalog.Document {
	d := saved.Records
	d.Engines = map[string]catalog.Engine{}
	for id, e := range saved.Records.Engines {
		d.Engines[id] = e
	}
	d.Runtime.Router = newest.Runtime.Router
	p := d.Presets[saved.Preset]
	if e, ok := newest.Engines[p.Engine]; ok {
		d.Engines[p.Engine] = e
	}
	return d
}

// Changes says what differs between two builds of one preset, old → new, in
// the words a user recognizes. No lines means nothing that runs changed.
func Changes(old, next catalog.Lock) []string {
	var lines []string
	add := func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }
	version := func(s catalog.Supply) string {
		if s.Release != nil {
			return s.Release.Version
		}
		return "Python closure"
	}
	if a, b := version(old.Records.Runtime.Router.Supply), version(next.Records.Runtime.Router.Supply); a != b {
		add("router %s %s → %s", old.Records.Runtime.Router.Package, a, b)
	}
	po, pn := old.Records.Presets[old.Preset], next.Records.Presets[next.Preset]
	eo, en := old.Records.Engines[po.Engine], next.Records.Engines[pn.Engine]
	if po.Engine != pn.Engine {
		add("engine %s → %s", po.Engine, pn.Engine)
	}
	if a, b := version(eo.Supply), version(en.Supply); a != b {
		add("engine %s %s → %s", pn.Engine, a, b)
	}
	material := func(label, a, b string) {
		ao, an := old.Records.Artifacts[a], next.Records.Artifacts[b]
		switch {
		case a != b:
			add("%s %s → %s", label, nameOr(a, "none"), nameOr(b, "none"))
		case a != "" && !reflect.DeepEqual(ao.Files, an.Files):
			add("%s %s: new files", label, a)
		}
	}
	material("model", po.Artifact, pn.Artifact)
	material("draft", po.Speculation.DraftArtifact, pn.Speculation.DraftArtifact)
	material("projector", po.ProjectorArtifact, pn.ProjectorArtifact)
	to, tn := old.Records.Templates[po.Template], next.Records.Templates[pn.Template]
	if po.TemplateID() != pn.TemplateID() {
		add("template %s → %s", po.TemplateID(), pn.TemplateID())
	} else if !reflect.DeepEqual(to.Files, tn.Files) || to.Transform != tn.Transform {
		add("template %s: new version", pn.TemplateID())
	}
	if po.Window() != pn.Window() {
		add("context %d → %d tokens", po.Window(), pn.Window())
	}
	if po.Compute != pn.Compute {
		add("compute %s → %s", po.Compute, pn.Compute)
	}
	if len(lines) == 0 && !catalog.SameExecution(old, next) {
		add("settings changed")
	}
	return lines
}

func nameOr(id, fallback string) string {
	if id == "" {
		return fallback
	}
	return id
}
