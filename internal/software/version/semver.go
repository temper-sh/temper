package version

import (
	"fmt"

	semver "github.com/Masterminds/semver/v3"
)

type semverScheme struct{}

func (semverScheme) validate(value string) error {
	_, err := parseSemver(value)
	return err
}

func (semverScheme) compare(left, right string) (int, error) {
	leftVersion, err := parseSemver(left)
	if err != nil {
		return 0, err
	}
	rightVersion, err := parseSemver(right)
	if err != nil {
		return 0, err
	}
	return leftVersion.Compare(rightVersion), nil
}

func parseSemver(value string) (*semver.Version, error) {
	parsed, err := semver.StrictNewVersion(value)
	if err != nil {
		return nil, fmt.Errorf(
			"semver %q must be strict MAJOR.MINOR.PATCH: %w",
			value,
			err,
		)
	}
	return parsed, nil
}
