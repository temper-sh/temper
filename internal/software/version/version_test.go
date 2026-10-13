package version_test

import (
	"testing"

	"github.com/temper-sh/temper/internal/software/version"
)

func TestSemverPrecedence(t *testing.T) {
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
}

func TestPEP440OrderingAndNormalization(t *testing.T) {
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
