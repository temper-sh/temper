package machine_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/software"
)

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
		var parsed machine.Facts
		err = yaml.Unmarshal(raw, &parsed)
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
	var parsed machine.Facts
	err = yaml.Unmarshal(data, &parsed)
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

func canonicalFacts() machine.Facts {
	return machine.Facts{
		Schema: machine.FactsSchemaV1,
		Target: software.Target{
			OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6",
		},
		HardwareModel: "Mac17,3", Chip: "Apple M5", OSBuild: "24G90",
		PhysicalMemoryBytes:     34359738368,
		MetalDeviceMemoryMiB:    24576,
		MetalDeviceMemorySource: machine.MetalDeviceSourceLive,
		WiredLimitMiB:           24576, WiredLimitSource: budget.WiredSourceMetal,
	}
}
