// Package update re-resolves existing manifest layout pins and commits all
// changed lock rows through one atomic replacement.
package update

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
	Layout       string
	DryRun       bool
	Now          func() time.Time
}

type Gate struct {
	Step    string
	Command string
}

type Entry struct {
	ID             string
	Changed        bool
	OldRevision    string
	NewRevision    string
	OldArtifactSet string
	NewArtifactSet string
	Gates          []Gate
}

type Result struct {
	Changed bool
	DryRun  bool
	All     bool
	Entries []Entry
}

func (r Result) ChangeCount() int {
	count := 0
	for _, entry := range r.Entries {
		if entry.Changed {
			count++
		}
	}
	return count
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
	if !snapshot.Exists() {
		return Result{}, errors.New("read lock: file does not exist; run temper resolve first")
	}

	targets, err := targetLayouts(document, options.Layout)
	if err != nil {
		return Result{}, err
	}
	for _, id := range targets {
		entry, ok := snapshot.Document.Entry(id)
		if !ok {
			return Result{}, fmt.Errorf("layout %q has no lock entry; run temper resolve first", id)
		}
		if err := pinning.ValidateSelection(id, document.Layouts[id], entry); err != nil {
			return Result{}, err
		}
	}
	if source == nil {
		return Result{}, errors.New("upstream reader is required to update layouts")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}
	resolved, err := pinning.ResolveLayouts(ctx, document, targets, now().Format("2006-01-02"), source)
	if err != nil {
		return Result{}, err
	}

	result := Result{DryRun: options.DryRun, All: options.Layout == ""}
	replacements := map[string]lockfile.Entry{}
	for _, id := range targets {
		oldEntry := snapshot.Document.Entries[id]
		newEntry := resolved[id]
		changed := oldEntry.Digest() != newEntry.Digest()
		entryResult := Entry{
			ID:             id,
			Changed:        changed,
			OldRevision:    oldEntry.Revision,
			NewRevision:    newEntry.Revision,
			OldArtifactSet: oldEntry.Digest(),
			NewArtifactSet: newEntry.Digest(),
		}
		if changed {
			result.Changed = true
			replacements[id] = newEntry
			entryResult.Gates = gatesFor(id, document.Layouts[id].Role)
		}
		result.Entries = append(result.Entries, entryResult)
	}
	if !result.Changed {
		return result, nil
	}

	candidate, err := snapshot.Document.WithReplacements(replacements)
	if err != nil {
		return Result{}, err
	}
	candidateData, err := lockfile.Marshal(candidate)
	if err != nil {
		return Result{}, err
	}
	if options.DryRun {
		return result, nil
	}
	if err := snapshot.Commit(ctx, candidateData); err != nil {
		return Result{}, err
	}
	return result, nil
}

func targetLayouts(document manifest.Document, target string) ([]string, error) {
	if target != "" {
		if _, ok := document.Layouts[target]; !ok {
			return nil, fmt.Errorf("layout %q is not selected in the manifest", target)
		}
		return []string{target}, nil
	}
	ids := make([]string, 0, len(document.Layouts))
	for id := range document.Layouts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}
