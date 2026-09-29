// Package setup plans and saves explicit user selections for guided setup.
// Planning is pure; saving and loading live in separate effect/read functions.
package setup

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/temper-sh/temper/internal/artifactset"
	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/check"
	"github.com/temper-sh/temper/internal/datadir"
	"github.com/temper-sh/temper/internal/hfcache"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/preset"
	"github.com/temper-sh/temper/internal/render/engine"
)

const MiB int64 = 1 << 20

type Download struct {
	Name   string `json:"name"`
	Bytes  int64  `json:"bytes"`
	Kind   string `json:"kind"` // model, template, or software
	Cached bool   `json:"cached"`
	Cache  string `json:"cache,omitempty"` // "huggingface" for shared cached files; "temper" for installed files.
}

func (d Download) Action() string {
	if d.Cached {
		if d.Cache == "huggingface" {
			return "Cached in Hugging Face"
		}
		return "Cached in Temper"
	}
	if d.Kind == "software" {
		return "May download"
	}
	return "Download on Prepare"
}

type PresetPlan struct {
	Preset                   string                       `json:"preset"`
	RuntimeDiskEstimateBytes int64                        `json:"runtime_disk_estimate_bytes,omitempty"`
	Lock                     catalog.Lock                 `json:"-"`
	ModelBytes               int64                        `json:"model_bytes"`
	Budget                   budget.Prediction            `json:"memory_prediction"`
	WiredMemory              *WiredMemoryAdvice           `json:"wired_memory_advice,omitempty"`
	Contexts                 map[string]ContextAssessment `json:"contexts,omitempty"`
	Refusals                 []string                     `json:"refusals,omitempty"`
}

type ContextAssessment struct {
	WindowTokens int                     `json:"window_tokens"`
	Status       string                  `json:"status"` // tested or unknown for this exact point and machine.
	Finding      *catalog.ContextFinding `json:"finding,omitempty"`
}

type Plan struct {
	Configuration          *Configuration `json:"configuration,omitempty"`
	Layouts                []LayoutPlan   `json:"layouts,omitempty"`
	Root                   string         `json:"root"`
	Presets                []PresetPlan   `json:"presets"`
	Downloads              []Download     `json:"downloads"`
	DownloadBytes          int64          `json:"download_bytes"`
	FreshDiskBytes         int64          `json:"fresh_disk_bytes"`
	RemainingDownloads     []Download     `json:"remaining_downloads"`
	RemainingDownloadBytes int64          `json:"remaining_download_bytes"`
	RemainingDiskBytes     int64          `json:"remaining_disk_bytes"`
	FreeDiskBytes          int64          `json:"free_disk_bytes"`
	CanPrepare             bool           `json:"can_prepare"`
	Refusals               []string       `json:"refusals,omitempty"`
	HFCache                *CachePlan     `json:"hf_cache,omitempty"`
}

type CachePlan struct {
	Root               string `json:"root"`
	SharedFilesystem   bool   `json:"shared_filesystem"`
	RemainingDiskBytes int64  `json:"remaining_disk_bytes"`
	FreeDiskBytes      int64  `json:"free_disk_bytes"`
	CopyBytes          int64  `json:"copy_bytes"`
}

// Assess evaluates a configuration using declared model sizes. Fits remains a
// prediction: KV cache, runtime overhead and real task behavior are unmeasured.
func Assess(locked catalog.Lock, facts machine.Facts) (PresetPlan, error) {
	if err := locked.Validate(); err != nil {
		return PresetPlan{}, err
	}
	observed, err := facts.Budget()
	if err != nil {
		return PresetPlan{}, err
	}
	result := PresetPlan{Preset: locked.Preset, Lock: locked}
	result.Contexts = make(map[string]ContextAssessment, len(locked.Records.Presets))
	for id, layout := range locked.Records.Presets {
		assessment := ContextAssessment{WindowTokens: layout.ContextWindowTokens, Status: "unknown"}
		template := ""
		if len(layout.Patches) > 0 {
			template = layout.Patches[0]
		}
		findings, err := catalog.MatchingContexts(locked.Records, id, template, facts)
		if err != nil {
			return PresetPlan{}, err
		}
		for _, finding := range findings {
			if finding.WindowTokens == layout.ContextWindowTokens {
				assessment.Status, assessment.Finding = "tested", &finding
				break
			}
		}
		result.Contexts[id] = assessment
	}
	if !locked.Target.Matches(facts.Target) {
		result.Refusals = append(result.Refusals, "configuration does not support this operating system or architecture")
	}
	projection, err := locked.Projections()
	if err != nil {
		return PresetPlan{}, err
	}
	modelBytes := make(map[string]int64)
	var onDemandBytes, onDemandGPU int64
	for id, layout := range locked.Records.Presets {
		var size int64
		files := append([]catalog.File(nil), locked.Records.Artifacts[layout.Artifact].Files...)
		if layout.Speculation.DraftArtifact != "" {
			files = append(files, locked.Records.Artifacts[layout.Speculation.DraftArtifact].Files...)
		}
		for _, file := range files {
			if err := add(&size, file.Bytes); err != nil {
				return PresetPlan{}, err
			}
		}
		if layout.EngineConfig.Splash != nil {
			if err := engine.SplashCompatibility(facts.Chip, facts.Target.DistributionVersion); err != nil {
				result.Refusals = append(result.Refusals, err.Error())
			}
			// A planning estimate for the additional native copy, not an installed-size
			// fact. Splash admits exact missing files and its 2 GiB reserve at startup.
			if err := add(&result.RuntimeDiskEstimateBytes, size); err != nil {
				return PresetPlan{}, err
			}
		}
		modelBytes[id] = size
		if err := add(&result.ModelBytes, size); err != nil {
			return PresetPlan{}, err
		}
		onDemandBytes = max(onDemandBytes, size)
		if layout.EngineConfig.Splash != nil || layout.EngineConfig.GPULayers > 0 {
			onDemandGPU = max(onDemandGPU, size)
		}
	}
	minimum := onDemandBytes
	if err := add(&minimum, budget.OSFloorMiB*MiB); err != nil {
		return PresetPlan{}, err
	}
	if minimum > facts.PhysicalMemoryBytes {
		result.Refusals = append(result.Refusals, fmt.Sprintf("model weights and the OS allowance need at least %s; this machine has %s", Size(minimum), Size(facts.PhysicalMemoryBytes)))
	}
	gpuMinimum := onDemandGPU
	if gpuMinimum > 0 {
		if err := add(&gpuMinimum, budget.OSFloorMiB*MiB); err != nil {
			return PresetPlan{}, err
		}
		if gpuMinimum > facts.WiredLimitMiB*MiB {
			result.Refusals = append(result.Refusals, fmt.Sprintf("conservative full-model GPU allowance and OS allowance exceed the %s wired-memory limit", Size(facts.WiredLimitMiB*MiB)))
		}
	}
	mode := projection.Manifest.Modes[locked.Preset]
	result.Budget, err = check.PredictBudget(projection.Manifest, mode, observed, modelBytes)
	if err != nil {
		return PresetPlan{}, err
	}
	if result.Budget.Status == budget.StatusExceeded {
		result.Refusals = append(result.Refusals, fmt.Sprintf("configured memory allocation exceeds the %s wired-memory limit (prediction)", Size(result.Budget.WiredLimitMiB*MiB)))
	}
	gpuMinimumMiB := gpuMinimum / MiB
	if gpuMinimum%MiB != 0 {
		gpuMinimumMiB++
	}
	result.WiredMemory = wiredMemoryAdvice(max(gpuMinimumMiB, result.Budget.RequiredMiB), facts, result.Budget)
	return result, nil
}

// build deduplicates exact model and software material across selected presets.
func build(root string, facts machine.Facts, freeBytes int64, locks []catalog.Lock, material Material) (Plan, error) {
	resolved, err := datadir.Resolve(root)
	if err != nil {
		return Plan{}, err
	}
	if material.root != "" && material.root != resolved {
		return Plan{}, errors.New("inspected model material belongs to another root")
	}
	if freeBytes < 0 {
		return Plan{}, errors.New("free disk space is unavailable")
	}
	locks = slices.Clone(locks)
	slices.SortFunc(locks, func(a, b catalog.Lock) int { return strings.Compare(a.Preset, b.Preset) })
	plan := Plan{Root: resolved, FreeDiskBytes: freeBytes, CanPrepare: true}
	if material.hfCache != nil {
		cache := *material.hfCache
		plan.HFCache = &cache
	}
	seenSets := map[string]bool{}
	seenModels := map[string]int64{}
	seenSoftware := map[string]bool{}
	for _, locked := range locks {
		mode, err := Assess(locked, facts)
		if err != nil {
			return Plan{}, err
		}
		plan.Presets = append(plan.Presets, mode)
		for _, refusal := range mode.Refusals {
			plan.Refusals = append(plan.Refusals, mode.Preset+": "+refusal)
		}
		projection, err := locked.Projections()
		if err != nil {
			return Plan{}, err
		}
		for _, id := range keys(locked.Records.Presets) {
			layout := locked.Records.Presets[id]
			set, err := artifactset.New(resolved, id, projection.Manifest.Layouts[id], projection.Artifacts.Entries[id], projection.Manifest.Patches)
			if err != nil {
				return Plan{}, err
			}
			remaining := !material.verified[set.Path()]
			artifacts := []catalog.Artifact{locked.Records.Artifacts[layout.Artifact]}
			if layout.Speculation.DraftArtifact != "" {
				artifacts = append(artifacts, locked.Records.Artifacts[layout.Speculation.DraftArtifact])
			}
			for _, artifact := range artifacts {
				for _, file := range artifact.Files {
					if size, seen := seenModels[file.SHA256]; seen {
						if size != file.Bytes {
							return Plan{}, fmt.Errorf("model %q has conflicting sizes for the same SHA-256", file.Path)
						}
						continue
					}
					seenModels[file.SHA256] = file.Bytes
					existing, reusable := material.models[file.SHA256]
					if reusable && existing.Size != file.Bytes {
						return Plan{}, fmt.Errorf("model %q catalog size %d differs from installed receipt size %d", file.Path, file.Bytes, existing.Size)
					}
					hf, cached := material.hfModels[hfcache.Entry{Repo: artifact.Repo, Revision: artifact.Revision, Name: file.Path, SHA256: file.SHA256}]
					if err := plan.download("model", file.Path, file.Bytes, file.Bytes, !reusable && !cached); err != nil {
						return Plan{}, err
					}
					if cached && !reusable {
						plan.Downloads[len(plan.Downloads)-1].Cache = "huggingface"
						if hf.copy {
							if err := add(&plan.RemainingDiskBytes, file.Bytes); err != nil {
								return Plan{}, err
							}
							if err := add(&plan.HFCache.CopyBytes, file.Bytes); err != nil {
								return Plan{}, err
							}
						}
					}
					if plan.HFCache != nil && !cached && !reusable {
						if err := add(&plan.HFCache.RemainingDiskBytes, file.Bytes); err != nil {
							return Plan{}, err
						}
						if !plan.HFCache.SharedFilesystem {
							if err := add(&plan.HFCache.CopyBytes, file.Bytes); err != nil {
								return Plan{}, err
							}
						}
					}
				}
			}
			if seenSets[set.Path()] {
				continue
			}
			seenSets[set.Path()] = true
			for _, patch := range layout.Patches {
				for _, file := range locked.Records.Patches[patch].Files {
					if err := plan.download("template", patch+"/"+file.Path, file.Bytes, file.Bytes, remaining); err != nil {
						return Plan{}, err
					}
				}
			}
		}
		supplies := []catalog.Supply{locked.Records.Runtime.Router}
		for _, id := range keys(locked.Records.Engines) {
			supplies = append(supplies, locked.Records.Engines[id].Supply)
		}
		sets, err := preset.Software(locked)
		if err != nil {
			return Plan{}, err
		}
		identities := make(map[string]string, len(sets))
		for _, set := range sets {
			identities[set.Package] = set.ID
		}
		for _, supply := range supplies {
			identity := identities[supply.Package]
			if seenSoftware[identity] {
				continue
			}
			seenSoftware[identity] = true
			remaining := !material.software[identity]

			if supply.Release == nil {
				return Plan{}, fmt.Errorf("software %s has no resolved release", supply.Package)
			}
			artifact := supply.Release.Artifact
			// One archive plus two unpacked copies covers provider staging and
			// committed files during a fresh preparation.
			disk := artifact.Size
			if err := add(&disk, artifact.UnpackedSize); err != nil {
				return Plan{}, err
			}
			if err := add(&disk, artifact.UnpackedSize); err != nil {
				return Plan{}, err
			}
			name := mode.Preset
			if err := plan.download("software", name+"/"+supply.Package+" "+supply.Release.Version, artifact.Size, disk, remaining); err != nil {
				return Plan{}, err
			}
		}
	}
	// Configuration and receipt overhead; this is explicitly a planning allowance.
	if err := add(&plan.FreshDiskBytes, 16*MiB); err != nil {
		return Plan{}, err
	}
	if err := add(&plan.RemainingDiskBytes, 16*MiB); err != nil {
		return Plan{}, err
	}
	if plan.RemainingDiskBytes > freeBytes {
		plan.Refusals = append(plan.Refusals, fmt.Sprintf("remaining-install disk allowance is %s; %s is free", Size(plan.RemainingDiskBytes), Size(freeBytes)))
	}
	if plan.HFCache != nil && !plan.HFCache.SharedFilesystem && plan.HFCache.RemainingDiskBytes > plan.HFCache.FreeDiskBytes {
		plan.Refusals = append(plan.Refusals, fmt.Sprintf("Hugging Face cache needs %s on its filesystem; %s is free", DownloadSize(plan.HFCache.RemainingDiskBytes), DownloadSize(plan.HFCache.FreeDiskBytes)))
	}
	plan.CanPrepare = len(plan.Refusals) == 0
	return plan, nil
}

func (p *Plan) download(kind, name string, transfer, disk int64, remaining bool) error {
	if err := add(&p.DownloadBytes, transfer); err != nil {
		return err
	}
	if err := add(&p.FreshDiskBytes, disk); err != nil {
		return err
	}
	item := Download{Name: name, Bytes: transfer, Kind: kind, Cached: !remaining}
	if item.Cached {
		item.Cache = "temper"
	}
	p.Downloads = append(p.Downloads, item)
	if remaining {
		if err := add(&p.RemainingDownloadBytes, transfer); err != nil {
			return err
		}
		if err := add(&p.RemainingDiskBytes, disk); err != nil {
			return err
		}
		p.RemainingDownloads = append(p.RemainingDownloads, item)
	}
	return nil
}

func add(total *int64, value int64) error {
	if value < 0 || value > math.MaxInt64-*total {
		return errors.New("catalog sizes exceed supported range")
	}
	*total += value
	return nil
}

func Size(bytes int64) string { return fmt.Sprintf("%.2f GiB", float64(bytes)/float64(1<<30)) }

func DownloadSize(bytes int64) string {
	switch {
	case bytes < 1<<10:
		return fmt.Sprintf("%d B", bytes)
	case bytes < 1<<20:
		return fmt.Sprintf("%.2f KiB", float64(bytes)/float64(1<<10))
	case bytes < 1<<30:
		return fmt.Sprintf("%.2f MiB", float64(bytes)/float64(1<<20))
	default:
		return Size(bytes)
	}
}

func (p Plan) WeightSummary() string {
	var cached, hf, needed int64
	for _, item := range p.Downloads {
		if item.Kind != "model" {
			continue
		}
		if item.Cached {
			if item.Cache == "huggingface" {
				hf += item.Bytes
			} else {
				cached += item.Bytes
			}
		} else {
			needed += item.Bytes
		}
	}
	if hf > 0 {
		parts := []string{DownloadSize(hf) + " cached in Hugging Face"}
		if cached > 0 {
			parts = append(parts, DownloadSize(cached)+" cached in Temper")
		}
		if needed == 0 {
			return "Weights: " + strings.Join(parts, "; ") + ". No weight download on Prepare."
		}
		return "Weights: " + DownloadSize(needed) + " to download on Prepare; " + strings.Join(parts, "; ") + "."
	}
	if needed == 0 && cached > 0 {
		return "Weights: all cached in Temper (" + DownloadSize(cached) + "). No weight download on Prepare."
	}
	if cached > 0 {
		return "Weights: " + DownloadSize(needed) + " to download on Prepare; " + DownloadSize(cached) + " cached in Temper."
	}
	return "Weights: " + DownloadSize(needed) + " to download on Prepare."
}

// Section groups the same disclosure for terminal blocks and plain CLI output.
type Section struct {
	Title     string
	Lines     []string
	Downloads []Download // Non-nil for the collapsible transfer table.
	Warning   bool
}

func (p Plan) Sections() []Section { return p.configurationSections() }

func (p Plan) Lines() []string {
	var lines []string
	for _, section := range p.Sections() {
		if section.Warning {
			lines = append(lines, section.Title)
		}
		lines = append(lines, section.Lines...)
		for _, item := range section.Downloads {
			lines = append(lines, item.Name+" — "+DownloadSize(item.Bytes)+" — "+item.Action())
		}
	}
	return lines
}

func keys[T any](values map[string]T) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	slices.Sort(result)
	return result
}
