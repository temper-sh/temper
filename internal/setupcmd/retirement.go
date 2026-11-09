package setupcmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/cleanup"
	"github.com/temper-sh/temper/internal/hfcache"
	"github.com/temper-sh/temper/internal/managed"
	"github.com/temper-sh/temper/internal/preset"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/setupui"
)

func retirementOption(saved setup.Preset, option setupui.PresetOption, view catalog.View) setupui.PresetOption {
	origin := setup.RetirementOrigin(saved)
	if r, ok := view.Document.RetiredPresets[origin]; ok {
		option.Retirement = &r
		option.Group, option.Section = "retired", ""
		option.Successor, _ = view.Document.Successor(origin)
		if option.Successor == "" {
			_, err := view.Document.Successor(origin)
			option.SuccessorUnavailable = err.Error()
		}
	}
	return option
}

func (c Command) applyRetirement(ctx context.Context, root string, current, next setup.Configuration, rev string, changes []setup.RetirementChange, deleteWeights, dry, jsonOutput bool, out, diagnostics io.Writer) int {
	plan, err := c.Preview(ctx, root, next)
	if err != nil {
		return c.fail(diagnostics, err)
	}
	if !plan.CanPrepare {
		return c.fail(diagnostics, fmt.Errorf("cannot apply retirement: %s", strings.Join(plan.Refusals, "; ")))
	}
	after, _, err := setup.SaveConfiguration(ctx, root, next, rev, true)
	if err != nil {
		return c.fail(diagnostics, err)
	}
	state := &setup.RetirementState{Schema: setup.RetirementSchema, BeforeRevision: rev, AfterRevision: after, Configuration: next, Retired: map[string]setup.Preset{}, Changes: changes, DeleteWeights: deleteWeights}
	previous, err := (managed.FileStore{Root: root}).Read()
	if err != nil {
		return c.fail(diagnostics, err)
	}
	state.PreviousState, err = json.Marshal(previous)
	if err != nil {
		return c.fail(diagnostics, err)
	}
	if c.CacheRoot != nil {
		state.HFCache, err = c.CacheRoot()
		if err != nil {
			return c.fail(diagnostics, err)
		}
	}
	for _, change := range changes {
		old, ok := current.Presets[change.Preset]
		if !ok {
			return c.fail(diagnostics, fmt.Errorf("retired selection %q changed", change.Preset))
		}
		state.Retired[change.Preset] = old
	}
	if err = state.Validate(); err != nil {
		return c.fail(diagnostics, err)
	}
	if dry {
		// Name each shared file before consent; their deletion cannot be undone.
		files, err := cleanup.ReviewHFFiles(next, state.Retired, state.HFCache)
		if err != nil {
			return c.fail(diagnostics, err)
		}
		if !jsonOutput {
			for _, line := range plan.Lines() {
				fmt.Fprintln(out, line)
			}
			for _, change := range changes {
				target := "remove"
				if change.Successor != "" {
					target = change.Successor
				}
				fmt.Fprintf(out, "%s → %s\n", change.Preset, target)
			}
			if len(files) == 0 {
				fmt.Fprintln(out, "No unused shared HF files to delete.")
			} else {
				action := "kept; add --delete-retired-weights to delete them across cached revisions"
				if deleteWeights {
					action = "deleted across cached revisions"
				}
				fmt.Fprintf(out, "Unused shared HF files, %s (other applications may use them):\n", action)
				for _, f := range files {
					fmt.Fprintf(out, "  %s/%s · %s\n", f.Repo, f.Name, setup.Size(f.Bytes))
				}
			}
			fmt.Fprintln(out, "Dry run: no selection or files changed.")
			return 0
		}
		return c.configurationOutput(out, diagnostics, map[string]any{"schema": "temper-retirement-result/v1", "dry_run": true, "changes": changes, "delete_weights": deleteWeights, "unused_hf_files": files, "plan": plan})
	}
	// Do not hold the root lock across transport. The guarded commit checks the
	// selection again after preparation, leaving old configuration recoverable.
	if err = c.checkRetirementRemoval(ctx, state); err != nil {
		return c.fail(diagnostics, fmt.Errorf("old selection retained; %w", err))
	}
	if err = c.prepareRetirement(ctx, root, state, diagnostics); err != nil {
		return c.fail(diagnostics, fmt.Errorf("old selection retained; preparation incomplete: %w", err))
	}
	if _, _, err = setup.SaveConfiguration(ctx, root, next, rev, true); err != nil {
		return c.fail(diagnostics, fmt.Errorf("selection changed during preparation; review again: %w", err))
	}
	if err = setup.SaveRetirement(ctx, root, state); err != nil {
		return c.fail(diagnostics, err)
	}
	if _, _, err = setup.SaveConfiguration(ctx, root, next, rev, false); err != nil {
		return c.retirementFailure(diagnostics, "configuration switch", err)
	}
	return c.completeRetirement(ctx, root, state, jsonOutput, out, diagnostics)
}

func (c Command) checkRetirementRemoval(ctx context.Context, state *setup.RetirementState) error {
	if !state.DeleteWeights {
		return nil
	}
	files, err := cleanup.ReviewHFFiles(state.Configuration, state.Retired, state.HFCache)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}
	check := c.CheckHFRemoval
	if check == nil {
		check = hfcache.CheckFileRemoval
	}
	return check(ctx)
}

func (c Command) prepareRetirement(ctx context.Context, root string, state *setup.RetirementState, out io.Writer) error {
	ids := []string{}
	for id := range state.Configuration.Presets {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if err := preset.Prepare(ctx, root, state.Configuration.Presets[id].Lock, c.Effects, out); err != nil {
			return fmt.Errorf("preset %s: %w", id, err)
		}
	}
	return nil
}

func (c Command) resumeRetirement(ctx context.Context, root string, state *setup.RetirementState, dry, jsonOutput bool, out, diagnostics io.Writer) int {
	_, rev, err := setup.ReadConfiguration(root)
	if err != nil {
		return c.fail(diagnostics, err)
	}
	if rev != state.BeforeRevision && rev != state.AfterRevision {
		return c.fail(diagnostics, errors.New("configuration changed since retirement review; no activation or cleanup performed; use --discard-retirement to keep the current selection"))
	}
	if rev == state.BeforeRevision && state.Activated {
		return c.fail(diagnostics, errors.New("retirement activation checkpoint does not match the saved selection; no cleanup performed"))
	}
	if dry {
		plan, err := c.Preview(ctx, root, state.Configuration)
		if err != nil {
			return c.fail(diagnostics, err)
		}
		return c.configurationOutput(out, diagnostics, map[string]any{"schema": "temper-retirement-result/v1", "dry_run": true, "pending": true, "activated": state.Activated, "plan": plan, "changes": state.Changes, "delete_weights": state.DeleteWeights})
	}
	if err = c.checkRetirementRemoval(ctx, state); err != nil {
		return c.retirementFailure(diagnostics, "shared weight deletion prerequisite", err)
	}
	if !state.Activated {
		// Preparation is idempotent. Repeat it before every activation retry: a
		// crash may precede the guarded switch, and prepared material may have
		// been removed since the first attempt.
		if err = c.prepareRetirement(ctx, root, state, diagnostics); err != nil {
			return c.retirementFailure(diagnostics, "preparation", err)
		}
		if rev == state.BeforeRevision {
			if _, _, err = setup.SaveConfiguration(ctx, root, state.Configuration, rev, false); err != nil {
				return c.retirementFailure(diagnostics, "configuration switch", err)
			}
		}
	}
	return c.completeRetirement(ctx, root, state, jsonOutput, out, diagnostics)
}

// completeRetirement activates the switched selection once, then releases only
// the material the retirement freed. Each stage is safe to repeat on resume.
func (c Command) completeRetirement(ctx context.Context, root string, state *setup.RetirementState, jsonOutput bool, out, diagnostics io.Writer) int {
	if !state.Activated {
		layout := layoutToStart(root, state.Configuration)
		if layout != "" && len(state.Configuration.Layouts[layout].Presets) > 0 {
			_, status, startErr := c.start(ctx, root, layout, nil)
			if startErr != nil {
				return c.retirementFailure(diagnostics, "activation", startErr)
			}
			if status == nil || status.Pending || status.Error != "" || !status.Observed.Exists || !status.Observed.Ready || status.State.Current == nil || status.State.Current.Layout != layout {
				return c.retirementFailure(diagnostics, "activation", errors.New("layout has not reached its requested state"))
			}
		} else {
			_, status, stopErr := c.stopLayout(ctx, root, false)
			if stopErr != nil {
				return c.retirementFailure(diagnostics, "stop empty layout", stopErr)
			}
			if status == nil || status.Pending || status.Error != "" || status.Observed.Exists {
				return c.retirementFailure(diagnostics, "stop empty layout", errors.New("old layout has not stopped"))
			}
		}
		state.Activated = true
		if err := setup.SaveRetirement(ctx, root, state); err != nil {
			return c.retirementFailure(diagnostics, "activation checkpoint", err)
		}
	}
	unlock, err := setup.LockRoot(root)
	if err != nil {
		return c.fail(diagnostics, err)
	}
	defer unlock()
	current, rev, err := setup.ReadConfiguration(root)
	if err != nil || rev != state.AfterRevision {
		return c.retirementFailure(diagnostics, "cleanup", errors.New("configuration changed; files retained"))
	}
	owned, err := setup.ReadRetirement(root)
	if err != nil || owned == nil || owned.Revision != state.Revision {
		return c.retirementFailure(diagnostics, "cleanup", errors.New("retirement intent changed; files retained"))
	}
	managedState, err := (managed.FileStore{Root: root}).Read()
	if err != nil {
		return c.retirementFailure(diagnostics, "cleanup", err)
	}
	cache := state.HFCache
	var previous managed.State
	if len(state.PreviousState) > 0 {
		if err = json.Unmarshal(state.PreviousState, &previous); err != nil {
			return c.retirementFailure(diagnostics, "cleanup", err)
		}
	}
	plan, err := cleanup.Released(root, current, state.Retired, previous, managedState, cache)
	if err != nil {
		return c.retirementFailure(diagnostics, "cleanup", err)
	}
	if len(plan.Kept) > 0 {
		return c.retirementFailure(diagnostics, "cleanup", fmt.Errorf("cannot verify released material: %s", strings.Join(plan.Kept, "; ")))
	}
	for _, item := range plan.Remove {
		if item.Kind == cleanup.KindSoftware && c.Remover == nil {
			return c.retirementFailure(diagnostics, "cleanup", errors.New("software removal is unavailable"))
		}
	}
	if err = cleanup.Apply(ctx, root, plan, c.Remover); err != nil {
		return c.retirementFailure(diagnostics, "cleanup", err)
	}
	deleted := []string{}
	if state.DeleteWeights {
		remove := c.RemoveHF
		if remove == nil {
			remove = hfcache.RemoveFile
		}
		for _, file := range plan.UnusedHFFiles {
			files, inspectErr := (hfcache.Cache{Root: cache}).NamedFiles(file)
			if inspectErr != nil {
				return c.retirementFailure(diagnostics, "shared weight cleanup", inspectErr)
			}
			if len(files) == 0 {
				continue
			}
			if err = remove(ctx, cache, file, diagnostics); err != nil {
				return c.retirementFailure(diagnostics, "shared weight cleanup", err)
			}
			remaining, err := (hfcache.Cache{Root: cache}).NamedFiles(file)
			if err != nil {
				return c.retirementFailure(diagnostics, "shared weight cleanup verification", err)
			}
			if len(remaining) > 0 {
				return c.retirementFailure(diagnostics, "shared weight cleanup verification", fmt.Errorf("hf reported success but %s/%s remains in %d cached revisions", file.Repo, file.Name, len(remaining)))
			}
			deleted = append(deleted, file.Repo+"/"+file.Name)
		}
	}
	if err = setup.ClearRetirement(root, state); err != nil {
		return c.retirementFailure(diagnostics, "completion", err)
	}
	if jsonOutput {
		return c.configurationOutput(out, diagnostics, map[string]any{"schema": "temper-retirement-result/v1", "changed": true, "revision": rev, "changes": state.Changes, "cleanup": plan, "deleted_hf_files": deleted})
	}
	fmt.Fprintln(out, "Applied retirement changes. Unused managed files cleaned up.")
	if len(deleted) > 0 {
		fmt.Fprintln(out, "Deleted shared HF files across cached revisions: "+strings.Join(deleted, ", "))
	} else if len(plan.UnusedHFFiles) > 0 {
		fmt.Fprintln(out, "Shared Hugging Face weights retained; other applications may use them.")
	}
	return 0
}

// reviewHFFiles names, for the wizard's review, the shared files that deletion
// consent would cover.
func (c Command) reviewHFFiles(next setup.Configuration, retired map[string]setup.Preset) ([]setupui.SharedFile, error) {
	if c.CacheRoot == nil {
		return nil, nil
	}
	cache, err := c.CacheRoot()
	if err != nil {
		return nil, err
	}
	files, err := cleanup.ReviewHFFiles(next, retired, cache)
	if err != nil {
		return nil, err
	}
	shared := []setupui.SharedFile{}
	for _, f := range files {
		shared = append(shared, setupui.SharedFile{Name: f.Repo + "/" + f.Name, Bytes: f.Bytes})
	}
	return shared, nil
}

func (c Command) retirementFailure(out io.Writer, stage string, err error) int {
	return c.fail(out, fmt.Errorf("retirement %s incomplete: %w; retry with temper configure --resume --prepare", stage, err))
}
