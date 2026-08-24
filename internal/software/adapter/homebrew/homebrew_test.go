package homebrew_test

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/adapter/homebrew"
	"github.com/temper-sh/temper/internal/software/catalog"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
)

type response struct {
	output homebrew.Output
	err    error
}

type recordingRunner struct {
	responses       []response
	calls           []homebrew.Command
	deadlines       []bool
	remainingBudget []time.Duration
}

func (r *recordingRunner) Run(ctx context.Context, command homebrew.Command) (homebrew.Output, error) {
	r.calls = append(r.calls, command)
	deadline, ok := ctx.Deadline()
	r.deadlines = append(r.deadlines, ok)
	if ok {
		r.remainingBudget = append(r.remainingBudget, time.Until(deadline))
	}
	index := len(r.calls) - 1
	if index >= len(r.responses) {
		return homebrew.Output{}, errors.New("unexpected command")
	}
	return r.responses[index].output, r.responses[index].err
}

func TestCandidatesTranslateExactBottleClosure(t *testing.T) {
	runner := &recordingRunner{responses: []response{
		{output: homebrew.Output{Stdout: []byte("homebrew/core/openssl@3\nhomebrew/core/libfoo\n")}},
		{output: homebrew.Output{Stdout: []byte(formulaJSON)}},
	}}
	resolver := newResolver(t, runner)

	candidates, err := resolver.Candidates(context.Background(), validRequest())
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || !candidates[0].Current {
		t.Fatalf("candidates = %#v", candidates)
	}
	candidate := candidates[0]
	if candidate.RootUnit != "homebrew:system:llama.cpp" {
		t.Fatalf("root unit = %q", candidate.RootUnit)
	}
	if len(candidate.Units) != 3 {
		t.Fatalf("units = %d, want 3", len(candidate.Units))
	}
	root := candidate.Units[candidate.RootUnit]
	if root.NativeName != "llama.cpp" || root.Version != "1.2.3" || root.Revision != "formula:1+scheme:0+bottle:2" {
		t.Fatalf("root = %#v", root)
	}
	if len(root.Artifacts) != 1 || root.Artifacts[0].SHA256 != strings.Repeat("a", 64) || !strings.Contains(root.Artifacts[0].Locator, "sequoia") {
		t.Fatalf("root artifacts = %#v", root.Artifacts)
	}
	libfooID, libfoo := unitByName(t, candidate, "libfoo")
	opensslID, openssl := unitByName(t, candidate, "openssl@3")
	wantRootDependencies := []string{libfooID, opensslID}
	sort.Strings(wantRootDependencies)
	if !reflect.DeepEqual(root.Dependencies, wantRootDependencies) {
		t.Fatalf("root dependencies = %v", root.Dependencies)
	}
	if !reflect.DeepEqual(libfoo.Dependencies, []string{opensslID}) {
		t.Fatalf("libfoo dependencies = %v", libfoo.Dependencies)
	}
	if len(openssl.Dependencies) != 0 || !strings.Contains(libfoo.Artifacts[0].Locator, "/all/") {
		t.Fatalf("translated dependencies = %#v / %#v", libfoo, openssl)
	}
	assertLockValid(t, candidate)

	wantCalls := []homebrew.Command{
		{Executable: "/opt/homebrew/bin/brew", Args: []string{
			"deps", "--formula", "--full-name", "--topological", "--os=macos", "--arch=arm64", "homebrew/core/llama.cpp",
		}},
		{Executable: "/opt/homebrew/bin/brew", Args: []string{
			"info", "--json=v1", "--variations", "--formula", "homebrew/core/llama.cpp", "homebrew/core/libfoo", "homebrew/core/openssl@3",
		}},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("commands = %#v\nwant %#v", runner.calls, wantCalls)
	}
	if !reflect.DeepEqual(runner.deadlines, []bool{true, true}) {
		t.Fatalf("deadlines = %v", runner.deadlines)
	}
	if runner.remainingBudget[1] > runner.remainingBudget[0] {
		t.Fatalf("provider read budget reset between commands: %v", runner.remainingBudget)
	}
}

func TestCandidatesRefuseBeforeCommandsWhenTargetCannotSelectBottle(t *testing.T) {
	runner := &recordingRunner{}
	resolver := newResolver(t, runner)
	request := validRequest()
	request.Target.DistributionVersion = "27.0"

	_, err := resolver.Candidates(context.Background(), request)
	if err == nil || !strings.Contains(err.Error(), "no reviewed bottle tag for macos 27") {
		t.Fatalf("error = %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("commands = %#v, want none", runner.calls)
	}
}

func TestCandidatesRefuseIncompleteProviderClosure(t *testing.T) {
	runner := &recordingRunner{responses: []response{
		{output: homebrew.Output{Stdout: []byte("homebrew/core/openssl@3\nhomebrew/core/libfoo\nhomebrew/core/missing\n")}},
		{output: homebrew.Output{Stdout: []byte(formulaJSON)}},
	}}
	resolver := newResolver(t, runner)

	_, err := resolver.Candidates(context.Background(), validRequest())
	if err == nil || !strings.Contains(err.Error(), `omitted formula "homebrew/core/missing"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestCandidatesRefuseBottleWithoutExactHash(t *testing.T) {
	badJSON := strings.Replace(formulaJSON, strings.Repeat("a", 64), "not-a-hash", 1)
	runner := successfulRunner(badJSON)
	resolver := newResolver(t, runner)

	_, err := resolver.Candidates(context.Background(), validRequest())
	if err == nil || !strings.Contains(err.Error(), "sha256 must be 64 lowercase") {
		t.Fatalf("error = %v", err)
	}
}

func TestCandidatesRefuseBottleForAnotherMacOSRelease(t *testing.T) {
	badJSON := strings.Replace(formulaJSON, `"arm64_sequoia"`, `"arm64_sonoma"`, 1)
	runner := successfulRunner(badJSON)
	resolver := newResolver(t, runner)

	_, err := resolver.Candidates(context.Background(), validRequest())
	if err == nil || !strings.Contains(err.Error(), `has no bottle for "arm64_sequoia"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestCandidatesRefuseUntranslatedTargetVariation(t *testing.T) {
	badJSON := strings.Replace(formulaJSON, `"variations": {"arm64_linux": {"dependencies": ["linux-only"]}}`, `"variations": {"arm64_sequoia": {"dependencies": ["target-only"]}}`, 1)
	runner := successfulRunner(badJSON)
	resolver := newResolver(t, runner)

	_, err := resolver.Candidates(context.Background(), validRequest())
	if err == nil || !strings.Contains(err.Error(), `unsupported metadata variation for "arm64_sequoia"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestCandidatesRefuseOrphanFromDependencyCommand(t *testing.T) {
	orphan := `,
  {
    "name": "orphan", "tap": "homebrew/core", "revision": 0, "version_scheme": 0,
    "versions": {"stable": "9.9.9", "bottle": true},
    "dependencies": [], "recommended_dependencies": [], "disabled": false,
    "bottle": {"stable": {"rebuild": 0, "files": {
      "arm64_sequoia": {"url": "https://bottles.invalid/orphan/sequoia/orphan.tar.gz", "sha256": "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}
    }}}
  }`
	data := strings.TrimSuffix(strings.TrimSpace(formulaJSON), "]") + orphan + "\n]"
	runner := &recordingRunner{responses: []response{
		{output: homebrew.Output{Stdout: []byte("homebrew/core/openssl@3\nhomebrew/core/libfoo\nhomebrew/core/orphan\n")}},
		{output: homebrew.Output{Stdout: []byte(data)}},
	}}
	resolver := newResolver(t, runner)

	_, err := resolver.Candidates(context.Background(), validRequest())
	if err == nil || !strings.Contains(err.Error(), "not reachable from the root") {
		t.Fatalf("error = %v", err)
	}
}

func TestCandidatesDoNotRetryCommandFailure(t *testing.T) {
	sentinel := errors.New("brew unavailable")
	runner := &recordingRunner{responses: []response{{err: sentinel}}}
	resolver := newResolver(t, runner)

	_, err := resolver.Candidates(context.Background(), validRequest())
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("commands = %d, want one", len(runner.calls))
	}
}

func TestNewRequiresInjectedExecutionFacts(t *testing.T) {
	runner := &recordingRunner{}
	tests := []struct {
		name    string
		runner  homebrew.CommandRunner
		options homebrew.Options
	}{
		{name: "runner", options: homebrew.Options{Executable: "/opt/homebrew/bin/brew", Timeout: time.Second}},
		{name: "absolute executable", runner: runner, options: homebrew.Options{Executable: "brew", Timeout: time.Second}},
		{name: "timeout", runner: runner, options: homebrew.Options{Executable: "/opt/homebrew/bin/brew"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := homebrew.New(test.runner, test.options); err == nil {
				t.Fatal("New() succeeded")
			}
		})
	}
}

func TestDescriptorIsTheCompiledHomebrewContract(t *testing.T) {
	resolver := newResolver(t, &recordingRunner{})
	descriptor := resolver.Descriptor()
	if descriptor.ID != "homebrew" || descriptor.Method != "system-package" || descriptor.EffectModel != "shared" || descriptor.Protocol != catalog.AdapterProtocolV1 {
		t.Fatalf("descriptor = %#v", descriptor)
	}
	if !descriptor.Supports(software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6"}) {
		t.Fatal("descriptor does not support darwin/arm64")
	}
}

func newResolver(t *testing.T, runner homebrew.CommandRunner) *homebrew.Resolver {
	t.Helper()
	resolver, err := homebrew.New(runner, homebrew.Options{Executable: "/opt/homebrew/bin/brew", Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

func validRequest() adapter.ResolveRequest {
	return adapter.ResolveRequest{
		Package: "llama-cpp",
		Target: software.Target{
			OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6",
		},
		Recipe: catalog.Recipe{
			Method: "system-package",
			Source: catalog.Source{Kind: "homebrew-formula", Tap: "homebrew/core", Formula: "llama.cpp"},
		},
	}
}

func successfulRunner(data string) *recordingRunner {
	return &recordingRunner{responses: []response{
		{output: homebrew.Output{Stdout: []byte("homebrew/core/openssl@3\nhomebrew/core/libfoo\n")}},
		{output: homebrew.Output{Stdout: []byte(data)}},
	}}
}

func unitByName(t *testing.T, candidate software.Candidate, name string) (string, software.ResolvedUnit) {
	t.Helper()
	for id, unit := range candidate.Units {
		if unit.NativeName == name {
			return id, unit
		}
	}
	t.Fatalf("unit %q not found", name)
	return "", software.ResolvedUnit{}
}

func assertLockValid(t *testing.T, candidate software.Candidate) {
	t.Helper()
	units := make(map[string]softwarelock.Unit, len(candidate.Units))
	for id, unit := range candidate.Units {
		units[id] = softwarelock.Unit{
			Adapter: "homebrew", Scope: unit.Scope, NativeName: unit.NativeName,
			Version: unit.Version, Revision: unit.Revision,
			Dependencies: unit.Dependencies, Artifacts: unit.Artifacts,
		}
	}
	document := softwarelock.Document{
		Schema: softwarelock.SchemaV1,
		Provenance: softwarelock.Provenance{Catalog: &softwarelock.CatalogIdentity{
			Schema: catalog.SchemaV1, Sequence: 1, SHA256: strings.Repeat("f", 64),
		}},
		Target:   validRequest().Target,
		Resolved: "2026-08-20",
		Selections: map[string]softwarelock.Selection{
			"llama-cpp": {
				Provenance: softwarelock.ProvenanceCatalog,
				Method:     "system-package", Adapter: "homebrew", RecipeRevision: "llama-cpp/v1", RootUnit: candidate.RootUnit,
			},
		},
		Units: units,
	}
	if err := document.Validate(); err != nil {
		t.Fatalf("translated candidate does not satisfy lock invariants: %v", err)
	}
}

const formulaJSON = `
[
  {
    "name": "llama.cpp", "tap": "homebrew/core", "revision": 1, "version_scheme": 0,
    "versions": {"stable": "1.2.3", "bottle": true},
    "dependencies": ["libfoo"], "recommended_dependencies": ["openssl@3"], "disabled": false,
    "bottle": {"stable": {"rebuild": 2, "files": {
      "arm64_sequoia": {"url": "https://bottles.invalid/llama.cpp/sequoia/llama.cpp.tar.gz", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
    }}},
    "variations": {"arm64_linux": {"dependencies": ["linux-only"]}},
    "future_additive_field": {"accepted": true}
  },
  {
    "name": "libfoo", "tap": "homebrew/core", "revision": 0, "version_scheme": 0,
    "versions": {"stable": "4.5.6", "bottle": true},
    "dependencies": ["openssl@3"], "recommended_dependencies": [], "disabled": false,
    "bottle": {"stable": {"rebuild": 0, "files": {
      "all": {"url": "https://bottles.invalid/libfoo/all/libfoo.tar.gz", "sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
    }}}
  },
  {
    "name": "openssl@3", "tap": "homebrew/core", "revision": 0, "version_scheme": 0,
    "versions": {"stable": "3.4.0", "bottle": true},
    "dependencies": [], "recommended_dependencies": [], "disabled": false,
    "bottle": {"stable": {"rebuild": 1, "files": {
      "arm64_sequoia": {"url": "https://bottles.invalid/openssl@3/sequoia/openssl@3.tar.gz", "sha256": "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}
    }}}
  }
]
`
