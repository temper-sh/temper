package machine_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/software"
)

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
