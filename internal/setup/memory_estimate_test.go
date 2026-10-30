package setup_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/modelmemory"
	"github.com/temper-sh/temper/internal/setup"
)

func attentionLock(t *testing.T, context int, compute catalog.ComputeDevice) catalog.Lock {
	t.Helper()
	d := catalogDocument(t)
	p := d.Presets[compactLocal]
	p.Compute, p.ComputeOptions = compute, []catalog.ComputeDevice{catalog.ComputeCPU, catalog.ComputeGPU}
	p.Speculation = catalog.Speculation{Method: "none", Source: "none"}
	p.Profiles = p.Profiles[:1]
	p.Profiles[0].ContextWindowTokens = context
	p.Profiles[0].EngineConfig.KVCache = "f16"
	p.Profiles[0].EngineConfig.PromptCacheRAMMiB = new(0)
	d.Presets[compactLocal] = p
	a := d.Artifacts[p.Artifact]
	a.Files[0].Bytes = 3 << 30
	a.ContextMemory = &modelmemory.Attention{Kind: "full-attention/v1", Architecture: "qwen3", Layers: 32, KVHeads: 8, KeyLength: 128, ValueLength: 128}
	d.Artifacts[p.Artifact] = a
	return selectedLock(t, d, compactLocal)
}

func TestContextChangesSharedPresetAndLayoutAdmission(t *testing.T) {
	for _, compute := range []catalog.ComputeDevice{catalog.ComputeCPU, catalog.ComputeGPU} {
		small := attentionLock(t, 4096, compute)
		large := attentionLock(t, 65536, compute)
		before, err := setup.Assess(small, facts(8))
		if err != nil || len(before.Refusals) != 0 || before.MemoryBytes != 3584<<20 || before.Memory.Status != "incomplete" {
			t.Fatalf("small context: %+v %v", before, err)
		}
		after, err := setup.Assess(large, facts(8))
		if err != nil || len(after.Refusals) == 0 || after.MemoryBytes != 11<<30 || after.Memory.Status != "exceeds-budget" {
			t.Fatalf("large context: %+v %v", after, err)
		}
		c := setup.EmptyConfiguration()
		c.Presets["coding"] = setup.Preset{Name: "Coding", Lock: large}
		c.Layouts["work"] = setup.Layout{Name: "Work", Presets: []string{"coding"}, Startup: []string{"coding"}, IdleSeconds: 60}
		p, err := setup.BuildConfiguration(t.TempDir(), facts(8), 100<<30, c, setup.Material{})
		if err != nil || p.CanPrepare || p.Layouts[0].StartupBytes != after.MemoryBytes || p.Layouts[0].AllFit || !p.Layouts[0].MemoryIncomplete {
			t.Fatal("layout lost corrected estimate", p.Layouts, err)
		}
		raw, _ := json.Marshal(p)
		if !strings.Contains(string(raw), `"kv_cache_bytes":8589934592`) || !strings.Contains(strings.Join(p.Lines(), "\n"), "KV/context cache: 8.00 GiB estimated payload") {
			t.Fatal("CLI/JSON lost the shared estimate", string(raw))
		}
	}
}

func TestGeometryIsArtifactMetadataAndSurvivesLockRoundTrip(t *testing.T) {
	with := attentionLock(t, 4096, catalog.ComputeGPU)
	raw, err := catalog.MarshalLock(with)
	if err != nil {
		t.Fatal(err)
	}
	again, err := catalog.ParseLock(raw)
	if err != nil {
		t.Fatal(err)
	}
	a := again.Records.Artifacts[again.Records.Presets[again.Preset].Artifact]
	if a.ContextMemory == nil || a.ContextMemory.KVHeads != 8 {
		t.Fatal("geometry lost on lock round-trip")
	}
	a.ContextMemory = nil
	again.Records.Artifacts[again.Records.Presets[again.Preset].Artifact] = a
	if !catalog.SameExecution(with, again) {
		t.Fatal("descriptive geometry changed executable identity")
	}
	estimate, err := setup.Assess(again, facts(32))
	if err != nil || estimate.Memory.KVCacheBytes != nil || estimate.Memory.Status != "incomplete" {
		t.Fatal("missing geometry implied known cache", estimate.Memory, err)
	}
}

func TestPromptReserveAddsOnceAndStaysOutsideGPUAllowance(t *testing.T) {
	l := attentionLock(t, 4096, catalog.ComputeGPU)
	p := l.Records.Presets[l.Preset]
	p.Profiles[0].EngineConfig.PromptCacheRAMMiB = new(4096)
	l.Records.Presets[l.Preset] = p
	observed := facts(16)
	observed.MetalDeviceMemoryMiB, observed.WiredLimitMiB = 5*1024, 5*1024
	got, err := setup.Assess(l, observed)
	if err != nil || len(got.Refusals) != 0 || got.MemoryBytes != 7680<<20 || got.Memory.PromptCacheReserveBytes != 4<<30 {
		t.Fatal("host cache reserve omitted, double-counted or charged against GPU budget", got, err)
	}
}

func TestSplashCeilingDoesNotRefuseThePresetAtTheMetalLimit(t *testing.T) {
	d := catalogDocument(t)
	l := selectedLock(t, d, "qwen3.8-27b-q4xl-splash")
	observed := facts(32)
	observed.Chip = "Apple M5"
	observed.MetalDeviceMemoryMiB, observed.WiredLimitMiB = 24*1024, 24*1024
	got, err := setup.Assess(l, observed)
	cap := l.Records.Presets[l.Preset].Config().Splash.MaxMemoryBytes
	if err != nil || len(got.Refusals) != 0 || got.Memory.Basis != "estimated" || got.Memory.KVCacheBytes != nil ||
		got.Memory.EngineLimitBytes != cap || got.Memory.EngineEnvelopeBytes != 0 || got.MemoryBytes != got.ModelBytes {
		t.Fatal("Splash ceiling became a required allocation", got.Memory, got.Refusals, err)
	}
}

func TestOmittedPromptReserveIsUnknownRatherThanExplicitZero(t *testing.T) {
	l := attentionLock(t, 4096, catalog.ComputeGPU)
	p := l.Records.Presets[l.Preset]
	p.Profiles[0].EngineConfig.PromptCacheRAMMiB = nil
	l.Records.Presets[l.Preset] = p
	got, err := setup.Assess(l, facts(16))
	if err != nil || !strings.Contains(strings.Join(got.Memory.Unknown, " "), "prompt-cache RAM") {
		t.Fatal("omitted engine reserve represented as known zero", got.Memory, err)
	}
}
