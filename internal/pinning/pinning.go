// Package pinning resolves manifest layouts into exact lock rows through the
// narrow immutable-upstream read boundary.
package pinning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/patch"
	"github.com/temper-sh/temper/internal/upstream"
)

const maxPatchSource = 16 << 20

// ResolveLayouts performs the upstream reads for a complete set of layout
// rows. Model weights are never opened; only metadata and selected small patch
// sources cross this boundary.
func ResolveLayouts(ctx context.Context, document manifest.Document, ids []string, resolvedDate string, source upstream.Reader) (map[string]lockfile.Entry, error) {
	if len(ids) == 0 {
		return map[string]lockfile.Entry{}, nil
	}
	if source == nil {
		return nil, errors.New("upstream reader is required to resolve layouts")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ordered := append([]string(nil), ids...)
	sort.Strings(ordered)
	patches := map[string]string{}
	entries := make(map[string]lockfile.Entry, len(ordered))
	for _, id := range ordered {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		layout, ok := document.Layouts[id]
		if !ok {
			return nil, fmt.Errorf("resolve layout %q: manifest layout does not exist", id)
		}
		pin, err := resolveModel(ctx, source, layout.Model.Repo, layout.ModelFiles())
		if err != nil {
			return nil, fmt.Errorf("resolve layout %q: %w", id, err)
		}
		files := make([]lockfile.File, 0, len(pin.Files))
		for _, file := range pin.Files {
			files = append(files, lockfile.File{Name: file.Name, SHA256: file.SHA256})
		}
		entry := lockfile.Entry{
			Repo:     layout.Model.Repo,
			Revision: pin.Revision,
			Files:    files,
			Resolved: resolvedDate,
		}
		if layout.ChatTemplate != "" {
			finalHash, ok := patches[layout.ChatTemplate]
			if !ok {
				definition := document.Patches[layout.ChatTemplate]
				finalHash, err = resolvePatch(ctx, source, definition)
				if err != nil {
					return nil, fmt.Errorf("resolve patch %q: %w", layout.ChatTemplate, err)
				}
				patches[layout.ChatTemplate] = finalHash
			}
			entry.Patches = []lockfile.Patch{{Name: layout.ChatTemplate, SHA256: finalHash}}
		}
		entries[id] = entry
	}
	return entries, nil
}

// ValidateSelection ensures update-like operations move only resolution facts,
// never manifest-owned repository, file, or patch selection.
func ValidateSelection(id string, layout manifest.Layout, entry lockfile.Entry) error {
	if entry.Repo != layout.Model.Repo {
		return fmt.Errorf("layout %q repo drift: manifest has %q, lock has %q", id, layout.Model.Repo, entry.Repo)
	}
	wanted := layout.ModelFiles()
	if len(entry.Files) != len(wanted) {
		return fmt.Errorf("layout %q selected model file set drift", id)
	}
	for index, file := range wanted {
		if entry.Files[index].Name != file {
			return fmt.Errorf("layout %q selected model file set drift at %q", id, file)
		}
	}
	if layout.ChatTemplate == "" {
		if len(entry.Patches) != 0 {
			return fmt.Errorf("layout %q selected patch drift: manifest selects no patch", id)
		}
	} else if len(entry.Patches) != 1 || entry.Patches[0].Name != layout.ChatTemplate {
		return fmt.Errorf("layout %q selected patch drift: manifest has %q", id, layout.ChatTemplate)
	}
	return nil
}

func resolveModel(ctx context.Context, source upstream.Reader, repo string, files []string) (upstream.SnapshotPin, error) {
	if batch, ok := source.(upstream.SnapshotReader); ok {
		pin, err := batch.ResolveSnapshot(ctx, repo, files)
		if err != nil {
			return upstream.SnapshotPin{}, err
		}
		if err := validateSnapshotPin(files, pin); err != nil {
			return upstream.SnapshotPin{}, err
		}
		return pin, nil
	}
	pin := upstream.SnapshotPin{Files: make([]upstream.SnapshotFilePin, 0, len(files))}
	for _, file := range files {
		resolved, err := source.Resolve(ctx, repo, file)
		if err != nil {
			return upstream.SnapshotPin{}, err
		}
		if pin.Revision == "" {
			pin.Revision = resolved.Revision
		} else if pin.Revision != resolved.Revision {
			return upstream.SnapshotPin{}, errors.New("repository main moved while resolving the model snapshot")
		}
		pin.Files = append(pin.Files, upstream.SnapshotFilePin{Name: file, SHA256: resolved.SHA256})
	}
	return pin, validateSnapshotPin(files, pin)
}

func validateSnapshotPin(wanted []string, pin upstream.SnapshotPin) error {
	if pin.Revision == "" || len(pin.Files) != len(wanted) {
		return errors.New("upstream returned an incomplete model snapshot pin")
	}
	for index, file := range wanted {
		if pin.Files[index].Name != file || pin.Files[index].SHA256 == "" {
			return fmt.Errorf("upstream model snapshot pin does not match %q", file)
		}
	}
	return nil
}

func resolvePatch(ctx context.Context, source upstream.Reader, definition manifest.Patch) (string, error) {
	parsed, err := patch.ParseSource(definition.Source)
	if err != nil {
		return "", err
	}
	reader, err := source.Open(ctx, parsed.Repo, parsed.Revision, parsed.File)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxPatchSource+1))
	if err != nil {
		return "", fmt.Errorf("read source: %w", err)
	}
	if len(data) > maxPatchSource {
		return "", fmt.Errorf("source exceeds %d bytes", maxPatchSource)
	}
	final, err := patch.Apply(parsed.Transform, data)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(final)
	return hex.EncodeToString(hash[:]), nil
}
