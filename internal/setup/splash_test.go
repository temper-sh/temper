package setup_test

import (
	"testing"

	"github.com/temper-sh/temper/internal/setup"
)

func TestSplash17PlanningDoesNotBudgetAnObsoleteWeightCache(t *testing.T) {
	doc := catalogDocument(t)
	const id = "qwen3.8-27b-q4xl-splash"
	legacy, err := setup.Assess(selectedLock(t, doc, id), facts(32))
	if err != nil || legacy.RuntimeDiskEstimateBytes == 0 {
		t.Fatal("legacy first-start cache estimate lost", err)
	}
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
	}
	doc.Presets[id] = p
	current, err := setup.Assess(selectedLock(t, doc, id), facts(32))
	if err != nil || current.RuntimeDiskEstimateBytes != 0 || current.ModelBytes != legacy.ModelBytes {
		t.Fatal("current planning changed weights or retained obsolete cache", current, err)
	}
}
