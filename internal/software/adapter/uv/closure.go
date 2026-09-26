package uv

import (
	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/installplan"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"regexp"
)

var sourceCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

// ValidateClosure applies the installer's target, interpreter and wheel checks
// before a catalog can publish an exact Python environment.
func ValidateClosure(target software.Target, units map[string]softwarelock.Unit) error {
	_, err := validateLockedGroup(target, installplan.Installation{ID: "catalog-validation", Root: "/catalog-validation"}, units)
	return err
}
