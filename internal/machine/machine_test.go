package machine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/software"
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

func TestDetectFactsReturnsCanonicalFieldKitMachineScope(t *testing.T) {
	facts, err := detectFacts(context.Background(), cannedQuery(map[string]string{
		"hw.memsize":               "34359738368\n",
		"iogpu.wired_limit_mb":     "24576\n",
		"hw.model":                 "Mac17,3\n",
		"machdep.cpu.brand_string": "Apple M5\n",
		"kern.osproductversion":    "15.6\n",
		"kern.osversion":           "24G90\n",
	}), "darwin", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	wantTarget := software.Target{
		OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6",
	}
	if facts.Schema != FactsSchemaV1 || facts.Target != wantTarget || facts.HardwareModel != "Mac17,3" || facts.Chip != "Apple M5" || facts.OSBuild != "24G90" {
		t.Fatalf("facts identity = %#v", facts)
	}
	if facts.PhysicalMemoryBytes != 34359738368 || facts.MetalDeviceMemoryMiB != 26542 || facts.MetalDeviceMemorySource != MetalDeviceSourcePredicted {
		t.Fatalf("facts memory = %#v", facts)
	}
	if facts.WiredLimitMiB != 24576 || facts.WiredLimitSource != budget.WiredSourceLive {
		t.Fatalf("facts wired limit = %#v", facts)
	}
	memory, err := facts.Budget()
	if err != nil {
		t.Fatal(err)
	}
	if memory != (budget.Machine{PhysicalMiB: 32768, DeviceMiB: 26542, WiredLimitMiB: 24576, WiredSource: budget.WiredSourceLive}) {
		t.Fatalf("Budget() = %#v", memory)
	}
}

func TestDetectTargetReturnsTheExactMacOSTarget(t *testing.T) {
	target, err := detectTarget(context.Background(), cannedQuery(map[string]string{
		"kern.osproductversion": "15.6\n",
	}), "darwin", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	want := software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6"}
	if target != want {
		t.Fatalf("target = %#v, want %#v", target, want)
	}
}

func TestDetectTargetRefusesAnUnreadableOrInvalidVersion(t *testing.T) {
	tests := []queryFunc{
		func(context.Context, string) (string, error) { return "", errors.New("denied") },
		func(context.Context, string) (string, error) { return "\n", nil },
	}
	for _, query := range tests {
		if _, err := detectTarget(context.Background(), query, "darwin", "arm64"); err == nil {
			t.Fatal("detectTarget() error = nil")
		}
	}
}

func TestDetectFactsLabelsPredictedWiredLimit(t *testing.T) {
	facts, err := detectFacts(context.Background(), func(_ context.Context, name string) (string, error) {
		values := map[string]string{
			"hw.memsize":               "34359738368",
			"hw.model":                 "Mac17,3",
			"machdep.cpu.brand_string": "Apple M5",
			"kern.osproductversion":    "15.6",
			"kern.osversion":           "24G90",
		}
		value, ok := values[name]
		if !ok {
			return "", errors.New("unknown oid")
		}
		return value, nil
	}, "darwin", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if facts.WiredLimitMiB != 21299 || facts.WiredLimitSource != budget.WiredSourcePredicted {
		t.Fatalf("facts = %#v", facts)
	}
}

func TestFactsRefusesNoncanonicalOrInconsistentIdentity(t *testing.T) {
	facts := Facts{
		Schema:        FactsSchemaV1,
		Target:        software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6"},
		HardwareModel: "Mac17,3", Chip: "Apple M5", OSBuild: "24G90",
		PhysicalMemoryBytes: 34359738368, MetalDeviceMemoryMiB: 26542,
		MetalDeviceMemorySource: MetalDeviceSourcePredicted,
		WiredLimitMiB:           21299, WiredLimitSource: budget.WiredSourcePredicted,
	}

	facts.Chip = " Apple M5"
	if err := facts.Validate(); err == nil || !strings.Contains(err.Error(), "chip") {
		t.Fatalf("Validate() error = %v", err)
	}
	facts.Chip = "Apple M5"
	facts.MetalDeviceMemoryMiB++
	if err := facts.Validate(); err == nil || !strings.Contains(err.Error(), "metal device") {
		t.Fatalf("Validate() error = %v", err)
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
