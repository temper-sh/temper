package runtimeconfig_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/runtimeconfig"
)

func TestSplashRelaunchRoundTripAllowsOnlyTheExactFixedContextCommand(t *testing.T) {
	initial := []string{"serve-native", "/prepared", "32768", "25769803776", "0", "--kv-format", "bf16"}
	relaunch := append(slices.Clone(initial), "--relaunch-context", "32768")
	for _, scenario := range []string{"valid", "context", "target", "extra", "automatic context", "frontend", "other package"} {
		t.Run(scenario, func(t *testing.T) {
			r := runtimeconfig.Requirement{Package: "splash", RelativeExecutable: "engine/splash", Role: "engine", Arguments: slices.Clone(initial), RelaunchArguments: slices.Clone(relaunch)}
			switch scenario {
			case "context":
				r.RelaunchArguments[len(relaunch)-1] = "16384"
			case "target":
				r.RelaunchArguments[1] = "/other"
			case "extra":
				r.RelaunchArguments = append(r.RelaunchArguments, "--arbitrary")
			case "automatic context":
				r.Arguments[2], r.RelaunchArguments[2], r.RelaunchArguments[len(relaunch)-1] = "0", "0", "0"
			case "frontend":
				r.Role = "frontend"
			case "other package":
				r.Package = "vllm-metal"
			}
			d := runtimeconfig.Document{Schema: runtimeconfig.SchemaV1, Requirements: []runtimeconfig.Requirement{
				{Package: "llama-swap", RelativeExecutable: "llama-swap"}, r,
				{Package: "splash", RelativeExecutable: "python/bin/python3.13", Role: "frontend", Arguments: []string{"bootstrap"}},
			}}
			raw, err := runtimeconfig.Marshal(d)
			if scenario != "valid" {
				if err == nil {
					t.Fatal("invalid relaunch authority serialized")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := runtimeconfig.Parse(raw)
			if err != nil || !slices.Equal(parsed.Requirements[1].RelaunchArguments, relaunch) {
				t.Fatal("relaunch authority changed in round trip", parsed, err)
			}
		})
	}
}

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
		{name: "Kapitsa missing identity", requirements: []runtimeconfig.Requirement{{Package: "kapitsa", RelativeExecutable: "kapitsa"}, {Package: "llama-swap", RelativeExecutable: "llama-swap"}}, want: "exact console command identity"},
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
