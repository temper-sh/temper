// Package check audits agreement between a manifest, lock, selected mode, and
// local immutable artifact sets without changing any state.
package check

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"

	"github.com/temper-sh/temper/internal/artifactset"
	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/datadir"
	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/manifest"
)

const (
	VerificationReceipt = "receipt"
	VerificationSHA256  = "sha256"

	CodeLockEntryMissing        = "lock-entry-missing"
	CodeLockEntryOrphan         = "lock-entry-orphan"
	CodeLockSelectionDrift      = "lock-selection-drift"
	CodeArtifactNotMaterialized = "artifact-not-materialized"
	CodeArtifactInvalid         = "artifact-invalid"
	CodeArtifactHashMismatch    = "artifact-hash-mismatch"
	CodeBudgetExceeded          = "budget-exceeded"
)

type Options struct {
	ManifestPath string
	LockPath     string
	Root         string
	Mode         string
	Verify       bool
	Machine      budget.Machine
}

type Finding struct {
	Code   string
	Layout string
	Detail string
}

type LayoutResult struct {
	ID          string
	ArtifactSet string
	Files       int
	OK          bool
}

type Result struct {
	Mode         string
	Verification string
	Layouts      []LayoutResult
	Findings     []Finding
	Budget       budget.Prediction
}

func (r Result) OK() bool { return len(r.Findings) == 0 }

// Run performs a complete read-only audit. Expected drift and artifact
// failures are accumulated as findings; invalid inputs and interrupted reads
// are returned as errors because no valid report can be completed.
func Run(ctx context.Context, options Options) (Result, error) {
	if options.ManifestPath == "" || options.LockPath == "" || options.Mode == "" {
		return Result{}, errors.New("manifest, lock, root and mode are required")
	}
	root, err := datadir.Resolve(options.Root)
	if err != nil {
		return Result{}, err
	}
	manifestData, err := os.ReadFile(options.ManifestPath)
	if err != nil {
		return Result{}, fmt.Errorf("read manifest: %w", err)
	}
	document, err := manifest.Parse(manifestData)
	if err != nil {
		return Result{}, err
	}
	lockData, err := os.ReadFile(options.LockPath)
	if err != nil {
		return Result{}, fmt.Errorf("read lock: %w", err)
	}
	locked, err := lockfile.Parse(lockData)
	if err != nil {
		return Result{}, err
	}
	mode, err := document.Mode(options.Mode)
	if err != nil {
		return Result{}, err
	}
	if _, err := budget.Predict(budget.Input{
		Utilization: document.Defaults.GPUMemoryUtilization,
		Machine:     options.Machine,
	}); err != nil {
		return Result{}, fmt.Errorf("validate wall-model inputs: %w", err)
	}

	verification := VerificationReceipt
	if options.Verify {
		verification = VerificationSHA256
	}
	selected := selectedLayoutIDs(mode)
	result := Result{
		Mode:         options.Mode,
		Verification: verification,
		Layouts:      make([]LayoutResult, len(selected)),
	}

	sets := make(map[string]artifactset.Set, len(document.Layouts))
	modelBytes := make(map[string]int64, len(selected))
	for _, layoutID := range sortedLayoutIDs(document.Layouts) {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		entry, ok := locked.Entry(layoutID)
		if !ok {
			result.Findings = append(result.Findings, Finding{
				Code:   CodeLockEntryMissing,
				Layout: layoutID,
				Detail: fmt.Sprintf("layout %q has no lock entry; run temper resolve first", layoutID),
			})
			continue
		}
		set, err := artifactset.New(root, layoutID, document.Layouts[layoutID], entry, document.Patches)
		if err != nil {
			result.Findings = append(result.Findings, Finding{Code: CodeLockSelectionDrift, Layout: layoutID, Detail: err.Error()})
			continue
		}
		sets[layoutID] = set
	}
	for _, layoutID := range sortedLockIDs(locked.Entries) {
		if _, ok := document.Layouts[layoutID]; !ok {
			result.Findings = append(result.Findings, Finding{
				Code:   CodeLockEntryOrphan,
				Layout: layoutID,
				Detail: fmt.Sprintf("lock entry %q has no manifest layout", layoutID),
			})
		}
	}

	for index, layoutID := range selected {
		layoutResult := LayoutResult{ID: layoutID}
		set, ok := sets[layoutID]
		if !ok {
			result.Layouts[index] = layoutResult
			continue
		}
		layoutResult.ArtifactSet = set.Digest()
		layoutResult.Files = len(set.Files())
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		var (
			inspection artifactset.Inspection
			verifyErr  error
		)
		if options.Verify {
			inspection, verifyErr = set.InspectContent(ctx)
		} else {
			inspection, verifyErr = set.Inspect()
		}
		if verifyErr == nil {
			modelBytes[layoutID] = inspection.ModelBytes
			layoutResult.OK = true
			result.Layouts[index] = layoutResult
			continue
		}
		if errors.Is(verifyErr, context.Canceled) || errors.Is(verifyErr, context.DeadlineExceeded) {
			return Result{}, verifyErr
		}
		code := CodeArtifactInvalid
		switch {
		case errors.Is(verifyErr, artifactset.ErrNotMaterialized):
			code = CodeArtifactNotMaterialized
		case errors.Is(verifyErr, artifactset.ErrContentMismatch):
			code = CodeArtifactHashMismatch
		}
		result.Findings = append(result.Findings, Finding{Code: code, Layout: layoutID, Detail: verifyErr.Error()})
		result.Layouts[index] = layoutResult
	}

	result.Budget, err = predictBudget(document, mode, options.Machine, modelBytes)
	if err != nil {
		return Result{}, fmt.Errorf("predict resident budget: %w", err)
	}
	if result.Budget.Status == budget.StatusExceeded {
		detail := fmt.Sprintf("predicted resident requirement %d MiB exceeds wired limit %d MiB",
			result.Budget.RequiredMiB, result.Budget.WiredLimitMiB)
		if result.Budget.HasSuggestion {
			detail += fmt.Sprintf("; gpu_memory_utilization %s or lower would fit",
				formatConservativeFraction(result.Budget.SuggestedUtilization))
		} else {
			detail += "; admitted model lower bounds and the OS floor cannot fit"
		}
		result.Findings = append(result.Findings, Finding{
			Code: CodeBudgetExceeded, Layout: result.Budget.Holder, Detail: detail,
		})
	}

	sort.Slice(result.Findings, func(i, j int) bool {
		if result.Findings[i].Layout != result.Findings[j].Layout {
			return result.Findings[i].Layout < result.Findings[j].Layout
		}
		if result.Findings[i].Code != result.Findings[j].Code {
			return result.Findings[i].Code < result.Findings[j].Code
		}
		return result.Findings[i].Detail < result.Findings[j].Detail
	})
	return result, nil
}

func formatConservativeFraction(value float64) string {
	for precision := 3; precision <= 15; precision++ {
		factor := math.Pow10(precision)
		rounded := math.Floor(value*factor) / factor
		if rounded > 0 {
			return strconv.FormatFloat(rounded, 'f', precision, 64)
		}
	}
	return strconv.FormatFloat(math.Nextafter(value, 0), 'g', -1, 64)
}

func predictBudget(document manifest.Document, mode manifest.Mode, machine budget.Machine, modelBytes map[string]int64) (budget.Prediction, error) {
	residents := make([]budget.Resident, 0, len(mode.Members.Resident))
	for _, member := range mode.Members.Resident {
		gpu := member.NGL == nil || *member.NGL > 0
		bytes, admitted := modelBytes[member.Layout]
		if gpu && !admitted {
			return budget.Prediction{
				Status: budget.StatusUnavailable,
				Reason: fmt.Sprintf("GPU-resident artifact %q did not pass admission", member.Layout),
			}, nil
		}
		modelMiB, err := bytesToMiB(bytes)
		if err != nil {
			return budget.Prediction{}, fmt.Errorf("layout %q model size: %w", member.Layout, err)
		}
		layout := document.Layouts[member.Layout]
		residents = append(residents, budget.Resident{
			ID:       member.Layout,
			Holder:   member.Preferred && layout.Role == "coder",
			GPU:      gpu,
			ModelMiB: modelMiB,
		})
	}
	return budget.Predict(budget.Input{
		Utilization: document.Defaults.GPUMemoryUtilization,
		Machine:     machine,
		Residents:   residents,
	})
}

func bytesToMiB(bytes int64) (int64, error) {
	if bytes < 0 {
		return 0, errors.New("byte size cannot be negative")
	}
	if bytes == 0 {
		return 0, nil
	}
	const bytesPerMiB int64 = 1024 * 1024
	return (bytes-1)/bytesPerMiB + 1, nil
}

func selectedLayoutIDs(mode manifest.Mode) []string {
	ids := make([]string, 0, len(mode.Members.Resident)+len(mode.Members.OnDemand))
	for _, member := range mode.Members.Resident {
		ids = append(ids, member.Layout)
	}
	for _, member := range mode.Members.OnDemand {
		ids = append(ids, member.Layout)
	}
	sort.Strings(ids)
	return ids
}

func sortedLayoutIDs(layouts map[string]manifest.Layout) []string {
	ids := make([]string, 0, len(layouts))
	for id := range layouts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func sortedLockIDs(entries map[string]lockfile.Entry) []string {
	ids := make([]string, 0, len(entries))
	for id := range entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
