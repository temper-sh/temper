package version_test

import (
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software/version"
)

func TestSemverPrecedenceAndConstraints(t *testing.T) {
	ordered := []string{
		"1.0.0-alpha",
		"1.0.0-alpha.1",
		"1.0.0-alpha.beta",
		"1.0.0-beta",
		"1.0.0-beta.2",
		"1.0.0-beta.11",
		"1.0.0-rc.1",
		"1.0.0",
		"1.10.0",
	}
	assertOrdered(t, "semver", ordered)

	for _, tt := range []struct {
		value      string
		constraint string
		want       bool
	}{
		{value: "1.4.2", constraint: ">=1.2.0, <2.0.0", want: true},
		{value: "2.0.0", constraint: ">=1.2.0, <2.0.0", want: false},
		{value: "1.4.0", constraint: "!=1.4.0", want: false},
	} {
		got, err := version.Satisfies("semver", tt.value, tt.constraint)
		if err != nil {
			t.Fatalf("Satisfies(%q, %q) error = %v", tt.value, tt.constraint, err)
		}
		if got != tt.want {
			t.Errorf("Satisfies(%q, %q) = %v, want %v", tt.value, tt.constraint, got, tt.want)
		}
	}
}

func TestPEP440OrderingNormalizationAndCompatibleRelease(t *testing.T) {
	ordered := []string{
		"1.dev0",
		"1.0.dev456",
		"1.0a1",
		"1.0a2.dev456",
		"1.0a12.dev456",
		"1.0a12",
		"1.0b1.dev456",
		"1.0b2",
		"1.0b2.post345.dev456",
		"1.0b2.post345",
		"1.0rc1.dev456",
		"1.0RC1",
		"1.0",
		"1.0+abc.5",
		"1.0+abc.7",
		"1.0+5",
		"1.0.post456.dev34",
		"1.0.post456",
		"1.0.15",
		"1.1.dev1",
	}
	assertOrdered(t, "pep440", ordered)

	equal, err := version.Compare("pep440", "v1.0", "1.0.0")
	if err != nil || equal != 0 {
		t.Fatalf("Compare(normalized PEP 440) = %d, %v, want equal", equal, err)
	}
	for _, tt := range []struct {
		value string
		want  bool
	}{
		{value: "1.4.9", want: true},
		{value: "1.5", want: false},
		{value: "2.0", want: false},
	} {
		got, err := version.Satisfies("pep440", tt.value, "~=1.4.5")
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Errorf("Satisfies(%q, ~=1.4.5) = %v, want %v", tt.value, got, tt.want)
		}
	}
	localMatch, err := version.Satisfies("pep440", "1.0+temper.1", "==1.0")
	if err != nil || !localMatch {
		t.Fatalf("public equality did not ignore candidate local label: %v, %v", localMatch, err)
	}
	localMismatch, err := version.Satisfies("pep440", "1.0+temper.1", "==1.0+temper.2")
	if err != nil || localMismatch {
		t.Fatalf("local equality mismatch = %v, %v", localMismatch, err)
	}
}

func TestPEP440ExclusiveAndInclusiveOrdering(t *testing.T) {
	for _, tt := range []struct {
		value      string
		constraint string
		want       bool
	}{
		{value: "2.0.post1", constraint: ">2", want: false},
		{value: "2.0+local", constraint: ">2", want: false},
		{value: "2.0.dev1", constraint: "<2", want: false},
		{value: "1.9.dev1", constraint: "<2", want: true},
		{value: "2.0+local", constraint: ">=2", want: true},
		{value: "2.0+local", constraint: "<=2", want: true},
		{value: "2!1.0", constraint: ">2.0", want: true},
	} {
		got, err := version.Satisfies("pep440", tt.value, tt.constraint)
		if err != nil {
			t.Fatalf("Satisfies(%q, %q) error = %v", tt.value, tt.constraint, err)
		}
		if got != tt.want {
			t.Errorf("Satisfies(%q, %q) = %v, want %v", tt.value, tt.constraint, got, tt.want)
		}
	}
}

func TestVersionRefusesInventedOrMalformedSemantics(t *testing.T) {
	for _, tt := range []struct {
		scheme     string
		value      string
		constraint string
		want       string
	}{
		{scheme: "semver", value: "v1.2.3", constraint: ">=1.0.0", want: "strict MAJOR.MINOR.PATCH"},
		{scheme: "semver", value: "1.2.3", constraint: "^1.0.0", want: "explicit comparison operator"},
		{scheme: "semver", value: "1.2.3", constraint: "~=1.2.0", want: "only for pep440"},
		{scheme: "opaque", value: "rolling", constraint: ">=old", want: "no constraints"},
		{scheme: "pep440", value: "1.2", constraint: ">=1.0+local", want: "local version"},
	} {
		_, err := version.Satisfies(tt.scheme, tt.value, tt.constraint)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("Satisfies(%q, %q, %q) error = %v, want %q", tt.scheme, tt.value, tt.constraint, err, tt.want)
		}
	}
}

func TestVersionRefusesConstraintSyntaxOutsideTemperGrammar(t *testing.T) {
	for _, tt := range []struct {
		scheme     string
		constraint string
	}{
		{scheme: "semver", constraint: "1.2.3"},
		{scheme: "semver", constraint: ">=1.0.0 || <2.0.0"},
		{scheme: "pep440", constraint: "==1.2.*"},
		{scheme: "pep440", constraint: ">=1,<2 || >=3"},
		{scheme: "pep440", constraint: "===arbitrary"},
	} {
		if _, err := version.Satisfies(tt.scheme, "1.2.3", tt.constraint); err == nil {
			t.Errorf("Satisfies(%q, %q) accepted syntax outside Temper grammar", tt.scheme, tt.constraint)
		}
	}
}

func TestConstraintPrereleaseIntent(t *testing.T) {
	stable, err := version.ConstraintAllowsPrerelease("pep440", ">=1.0,<2")
	if err != nil || stable {
		t.Fatalf("stable constraint intent = %v, %v", stable, err)
	}
	preview, err := version.ConstraintAllowsPrerelease("pep440", ">=1.0rc1,<2")
	if err != nil || !preview {
		t.Fatalf("preview constraint intent = %v, %v", preview, err)
	}
}

func assertOrdered(t *testing.T, scheme string, values []string) {
	t.Helper()
	for index := 1; index < len(values); index++ {
		got, err := version.Compare(scheme, values[index-1], values[index])
		if err != nil {
			t.Fatalf("Compare(%q, %q) error = %v", values[index-1], values[index], err)
		}
		if got >= 0 {
			t.Fatalf("Compare(%q, %q) = %d, want ascending", values[index-1], values[index], got)
		}
	}
}
