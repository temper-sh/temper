// Package setupcmd orchestrates reads, review, one configuration commit, and
// optional exact-lock preparation. The TUI performs no product effects.
package setupcmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/catalog/distribution"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/setupui"
	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter/upstreamrelease"
	"github.com/temper-sh/temper/internal/software/catalogsource"
	"github.com/temper-sh/temper/internal/software/catalogtrust"
)

type Dispatch func(context.Context, []string, io.Writer, io.Writer) int
type CatalogReader func(context.Context, string, string) (catalog.Document, string, error)
type SoftwareResolver func(context.Context, catalog.Document, catalog.Selection, string) (catalog.Document, error)
type UI func(context.Context, io.Reader, io.Writer, setupui.Input, func(context.Context, setupui.Choices) (setupui.Review, error)) (setupui.Decision, error)

type Command struct {
	Detect    func(context.Context) (machine.Facts, error)
	Disk      func(string) (int64, error)
	Home      func() (string, error)
	CacheRoot func() (string, error)
	Catalog   CatalogReader
	Resolve   SoftwareResolver
	UI        UI
	Dispatch  Dispatch
}

func New(detect func(context.Context) (machine.Facts, error), dispatch Dispatch) Command {
	return Command{Detect: detect, Disk: setup.FreeDisk, Home: os.UserHomeDir, Catalog: readCatalog, Resolve: resolveSoftware, UI: setupui.Run, Dispatch: dispatch}
}

type stringsFlag []string

func (s *stringsFlag) String() string         { return strings.Join(*s, ",") }
func (s *stringsFlag) Set(value string) error { *s = append(*s, value); return nil }

type report struct {
	Schema   string     `json:"schema"`
	Plan     setup.Plan `json:"plan"`
	Changed  bool       `json:"changed"`
	DryRun   bool       `json:"dry_run"`
	Prepared []prepared `json:"prepared,omitempty"`
}
type prepared struct {
	Mode       string `json:"mode"`
	Profile    string `json:"profile"`
	Default    bool   `json:"default"`
	Generation string `json:"generation"`
	Command    string `json:"command"`
}

func (c Command) Run(ctx context.Context, args []string, in io.Reader, out, diagnostics io.Writer) int {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	f := flag.NewFlagSet("temper init", flag.ContinueOnError)
	f.SetOutput(diagnostics)
	rootArg := f.String("root", "", "configuration and state root (default ~/.temper)")
	catalogPath := f.String("catalog", "", "explicit local authoring catalog")
	softwareChoice := f.String("software", "recorded", "recorded, latest or tested software")
	defaultProfile := f.String("default-profile", "", "selected local profile to load by default; required with alternatives")
	prepare := f.Bool("prepare", false, "save and prepare explicitly selected configurations")
	resume := f.Bool("resume", false, "use saved exact choices and locks without re-resolving")
	dry := f.Bool("dry-run", false, "preview without filesystem or installation changes")
	jsonOutput := f.Bool("json", false, "emit the noninteractive setup result as JSON")
	var profiles, templates, contexts stringsFlag
	f.Var(&profiles, "profile", "explicit profile ID; repeat to install another model")
	f.Var(&templates, "template", "LAYOUT=PATCH or LAYOUT=builtin; repeat per model")
	f.Var(&contexts, "context", "LAYOUT=TOKENS; otherwise use the largest applicable tested context")
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if f.NArg() != 0 {
		return fail(diagnostics, errors.New("unexpected init arguments"))
	}
	explicitSoftware := false
	f.Visit(func(v *flag.Flag) {
		if v.Name == "software" {
			explicitSoftware = true
		}
	})
	if *resume && (len(profiles) > 0 || len(templates) > 0 || len(contexts) > 0 || *catalogPath != "" || explicitSoftware || *defaultProfile != "") {
		return fail(diagnostics, errors.New("--resume uses saved choices; omit --profile, --default-profile, --template, --context, --catalog and --software"))
	}
	if !*resume && len(profiles) == 0 && (len(templates) > 0 || len(contexts) > 0 || explicitSoftware || *prepare || *jsonOutput || *defaultProfile != "") {
		return fail(diagnostics, errors.New("scripted options require --profile or --resume; interactive init asks for those choices"))
	}
	home := ""
	if *rootArg == "" {
		var err error
		home, err = c.Home()
		if err != nil {
			return fail(diagnostics, err)
		}
	}
	root, err := setup.Root(*rootArg, home)
	if err != nil {
		return fail(diagnostics, err)
	}
	if err := ctx.Err(); err != nil {
		return fail(diagnostics, err)
	}
	facts, err := c.Detect(ctx)
	if err != nil {
		return fail(diagnostics, err)
	}
	free, err := c.Disk(root)
	if err != nil {
		return fail(diagnostics, err)
	}
	var plan setup.Plan
	if *resume {
		saved, err := setup.Load(root)
		if err != nil {
			return fail(diagnostics, err)
		}
		plan, err = c.inspectPlan(root, facts, free, saved.Locks, saved.DefaultProfile)
		if err != nil {
			return fail(diagnostics, err)
		}
	} else {
		d, snapshot, err := c.Catalog(ctx, root, *catalogPath)
		if err != nil {
			return fail(diagnostics, err)
		}
		if len(profiles) > 0 {
			choices, err := scriptChoices(d, profiles, templates, contexts, *softwareChoice)
			if err != nil {
				return fail(diagnostics, err)
			}
			choices.DefaultProfile = *defaultProfile
			plan, err = c.compile(ctx, root, d, snapshot, facts, free, choices)
			if err != nil {
				return fail(diagnostics, err)
			}
		} else {
			if stream, ok := in.(*os.File); ok && !term.IsTerminal(stream.Fd()) {
				return fail(diagnostics, errors.New("interactive setup requires a terminal; use --profile for scripted setup"))
			}
			input, err := options(d, facts, free, *catalogPath != "")
			if err != nil {
				return fail(diagnostics, err)
			}
			if *dry {
				input.Machine += " · dry run"
			}
			var mu sync.Mutex
			plans := map[string]setup.Plan{}
			preview := func(ctx context.Context, choices setupui.Choices) (setupui.Review, error) {
				// A retry must see resource changes made while the wizard is open.
				// Keep reads local: previews may be cancelled and overlap.
				currentFacts, err := c.Detect(ctx)
				if err != nil {
					return setupui.Review{}, err
				}
				currentFree, err := c.Disk(root)
				if err != nil {
					return setupui.Review{}, err
				}
				p, err := c.compile(ctx, root, d, snapshot, currentFacts, currentFree, choices)
				if err != nil {
					var missing *catalog.ContextRequiredError
					if errors.As(err, &missing) {
						return setupui.Review{ContextRequired: &setupui.ContextRequest{Profile: missing.Profile, Layout: missing.Layout}}, nil
					}
					return setupui.Review{}, err
				}
				files, err := p.Files()
				if err != nil {
					return setupui.Review{}, err
				}
				raw, err := json.Marshal(files)
				if err != nil {
					return setupui.Review{}, err
				}
				sum := sha256.Sum256(raw)
				token := hex.EncodeToString(sum[:])
				mu.Lock()
				plans[token] = p
				mu.Unlock()
				var sections []setupui.Section
				for _, section := range p.Sections() {
					block := setupui.Section{Title: section.Title, Lines: section.Lines, Warning: section.Warning}
					if section.Downloads != nil {
						block.Downloads = make([]setupui.Download, 0, len(section.Downloads))
						for _, item := range section.Downloads {
							block.Downloads = append(block.Downloads, setupui.Download{File: item.Name, Size: setup.DownloadSize(item.Bytes), Status: item.Action()})
						}
					}
					sections = append(sections, block)
				}
				return setupui.Review{Token: token, Sections: sections, CanPrepare: p.CanPrepare, PrepareReason: strings.Join(p.Refusals, "; ")}, nil
			}
			decision, err := c.UI(ctx, in, out, input, preview)
			if err != nil {
				return fail(diagnostics, err)
			}
			if decision.Action == "cancel" {
				fmt.Fprintln(out, "Setup cancelled.")
				return 0
			}
			if decision.Action != "save" && decision.Action != "prepare" {
				return fail(diagnostics, errors.New("wizard returned no explicit save or prepare decision"))
			}
			mu.Lock()
			accepted, ok := plans[decision.ReviewToken]
			mu.Unlock()
			if !ok {
				return fail(diagnostics, errors.New("wizard decision has no matching reviewed configuration"))
			}
			plan = accepted
			*prepare = decision.Action == "prepare"
		}
	}
	if *prepare && !*dry {
		// Long reviews may outlive disk or wired-memory observations. Keep the
		// exact reviewed locks and refresh only the machine/resource facts.
		facts, err = c.Detect(ctx)
		if err != nil {
			return fail(diagnostics, err)
		}
		free, err = c.Disk(root)
		if err != nil {
			return fail(diagnostics, err)
		}
		locks := make([]catalog.Lock, 0, len(plan.Modes))
		for _, mode := range plan.Modes {
			locks = append(locks, mode.Lock)
		}
		plan, err = c.inspectPlan(root, facts, free, locks, plan.DefaultProfile)
		if err != nil {
			return fail(diagnostics, err)
		}
	}
	if *prepare && !plan.CanPrepare && !*dry {
		for _, section := range plan.Sections() {
			if section.Warning {
				fmt.Fprintln(diagnostics, section.Title)
				fmt.Fprintln(diagnostics, strings.Join(section.Lines, "\n"))
			}
		}
		return fail(diagnostics, errors.New(strings.Join(plan.Refusals, "; ")))
	}
	changed := false
	if !*resume {
		changed, err = setup.Save(ctx, plan, *dry)
		if err != nil {
			return fail(diagnostics, err)
		}
	}
	result := report{Schema: "temper-setup-plan/v1", Plan: plan, Changed: changed, DryRun: *dry}
	if !*jsonOutput {
		for _, line := range plan.Lines() {
			fmt.Fprintln(out, line)
		}
		if *dry {
			fmt.Fprintln(out, "Dry run: no files or installations changed.")
		} else {
			fmt.Fprintln(out, "Configuration ready at "+filepath.Join(root, setup.ConfigurationDir))
		}
	}
	if *prepare && !*dry {
		preparedLocks, err := os.MkdirTemp("", "temper-setup-locks-")
		if err != nil {
			return fail(diagnostics, fmt.Errorf("configuration saved; prepare exact inputs: %w", err))
		}
		defer os.RemoveAll(preparedLocks)
		for _, mode := range plan.Modes {
			if err := ctx.Err(); err != nil {
				return fail(diagnostics, fmt.Errorf("configuration saved; preparation interrupted: %w", err))
			}
			name := plan.ConfigurationName(mode)
			lockPath := filepath.Join(root, setup.ConfigurationDir, name+".execution.lock.json")
			exactPath := filepath.Join(preparedLocks, name+".execution.lock.json")
			exact, err := catalog.MarshalLock(mode.Lock)
			if err != nil {
				return fail(diagnostics, err)
			}
			if err := os.WriteFile(exactPath, exact, 0o400); err != nil {
				return fail(diagnostics, err)
			}
			var output strings.Builder
			fmt.Fprintln(diagnostics, "Preparing "+mode.Profile+"…")
			code := c.Dispatch(ctx, []string{"execution", "prepare", "--root", root, "--installation", "setup-" + name, "--lock", exactPath}, &output, diagnostics)
			if code != 0 {
				return fail(diagnostics, fmt.Errorf("configuration saved; %s preparation failed (exit %d). Resume with temper init --root %s --resume --prepare", mode.Profile, code, quote(root)))
			}
			var material struct {
				Generation string `json:"generation"`
			}
			if err := json.Unmarshal([]byte(output.String()), &material); err != nil {
				return fail(diagnostics, errors.New("configuration saved; preparation did not report a valid rendered generation"))
			}
			_, decodeErr := hex.DecodeString(material.Generation)
			if decodeErr != nil || len(material.Generation) != 64 {
				return fail(diagnostics, errors.New("configuration saved; preparation did not report a valid rendered generation"))
			}
			statusPath := filepath.Join(root, "setup-"+name+"-"+time.Now().UTC().Format("20060102T150405.000000000Z")+".status.json")
			command := "temper execution serve --root " + quote(root) + " --installation setup-" + name + " --lock " + quote(lockPath) + " --generation " + material.Generation + " --status-file " + quote(statusPath)
			isDefault := mode.Profile == plan.DefaultProfile
			result.Prepared = append(result.Prepared, prepared{Mode: mode.Mode, Profile: mode.Profile, Default: isDefault, Generation: material.Generation, Command: command})
			if !*jsonOutput {
				label := mode.Profile
				if isDefault {
					label += " (default local model)"
				}
				fmt.Fprintln(out, "Prepared "+label+". Run a temporary supervised session with:\n"+command)
				fmt.Fprintln(out, "After first use, an idle engine unload or restart ends the session.")
				fmt.Fprintln(out, "Use a new --status-file path for each subsequent start.")
			}
		}
	} else if !*dry && !*jsonOutput {
		fmt.Fprintf(out, "Prepare later: temper init --root %s --resume --prepare\n", quote(root))
	}
	if *jsonOutput {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			return fail(diagnostics, err)
		}
	}
	return 0
}

func (c Command) compile(ctx context.Context, root string, d catalog.Document, snapshot string, facts machine.Facts, free int64, choices setupui.Choices) (setup.Plan, error) {
	if choices.Software != "recorded" && choices.Software != "latest" && choices.Software != "tested" {
		return setup.Plan{}, errors.New("choose recorded, latest or tested software")
	}
	var locks []catalog.Lock
	for _, choice := range choices.Profiles {
		profile, ok := d.Profiles[choice.Profile]
		if !ok || setup.Mode(profile) != choice.Mode {
			return setup.Plan{}, errors.New("profile does not belong to the selected mode")
		}
		selection, err := catalog.ResolveSelection(d, catalog.Selection{Schema: catalog.SelectionSchema, Profile: choice.Profile, Templates: choice.Templates, ContextWindows: choice.ContextWindows})
		if err != nil {
			return setup.Plan{}, err
		}
		resolved, err := c.Resolve(ctx, d, selection, choices.Software)
		if err != nil {
			return setup.Plan{}, err
		}
		// Template defaults are now explicit, but only caller-supplied context
		// numbers are overrides. Match automatic choices to the resolved release.
		selection.ContextWindows = choice.ContextWindows
		selection, err = catalog.ResolveMachineContexts(resolved, selection, facts)
		if err != nil {
			return setup.Plan{}, err
		}
		locked, err := catalog.Compile(resolved, selection, software.Target{OS: "darwin", Arch: "arm64"})
		if err != nil {
			return setup.Plan{}, err
		}
		if snapshot != "" {
			locked.SourceSnapshotSHA256 = snapshot
		}
		locks = append(locks, locked)
	}
	return c.inspectPlan(root, facts, free, locks, choices.DefaultProfile)
}

func (c Command) inspectPlan(root string, facts machine.Facts, free int64, locks []catalog.Lock, defaultProfile string) (setup.Plan, error) {
	cache := ""
	if c.CacheRoot != nil {
		var err error
		cache, err = c.CacheRoot()
		if err != nil {
			return setup.Plan{}, err
		}
	}
	material, err := setup.InspectModelsWithCache(root, locks, cache)
	if err != nil {
		return setup.Plan{}, err
	}
	return setup.BuildWithMaterial(root, facts, free, locks, material, defaultProfile)
}

func scriptChoices(d catalog.Document, profiles, overrides, contexts []string, softwareChoice string) (setupui.Choices, error) {
	choices := setupui.Choices{Software: softwareChoice}
	templates := map[string]string{}
	for _, raw := range overrides {
		layout, patch, ok := strings.Cut(raw, "=")
		if !ok || layout == "" || patch == "" {
			return choices, errors.New("--template requires LAYOUT=PATCH or LAYOUT=builtin")
		}
		if _, exists := templates[layout]; exists {
			return choices, fmt.Errorf("template repeated for %q", layout)
		}
		if patch == "builtin" {
			patch = ""
		}
		templates[layout] = patch
	}
	windows := map[string]int{}
	for _, raw := range contexts {
		layout, value, ok := strings.Cut(raw, "=")
		tokens, err := strconv.Atoi(value)
		if !ok || layout == "" || err != nil || tokens <= 0 {
			return choices, errors.New("--context requires LAYOUT=TOKENS with a positive integer")
		}
		if _, exists := windows[layout]; exists {
			return choices, fmt.Errorf("context repeated for %q", layout)
		}
		windows[layout] = tokens
	}
	used := map[string]bool{}
	for _, id := range profiles {
		p, ok := d.Profiles[id]
		if !ok {
			return choices, fmt.Errorf("unknown profile %q", id)
		}
		choice := setupui.Choice{Mode: setup.Mode(p), Profile: id, Templates: map[string]string{}, ContextWindows: map[string]int{}}
		for _, b := range p.Bindings {
			used[b.Layout] = true
			if patch, ok := templates[b.Layout]; ok {
				choice.Templates[b.Layout] = patch
			}
			if tokens, ok := windows[b.Layout]; ok {
				choice.ContextWindows[b.Layout] = tokens
			}
		}
		choices.Profiles = append(choices.Profiles, choice)
	}
	for layout := range templates {
		if !used[layout] {
			return choices, fmt.Errorf("template override names unselected layout %q", layout)
		}
	}
	for layout := range windows {
		if !used[layout] {
			return choices, fmt.Errorf("context override names unselected layout %q", layout)
		}
	}
	return choices, nil
}

func options(d catalog.Document, facts machine.Facts, free int64, authoring bool) (setupui.Input, error) {
	input := setupui.Input{Machine: fmt.Sprintf("%s · %s memory · %s free disk", facts.Chip, setup.Size(facts.PhysicalMemoryBytes), setup.Size(free)), Modes: []setupui.Option{
		{ID: "local", Name: "Local", Description: "A selected local model does the main work, including compact chat models."},
		{ID: "utility", Name: "Utility", Description: "Your harness supplies the main model; Temper prepares selected local helpers."},
	}}
	if authoring {
		input.Machine += " · explicitly supplied authoring catalog"
	}
	ids := make([]string, 0, len(d.Profiles))
	for id := range d.Profiles {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	available := map[string]bool{}
	for _, id := range ids {
		p := d.Profiles[id]
		option := setupui.Profile{Option: setupui.Option{ID: id, Name: id}, Mode: setup.Mode(p), SoftwareUnavailable: map[string]string{}}
		var names, components, descriptions, links []string
		for _, binding := range p.Bindings {
			layout := d.Layouts[binding.Layout]
			artifact := d.Artifacts[layout.Artifact]
			modelName := artifact.ModelName
			if modelName == "" {
				modelName = layout.DisplayName
			}
			names = append(names, modelName)
			weightsName := artifact.WeightsName
			if weightsName == "" {
				weightsName = artifact.Repo + " · " + strings.ToUpper(artifact.Format)
			}
			engine := d.Engines[layout.Engine]
			engineName := engine.DisplayName
			if engineName == "" {
				engineName = engine.Family
			}
			components = append(components, "Weights: "+weightsName, "Engine: "+engineName)
			if catalog.MemoryTier(layout.MemoryTier).Rank() > catalog.MemoryTier(option.MemoryTier).Rank() {
				option.MemoryTier = layout.MemoryTier
			}
			if artifact.Description != "" {
				descriptions = append(descriptions, artifact.Description)
			}
			if artifact.AssessmentURL != "" {
				links = append(links, artifact.AssessmentURL)
			}
			window := setupui.ContextWindow{Layout: binding.Layout, Name: layout.DisplayName, Minimum: layout.RequestDefaults.MaxOutputTokens + 1, Maximum: layout.ContextLimit(), RecordedDefaults: map[string]int{}, ManualRequired: len(layout.ContextFindings) == 0}
			t := setupui.Template{Layout: binding.Layout, Name: layout.DisplayName, Options: []setupui.Option{{ID: "", Name: "Model's embedded template"}}}
			if len(layout.Patches) > 0 {
				t.Default = layout.Patches[0]
			}
			patches := make([]string, 0, len(d.Patches))
			for patch := range d.Patches {
				patches = append(patches, patch)
			}
			slices.Sort(patches)
			for _, patch := range patches {
				if slices.Contains(d.Patches[patch].CompatibleArtifacts, layout.Artifact) {
					t.Options = append(t.Options, setupui.Option{ID: patch, Name: patch})
				}
			}
			option.Templates = append(option.Templates, t)
			if d.Engines[layout.Engine].Supply.Release != nil && len(p.Bindings) == 1 {
				for _, template := range t.Options {
					findings, err := catalog.MatchingContexts(d, binding.Layout, template.ID, facts)
					if err != nil {
						return input, err
					}
					if len(findings) > 0 {
						window.RecordedDefaults[template.ID] = findings[0].WindowTokens
					}
				}
			}
			option.Contexts = append(option.Contexts, window)
		}
		option.Name = "Model: " + strings.Join(names, " + ")
		option.Components = strings.Join(components, "\n")
		option.Description = strings.Join(descriptions, "\n")
		option.AssessmentURL = strings.Join(links, "\n")
		selection, err := catalog.ResolveSelection(d, catalog.Selection{Schema: catalog.SelectionSchema, Profile: id})
		if err != nil {
			return input, err
		}
		if err := catalog.ValidateTestedSoftware(d, selection); err != nil {
			option.SoftwareUnavailable["tested"] = err.Error()
		}
		locked, err := catalog.Compile(d, selection, software.Target{OS: "darwin", Arch: "arm64"})
		if err != nil {
			option.SoftwareUnavailable["recorded"] = err.Error()
			option.Details = "Exact software will be resolved during review."
		} else {
			assessment, err := setup.Assess(locked, facts)
			if err != nil {
				return input, err
			}
			option.Details = fmt.Sprintf("Model files %s; memory wall %s (prediction).", setup.Size(assessment.ModelBytes), assessment.Budget.Status)
			if assessment.WiredMemory != nil {
				advice := assessment.WiredMemory.Section()
				option.Advice = &setupui.Section{Title: advice.Title, Lines: advice.Lines, Warning: true}
			}
			option.DisabledReason = strings.Join(assessment.Refusals, "; ")
		}
		if option.DisabledReason == "" {
			available[option.Mode] = true
		}
		input.Profiles = append(input.Profiles, option)
	}
	// Availability and descending capacity group the choices. The author's
	// order ranks layouts only within a group, never as a selection/default.
	rank := func(id string) int {
		best := len(d.LayoutOrder)
		for _, binding := range d.Profiles[id].Bindings {
			if i := slices.Index(d.LayoutOrder, binding.Layout); i >= 0 {
				best = min(best, i)
			}
		}
		return best
	}
	slices.SortStableFunc(input.Profiles, func(a, b setupui.Profile) int {
		if (a.DisabledReason == "") != (b.DisabledReason == "") {
			if a.DisabledReason == "" {
				return -1
			}
			return 1
		}
		if diff := catalog.MemoryTier(b.MemoryTier).Rank() - catalog.MemoryTier(a.MemoryTier).Rank(); diff != 0 {
			return diff
		}
		if diff := rank(a.ID) - rank(b.ID); diff != 0 {
			return diff
		}
		return strings.Compare(a.ID, b.ID)
	})
	for i := range input.Modes {
		if !available[input.Modes[i].ID] {
			input.Modes[i].DisabledReason = "No compatible profile in this catalog fits the current machine's declared limits."
			// A catalog containing only a blocked profile must still show the
			// manual remedy before the user can reach its model screen.
			for _, profile := range input.Profiles {
				if profile.Mode == input.Modes[i].ID && profile.Advice != nil {
					input.Modes[i].Advice = profile.Advice
					break
				}
			}
		}
	}
	return input, nil
}

func readCatalog(ctx context.Context, root, path string) (catalog.Document, string, error) {
	if path != "" {
		info, err := os.Lstat(path)
		if err != nil {
			return catalog.Document{}, "", err
		}
		if !info.Mode().IsRegular() || info.Size() > catalogsource.MaxCatalogBytes {
			return catalog.Document{}, "", errors.New("authoring catalog must be a bounded regular file")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return catalog.Document{}, "", err
		}
		d, err := catalog.Parse(raw)
		return d, "", err
	}
	trust, err := catalogtrust.Production()
	if err != nil {
		return catalog.Document{}, "", err
	}
	snapshot, err := distribution.Read(root, trust)
	if errors.Is(err, distribution.ErrNoCatalog) {
		source, sourceErr := catalogsource.NewProductionHTTPS(&http.Client{Timeout: 30 * time.Second})
		if sourceErr != nil {
			return catalog.Document{}, "", sourceErr
		}
		bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		snapshot, err = distribution.Preview(bounded, root, trust, source)
	}
	if err != nil {
		return catalog.Document{}, "", err
	}
	return snapshot.Document, snapshot.SHA256, nil
}

func resolveSoftware(ctx context.Context, d catalog.Document, s catalog.Selection, choice string) (catalog.Document, error) {
	reader, err := upstreamrelease.NewHTTPReader(&http.Client{Timeout: 5 * time.Minute})
	if err != nil {
		return catalog.Document{}, err
	}
	return catalog.ResolveSoftware(ctx, d, s, choice, reader)
}

func fail(out io.Writer, err error) int { fmt.Fprintf(out, "temper init: %v\n", err); return 1 }
func quote(value string) string         { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
