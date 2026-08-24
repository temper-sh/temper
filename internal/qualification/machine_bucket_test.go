package qualification_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/qualification"
	"github.com/temper-sh/temper/internal/software"
)

const gibibyte = int64(1024 * 1024 * 1024)

func TestParseMachineBucketRoundTripsCanonicalFixture(t *testing.T) {
	data := readFixture(t)

	bucket, err := qualification.ParseMachineBucket(data)
	if err != nil {
		t.Fatal(err)
	}
	if bucket.Schema != qualification.MachineBucketSchemaV1 || bucket.ID != "example-gen5-32g-standard" || bucket.Revision != 1 {
		t.Fatalf("bucket identity = %#v", bucket)
	}

	encoded, err := qualification.MarshalMachineBucket(bucket)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, data) {
		t.Fatalf("round trip changed canonical bytes\n got:\n%s\nwant:\n%s", encoded, data)
	}
	wantDigest := fmt.Sprintf("%x", sha256.Sum256(data))
	if got := qualification.Digest(data); got != wantDigest {
		t.Fatalf("Digest() = %q, want %q", got, wantDigest)
	}
}

func TestMachineBucketMatchesOnlyTheCompleteHardPredicate(t *testing.T) {
	bucket := parseFixture(t)

	tests := []struct {
		name   string
		mutate func(machine.Facts) machine.Facts
		want   bool
	}{
		{name: "matching facts", mutate: func(facts machine.Facts) machine.Facts { return facts }, want: true},
		{name: "another hardware model", mutate: func(facts machine.Facts) machine.Facts {
			facts.HardwareModel = "ExampleMac9,9"
			return facts
		}},
		{name: "another chip", mutate: func(facts machine.Facts) machine.Facts {
			facts.Chip = "Example Chip Z"
			return facts
		}},
		{name: "memory outside the inclusive range", mutate: func(facts machine.Facts) machine.Facts {
			return factsWithMemory(facts, 64*gibibyte)
		}},
		{name: "another target", mutate: func(facts machine.Facts) machine.Facts {
			facts.Target = software.Target{OS: "linux", Arch: "arm64", Distribution: "example", DistributionVersion: "1"}
			return facts
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched, err := bucket.Matches(tt.mutate(machineFacts()))
			if err != nil {
				t.Fatal(err)
			}
			if matched != tt.want {
				t.Errorf("Matches() = %t, want %t", matched, tt.want)
			}
		})
	}
}

func TestParseMachineBucketRefusesNoncanonicalOrAmbiguousYAML(t *testing.T) {
	canonical := string(readFixture(t))
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "unknown field",
			input: strings.Replace(canonical, "facts_schema:", "extra: forbidden\nfacts_schema:", 1),
			want:  "field extra not found",
		},
		{
			name:  "alias",
			input: strings.Replace(strings.Replace(canonical, "memory: Example 32 GiB", "memory: &shared Example 32 GiB", 1), "title: Example 32 GiB by generation and bandwidth", "title: *shared", 1),
			want:  "not canonical",
		},
		{
			name:  "duplicate key",
			input: strings.Replace(canonical, "id: example-gen5-32g-standard", "id: example-gen5-32g-standard\nid: duplicate", 1),
			want:  "mapping key \"id\" already defined",
		},
		{
			name:  "multiple documents",
			input: canonical + "---\nnull\n",
			want:  "multiple YAML documents",
		},
		{
			name:  "missing final newline",
			input: strings.TrimSuffix(canonical, "\n"),
			want:  "not canonical",
		},
		{
			name:  "noncanonical integer",
			input: strings.Replace(canonical, "revision: 1", "revision: 01", 1),
			want:  "not canonical",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := qualification.ParseMachineBucket([]byte(tt.input))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParseMachineBucket() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestMachineBucketValidationRefusesInvalidDomainFacts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*qualification.MachineBucket)
		want   string
	}{
		{name: "unknown schema", mutate: func(bucket *qualification.MachineBucket) { bucket.Schema = "unknown/v1" }, want: "schema is"},
		{name: "unstable id", mutate: func(bucket *qualification.MachineBucket) { bucket.ID = "Example_Bucket" }, want: "lowercase stable id"},
		{name: "zero revision", mutate: func(bucket *qualification.MachineBucket) { bucket.Revision = 0 }, want: "revision must be greater"},
		{name: "untrimmed title", mutate: func(bucket *qualification.MachineBucket) { bucket.Title = " Example" }, want: "title must be"},
		{name: "unknown facts schema", mutate: func(bucket *qualification.MachineBucket) { bucket.FactsSchema = "unknown/v1" }, want: "facts_schema"},
		{name: "versioned target", mutate: func(bucket *qualification.MachineBucket) { bucket.Predicate.Target.DistributionVersion = "99" }, want: "without a version"},
		{name: "empty model set", mutate: func(bucket *qualification.MachineBucket) { bucket.Predicate.HardwareModels = nil }, want: "hardware_models must not be empty"},
		{name: "unsorted chip set", mutate: func(bucket *qualification.MachineBucket) {
			bucket.Predicate.Chips[0], bucket.Predicate.Chips[1] = bucket.Predicate.Chips[1], bucket.Predicate.Chips[0]
		}, want: "chips must be unique and sorted"},
		{name: "inverted memory range", mutate: func(bucket *qualification.MachineBucket) { bucket.Predicate.PhysicalMemoryBytes.Maximum = 1 }, want: "maximum must be greater"},
		{name: "empty axis label", mutate: func(bucket *qualification.MachineBucket) { bucket.AxisLabels.MemoryBandwidth = "" }, want: "memory_bandwidth must be"},
		{name: "unknown evidence kind", mutate: func(bucket *qualification.MachineBucket) { bucket.Evidence[0].Kind = "raw-labs-run" }, want: "kind \"raw-labs-run\" is not supported"},
		{name: "invalid evidence digest", mutate: func(bucket *qualification.MachineBucket) { bucket.Evidence[0].SHA256 = "nope" }, want: "sha256 must be"},
		{name: "empty evidence", mutate: func(bucket *qualification.MachineBucket) { bucket.Evidence = nil }, want: "evidence must not be empty"},
		{name: "empty invalidation triggers", mutate: func(bucket *qualification.MachineBucket) { bucket.InvalidationTriggers = nil }, want: "invalidation_triggers must not be empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket := parseFixture(t)
			tt.mutate(&bucket)

			_, err := qualification.MarshalMachineBucket(bucket)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("MarshalMachineBucket() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestMachineBucketMatchesRefusesInvalidFacts(t *testing.T) {
	bucket := parseFixture(t)
	facts := machineFacts()
	facts.PhysicalMemoryBytes = 0

	matched, err := bucket.Matches(facts)
	if err == nil || !strings.Contains(err.Error(), "machine facts invalid") {
		t.Fatalf("Matches() = %t, %v, want invalid-facts refusal", matched, err)
	}
}

func readFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/machine-bucket.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func parseFixture(t *testing.T) qualification.MachineBucket {
	t.Helper()
	bucket, err := qualification.ParseMachineBucket(readFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	return bucket
}

func machineFacts() machine.Facts {
	physicalBytes := 32 * gibibyte
	physicalMiB := physicalBytes / (1024 * 1024)
	return machine.Facts{
		Schema: machine.FactsSchemaV1,
		Target: software.Target{
			OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "99.1",
		},
		HardwareModel:           "ExampleMac1,1",
		Chip:                    "Example Chip A",
		OSBuild:                 "99A1",
		PhysicalMemoryBytes:     physicalBytes,
		MetalDeviceMemoryMiB:    physicalMiB * 81 / 100,
		MetalDeviceMemorySource: machine.MetalDeviceSourcePredicted,
		WiredLimitMiB:           physicalMiB * 65 / 100,
		WiredLimitSource:        budget.WiredSourcePredicted,
	}
}

func factsWithMemory(facts machine.Facts, physicalBytes int64) machine.Facts {
	physicalMiB := physicalBytes / (1024 * 1024)
	facts.PhysicalMemoryBytes = physicalBytes
	facts.MetalDeviceMemoryMiB = physicalMiB * 81 / 100
	facts.WiredLimitMiB = physicalMiB * 65 / 100
	return facts
}
