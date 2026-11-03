package cleanup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/managed"
	"github.com/temper-sh/temper/internal/setup"
)

func TestRetirementCleanupOnlyReleasesItsOwnUnreferencedMaterial(t *testing.T) {
	root := t.TempDir()
	cache := t.TempDir()
	old := compiled(t, savedPreset)
	successor := compiled(t, "gemma-4-e2b-q4km-off")
	oldPreset := setup.Preset{Name: "Old", Lock: old}
	next := setup.EmptyConfiguration()
	next.Presets["successor"] = setup.Preset{Name: "Successor", Lock: successor}
	paths := map[string]bool{}
	repos := map[string]bool{}
	if err := markMaterial(root, old, paths, repos); err != nil {
		t.Fatal(err)
	}
	for p := range paths {
		mkdir(t, p)
	}
	for repo := range repos {
		mkdir(t, filepath.Join(cache, "models--"+replaceSlash(repo), "blobs"))
	}
	unrelated := mkdir(t, filepath.Join(root, "runtime", "splash", "unrelated-old-cache"))
	plan, err := Released(root, next, map[string]setup.Preset{"old": oldPreset}, managed.State{}, managed.State{}, cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Remove) == 0 || len(plan.UnusedRepos) == 0 {
		t.Fatal("old material not released", plan)
	}
	for _, item := range plan.Remove {
		if item.Path == unrelated || !paths[item.Path] {
			t.Fatal("retirement swept unrelated garbage", item)
		}
	}
	// Another saved alias still owns the same model/cache, so replacement of
	// one selection cannot reclaim its material or its source repositories.
	next.Presets["keep-alias"] = oldPreset
	shared, err := Released(root, next, map[string]setup.Preset{"old": oldPreset}, managed.State{}, managed.State{}, cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(shared.Remove)+len(shared.UnusedRepos) != 0 {
		t.Fatal("deleted shared material", shared)
	}
	delete(next.Presets, "keep-alias")
	var statePaths []string
	for p := range paths {
		statePaths = append(statePaths, p)
	}
	active := managed.State{Current: &managed.Job{StatePaths: statePaths}}
	protected, err := Released(root, next, map[string]setup.Preset{"old": oldPreset}, active, active, cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(protected.Remove) != 0 {
		t.Fatal("released material used by the current job", protected.Remove)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatal("planning changed disk", err)
	}
}

func replaceSlash(repo string) string {
	for i := 0; i < len(repo); i++ {
		if repo[i] == '/' {
			return repo[:i] + "--" + repo[i+1:]
		}
	}
	return repo
}

func TestRetirementHFDeletionPreservesOtherQuantsAndSelectedRevisions(t *testing.T) {
	root, cache := t.TempDir(), t.TempDir()
	old := compiled(t, savedPreset)
	record := old.Records.Presets[old.Preset]
	artifact := old.Records.Artifacts[record.Artifact]
	source := artifact.HuggingFace()
	file := artifact.Files[0]
	snapshot := filepath.Join(cache, "models--"+replaceSlash(source.Repo), "snapshots", source.Revision, file.Path)
	mkdir(t, filepath.Dir(snapshot))
	if err := os.WriteFile(snapshot, []byte("old quant"), 0600); err != nil {
		t.Fatal(err)
	}
	raw, err := catalog.MarshalLock(old)
	if err != nil {
		t.Fatal(err)
	}
	other, err := catalog.ParseLock(raw)
	if err != nil {
		t.Fatal(err)
	}
	a := other.Records.Artifacts[record.Artifact]
	a.Files[0].Path = "another-quant.gguf"
	other.Records.Artifacts[record.Artifact] = a
	next := setup.EmptyConfiguration()
	next.Presets["different-quant"] = setup.Preset{Name: "Another quant", Lock: other}
	plan, err := Released(root, next, map[string]setup.Preset{"old": {Name: "Old", Lock: old}}, managed.State{}, managed.State{}, cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.UnusedRepos) != 0 || len(plan.UnusedHFFiles) != 1 || plan.UnusedHFFiles[0].Name != file.Path {
		t.Fatal("file deletion swept a shared repository or missed the unused quant", plan)
	}
	// The CLI file target spans cached revisions, so even a selected version
	// with different bytes protects the filename from automatic deletion.
	a.Files[0].Path = file.Path
	a.Files[0].SHA256 = strings.Repeat("b", 64)
	a.Sources[0].Revision = strings.Repeat("b", 40)
	other.Records.Artifacts[record.Artifact] = a
	next.Presets["different-quant"] = setup.Preset{Name: "Other revision", Lock: other}
	plan, err = Released(root, next, map[string]setup.Preset{"old": {Name: "Old", Lock: old}}, managed.State{}, managed.State{}, cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.UnusedHFFiles) != 0 {
		t.Fatal("deleted a selected filename at another revision", plan.UnusedHFFiles)
	}
}
