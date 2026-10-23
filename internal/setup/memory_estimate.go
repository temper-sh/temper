package setup

import (
	"fmt"
	"strings"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/modelmemory"
)

// MemoryEstimate selects measured usage when applicable, otherwise estimates.
// Its basis and limits distinguish observed peaks from allocation guarantees.
type MemoryEstimate struct {
	Basis                   string                     `json:"basis"`
	Status                  string                     `json:"status"`
	Measurement             *catalog.MemoryMeasurement `json:"measurement,omitempty"`
	FallbackReason          string                     `json:"fallback_reason,omitempty"`
	WeightFileBytes         int64                      `json:"weight_file_bytes"`
	ContextTokens           int                        `json:"context_tokens"`
	ParallelSlots           int                        `json:"parallel_slots"`
	CacheFormat             string                     `json:"cache_format,omitempty"`
	KVCacheBytes            *int64                     `json:"kv_cache_bytes"`
	PromptCacheReserveBytes int64                      `json:"prompt_cache_reserve_bytes"`
	EngineEnvelopeBytes     int64                      `json:"engine_envelope_bytes"`
	EngineLimitBytes        int64                      `json:"engine_limit_bytes,omitempty"`
	AccountedBytes          int64                      `json:"accounted_bytes"`
	Unknown                 []string                   `json:"unknown"`
}

func estimateMemory(lock catalog.Lock, weights int64) (MemoryEstimate, error) {
	p := lock.Records.Presets[lock.Preset]
	c := p.Config()
	m := MemoryEstimate{Basis: "estimated", Status: "incomplete", WeightFileBytes: weights, ContextTokens: p.Window(), ParallelSlots: 1,
		Unknown: []string{"runtime graphs, work buffers and allocator overhead", "no applicable catalog memory measurement"}}
	if p.Interface != "chat-completions" {
		m.Unknown = append(m.Unknown, "non-chat engine context/workspace allocation")
		return m, nil
	}
	switch c.Kind {
	case "kapitsa/v1":
		m.CacheFormat, m.ParallelSlots, m.EngineLimitBytes = "f16", c.Kapitsa.Slots, c.Kapitsa.MaxMemoryBytes
		m.Unknown = append(m.Unknown, "Kapitsa context/recurrent cache, workspace and allocations in the separate ANE service")
	case "splash/v1", "splash/v2":
		m.CacheFormat, m.EngineLimitBytes = c.Splash.KVCache, c.Splash.MaxMemoryBytes
		m.Unknown = append(m.Unknown, "Splash context/recurrent cache and native memory planning")
	case "llama-server/v1":
		if c.Controls.CPUMoELayers > 0 {
			m.Unknown = append(m.Unknown, "MoE CPU/GPU weight split: per-layer expert sizes are unavailable; CPU placement does not reduce total physical RAM accounting")
		}
		m.CacheFormat, m.ParallelSlots = c.KVCache, c.Parallel
		var err error
		m.PromptCacheReserveBytes, err = modelmemory.Product(int64(c.PromptCacheRAMMiB), MiB)
		if err != nil {
			return m, err
		}
		if geometry := lock.Records.Artifacts[p.Artifact].ContextMemory; geometry == nil {
			m.Unknown = append(m.Unknown, "KV/context cache: exact artifact geometry is missing (including any hybrid/recurrent state)")
		} else {
			bytes, err := modelmemory.LlamaKV(*geometry, p.Window(), c.Parallel, c.KVCache)
			if err != nil {
				return m, err
			}
			m.KVCacheBytes = &bytes
		}
	case "rapid-mlx/v1":
		m.CacheFormat, m.ParallelSlots = c.Python.RapidMLX.KVCacheDType, c.Python.RapidMLX.MaxNumSeqs
		m.Unknown = append(m.Unknown, "Rapid-MLX active KV and prefix-cache allocation within the engine envelope")
	case "vllm-metal/v1":
		m.CacheFormat, m.ParallelSlots = c.Python.VLLMMetal.KVCacheDType, c.Python.VLLMMetal.MaxNumSeqs
		m.Unknown = append(m.Unknown, "vLLM-Metal KV/prefix-cache block allocation within the engine envelope")
	default:
		m.Unknown = append(m.Unknown, "context/recurrent cache for "+c.Kind+" is not modeled")
	}
	if p.Speculation.Method != "" && p.Speculation.Method != "none" {
		m.Unknown = append(m.Unknown, "speculative decoding cache/state beyond the target KV cache")
	}
	if p.ProjectorArtifact != "" {
		m.Unknown = append(m.Unknown, "vision encoder workspace and image-dependent activations")
	}
	return m, nil
}

func (m MemoryEstimate) Lines() []string {
	if measured := m.Measurement; m.Basis == "measured" && measured != nil {
		loaded := "not sampled"
		if measured.LoadedBytes != nil {
			loaded = Size(*measured.LoadedBytes)
		}
		return []string{
			fmt.Sprintf("Measured memory: %s peak; loaded %s; counter %s (%s).", Size(measured.PeakBytes), loaded, measured.Counter, measured.ObservedOn),
			fmt.Sprintf("Observed on %s / %s RAM: %s", measured.Machine.Chip, Size(measured.Machine.PhysicalMemoryBytes), measured.Workload),
			"Limits: " + measured.Limitations + " Observed peak is not a worst-case memory guarantee.",
			"Assessment: " + measured.Source,
		}
	}
	lines := []string{fmt.Sprintf("Memory estimate: %s accounted; weight files %s (storage proxy, not resident memory).", Size(m.AccountedBytes), Size(m.WeightFileBytes))}
	if m.FallbackReason != "" {
		lines = append(lines, "Using an estimate: "+m.FallbackReason+".")
	}
	cache := "unknown"
	if m.KVCacheBytes != nil {
		cache = Size(*m.KVCacheBytes) + " estimated payload"
	}
	format := m.CacheFormat
	if format == "" {
		format = "not modeled"
	}
	lines = append(lines, fmt.Sprintf("KV/context cache: %s; context %d tokens, %d parallel slot(s), format %s.", cache, m.ContextTokens, m.ParallelSlots, format))
	if m.PromptCacheReserveBytes > 0 {
		lines = append(lines, "Prompt-cache RAM reserve: "+Size(m.PromptCacheReserveBytes)+" configured cap, not observed allocation.")
	}
	if m.EngineEnvelopeBytes > 0 {
		lines = append(lines, "Engine allocation envelope: "+Size(m.EngineEnvelopeBytes)+"; includes weights/cache and is not added to them.")
	}
	if m.EngineLimitBytes > 0 {
		lines = append(lines, "Engine memory limit: "+Size(m.EngineLimitBytes)+"; a ceiling, not a required allocation.")
	}
	lines = append(lines, "Total runtime fit is unknown: "+strings.Join(m.Unknown, "; ")+".")
	return lines
}
