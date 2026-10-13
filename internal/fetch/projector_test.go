package fetch_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/fetch"
	"github.com/temper-sh/temper/internal/lockfile"
)

func TestProjectorFetchVerifiesTheWholeSetBeforePublication(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "corrupt projector"}[corrupt], func(t *testing.T) {
			dir := t.TempDir()
			manifestPath, lockPath, entry := writeInputs(t, dir, false, "weights")
			raw, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			raw = []byte(strings.Replace(string(raw), "modalities: [text]", "modalities: [text, image]\n    projector: {repo: owner/projector, format: gguf, files: [mmproj.gguf]}", 1))
			if err := os.WriteFile(manifestPath, raw, 0600); err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256([]byte("projector"))
			entry.Projector = &lockfile.Model{Repo: "owner/projector", Revision: modelRevision, Files: []lockfile.File{{Name: "mmproj.gguf", SHA256: hex.EncodeToString(hash[:])}}}
			raw, err = lockfile.Marshal(lockfile.Document{Schema: lockfile.SchemaV1, Entries: map[string]lockfile.Entry{"coder": entry}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(lockPath, raw, 0600); err != nil {
				t.Fatal(err)
			}
			bytes := "projector"
			if corrupt {
				bytes = "corrupt"
			}
			source := &fakeSource{files: map[string]string{"owner/model@" + modelRevision + "/nested/model.gguf": "weights", "owner/projector@" + modelRevision + "/mmproj.gguf": bytes}}
			options := fetch.Options{Manifest: readManifest(t, manifestPath), Lock: readLock(t, lockPath), Root: filepath.Join(dir, "root"), Layout: "coder", DryRun: true}
			if _, err := runFetch(context.Background(), options, source); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(options.Root); !os.IsNotExist(err) {
				t.Fatal("preview wrote the installation root")
			}
			options.DryRun = false
			result, err := runFetch(context.Background(), options, source)
			set := filepath.Join(options.Root, "artifacts", "layouts", "coder", entry.Digest())
			if corrupt {
				if err == nil {
					t.Fatal("corrupt projector accepted")
				}
				if _, err := os.Stat(set); !os.IsNotExist(err) {
					t.Fatal("partial artifact set published")
				}
				return
			}
			if err != nil || !result.Changed {
				t.Fatalf("fetch: %+v %v", result, err)
			}
			assertFile(t, filepath.Join(set, "projector", "mmproj.gguf"), "projector")
			if result, err := runFetch(context.Background(), options, nil); err != nil || result.Changed {
				t.Fatalf("replay: %+v %v", result, err)
			}
		})
	}
}
