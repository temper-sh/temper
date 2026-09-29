// Package setup plans and saves explicit user selections for guided setup.
// Planning is pure; saving and loading live in separate effect/read functions.
package setup

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
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

const ConfigurationDir = "configuration"
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

type ModePlan struct {
	RuntimeDiskEstimateBytes int64                        `json:"runtime_disk_estimate_bytes,omitempty"`
	Mode                     string                       `json:"mode"`
	Profile                  string                       `json:"profile"`
	Selection                catalog.Selection            `json:"selection"`
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
	DefaultProfile         string         `json:"default_profile,omitempty"`
	Modes                  []ModePlan     `json:"modes"`
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

func Mode(profile catalog.Profile) string {
	if profile.Foreground == "external" {
		return "utility"
	}
	return "local"
}

// Assess evaluates a configuration using declared model sizes. Fits remains a
// prediction: KV cache, runtime overhead and real task behavior are unmeasured.
func Assess(locked catalog.Lock, facts machine.Facts) (ModePlan, error) {
	if err := locked.Validate(); err != nil {
		return ModePlan{}, err
	}
	observed, err := facts.Budget()
	if err != nil {
		return ModePlan{}, err
	}
	p := locked.Records.Profiles[locked.Selection.Profile]
	result := ModePlan{Mode: Mode(p), Profile: locked.Selection.Profile, Selection: locked.Selection, Lock: locked}
	result.Contexts = make(map[string]ContextAssessment, len(locked.Records.Layouts))
	for id, layout := range locked.Records.Layouts {
		assessment := ContextAssessment{WindowTokens: layout.ContextWindowTokens, Status: "unknown"}
		if len(p.Bindings) == 1 {
			template := ""
			if len(layout.Patches) > 0 {
				template = layout.Patches[0]
			}
			findings, err := catalog.MatchingContexts(locked.Records, id, template, facts)
			if err != nil {
				return ModePlan{}, err
			}
			for _, finding := range findings {
				if finding.WindowTokens == layout.ContextWindowTokens {
					assessment.Status, assessment.Finding = "tested", &finding
					break
				}
			}
		}
		result.Contexts[id] = assessment
	}
	if len(locked.Records.Layouts) != 1 {
		result.Refusals = append(result.Refusals, "guided setup currently serves exactly one layout per profile")
	}
	if !locked.Target.Matches(facts.Target) {
		result.Refusals = append(result.Refusals, "configuration does not support this operating system or architecture")
	}
	projection, err := locked.Projections()
	if err != nil {
		return ModePlan{}, err
	}
	modelBytes := make(map[string]int64)
	var residentBytes, onDemandBytes, residentGPU, onDemandGPU int64
	for _, binding := range p.Bindings {
		layout := locked.Records.Layouts[binding.Layout]
		var size int64
		files := append([]catalog.File(nil), locked.Records.Artifacts[layout.Artifact].Files...)
		if layout.Speculation.DraftArtifact != "" {
			files = append(files, locked.Records.Artifacts[layout.Speculation.DraftArtifact].Files...)
		}
		for _, file := range files {
			if err := add(&size, file.Bytes); err != nil {
				return ModePlan{}, err
			}
		}
		if layout.EngineConfig.Splash != nil {
			if err := engine.SplashCompatibility(facts.Chip, facts.Target.DistributionVersion); err != nil {
				result.Refusals = append(result.Refusals, err.Error())
			}
			// A planning estimate for the additional native copy, not an installed-size
			// fact. Splash admits exact missing files and its 2 GiB reserve at startup.
			if err := add(&result.RuntimeDiskEstimateBytes, size); err != nil {
				return ModePlan{}, err
			}
		}
		modelBytes[binding.Layout] = size
		if err := add(&result.ModelBytes, size); err != nil {
			return ModePlan{}, err
		}
		if binding.Residency == "resident" {
			if err := add(&residentBytes, size); err != nil {
				return ModePlan{}, err
			}
			if layout.EngineConfig.Splash != nil || layout.EngineConfig.GPULayers > 0 {
				if err := add(&residentGPU, size); err != nil {
					return ModePlan{}, err
				}
			}
		} else {
			onDemandBytes = max(onDemandBytes, size)
			if layout.EngineConfig.Splash != nil || layout.EngineConfig.GPULayers > 0 {
				onDemandGPU = max(onDemandGPU, size)
			}
		}
	}
	minimum := residentBytes
	if err := add(&minimum, onDemandBytes); err != nil {
		return ModePlan{}, err
	}
	if err := add(&minimum, budget.OSFloorMiB*MiB); err != nil {
		return ModePlan{}, err
	}
	if minimum > facts.PhysicalMemoryBytes {
		result.Refusals = append(result.Refusals, fmt.Sprintf("model weights and the OS allowance need at least %s; this machine has %s", Size(minimum), Size(facts.PhysicalMemoryBytes)))
	}
	gpuMinimum := residentGPU
	if err := add(&gpuMinimum, onDemandGPU); err != nil {
		return ModePlan{}, err
	}
	if gpuMinimum > 0 {
		if err := add(&gpuMinimum, budget.OSFloorMiB*MiB); err != nil {
			return ModePlan{}, err
		}
		if gpuMinimum > facts.WiredLimitMiB*MiB {
			result.Refusals = append(result.Refusals, fmt.Sprintf("conservative full-model GPU allowance and OS allowance exceed the %s wired-memory limit", Size(facts.WiredLimitMiB*MiB)))
		}
	}
	mode := projection.Manifest.Modes[locked.Selection.Profile]
	result.Budget, err = check.PredictBudget(projection.Manifest, mode, observed, modelBytes)
	if err != nil {
		return ModePlan{}, err
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

// Build combines selected configurations into one installation preview. Identical
// model bytes count once across layouts and templates. Software installations are
// separate per configuration, matching the actual execution-prepare calls.
func Build(root string, facts machine.Facts, freeBytes int64, locks []catalog.Lock, defaultProfile string) (Plan, error) {
	return BuildWithMaterial(root, facts, freeBytes, locks, Material{}, defaultProfile)
}

// BuildWithMaterial keeps fresh totals for review and subtracts only material
// admitted by InspectModels from the remaining preparation allowance.
func BuildWithMaterial(root string, facts machine.Facts, freeBytes int64, locks []catalog.Lock, material Material, defaultProfile string) (Plan, error) {
	return build(root, facts, freeBytes, locks, material, defaultProfile, false)
}

func build(root string, facts machine.Facts, freeBytes int64, locks []catalog.Lock, material Material, defaultProfile string, presets bool) (Plan, error) {
	resolved, err := datadir.Resolve(root)
	if err != nil {
		return Plan{}, err
	}
	if material.root != "" && material.root != resolved {
		return Plan{}, errors.New("inspected model material belongs to another root")
	}
	if len(locks) == 0 && !presets {
		return Plan{}, errors.New("choose at least one profile")
	}
	if !presets {
		defaultProfile, err = selectedDefault(locks, defaultProfile)
		if err != nil {
			return Plan{}, err
		}
	}
	if freeBytes < 0 {
		return Plan{}, errors.New("free disk space is unavailable")
	}
	// Match preparation order when configurations share weights. The default
	// local configuration is prepared first, then alternatives, then helpers.
	locks = slices.Clone(locks)
	slices.SortFunc(locks, func(a, b catalog.Lock) int {
		return strings.Compare(configurationName(Mode(a.Records.Profiles[a.Selection.Profile]), a.Selection.Profile, defaultProfile), configurationName(Mode(b.Records.Profiles[b.Selection.Profile]), b.Selection.Profile, defaultProfile))
	})
	plan := Plan{Root: resolved, DefaultProfile: defaultProfile, FreeDiskBytes: freeBytes, CanPrepare: true}
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
		plan.Modes = append(plan.Modes, mode)
		for _, refusal := range mode.Refusals {
			plan.Refusals = append(plan.Refusals, mode.Profile+": "+refusal)
		}
		projection, err := locked.Projections()
		if err != nil {
			return Plan{}, err
		}
		for _, id := range keys(locked.Records.Layouts) {
			layout := locked.Records.Layouts[id]
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
		for _, supply := range supplies {
			remaining := true
			if presets {
				sets, err := preset.Software(locked)
				if err != nil {
					return Plan{}, err
				}
				var identity string
				for _, set := range sets {
					if set.Package == supply.Package {
						identity = set.ID
					}
				}
				if seenSoftware[identity] {
					continue
				}
				seenSoftware[identity] = true
				remaining = !material.software[identity]
			}
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
			name := plan.ConfigurationName(mode)
			if presets {
				name = mode.Profile
			}
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

// A sole explicitly selected local profile is necessarily the default. With
// alternatives, the caller must name it; catalog order never chooses it.
func selectedDefault(locks []catalog.Lock, selected string) (string, error) {
	seen := map[string]bool{}
	var local []string
	utilities := 0
	for _, locked := range locks {
		if err := locked.Validate(); err != nil {
			return "", err
		}
		id := locked.Selection.Profile
		if seen[id] {
			return "", fmt.Errorf("profile %q was selected more than once", id)
		}
		seen[id] = true
		if Mode(locked.Records.Profiles[id]) == "local" {
			local = append(local, id)
		} else {
			utilities++
		}
	}
	if utilities > 1 {
		return "", errors.New("select only one utility profile")
	}
	if selected == "" && len(local) == 1 {
		return local[0], nil
	}
	if selected == "" && len(local) == 0 {
		return "", nil
	}
	if selected == "" {
		return "", errors.New("choose a default profile when selecting multiple local models")
	}
	if !slices.Contains(local, selected) {
		return "", fmt.Errorf("default profile %q must be one of the selected local models", selected)
	}
	return selected, nil
}

// ConfigurationName keeps the default local and utility paths stable. Other
// installed local configurations have their own complete selection/lock pair.
func (p Plan) ConfigurationName(mode ModePlan) string {
	return configurationName(mode.Mode, mode.Profile, p.DefaultProfile)
}

func configurationName(mode, profile, defaultProfile string) string {
	if mode == "local" && profile != defaultProfile {
		return "local." + profile
	}
	return mode
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

func (p Plan) Sections() []Section {
	if p.Configuration != nil {
		return p.configurationSections()
	}
	var sections []Section
	var advice *WiredMemoryAdvice
	var tightModes []string
	for _, mode := range p.Modes {
		if mode.WiredMemory != nil {
			tightModes = append(tightModes, mode.Mode)
			if advice == nil || mode.WiredMemory.RequiredMiB > advice.RequiredMiB {
				advice = mode.WiredMemory
			}
		}
	}
	if advice != nil {
		// Modes are alternatives; the largest requirement determines one system
		// setting. Printing a lower command afterward could undo that remedy.
		section := advice.Section()
		section.Title += " · " + strings.Join(tightModes, ", ")
		sections = append(sections, section)
	}
	sections = append(sections, Section{Title: "Configuration", Lines: []string{"Configuration: " + filepath.Join(p.Root, ConfigurationDir)}})
	for _, mode := range p.Modes {
		lines := []string{mode.Mode + ": " + mode.Profile}
		if mode.Mode == "local" {
			if mode.Profile == p.DefaultProfile {
				lines = append(lines, "Default local model; used by the default foreground command.")
			} else {
				lines = append(lines, "Installed alternative; starts only when explicitly chosen.")
			}
		}
		for _, id := range keys(mode.Lock.Records.Layouts) {
			layout := mode.Lock.Records.Layouts[id]
			lines = append(lines, fmt.Sprintf("  %s: context %d tokens (input + output); output default %d", layout.DisplayName, layout.ContextWindowTokens, layout.RequestDefaults.MaxOutputTokens))
			artifact := mode.Lock.Records.Artifacts[layout.Artifact]
			if artifact.Description != "" {
				lines = append(lines, "  "+artifact.Description)
			}
			if artifact.AssessmentURL != "" {
				lines = append(lines, "  Assessment: "+artifact.AssessmentURL)
			}
			if finding := mode.Contexts[id].Finding; finding != nil {
				lines = append(lines, "  Context: tested for this machine and configuration; "+finding.Evidence,
					"  Test limits: engine "+Size(finding.EngineMemoryLimitBytes)+"; swap growth "+Size(finding.SwapGrowthLimitBytes)+". These describe the test, not runtime caps.")
				if finding.LatencyNote != "" {
					lines = append(lines, "  Response time: "+finding.LatencyNote)
				}
			} else {
				lines = append(lines, "  Context fit: unknown for this machine and configuration; the selected window is an explicit choice.")
			}
		}
		for _, layout := range keys(mode.Selection.Templates) {
			template := mode.Selection.Templates[layout]
			if template == "" {
				template = "model's embedded template"
			}
			lines = append(lines, "  "+layout+" — "+template)
		}
		lines = append(lines, "  Model files: "+Size(mode.ModelBytes)+"; memory wall: "+mode.Budget.Status+" (prediction)")
		if mode.RuntimeDiskEstimateBytes > 0 {
			lines = append(lines, "  Splash first-start weight cache: allow roughly another "+Size(mode.RuntimeDiskEstimateBytes)+", plus 2 GiB free. This estimate is additional to installation totals; Splash checks exact space before conversion.")
		}
		sections = append(sections, Section{Title: "Mode · " + mode.Mode, Lines: lines})
	}
	downloads := []string{p.WeightSummary(), "Save downloads no weights. Prepare fetches missing files and verifies cached weights.",
		"Temper cache checked: " + filepath.Join(p.Root, "artifacts", "layouts"),
		"Remaining model/runtime download allowance: " + DownloadSize(p.RemainingDownloadBytes) + ". Software is checked during preparation.",
	}
	if p.HFCache != nil {
		downloads = append(downloads, "Hugging Face cache checked: "+p.HFCache.Root)
		downloads = append(downloads, "Missing models use hf. If needed, uv provisions it; support-tool downloads and cache overhead are additional, unestimated space.")
	}
	sections = append(sections, Section{Title: "Downloads", Lines: downloads, Downloads: p.Downloads}, Section{Title: "Disk space", Lines: []string{
		"Fresh-install disk allowance: " + Size(p.FreshDiskBytes), "Remaining-install disk allowance: " + Size(p.RemainingDiskBytes) + "; free: " + Size(p.FreeDiskBytes),
		"Inspected model files reduce downloads; links share storage, and cross-filesystem copies need installation space. Software allowance stays conservative.",
	}}, Section{Title: "Memory and runtime limits", Lines: []string{
		"KV-cache and engine memory overhead are unmeasured; file sizes do not establish runtime fit.",
		"Larger context windows need more runtime memory. Reduce Context if the selected window exceeds available memory.",
		"GPU allowance counts full model files when offload is enabled; partial-offload savings are unknown.",
		"Foreground serving is a temporary supervised session; an idle engine unload or restart ends it.",
	}}, Section{Title: "What happens next", Lines: []string{
		"Local models run one at a time; the default is the primary launch choice. Utility mode runs separately. Pi retains its own configuration.",
		"Save downloads no models; Prepare installs the listed files without starting a model.",
	}})
	if p.HFCache != nil {
		lines := []string{"Shared model cache: " + p.HFCache.Root, "New cached model files: " + DownloadSize(p.HFCache.RemainingDiskBytes)}
		if p.HFCache.SharedFilesystem {
			lines = append(lines, "Cache and Temper share a filesystem; cache storage is included in the installation disk allowance.")
		} else {
			lines = append(lines, "Cache uses a separate filesystem; cache free space: "+DownloadSize(p.HFCache.FreeDiskBytes))
		}
		if p.HFCache.CopyBytes > 0 {
			lines = append(lines, "Cross-filesystem model copies: "+DownloadSize(p.HFCache.CopyBytes)+"; included in the installation disk allowance.")
		}
		lines = append(lines, "Shared HF cache files are retained when a Temper installation is removed.")
		sections = append(sections, Section{Title: "Shared cache storage", Lines: lines})
	}
	if len(p.Refusals) > 0 {
		var lines []string
		for _, refusal := range p.Refusals {
			lines = append(lines, "Cannot prepare: "+refusal)
		}
		sections = append(sections, Section{Title: "Preparation unavailable", Lines: lines})
	}
	return sections
}

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

func (p Plan) Files() (map[string][]byte, error) {
	if p.Configuration != nil {
		raw, err := p.Configuration.Bytes()
		return map[string][]byte{ConfigurationFile: raw}, err
	}
	files := make(map[string][]byte)
	var locks []catalog.Lock
	for _, mode := range p.Modes {
		locks = append(locks, mode.Lock)
	}
	selected, err := selectedDefault(locks, p.DefaultProfile)
	if err != nil {
		return nil, err
	}
	if selected != p.DefaultProfile {
		return nil, errors.New("plan does not record its default local model")
	}
	for _, mode := range p.Modes {
		if mode.Mode != "local" && mode.Mode != "utility" {
			return nil, errors.New("invalid setup mode")
		}
		if mode.Profile != mode.Selection.Profile {
			return nil, errors.New("plan profile differs from selection")
		}
		if !EqualChoices(mode.Selection, mode.Lock.Selection) {
			return nil, errors.New("selection differs from the exact execution lock")
		}
		if Mode(mode.Lock.Records.Profiles[mode.Selection.Profile]) != mode.Mode {
			return nil, errors.New("execution lock belongs to another mode")
		}
		if err := mode.Selection.Validate(); err != nil {
			return nil, err
		}
		selection, err := json.MarshalIndent(mode.Selection, "", "  ")
		if err != nil {
			return nil, err
		}
		locked, err := catalog.MarshalLock(mode.Lock)
		if err != nil {
			return nil, err
		}
		name := p.ConfigurationName(mode)
		files[name+".selection.json"] = append(selection, '\n')
		files[name+".execution.lock.json"] = locked
	}
	return files, nil
}

func keys[T any](values map[string]T) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	slices.Sort(result)
	return result
}
