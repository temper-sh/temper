// Package software holds the provider-neutral values shared by software
// catalog, resolution, installation, and receipt slices.
package software

import (
	"fmt"
	"regexp"
)

var targetTokenPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._+-][a-z0-9]+)*$`)

// Target is either a normalized exact machine target or a catalog selector.
// Empty distribution fields mean that a selector does not constrain them.
type Target struct {
	OS                  string `yaml:"os" json:"os"`
	Arch                string `yaml:"arch" json:"arch"`
	Distribution        string `yaml:"distribution,omitempty" json:"distribution,omitempty"`
	DistributionVersion string `yaml:"distribution_version,omitempty" json:"distribution_version,omitempty"`
}

func (t Target) Validate() error {
	if t.OS == "" || !targetTokenPattern.MatchString(t.OS) {
		return fmt.Errorf("os %q must be a normalized lowercase token", t.OS)
	}
	if t.Arch == "" || !targetTokenPattern.MatchString(t.Arch) {
		return fmt.Errorf("arch %q must be a normalized lowercase token", t.Arch)
	}
	if t.Distribution != "" && !targetTokenPattern.MatchString(t.Distribution) {
		return fmt.Errorf("distribution %q must be a normalized lowercase token", t.Distribution)
	}
	if t.DistributionVersion != "" {
		if t.Distribution == "" {
			return fmt.Errorf("distribution_version requires distribution")
		}
		if !targetTokenPattern.MatchString(t.DistributionVersion) {
			return fmt.Errorf("distribution_version %q must be a normalized lowercase token", t.DistributionVersion)
		}
	}
	return nil
}

// Matches reports whether an exact target satisfies this selector.
func (t Target) Matches(exact Target) bool {
	return t.OS == exact.OS &&
		t.Arch == exact.Arch &&
		(t.Distribution == "" || t.Distribution == exact.Distribution) &&
		(t.DistributionVersion == "" || t.DistributionVersion == exact.DistributionVersion)
}

// Overlaps reports whether any normalized target could match both selectors.
func (t Target) Overlaps(other Target) bool {
	if t.OS != other.OS || t.Arch != other.Arch {
		return false
	}
	if t.Distribution != "" && other.Distribution != "" && t.Distribution != other.Distribution {
		return false
	}
	if t.DistributionVersion != "" && other.DistributionVersion != "" && t.DistributionVersion != other.DistributionVersion {
		return false
	}
	return true
}
