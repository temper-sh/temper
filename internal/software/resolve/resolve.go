// Package resolve orchestrates software-lock reads, provider candidate reads,
// pure catalog selection, and one atomic lock commit.
package resolve

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/catalog"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/lockstore"
	"github.com/temper-sh/temper/internal/software/selection"
	"github.com/temper-sh/temper/internal/software/testedstatus"
)

type Request struct {
	Package string
	Method  string
}

type Options struct {
	LockPath string
	Target   software.Target
	Requests []Request
	DryRun   bool
	Now      func() time.Time
}

type Entry struct {
	Package string
	Method  string
	Adapter string
	Version string
}

type Result struct {
	Changed  bool
	DryRun   bool
	Entries  []Entry
	Statuses []testedstatus.Entry
}

func Run(ctx context.Context, options Options, supply catalog.Snapshot, resolvers adapter.ResolverFamily) (Result, error) {
	if options.LockPath == "" {
		return Result{}, errors.New("software lock path is required")
	}
	if err := options.Target.Validate(); err != nil {
		return Result{}, fmt.Errorf("software target: %w", err)
	}
	if len(options.Requests) == 0 {
		return Result{}, errors.New("at least one software package request is required")
	}
	if err := supply.Validate(); err != nil {
		return Result{}, err
	}
	snapshot, err := lockstore.Read(options.LockPath)
	if err != nil {
		return Result{}, err
	}
	if snapshot.Exists() {
		if err := snapshot.Document.ValidateAgainst(supply.Document, supply.SHA256); err != nil {
			return Result{}, fmt.Errorf("existing software lock: %w", err)
		}
		if snapshot.Document.Target != options.Target {
			return Result{}, errors.New("existing software lock target differs from requested target")
		}
	}

	requests := append([]Request(nil), options.Requests...)
	sort.Slice(requests, func(i, j int) bool { return requests[i].Package < requests[j].Package })
	seen := map[string]bool{}
	var missing []Request
	for _, request := range requests {
		if request.Package == "" || request.Method == "" {
			return Result{}, errors.New("software request package and method are required")
		}
		if seen[request.Package] {
			return Result{}, fmt.Errorf("software package %q requested more than once", request.Package)
		}
		seen[request.Package] = true
		if selected, exists := snapshot.Document.Selections[request.Package]; exists {
			if selected.Method != request.Method {
				return Result{}, fmt.Errorf("software package %q is locked with method %q, requested %q", request.Package, selected.Method, request.Method)
			}
			continue
		}
		missing = append(missing, request)
	}
	result := Result{DryRun: options.DryRun}
	if len(missing) == 0 {
		statuses, err := testedstatus.Compare(snapshot.Document, supply)
		if err != nil {
			return Result{}, err
		}
		result.Statuses = requestedStatuses(statuses, requests)
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	type pendingRead struct {
		request    Request
		resolver   adapter.CandidateResolver
		descriptor adapter.Descriptor
		recipe     catalog.Recipe
	}
	pending := make([]pendingRead, 0, len(missing))
	for _, request := range missing {
		resolver, descriptor, err := resolvers.For(supply.Document, request.Method, options.Target)
		if err != nil {
			return Result{}, fmt.Errorf("resolve software package %q: %w", request.Package, err)
		}
		pkg, ok := supply.Document.Packages[request.Package]
		if !ok {
			return Result{}, fmt.Errorf("catalog package %q does not exist", request.Package)
		}
		recipe, ok := pkg.Recipes[descriptor.ID]
		if !ok || recipe.Method != request.Method {
			return Result{}, fmt.Errorf("catalog package %q has no %q recipe for adapter %q", request.Package, request.Method, descriptor.ID)
		}
		pending = append(pending, pendingRead{request: request, resolver: resolver, descriptor: descriptor, recipe: recipe})
	}

	selectionRequests := make([]selection.Request, 0, len(pending))
	for _, item := range pending {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		candidates, err := item.resolver.Candidates(ctx, adapter.ResolveRequest{
			Package: item.request.Package, Recipe: item.recipe, Supply: supply.Document, Target: options.Target,
		})
		if err != nil {
			return Result{}, fmt.Errorf("read %s candidates for package %q: %w", item.descriptor.ID, item.request.Package, err)
		}
		selectionRequests = append(selectionRequests, selection.Request{Package: item.request.Package, Method: item.request.Method, Candidates: candidates})
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}
	var existing *softwarelock.Document
	if snapshot.Exists() {
		existing = &snapshot.Document
	}
	candidate, err := selection.Resolve(supply, options.Target, now(), existing, selectionRequests)
	if err != nil {
		return Result{}, err
	}
	candidateData, err := softwarelock.Marshal(candidate)
	if err != nil {
		return Result{}, err
	}
	statuses, err := testedstatus.Compare(candidate, supply)
	if err != nil {
		return Result{}, err
	}
	result.Statuses = requestedStatuses(statuses, requests)
	for _, request := range missing {
		selected := candidate.Selections[request.Package]
		root := candidate.Units[selected.RootUnit]
		result.Entries = append(result.Entries, Entry{
			Package: request.Package, Method: selected.Method, Adapter: selected.Adapter, Version: root.Version,
		})
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

func requestedStatuses(statuses []testedstatus.Entry, requests []Request) []testedstatus.Entry {
	requested := make(map[string]bool, len(requests))
	for _, request := range requests {
		requested[request.Package] = true
	}
	result := make([]testedstatus.Entry, 0, len(requests))
	for _, status := range statuses {
		if requested[status.Package] {
			result = append(result, status)
		}
	}
	return result
}
