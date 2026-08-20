// Package resolve orchestrates manifest reads, immutable upstream metadata
// reads, and a single atomic lock-file commit.
package resolve

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/lockstore"
	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/pinning"
	"github.com/temper-sh/temper/internal/upstream"
)

type Options struct {
	ManifestPath string
	LockPath     string
	DryRun       bool
	Now          func() time.Time
}

type Entry struct {
	ID       string
	Revision string
}

type Result struct {
	Changed bool
	DryRun  bool
	Entries []Entry
}

func Run(ctx context.Context, options Options, source upstream.Reader) (Result, error) {
	if options.ManifestPath == "" || options.LockPath == "" {
		return Result{}, errors.New("manifest and lock paths are required")
	}
	manifestData, err := os.ReadFile(options.ManifestPath)
	if err != nil {
		return Result{}, fmt.Errorf("read manifest: %w", err)
	}
	document, err := manifest.Parse(manifestData)
	if err != nil {
		return Result{}, err
	}
	snapshot, err := lockstore.Read(options.LockPath)
	if err != nil {
		return Result{}, err
	}
	locked := snapshot.Document
	if err := validateExisting(document, locked); err != nil {
		return Result{}, err
	}

	missing := missingLayouts(document, locked)
	result := Result{DryRun: options.DryRun}
	if len(missing) == 0 {
		return result, nil
	}
	if source == nil {
		return Result{}, errors.New("upstream reader is required to resolve missing layouts")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}
	resolvedDate := now().Format("2006-01-02")
	additions, err := pinning.ResolveLayouts(ctx, document, missing, resolvedDate, source)
	if err != nil {
		return Result{}, err
	}
	for _, id := range missing {
		result.Entries = append(result.Entries, Entry{ID: id, Revision: additions[id].Revision})
	}

	candidate, err := locked.WithMissing(additions)
	if err != nil {
		return Result{}, err
	}
	candidateData, err := lockfile.Marshal(candidate)
	if err != nil {
		return Result{}, err
	}
	result.Changed = true
	if options.DryRun {
		return result, nil
	}
	if err := snapshot.Commit(ctx, candidateData); err != nil {
		return Result{}, err
	}
	return result, nil
}

func validateExisting(document manifest.Document, locked lockfile.Document) error {
	for _, id := range sortedKeys(document.Layouts) {
		layout := document.Layouts[id]
		entry, ok := locked.Entry(id)
		if !ok {
			continue
		}
		if err := pinning.ValidateSelection(id, layout, entry); err != nil {
			return err
		}
	}
	return nil
}

func missingLayouts(document manifest.Document, locked lockfile.Document) []string {
	var ids []string
	for id := range document.Layouts {
		if _, ok := locked.Entry(id); !ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
