package catalog_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/machine"
)

func measuredDocument(t *testing.T) catalog.Document {
	t.Helper()
	d := document()
	p := d.Presets["qwen-32k"]
	loaded := int64(3 << 30)
	p.Profiles[0].Memory = &catalog.MemoryMeasurement{
		ObservedOn: "2026-10-20", Source: "https://example.com/results", Workload: "Short fixture workload",
		Counter: "engine-rss", LoadedBytes: &loaded, PeakBytes: 4 << 30, Limitations: "Not a full-window measurement.",
		Machine:   catalog.MemoryMachine{Chip: "Apple M5", PhysicalMemoryBytes: 32 << 30, MetalMemoryMiB: 24 * 1024, OS: "macOS 26.6"},
		Execution: compile(t, d).MemoryExecution(),
	}
	d.Presets["qwen-32k"] = p
	return d
}

func TestMemoryObservationSurvivesCatalogAndLockWithoutChangingExecution(t *testing.T) {
	before := compile(t, document())
	data, err := json.Marshal(measuredDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	d, err := catalog.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	after := compile(t, d)
	if !catalog.SameExecution(before, after) {
		t.Fatal("measurement changed executable identity")
	}
	raw, err := catalog.MarshalLock(after)
	if err != nil {
		t.Fatal(err)
	}
	again, err := catalog.ParseLock(raw)
	if err != nil || again.Records.Presets[again.Preset].Profiles[0].Memory.PeakBytes != 4<<30 {
		t.Fatal("measurement lost from saved lock", err)
	}
	// Authoring a measurement is not allowed to mutate its tested inputs later.
	p := after.Records.Presets[after.Preset]
	p.Profiles[0].EngineConfig.KVCache = "q4"
	if p.Profiles[0].Memory.Execution.EngineConfig.KVCache != "q8" {
		t.Fatal("measurement shares mutable configuration")
	}
}

func TestMeasurementSnapshotDoesNotFollowMutableSplashInputs(t *testing.T) {
	l := compileSplash(t, splashCatalog(t))
	before := l.MemoryExecution()
	p := l.Records.Presets[l.Preset]
	oldCap := p.Profiles[0].EngineConfig.Splash.MaxMemoryBytes
	p.Profiles[0].EngineConfig.Splash.MaxMemoryBytes = oldCap / 2
	a := l.Records.Artifacts[p.Artifact]
	oldSHA := a.Files[0].SHA256
	a.Files[0].SHA256 = strings.Repeat("f", 64)
	if before.EngineConfig.Splash.MaxMemoryBytes != oldCap || before.Weights[0].SHA256 != oldSHA {
		t.Fatal("changing execution rewrote its retained observation")
	}
	m := catalog.MemoryMeasurement{Execution: before}
	if ok, _ := m.Applicable(l, machine.Facts{}); ok {
		t.Fatal("changed cap and weights still matched")
	}
}

func TestMissingLoadedMeasurementRemainsNull(t *testing.T) {
	d := measuredDocument(t)
	d.Presets["qwen-32k"].Profiles[0].Memory.LoadedBytes = nil
	raw, err := json.Marshal(d)
	if err != nil || !strings.Contains(string(raw), `"loaded_bytes":null`) {
		t.Fatal("missing sample was omitted or changed to zero", err)
	}
	parsed, err := catalog.Parse(raw)
	if err != nil || parsed.Presets["qwen-32k"].Profiles[0].Memory.LoadedBytes != nil {
		t.Fatal("missing sample did not survive the catalog", err)
	}
}

func TestInvalidMeasurementDoesNotBecomeAnAdmissionFact(t *testing.T) {
	for _, scenario := range []string{"negative peak", "loaded above peak", "unknown counter", "missing provenance", "bad engine identity", "zero capacity"} {
		t.Run(scenario, func(t *testing.T) {
			d := measuredDocument(t)
			m := d.Presets["qwen-32k"].Profiles[0].Memory
			switch scenario {
			case "negative peak":
				m.PeakBytes = -1
			case "loaded above peak":
				*m.LoadedBytes = m.PeakBytes + 1
			case "unknown counter":
				m.Counter = "file-size"
			case "missing provenance":
				m.Source = ""
			case "bad engine identity":
				m.Execution.EngineFiles = []string{strings.Repeat("x", 64)}
			case "zero capacity":
				m.Machine.PhysicalMemoryBytes = 0
			}
			raw, _ := json.Marshal(d)
			if _, err := catalog.Parse(raw); err == nil {
				t.Fatal("invalid observation accepted")
			}
		})
	}
}
