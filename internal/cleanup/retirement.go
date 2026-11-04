package cleanup

import (
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/hfcache"
	"github.com/temper-sh/temper/internal/managed"
	"github.com/temper-sh/temper/internal/setup"
	"maps"
	"slices"
	"strings"
)

// Released finds material newly unreferenced by the retired selections. Existing
// unrelated garbage is excluded; current/desired jobs remain protected by Build.
func Released(root string, current setup.Configuration, retired map[string]setup.Preset, previous, state managed.State, cache string) (Plan, error) {
	raw, err := current.Bytes()
	if err != nil {
		return Plan{}, err
	}
	before, err := setup.ParseConfiguration(raw)
	if err != nil {
		return Plan{}, err
	}
	for id, p := range retired {
		before.Presets[id] = p
	}
	old, err := Build(root, before, previous, cache)
	if err != nil {
		return Plan{}, err
	}
	next, err := Build(root, current, state, cache)
	if err != nil {
		return Plan{}, err
	}
	unused := map[string]bool{}
	repos := map[string]bool{}
	for _, item := range old.Remove {
		unused[item.Path] = true
	}
	for _, repo := range old.UnusedRepos {
		repos[repo.ID] = true
	}
	previouslyKept := map[string]bool{}
	for _, item := range old.Kept {
		previouslyKept[item] = true
	}
	result := Plan{}
	for _, item := range next.Kept {
		if !previouslyKept[item] {
			result.Kept = append(result.Kept, item)
		}
	}
	for _, item := range next.Remove {
		if !unused[item.Path] {
			result.Remove = append(result.Remove, item)
		}
	}
	for _, repo := range next.UnusedRepos {
		if !repos[repo.ID] {
			result.UnusedRepos = append(result.UnusedRepos, repo)
		}
	}
	if cache != "" {
		result.UnusedHFFiles = releasedHFFiles(current, retired, state.Current, state.Desired)
	}
	return result, nil
}

// HFFile is a named shared-cache file a retirement may delete with consent.
// Bytes counts one cached revision's copy, not total reclaimable storage;
// deletion spans cached revisions and HF retains blobs shared with other files.
type HFFile struct {
	hfcache.Entry
	Bytes int64 `json:"bytes"`
}

// ReviewHFFiles lists the cached files that consent would let a retirement
// delete. It runs before the switch, so the running job's use of retired
// presets is not protection here; Released adds that protection at cleanup.
func ReviewHFFiles(next setup.Configuration, retired map[string]setup.Preset, cache string) ([]HFFile, error) {
	if cache == "" {
		return nil, nil
	}
	var files []HFFile
	for _, e := range releasedHFFiles(next, retired) {
		copies, err := (hfcache.Cache{Root: cache}).NamedFiles(e)
		if err != nil {
			return nil, err
		}
		if len(copies) > 0 {
			// Show one copy's size, not a sum of potentially shared blobs.
			files = append(files, HFFile{Entry: e, Bytes: copies[0].Size})
		}
	}
	return files, nil
}

// releasedHFFiles names retired presets' files that no remaining selection or
// listed job uses. hf cache rm file URIs address a repository path across
// cached revisions, so every selected use of that path is protected, even at
// a different revision.
func releasedHFFiles(current setup.Configuration, retired map[string]setup.Preset, jobs ...*managed.Job) []hfcache.Entry {
	referenced := map[string]bool{}
	key := func(e hfcache.Entry) string { return e.Repo + "/" + e.Name }
	for _, p := range current.Presets {
		for _, e := range retirementFiles(p.Lock) {
			referenced[key(e)] = true
		}
	}
	for _, job := range jobs {
		if job != nil {
			for _, id := range job.Presets {
				if old, ok := retired[id]; ok {
					for _, e := range retirementFiles(old.Lock) {
						referenced[key(e)] = true
					}
				}
			}
		}
	}
	var files []hfcache.Entry
	for _, id := range slices.Sorted(maps.Keys(retired)) {
		for _, e := range retirementFiles(retired[id].Lock) {
			if k := key(e); !referenced[k] {
				files = append(files, e)
				referenced[k] = true
			}
		}
	}
	slices.SortFunc(files, func(a, b hfcache.Entry) int { return strings.Compare(key(a), key(b)) })
	return files
}

func retirementFiles(lock catalog.Lock) []hfcache.Entry {
	var entries []hfcache.Entry
	add := func(source catalog.Source, files []catalog.File) {
		if source.Repo == "" {
			return
		}
		for _, f := range files {
			entries = append(entries, hfcache.Entry{Repo: source.Repo, Revision: source.Revision, Name: f.Path, SHA256: f.SHA256})
		}
	}
	for _, id := range slices.Sorted(maps.Keys(lock.Records.Artifacts)) {
		a := lock.Records.Artifacts[id]
		add(a.HuggingFace(), a.Files)
	}
	for _, id := range slices.Sorted(maps.Keys(lock.Records.Templates)) {
		t := lock.Records.Templates[id]
		add(t.HuggingFace(), t.Files)
	}
	return entries
}
