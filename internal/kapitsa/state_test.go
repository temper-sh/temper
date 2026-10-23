package kapitsa_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/kapitsa"
)

func TestCalibrationIsPreservedIdentifiedAndInspectedWithoutWrites(t *testing.T) {
	root := t.TempDir()
	state := kapitsa.StatePath(root, "qwen", "weights", "engine")
	got, err := kapitsa.Inspect(root, state)
	if err != nil || len(got.Calibrations) != 0 {
		t.Fatal(got, err)
	}
	if _, err := os.Stat(filepath.Join(root, "runtime")); !os.IsNotExist(err) {
		t.Fatal("inspection created state")
	}
	if err := kapitsa.PrepareState(root, state); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(state, "kflash"), 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(state, "kflash", "local.json")
	if err := os.WriteFile(file, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(file)
	if err := kapitsa.PrepareState(root, state); err != nil {
		t.Fatal(err)
	}
	got, err = kapitsa.Inspect(root, state)
	if err != nil || len(got.Calibrations) != 1 || got.Calibrations[0].SHA256 != "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a" {
		t.Fatal(got, err)
	}
	after, _ := os.Stat(file)
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("retry rewrote calibration")
	}
	if kapitsa.StatePath(root, "qwen", "weights", "new-engine") == state {
		t.Fatal("changed engine reused calibration")
	}
}

func TestStateAndCalibrationRefuseSymlinkRedirection(t *testing.T) {
	for _, parent := range []string{"runtime", "calibration"} {
		t.Run(parent, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			state := kapitsa.StatePath(root, "qwen", "weights", "engine")
			if parent == "runtime" {
				if err := os.Symlink(outside, filepath.Join(root, "runtime")); err != nil {
					t.Fatal(err)
				}
				if err := kapitsa.PrepareState(root, state); err == nil {
					t.Fatal("followed redirected state")
				}
			} else {
				if err := kapitsa.PrepareState(root, state); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(state, "kflash")); err != nil {
					t.Fatal(err)
				}
				if _, err := kapitsa.Inspect(root, state); err == nil || !strings.Contains(err.Error(), "real directory") {
					t.Fatal("followed redirected calibration", err)
				}
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatal("wrote outside root", err)
			}
		})
	}
}
