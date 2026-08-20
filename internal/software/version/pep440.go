package version

import (
	"fmt"

	pep440 "github.com/aquasecurity/go-pep440-version"
)

type pep440Scheme struct{}

func (pep440Scheme) validate(value string) error {
	_, err := parsePEP440(value)
	return err
}

func (pep440Scheme) compare(left, right string) (int, error) {
	leftVersion, err := parsePEP440(left)
	if err != nil {
		return 0, err
	}
	rightVersion, err := parsePEP440(right)
	if err != nil {
		return 0, err
	}
	return leftVersion.Compare(rightVersion), nil
}

func (pep440Scheme) isPrerelease(value string) (bool, error) {
	parsed, err := parsePEP440(value)
	if err != nil {
		return false, err
	}
	return parsed.IsPreRelease(), nil
}

func (implementation pep440Scheme) satisfies(value, constraint string) (bool, error) {
	candidate, err := parsePEP440(value)
	if err != nil {
		return false, err
	}
	_, specifiers, err := implementation.specifiers(constraint)
	if err != nil {
		return false, err
	}
	return specifiers.Check(candidate), nil
}

func (implementation pep440Scheme) constraintAllowsPrerelease(constraint string) (bool, error) {
	terms, _, err := implementation.specifiers(constraint)
	if err != nil {
		return false, err
	}
	for _, term := range terms {
		bound, parseErr := parsePEP440(term.bound)
		if parseErr != nil {
			return false, parseErr
		}
		if bound.IsPreRelease() {
			return true, nil
		}
	}
	return false, nil
}

func (implementation pep440Scheme) specifiers(
	constraint string,
) ([]constraintTerm, pep440.Specifiers, error) {
	terms, err := parseConstraint(constraint, true, implementation.validate)
	if err != nil {
		return nil, pep440.Specifiers{}, err
	}
	specifiers, err := pep440.NewSpecifiers(constraint)
	if err != nil {
		return nil, pep440.Specifiers{}, constraintError(constraint, err.Error())
	}
	return terms, specifiers, nil
}

func parsePEP440(value string) (pep440.Version, error) {
	parsed, err := pep440.Parse(value)
	if err != nil {
		return pep440.Version{}, fmt.Errorf("pep440 version %q is invalid: %w", value, err)
	}
	return parsed, nil
}
