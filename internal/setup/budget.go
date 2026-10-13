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
	for _, member := range mode.Members.Resident {
		layout := document.Layouts[member.Layout]
		gpu := layout.CrispEmbed == nil && (member.NGL == nil || *member.NGL > 0)
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
