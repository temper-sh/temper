package engine

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

var appleMChip = regexp.MustCompile(`^Apple M([0-9]+)(?:\s|$)`)

// SplashCompatibility is the upstream 1.1.0 platform floor, not a memory-fit claim.
func SplashCompatibility(chip, macOS string) error {
	match := appleMChip.FindStringSubmatch(chip)
	generation := 0
	if len(match) > 1 {
		generation, _ = strconv.Atoi(match[1])
	}
	parts := strings.Split(macOS, ".")
	major, minor := 0, 0
	if len(parts) > 0 {
		major, _ = strconv.Atoi(parts[0])
	}
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	if generation < 3 || major < 26 || major == 26 && minor < 4 {
		return errors.New("Splash 1.1.0 requires Apple M3 or newer and macOS 26.4 or later")
	}
	return nil
}
