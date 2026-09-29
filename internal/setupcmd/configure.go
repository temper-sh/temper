package setupcmd

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/preset"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/setupui"
	"github.com/temper-sh/temper/internal/software"
)

// Configure serves both initial setup and later edits.
func (c Command) Configure(ctx context.Context, args []string, in io.Reader, out, diagnostics io.Writer) int {
	f := flag.NewFlagSet("temper configure", flag.ContinueOnError)
	f.SetOutput(diagnostics)
	rootArg := f.String("root", "", "configuration root (default ~/.temper)")
	path := f.String("catalog", "", "explicit authoring catalog")
	file := f.String("file", "", "complete configuration JSON to review and save")
	revision := f.String("revision", "", "expected saved configuration revision")
	show := f.Bool("show", false, "show current configuration and revision")
	resume := f.Bool("resume", false, "review saved exact inputs offline")
	prepare := f.Bool("prepare", false, "prepare selected exact material after saving")
	dry := f.Bool("dry-run", false, "review without saving or preparing")
	jsonOutput := f.Bool("json", false, "emit JSON")
	softwareChoice := f.String("software", "recorded", "recorded, latest or tested")
	customID := f.String("as", "", "save one selected preset under a custom ID")
	name := f.String("name", "", "custom preset display name")
	var selected, removed, contexts, templates stringsFlag
	f.Var(&selected, "preset", "explicit catalog preset; repeat to select several")
	f.Var(&removed, "remove-preset", "remove an unreferenced preset")
	f.Var(&contexts, "context", "PRESET=TOKENS")
	f.Var(&templates, "template", "PRESET=PATCH or PRESET=builtin")
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if f.NArg() != 0 {
		return fail(diagnostics, errors.New("unexpected configuration arguments"))
	}
	home := ""
	var err error
	if *rootArg == "" {
		home, err = c.Home()
		if err != nil {
			return fail(diagnostics, err)
		}
	}
	root, err := setup.Root(*rootArg, home)
	if err != nil {
		return fail(diagnostics, err)
	}
	current, rev, err := setup.ReadConfiguration(root)
	if err != nil {
		return fail(diagnostics, err)
	}
	if *show {
		if len(selected)+len(removed)+len(contexts)+len(templates) > 0 || *file != "" || *prepare || *resume || *customID != "" || *name != "" {
			return fail(diagnostics, errors.New("--show cannot be combined with edits"))
		}
		return configurationOutput(out, diagnostics, map[string]any{"revision": rev, "configuration": current})
	}
	scripted := len(selected)+len(removed) > 0 || *file != ""
	if *resume && scripted {
		return fail(diagnostics, errors.New("--resume uses saved exact inputs; omit edits"))
	}
	if *resume && (*path != "" || len(contexts)+len(templates) > 0 || *softwareChoice != "recorded" || *customID != "" || *name != "") {
		return fail(diagnostics, errors.New("--resume uses saved exact inputs; omit catalog and preset settings"))
	}
	if *resume && rev == "" {
		return fail(diagnostics, errors.New("no saved configuration to resume"))
	}
	if scripted && rev != "" && *revision != rev {
		return fail(diagnostics, fmt.Errorf("editing existing choices requires --revision %s; inspect with configure --show --json", rev))
	}
	if *revision != "" && *revision != rev {
		return fail(diagnostics, errors.New("configuration revision changed; reload before editing"))
	}
	if *customID != "" && len(selected) != 1 {
		return fail(diagnostics, errors.New("--as requires exactly one --preset"))
	}
	if *name != "" && len(selected) != 1 {
		return fail(diagnostics, errors.New("--name requires exactly one --preset"))
	}
	if len(contexts)+len(templates) > 0 && len(selected) == 0 {
		return fail(diagnostics, errors.New("settings edits require an explicit --preset"))
	}
	if *file != "" && (len(selected)+len(removed) > 0) {
		return fail(diagnostics, errors.New("--file is a complete replacement; omit other edits"))
	}
	candidate := current
	if *file != "" {
		info, err := os.Lstat(*file)
		if err != nil {
			return fail(diagnostics, err)
		}
		if !info.Mode().IsRegular() || info.Size() > 32<<20 {
			return fail(diagnostics, errors.New("--file requires a regular configuration JSON file of at most 32 MiB"))
		}
		raw, err := os.ReadFile(*file)
		if err != nil {
			return fail(diagnostics, err)
		}
		candidate, err = setup.ParseConfiguration(raw)
		if err != nil {
			return fail(diagnostics, err)
		}
	}
	for _, id := range removed {
		if refs := candidate.References(id); len(refs) > 0 {
			return fail(diagnostics, fmt.Errorf("preset %q is used by layouts %s; explicitly correct those layouts first", id, strings.Join(refs, ", ")))
		}
		delete(candidate.Presets, id)
	}
	facts, err := c.Detect(ctx)
	if err != nil {
		return fail(diagnostics, err)
	}
	free, err := c.Disk(root)
	if err != nil {
		return fail(diagnostics, err)
	}
	preview := func(ctx context.Context, next setup.Configuration) (setup.Plan, error) {
		facts, err := c.Detect(ctx)
		if err != nil {
			return setup.Plan{}, err
		}
		free, err := c.Disk(root)
		if err != nil {
			return setup.Plan{}, err
		}
		var locks []catalog.Lock
		for _, p := range next.Presets {
			locks = append(locks, p.Lock)
		}
		var material setup.Material
		if c.CacheRoot != nil {
			cache, err := c.CacheRoot()
			if err != nil {
				return setup.Plan{}, err
			}
			material, err = setup.InspectModelsWithCache(root, locks, cache)
			if err != nil {
				return setup.Plan{}, err
			}
		} else {
			material, err = setup.InspectModels(root, locks)
			if err != nil {
				return setup.Plan{}, err
			}
		}
		if err := material.InspectPresetSoftware(ctx, locks); err != nil {
			return setup.Plan{}, err
		}
		return setup.BuildConfiguration(root, facts, free, next, material)
	}
	interactive := !scripted && !*resume
	if interactive {
		if terminal, ok := in.(*os.File); !ok || !term.IsTerminal(terminal.Fd()) {
			return fail(diagnostics, errors.New("interactive configuration requires a terminal; use --preset, --file, --resume or --show"))
		}
	}
	if interactive || len(selected) > 0 {
		d, _, err := c.Catalog(ctx, root, *path)
		if err != nil {
			return fail(diagnostics, err)
		}
		compile := func(id, template string, window int) (catalog.Lock, error) {
			d, err := c.Resolve(ctx, d, id, *softwareChoice)
			if err != nil {
				return catalog.Lock{}, err
			}
			return catalog.CompilePreset(d, id, template, window, software.Target{OS: "darwin", Arch: "arm64"})
		}
		if interactive {
			if *jsonOutput {
				return fail(diagnostics, errors.New("use --preset, --file, --resume or --show for scripted configuration"))
			}
			if rev == "" {
				candidate.Layouts = setup.StarterLayouts()
			}
			// Discovery happens once outside the terminal state machine. Customizing
			// settings uses the same resolved software, without further network reads.
			input := setupui.PresetInput{Machine: fmt.Sprintf("%s · %s RAM · %s free", facts.Chip, setup.Size(facts.PhysicalMemoryBytes), setup.Size(free)), Configuration: candidate}
			for id, p := range d.Presets {
				lock, err := compile(id, "", 0)
				if err != nil {
					return fail(diagnostics, err)
				}
				one, err := setup.Assess(lock, facts)
				if err != nil {
					return fail(diagnostics, err)
				}
				a := d.Artifacts[p.Artifact]
				e := d.Engines[p.Engine]
				document := lock.Records
				document.Patches = map[string]catalog.Patch{}
				for key, patch := range d.Patches {
					if slices.Contains(patch.CompatibleArtifacts, p.Artifact) {
						patch.CompatibleArtifacts = []string{p.Artifact}
						document.Patches[key] = patch
					}
				}
				o := setupui.PresetOption{ID: id, Name: p.DisplayName, Model: a.ModelName, Weights: a.WeightsName, Engine: e.DisplayName, Description: p.Description, AssessmentURL: p.AssessmentURL, Tier: p.MemoryTier, Recommended: p.Recommended, Lock: lock, Document: document, Unavailable: strings.Join(one.Refusals, "; "), Details: fmt.Sprintf("Model files %s · memory %s (prediction)", setup.Size(one.ModelBytes), one.Budget.Status)}
				for patch, material := range d.Patches {
					if slices.Contains(material.CompatibleArtifacts, p.Artifact) {
						o.Templates = append(o.Templates, patch)
					}
				}
				slices.Sort(o.Templates)
				input.Presets = append(input.Presets, o)
			}
			// Saved exact presets remain editable even after catalog removal or a
			// local rename. Their lock owns their settings, not today's catalog.
			for id, saved := range candidate.Presets {
				index := slices.IndexFunc(input.Presets, func(p setupui.PresetOption) bool { return p.ID == id })
				for _, p := range saved.Lock.Records.Presets {
					a := saved.Lock.Records.Artifacts[p.Artifact]
					e := saved.Lock.Records.Engines[p.Engine]
					one, err := setup.Assess(saved.Lock, facts)
					if err != nil {
						return fail(diagnostics, err)
					}
					o := setupui.PresetOption{ID: id, Name: saved.Name, Model: a.ModelName, Weights: a.WeightsName, Engine: e.DisplayName, Description: p.Description, Tier: p.MemoryTier, Lock: saved.Lock, Document: saved.Lock.Records, Unavailable: strings.Join(one.Refusals, "; "), Details: "Saved exact preset · catalog recommendation does not apply to customized settings"}
					for patch := range saved.Lock.Records.Patches {
						o.Templates = append(o.Templates, patch)
					}
					slices.Sort(o.Templates)
					if index >= 0 {
						o.Recommended = input.Presets[index].Recommended
						input.Presets[index] = o
					} else {
						input.Presets = append(input.Presets, o)
					}
				}
			}
			rank := func(id string) int {
				v := slices.Index(d.PresetOrder, id)
				if v < 0 {
					return len(d.PresetOrder)
				}
				return v
			}
			slices.SortStableFunc(input.Presets, func(a, b setupui.PresetOption) int {
				if (a.Unavailable == "") != (b.Unavailable == "") {
					if a.Unavailable == "" {
						return -1
					}
					return 1
				}
				if v := catalog.MemoryTier(b.Tier).Rank() - catalog.MemoryTier(a.Tier).Rank(); v != 0 {
					return v
				}
				if v := rank(a.ID) - rank(b.ID); v != 0 {
					return v
				}
				return strings.Compare(a.ID, b.ID)
			})
			decision, err := setupui.RunPresets(ctx, in, out, input, preview)
			if err != nil {
				return fail(diagnostics, err)
			}
			if decision.Action == "cancel" {
				fmt.Fprintln(out, "Configuration canceled.")
				return 0
			}
			candidate = decision.Configuration
			*prepare = decision.Action == "prepare"
		} else {
			values := func(args []string) (map[string]string, error) {
				m := map[string]string{}
				for _, s := range args {
					k, v, ok := strings.Cut(s, "=")
					if !ok || !slices.Contains(selected, k) {
						return nil, fmt.Errorf("setting %q must name a selected preset", s)
					}
					if _, ok := m[k]; ok {
						return nil, fmt.Errorf("repeated setting for %s", k)
					}
					m[k] = v
				}
				return m, nil
			}
			windows, err := values(contexts)
			if err != nil {
				return fail(diagnostics, err)
			}
			patches, err := values(templates)
			if err != nil {
				return fail(diagnostics, err)
			}
			seen := map[string]bool{}
			for _, id := range selected {
				if seen[id] {
					return fail(diagnostics, fmt.Errorf("preset %q selected twice", id))
				}
				seen[id] = true
				window := 0
				if value, ok := windows[id]; ok {
					window, err = strconv.Atoi(value)
					if err != nil || window <= 0 {
						return fail(diagnostics, fmt.Errorf("invalid context %q", value))
					}
				}
				lock, err := compile(id, patches[id], window)
				if err != nil {
					return fail(diagnostics, err)
				}
				key := id
				display := d.Presets[id].DisplayName
				if windows[id] != "" || patches[id] != "" || *softwareChoice != "recorded" || *customID != "" {
					display += " (customized)"
				}
				if *customID != "" {
					key = *customID
				}
				if *name != "" {
					display = *name
				}
				candidate.Presets[key] = setup.Preset{Name: display, Lock: lock}
			}
		}
	}
	plan, err := preview(ctx, candidate)
	if err != nil {
		return fail(diagnostics, err)
	}
	if *prepare && !plan.CanPrepare {
		return fail(diagnostics, fmt.Errorf("cannot prepare: %s", strings.Join(plan.Refusals, "; ")))
	}
	if !*jsonOutput {
		for _, line := range plan.Lines() {
			fmt.Fprintln(out, line)
		}
	}
	nextRev, changed, err := setup.SaveConfiguration(ctx, root, candidate, rev, *dry)
	if err != nil {
		return fail(diagnostics, err)
	}
	var prepared []string
	if *prepare && !*dry {
		ids := make([]string, 0, len(candidate.Presets))
		for id := range candidate.Presets {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			if err := preset.Prepare(ctx, root, candidate.Presets[id].Lock, preset.Dispatch(c.Dispatch), diagnostics, diagnostics); err != nil {
				return fail(diagnostics, fmt.Errorf("configuration saved; preset %s preparation incomplete, resume with --resume --prepare: %w", id, err))
			}
			prepared = append(prepared, id)
		}
	}
	if *jsonOutput {
		return configurationOutput(out, diagnostics, map[string]any{"schema": "temper-configuration-result/v1", "revision": nextRev, "changed": changed, "dry_run": *dry, "plan": plan, "prepared": prepared})
	}
	fmt.Fprintf(out, "RESULT configure changed=%t dry_run=%t revision=%s\n", changed, *dry, nextRev)
	return 0
}

func configurationOutput(out, diagnostics io.Writer, value any) int {
	e := json.NewEncoder(out)
	e.SetIndent("", "  ")
	if err := e.Encode(value); err != nil {
		return fail(diagnostics, err)
	}
	return 0
}
