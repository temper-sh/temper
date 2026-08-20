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
		pin, err := source.Resolve(ctx, layout.Model.Repo, layout.Model.File)
		if err != nil {
			return nil, fmt.Errorf("resolve layout %q: %w", id, err)
		}
		entry := lockfile.Entry{
			Repo:     layout.Model.Repo,
			Revision: pin.Revision,
			Files:    []lockfile.File{{Name: layout.Model.File, SHA256: pin.SHA256}},
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
	if len(entry.Files) != 1 || entry.Files[0].Name != layout.Model.File {
		return fmt.Errorf("layout %q selected model file drift: manifest has %q", id, layout.Model.File)
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
