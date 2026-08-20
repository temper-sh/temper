// Package version owns the closed family of version schemes understood by
// catalog policy. It deliberately supports a small constraint language:
// comma-separated ==, !=, <, <=, >, and >= comparisons, plus PEP 440 ~=.
//
// Maintained libraries own each scheme's parsing and comparison rules. Their
// types stay behind this package so catalog policy does not depend on a
// particular implementation.
package version

import (
	"fmt"
	"strings"
)

type scheme interface {
	validate(string) error
	compare(string, string) (int, error)
	isPrerelease(string) (bool, error)
	satisfies(string, string) (bool, error)
	constraintAllowsPrerelease(string) (bool, error)
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

func IsPrerelease(name, value string) (bool, error) {
	implementation, ok := schemes[name]
	if !ok {
		return false, fmt.Errorf("version scheme %q has no prerelease semantics", name)
	}
	return implementation.isPrerelease(value)
}

func Satisfies(name, value, constraint string) (bool, error) {
	implementation, ok := schemes[name]
	if !ok {
		return false, fmt.Errorf("version scheme %q has no constraints", name)
	}
	return implementation.satisfies(value, constraint)
}

// ConstraintAllowsPrerelease reports whether policy explicitly names a
// prerelease boundary. Selection otherwise filters prereleases from moving
// latest/range policies.
func ConstraintAllowsPrerelease(name, constraint string) (bool, error) {
	implementation, ok := schemes[name]
	if !ok {
		return false, fmt.Errorf("version scheme %q has no constraints", name)
	}
	return implementation.constraintAllowsPrerelease(constraint)
}

type constraintTerm struct {
	operator string
	bound    string
}

func parseConstraint(
	constraint string,
	allowCompatible bool,
	validateBound func(string) error,
) ([]constraintTerm, error) {
	if strings.TrimSpace(constraint) == "" {
		return nil, constraintError(constraint, "must not be empty")
	}

	parts := strings.Split(constraint, ",")
	terms := make([]constraintTerm, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		operator := ""
		for _, candidate := range []string{"~=", "==", "!=", ">=", "<=", ">", "<"} {
			if strings.HasPrefix(part, candidate) {
				operator = candidate
				break
			}
		}
		if operator == "" {
			return nil, constraintError(
				constraint,
				fmt.Sprintf("term %q needs an explicit comparison operator", part),
			)
		}
		if operator == "~=" && !allowCompatible {
			return nil, constraintError(constraint, "~= is supported only for pep440")
		}

		bound := strings.TrimSpace(part[len(operator):])
		if bound == "" {
			return nil, constraintError(
				constraint,
				fmt.Sprintf("term %q has no version", part),
			)
		}
		if err := validateBound(bound); err != nil {
			return nil, constraintError(constraint, err.Error())
		}
		terms = append(terms, constraintTerm{operator: operator, bound: bound})
	}
	return terms, nil
}

func constraintError(constraint, problem string) error {
	return fmt.Errorf("constraint %q invalid: %s", constraint, problem)
}
