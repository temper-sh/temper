package machine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/software"
)

func TestDetectUsesMetalWorkingSetInsteadOfPercentagesOrOverride(t *testing.T) {
	machine, err := detect(context.Background(), cannedQuery(map[string]string{
		"hw.memsize":           "34359738368\n",
		"iogpu.wired_limit_mb": "28672\n",
	}), metalReading(24576))
	if err != nil {
		t.Fatal(err)
	}
	if machine.PhysicalMiB != 32768 || machine.DeviceMiB != 24576 || machine.WiredLimitMiB != 24576 || machine.WiredSource != budget.WiredSourceMetal {
		t.Fatalf("machine = %#v", machine)
	}
}

func TestDetectRefusesUnavailableOrImpossibleMetalBudget(t *testing.T) {
	for _, tc := range []struct {
		name  string
		bytes uint64
		err   error
	}{
		{"no device", 0, errors.New("no default Metal device")},
		{"zero", 0, nil},
		{"below one MiB", uint64(bytesPerMiB - 1), nil},
		{"above physical memory", 34359738369, nil},
		{"overflow", ^uint64(0), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := detect(context.Background(), cannedQuery(map[string]string{"hw.memsize": "34359738368"}),
				func(context.Context) (uint64, error) { return tc.bytes, tc.err })
			if err == nil || !strings.Contains(err.Error(), "Metal") {
				t.Fatalf("missing Metal reading silently became an estimate: %v", err)
			}
			if tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatalf("underlying failure lost: %v", err)
			}
		})
	}
}

func TestDetectRefusesAnUnreadablePhysicalCapacity(t *testing.T) {
	tests := []queryFunc{
		func(context.Context, string) (string, error) { return "", errors.New("denied") },
		func(context.Context, string) (string, error) { return "not-a-number", nil },
	}
	for _, query := range tests {
		_, err := detect(context.Background(), query, metalReading(24576))
		if err == nil || !strings.Contains(err.Error(), "physical memory") {
			t.Fatalf("error = %v", err)
		}
	}
}

func TestDetectHonorsCancellationBeforeMetalRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	_, err := detect(ctx, func(_ context.Context, name string) (string, error) {
		if name == "hw.memsize" {
			cancel()
			return "34359738368", nil
		}
		return "", context.Canceled
	}, func(context.Context) (uint64, error) { t.Fatal("Metal queried after cancellation"); return 0, nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestDetectHonorsCancellationDuringMetalRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	_, err := detect(ctx, cannedQuery(map[string]string{"hw.memsize": "34359738368"}),
		func(context.Context) (uint64, error) { cancel(); return 24576 * uint64(bytesPerMiB), nil })
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
	}), metalReading(24576), "darwin", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	wantTarget := software.Target{
		OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6",
	}
	if facts.Schema != FactsSchemaV1 || facts.Target != wantTarget || facts.HardwareModel != "Mac17,3" || facts.Chip != "Apple M5" || facts.OSBuild != "24G90" {
		t.Fatalf("facts identity = %#v", facts)
	}
	if facts.PhysicalMemoryBytes != 34359738368 || facts.MetalDeviceMemoryMiB != 24576 || facts.MetalDeviceMemorySource != MetalDeviceSourceLive {
		t.Fatalf("facts memory = %#v", facts)
	}
	if facts.WiredLimitMiB != 24576 || facts.WiredLimitSource != budget.WiredSourceMetal || facts.WiredLimitOverrideMiB == nil || *facts.WiredLimitOverrideMiB != 24576 {
		t.Fatalf("facts wired limit = %#v", facts)
	}
	memory, err := facts.Budget()
	if err != nil {
		t.Fatal(err)
	}
	if memory != (budget.Machine{PhysicalMiB: 32768, DeviceMiB: 24576, WiredLimitMiB: 24576, WiredSource: budget.WiredSourceMetal}) {
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

func TestDetectFactsKeepsOverrideSeparateFromEffectiveBudget(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		known       bool
		want        int64
	}{
		{"different override", "28672\n", true, 28672},
		{"OS default", "0\n", true, 0},
		{"unavailable", "", false, 0},
		{"malformed", "unknown", false, 0},
		{"negative", "-1", false, 0},
		{"above RAM", "65536", true, 65536},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{
				"hw.memsize":               "34359738368",
				"hw.model":                 "Mac17,3",
				"machdep.cpu.brand_string": "Apple M5",
				"kern.osproductversion":    "15.6",
				"kern.osversion":           "24G90",
			}
			if tc.value != "" {
				values["iogpu.wired_limit_mb"] = tc.value
			}
			facts, err := detectFacts(context.Background(), cannedQuery(values),
				func(context.Context) (uint64, error) { return 24576*uint64(bytesPerMiB) + 1023, nil }, "darwin", "arm64")
			if err != nil {
				t.Fatal(err)
			}
			if facts.WiredLimitMiB != 24576 || facts.MetalDeviceMemoryMiB != 24576 || facts.WiredLimitSource != budget.WiredSourceMetal {
				t.Fatalf("override replaced effective Metal budget: %#v", facts)
			}
			if (facts.WiredLimitOverrideMiB != nil) != tc.known || tc.known && *facts.WiredLimitOverrideMiB != tc.want {
				t.Fatalf("wrong optional override: %#v", facts)
			}
		})
	}
}

func TestLiveMetalFactsRefuseInconsistentReadings(t *testing.T) {
	facts := Facts{Schema: FactsSchemaV1,
		Target:        software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "26.6"},
		HardwareModel: "Mac17,3", Chip: "Apple M5", OSBuild: "25G76", PhysicalMemoryBytes: 34359738368,
		MetalDeviceMemoryMiB: 24576, MetalDeviceMemorySource: MetalDeviceSourceLive,
		WiredLimitMiB: 24576, WiredLimitSource: budget.WiredSourceMetal}
	if err := facts.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Facts){
		func(f *Facts) { f.WiredLimitMiB++ },
		func(f *Facts) { f.MetalDeviceMemorySource = MetalDeviceSourcePredicted },
		func(f *Facts) { f.WiredLimitSource = budget.WiredSourceLive },
		func(f *Facts) { f.MetalDeviceMemoryMiB = 0 },
		func(f *Facts) { negative := int64(-1); f.WiredLimitOverrideMiB = &negative },
	} {
		invalid := facts
		mutate(&invalid)
		if err := invalid.Validate(); err == nil {
			t.Fatalf("invalid facts accepted: %#v", invalid)
		}
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

func metalReading(mib int64) metalQueryFunc {
	return func(context.Context) (uint64, error) { return uint64(mib * bytesPerMiB), nil }
}
