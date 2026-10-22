package setupcmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/catalog/distribution"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/userpreset"
)

type layoutPresetFile struct {
	raw    []byte
	preset setup.Preset
}

// layoutPresetFolder is a read snapshot, not a selection registry. Files become
// available immediately; only included presets cross the configuration commit.
type layoutPresetFolder struct {
	root     string
	view     catalog.View
	memory   int
	files    map[string]layoutPresetFile
	problems map[string]string
}

func (c Command) readLayoutPresetFolder(ctx context.Context, root string, saved setup.Configuration) (layoutPresetFolder, error) {
	folder := layoutPresetFolder{root: root, files: map[string]layoutPresetFile{}, problems: map[string]string{}}
	ids, err := userpreset.List(root)
	if err != nil {
		return folder, err
	}
	// Read the catalog and machine only if a parseable new file needs compiling.
	loaded := false
	var sourceError error
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return folder, err
		}
		if preset, ok := saved.Presets[id]; ok {
			if preset.UserPreset != id {
				folder.problems[id] = "This file's ID belongs to another saved preset; rename the file before using it."
			}
			continue // Existing layouts keep their exact saved lock.
		}
		raw, err := userpreset.Read(root, id)
		if err == nil {
			_, err = catalog.ParseUserPreset(id, raw)
		}
		if err != nil {
			folder.problems[id] = err.Error()
			continue
		}
		if !loaded {
			loaded = true
			read := c.StoredCatalog
			if read == nil {
				read = readStoredCatalog
			}
			folder.view, sourceError = read(root)
			if errors.Is(sourceError, distribution.ErrNoCatalog) {
				sourceError = fmt.Errorf("no stored catalog; run temper catalog update --root %q", root)
			}
			if sourceError == nil {
				if c.Detect == nil {
					sourceError = errors.New("machine facts unavailable")
				} else {
					facts, err := c.Detect(ctx)
					sourceError = err
					folder.memory = int(facts.PhysicalMemoryBytes >> 30)
				}
			}
		}
		if sourceError != nil {
			folder.problems[id] = sourceError.Error()
			continue
		}
		preset, err := folder.compile(id, raw)
		if err != nil {
			folder.problems[id] = err.Error()
			continue
		}
		folder.files[id] = layoutPresetFile{raw: raw, preset: preset}
	}
	return folder, ctx.Err()
}

func (f layoutPresetFolder) compile(id string, raw []byte) (setup.Preset, error) {
	loaded, err := userpreset.Resolve(f.root, id, raw, f.view)
	if err != nil {
		return setup.Preset{}, err
	}
	lock, err := setup.BuildPreset(loaded.Document, id, f.memory, setup.Choices{})
	if err != nil {
		return setup.Preset{}, err
	}
	preset := setup.Preset{Name: displayName(id, loaded.Document.Presets[id]), UserPreset: id, Lock: lock,
		ForkBaseline: setup.CaptureForkBaseline(loaded.File, f.view.Document, f.memory)}
	check := setup.EmptyConfiguration()
	check.Presets[id] = preset
	return preset, check.Validate()
}

// check protects the first placement from changes since the editor opened,
// including local template edits. Publication uses the reviewed lock itself.
func (f layoutPresetFolder) check(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	original, ok := f.files[id]
	if !ok {
		return fmt.Errorf("preset %q is unavailable", id)
	}
	raw, err := userpreset.Read(f.root, id)
	if err != nil {
		return fmt.Errorf("preset %q cannot be read; reopen the layout editor: %w", id, err)
	}
	if bytes.Equal(raw, original.raw) {
		next, err := f.compile(id, raw)
		if err == nil && sameLock(next.Lock, original.preset.Lock) {
			return nil
		}
	}
	return fmt.Errorf("preset %q or its template changed; reopen the layout editor and review before saving", id)
}
