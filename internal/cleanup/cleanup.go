// Package cleanup finds and removes Temper's own cached material that no saved
// preset and no running layout uses. Planning only reads; Apply removes. The
// Hugging Face cache belongs to the user: cleanup only lists what no saved
// preset uses there.
package cleanup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/temper-sh/temper/internal/artifactset"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/kapitsa"
	"github.com/temper-sh/temper/internal/managed"
	"github.com/temper-sh/temper/internal/preset"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/receiptstore"
	"github.com/temper-sh/temper/internal/splash"
)

// Kinds of removable material, in the order Apply removes them.
const (
	KindSoftware  = "software"  // an engine or router installation
	KindModels    = "models"    // model links of a preset
	KindSplash    = "splash"    // Splash files prepared for a preset
	KindRuntime   = "runtime"   // engine-owned caches and calibration
	KindGenerated = "generated" // old rendered settings and launch records
)

// Item is one directory cleanup removes.
type Item struct {
	Kind string `json:"kind"`
	// Label says in plain words what the item is.
	Label string `json:"label"`
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	// Installation and lock identify a software installation; Apply removes it
	// through the software remover so shared claims stay correct.
	Installation string            `json:"installation,omitempty"`
	lock         lockfile.Document `json:"-"`
}

// Repo is a Hugging Face model repository no saved preset uses.
type Repo struct {
	ID    string `json:"id"`
	Bytes int64  `json:"bytes"`
}

// Plan is what cleanup would do. Kept lists installations it cannot verify
// and therefore leaves in place.
type Plan struct {
	Remove      []Item   `json:"remove"`
	Kept        []string `json:"kept,omitempty"`
	UnusedRepos []Repo   `json:"unused_hf_repos"`
}

// Bytes is the total size Apply frees.
func (p Plan) Bytes() int64 {
	var total int64
	for _, item := range p.Remove {
		total += item.Bytes
	}
	return total
}

// Remover releases one exact software installation.
type Remover interface {
	Remove(ctx context.Context, root, installation string, lock lockfile.Document, out io.Writer) error
}

var digestName = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Build plans a cleanup of root. Material used by a saved preset, or named
// by the current or desired managed job, stays. hfCache may be empty.
func Build(root string, configuration setup.Configuration, state managed.State, hfCache string) (Plan, error) {
	used := map[string]bool{}
	installations := map[string]bool{}
	repos := map[string]bool{}
	for _, p := range configuration.Presets {
		sets, err := preset.Software(p.Lock)
		if err != nil {
			return Plan{}, err
		}
		for _, set := range sets {
			installations[set.ID] = true
		}
		if err := markMaterial(root, p.Lock, used, repos); err != nil {
			return Plan{}, err
		}
	}
	// A running layout may still use material its saved presets no longer
	// name; any path its job mentions stays.
	var mentioned []string
	for _, job := range []*managed.Job{state.Current, state.Desired} {
		if job == nil {
			continue
		}
		mentioned = append(mentioned, job.Router, job.Launcher, job.Config)
		mentioned = append(mentioned, job.StatePaths...)
		for _, p := range job.Processes {
			mentioned = append(mentioned, p.Path, p.Record)
			mentioned = append(mentioned, p.Arguments...)
		}
	}
	inUse := func(path string) bool {
		if used[path] {
			return true
		}
		for _, m := range mentioned {
			if m == path || strings.HasPrefix(m, path+string(filepath.Separator)) || strings.Contains(m, path+string(filepath.Separator)) {
				return true
			}
		}
		return false
	}

	var plan Plan
	installed, err := children(filepath.Join(root, "software", "installations"))
	if err != nil {
		return Plan{}, err
	}
	for _, id := range installed {
		path := filepath.Join(root, "software", "installations", id)
		if installations[id] || inUse(path) {
			continue
		}
		lock, err := lockFromReceipt(root, id)
		if err != nil {
			plan.Kept = append(plan.Kept, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		plan.Remove = append(plan.Remove, Item{Kind: KindSoftware, Label: softwareLabel(lock), Path: path, Bytes: size(path), Installation: id, lock: lock})
	}
	for _, group := range []struct{ kind, label, dir string }{
		{KindModels, "model links of ", filepath.Join(root, "artifacts", "layouts")},
		{KindSplash, "Splash files of ", filepath.Join(root, "prepared", "splash")},
	} {
		presets, err := children(group.dir)
		if err != nil {
			return Plan{}, err
		}
		for _, p := range presets {
			versions, err := children(filepath.Join(group.dir, p))
			if err != nil {
				return Plan{}, err
			}
			for _, v := range versions {
				if path := filepath.Join(group.dir, p, v); !inUse(path) {
					plan.Remove = append(plan.Remove, Item{Kind: group.kind, Label: group.label + p, Path: path, Bytes: size(path)})
				}
			}
		}
	}
	runtime, err := children(filepath.Join(root, "runtime", "splash"))
	if err != nil {
		return Plan{}, err
	}
	for _, v := range runtime {
		if path := filepath.Join(root, "runtime", "splash", v); !inUse(path) {
			plan.Remove = append(plan.Remove, Item{Kind: KindRuntime, Label: "Splash weight cache", Path: path, Bytes: size(path)})
		}
	}
	kapitsaStates, err := children(filepath.Join(root, "runtime", "kapitsa"))
	if err != nil {
		return Plan{}, err
	}
	for _, v := range kapitsaStates {
		if path := filepath.Join(root, "runtime", "kapitsa", v); !inUse(path) {
			plan.Remove = append(plan.Remove, Item{Kind: KindRuntime, Label: "Kapitsa calibration", Path: path, Bytes: size(path)})
		}
	}
	keep := map[string]bool{}
	for _, job := range []*managed.Job{state.Current, state.Desired} {
		if job != nil {
			keep[job.Generation], keep[job.LauncherSHA256] = true, true
		}
	}
	for _, dir := range []string{"generations", "commands", "launchers"} {
		names, err := children(filepath.Join(root, "managed", dir))
		if err != nil {
			return Plan{}, err
		}
		for _, name := range names {
			path := filepath.Join(root, "managed", dir, name)
			if digestName.MatchString(name) && !keep[name] && !inUse(path) {
				plan.Remove = append(plan.Remove, Item{Kind: KindGenerated, Label: "old layout files", Path: path, Bytes: size(path)})
			}
		}
	}
	if hfCache != "" {
		cached, err := children(hfCache)
		if err != nil {
			return Plan{}, err
		}
		for _, name := range cached {
			id, ok := strings.CutPrefix(name, "models--")
			if !ok {
				continue
			}
			id = strings.ReplaceAll(id, "--", "/")
			if !repos[id] {
				plan.UnusedRepos = append(plan.UnusedRepos, Repo{ID: id, Bytes: size(filepath.Join(hfCache, name, "blobs"))})
			}
		}
	}
	return plan, nil
}

// markMaterial records the artifact sets, Splash paths and Hugging Face
// repos (models, drafts, projectors and templates) one saved preset uses.
func markMaterial(root string, lock catalog.Lock, used, repos map[string]bool) error {
	for _, a := range lock.Records.Artifacts {
		for _, s := range a.Sources {
			repos[s.Repo] = true
		}
	}
	for _, t := range lock.Records.Templates {
		for _, s := range t.Sources {
			repos[s.Repo] = true
		}
	}
	p, err := lock.Projections()
	if err != nil {
		return err
	}
	for id, layout := range p.Manifest.Layouts {
		entry := p.Artifacts.Entries[id]
		set, err := artifactset.New(root, id, layout, entry, p.Manifest.Patches)
		if err != nil {
			return err
		}
		used[set.Path()] = true
		if layout.Kapitsa != nil {
			used[kapitsa.StatePath(root, id, entry.Digest(), layout.Kapitsa.SoftwareSHA256)] = true
		}
		if layout.Splash != nil {
			assembly, state := splash.Paths(root, id, entry.Digest(), layout.Splash.SoftwareSHA256)
			used[assembly], used[state] = true, true
		}
	}
	return nil
}

// lockFromReceipt rebuilds the exact software lock an installation was made
// from. Its semantic digest must equal the installation's recorded digest,
// so removal acts on exactly the installed software.
func lockFromReceipt(root, installation string) (lockfile.Document, error) {
	snapshot, err := receiptstore.Read(root, installation)
	if err != nil {
		return lockfile.Document{}, err
	}
	if !snapshot.Exists() {
		return lockfile.Document{}, errors.New("no installation receipt")
	}
	r := snapshot.Document
	lock := lockfile.Document{Schema: lockfile.SchemaV1, Target: r.Target, Selections: map[string]lockfile.Selection{}, Units: r.DesiredUnits()}
	for _, q := range r.Requirements {
		lock.Requires = append(lock.Requires, lockfile.InstallationRequirement{SoftwareLockDigest: q.SoftwareLockDigest})
	}
	var first string
	for id, s := range r.Selections {
		lock.Selections[id] = lockfile.Selection{Provenance: s.Provenance, Method: s.Method, Adapter: s.Adapter, RecipeRevision: s.RecipeRevision, RootUnit: s.RootUnit}
		if first == "" || id < first {
			first = id
		}
	}
	// Provenance says where a request came from and is not part of the
	// digest; removal needs only a valid one.
	lock.Provenance.Execution = &lockfile.ExecutionIdentity{Schema: "temper-execution-lock/v1", Preset: first}
	digest, err := lock.SemanticDigest()
	if err != nil {
		return lockfile.Document{}, err
	}
	if digest != r.SoftwareLockDigest {
		return lockfile.Document{}, errors.New("receipt does not match its software lock digest")
	}
	return lock, nil
}

// Apply removes the planned items. Software goes through remover, so
// shared units stay claimed by their other installations.
func Apply(ctx context.Context, root string, plan Plan, remover Remover) error {
	for _, item := range plan.Remove {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !strings.HasPrefix(item.Path, root+string(filepath.Separator)) {
			return fmt.Errorf("refusing to remove %s outside %s", item.Path, root)
		}
		var err error
		if item.Kind == KindSoftware {
			err = remover.Remove(ctx, root, item.Installation, item.lock, io.Discard)
			if err == nil {
				err = os.RemoveAll(item.Path)
			}
		} else {
			err = os.RemoveAll(item.Path)
		}
		if err != nil {
			return fmt.Errorf("remove %s: %w", item.Path, err)
		}
	}
	return nil
}

// softwareLabel names an installation by its selections and versions.
func softwareLabel(lock lockfile.Document) string {
	var parts []string
	for _, id := range slices.Sorted(maps.Keys(lock.Selections)) {
		part := id
		if unit, ok := lock.Units[lock.Selections[id].RootUnit]; ok && unit.Version != "" {
			part += " " + unit.Version
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ", ")
}

func children(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	return names, nil
}

// size counts regular files below path; links count as nothing.
func size(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}
