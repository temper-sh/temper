package managed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/splash"
	"github.com/temper-sh/temper/internal/testfixture"
)

func TestSplash17ManagedJobRetainsExactRelaunchAndFrontendAcrossSerialization(t *testing.T) {
	const id = "qwen3.8-27b-q4xl-splash"
	doc := testfixture.MixedEngineCatalog(t)
	p := doc.Presets[id]
	e := doc.Engines[p.Engine]
	e.Adapter = "splash/v2"
	e.Supply.Release.Version = "1.7.0"
	e.Supply.Release.Artifact.Locator = "https://github.com/incoai/splash/releases/download/1.7.0/splash-1.7.0-arm64-macos26.tar.gz"
	e.Supply.Release.Artifact.ArchiveRoot = "splash-1.7.0-arm64-macos26"
	doc.Engines[p.Engine] = e
	for i := range p.Profiles {
		p.Profiles[i].EngineConfig.Kind = "splash/v2"
		p.Profiles[i].EngineConfig.Splash.Kind = "splash/v2"
		p.Profiles[i].Memory = nil
	}
	doc.Presets[id] = p
	// Only small fixture bytes are prepared; the subprocess below stands in for
	// the public preparation boundary and does not reproduce its private format.
	contents := map[string][]byte{}
	file := func(prefix string, f catalog.File) catalog.File {
		data := []byte(prefix + f.Path)
		contents[prefix+f.Path] = data
		sum := sha256.Sum256(data)
		f.SHA256, f.Bytes = hex.EncodeToString(sum[:]), int64(len(data))
		return f
	}
	for artifact, prefix := range map[string]string{p.Artifact: "model/", p.Speculation.DraftArtifact: "draft/"} {
		a := doc.Artifacts[artifact]
		for i := range a.Files {
			a.Files[i] = file(prefix, a.Files[i])
		}
		doc.Artifacts[artifact] = a
	}
	template := doc.Templates[p.Template]
	template.Files[0] = file("patches/"+p.Template+"/", template.Files[0])
	doc.Templates[p.Template] = template
	lock, err := catalog.CompilePreset(doc, id, 0, 32768, doc.Runtime.Router.Target)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := lock.Projections()
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	testfixture.MaterializeLayout(t, root, projection.Manifest, projection.Artifacts, id, contents)
	material, err := splash.New(root, id, projection.Manifest.Layouts[id], projection.Artifacts.Entries[id], projection.Manifest.Patches)
	if err != nil {
		t.Fatal(err)
	}
	python := filepath.Join(t.TempDir(), "prepare")
	if err := os.WriteFile(python, []byte("#!/bin/sh\nset -eu\n"+`mkdir -p "$7/.resolved/selected"
printf 'metadata\n' > "$7/.resolved/selected/config.json"
printf '"%s/.resolved/selected"\n' "$7"
`), 0700); err != nil {
		t.Fatal(err)
	}
	if err := material.Prepare(context.Background(), python, nil); err != nil {
		t.Fatal(err)
	}
	model, err := material.ServerPath()
	if err != nil {
		t.Fatal(err)
	}
	c := setup.EmptyConfiguration()
	c.Presets[id] = setup.Preset{Name: "Splash fixture", Lock: lock}
	c.Layouts = setup.StarterLayouts([]string{id})
	rendered, err := Render(root, "127.0.0.1:18080", c, setup.LayoutPlan{ID: "default", Layout: c.Layouts["default"], AllFit: true}, func(_ catalog.Lock, pkg, relative string) (string, error) {
		return "/software/" + pkg + "/" + relative, nil
	}, fixtureLauncher)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(rendered.Job)
	if err != nil {
		t.Fatal(err)
	}
	var job Job
	if err := json.Unmarshal(raw, &job); err != nil {
		t.Fatal(err)
	}
	if err := job.Validate(root); err != nil {
		t.Fatal(err)
	}
	if job.digest(rendered.Config) != job.Generation || len(job.Processes) != 2 {
		t.Fatal("serialized job changed its generation or process closure")
	}
	frontend, native := job.Processes[0], job.Processes[1]
	want := append(slices.Clone(native.Arguments), "--relaunch-context", "32768")
	if frontend.Role != id || frontend.Record == "" || !slices.Contains(frontend.Arguments, model) || native.Parent != id || native.Role != id+"/engine" || native.Arguments[1] != model || !slices.Equal(native.RelaunchArguments, want) {
		t.Fatal("managed job lost the selected path, relaunch or direct frontend", job.Processes)
	}
	commands := job.commands()
	if len(commands) != 3 || commands[2].ID != native.Role || commands[2].Parent != id || !slices.Equal(commands[2].RelaunchArguments, want) {
		t.Fatal("managed observation/recovery lost the serialized selection", commands)
	}
	for name, change := range map[string]func(*Process){
		"context": func(p *Process) { p.RelaunchArguments[len(p.RelaunchArguments)-1] = "65536" },
		"parent":  func(p *Process) { p.Parent = "other" },
		"nul":     func(p *Process) { p.RelaunchArguments[1] += "\x00" },
	} {
		t.Run(name, func(t *testing.T) {
			var altered Job
			if err := json.Unmarshal(raw, &altered); err != nil {
				t.Fatal(err)
			}
			change(&altered.Processes[1])
			if altered.digest(rendered.Config) == job.Generation || altered.Validate(root) == nil {
				t.Fatal("altered relaunch metadata retained authority")
			}
		})
	}
}
