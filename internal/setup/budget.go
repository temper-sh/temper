package setup

import (
	"errors"
	"fmt"

	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/manifest"
)

// PredictBudget applies the shared wall model to explicit model sizes. Setup
// supplies catalog-declared sizes; installed checks supply admitted file sizes.
func PredictBudget(document manifest.Document, mode manifest.Mode, machine budget.Machine, modelBytes map[string]int64) (budget.Prediction, error) {
	residents := make([]budget.Resident, 0, len(mode.Members.Resident))
	mixedPlacement := false
	kapitsa := false
	for _, member := range mode.Members.Resident {
		layout := document.Layouts[member.Layout]
		gpu := layout.CrispEmbed == nil && (member.NGL == nil || *member.NGL > 0)
		if gpu && layout.Llama != nil && layout.Llama.Controls != nil && layout.Llama.Controls.CPUMoELayers > 0 {
			mixedPlacement = true
		}
		kapitsa = kapitsa || gpu && layout.Kapitsa != nil
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
		residents = append(residents, budget.Resident{
			ID:       member.Layout,
			Holder:   member.Layout == document.ForegroundLayout(mode) && layout.TechnicalInterface() == "chat-completions",
			GPU:      gpu,
			ModelMiB: modelMiB,
		})
	}
	prediction, err := budget.Predict(budget.Input{
		Utilization: document.Defaults.GPUMemoryUtilization,
		Machine:     machine,
		Residents:   residents,
	})
	if err == nil && mixedPlacement {
		// Retain the unchanged whole-model allowance for physical RAM accounting,
		// but do not claim a GPU fit, refusal or sysctl suggestion from that proxy.
		prediction.Status = budget.StatusUnavailable
		prediction.Reason = "MoE CPU/GPU split is unknown; numeric allocation is the conservative whole-model allowance, not a GPU requirement"
		prediction.HasSuggestion = false
	}
	if err == nil && kapitsa {
		prediction.Status = budget.StatusUnavailable
		prediction.Reason = "Kapitsa Metal allocation is unknown; numeric allocation is a whole-model planning allowance, not a wired-memory requirement"
		prediction.HasSuggestion = false
	}
	return prediction, err
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
