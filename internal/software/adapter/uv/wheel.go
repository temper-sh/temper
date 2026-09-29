package uv

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/temper-sh/temper/internal/software/version"
)

var (
	wheelBuildTagPattern    = regexp.MustCompile(`^[0-9][0-9a-zA-Z_.]*$`)
	wheelPythonTagPattern   = regexp.MustCompile(`^(cp|py)([0-9]+)$`)
	wheelMacPlatformPattern = regexp.MustCompile(`^macosx_[0-9]+_[0-9]+_(arm64|universal2)$`)
	pythonReleasePattern    = regexp.MustCompile(`^([0-9]+)\.([0-9]+)`)
)

func validateWheelFilename(filename, packageName, packageVersion, runtimeVersion string) error {
	base := strings.TrimSuffix(filename, ".whl")
	parts := strings.Split(base, "-")
	if len(parts) != 5 && len(parts) != 6 {
		return fmt.Errorf("wheel filename %q does not have a supported tag shape", filename)
	}
	distribution := strings.ToLower(strings.ReplaceAll(parts[0], "_", "-"))
	if distribution != packageName {
		return fmt.Errorf("wheel filename %q does not name package %q", filename, packageName)
	}
	order, err := version.Compare("pep440", parts[1], packageVersion)
	if err != nil || order != 0 {
		return fmt.Errorf("wheel filename %q does not name package version %q", filename, packageVersion)
	}
	if len(parts) == 6 && !wheelBuildTagPattern.MatchString(parts[2]) {
		return fmt.Errorf("wheel filename %q build tag is invalid", filename)
	}
	pythonTag, abiTag, platformTag := parts[len(parts)-3], parts[len(parts)-2], parts[len(parts)-1]
	if !wheelTagsCompatible(pythonTag, abiTag, platformTag, runtimeVersion) {
		return fmt.Errorf("wheel filename %q is not compatible with CPython %s on darwin/arm64", filename, runtimeVersion)
	}
	return nil
}

func wheelTagsCompatible(pythonTags, abiTags, platformTags, runtimeVersion string) bool {
	release := pythonReleasePattern.FindStringSubmatch(runtimeVersion)
	if release == nil {
		return false
	}
	major, majorErr := strconv.Atoi(release[1])
	minor, minorErr := strconv.Atoi(release[2])
	if majorErr != nil || minorErr != nil {
		return false
	}
	for _, platformTag := range strings.Split(platformTags, ".") {
		platformAny := platformTag == "any"
		if !platformAny && !wheelMacPlatformPattern.MatchString(platformTag) {
			continue
		}
		for _, pythonTag := range strings.Split(pythonTags, ".") {
			for _, abiTag := range strings.Split(abiTags, ".") {
				if platformAny && abiTag != "none" {
					continue
				}
				if pythonABICompatible(pythonTag, abiTag, major, minor) {
					return true
				}
			}
		}
	}
	return false
}

func pythonABICompatible(pythonTag, abiTag string, runtimeMajor, runtimeMinor int) bool {
	match := wheelPythonTagPattern.FindStringSubmatch(pythonTag)
	if match == nil {
		return false
	}
	digits := match[2]
	if match[1] == "py" {
		if abiTag != "none" {
			return false
		}
		if digits == strconv.Itoa(runtimeMajor) {
			return true
		}
		if len(digits) < 2 || digits[0] != '3' || runtimeMajor != 3 {
			return false
		}
		tagMinor, err := strconv.Atoi(digits[1:])
		return err == nil && tagMinor <= runtimeMinor
	}
	if len(digits) < 2 || digits[0] != '3' || runtimeMajor != 3 {
		return false
	}
	tagMinor, err := strconv.Atoi(digits[1:])
	if err != nil {
		return false
	}
	if abiTag == "abi3" {
		return tagMinor <= runtimeMinor
	}
	return tagMinor == runtimeMinor && (abiTag == pythonTag || abiTag == "none")
}
