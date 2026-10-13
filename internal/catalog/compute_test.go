package catalog_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/render"
)

func TestComputeRequiresAnExplicitSupportedSelection(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*catalog.Preset)
	}{
		{"missing selection", func(p *catalog.Preset) { p.Compute = "" }},
		{"missing choices", func(p *catalog.Preset) { p.ComputeOptions = nil }},
		{"unsupported choice", func(p *catalog.Preset) { p.ComputeOptions = []catalog.ComputeDevice{"tpu"} }},
		{"duplicate choice", func(p *catalog.Preset) {
			p.ComputeOptions = []catalog.ComputeDevice{catalog.ComputeGPU, catalog.ComputeGPU}
		}},
		{"selection outside choices", func(p *catalog.Preset) { p.ComputeOptions = []catalog.ComputeDevice{catalog.ComputeCPU} }},
		{"GPU with zero layers", func(p *catalog.Preset) { p.Profiles[0].EngineConfig.GPULayers = 0 }},
		{"CPU with invalid GPU tuning", func(p *catalog.Preset) { p.Compute = catalog.ComputeCPU; p.Profiles[0].EngineConfig.GPULayers = -1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := document()
			p := d.Presets["qwen-32k"]
			tc.change(&p)
			d.Presets["qwen-32k"] = p
			if err := d.Validate(); err == nil {
				t.Fatal("invalid compute selection accepted")
			}
		})
	}
	// GPU-only adapters reject even an unselected CPU choice.
	for _, id := range []string{"splash-q4", "rapid-mlx", "vllm-metal"} {
		t.Run(id, func(t *testing.T) {
			d := qwenStudy(t)
			p := d.Presets[id]
			p.ComputeOptions = append(p.ComputeOptions, catalog.ComputeCPU)
			d.Presets[id] = p
			if err := d.Validate(); err == nil || !strings.Contains(err.Error(), "GPU compute only") {
				t.Fatalf("unsupported adapter choice: %v", err)
			}
		})
	}
}

func TestComputeRoundTripPreservesTuningAndReusesMaterials(t *testing.T) {
	d := document()
	p := d.Presets["qwen-32k"]
	p.Profiles[0].EngineConfig.GPULayers = 7 // Switching back must retain partial GPU offload.
	d.Presets["qwen-32k"] = p
	base := compile(t, d)
	before, _ := catalog.MarshalLock(base)
	settings := catalog.ExecutionSettings{ContextWindowTokens: p.Window(), MaxOutputTokens: p.RequestDefaults.MaxOutputTokens, Compute: catalog.ComputeCPU}
	cpu, err := catalog.ConfigureExecution(base, base.Preset, settings)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.SameExecution(cpu, base) {
		t.Fatal("compute selection lost execution identity or source provenance")
	}
	raw, err := catalog.MarshalLock(cpu)
	if err != nil {
		t.Fatal(err)
	}
	cpu, err = catalog.ParseLock(raw)
	if err != nil {
		t.Fatal(err)
	}
	if cpu.Records.Presets[cpu.Preset].Compute != catalog.ComputeCPU || cpu.Records.Presets[cpu.Preset].Config().GPULayers != 7 {
		t.Fatal("CPU selection lost authored GPU tuning")
	}
	for _, lock := range []catalog.Lock{base, cpu} {
		projection, err := lock.Projections()
		if err != nil {
			t.Fatal(err)
		}
		commands, err := render.Commands(render.Inputs{Manifest: projection.Manifest, Lock: projection.Artifacts, Mode: lock.Preset, Root: "/temper"})
		if err != nil {
			t.Fatal(err)
		}
		command := strings.Join(commands[lock.Preset].Lines(), "\n")
		if catalog.SameExecution(lock, cpu) {
			for _, flag := range []string{"-ngl 0", "--device none", "--no-kv-offload", "--no-op-offload"} {
				if !strings.Contains(command, flag) {
					t.Fatalf("CPU command missing %q: %s", flag, command)
				}
			}
		} else if !strings.Contains(command, "-ngl 7") || strings.Contains(command, "--device none") {
			t.Fatal("GPU command lost offload tuning")
		}
	}
	a, _ := base.Projections()
	b, _ := cpu.Projections()
	softwareA, _ := a.Software.SemanticDigest()
	softwareB, _ := b.Software.SemanticDigest()
	if softwareA != softwareB || !reflect.DeepEqual(a.Artifacts, b.Artifacts) {
		t.Fatal("compute selection changed reusable software or weights")
	}
	settings.Compute = "" // Subsequent context edits preserve CPU.
	settings.ContextWindowTokens -= 1024
	edited, err := catalog.ConfigureExecution(cpu, cpu.Preset, settings)
	if err != nil || edited.Records.Presets[cpu.Preset].Compute != catalog.ComputeCPU {
		t.Fatalf("context edit reset CPU: %v", err)
	}
	settings.ContextWindowTokens = p.Window()
	settings.Compute = catalog.ComputeGPU
	again, err := catalog.ConfigureExecution(cpu, cpu.Preset, settings)
	if err != nil || !reflect.DeepEqual(base, again) {
		t.Fatalf("GPU round trip changed settings: %v", err)
	}
	after, _ := catalog.MarshalLock(base)
	if !bytes.Equal(before, after) {
		t.Fatal("compute edit mutated the source lock")
	}
}

func TestComputeChoicePolicyAndInactiveTuningDoNotChangeExecution(t *testing.T) {
	d := document()
	base := compile(t, d)
	p := d.Presets[base.Preset]
	p.ComputeOptions = []catalog.ComputeDevice{catalog.ComputeGPU}
	d.Presets[base.Preset] = p
	changed := compile(t, d)
	if !catalog.SameExecution(base, changed) || reflect.DeepEqual(base, changed) {
		t.Fatal("choice policy changed execution or failed to change the source")
	}
	p.Compute, p.ComputeOptions = catalog.ComputeCPU, []catalog.ComputeDevice{catalog.ComputeCPU}
	d.Presets[base.Preset] = p
	cpu := compile(t, d)
	p.Profiles[0].EngineConfig.GPULayers = 0
	d.Presets[base.Preset] = p
	if !catalog.SameExecution(cpu, compile(t, d)) {
		t.Fatal("unused GPU tuning changed CPU execution")
	}
}
