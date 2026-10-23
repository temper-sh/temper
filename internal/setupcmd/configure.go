package setupcmd

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/clihelp"
	"github.com/temper-sh/temper/internal/managed"
	"github.com/temper-sh/temper/internal/preset"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/setupui"
)

// Configure serves both initial setup and later edits.
func (c Command) Configure(ctx context.Context, args []string, in io.Reader, out, diagnostics io.Writer) int {
	fail := c.fail
	f := flag.NewFlagSet("temper "+c.verb(), flag.ContinueOnError)
	f.SetOutput(diagnostics)
	rootArg := f.String("root", "", "`PATH` Temper folder (default ~/.temper)")
	path := f.String("catalog", "", "`FILE` local catalog to use as is, without a signature check")
	file := f.String("file", "", "`FILE` JSON configuration that replaces the saved one")
	revision := f.String("revision", "", "`REVISION` the saved configuration's revision; scripted edits need it (see --show)")
	show := f.Bool("show", false, "show the saved presets, layouts and revision")
	resume := f.Bool("resume", false, "use the saved choices again, without reading a new catalog")
	prepare := f.Bool("prepare", false, "download and install after saving, then start the layout")
	dry := f.Bool("dry-run", false, "show what would change; save and install nothing")
	jsonOutput := f.Bool("json", false, "print one JSON document for scripts")
	softwareChoice := f.String("software", "", "`stable|latest` software for every saved preset: the releases the catalog tested, or the newest")
	customID := f.String("as", "", "`ID` save the one selected preset under this ID")
	name := f.String("name", "", "`NAME` display name for the one selected preset")
	var selected, removed, contexts, computes stringsFlag
	f.Var(&selected, "preset", "`ID` catalog preset to select; repeat for several")
	f.Var(&removed, "remove-preset", "`ID` saved preset to remove; no layout may use it")
	f.Var(&contexts, "context", "`PRESET=TOKENS` context window of a selected preset")
	f.Var(&computes, "compute", "`PRESET=cpu|gpu` compute of a selected preset")
	help := clihelp.Help{
		Summary: "Change your saved presets and layouts. Without options it opens the guided setup.",
		Usage: []string{"temper configure", "temper configure --resume --prepare", "temper configure --show [--json]",
			"temper configure --file FILE --revision REVISION", "temper configure --software stable|latest"},
		Examples: []string{"temper configure --resume --prepare   # install saved choices and start the layout"},
	}
	if c.verb() == "init" {
		help = clihelp.Help{
			Summary: "Choose presets for this Mac, install them and start the default layout.\nWithout options it opens the guided setup.",
			Usage:   []string{"temper init", "temper init --preset ID [--preset ID]... [--prepare] [--dry-run] [--json]"},
			Examples: []string{"temper init", "temper init --preset qwen3.5-4b-q4km-off --prepare",
				"temper catalog inspect --root ~/.temper   # list preset IDs"},
		}
	}
	f.Usage = clihelp.Usage(diagnostics, help, f)
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
		if len(selected)+len(removed)+len(contexts)+len(computes) > 0 || *file != "" || *prepare || *resume || *customID != "" || *name != "" {
			return fail(diagnostics, errors.New("--show cannot be combined with edits"))
		}
		if *jsonOutput {
			return c.configurationOutput(out, diagnostics, map[string]any{"revision": rev, "configuration": current})
		}
		renderConfiguration(out, current, rev)
		return 0
	}
	scripted := len(selected)+len(removed) > 0 || *file != ""
	if *resume && scripted {
		return fail(diagnostics, errors.New("--resume uses saved exact inputs; omit edits"))
	}
	if *softwareChoice != "" && *softwareChoice != catalog.SoftwareStable && *softwareChoice != catalog.SoftwareLatest {
		return fail(diagnostics, errors.New("--software must be stable or latest"))
	}
	if *resume && (*path != "" || len(contexts)+len(computes) > 0 || *softwareChoice != "" || *customID != "" || *name != "") {
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
	if len(contexts)+len(computes) > 0 && len(selected) == 0 {
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
		return c.Preview(ctx, root, next)
	}
	interactive := !scripted && !*resume
	if interactive {
		if terminal, ok := in.(*os.File); !ok || !term.IsTerminal(terminal.Fd()) {
			return fail(diagnostics, errors.New("interactive configuration requires a terminal; use --preset, --file, --resume or --show"))
		}
	}
	software := candidate.Software
	if *softwareChoice != "" {
		software = *softwareChoice
	}
	resolutions := map[string]catalog.Resolution{}
	if interactive || len(selected) > 0 || software != candidate.Software {
		view, err := c.Catalog(ctx, root, *path, !*dry)
		if err != nil {
			return fail(diagnostics, err)
		}
		d := view.Document
		// Each preset uses the profile this Mac's memory selects.
		memoryGiB := int(facts.PhysicalMemoryBytes >> 30)
		build := func(id string, choices setup.Choices) (catalog.Lock, error) {
			d, used, err := c.Resolve(ctx, d, id, software)
			if err != nil {
				return catalog.Lock{}, err
			}
			for _, r := range used {
				resolutions[r.Package] = r
			}
			return setup.BuildPreset(d, id, memoryGiB, choices)
		}
		if software != candidate.Software {
			// One router per layout: the setting changes every saved preset's
			// software; models and settings stay.
			for _, id := range slices.Sorted(maps.Keys(candidate.Presets)) {
				if slices.Contains(selected, id) {
					continue
				}
				saved := candidate.Presets[id]
				next, err := c.rebuildSoftware(ctx, saved.Lock, d, software, resolutions)
				if err != nil {
					return fail(diagnostics, fmt.Errorf("preset %s: %w", id, err))
				}
				if !*jsonOutput {
					printChanges(out, id, saved.Lock, next)
				}
				saved.Lock = next
				candidate.Presets[id] = saved
			}
			candidate.Software = software
		}
		if interactive {
			if *jsonOutput {
				return fail(diagnostics, errors.New("use --preset, --file, --resume or --show for scripted configuration"))
			}
			// Discovery happens once outside the terminal state machine. Customizing
			// settings uses the same resolved software, without further network reads.
			input := setupui.PresetInput{Machine: fmt.Sprintf("%s · %s RAM · %s free · catalog of %s", facts.Chip, setup.Size(facts.PhysicalMemoryBytes), setup.Size(free), d.Date), Configuration: candidate, NewConfiguration: rev == "", Groups: d.Groups()}
			if c.Harnesses != nil {
				input.Harnesses, err = c.Harnesses()
				if err != nil {
					return fail(diagnostics, fmt.Errorf("detect harnesses: %w", err))
				}
			}
			input.Assess = func(lock catalog.Lock) (setup.PresetPlan, error) { return setup.Assess(lock, facts) }
			unavailableOption := func(id string, p catalog.PresetPresentation, reason string) setupui.PresetOption {
				group, section := d.Placement(p)
				o := setupui.PresetOption{ID: id, Name: nameOr(p.DisplayName, id), Description: p.Description, Tier: p.MemoryTier, Group: group.ID, Unavailable: reason}
				if section != nil {
					o.Section = section.ID
				}
				return o
			}
			for id, p := range d.Presets {
				// One preset's error marks only that preset unavailable.
				lock, err := build(id, setup.Choices{})
				if err != nil {
					input.Presets = append(input.Presets, unavailableOption(id, p.Presentation, err.Error()))
					continue
				}
				one, err := setup.Assess(lock, facts)
				if err != nil {
					input.Presets = append(input.Presets, unavailableOption(id, p.Presentation, err.Error()))
					continue
				}
				a := d.Artifacts[p.Artifact]
				e := d.Engines[p.Engine]
				group, section := d.Placement(p.Presentation)
				o := setupui.PresetOption{ID: id, Name: displayName(id, p), Model: a.Presentation.ModelName, Weights: a.Presentation.WeightsName, Engine: e.Presentation.DisplayName, Description: p.Presentation.Description, AssessmentURL: p.Presentation.AssessmentURL, Tier: p.Presentation.MemoryTier, Group: group.ID, Lock: lock, ModelBytes: one.ModelBytes, MemoryBytes: one.MemoryBytes, MemoryBasis: one.Memory.Basis, Unavailable: strings.Join(one.Refusals, "; ")}
				if section != nil {
					o.Section = section.ID
				}
				input.Presets = append(input.Presets, o)
			}
			for _, u := range view.Unavailable {
				if u.Kind == "preset" {
					input.Presets = append(input.Presets, unavailableOption(u.ID, u.Presentation, u.Reason))
				}
			}
			printResolutions(diagnostics, resolutions)
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
					o := setupui.PresetOption{ID: id, Name: saved.Name, UserPreset: saved.UserPreset, ForkBaseline: saved.ForkBaseline, Choices: saved.Choices, Model: a.Presentation.ModelName, Weights: a.Presentation.WeightsName, Engine: e.Presentation.DisplayName, Description: p.Presentation.Description, Tier: p.Presentation.MemoryTier, Group: d.FallbackGroup(), Lock: saved.Lock, ModelBytes: one.ModelBytes, MemoryBytes: one.MemoryBytes, MemoryBasis: one.Memory.Basis, Unavailable: strings.Join(one.Refusals, "; "), Details: "Saved exact preset · catalog recommendation does not apply to customized settings"}
					if index >= 0 {
						o.Group, o.Section = input.Presets[index].Group, input.Presets[index].Section
						input.Presets[index] = o
					} else {
						input.Presets = append(input.Presets, o)
					}
				}
			}
			rank := d.PresetRank
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
				fmt.Fprintln(out, "Canceled. Nothing was saved.")
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
			devices, err := values(computes)
			if err != nil {
				return fail(diagnostics, err)
			}
			seen := map[string]bool{}
			for _, id := range selected {
				if seen[id] {
					return fail(diagnostics, fmt.Errorf("preset %q selected twice", id))
				}
				seen[id] = true
				var choices setup.Choices
				if value, ok := windows[id]; ok {
					choices.ContextWindow, err = strconv.Atoi(value)
					if err != nil || choices.ContextWindow <= 0 {
						return fail(diagnostics, fmt.Errorf("invalid context %q", value))
					}
				}
				if device, ok := devices[id]; ok {
					if device != "cpu" && device != "gpu" {
						return fail(diagnostics, fmt.Errorf("invalid compute %q: choose cpu or gpu", device))
					}
					choices.Compute = catalog.ComputeDevice(device)
				}
				if _, ok := d.Presets[id]; !ok {
					if reason := unavailableReason(view, id, ""); reason != "" {
						return fail(diagnostics, fmt.Errorf("preset %q is not available: %s", id, reason))
					}
					return fail(diagnostics, clihelp.UnknownID("preset", id, slices.Collect(maps.Keys(d.Presets)), "temper catalog inspect --root "+root))
				}
				lock, err := build(id, choices)
				if err != nil {
					return fail(diagnostics, err)
				}
				key := id
				display := displayName(id, d.Presets[id])
				if choices != (setup.Choices{}) || *customID != "" {
					display += " (customized)"
				}
				if *customID != "" {
					key = *customID
				}
				if *name != "" {
					display = *name
				}
				if saved, ok := candidate.Presets[key]; ok && !*jsonOutput {
					printChanges(out, key, saved.Lock, lock)
				}
				candidate.Presets[key] = setup.Preset{Name: display, Choices: choices, Lock: lock}
			}

			printResolutions(diagnostics, resolutions)
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
	if rev == "" && !*dry {
		if err := managed.SelectInitialDefault(ctx, root, nextRev); err != nil {
			return fail(diagnostics, fmt.Errorf("configuration saved; initial layout selection incomplete: %w; select explicitly with temper layout activate default", err))
		}
	}
	var prepared []string
	if *prepare && !*dry {
		ids := make([]string, 0, len(candidate.Presets))
		for id := range candidate.Presets {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		progress := diagnostics
		if *jsonOutput {
			progress = io.Discard
		}
		for _, id := range ids {
			if err := preset.Prepare(ctx, root, candidate.Presets[id].Lock, c.Effects, progress); err != nil {
				return fail(diagnostics, fmt.Errorf("configuration saved; preset %s preparation incomplete, resume with --resume --prepare: %w", id, err))
			}
			prepared = append(prepared, id)
		}
	}
	result := map[string]any{"schema": "temper-configuration-result/v1", "revision": nextRev, "changed": changed, "dry_run": *dry, "plan": plan, "prepared": prepared, "software": sortedResolutions(resolutions)}
	if len(prepared) > 0 {
		if !*jsonOutput {
			fmt.Fprintf(out, "Installed %s.\n", count(len(prepared), "preset", "presets"))
		}
		started := c.startAfterInstall(ctx, root, candidate, *jsonOutput, out, result)
		if *jsonOutput {
			_ = c.configurationOutput(out, diagnostics, result)
		}
		if !started {
			return 1
		}
		return 0
	}
	if *jsonOutput {
		return c.configurationOutput(out, diagnostics, result)
	}
	switch {
	case *dry:
		fmt.Fprintln(out, "Dry run: nothing was saved or installed.")
	case changed:
		fmt.Fprintln(out, "Saved your choices. To install them, run: temper configure --resume --prepare")
	default:
		fmt.Fprintln(out, "Nothing changed.")
	}
	return 0
}

// Preview reviews a candidate configuration against this Mac and the
// material already on disk. It saves and downloads nothing.
func (c Command) Preview(ctx context.Context, root string, next setup.Configuration) (setup.Plan, error) {
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
	if c.Downloader != nil {
		material.InspectDownloadClient(c.Downloader)
	}
	if err := material.InspectPresetSoftware(ctx, locks); err != nil {
		return setup.Plan{}, err
	}
	return setup.BuildConfiguration(root, facts, free, next, material)
}

func (c Command) configurationOutput(out, diagnostics io.Writer, value any) int {
	e := json.NewEncoder(out)
	e.SetIndent("", "  ")
	if err := e.Encode(value); err != nil {
		return c.fail(diagnostics, err)
	}
	return 0
}

func nameOr(name, id string) string {
	if name != "" {
		return name
	}
	return id
}

// unavailableReason says why a preset is missing from a catalog view.
func unavailableReason(view catalog.View, id, otherwise string) string {
	for _, u := range view.Unavailable {
		if u.Kind == "preset" && u.ID == id {
			return u.Reason
		}
	}
	return otherwise
}

// displayName is a preset's catalog name, or its ID when the catalog has none.
func displayName(id string, p catalog.Preset) string {
	if p.Presentation.DisplayName != "" {
		return p.Presentation.DisplayName
	}
	return id
}
