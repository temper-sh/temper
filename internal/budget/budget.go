// Package budget predicts whether a selected GPU-resident posture fits below
// a machine's Metal wired-memory wall. It performs no reads or effects.
package budget

import (
	"errors"
	"fmt"
	"math"
)

const (
	StatusFits          = "fits"
	StatusExceeded      = "exceeded"
	StatusUnavailable   = "unavailable"
	StatusNotApplicable = "not-applicable"

	WiredSourceLive      = "live-sysctl"
	WiredSourcePredicted = "predicted-macos-default"

	OSFloorMiB int64 = 1024
)

// Machine contains the live or conservatively predicted machine facts used by
// the wall model. Capacities are expressed in MiB.
type Machine struct {
	PhysicalMiB   int64
	DeviceMiB     int64
	WiredLimitMiB int64
	WiredSource   string
}

// Resident describes one mode member that may occupy GPU memory while the
// mode is idle. ModelMiB is the admitted model file size rounded up to MiB.
type Resident struct {
	ID       string
	Holder   bool
	GPU      bool
	ModelMiB int64
}

// Input is the complete pure input to one wall-model prediction.
type Input struct {
	Utilization float64
	Machine     Machine
	Residents   []Resident
}

// Prediction is the fully explained result of one wall-model calculation.
type Prediction struct {
	Status               string
	Reason               string
	Holder               string
	PhysicalMiB          int64
	DeviceMiB            int64
	Utilization          float64
	AllocationMiB        int64
	HolderMinimumMiB     int64
	CoTenantsMiB         int64
	OSFloorMiB           int64
	RequiredMiB          int64
	WiredLimitMiB        int64
	SpareMiB             int64
	WiredSource          string
	SuggestedUtilization float64
	HasSuggestion        bool
}

// Predict applies the bootstrap wall model to already-read, typed facts.
func Predict(input Input) (Prediction, error) {
	if err := validate(input); err != nil {
		return Prediction{}, err
	}
	prediction := Prediction{
		Status:        StatusNotApplicable,
		Reason:        "mode has no preferred GPU-resident coder",
		PhysicalMiB:   input.Machine.PhysicalMiB,
		DeviceMiB:     input.Machine.DeviceMiB,
		Utilization:   input.Utilization,
		OSFloorMiB:    OSFloorMiB,
		WiredLimitMiB: input.Machine.WiredLimitMiB,
		WiredSource:   input.Machine.WiredSource,
	}

	var holder *Resident
	for index := range input.Residents {
		resident := &input.Residents[index]
		if resident.Holder && resident.GPU {
			holder = resident
			continue
		}
		if resident.GPU {
			var err error
			prediction.CoTenantsMiB, err = add(prediction.CoTenantsMiB, resident.ModelMiB)
			if err != nil {
				return Prediction{}, fmt.Errorf("sum GPU-resident model sizes: %w", err)
			}
		}
	}
	if holder == nil {
		return prediction, nil
	}

	prediction.Reason = ""
	prediction.Holder = holder.ID
	allocation := input.Utilization * float64(input.Machine.DeviceMiB)
	if allocation > math.MaxInt64 {
		return Prediction{}, errors.New("holder allocation exceeds supported range")
	}
	prediction.AllocationMiB = int64(allocation)
	prediction.HolderMinimumMiB = holder.ModelMiB
	holderMiB := max(prediction.AllocationMiB, prediction.HolderMinimumMiB)
	required, err := add(holderMiB, prediction.CoTenantsMiB)
	if err != nil {
		return Prediction{}, fmt.Errorf("sum holder and co-tenant memory: %w", err)
	}
	prediction.RequiredMiB, err = add(required, prediction.OSFloorMiB)
	if err != nil {
		return Prediction{}, fmt.Errorf("add OS floor: %w", err)
	}
	prediction.SpareMiB = prediction.WiredLimitMiB - prediction.RequiredMiB
	if prediction.SpareMiB >= 0 {
		prediction.Status = StatusFits
		return prediction, nil
	}

	prediction.Status = StatusExceeded
	availableForHolder := prediction.WiredLimitMiB - prediction.CoTenantsMiB - prediction.OSFloorMiB
	if availableForHolder >= prediction.HolderMinimumMiB {
		suggested := float64(availableForHolder) / float64(prediction.DeviceMiB)
		if suggested >= 0 && suggested < prediction.Utilization {
			prediction.SuggestedUtilization = min(suggested, 1)
			prediction.HasSuggestion = true
		}
	}
	return prediction, nil
}

func add(left, right int64) (int64, error) {
	if right > math.MaxInt64-left {
		return 0, errors.New("memory total exceeds supported range")
	}
	return left + right, nil
}

func validate(input Input) error {
	if input.Utilization <= 0 || input.Utilization > 1 {
		return errors.New("gpu memory utilization must be greater than zero and at most one")
	}
	machine := input.Machine
	if machine.PhysicalMiB <= 0 || machine.DeviceMiB <= 0 || machine.WiredLimitMiB <= 0 {
		return errors.New("machine memory capacities must be positive")
	}
	if machine.DeviceMiB > machine.PhysicalMiB || machine.WiredLimitMiB > machine.PhysicalMiB {
		return errors.New("machine device and wired capacities cannot exceed physical memory")
	}
	if machine.WiredSource != WiredSourceLive && machine.WiredSource != WiredSourcePredicted {
		return fmt.Errorf("unknown wired-limit source %q", machine.WiredSource)
	}

	seen := make(map[string]bool, len(input.Residents))
	holders := 0
	for _, resident := range input.Residents {
		if resident.ID == "" {
			return errors.New("resident id is required")
		}
		if seen[resident.ID] {
			return fmt.Errorf("resident %q is repeated", resident.ID)
		}
		seen[resident.ID] = true
		if resident.ModelMiB < 0 {
			return fmt.Errorf("resident %q model size cannot be negative", resident.ID)
		}
		if resident.Holder {
			holders++
		}
	}
	if holders > 1 {
		return errors.New("wall model has more than one holder")
	}
	return nil
}
