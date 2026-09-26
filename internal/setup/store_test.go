package setup_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/temper-sh/temper/internal/setup"
)

func TestDefaultRootSaveAndResumeExactLocks(t *testing.T) {
	home := t.TempDir()
	root, err := setup.Root("", home)
	if err != nil {
		t.Fatal(err)
	}
	if root != filepath.Join(home, ".temper") {
		t.Fatalf("default root = %q", root)
	}
	plan := planFor(t, root, 16, compactLocal, compactUtility)
	if changed, err := setup.Save(context.Background(), plan, false); err != nil || !changed {
		t.Fatalf("first save: changed=%v, err=%v", changed, err)
	}
	entries, err := os.ReadDir(filepath.Join(root, setup.ConfigurationDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("saved files = %d, want two complete pairs", len(entries))
	}
	saved, err := setup.Load(root)
	locks := saved.Locks
	if err != nil {
		t.Fatal(err)
	}
	if len(locks) != 2 || locks[0].Selection.Profile != compactLocal || locks[1].Selection.Profile != compactUtility {
		t.Fatalf("resumed choices = %+v", locks)
	}
	for i, mode := range plan.Modes {
		if locks[i].Digests.Profile != mode.Lock.Digests.Profile || locks[i].SourceSnapshotSHA256 != mode.Lock.SourceSnapshotSHA256 {
			t.Fatalf("%s resumed a different execution identity", mode.Mode)
		}
	}
	if changed, err := setup.Save(context.Background(), plan, false); err != nil || changed {
		t.Fatalf("identical second save: changed=%v, err=%v", changed, err)
	}
}

func TestDryRunAndPreCanceledSaveDoNotCreateRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "new-root")
	plan := planFor(t, root, 8, compactLocal)
	if changed, err := setup.Save(context.Background(), plan, true); err != nil || !changed {
		t.Fatalf("dry run: changed=%v, err=%v", changed, err)
	}
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry run created root: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if changed, err := setup.Save(ctx, plan, false); !errors.Is(err, context.Canceled) || changed {
		t.Fatalf("pre-canceled save: changed=%v, err=%v", changed, err)
	}
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled save created root: %v", err)
	}
}

func TestEditedSelectionAndLockRemainUntouched(t *testing.T) {
	for _, name := range []string{"selection", "lock"} {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "root")
			plan := planFor(t, root, 16, compactLocal)
			if _, err := setup.Save(context.Background(), plan, false); err != nil {
				t.Fatal(err)
			}
			file := "local.selection.json"
			modified := []byte("{\"schema\":\"temper-selection/v2\",\"profile\":\"qwen3.8-27b-q4xl-local\"}\n")
			if name == "lock" {
				file, modified = "local.execution.lock.json", []byte("{}\n")
			}
			path := filepath.Join(root, setup.ConfigurationDir, file)
			if err := os.WriteFile(path, modified, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := setup.Load(root); err == nil {
				t.Fatal("resume accepted edited configuration")
			}
			if changed, err := setup.Save(context.Background(), plan, false); err == nil || changed {
				t.Fatalf("save replaced edited configuration: changed=%v, err=%v", changed, err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, modified) {
				t.Fatal("user edit was replaced")
			}
		})
	}
}

func TestMismatchedChoiceAndLockNeverCreateRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	plan := planFor(t, root, 16, compactLocal)
	plan.Modes[0].Selection.Profile = largeLocal
	if changed, err := setup.Save(context.Background(), plan, false); err == nil || changed {
		t.Fatalf("mismatched save: changed=%v, err=%v", changed, err)
	}
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("mismatched save created root: %v", err)
	}
}

func TestManifestAndSymlinkBoundariesPreserveExistingFiles(t *testing.T) {
	t.Run("manifest", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "root")
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
		manifest := filepath.Join(root, "manifest.yaml")
		if err := os.WriteFile(manifest, []byte("user: true\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		plan := planFor(t, root, 16, compactLocal)
		if changed, err := setup.Save(context.Background(), plan, false); err == nil || changed {
			t.Fatalf("manifest collision: changed=%v, err=%v", changed, err)
		}
		got, err := os.ReadFile(manifest)
		if err != nil || string(got) != "user: true\n" {
			t.Fatalf("manifest changed: %q, %v", got, err)
		}
	})
	t.Run("root symlink", func(t *testing.T) {
		parent := t.TempDir()
		external := filepath.Join(parent, "external")
		if err := os.Mkdir(external, 0o700); err != nil {
			t.Fatal(err)
		}
		root := filepath.Join(parent, "root")
		if err := os.Symlink(external, root); err != nil {
			t.Fatal(err)
		}
		plan := planFor(t, root, 16, compactLocal)
		if changed, err := setup.Save(context.Background(), plan, false); err == nil || changed {
			t.Fatalf("symlink root accepted: changed=%v, err=%v", changed, err)
		}
		if _, err := setup.Load(root); err == nil {
			t.Fatal("resume followed symlink root")
		}
	})
	t.Run("configuration symlink", func(t *testing.T) {
		parent := t.TempDir()
		root, external := filepath.Join(parent, "root"), filepath.Join(parent, "external")
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(external, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(external, filepath.Join(root, setup.ConfigurationDir)); err != nil {
			t.Fatal(err)
		}
		plan := planFor(t, root, 16, compactLocal)
		if changed, err := setup.Save(context.Background(), plan, false); err == nil || changed {
			t.Fatalf("symlink configuration accepted: changed=%v, err=%v", changed, err)
		}
		if _, err := setup.Load(root); err == nil {
			t.Fatal("resume followed symlink configuration")
		}
	})
}

func TestConcurrentConflictingSavesPublishOneCompleteChoice(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	plans := []setup.Plan{planFor(t, root, 16, compactLocal), planFor(t, root, 16, compactUtility)}
	start := make(chan struct{})
	results := make(chan error, len(plans))
	var wg sync.WaitGroup
	for _, plan := range plans {
		wg.Add(1)
		go func(p setup.Plan) {
			defer wg.Done()
			<-start
			_, err := setup.Save(context.Background(), p, false)
			results <- err
		}(plan)
	}
	close(start)
	wg.Wait()
	close(results)
	var successes int
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful conflicting saves = %d, want one", successes)
	}
	saved, err := setup.Load(root)
	locks := saved.Locks
	if err != nil {
		t.Fatal(err)
	}
	if len(locks) != 1 || (locks[0].Selection.Profile != compactLocal && locks[0].Selection.Profile != compactUtility) {
		t.Fatalf("published incomplete or unexpected choice: %+v", locks)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".setup-") {
			t.Fatalf("partial stage left behind: %s", entry.Name())
		}
	}
	config, err := os.ReadDir(filepath.Join(root, setup.ConfigurationDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(config) != 2 {
		t.Fatalf("published configuration has %d files, want one complete pair", len(config))
	}
}
