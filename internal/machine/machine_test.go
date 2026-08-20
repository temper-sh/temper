package machine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/budget"
)

func TestDetectUsesLiveWiredLimit(t *testing.T) {
	machine, err := detect(context.Background(), cannedQuery(map[string]string{
		"hw.memsize":           "34359738368\n",
		"iogpu.wired_limit_mb": "24576\n",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if machine.PhysicalMiB != 32768 || machine.DeviceMiB != 26542 || machine.WiredLimitMiB != 24576 || machine.WiredSource != budget.WiredSourceLive {
		t.Fatalf("machine = %#v", machine)
	}
}

func TestDetectLabelsThePredictedDefaultWhenLiveLimitIsAbsent(t *testing.T) {
	machine, err := detect(context.Background(), func(_ context.Context, name string) (string, error) {
		if name == "hw.memsize" {
			return "34359738368", nil
		}
		return "", errors.New("unknown oid")
	})
	if err != nil {
		t.Fatal(err)
	}
	if machine.WiredLimitMiB != 21299 || machine.WiredSource != budget.WiredSourcePredicted {
		t.Fatalf("machine = %#v", machine)
	}
}

func TestDetectTreatsANonpositiveLiveLimitAsAbsent(t *testing.T) {
	machine, err := detect(context.Background(), cannedQuery(map[string]string{
		"hw.memsize":           "34359738368",
		"iogpu.wired_limit_mb": "0",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if machine.WiredLimitMiB != 21299 || machine.WiredSource != budget.WiredSourcePredicted {
		t.Fatalf("machine = %#v", machine)
	}
}

func TestDetectRefusesAnUnreadablePhysicalCapacity(t *testing.T) {
	tests := []queryFunc{
		func(context.Context, string) (string, error) { return "", errors.New("denied") },
		func(context.Context, string) (string, error) { return "not-a-number", nil },
	}
	for _, query := range tests {
		_, err := detect(context.Background(), query)
		if err == nil || !strings.Contains(err.Error(), "physical memory") {
			t.Fatalf("error = %v", err)
		}
	}
}

func TestDetectHonorsCancellationDuringTheOptionalRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	_, err := detect(ctx, func(_ context.Context, name string) (string, error) {
		if name == "hw.memsize" {
			cancel()
			return "34359738368", nil
		}
		return "", context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func cannedQuery(values map[string]string) queryFunc {
	return func(_ context.Context, name string) (string, error) {
		value, ok := values[name]
		if !ok {
			return "", errors.New("missing fixture")
		}
		return value, nil
	}
}
