// Package machine reads the local machine facts required by Temper checks.
package machine

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/software"
)

const (
	bytesPerMiB int64 = 1024 * 1024

	FactsSchemaV1              = "temper-machine-facts/v1"
	MetalDeviceSourcePredicted = "predicted-metal-81-percent"
)

// Facts is the canonical machine scope bound into a Field Kit packet. It
// carries exact host identity and the labeled memory values used by Temper's
// wall model, but no serial number or other stable personal identifier.
type Facts struct {
	Schema                  string          `yaml:"schema"`
	Target                  software.Target `yaml:"target"`
	HardwareModel           string          `yaml:"hardware_model"`
	Chip                    string          `yaml:"chip"`
	OSBuild                 string          `yaml:"os_build"`
	PhysicalMemoryBytes     int64           `yaml:"physical_memory_bytes"`
	MetalDeviceMemoryMiB    int64           `yaml:"metal_device_memory_mib"`
	MetalDeviceMemorySource string          `yaml:"metal_device_memory_source"`
	WiredLimitMiB           int64           `yaml:"wired_limit_mib"`
	WiredLimitSource        string          `yaml:"wired_limit_source"`
}

// Validate enforces the canonical machine-facts schema without reading the
// host. Distribution fields are required because packet identity is an exact
// witness scope rather than a target selector.
func (f Facts) Validate() error {
	var problems []string
	problem := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	if f.Schema != FactsSchemaV1 {
		problem("schema is %q, want %q", f.Schema, FactsSchemaV1)
	}
	if err := f.Target.Validate(); err != nil {
		problem("target: %v", err)
	} else if f.Target.Distribution == "" || f.Target.DistributionVersion == "" {
		problem("target distribution and distribution_version are required")
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "hardware_model", value: f.HardwareModel},
		{name: "chip", value: f.Chip},
		{name: "os_build", value: f.OSBuild},
	} {
		if !canonicalLabel(field.value) {
			problem("%s must be nonempty, trimmed, and contain no control characters", field.name)
		}
	}
	if f.PhysicalMemoryBytes <= 0 {
		problem("physical_memory_bytes must be positive")
	}
	physicalMiB := f.PhysicalMemoryBytes / bytesPerMiB
	if physicalMiB <= 0 {
		problem("physical_memory_bytes must be at least one MiB")
	}
	if f.MetalDeviceMemorySource != MetalDeviceSourcePredicted {
		problem("metal_device_memory_source is %q, want %q", f.MetalDeviceMemorySource, MetalDeviceSourcePredicted)
	}
	if f.MetalDeviceMemoryMiB != physicalMiB*81/100 {
		problem("metal device memory must equal the labeled 81-percent prediction")
	}
	if f.WiredLimitMiB <= 0 || f.WiredLimitMiB > physicalMiB {
		problem("wired_limit_mib must be positive and no greater than physical memory")
	}
	switch f.WiredLimitSource {
	case budget.WiredSourceLive:
	case budget.WiredSourcePredicted:
		if f.WiredLimitMiB != physicalMiB*65/100 {
			problem("predicted wired limit must equal the conservative macOS default")
		}
	default:
		problem("wired_limit_source %q is not supported", f.WiredLimitSource)
	}
	if len(problems) > 0 {
		return errors.New("machine facts invalid: " + strings.Join(problems, "; "))
	}
	return nil
}

// Budget projects the canonical packet facts into the wall-model value.
func (f Facts) Budget() (budget.Machine, error) {
	if err := f.Validate(); err != nil {
		return budget.Machine{}, err
	}
	return budget.Machine{
		PhysicalMiB:   f.PhysicalMemoryBytes / bytesPerMiB,
		DeviceMiB:     f.MetalDeviceMemoryMiB,
		WiredLimitMiB: f.WiredLimitMiB,
		WiredSource:   f.WiredLimitSource,
	}, nil
}

// DetectFacts reads the exact, non-identifying macOS hardware and memory scope
// required for a Field Kit binding without changing machine state.
func DetectFacts(ctx context.Context) (Facts, error) {
	if runtime.GOOS != "darwin" {
		return Facts{}, errors.New("field-kit machine detection requires macOS")
	}
	return detectFacts(ctx, sysctl, runtime.GOOS, runtime.GOARCH)
}

// DetectTarget reads the exact host target bound to public software commands.
// Only the reviewed macOS adapter target is supported in the current slice.
func DetectTarget(ctx context.Context) (software.Target, error) {
	if runtime.GOOS != "darwin" {
		return software.Target{}, errors.New("software target detection requires macOS")
	}
	return detectTarget(ctx, sysctl, runtime.GOOS, runtime.GOARCH)
}

// Detect reads macOS memory facts without changing machine state.
func Detect(ctx context.Context) (budget.Machine, error) {
	if runtime.GOOS != "darwin" {
		return budget.Machine{}, errors.New("wall-model machine detection requires macOS")
	}
	return detect(ctx, sysctl)
}

type queryFunc func(context.Context, string) (string, error)

func detect(ctx context.Context, query queryFunc) (budget.Machine, error) {
	machine, _, err := detectMemory(ctx, query)
	return machine, err
}

func detectMemory(ctx context.Context, query queryFunc) (budget.Machine, int64, error) {
	if err := ctx.Err(); err != nil {
		return budget.Machine{}, 0, err
	}
	physicalText, err := query(ctx, "hw.memsize")
	if err != nil {
		return budget.Machine{}, 0, fmt.Errorf("read physical memory: %w", err)
	}
	physicalBytes, err := parsePositive(physicalText)
	if err != nil {
		return budget.Machine{}, 0, fmt.Errorf("read physical memory: %w", err)
	}
	physicalMiB := physicalBytes / bytesPerMiB
	if physicalMiB <= 0 {
		return budget.Machine{}, 0, errors.New("read physical memory: value is below one MiB")
	}

	machine := budget.Machine{
		PhysicalMiB: physicalMiB,
		DeviceMiB:   physicalMiB * 81 / 100,
		WiredSource: budget.WiredSourcePredicted,
	}
	wiredText, wiredErr := query(ctx, "iogpu.wired_limit_mb")
	if err := ctx.Err(); err != nil {
		return budget.Machine{}, 0, err
	}
	if wiredErr == nil {
		wiredMiB, parseErr := parsePositive(wiredText)
		if parseErr == nil {
			machine.WiredLimitMiB = wiredMiB
			machine.WiredSource = budget.WiredSourceLive
		}
	}
	if machine.WiredLimitMiB == 0 {
		machine.WiredLimitMiB = physicalMiB * 65 / 100
	}
	if machine.DeviceMiB <= 0 || machine.WiredLimitMiB <= 0 || machine.WiredLimitMiB > physicalMiB {
		return budget.Machine{}, 0, errors.New("machine reported impossible memory capacities")
	}
	return machine, physicalBytes, nil
}

func detectFacts(ctx context.Context, query queryFunc, operatingSystem, architecture string) (Facts, error) {
	memory, physicalBytes, err := detectMemory(ctx, query)
	if err != nil {
		return Facts{}, err
	}
	readRequired := func(name, label string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		value, err := query(ctx, name)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", label, err)
		}
		value = strings.TrimSpace(value)
		if !canonicalLabel(value) {
			return "", fmt.Errorf("read %s: value is empty or noncanonical", label)
		}
		return value, nil
	}
	hardwareModel, err := readRequired("hw.model", "hardware model")
	if err != nil {
		return Facts{}, err
	}
	chip, err := readRequired("machdep.cpu.brand_string", "chip")
	if err != nil {
		return Facts{}, err
	}
	target, err := detectTarget(ctx, query, operatingSystem, architecture)
	if err != nil {
		return Facts{}, err
	}
	osBuild, err := readRequired("kern.osversion", "macOS build")
	if err != nil {
		return Facts{}, err
	}
	facts := Facts{
		Schema:        FactsSchemaV1,
		Target:        target,
		HardwareModel: hardwareModel, Chip: chip, OSBuild: osBuild,
		PhysicalMemoryBytes:     physicalBytes,
		MetalDeviceMemoryMiB:    memory.DeviceMiB,
		MetalDeviceMemorySource: MetalDeviceSourcePredicted,
		WiredLimitMiB:           memory.WiredLimitMiB,
		WiredLimitSource:        memory.WiredSource,
	}
	if err := facts.Validate(); err != nil {
		return Facts{}, err
	}
	return facts, nil
}

func detectTarget(ctx context.Context, query queryFunc, operatingSystem, architecture string) (software.Target, error) {
	if err := ctx.Err(); err != nil {
		return software.Target{}, err
	}
	productVersion, err := query(ctx, "kern.osproductversion")
	if err != nil {
		return software.Target{}, fmt.Errorf("read macOS product version: %w", err)
	}
	productVersion = strings.TrimSpace(productVersion)
	if !canonicalLabel(productVersion) {
		return software.Target{}, errors.New("read macOS product version: value is empty or noncanonical")
	}
	target := software.Target{
		OS: operatingSystem, Arch: architecture,
		Distribution: "macos", DistributionVersion: productVersion,
	}
	if err := target.Validate(); err != nil {
		return software.Target{}, fmt.Errorf("host target: %w", err)
	}
	return target, nil
}

func sysctl(ctx context.Context, name string) (string, error) {
	output, err := exec.CommandContext(ctx, "/usr/sbin/sysctl", "-n", name).Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func parsePositive(value string) (int64, error) {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %q: %w", strings.TrimSpace(value), err)
	}
	if parsed <= 0 {
		return 0, errors.New("value must be positive")
	}
	return parsed, nil
}

func canonicalLabel(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n\x00")
}
