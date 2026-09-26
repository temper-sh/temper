package setup_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/setup"
)

func TestInstalledAlternativesPublishAtomicallyAndSaveAgainUnchanged(t *testing.T) {
	d := catalogDocument(t)
	locks := []catalog.Lock{selectedLock(t, d, compactLocal), selectedLock(t, d, largeLocal), selectedLock(t, d, compactUtility)}
	root := filepath.Join(t.TempDir(), "root")
	plan, err := setup.Build(root, facts(32), 100<<30, locks, largeLocal)
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := setup.Save(context.Background(), plan, true); err != nil || !changed {
		t.Fatalf("preview: changed=%v error=%v", changed, err)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatal("multi-model dry run created the root")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if changed, err := setup.Save(ctx, plan, false); err == nil || changed {
		t.Fatal("canceled save committed selections")
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatal("canceled save created the root")
	}
	if changed, err := setup.Save(context.Background(), plan, false); err != nil || !changed {
		t.Fatalf("save: changed=%v error=%v", changed, err)
	}
	if changed, err := setup.Save(context.Background(), plan, false); err != nil || changed {
		t.Fatalf("second save: changed=%v error=%v", changed, err)
	}
	saved, err := setup.Load(root)
	if err != nil || saved.DefaultProfile != largeLocal || len(saved.Locks) != 3 {
		t.Fatalf("resume lost selections: %+v %v", saved, err)
	}
	changedDefault, err := setup.Build(root, facts(32), 100<<30, locks, compactLocal)
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := setup.Save(context.Background(), changedDefault, false); err == nil || changed {
		t.Fatal("a different default overwrote the existing user configuration")
	}
	stillSaved, err := setup.Load(root)
	if err != nil || stillSaved.DefaultProfile != largeLocal {
		t.Fatal("refused save altered the user's default")
	}
}

func TestResumeRefusesIncompleteOrMisnamedAlternatives(t *testing.T) {
	for _, scenario := range []string{"missing default", "missing alternative lock", "misnamed alternative", "duplicated default"} {
		t.Run(scenario, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "root")
			d := catalogDocument(t)
			locks := []catalog.Lock{selectedLock(t, d, compactLocal), selectedLock(t, d, largeLocal)}
			plan, err := setup.Build(root, facts(32), 100<<30, locks, largeLocal)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := setup.Save(context.Background(), plan, false); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, setup.ConfigurationDir)
			switch scenario {
			case "missing default":
				for _, suffix := range []string{".selection.json", ".execution.lock.json"} {
					if err := os.Remove(filepath.Join(path, "local"+suffix)); err != nil {
						t.Fatal(err)
					}
				}
			case "missing alternative lock":
				if err := os.Remove(filepath.Join(path, "local."+compactLocal+".execution.lock.json")); err != nil {
					t.Fatal(err)
				}
			case "misnamed alternative":
				for _, suffix := range []string{".selection.json", ".execution.lock.json"} {
					if err := os.Rename(filepath.Join(path, "local."+compactLocal+suffix), filepath.Join(path, "local.wrong"+suffix)); err != nil {
						t.Fatal(err)
					}
				}
			case "duplicated default":
				for _, suffix := range []string{".selection.json", ".execution.lock.json"} {
					raw, err := os.ReadFile(filepath.Join(path, "local"+suffix))
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(path, "local."+largeLocal+suffix), raw, 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := setup.Load(root); err == nil {
				t.Fatal("resume accepted broken selection/default ownership")
			}
		})
	}
}
