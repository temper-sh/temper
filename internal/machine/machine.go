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
)

const bytesPerMiB int64 = 1024 * 1024

// Detect reads macOS memory facts without changing machine state.
func Detect(ctx context.Context) (budget.Machine, error) {
	if runtime.GOOS != "darwin" {
		return budget.Machine{}, errors.New("wall-model machine detection requires macOS")
	}
	return detect(ctx, sysctl)
}

type queryFunc func(context.Context, string) (string, error)

func detect(ctx context.Context, query queryFunc) (budget.Machine, error) {
	if err := ctx.Err(); err != nil {
		return budget.Machine{}, err
	}
	physicalText, err := query(ctx, "hw.memsize")
	if err != nil {
		return budget.Machine{}, fmt.Errorf("read physical memory: %w", err)
	}
	physicalBytes, err := parsePositive(physicalText)
	if err != nil {
		return budget.Machine{}, fmt.Errorf("read physical memory: %w", err)
	}
	physicalMiB := physicalBytes / bytesPerMiB
	if physicalMiB <= 0 {
		return budget.Machine{}, errors.New("read physical memory: value is below one MiB")
	}

	machine := budget.Machine{
		PhysicalMiB: physicalMiB,
		DeviceMiB:   physicalMiB * 81 / 100,
		WiredSource: budget.WiredSourcePredicted,
	}
	wiredText, wiredErr := query(ctx, "iogpu.wired_limit_mb")
	if err := ctx.Err(); err != nil {
		return budget.Machine{}, err
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
		return budget.Machine{}, errors.New("machine reported impossible memory capacities")
	}
	return machine, nil
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
