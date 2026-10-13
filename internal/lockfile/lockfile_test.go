package lockfile_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/lockfile"
)

func TestParseRequiresImmutablePins(t *testing.T) {
	_, err := lockfile.Parse([]byte(`schema: temper-lock/v1
entries:
  coder:
    repo: org/Coder
    revision: main
    files:
      - {name: ../coder.gguf, sha256: not-a-hash}
    resolved: yesterday
`))
	if err == nil {
		t.Fatal("Parse succeeded, want invalid lock")
	}
	for _, wanted := range []string{"40-character lowercase commit hash", "not a safe relative path", "64 lowercase hexadecimal", "must be YYYY-MM-DD"} {
		if !strings.Contains(err.Error(), wanted) {
			t.Errorf("error does not contain %q: %v", wanted, err)
		}
	}
}

func TestWithMissingAddsRowsWithoutChangingExistingValues(t *testing.T) {
	existing, err := lockfile.Parse([]byte(validLock))
	if err != nil {
		t.Fatal(err)
	}
	wantCoder := existing.Entries["coder"]
	addition := lockfile.Entry{
		Repo: "org/Reranker", Revision: strings.Repeat("b", 40),
		Files:    []lockfile.File{{Name: "reranker.gguf", SHA256: strings.Repeat("d", 64)}},
		Resolved: "2026-08-20",
	}

	candidate, err := existing.WithMissing(map[string]lockfile.Entry{"reranker": addition})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(candidate.Entries["coder"], wantCoder) {
		t.Fatalf("existing row changed: %#v", candidate.Entries["coder"])
	}
	if !reflect.DeepEqual(candidate.Entries["reranker"], addition) {
		t.Fatalf("addition = %#v, want %#v", candidate.Entries["reranker"], addition)
	}
	if _, exists := existing.Entries["reranker"]; exists {
		t.Fatal("WithMissing mutated its input")
	}

	encoded, err := lockfile.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := lockfile.Parse(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(roundTrip, candidate) {
		t.Fatalf("round trip = %#v, want %#v\n%s", roundTrip, candidate, encoded)
	}
}

func TestWithMissingRefusesToMoveAnExistingPin(t *testing.T) {
	existing, err := lockfile.Parse([]byte(validLock))
	if err != nil {
		t.Fatal(err)
	}
	_, err = existing.WithMissing(map[string]lockfile.Entry{"coder": existing.Entries["coder"]})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("WithMissing error = %v, want existing-row refusal", err)
	}
}

func TestWithReplacementsMovesOnlyExistingRowsWithoutMutatingInput(t *testing.T) {
	existing, err := lockfile.Parse([]byte(validLock))
	if err != nil {
		t.Fatal(err)
	}
	original := existing.Entries["coder"]
	replacement := original
	replacement.Revision = strings.Repeat("b", 40)
	replacement.Files = []lockfile.File{{Name: "coder.gguf", SHA256: strings.Repeat("d", 64)}}
	replacement.Resolved = "2026-08-20"

	candidate, err := existing.WithReplacements(map[string]lockfile.Entry{"coder": replacement})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(candidate.Entries["coder"], replacement) {
		t.Fatalf("replacement = %#v, want %#v", candidate.Entries["coder"], replacement)
	}
	if !reflect.DeepEqual(existing.Entries["coder"], original) {
		t.Fatal("WithReplacements mutated its input")
	}
	if _, err := existing.WithReplacements(map[string]lockfile.Entry{"missing": replacement}); err == nil {
		t.Fatal("WithReplacements accepted an absent row")
	}
}

func TestEntryDigestCoversArtifactIdentityButNotResolutionDateOrOrder(t *testing.T) {
	base := lockfile.Entry{
		Repo: "org/Coder", Revision: strings.Repeat("a", 40),
		Files: []lockfile.File{
			{Name: "b.gguf", SHA256: strings.Repeat("b", 64)},
			{Name: "a.gguf", SHA256: strings.Repeat("a", 64)},
		},
		Patches:  []lockfile.Patch{{Name: "sharp", SHA256: strings.Repeat("c", 64)}},
		Resolved: "2026-08-19",
	}
	reordered := base
	reordered.Files = []lockfile.File{base.Files[1], base.Files[0]}
	reordered.Resolved = "2026-08-20"
	if base.Digest() != reordered.Digest() {
		t.Fatal("digest changed with file order or human-only resolution date")
	}
	changed := base
	changed.Files = append([]lockfile.File(nil), base.Files...)
	changed.Files[0].SHA256 = strings.Repeat("d", 64)
	if base.Digest() == changed.Digest() {
		t.Fatal("digest ignored an artifact hash change")
	}
}

func TestEntryDigestSeparatesFilesFromPatches(t *testing.T) {
	first := lockfile.Entry{
		Repo: "org/Coder", Revision: strings.Repeat("a", 40),
		Files: []lockfile.File{
			{Name: "model.gguf", SHA256: strings.Repeat("b", 64)},
			{Name: "shared", SHA256: strings.Repeat("c", 64)},
		},
	}
	second := lockfile.Entry{
		Repo: "org/Coder", Revision: strings.Repeat("a", 40),
		Files:   []lockfile.File{{Name: "model.gguf", SHA256: strings.Repeat("b", 64)}},
		Patches: []lockfile.Patch{{Name: "shared", SHA256: strings.Repeat("c", 64)}},
	}
	if first.Digest() == second.Digest() {
		t.Fatal("digest did not distinguish the files and patches sections")
	}
}

const validLock = `schema: temper-lock/v1
entries:
  coder:
    repo: org/Coder
    revision: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    files:
      - {name: coder.gguf, sha256: cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc}
    resolved: 2026-08-19
`
