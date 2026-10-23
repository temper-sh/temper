// Package userpreset reads and writes a root's presets.d: one user preset per
// *.json file, named by its ID. Files with other names are never read, so a
// renamed file is off and a template file can sit next to its preset.
package userpreset

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/temper-sh/temper/internal/catalog"
	templatepatch "github.com/temper-sh/temper/internal/patch"
)

// Folder is the name of the user preset folder inside a root.
const Folder = "presets.d"

const maxFile = 1 << 20

// Dir is root's user preset folder.
func Dir(root string) string { return filepath.Join(root, Folder) }

// Path is the file of one user preset.
func Path(root, id string) string { return filepath.Join(Dir(root), id+".json") }

// List returns the IDs of the root's *.json files, sorted. An ID the file
// name cannot be is still listed, so a check can say so.
func List(root string) ([]string, error) {
	entries, err := os.ReadDir(Dir(root))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, entry := range entries {
		if id, ok := strings.CutSuffix(entry.Name(), ".json"); ok && id != "" && !strings.HasPrefix(id, ".") {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// Read returns one preset file; fs.ErrNotExist means the root has none by
// that ID.
func Read(root, id string) ([]byte, error) {
	if err := fileID(id); err != nil {
		return nil, err
	}
	return readFile(Path(root, id))
}

func fileID(id string) error {
	if id == "" || strings.HasPrefix(id, ".") || strings.ContainsAny(id, "/\\\x00\r\n") {
		return fmt.Errorf("%q cannot name a preset file", id)
	}
	return nil
}

// readTemplate reads a local template; preparation reads at most 16 MiB.
func readTemplate(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 16<<20 {
		return nil, fmt.Errorf("%s must be a regular file of at most 16 MiB", path)
	}
	return os.ReadFile(path)
}

func readFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxFile {
		return nil, fmt.Errorf("%s must be a regular file of at most 1 MiB", path)
	}
	return os.ReadFile(path)
}

// Exists reports whether the root has a preset file by that ID.
func Exists(root, id string) (bool, error) {
	_, err := os.Lstat(Path(root, id))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// Create writes a new preset file and never replaces one.
func Create(root, id string, data []byte) error {
	_, err := Save(context.Background(), root, id, nil, data, false)
	return err
}

// Replace writes new contents only if the file still holds previous, so an
// edit saved while Temper worked is not overwritten. The file is compared
// once more just before the rename; an edit saved in that instant can still
// be lost, because editors take no lock.
func Replace(root, id string, previous, data []byte) error {
	_, err := Save(context.Background(), root, id, previous, data, false)
	return err
}

// Save publishes validated bytes with an optimistic edit guard. A nil previous
// means creation. changed is true after publication, even if directory sync
// fails; callers must not describe that outcome as an unsaved draft.
func Save(ctx context.Context, root, id string, previous, data []byte, dry bool) (changed bool, err error) {
	return SaveWithTemplates(ctx, root, id, previous, data, nil, dry)
}

// SaveWithTemplates makes imported bytes durable before publishing the single
// preset reference. Ordinary failures remove only files created by this save;
// a process crash can leave an unreferenced copy, safely reused on retry.
func SaveWithTemplates(ctx context.Context, root, id string, previous, data []byte, templates map[string][]byte, dry bool) (changed bool, err error) {
	if !catalog.ValidPresetID(id) {
		return false, fmt.Errorf("%q is not a preset ID", id)
	}
	if len(data) > maxFile {
		return false, errors.New("preset must be at most 1 MiB")
	}
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return compare(root, id, previous)
	}
	if err := check(); err != nil {
		return false, err
	}
	names := make([]string, 0, len(templates))
	missingTemplate := false
	referenced := map[string]bool{}
	if len(templates) > 0 {
		u, err := catalog.ParseUserPreset(id, data)
		if err != nil {
			return false, err
		}
		for _, name := range u.LocalTemplates() {
			referenced[name] = true
		}
	}
	for name, content := range templates {
		if !referenced[name] || !templatepatch.ValidLocalFile(name) || len(content) == 0 || len(content) > 16<<20 {
			return false, fmt.Errorf("invalid imported template %q", name)
		}
		exists, err := existingTemplate(root, name, content)
		if err != nil {
			return false, err
		}
		missingTemplate = missingTemplate || !exists
		names = append(names, name)
	}
	sort.Strings(names)
	unchangedPreset := bytes.Equal(previous, data)
	if unchangedPreset && !missingTemplate {
		return false, nil
	}
	if dry {
		return true, nil
	}
	if err := os.MkdirAll(Dir(root), 0o700); err != nil {
		return false, err
	}
	unlock, err := lockFiles(root)
	if err != nil {
		return false, err
	}
	defer unlock()
	if err := check(); err != nil {
		return false, err
	}
	temporary, err := write(root, data)
	if err != nil {
		return false, err
	}
	defer os.Remove(temporary)
	created := map[string]os.FileInfo{}
	defer func() {
		if changed {
			return
		}
		for path, own := range created {
			if now, e := os.Lstat(path); e == nil && os.SameFile(own, now) {
				_ = os.Remove(path)
			}
		}
	}()
	for _, name := range names {
		if err := check(); err != nil {
			return false, err
		}
		exists, err := existingTemplate(root, name, templates[name])
		if err != nil {
			return false, err
		}
		if exists {
			continue
		}
		staged, err := write(root, templates[name])
		if err != nil {
			return false, err
		}
		path := filepath.Join(Dir(root), name)
		err = os.Link(staged, path)
		if err == nil {
			created[path], err = os.Lstat(staged)
		}
		_ = os.Remove(staged)
		if err != nil {
			return false, err
		}
	}
	if len(created) > 0 {
		if err := syncDir(Dir(root)); err != nil {
			return false, err
		}
	}
	if err := check(); err != nil {
		return false, err
	}
	if unchangedPreset {
		// The preset already refers to these bytes. Restore a missing managed
		// copy without rewriting the unchanged reference or its modification time.
		return len(created) > 0, nil
	}
	if previous == nil {
		err = os.Link(temporary, Path(root, id))
	} else {
		err = os.Rename(temporary, Path(root, id))
	}
	if err != nil {
		return false, err
	}
	if err := syncDir(Dir(root)); err != nil {
		return true, fmt.Errorf("preset saved, but directory sync failed: %w", err)
	}
	return true, nil
}

func compare(root, id string, previous []byte) error {
	info, err := os.Lstat(Path(root, id))
	if previous == nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		return fmt.Errorf("%s already exists; choose another ID", Path(root, id))
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s must be a regular file", Path(root, id))
	}
	current, err := Read(root, id)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, previous) {
		return fmt.Errorf("%s changed while Temper worked on it; reopen it before saving", Path(root, id))
	}
	return nil
}

// Delete removes only the reviewed preset file. Missing is already done;
// templates, execution locks and model material are outside this operation.
func Delete(ctx context.Context, root, id string, previous []byte, dry bool) (bool, error) {
	if err := fileID(id); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := compare(root, id, previous); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if dry {
		return true, nil
	}
	unlock, err := lockFiles(root)
	if err != nil {
		return false, err
	}
	defer unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := compare(root, id, previous); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if err := os.Remove(Path(root, id)); err != nil {
		return false, err
	}
	if err := syncDir(Dir(root)); err != nil {
		return true, fmt.Errorf("preset deleted, but directory sync failed: %w", err)
	}
	return true, nil
}

func syncDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func write(root string, data []byte) (string, error) {
	f, err := os.CreateTemp(Dir(root), ".temper-preset-*")
	if err != nil {
		return "", err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// Loaded is one user preset resolved against a catalog, ready to compile.
type Loaded struct {
	ID       string
	File     catalog.UserPreset
	Raw      []byte
	Document catalog.Document
	Warnings []string
}

// Load reads, parses and resolves one user preset against a catalog view.
func Load(root, id string, view catalog.View) (Loaded, error) {
	raw, err := Read(root, id)
	if err != nil {
		return Loaded{}, err
	}
	return Resolve(root, id, raw, view)
}

// Resolve parses and resolves one preset file's bytes. Its local template
// files are read now, so the lock records their exact content.
func Resolve(root, id string, raw []byte, view catalog.View) (Loaded, error) {
	return ResolveWithTemplates(root, id, raw, view, nil)
}

// ResolveWithTemplates validates a draft against selected in-memory templates
// without creating a staging directory or changing the user's source files.
func ResolveWithTemplates(root, id string, raw []byte, view catalog.View, templates map[string][]byte) (Loaded, error) {
	u, err := catalog.ParseUserPreset(id, raw)
	if err != nil {
		return Loaded{}, err
	}
	for template, file := range u.LocalTemplates() {
		content, selected := templates[file]
		var err error
		if selected && (len(content) == 0 || len(content) > 16<<20) {
			return Loaded{}, fmt.Errorf("templates[%q]: imported template must be nonempty and at most 16 MiB", template)
		}
		if !selected {
			content, err = readTemplate(filepath.Join(Dir(root), file))
		}
		if err != nil {
			return Loaded{}, fmt.Errorf("templates[%q]: %w", template, err)
		}
		if err := u.ReadLocalTemplate(template, content); err != nil {
			return Loaded{}, err
		}
	}
	d, warnings, err := catalog.ResolveUserPreset(id, u, view)
	if err != nil {
		return Loaded{}, err
	}
	if InCatalog(view, id) {
		warnings = append(warnings, fmt.Sprintf("the catalog now has a preset %q too; your file is used, rename it to use the catalog's", id))
	}
	return Loaded{ID: id, File: u, Raw: raw, Document: d, Warnings: warnings}, nil
}

// InCatalog reports whether the catalog has a preset by that ID, available or not.
func InCatalog(view catalog.View, id string) bool {
	if _, ok := view.Document.Presets[id]; ok {
		return true
	}
	for _, u := range view.Unavailable {
		if u.Kind == "preset" && u.ID == id {
			return true
		}
	}
	return false
}
