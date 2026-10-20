package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/temper-sh/temper/internal/machine"
)

// MemoryMeasurement is a reviewed observation for one runtime configuration.
// PeakBytes is the observed peak of Counter, never a maximum for all workloads.
// The original experiment owns raw evidence; Source links its public assessment.
type MemoryMeasurement struct {
	ObservedOn  string          `yaml:"observed_on" json:"observed_on"`
	Source      string          `yaml:"source" json:"source"`
	Workload    string          `yaml:"workload" json:"workload"`
	Counter     string          `yaml:"counter" json:"counter"`
	LoadedBytes *int64          `yaml:"loaded_bytes" json:"loaded_bytes"`
	PeakBytes   int64           `yaml:"peak_bytes" json:"peak_bytes"`
	Limitations string          `yaml:"limitations" json:"limitations"`
	Machine     MemoryMachine   `yaml:"machine" json:"machine"`
	Execution   MemoryExecution `yaml:"execution" json:"execution"`
}

// MemoryMachine records the tested machine class, not a personal machine ID.
// Measurements are used on the same chip/RAM/Metal-budget class only.
type MemoryMachine struct {
	Chip                string `yaml:"chip" json:"chip"`
	PhysicalMemoryBytes int64  `yaml:"physical_memory_bytes" json:"physical_memory_bytes"`
	MetalMemoryMiB      int64  `yaml:"metal_memory_mib" json:"metal_memory_mib"`
	OS                  string `yaml:"os" json:"os"`
}

// MemoryExecution preserves the inputs that give a measurement its meaning.
// Explicit values keep old measurements readable after a preset is edited.
// Names, descriptions, download locations and the measurement itself are not
// executable inputs. Request sampling is covered by the recorded workload.
type MemoryExecution struct {
	EngineVersion       string        `yaml:"engine_version" json:"engine_version"`
	EngineFiles         []string      `yaml:"engine_files" json:"engine_files"`
	Weights             []File        `yaml:"weights" json:"weights"`
	Draft               []File        `yaml:"draft,omitempty" json:"draft,omitempty"`
	Projector           []File        `yaml:"projector,omitempty" json:"projector,omitempty"`
	Compute             ComputeDevice `yaml:"compute" json:"compute"`
	ContextWindowTokens int           `yaml:"context_window_tokens" json:"context_window_tokens"`
	EngineConfig        EngineConfig  `yaml:"engine_config" json:"engine_config"`
	Speculation         Speculation   `yaml:"speculation" json:"speculation"`
}

// MemoryExecution returns a descriptive snapshot for an already validated lock.
// It performs no measurements and is not evidence that the execution was run.
func (l Lock) MemoryExecution() MemoryExecution {
	d := l.Records
	p := d.Presets[l.Preset]
	s := d.Engines[p.Engine].Supply
	m := MemoryExecution{
		Weights: sortedFiles(d.Artifacts[p.Artifact].Files),
		Compute: p.Compute, ContextWindowTokens: p.Window(),
		EngineConfig: p.Config(), Speculation: p.Speculation,
	}
	m.EngineConfig.GPULayers = p.gpuOffloadLayers(m.EngineConfig)
	// Artifact IDs are catalog names. The draft files below own its identity.
	m.Speculation.DraftArtifact = ""
	if p.Speculation.DraftArtifact != "" {
		m.Draft = sortedFiles(d.Artifacts[p.Speculation.DraftArtifact].Files)
	}
	if p.ProjectorArtifact != "" {
		m.Projector = sortedFiles(d.Artifacts[p.ProjectorArtifact].Files)
	}
	if s.Release != nil {
		m.EngineVersion = s.Release.Version
		m.EngineFiles = []string{s.Release.Artifact.SHA256}
	} else if s.Python != nil {
		m.EngineFiles = append(m.EngineFiles, s.Python.Runtime.Artifact.SHA256)
		for _, pkg := range s.Python.Packages {
			m.EngineFiles = append(m.EngineFiles, pkg.Artifact.SHA256)
			if pkg.Name == s.Python.Root {
				m.EngineVersion = pkg.Version
			}
		}
		slices.Sort(m.EngineFiles)
	}
	// A snapshot must not share mutable engine settings with its source.
	raw, _ := json.Marshal(m)
	var snapshot MemoryExecution
	_ = json.Unmarshal(raw, &snapshot)
	return snapshot
}

// Applicable reports why recorded evidence cannot replace an estimate.
// A stale measurement remains readable; it never prevents using the preset.
func (m MemoryMeasurement) Applicable(l Lock, facts machine.Facts) (bool, string) {
	if !reflect.DeepEqual(m.Execution, l.MemoryExecution()) {
		return false, "recorded memory measurement uses different weights, engine or runtime settings"
	}
	if m.Machine.Chip != facts.Chip || m.Machine.PhysicalMemoryBytes != facts.PhysicalMemoryBytes ||
		m.Machine.MetalMemoryMiB != facts.MetalDeviceMemoryMiB {
		return false, "recorded memory measurement uses a different chip or memory capacity"
	}
	return true, ""
}

func (m MemoryMeasurement) validate() error {
	if _, err := time.Parse("2006-01-02", m.ObservedOn); err != nil {
		return errors.New("memory measurement observed_on must be a YYYY-MM-DD date")
	}
	source, err := url.Parse(m.Source)
	if err != nil || source.Scheme != "https" || source.Host == "" || source.User != nil {
		return errors.New("memory measurement source must be a public HTTPS assessment URL")
	}
	for _, value := range []string{m.Workload, m.Limitations, m.Machine.Chip, m.Machine.OS, m.Execution.EngineVersion} {
		if strings.TrimSpace(value) == "" {
			return errors.New("memory measurement needs workload, limitations, machine and engine labels")
		}
	}
	switch m.Counter {
	case "engine-rss", "engine-physical-footprint", "engine-native-allocation", "system-wired-growth":
	default:
		return fmt.Errorf("unsupported memory measurement counter %q", m.Counter)
	}
	if m.PeakBytes <= 0 || m.LoadedBytes != nil && (*m.LoadedBytes < 0 || *m.LoadedBytes > m.PeakBytes) {
		return errors.New("memory measurement needs a positive peak and an optional loaded value no greater than peak")
	}
	if m.Machine.PhysicalMemoryBytes <= 0 || m.Machine.MetalMemoryMiB <= 0 ||
		m.Machine.MetalMemoryMiB > m.Machine.PhysicalMemoryBytes/(1<<20) {
		return errors.New("memory measurement machine capacities are invalid")
	}
	if m.Execution.ContextWindowTokens <= 0 || m.Execution.Compute != ComputeCPU && m.Execution.Compute != ComputeGPU {
		return errors.New("memory measurement execution needs context and compute")
	}
	if _, err := m.Execution.EngineConfig.value(); err != nil {
		return fmt.Errorf("memory measurement engine config: %w", err)
	}
	if len(m.Execution.EngineFiles) == 0 || len(m.Execution.Weights) == 0 {
		return errors.New("memory measurement requires exact engine and weight files")
	}
	for _, sha := range m.Execution.EngineFiles {
		if !hashPattern.MatchString(sha) {
			return errors.New("memory measurement engine files require SHA-256 identities")
		}
	}
	for _, files := range [][]File{m.Execution.Weights, m.Execution.Draft, m.Execution.Projector} {
		for _, file := range files {
			if !hashPattern.MatchString(file.SHA256) || file.Bytes <= 0 || file.Path == "" {
				return errors.New("memory measurement model files require path, size and SHA-256")
			}
		}
	}
	return nil
}
