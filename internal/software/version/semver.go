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

func (semverScheme) isPrerelease(value string) (bool, error) {
	parsed, err := parseSemver(value)
	if err != nil {
		return false, err
	}
	return parsed.Prerelease() != "", nil
}

func (implementation semverScheme) satisfies(value, constraint string) (bool, error) {
	candidate, err := parseSemver(value)
	if err != nil {
		return false, err
	}
	terms, err := parseConstraint(constraint, false, implementation.validate)
	if err != nil {
		return false, err
	}

	for _, term := range terms {
		bound, parseErr := parseSemver(term.bound)
		if parseErr != nil {
			return false, parseErr
		}
		if !comparisonMatches(candidate.Compare(bound), term.operator) {
			return false, nil
		}
	}
	return true, nil
}

func (implementation semverScheme) constraintAllowsPrerelease(constraint string) (bool, error) {
	terms, err := parseConstraint(constraint, false, implementation.validate)
	if err != nil {
		return false, err
	}
	for _, term := range terms {
		bound, parseErr := parseSemver(term.bound)
		if parseErr != nil {
			return false, parseErr
		}
		if bound.Prerelease() != "" {
			return true, nil
		}
	}
	return false, nil
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

func comparisonMatches(order int, operator string) bool {
	switch operator {
	case "==":
		return order == 0
	case "!=":
		return order != 0
	case "<":
		return order < 0
	case "<=":
		return order <= 0
	case ">":
		return order > 0
	case ">=":
		return order >= 0
	default:
		return false
	}
}
