package machine_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/software"
)

func TestHistoricalFactsKeepTheirExactCanonicalBytes(t *testing.T) {
	const historical = `schema: temper-machine-facts/v1
target:
  os: darwin
  arch: arm64
  distribution: macos
  distribution_version: "15.6"
hardware_model: Mac17,3
chip: Apple M5
os_build: 24G90
physical_memory_bytes: 34359738368
metal_device_memory_mib: 26542
metal_device_memory_source: predicted-metal-81-percent
wired_limit_mib: 24576
wired_limit_source: live-sysctl
`
	facts, err := machine.ParseFacts([]byte(historical))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := machine.MarshalFacts(facts)
	if err != nil || string(raw) != historical {
		t.Fatalf("historical bytes changed: %s, %v", raw, err)
	}
}

func TestLiveFactsRoundTripPreservesZeroAndAbsentOverrides(t *testing.T) {
	zero := int64(0)
	for _, override := range []*int64{nil, &zero} {
		facts := canonicalFacts()
		facts.MetalDeviceMemoryMiB, facts.WiredLimitMiB = 24576, 24576
		facts.MetalDeviceMemorySource, facts.WiredLimitSource = machine.MetalDeviceSourceLive, budget.WiredSourceMetal
		facts.WiredLimitOverrideMiB = override
		raw, err := machine.MarshalFacts(facts)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := machine.ParseFacts(raw)
		if err != nil || !reflect.DeepEqual(parsed, facts) {
			t.Fatalf("round trip lost live facts: %#v, %v", parsed, err)
		}
		if bytes.Contains(raw, []byte("wired_limit_override_mib:")) != (override != nil) {
			t.Fatalf("zero confused with unavailable: %s", raw)
		}
	}
}

func TestFactsCanonicalRoundTrip(t *testing.T) {
	facts := canonicalFacts()
	data, err := machine.MarshalFacts(facts)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := machine.ParseFacts(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parsed, facts) {
		t.Fatalf("round trip changed facts\n got: %#v\nwant: %#v", parsed, facts)
	}
	if !strings.HasPrefix(string(data), "schema: temper-machine-facts/v1\n") {
		t.Fatalf("unexpected canonical bytes:\n%s", data)
	}
}

func TestParseFactsRefusesAlternateAndUnknownBytes(t *testing.T) {
	data, err := machine.MarshalFacts(canonicalFacts())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := machine.ParseFacts(append(data, '\n')); err == nil || !strings.Contains(err.Error(), "not canonical") {
		t.Fatalf("alternate bytes error = %v", err)
	}
	unknown := strings.Replace(string(data), "schema:", "unknown: value\nschema:", 1)
	if _, err := machine.ParseFacts([]byte(unknown)); err == nil || !strings.Contains(err.Error(), "field unknown") {
		t.Fatalf("unknown field error = %v", err)
	}
}

func canonicalFacts() machine.Facts {
	return machine.Facts{
		Schema: machine.FactsSchemaV1,
		Target: software.Target{
			OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6",
		},
		HardwareModel: "Mac17,3", Chip: "Apple M5", OSBuild: "24G90",
		PhysicalMemoryBytes:     34359738368,
		MetalDeviceMemoryMiB:    26542,
		MetalDeviceMemorySource: machine.MetalDeviceSourcePredicted,
		WiredLimitMiB:           24576, WiredLimitSource: budget.WiredSourceLive,
	}
}
