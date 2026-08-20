package software_test

import (
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software"
)

func TestTargetSelectorMatchingAndOverlap(t *testing.T) {
	darwin := software.Target{OS: "darwin", Arch: "arm64"}
	macOS15 := software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6"}
	ubuntu := software.Target{OS: "linux", Arch: "arm64", Distribution: "ubuntu"}
	ubuntu26 := software.Target{OS: "linux", Arch: "arm64", Distribution: "ubuntu", DistributionVersion: "26.04"}
	ubuntu24 := software.Target{OS: "linux", Arch: "arm64", Distribution: "ubuntu", DistributionVersion: "24.04"}

	if !darwin.Matches(macOS15) {
		t.Error("broad Darwin selector did not match an exact macOS target")
	}
	if ubuntu.Matches(ubuntu26) != true {
		t.Error("Ubuntu selector did not match an exact Ubuntu release")
	}
	if !ubuntu.Overlaps(ubuntu26) {
		t.Error("broad and narrow Ubuntu selectors should overlap")
	}
	if ubuntu26.Overlaps(ubuntu24) {
		t.Error("different exact Ubuntu versions should not overlap")
	}
	if darwin.Overlaps(ubuntu) {
		t.Error("different operating systems should not overlap")
	}
}

func TestTargetValidationRequiresNormalizedFacts(t *testing.T) {
	tests := []struct {
		name   string
		target software.Target
		want   string
	}{
		{name: "missing architecture", target: software.Target{OS: "darwin"}, want: "arch"},
		{name: "mixed case", target: software.Target{OS: "Darwin", Arch: "arm64"}, want: "normalized lowercase"},
		{name: "version without distribution", target: software.Target{OS: "linux", Arch: "arm64", DistributionVersion: "26.04"}, want: "requires distribution"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.target.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want %q", err, tt.want)
			}
		})
	}
}
