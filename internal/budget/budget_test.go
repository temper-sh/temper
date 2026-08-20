package budget_test

import (
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/budget"
)

func TestPredictExplainsAFittingHolderEnvelope(t *testing.T) {
	prediction, err := budget.Predict(budget.Input{
		Utilization: 0.85,
		Machine: budget.Machine{
			PhysicalMiB: 32768, DeviceMiB: 26542, WiredLimitMiB: 24576,
			WiredSource: budget.WiredSourceLive,
		},
		Residents: []budget.Resident{{ID: "coder", Holder: true, GPU: true, ModelMiB: 16000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if prediction.Status != budget.StatusFits || prediction.AllocationMiB != 22560 || prediction.HolderMinimumMiB != 16000 || prediction.RequiredMiB != 23584 || prediction.SpareMiB != 992 {
		t.Fatalf("prediction = %#v", prediction)
	}
}

func TestPredictCountsResidentGPUTenantsAndSuggestsASolvableFraction(t *testing.T) {
	prediction, err := budget.Predict(budget.Input{
		Utilization: 0.85,
		Machine: budget.Machine{
			PhysicalMiB: 32768, DeviceMiB: 26542, WiredLimitMiB: 24576,
			WiredSource: budget.WiredSourcePredicted,
		},
		Residents: []budget.Resident{
			{ID: "coder", Holder: true, GPU: true, ModelMiB: 16000},
			{ID: "embed", GPU: true, ModelMiB: 1500},
			{ID: "cpu", GPU: false, ModelMiB: 9000},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if prediction.Status != budget.StatusExceeded || prediction.CoTenantsMiB != 1500 || prediction.RequiredMiB != 25084 || prediction.SpareMiB != -508 || !prediction.HasSuggestion {
		t.Fatalf("prediction = %#v", prediction)
	}
	want := float64(24576-1500-budget.OSFloorMiB) / 26542
	if prediction.SuggestedUtilization != want {
		t.Fatalf("suggested utilization = %v, want %v", prediction.SuggestedUtilization, want)
	}
}

func TestPredictUsesTheModelLowerBoundAndCannotSuggestPastIt(t *testing.T) {
	prediction, err := budget.Predict(budget.Input{
		Utilization: 0.25,
		Machine: budget.Machine{
			PhysicalMiB: 8192, DeviceMiB: 6635, WiredLimitMiB: 5324,
			WiredSource: budget.WiredSourceLive,
		},
		Residents: []budget.Resident{{ID: "coder", Holder: true, GPU: true, ModelMiB: 5000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if prediction.AllocationMiB != 1658 || prediction.RequiredMiB != 6024 || prediction.Status != budget.StatusExceeded || prediction.HasSuggestion {
		t.Fatalf("prediction = %#v", prediction)
	}
}

func TestPredictMarksCPUOnlyHolderNotApplicable(t *testing.T) {
	prediction, err := budget.Predict(budget.Input{
		Utilization: 0.85,
		Machine: budget.Machine{
			PhysicalMiB: 32768, DeviceMiB: 26542, WiredLimitMiB: 24576,
			WiredSource: budget.WiredSourceLive,
		},
		Residents: []budget.Resident{{ID: "coder", Holder: true, GPU: false, ModelMiB: 16000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if prediction.Status != budget.StatusNotApplicable || prediction.Reason == "" {
		t.Fatalf("prediction = %#v", prediction)
	}
}

func TestPredictRefusesAmbiguousOrImpossibleInputs(t *testing.T) {
	base := budget.Input{
		Utilization: 0.85,
		Machine: budget.Machine{
			PhysicalMiB: 32768, DeviceMiB: 26542, WiredLimitMiB: 24576,
			WiredSource: budget.WiredSourceLive,
		},
	}
	tests := []struct {
		name      string
		mutate    func(*budget.Input)
		wantError string
	}{
		{name: "bad fraction", mutate: func(input *budget.Input) { input.Utilization = 1.1 }, wantError: "utilization"},
		{name: "bad machine", mutate: func(input *budget.Input) { input.Machine.WiredLimitMiB = 40000 }, wantError: "physical"},
		{name: "duplicate resident", mutate: func(input *budget.Input) {
			input.Residents = []budget.Resident{{ID: "coder"}, {ID: "coder"}}
		}, wantError: "repeated"},
		{name: "two holders", mutate: func(input *budget.Input) {
			input.Residents = []budget.Resident{{ID: "one", Holder: true}, {ID: "two", Holder: true}}
		}, wantError: "more than one holder"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := base
			test.mutate(&input)
			_, err := budget.Predict(input)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want %q", err, test.wantError)
			}
		})
	}
}
