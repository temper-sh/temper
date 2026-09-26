package catalog

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/software"
)

// ContextFinding is a reviewed successful test point, not a model-wide maximum.
// Its execution identity prevents a software or tuning update from inheriting
// evidence for different inputs. Raw measurements stay with the cited producer.
type ContextFinding struct {
	WindowTokens           int            `yaml:"window_tokens" json:"window_tokens"`
	MaxOutputTokens        int            `yaml:"max_output_tokens" json:"max_output_tokens"`
	ExecutionSHA256        string         `yaml:"execution_sha256" json:"execution_sha256"`
	Machine                ContextMachine `yaml:"machine" json:"machine"`
	EngineMemoryLimitBytes int64          `yaml:"engine_memory_limit_bytes" json:"engine_memory_limit_bytes"`
	SwapGrowthLimitBytes   int64          `yaml:"swap_growth_limit_bytes" json:"swap_growth_limit_bytes"`
	Evidence               string         `yaml:"evidence" json:"evidence"`
	LatencyNote            string         `yaml:"latency_note,omitempty" json:"latency_note,omitempty"`
}

type ContextMachine struct {
	Target               software.Target `yaml:"target" json:"target"`
	Chip                 string          `yaml:"chip" json:"chip"`
	PhysicalMemoryBytes  int64           `yaml:"physical_memory_bytes" json:"physical_memory_bytes"`
	HardwareModel        string          `yaml:"hardware_model,omitempty" json:"hardware_model,omitempty"`
	MinimumWiredLimitMiB int64           `yaml:"minimum_wired_limit_mib" json:"minimum_wired_limit_mib"`
}

// ContextRequiredError identifies the selection that needs a manual window.
// Callers can request that input without treating missing evidence as a failed
// software lookup or guessing a machine capacity.
type ContextRequiredError struct {
	Profile, Layout, Name string
	Limit                 int
}

func (e *ContextRequiredError) Error() string {
	return fmt.Sprintf("%s: tested context is unknown for this machine and configuration; enter a window on the Context screen or pass --context %s=TOKENS (model limit %d)", e.Name, e.Layout, e.Limit)
}

// ContextLimit keeps issued catalogs' fixed-window behavior while allowing new
// catalogs to distinguish an authored configuration from the model's ceiling.
func (l Layout) ContextLimit() int {
	if l.ContextLimitTokens != 0 {
		return l.ContextLimitTokens
	}
	return l.ContextWindowTokens
}

func (l Layout) validateContext() error {
	if l.ContextLimitTokens < 0 || l.ContextLimit() < l.ContextWindowTokens {
		return errors.New("context limit must be at least the authored context window")
	}
	for _, f := range l.ContextFindings {
		if f.MaxOutputTokens <= 0 || f.WindowTokens <= f.MaxOutputTokens || f.WindowTokens > l.ContextLimit() || !hashPattern.MatchString(f.ExecutionSHA256) {
			return errors.New("context finding needs a bounded window, output allowance and exact execution digest")
		}
		m := f.Machine
		if err := m.Target.Validate(); err != nil {
			return fmt.Errorf("context machine: %w", err)
		}
		if m.Target.OS != "darwin" || m.Target.Arch != "arm64" || strings.TrimSpace(m.Chip) == "" || !plainText(m.Chip) || !plainText(m.HardwareModel) || m.PhysicalMemoryBytes <= 0 || m.MinimumWiredLimitMiB <= 0 || m.MinimumWiredLimitMiB > m.PhysicalMemoryBytes/(1<<20) {
			return errors.New("context finding needs Apple Silicon chip, exact RAM and a bounded wired-memory requirement")
		}
		if f.EngineMemoryLimitBytes <= 0 || f.EngineMemoryLimitBytes > m.PhysicalMemoryBytes || f.SwapGrowthLimitBytes < 0 || !webURL(f.Evidence) || !plainText(f.LatencyNote) {
			return errors.New("context finding needs resource limits, an evidence URL and plain-text latency guidance")
		}
	}
	return nil
}

// ContextExecutionSHA256 identifies a concrete tested layout using the same
// execution material identities as compilation, including the router. It does
// not assert that a test occurred.
func ContextExecutionSHA256(d Document, layoutID, template string, window int) (string, error) {
	if err := d.Validate(); err != nil {
		return "", err
	}
	l, ok := d.Layouts[layoutID]
	if !ok {
		return "", fmt.Errorf("unknown layout %q", layoutID)
	}
	if window <= l.RequestDefaults.MaxOutputTokens || window > l.ContextLimit() {
		return "", errors.New("context window is outside the layout's bounds")
	}
	if d.Schema == Schema && (d.Engines[l.Engine].Supply.Release == nil || d.Runtime.Router.Release == nil) {
		return "", errors.New("context evidence requires resolved software")
	}
	if template != "" {
		p, ok := d.Patches[template]
		if !ok || !slices.Contains(p.CompatibleArtifacts, l.Artifact) {
			return "", errors.New("context template is absent or incompatible")
		}
	}
	d = canonicalDocument(d)
	l = d.Layouts[layoutID]
	l.ContextWindowTokens = window
	l.Patches = []string{}
	if template != "" {
		l.Patches = []string{template}
	}
	router := d.Runtime.Router
	if d.Schema == Schema {
		router.Source, router.Versions = nil, nil
	}
	return digest(struct {
		Kind   string
		Layout string
		Router Supply
	}{"context-execution/v1", layoutExecutionDigest(d, l), router}), nil
}

// MatchingContexts returns only applicable points, largest first. Equal RAM on
// a different chip is not a match. It never interpolates or considers latency
// a capacity threshold. Missing evidence is an empty result, not a guessed cap.
func MatchingContexts(d Document, layoutID, template string, facts machine.Facts) ([]ContextFinding, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if err := facts.Validate(); err != nil {
		return nil, err
	}
	l, ok := d.Layouts[layoutID]
	if !ok {
		return nil, fmt.Errorf("unknown layout %q", layoutID)
	}
	var result []ContextFinding
	for _, f := range l.ContextFindings {
		m := f.Machine
		if !m.Target.Matches(facts.Target) || m.Chip != facts.Chip || m.PhysicalMemoryBytes != facts.PhysicalMemoryBytes || m.HardwareModel != "" && m.HardwareModel != facts.HardwareModel || m.MinimumWiredLimitMiB > facts.WiredLimitMiB || f.MaxOutputTokens != l.RequestDefaults.MaxOutputTokens {
			continue
		}
		execution, err := ContextExecutionSHA256(d, layoutID, template, f.WindowTokens)
		if err != nil {
			return nil, err
		}
		if execution == f.ExecutionSHA256 {
			result = append(result, f)
		}
	}
	slices.SortStableFunc(result, func(a, b ContextFinding) int {
		if a.WindowTokens > b.WindowTokens {
			return -1
		}
		if a.WindowTokens < b.WindowTokens {
			return 1
		}
		return strings.Compare(a.Evidence, b.Evidence)
	})
	return result, nil
}

// ResolveMachineContexts applies reviewed defaults only after software and
// templates are resolved. Explicit numbers remain choices, including untested
// ones. Multi-layout memory composition is not inferred from isolated tests.
func ResolveMachineContexts(d Document, s Selection, facts machine.Facts) (Selection, error) {
	resolved, err := ResolveSelection(d, s)
	if err != nil {
		return Selection{}, err
	}
	if err := facts.Validate(); err != nil {
		return Selection{}, err
	}
	profile := d.Profiles[s.Profile]
	for _, b := range profile.Bindings {
		if _, explicit := s.ContextWindows[b.Layout]; explicit {
			continue
		}
		if len(profile.Bindings) != 1 {
			return Selection{}, errors.New("automatic context needs composition evidence for a multi-layout profile; supply --context for each layout")
		}
		findings, err := MatchingContexts(d, b.Layout, resolved.Templates[b.Layout], facts)
		if err != nil {
			return Selection{}, err
		}
		if len(findings) == 0 {
			layout := d.Layouts[b.Layout]
			return Selection{}, &ContextRequiredError{Profile: s.Profile, Layout: b.Layout, Name: layout.DisplayName, Limit: layout.ContextLimit()}
		}
		resolved.ContextWindows[b.Layout] = findings[0].WindowTokens
	}
	return resolved, nil
}
