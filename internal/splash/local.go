package splash

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"

	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/manifest"
)

// The public API owns the upstream directory format and target/draft checks.
// An explicit draft and models_dir keep resolution and writes in this selection.
const LocalPreparationProgram = `import contextlib,json,pathlib,sys
p=pathlib.Path(sys.executable).resolve().parents[2]
sys.path.insert(0,str(p))
from install.local import prepare_local_model
with contextlib.redirect_stdout(sys.stderr):
 model=prepare_local_model(sys.argv[1],draft=sys.argv[2],language_only=True,models_dir=sys.argv[3])
print(json.dumps(str(model)))
`

type localEntry struct {
	SHA256 string `json:"sha256,omitempty"`
	Link   string `json:"link,omitempty"`
}

// Only the output path and small-file integrity belong here. Splash's model.json
// owns its format; the artifact set owns source weights and their admission.
type localReceipt struct {
	Identity string                `json:"identity"`
	Model    string                `json:"model"`
	Entries  map[string]localEntry `json:"entries"`
}

func (m Material) localInventory() (map[string]localEntry, error) {
	if _, err := directories(m.root, m.path, false); err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, file := range m.set.Files() {
		if !strings.HasPrefix(file.Path, "model/") && !strings.HasPrefix(file.Path, "draft/") {
			continue
		}
		target := filepath.Join(m.set.Path(), filepath.FromSlash(file.Path))
		// Splash resolves parent directories while preserving a cache link's
		// final name (notably /var versus /private/var on macOS).
		parent, err := filepath.EvalSymlinks(filepath.Dir(target))
		if err != nil {
			return nil, err
		}
		allowed[filepath.Join(parent, filepath.Base(target))] = true
	}
	base, err := filepath.EvalSymlinks(m.path)
	if err != nil {
		return nil, err
	}
	entries := map[string]localEntry{}
	err = filepath.WalkDir(m.path, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(m.path, path)
		if rel == "." || rel == "receipt.json" || rel == ".prepare.lock" {
			return nil
		}
		value := localEntry{}
		switch {
		case entry.IsDir():
		case entry.Type()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			absolute := target
			if !filepath.IsAbs(absolute) {
				absolute = filepath.Join(base, filepath.Dir(rel), target)
			}
			absolute = filepath.Clean(absolute)
			inside, _ := filepath.Rel(base, absolute)
			if !allowed[absolute] && (inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) || filepath.IsAbs(inside)) {
				return fmt.Errorf("Splash preparation links an unselected file: %s", rel)
			}
			value.Link = target
		case entry.Type().IsRegular():
			data, err := readRegular(path, 64<<20)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			value.SHA256 = hex.EncodeToString(sum[:])
		default:
			return fmt.Errorf("unexpected Splash preparation entry: %s", rel)
		}
		entries[filepath.ToSlash(rel)] = value
		return nil
	})
	return entries, err
}

func (m Material) localRecord() (localReceipt, error) {
	var r localReceipt
	if err := m.set.Verify(); err != nil {
		return r, err
	}
	if _, err := directories(m.root, m.path, false); err != nil {
		return r, err
	}
	data, err := readRegular(filepath.Join(m.path, "receipt.json"), 1<<20)
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return r, err
	}
	canonical, _ := json.MarshalIndent(r, "", "  ")
	if !bytes.Equal(data, append(canonical, '\n')) || r.Identity != m.key || !fs.ValidPath(r.Model) || r.Model == "." {
		return r, errors.New("Splash preparation receipt differs from selected inputs")
	}
	entries, err := m.localInventory()
	if err != nil {
		return r, err
	}
	if !reflect.DeepEqual(entries, r.Entries) || !preparedDirectory(entries, r.Model) {
		return r, errors.New("Splash prepared files changed")
	}
	return r, nil
}

func preparedDirectory(entries map[string]localEntry, model string) bool {
	entry, exists := entries[model]
	if !exists || entry != (localEntry{}) {
		return false
	}
	for name := range entries {
		if strings.HasPrefix(name, model+"/") {
			return true
		}
	}
	return false
}

// ServerPath reads the verified directory the selected release prepared.
// The renderer receives the concrete path; process ownership never uses a glob.
func (m Material) ServerPath() (string, error) {
	if !m.local {
		return m.path, m.Verify()
	}
	r, err := m.localRecord()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(filepath.Join(m.path, filepath.FromSlash(r.Model)))
}

// PreparedModels resolves local API outputs for callers that are about to render
// an installed selection. Pure compilation uses the deterministic planned paths.
func PreparedModels(root, modeName string, document manifest.Document, locked lockfile.Document) (map[string]string, error) {
	mode, err := document.Mode(modeName)
	if err != nil {
		return nil, err
	}
	paths := map[string]string{}
	members := append(append([]manifest.Member(nil), mode.Members.Resident...), mode.Members.OnDemand...)
	for _, member := range members {
		id := member.Layout
		layout := document.Layouts[id]
		if layout.Splash == nil {
			continue
		}
		m, err := New(root, id, layout, locked.Entries[id], document.Patches)
		if err != nil {
			return nil, err
		}
		if !m.local {
			continue
		}
		path, err := m.ServerPath()
		if err != nil {
			return nil, fmt.Errorf("Splash preparation for %s: %w", id, err)
		}
		paths[id] = path
	}
	return paths, nil
}

func (m Material) prepareLocal(ctx context.Context, python string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := m.set.Verify(); err != nil {
		return err
	}
	if _, err := directories(m.root, m.path, true); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(m.path, ".prepare.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	info, err := lock.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("Splash preparation lock must be a regular file")
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("another Splash preparation is running; retry: %w", err)
	}
	// A receipt is the commit point. Refuse drift instead of asking upstream to
	// repair an admitted directory. Without one its atomic preparation may resume.
	if _, err := os.Lstat(filepath.Join(m.path, "receipt.json")); err == nil {
		return m.Verify()
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if _, err := m.localInventory(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-I", "-B", "-c", LocalPreparationProgram, m.set.ModelPath(), filepath.Join(m.set.Path(), "draft"), filepath.Join(m.path, "models"))
	cmd.WaitDelay = 2 * time.Second
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + m.path, "HF_HUB_OFFLINE=1", "TRANSFORMERS_OFFLINE=1", "PYTHONDONTWRITEBYTECODE=1"}
	var out, stderr limitedOutput
	out.limit, stderr.limit = 64<<10, 64<<10
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("prepare local Splash model: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var model string
	if err := json.Unmarshal(out.Bytes(), &model); err != nil {
		return fmt.Errorf("decode prepared Splash directory: %w", err)
	}
	base, err := filepath.EvalSymlinks(m.path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(base, model)
	if err != nil || !filepath.IsAbs(model) || !fs.ValidPath(filepath.ToSlash(rel)) || rel == "." {
		return errors.New("Splash returned a model directory outside its preparation")
	}
	entries, err := m.localInventory()
	if err != nil {
		return err
	}
	if !preparedDirectory(entries, filepath.ToSlash(rel)) {
		return errors.New("Splash returned no prepared model directory")
	}
	r := localReceipt{Identity: m.key, Model: filepath.ToSlash(rel), Entries: entries}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	stage, err := os.CreateTemp(filepath.Dir(m.path), ".receipt-")
	if err != nil {
		return err
	}
	defer os.Remove(stage.Name())
	_, writeErr := stage.Write(append(data, '\n'))
	closeErr := stage.Close()
	if err := errors.Join(writeErr, closeErr, ctx.Err()); err != nil {
		return err
	}
	if err := os.Link(stage.Name(), filepath.Join(m.path, "receipt.json")); err != nil {
		return err
	}
	return m.Verify()
}
