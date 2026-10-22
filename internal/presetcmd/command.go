// Package presetcmd owns temper preset: your own presets in ROOT/presets.d,
// made and checked as plain files. catalog compile reads them; the guided
// setup does not yet.
package presetcmd

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/catalog/distribution"
	"github.com/temper-sh/temper/internal/clihelp"
	"github.com/temper-sh/temper/internal/datadir"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/catalogtrust"
	"github.com/temper-sh/temper/internal/userpreset"
)

var target = software.Target{OS: "darwin", Arch: "arm64"}

type Command struct {
	Input   io.Reader // Interactive entry only; defaults to stdin in New.
	Home    func() (string, error)
	Catalog func(root string) (catalog.View, error)
	Facts   func(context.Context) (machine.Facts, error)
	Hub     Hub
	// Plan is init's install review of a configuration: downloads, disk and
	// refusals for this Mac. It saves and downloads nothing.
	Plan func(ctx context.Context, root string, next setup.Configuration) (setup.Plan, error)
}

func New(facts func(context.Context) (machine.Facts, error), hub Hub) Command {
	return Command{Input: os.Stdin, Home: os.UserHomeDir, Catalog: rootCatalog, Facts: facts, Hub: hub}
}

// rootCatalog is the root's active signed catalog, as every command reads it.
func rootCatalog(root string) (catalog.View, error) {
	trust, err := catalogtrust.Production()
	if err != nil {
		return catalog.View{}, err
	}
	snapshot, err := distribution.Read(root, trust)
	if err != nil {
		return catalog.View{}, err
	}
	return catalog.View{Document: snapshot.Document, Unavailable: snapshot.Unavailable}, nil
}

var help = map[string]clihelp.Help{
	"fork": {Summary: "Make your own preset that changes some settings of a catalog preset.\nIt follows catalog updates: each compile starts from the newest origin.",
		Usage:    []string{"temper preset fork ORIGIN ID [--name TEXT] [--root PATH] [--json]"},
		Examples: []string{"temper preset fork qwen3.8-27b-q4xl-mtp qwen-cool --name \"Qwen, cooler sampling\""}},
	"copy": {Summary: "Make your own preset with every value of a preset, catalog or yours.\nYou own every value; only new engine builds arrive.",
		Usage:    []string{"temper preset copy SOURCE ID [--name TEXT] [--memory-gib N] [--root PATH] [--json]"},
		Examples: []string{"temper preset copy qwen3.8-27b-q4xl-mtp my-qwen"}},
	"check": {Summary: "Check your presets: read, compile, compare with the origin, and fit on this Mac.\nWithout an ID it checks every preset in ROOT/presets.d.",
		Usage: []string{"temper preset check [ID] [--memory-gib N] [--root PATH] [--json]"}},
	"fields": {Summary: "List the settings a fork of this preset can change, with their current values.",
		Usage: []string{"temper preset fields ID [--memory-gib N] [--root PATH] [--json]"}},
	"plan": {Summary: "Show what installing your preset on this Mac would download, the disk it needs\nand whether Temper can install it here. Nothing is saved or downloaded.",
		Usage: []string{"temper preset plan ID [--root PATH] [--json]"}},
	"weights": {Summary: "Use other weights from Hugging Face in your preset; Temper fills their sizes and SHA-256.\nA fork keeps its model, so the weights must be another build of it (same_model_as).",
		Usage:    []string{"temper preset weights ID REPO[@REVISION] FILE... [--weights-id ID] [--same-model-as ID] [--format FORMAT] [--license TEXT] [--root PATH] [--json]"},
		Examples: []string{"temper preset weights qwen-q6 unsloth/Qwen3.8-27B-GGUF Qwen3.8-27B-Q6_K.gguf", "temper preset weights my-mlx mlx-community/Qwen3.8-27B-4bit '*.safetensors' config.json tokenizer.json --format mlx-safetensors"}},
	"template": {Summary: "Use another chat template in your preset: a file from Hugging Face, your own file\nnext to the preset in ROOT/presets.d, or builtin for the template inside the weights.",
		Usage: []string{"temper preset template ID REPO[@REVISION] FILE [--template-id ID] [--transform NAME] [--license TEXT] [--root PATH] [--json]",
			"temper preset template ID --local FILE.jinja [--template-id ID] [--transform NAME] [--license TEXT] [--root PATH] [--json]",
			"temper preset template ID builtin [--root PATH] [--json]"},
		Examples: []string{"temper preset template qwen-next froggeric/Qwen-Fixed-Chat-Templates chat_template.jinja", "temper preset template qwen-mine --local qwen-mine.jinja"}},
}

// positional counts the arguments each operation takes before its flags.
var positional = map[string][2]int{"fork": {2, 2}, "copy": {2, 2}, "check": {0, 1}, "fields": {1, 1}, "plan": {1, 1}, "weights": {3, -1}, "template": {1, 3}}

func usage(w io.Writer) {
	fmt.Fprintln(w, strings.TrimSpace(`Usage:
  temper preset [--root PATH] [--memory-gib N] [--dry-run]
  temper preset edit ID [--root PATH] [--memory-gib N] [--dry-run]
  temper preset fork ORIGIN ID [--name TEXT]
  temper preset copy SOURCE ID [--name TEXT] [--memory-gib N]
  temper preset check [ID] [--memory-gib N]
  temper preset fields ID [--memory-gib N]
  temper preset plan ID
  temper preset weights ID REPO[@REVISION] FILE... [options]
  temper preset template ID (REPO[@REVISION] FILE | --local FILE.jinja | builtin) [options]
Your presets live in ROOT/presets.d, one ID.json file each. The terminal editor
saves only authoring files; layouts and existing locks stay unchanged.
catalog compile --root ROOT --preset ID builds a lock from a saved preset.
All commands take --root PATH (default ~/.temper). Scripted operations take
--json. Your presets are not tested by the catalog.`))
}

type options struct {
	root      string
	json      bool
	name      string
	memoryGiB int
	// weights and template records
	recordID, sameModelAs, format, license, transform, local string
}

func (c Command) Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "edit" || strings.HasPrefix(args[0], "--") && args[0] != "--help" {
		return c.interactive(ctx, args, stdout, stderr)
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		usage(stdout)
		return 0
	}
	operation := args[0]
	counts, ok := positional[operation]
	if !ok {
		fmt.Fprintf(stderr, "temper preset: unknown operation %q\n\n", operation)
		usage(stderr)
		return 2
	}
	args = args[1:]
	var values []string
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		values, args = append(values, args[0]), args[1:]
	}
	f := flag.NewFlagSet("temper preset "+operation, flag.ContinueOnError)
	f.SetOutput(stderr)
	var o options
	f.StringVar(&o.root, "root", "", "`PATH` Temper folder (default ~/.temper)")
	f.BoolVar(&o.json, "json", false, "print one JSON document for scripts")
	switch operation {
	case "fork", "copy":
		f.StringVar(&o.name, "name", "", "`TEXT` display name (default: the source's, plus \"(yours)\")")
	case "weights":
		f.StringVar(&o.recordID, "weights-id", "", "`ID` for the weights record (default: from the first file name)")
		f.StringVar(&o.sameModelAs, "same-model-as", "", "`ID` of the catalog weights these are another build of (a fork from the origin's repository gets the origin's)")
		f.StringVar(&o.format, "format", "", "`FORMAT` gguf, mlx-safetensors or safetensors (default: gguf for one .gguf file)")
		f.StringVar(&o.license, "license", "", "`TEXT` license (default: the one the model card declares)")
	case "template":
		f.StringVar(&o.recordID, "template-id", "", "`ID` for the template record (default: ID-template)")
		f.StringVar(&o.transform, "transform", "", "`NAME` a Temper template transform to apply")
		f.StringVar(&o.license, "license", "", "`TEXT` license (default: the one the repository card declares)")
		f.StringVar(&o.local, "local", "", "`FILE.jinja` your own template file in ROOT/presets.d, read again at each compile")
	}
	if operation == "copy" || operation == "check" || operation == "fields" {
		f.IntVar(&o.memoryGiB, "memory-gib", 0, "`GIB` build the profile a Mac with this much memory uses (default: this Mac's)")
	}
	f.Usage = clihelp.Usage(stderr, help[operation], f)
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if f.NArg() != 0 || len(values) < counts[0] || counts[1] >= 0 && len(values) > counts[1] || o.memoryGiB < 0 {
		f.Usage()
		return 2
	}
	root, err := c.root(o.root)
	if err != nil {
		return failed(stderr, err)
	}
	o.root = root
	switch operation {
	case "fork":
		err = c.fork(ctx, values[0], values[1], o, stdout)
	case "copy":
		err = c.copy(ctx, values[0], values[1], o, stdout)
	case "check":
		var invalid bool
		invalid, err = c.check(ctx, values, o, stdout)
		if err == nil && invalid {
			return 1
		}
	case "fields":
		err = c.fields(ctx, values[0], o, stdout)
	case "plan":
		err = c.plan(ctx, values[0], o, stdout)
	case "weights":
		err = c.weights(ctx, values[0], values[1], values[2:], o, stdout)
	case "template":
		err = c.template(ctx, values, o, stdout)
	}
	if err != nil {
		return failed(stderr, err)
	}
	return 0
}

func (c Command) root(path string) (string, error) {
	home := ""
	if path == "" {
		var err error
		if home, err = c.Home(); err != nil {
			return "", err
		}
	}
	root, err := setup.Root(path, home)
	if err != nil {
		return "", err
	}
	return root, datadir.Check(root)
}

func (c Command) view(root string) (catalog.View, error) {
	view, err := c.Catalog(root)
	if errors.Is(err, distribution.ErrNoCatalog) {
		return view, fmt.Errorf("%s has no catalog yet; run temper catalog update --root %s", root, root)
	}
	return view, err
}

// memory is the profile size to build: the flag, else this Mac's memory.
func (c Command) memory(ctx context.Context, o options) (int, *machine.Facts, error) {
	facts, err := c.Facts(ctx)
	if err != nil {
		if o.memoryGiB > 0 {
			return o.memoryGiB, nil, nil
		}
		return 0, nil, err
	}
	if o.memoryGiB > 0 {
		return o.memoryGiB, &facts, nil
	}
	return int(facts.PhysicalMemoryBytes >> 30), &facts, nil
}

// newID refuses an ID that cannot name a new preset file in this root.
func newID(root, id string, view catalog.View) error {
	if !catalog.ValidPresetID(id) {
		return fmt.Errorf("%q is not a preset ID: use lowercase letters, digits, dots and dashes", id)
	}
	if userpreset.InCatalog(view, id) {
		return fmt.Errorf("the catalog has a preset %q; catalog IDs mean one thing, so choose another ID", id)
	}
	if exists, err := userpreset.Exists(root, id); err != nil || exists {
		return errors.Join(err, fmt.Errorf("%s already exists; choose another ID", userpreset.Path(root, id)))
	}
	return nil
}

// create checks the new file the same way compile will, then writes it.
func create(root, id string, u catalog.UserPreset, view catalog.View, memoryGiB int) error {
	raw, err := catalog.MarshalUserPreset(u)
	if err != nil {
		return err
	}
	loaded, err := userpreset.Resolve(root, id, raw, view)
	if err != nil {
		return err
	}
	if _, err := catalog.CompilePreset(loaded.Document, id, memoryGiB, 0, target); err != nil {
		return err
	}
	if err := datadir.Ensure(root); err != nil {
		return err
	}
	return userpreset.Create(root, id, raw)
}

func (c Command) fork(ctx context.Context, origin, id string, o options, out io.Writer) error {
	view, err := c.view(o.root)
	if err != nil {
		return err
	}
	if err := newID(o.root, id, view); err != nil {
		return err
	}
	if _, ok := view.Document.Presets[origin]; !ok {
		if mine, _ := userpreset.Exists(o.root, origin); mine {
			return fmt.Errorf("forks start from catalog presets, and %s is yours: copy it with temper preset copy %s %s, or edit its file", origin, origin, id)
		}
		if userpreset.InCatalog(view, origin) {
			_, _, err := catalog.ResolveUserPreset(id, catalog.UserPreset{Schema: catalog.UserPresetSchema, Origin: origin}, view)
			return err
		}
		return clihelp.UnknownID("preset", origin, sortedKeys(view.Document.Presets), "temper catalog inspect --root "+o.root)
	}
	u := catalog.UserPreset{Schema: catalog.UserPresetSchema, Origin: origin, Name: o.name}
	if err := create(o.root, id, u, view, 0); err != nil {
		return err
	}
	if o.json {
		return encode(out, map[string]any{"schema": "temper-preset-write/v1", "preset": id, "form": "fork", "origin": origin, "path": userpreset.Path(o.root, id)})
	}
	fmt.Fprintf(out, "Wrote %s: a fork of %s that follows catalog updates.\n", userpreset.Path(o.root, id), origin)
	fmt.Fprintf(out, "Add your changes under \"changes\" (temper preset fields %s lists them), then run temper preset check %s.\n", id, id)
	return nil
}

func (c Command) copy(ctx context.Context, source, id string, o options, out io.Writer) error {
	view, err := c.view(o.root)
	if err != nil {
		return err
	}
	if err := newID(o.root, id, view); err != nil {
		return err
	}
	memoryGiB, _, err := c.memory(ctx, o)
	if err != nil {
		return err
	}
	d := view.Document
	if mine, err := userpreset.Exists(o.root, source); err != nil {
		return err
	} else if mine {
		loaded, err := userpreset.Load(o.root, source, view)
		if err != nil {
			return fmt.Errorf("your preset %s: %w", source, err)
		}
		d = loaded.Document
	} else if _, ok := d.Presets[source]; !ok {
		return clihelp.UnknownID("preset", source, sortedKeys(d.Presets), "temper catalog inspect --root "+o.root)
	}
	lock, err := catalog.CompilePreset(d, source, memoryGiB, 0, target)
	if err != nil {
		return err
	}
	if err := create(o.root, id, catalog.CopyOf(lock, o.name), view, memoryGiB); err != nil {
		return err
	}
	profile := lock.Records.Presets[source].Profiles[0].MinMemoryGiB
	if o.json {
		return encode(out, map[string]any{"schema": "temper-preset-write/v1", "preset": id, "form": "copy", "source": source, "profile_min_memory_gib": profile, "path": userpreset.Path(o.root, id)})
	}
	fmt.Fprintf(out, "Wrote %s: a copy of %s with its profile for %d GiB Macs.\n", userpreset.Path(o.root, id), source, profile)
	fmt.Fprintf(out, "You own every value; only new engine builds arrive. Check it with temper preset check %s.\n", id)
	return nil
}

type checked struct {
	ID          string                `json:"id"`
	Form        string                `json:"form,omitempty"`
	Origin      string                `json:"origin,omitempty"`
	Name        string                `json:"name,omitempty"`
	Valid       bool                  `json:"valid"`
	Error       string                `json:"error,omitempty"`
	Warnings    []string              `json:"warnings"`
	Changes     []catalog.Change      `json:"changes,omitempty"`
	Profile     int                   `json:"profile_min_memory_gib,omitempty"`
	Context     int                   `json:"context_window_tokens,omitempty"`
	Engine      string                `json:"engine,omitempty"`
	Release     string                `json:"engine_release,omitempty"`
	Fits        *bool                 `json:"fits,omitempty"`
	Refusals    []string              `json:"refusals,omitempty"`
	MemoryBytes int64                 `json:"memory_bytes,omitempty"`
	ModelBytes  int64                 `json:"model_bytes,omitempty"`
	Memory      *setup.MemoryEstimate `json:"memory_estimate,omitempty"`
}

func (c Command) check(ctx context.Context, ids []string, o options, out io.Writer) (bool, error) {
	view, err := c.view(o.root)
	if err != nil {
		return false, err
	}
	if len(ids) == 0 {
		if ids, err = userpreset.List(o.root); err != nil {
			return false, err
		}
	}
	memoryGiB, facts, err := c.memory(ctx, o)
	if err != nil {
		return false, err
	}
	results := []checked{}
	invalid := false
	for _, id := range ids {
		r := checkOne(o.root, id, view, memoryGiB, facts)
		invalid = invalid || !r.Valid
		results = append(results, r)
	}
	if o.json {
		return invalid, encode(out, map[string]any{"schema": "temper-preset-check/v1", "root": o.root, "memory_gib": memoryGiB, "presets": results})
	}
	if len(results) == 0 {
		fmt.Fprintf(out, "No presets in %s yet. Make one with temper preset fork or temper preset copy.\n", userpreset.Dir(o.root))
		return false, nil
	}
	for _, r := range results {
		describe(out, r)
	}
	fmt.Fprintln(out, "Your presets are not tested by the catalog: Temper makes no quality or speed claim for them.")
	return invalid, nil
}

// plan reviews installing one of your presets on this Mac the way init
// reviews catalog presets: downloads, disk and refusals. It saves nothing.
func (c Command) plan(ctx context.Context, id string, o options, out io.Writer) error {
	if c.Plan == nil {
		return errors.New("install planning is unavailable")
	}
	view, err := c.view(o.root)
	if err != nil {
		return err
	}
	loaded, err := userpreset.Load(o.root, id, view)
	if errors.Is(err, fs.ErrNotExist) {
		err = fmt.Errorf("there is no %s", userpreset.Path(o.root, id))
	}
	if err != nil {
		return err
	}
	memoryGiB, _, err := c.memory(ctx, o)
	if err != nil {
		return err
	}
	lock, err := catalog.CompilePreset(loaded.Document, id, memoryGiB, 0, target)
	if err != nil {
		return err
	}
	next := setup.EmptyConfiguration()
	next.Presets[id] = setup.Preset{Name: lock.Records.Presets[id].Presentation.DisplayName, Lock: lock}
	plan, err := c.Plan(ctx, o.root, next)
	if err != nil {
		return err
	}
	if o.json {
		return encode(out, map[string]any{"schema": "temper-preset-plan/v1", "root": o.root, "preset": id, "plan": plan, "warnings": append([]string{}, loaded.Warnings...)})
	}
	for _, w := range loaded.Warnings {
		fmt.Fprintln(out, "Warning: "+w)
	}
	for _, line := range plan.PresetLines() {
		fmt.Fprintln(out, line)
	}
	fmt.Fprintln(out, "Plan only: nothing was saved or downloaded. Install it with temper execution prepare after catalog compile.")
	return nil
}

func checkOne(root, id string, view catalog.View, memoryGiB int, facts *machine.Facts) checked {
	r := checked{ID: id, Warnings: []string{}}
	loaded, err := userpreset.Load(root, id, view)
	if errors.Is(err, fs.ErrNotExist) {
		err = fmt.Errorf("there is no %s", userpreset.Path(root, id))
	}
	if err != nil {
		r.Error = err.Error()
		return r
	}
	return checkLoaded(id, loaded, view, memoryGiB, facts)
}

func checkLoaded(id string, loaded userpreset.Loaded, view catalog.View, memoryGiB int, facts *machine.Facts) checked {
	r := checked{ID: id, Warnings: []string{}}
	r.Warnings = append(r.Warnings, loaded.Warnings...)
	r.Form, r.Origin = "copy", loaded.File.Origin
	if loaded.File.Fork() {
		r.Form = "fork"
	}
	lock, err := catalog.CompilePreset(loaded.Document, id, memoryGiB, 0, target)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.Valid = true
	p := lock.Records.Presets[id]
	r.Name, r.Profile, r.Context, r.Engine = p.Presentation.DisplayName, p.Profiles[0].MinMemoryGiB, p.Window(), p.Engine
	if s := lock.Records.Engines[p.Engine].Supply; s.Release != nil {
		r.Release = s.Release.Version
	}
	if r.Form == "fork" {
		if origin, err := catalog.CompilePreset(view.Document, r.Origin, memoryGiB, 0, target); err == nil {
			r.Changes = catalog.PresetChanges(origin.Records.Presets[r.Origin], p)
		}
	}
	if facts != nil {
		plan, err := setup.Assess(lock, *facts)
		if err != nil {
			r.Warnings = append(r.Warnings, "could not estimate the fit on this Mac: "+err.Error())
			return r
		}
		r.Memory = &plan.Memory
		r.Refusals, r.MemoryBytes, r.ModelBytes = plan.Refusals, plan.MemoryBytes, plan.ModelBytes
		if len(plan.Refusals) > 0 {
			fits := false
			r.Fits = &fits
		}
	}
	return r
}

func describe(out io.Writer, r checked) {
	if !r.Valid {
		fmt.Fprintf(out, "%s: invalid: %s\n", r.ID, r.Error)
		for _, w := range r.Warnings {
			fmt.Fprintf(out, "  Warning: %s\n", w)
		}
		return
	}
	title := r.ID + ": copy"
	if r.Form == "fork" {
		title = r.ID + ": fork of " + r.Origin
	}
	fmt.Fprintf(out, "%s, %q\n", title, r.Name)
	if r.Form == "fork" {
		if len(r.Changes) == 0 {
			fmt.Fprintln(out, "  Changes: none yet; it runs like its origin")
		} else {
			var lines []string
			for _, change := range r.Changes {
				lines = append(lines, fmt.Sprintf("%s %s → %s", change.Path, value(change.From), value(change.To)))
			}
			fmt.Fprintf(out, "  Changes: %s\n", strings.Join(lines, "; "))
		}
	}
	fmt.Fprintf(out, "  Builds for a %d GiB Mac: %d-token context, engine %s %s\n", r.Profile, r.Context, r.Engine, r.Release)
	switch {
	case r.Fits == nil:
		if r.Memory != nil {
			if r.Memory.Basis == "measured" {
				fmt.Fprintln(out, "  This Mac: observed peak is within the budget; fit outside the measured workload is unknown")
			} else {
				fmt.Fprintln(out, "  This Mac: runtime fit unknown; accounted estimate is within the budget")
			}
		}
	default:
		fmt.Fprintf(out, "  This Mac: does not fit: %s\n", strings.Join(r.Refusals, "; "))
	}
	if r.Memory != nil {
		for _, line := range r.Memory.Lines() {
			fmt.Fprintln(out, "  "+line)
		}
	}
	for _, w := range r.Warnings {
		fmt.Fprintf(out, "  Warning: %s\n", w)
	}
}

func value(v any) string {
	if v == nil {
		return "(none)"
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(raw)
}

func (c Command) fields(ctx context.Context, id string, o options, out io.Writer) error {
	view, err := c.view(o.root)
	if err != nil {
		return err
	}
	memoryGiB, _, err := c.memory(ctx, o)
	if err != nil {
		return err
	}
	d := view.Document
	if mine, err := userpreset.Exists(o.root, id); err != nil {
		return err
	} else if mine {
		loaded, err := userpreset.Load(o.root, id, view)
		if err != nil {
			return fmt.Errorf("your preset %s: %w", id, err)
		}
		d = loaded.Document
	} else if _, ok := d.Presets[id]; !ok {
		return clihelp.UnknownID("preset", id, sortedKeys(d.Presets), "temper catalog inspect --root "+o.root)
	}
	p := d.Presets[id]
	p.Profiles = []catalog.Profile{p.SelectProfile(memoryGiB)}
	kind, values := p.Config().Kind, catalog.Values(p)
	type row struct {
		catalog.Field
		Value any `json:"value"`
	}
	var rows []row
	for _, f := range catalog.Fields(kind) {
		rows = append(rows, row{f, values[f.Path]})
	}
	if o.json {
		return encode(out, map[string]any{"schema": "temper-preset-fields/v1", "preset": id, "engine_config_kind": kind, "profile_min_memory_gib": p.Profiles[0].MinMemoryGiB, "fields": rows})
	}
	fmt.Fprintf(out, "Settings a fork of %s can change (%s, profile for %d GiB Macs).\n", id, kind, p.Profiles[0].MinMemoryGiB)
	fmt.Fprintln(out, "Write them under \"changes\" with the same nesting, for example {\"request_defaults\": {\"sampling\": {\"temperature\": 0.7}}}.")
	for _, r := range rows {
		kind := r.Type
		if len(r.Choices) > 0 {
			kind = strings.Join(r.Choices, "|")
		}
		fmt.Fprintf(out, "  %s = %s  (%s) %s\n", r.Path, value(r.Value), kind, r.Help)
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func encode(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func failed(w io.Writer, err error) int {
	fmt.Fprintf(w, "temper: %v\n", err)
	return 1
}
