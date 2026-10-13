package runtimeconfig_test

import (
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/runtimeconfig"
)

func TestCanonicalRoundTrip(t *testing.T) {
	document := runtimeconfig.Document{
		Schema: runtimeconfig.SchemaV1,
		Requirements: []runtimeconfig.Requirement{
			{Package: "llama-cpp", RelativeExecutable: "llama-server"},
			{Package: "llama-swap", RelativeExecutable: "llama-swap"},
			{Package: "rapid-mlx", RelativeExecutable: "bin/rapid-mlx"},
		},
	}
	data, err := runtimeconfig.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := runtimeconfig.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Requirements) != 3 || parsed.Requirements[2].Package != "rapid-mlx" {
		t.Fatalf("Parse() = %#v", parsed)
	}
}

func TestValidationRefusesUnsafeOrAmbiguousRequirements(t *testing.T) {
	tests := []struct {
		name         string
		requirements []runtimeconfig.Requirement
		want         string
	}{
		{name: "no router", requirements: []runtimeconfig.Requirement{{Package: "rapid-mlx", RelativeExecutable: "bin/rapid-mlx"}}, want: "exact llama-swap"},
		{name: "escape", requirements: []runtimeconfig.Requirement{{Package: "llama-swap", RelativeExecutable: "llama-swap"}, {Package: "rapid-mlx", RelativeExecutable: "../rapid-mlx"}}, want: "safe relative path"},
		{name: "duplicate", requirements: []runtimeconfig.Requirement{{Package: "llama-swap", RelativeExecutable: "llama-swap"}, {Package: "llama-swap", RelativeExecutable: "llama-swap"}}, want: "repeat"},
		{name: "unsorted", requirements: []runtimeconfig.Requirement{{Package: "rapid-mlx", RelativeExecutable: "bin/rapid-mlx"}, {Package: "llama-swap", RelativeExecutable: "llama-swap"}}, want: "sorted"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := (runtimeconfig.Document{Schema: runtimeconfig.SchemaV1, Requirements: test.requirements}).Validate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want %q", err, test.want)
			}
		})
	}
}
