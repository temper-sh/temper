package lockfile_test

import (
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/lockfile"
)

func TestGGUFDraftRefusesUnsafePathsAndIncompletePins(t *testing.T) {
	for _, tc := range []struct{ label, name, revision, hash string }{
		{"parent traversal", "../draft.gguf", strings.Repeat("b", 40), strings.Repeat("c", 64)},
		{"absolute path", "/draft.gguf", strings.Repeat("b", 40), strings.Repeat("c", 64)},
		{"mutable revision", "draft.gguf", "main", strings.Repeat("c", 64)},
		{"invalid hash", "draft.gguf", strings.Repeat("b", 40), "unknown"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			d, err := lockfile.Parse([]byte(validLock))
			if err != nil {
				t.Fatal(err)
			}
			e := d.Entries["coder"]
			e.Draft = &lockfile.Draft{Repo: "org/Assistant", Revision: tc.revision, Files: []lockfile.File{{Name: tc.name, SHA256: tc.hash}}}
			d.Entries["coder"] = e
			if err := d.Validate(); err == nil {
				t.Fatal("unsafe draft accepted")
			}
		})
	}
}
