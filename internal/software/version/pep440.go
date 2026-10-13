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

func parsePEP440(value string) (pep440.Version, error) {
	parsed, err := pep440.Parse(value)
	if err != nil {
		return pep440.Version{}, fmt.Errorf("pep440 version %q is invalid: %w", value, err)
	}
	return parsed, nil
}
