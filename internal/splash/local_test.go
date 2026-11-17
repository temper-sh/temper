package splash

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/manifest"
)

// Stand in only for the subprocess boundary. These files are deliberately not
// an implementation of Splash's private assembly format and contain no weights.
const localOutput = `mkdir -p "$7"
models=$(cd "$7" && pwd -P)
target=$(cd "$(dirname "$5")" && pwd -P)/$(basename "$5")
draft=$(cd "$6" && pwd -P)
model="$models/.resolved/example"
mkdir -p "$model"
printf 'metadata\n' > "$model/config.json"
ln -sf "$target" "$model/target.gguf"
ln -sf "$draft/model.safetensors" "$model/draft.safetensors"
ln -sf .resolved/example "$models/selection"
printf '"%s"\n' "$model"
`

func localExecutable(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "python")
	script := "#!/bin/sh\nset -eu\n" + `test "$1" = -I
test "$2" = -B
test "$3" = -c
test "$HF_HUB_OFFLINE" = 1
test "$TRANSFORMERS_OFFLINE" = 1
` + body
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLocalPreparationReusesVerifiedOutputWithoutStartingPython(t *testing.T) {
	m := fixtureVersion(t, "1.7.0")
	if err := m.Prepare(context.Background(), localExecutable(t, localOutput), nil); err != nil {
		t.Fatal(err)
	}
	path, err := m.ServerPath()
	if err != nil || !strings.HasSuffix(path, "/models/.resolved/example") {
		t.Fatal(path, err)
	}
	data, err := os.ReadFile(filepath.Join(path, "target.gguf"))
	if err != nil || string(data) != "target" {
		t.Fatal("prepared output did not reference selected target", string(data), err)
	}
	receipt := filepath.Join(m.path, "receipt.json")
	before, _ := os.Stat(receipt)
	if err := m.Prepare(context.Background(), "/must-not-execute", nil); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(receipt)
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("clean retry rewrote admission")
	}
	if _, err := os.Stat(m.state); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preparation created runtime state", err)
	}
	if err := m.PrepareState(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.state, "weights")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("modern preparation created obsolete weight cache", err)
	}
}

func TestLocalPreparationFailureCanResumeBeforeAdmission(t *testing.T) {
	m := fixtureVersion(t, "1.7.0")
	failure := localExecutable(t, "mkdir -p \"$7\"\nprintf 'unfinished' > \"$7/partial\"\nexit 1\n")
	if err := m.Prepare(context.Background(), failure, nil); err == nil {
		t.Fatal("failed preparation admitted")
	}
	if _, err := m.ServerPath(); err == nil {
		t.Fatal("unfinished output became renderable")
	}
	if err := m.Prepare(context.Background(), localExecutable(t, "rm -f \"$7/partial\"\n"+localOutput), nil); err != nil {
		t.Fatal("retry failed", err)
	}
	if err := m.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestLocalPreparationRefusesDriftWithoutRepair(t *testing.T) {
	for _, change := range []string{"metadata", "extra", "link", "source"} {
		t.Run(change, func(t *testing.T) {
			m := fixtureVersion(t, "1.7.0")
			if err := m.Prepare(context.Background(), localExecutable(t, localOutput), nil); err != nil {
				t.Fatal(err)
			}
			path, _ := m.ServerPath()
			var err error
			switch change {
			case "metadata":
				err = os.WriteFile(filepath.Join(path, "config.json"), []byte("changed"), 0600)
			case "extra":
				err = os.WriteFile(filepath.Join(path, "unexpected"), []byte("added"), 0600)
			case "link":
				err = os.Remove(filepath.Join(path, "target.gguf"))
				if err == nil {
					err = os.Symlink("/unselected", filepath.Join(path, "target.gguf"))
				}
			case "source":
				err = os.Remove(m.set.ModelPath())
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := m.Prepare(context.Background(), "/must-not-execute", nil); err == nil {
				t.Fatal("changed output silently repaired")
			}
			if _, err := m.ServerPath(); err == nil {
				t.Fatal("changed output became renderable")
			}
		})
	}
}

func TestLocalPreparationRefusesInvalidOutputs(t *testing.T) {
	for name, body := range map[string]string{
		"outside":         "printf '\"/outside\"\\n'\n",
		"empty":           "mkdir -p \"$7/empty\"\nprintf '\"%s\"\\n' \"$7/empty\"\n",
		"unselected link": "mkdir -p \"$7\"\nln -s /unselected \"$7/link\"\n" + localOutput,
		"invalid output":  "printf 'not a JSON path'\n",
	} {
		t.Run(name, func(t *testing.T) {
			m := fixtureVersion(t, "1.7.0")
			if err := m.Prepare(context.Background(), localExecutable(t, body), nil); err == nil {
				t.Fatal("invalid output admitted")
			}
			if _, err := os.Lstat(filepath.Join(m.path, "receipt.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("failure committed receipt", err)
			}
		})
	}
}

func TestLocalPreparationSerializesAdmissionAndCancelsBeforeCommit(t *testing.T) {
	m := fixtureVersion(t, "1.7.0")
	if _, err := directories(m.root, m.path, true); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(m.path, ".prepare.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if err := m.Prepare(context.Background(), "/must-not-execute", nil); err == nil || !strings.Contains(err.Error(), "another Splash preparation") {
		t.Fatal("concurrent writer not refused", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	// A bounded child verifies that cancellation cannot admit a partial result.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := m.Prepare(ctx, localExecutable(t, "mkdir -p \"$7\"\nexec /bin/sleep 10\n"), nil); err == nil {
		t.Fatal("canceled preparation succeeded")
	}
	if _, err := os.Lstat(filepath.Join(m.path, "receipt.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancellation committed receipt", err)
	}
	if err := m.Prepare(context.Background(), localExecutable(t, localOutput), nil); err != nil {
		t.Fatal("cannot retry after cancellation", err)
	}
}

func TestPreparedModelsIgnoresUnselectedModesWithoutWriting(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent")
	doc := manifest.Document{
		Layouts: map[string]manifest.Layout{"unprepared": {EngineVersion: "1.7.0", Splash: &manifest.SplashTuning{}}},
		Modes:   map[string]manifest.Mode{"other": {}},
	}
	paths, err := PreparedModels(root, "other", doc, lockfile.Document{})
	if err != nil || len(paths) != 0 {
		t.Fatal(paths, err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("inspection created root", err)
	}
}
