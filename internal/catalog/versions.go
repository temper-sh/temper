package catalog

import (
	"errors"
	"fmt"
	"strings"

	"github.com/temper-sh/temper/internal/buildinfo"
	"github.com/temper-sh/temper/internal/software/adapter/upstreamrelease"
	softwareversion "github.com/temper-sh/temper/internal/software/version"
)

// VersionExclusion excludes the releases of one range: one or two comparisons
// joined by a comma (AND) with <, <=, >, >= or =. Only a lower bound means
// "broken and not fixed yet"; only an upper bound is a floor.
type VersionExclusion struct {
	Range  string `yaml:"range" json:"range"`
	Reason string `yaml:"reason,omitempty" json:"reason,omitempty"`
}

type comparison struct{ op, version string }

func parseRange(value string) ([]comparison, error) {
	parts := strings.Split(value, ",")
	if len(parts) > 2 {
		return nil, fmt.Errorf("version range %q has more than two comparisons", value)
	}
	var result []comparison
	lower, upper := 0, 0
	for _, part := range parts {
		part = strings.TrimSpace(part)
		var c comparison
		for _, op := range []string{"<=", ">=", "<", ">", "="} {
			if strings.HasPrefix(part, op) {
				c = comparison{op: op, version: strings.TrimSpace(strings.TrimPrefix(part, op))}
				break
			}
		}
		if c.op == "" || c.version == "" {
			return nil, fmt.Errorf("version range %q needs <, <=, >, >= or = and a version", value)
		}
		if _, err := upstreamrelease.CompareVersions(c.version, c.version); err != nil {
			return nil, err
		}
		switch c.op {
		case ">", ">=":
			lower++
		case "<", "<=":
			upper++
		case "=":
			if len(parts) != 1 {
				return nil, fmt.Errorf("version range %q: = stands alone", value)
			}
		}
		result = append(result, c)
	}
	if lower > 1 || upper > 1 {
		return nil, fmt.Errorf("version range %q needs at most one lower and one upper bound", value)
	}
	if len(result) == 2 {
		lo, hi := result[0], result[1]
		if lo.op == "<" || lo.op == "<=" {
			lo, hi = hi, lo
		}
		order, err := upstreamrelease.CompareVersions(lo.version, hi.version)
		if err != nil {
			return nil, err
		}
		if order > 0 || order == 0 && (lo.op == ">" || hi.op == "<") {
			return nil, fmt.Errorf("version range %q is empty", value)
		}
	}
	return result, nil
}

func (e VersionExclusion) validate() error {
	if _, err := parseRange(e.Range); err != nil {
		return err
	}
	if !plainText(e.Reason) {
		return errors.New("version exclusion reason must be plain text")
	}
	return nil
}

func (e VersionExclusion) excludes(version string) (bool, error) {
	comparisons, err := parseRange(e.Range)
	if err != nil {
		return false, err
	}
	for _, c := range comparisons {
		order, err := upstreamrelease.CompareVersions(version, c.version)
		if err != nil {
			return false, err
		}
		match := map[string]bool{"<": order < 0, "<=": order <= 0, ">": order > 0, ">=": order >= 0, "=": order == 0}[c.op]
		if !match {
			return false, nil
		}
	}
	return true, nil
}

func validateExclusions(rules []VersionExclusion) error {
	for _, rule := range rules {
		if err := rule.validate(); err != nil {
			return err
		}
	}
	return nil
}

// excluded returns the first rule of the union that excludes version.
func excluded(version string, rules ...[]VersionExclusion) (*VersionExclusion, error) {
	for _, list := range rules {
		for _, rule := range list {
			hit, err := rule.excludes(version)
			if err != nil {
				return nil, err
			}
			if hit {
				return &rule, nil
			}
		}
	}
	return nil, nil
}

// allowedRelease refuses a recorded release that the union of rules excludes.
func allowedRelease(release *upstreamrelease.Release, rules ...[]VersionExclusion) error {
	if release == nil {
		return nil
	}
	rule, err := excluded(release.Version, rules...)
	if err != nil {
		return err
	}
	if rule != nil {
		return exclusionError(release.Version, *rule)
	}
	return nil
}

func exclusionError(version string, rule VersionExclusion) error {
	if rule.Reason == "" {
		return fmt.Errorf("release %s is excluded (%s)", version, rule.Range)
	}
	return fmt.Errorf("release %s is excluded (%s): %s", version, rule.Range, rule.Reason)
}

func validateMinimumTemper(value string) error {
	if value == "" {
		return nil
	}
	if _, err := softwareversion.Compare("semver", value, value); err != nil {
		return fmt.Errorf("minimum_temper: %w", err)
	}
	return nil
}

// needsTemper reports the unmet minimum, or "" when client satisfies it.
// Development builds satisfy every minimum.
func needsTemper(minimum, client string) (string, error) {
	if minimum == "" || client == buildinfo.DevVersion {
		return "", nil
	}
	order, err := softwareversion.Compare("semver", strings.TrimPrefix(client, "v"), minimum)
	if err != nil {
		return "", err
	}
	if order < 0 {
		return fmt.Sprintf("needs Temper %s or newer (this is %s)", minimum, client), nil
	}
	return "", nil
}
