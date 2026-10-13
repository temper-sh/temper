// Package version owns the closed family of version schemes understood by
// software validation and ordering.
//
// Maintained libraries own each scheme's parsing and comparison rules. Their
// types stay behind this package so catalog policy does not depend on a
// particular implementation.
package version

import (
	"fmt"
)

type scheme interface {
	validate(string) error
	compare(string, string) (int, error)
}

var schemes = map[string]scheme{
	"semver": semverScheme{},
	"pep440": pep440Scheme{},
}

func Validate(name, value string) error {
	implementation, ok := schemes[name]
	if !ok {
		return fmt.Errorf("version scheme %q has no ordered parser", name)
	}
	return implementation.validate(value)
}

func Compare(name, left, right string) (int, error) {
	implementation, ok := schemes[name]
	if !ok {
		return 0, fmt.Errorf("version scheme %q has no ordering", name)
	}
	return implementation.compare(left, right)
}
