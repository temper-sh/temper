package catalog

import (
	"slices"
	"strings"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter/upstreamrelease"
)

// SameExecution reports whether two locks start the same material with the
// same settings. One running Temper computes it fresh on both sides from
// explicit field lists; it is never stored or printed. Presentation, version
// policy, sources, record IDs and choice policy cannot change it.
func SameExecution(a, b Lock) bool { return executionKey(a) == executionKey(b) }

type materialKey struct {
	Format string
	Files  []File
}

type templateKey struct {
	Files     []File
	Transform string
}

type supplyKey struct {
	Package string
	Target  software.Target
	Release *upstreamrelease.Release
	Python  *PythonSupply
}

type speculationKey struct {
	Method         string
	Source         string
	MaxDraftTokens int
	Draft          *materialKey
}

func executionKey(l Lock) string {
	d := l.Records
	p := d.Presets[l.Preset]
	artifact := func(id string) *materialKey {
		if id == "" {
			return nil
		}
		a := d.Artifacts[id]
		return &materialKey{Format: a.Format, Files: sortedFiles(a.Files)}
	}
	supply := func(s Supply) supplyKey {
		// Release identity is the version, commit and archive bytes; the
		// download URL says where it comes from, not what runs.
		release := s.Release
		if release != nil {
			copied := *release
			copied.Artifact.Locator = ""
			release = &copied
		}
		return supplyKey{Package: s.Package, Target: s.Target, Release: release, Python: s.Python}
	}
	var template *templateKey
	if p.patched() {
		t := d.Templates[p.Template]
		template = &templateKey{Files: sortedFiles(t.Files), Transform: t.Transform}
	}
	config := p.Config()
	config.GPULayers = p.gpuOffloadLayers(config)
	e := d.Engines[p.Engine]
	return digest(struct {
		Target        software.Target
		Router        supplyKey
		EngineAdapter string
		Engine        supplyKey
		Artifact      *materialKey
		Projector     *materialKey
		Template      *templateKey
		Interface     string
		Modalities    []string
		Compute       ComputeDevice
		Window        int
		Request       RequestDefaults
		Speculation   speculationKey
		EngineConfig  EngineConfig
	}{
		Target: l.Target, Router: supply(d.Runtime.Router.Supply), EngineAdapter: e.Adapter, Engine: supply(e.Supply),
		Artifact: artifact(p.Artifact), Projector: artifact(p.ProjectorArtifact), Template: template,
		Interface: p.Interface, Modalities: p.Modalities, Compute: p.Compute, Window: p.Window(),
		Request: p.RequestDefaults, EngineConfig: config,
		Speculation: speculationKey{Method: p.Speculation.Method, Source: p.Speculation.Source,
			MaxDraftTokens: p.Speculation.MaxDraftTokens, Draft: artifact(p.Speculation.DraftArtifact)},
	})
}

func sortedFiles(files []File) []File {
	result := slices.Clone(files)
	slices.SortFunc(result, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	return result
}
