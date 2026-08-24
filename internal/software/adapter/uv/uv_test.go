package uv_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/adapter/uv"
	"github.com/temper-sh/temper/internal/software/catalog"
	"github.com/temper-sh/temper/internal/software/selection"
)

type commandResponse struct {
	output uv.Output
	err    error
}

type recordingRunner struct {
	responses []commandResponse
	calls     []uv.Command
	deadlines []time.Time
}

func (r *recordingRunner) Run(ctx context.Context, command uv.Command) (uv.Output, error) {
	r.calls = append(r.calls, command)
	deadline, ok := ctx.Deadline()
	if !ok {
		return uv.Output{}, errors.New("uv command has no deadline")
	}
	r.deadlines = append(r.deadlines, deadline)
	index := len(r.calls) - 1
	if index >= len(r.responses) {
		return uv.Output{}, errors.New("unexpected uv command")
	}
	return r.responses[index].output, r.responses[index].err
}

type recordingMetadata struct {
	data      []byte
	err       error
	locators  []string
	limits    []int64
	deadlines []time.Time
}

func (r *recordingMetadata) Read(ctx context.Context, locator string, limit int64) ([]byte, error) {
	r.locators = append(r.locators, locator)
	r.limits = append(r.limits, limit)
	deadline, ok := ctx.Deadline()
	if !ok {
		return nil, errors.New("uv metadata read has no deadline")
	}
	r.deadlines = append(r.deadlines, deadline)
	return r.data, r.err
}

func TestCandidatesTranslateVersionMatchedRuntimeAndFlattenedWheelClosure(t *testing.T) {
	runner := successfulRunner(pylockFixture)
	metadata := &recordingMetadata{data: []byte(pythonMetadataFixture)}
	resolver := newResolver(t, runner, metadata)
	request := validRequest()

	candidates, err := resolver.Candidates(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Current {
		t.Fatalf("candidates = %#v", candidates)
	}
	candidate := candidates[0]
	if candidate.RootUnit != "uv:rapid-mlx:rapid-mlx" || len(candidate.Units) != 4 {
		t.Fatalf("candidate root/units = %q/%d", candidate.RootUnit, len(candidate.Units))
	}
	runtime := candidate.Units["uv:rapid-mlx:cpython"]
	if runtime.Version != "3.12.14" || runtime.Revision != "python-build-standalone:20260814" || len(runtime.Artifacts) != 1 || runtime.Artifacts[0].SHA256 != strings.Repeat("a", 64) {
		t.Fatalf("runtime = %#v", runtime)
	}
	root := candidate.Units[candidate.RootUnit]
	wantRootDependencies := []string{
		"uv:rapid-mlx:cpython",
		"uv:rapid-mlx:mlx",
		"uv:rapid-mlx:typing-extensions",
	}
	if !reflect.DeepEqual(root.Dependencies, wantRootDependencies) {
		t.Fatalf("root dependencies = %v, want %v", root.Dependencies, wantRootDependencies)
	}
	mlx := candidate.Units["uv:rapid-mlx:mlx"]
	if mlx.Version != "1.2.3" || !reflect.DeepEqual(mlx.Dependencies, []string{"uv:rapid-mlx:cpython"}) || len(mlx.Artifacts) != 1 || mlx.Artifacts[0].Size != 222 {
		t.Fatalf("mlx unit = %#v", mlx)
	}
	if got := candidate.Units["uv:rapid-mlx:typing-extensions"].Dependencies; !reflect.DeepEqual(got, []string{"uv:rapid-mlx:cpython"}) {
		t.Fatalf("transitive dependencies = %v", got)
	}

	wantCalls := []uv.Command{
		{Executable: "/opt/homebrew/bin/uv", Args: []string{"--version"}},
		{
			Executable: "/opt/homebrew/bin/uv",
			Args: []string{
				"pip", "compile", "-", "--format", "pylock.toml",
				"--python-version", "3.12.14", "--python-platform", "aarch64-apple-darwin",
				"--default-index", "https://pypi.org/simple", "--index-strategy", "first-index",
				"--resolution", "highest", "--prerelease", "disallow", "--only-binary", ":all:",
				"--no-build", "--no-config", "--no-cache", "--no-python-downloads", "--no-header",
			},
			Stdin: []byte("mlx>=1,<2,>=1.2,<1.3\nrapid-mlx>=0.1,<0.2\n"),
		},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("commands = %#v\nwant %#v", runner.calls, wantCalls)
	}
	if len(metadata.locators) != 1 || metadata.locators[0] != "https://raw.githubusercontent.com/astral-sh/uv/0.12.5/crates/uv-python/download-metadata.json" || !reflect.DeepEqual(metadata.limits, []int64{uv.MaxPythonMetadataBytes}) {
		t.Fatalf("metadata reads = %v limits %v", metadata.locators, metadata.limits)
	}
	if len(runner.deadlines) != 2 || len(metadata.deadlines) != 1 || !runner.deadlines[0].Equal(metadata.deadlines[0]) || !runner.deadlines[0].Equal(runner.deadlines[1]) {
		t.Fatalf("provider reads do not share one deadline: commands=%v metadata=%v", runner.deadlines, metadata.deadlines)
	}

	family, err := adapter.NewResolverFamily(resolver)
	if err != nil {
		t.Fatal(err)
	}
	if _, descriptor, err := family.For(request.Supply, "python-environment", request.Target); err != nil || descriptor.ID != "uv" {
		t.Fatalf("resolver family = %#v, %v", descriptor, err)
	}
	locked, err := selection.Resolve(
		catalog.Snapshot{Document: request.Supply, SHA256: strings.Repeat("f", 64)},
		request.Target,
		time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC),
		nil,
		[]selection.Request{{Package: request.Package, Method: "python-environment", Candidates: candidates}},
	)
	if err != nil {
		t.Fatalf("translated candidate does not satisfy selection and lock invariants: %v", err)
	}
	if locked.Units["uv:rapid-mlx:cpython"].Artifacts[0].Locator == "" {
		t.Fatal("selected lock omitted managed Python artifact")
	}
}

func TestCandidatesAllowPrereleaseOnlyForCatalogPackagesThatRequestIt(t *testing.T) {
	request := validRequest()
	mlx := request.Supply.Packages["mlx"]
	recipe := mlx.Recipes["uv"]
	recipe.Selection = catalog.Selection{Policy: "range", Constraint: ">=1.3rc1,<2"}
	mlx.Recipes["uv"] = recipe
	request.Supply.Packages["mlx"] = mlx
	request.Recipe = request.Supply.Packages[request.Package].Recipes["uv"]
	prereleasePylock := strings.Replace(pylockFixture, `version = "1.2.3"`, `version = "1.3rc2"`, 1)
	prereleasePylock = strings.Replace(prereleasePylock, "mlx-1.2.3-", "mlx-1.3rc2-", 1)
	runner := successfulRunner(prereleasePylock)
	resolver := newResolver(t, runner, &recordingMetadata{data: []byte(pythonMetadataFixture)})

	if _, err := resolver.Candidates(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	lastArgs := runner.calls[1].Args
	wantSuffix := []string{"--prerelease-package", "mlx=allow"}
	if !reflect.DeepEqual(lastArgs[len(lastArgs)-len(wantSuffix):], wantSuffix) {
		t.Fatalf("command suffix = %v, want %v", lastArgs, wantSuffix)
	}
}

func TestCandidatesRefuseProtocolDriftAndIncompleteExactFacts(t *testing.T) {
	tests := []struct {
		name      string
		uvVersion string
		metadata  string
		pylock    string
		want      string
		wantReads int
		wantCalls int
	}{
		{name: "unsupported uv", uvVersion: "uv 0.13.0\n", metadata: pythonMetadataFixture, pylock: pylockFixture, want: "supported stable 0.12.x", wantCalls: 1},
		{name: "runtime hash", uvVersion: "uv 0.12.5\n", metadata: strings.Replace(pythonMetadataFixture, strings.Repeat("a", 64), "bad-hash", 1), pylock: pylockFixture, want: "SHA-256 is invalid", wantReads: 1, wantCalls: 1},
		{name: "marker", uvVersion: "uv 0.12.5\n", metadata: pythonMetadataFixture, pylock: strings.Replace(pylockFixture, `version = "1.2.3"`, "version = \"1.2.3\"\nmarker = \"sys_platform == 'darwin'\"", 1), want: "unevaluated environment marker", wantReads: 1, wantCalls: 2},
		{name: "index", uvVersion: "uv 0.12.5\n", metadata: pythonMetadataFixture, pylock: strings.Replace(pylockFixture, "https://pypi.org/simple", "https://private.example/simple", 1), want: "is not PyPI", wantReads: 1, wantCalls: 2},
		{name: "wheel hash", uvVersion: "uv 0.12.5\n", metadata: pythonMetadataFixture, pylock: strings.Replace(pylockFixture, strings.Repeat("b", 64), "short", 1), want: "exactly one lowercase SHA-256", wantReads: 1, wantCalls: 2},
		{name: "wheel target", uvVersion: "uv 0.12.5\n", metadata: pythonMetadataFixture, pylock: strings.Replace(pylockFixture, "macosx_15_0_arm64", "manylinux_2_28_aarch64", 1), want: "is not compatible with CPython 3.12.14 on darwin/arm64", wantReads: 1, wantCalls: 2},
		{name: "missing catalog dependency", uvVersion: "uv 0.12.5\n", metadata: pythonMetadataFixture, pylock: pylockWithoutMLX, want: `omits catalog package "mlx"`, wantReads: 1, wantCalls: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &recordingRunner{responses: []commandResponse{
				{output: uv.Output{Stdout: []byte(test.uvVersion)}},
				{output: uv.Output{Stdout: []byte(test.pylock)}},
			}}
			metadata := &recordingMetadata{data: []byte(test.metadata)}
			resolver := newResolver(t, runner, metadata)
			_, err := resolver.Candidates(context.Background(), validRequest())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
			if len(runner.calls) != test.wantCalls || len(metadata.locators) != test.wantReads {
				t.Fatalf("calls/reads = %d/%d, want %d/%d", len(runner.calls), len(metadata.locators), test.wantCalls, test.wantReads)
			}
		})
	}
}

func TestCandidatesDoNotRetryReaderOrCommandFailures(t *testing.T) {
	sentinel := errors.New("provider unavailable")
	tests := []struct {
		name     string
		runner   *recordingRunner
		metadata *recordingMetadata
		calls    int
		reads    int
	}{
		{
			name:     "version command",
			runner:   &recordingRunner{responses: []commandResponse{{err: sentinel}}},
			metadata: &recordingMetadata{data: []byte(pythonMetadataFixture)}, calls: 1,
		},
		{
			name:     "metadata",
			runner:   &recordingRunner{responses: []commandResponse{{output: uv.Output{Stdout: []byte("uv 0.12.5\n")}}}},
			metadata: &recordingMetadata{err: sentinel}, calls: 1, reads: 1,
		},
		{
			name: "compile command",
			runner: &recordingRunner{responses: []commandResponse{
				{output: uv.Output{Stdout: []byte("uv 0.12.5\n")}}, {err: sentinel},
			}},
			metadata: &recordingMetadata{data: []byte(pythonMetadataFixture)}, calls: 2, reads: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := newResolver(t, test.runner, test.metadata)
			_, err := resolver.Candidates(context.Background(), validRequest())
			if !errors.Is(err, sentinel) {
				t.Fatalf("error = %v", err)
			}
			if len(test.runner.calls) != test.calls || len(test.metadata.locators) != test.reads {
				t.Fatalf("calls/reads = %d/%d, want %d/%d", len(test.runner.calls), len(test.metadata.locators), test.calls, test.reads)
			}
		})
	}
}

func TestCandidatesRefuseInvalidRequestBeforeProviderReads(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*adapter.ResolveRequest)
		want   string
	}{
		{name: "target", mutate: func(r *adapter.ResolveRequest) { r.Target.OS = "linux" }, want: "does not support target"},
		{name: "recipe drift", mutate: func(r *adapter.ResolveRequest) { r.Recipe.RecipeRevision = "other/v1" }, want: "differs from the supplied catalog"},
		{name: "non PyPI", mutate: func(r *adapter.ResolveRequest) {
			pkg := r.Supply.Packages["rapid-mlx"]
			recipe := pkg.Recipes["uv"]
			recipe.Source.Index = "private"
			pkg.Recipes["uv"] = recipe
			r.Supply.Packages["rapid-mlx"] = pkg
			r.Recipe = recipe
		}, want: "index \"private\" is not supported"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &recordingRunner{}
			metadata := &recordingMetadata{}
			resolver := newResolver(t, runner, metadata)
			request := validRequest()
			test.mutate(&request)
			_, err := resolver.Candidates(context.Background(), request)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
			if len(runner.calls) != 0 || len(metadata.locators) != 0 {
				t.Fatalf("invalid request performed provider reads: %d/%d", len(runner.calls), len(metadata.locators))
			}
		})
	}
}

func TestNewAndDescriptorFreezeExecutionContract(t *testing.T) {
	runner := &recordingRunner{}
	metadata := &recordingMetadata{}
	tests := []struct {
		name     string
		runner   uv.CommandRunner
		metadata uv.MetadataReader
		options  uv.Options
	}{
		{name: "runner", metadata: metadata, options: uv.Options{Executable: "/opt/homebrew/bin/uv", Timeout: time.Second}},
		{name: "metadata", runner: runner, options: uv.Options{Executable: "/opt/homebrew/bin/uv", Timeout: time.Second}},
		{name: "absolute executable", runner: runner, metadata: metadata, options: uv.Options{Executable: "uv", Timeout: time.Second}},
		{name: "timeout", runner: runner, metadata: metadata, options: uv.Options{Executable: "/opt/homebrew/bin/uv"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := uv.New(test.runner, test.metadata, test.options); err == nil {
				t.Fatal("New() succeeded")
			}
		})
	}
	descriptor := uv.Descriptor()
	if descriptor.ID != "uv" || descriptor.Method != "python-environment" || descriptor.Protocol != catalog.AdapterProtocolV1 || descriptor.EffectModel != "isolated" || !descriptor.Supports(validRequest().Target) {
		t.Fatalf("descriptor = %#v", descriptor)
	}
}

func newResolver(t *testing.T, runner uv.CommandRunner, metadata uv.MetadataReader) *uv.Resolver {
	t.Helper()
	resolver, err := uv.New(runner, metadata, uv.Options{Executable: "/opt/homebrew/bin/uv", Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

func successfulRunner(pylock string) *recordingRunner {
	return &recordingRunner{responses: []commandResponse{
		{output: uv.Output{Stdout: []byte("uv 0.12.5 (Homebrew 2026-08-14 aarch64-apple-darwin)\n")}},
		{output: uv.Output{Stdout: []byte(pylock)}},
	}}
}

func validRequest() adapter.ResolveRequest {
	supply := validSupply()
	return adapter.ResolveRequest{
		Package: "rapid-mlx", Recipe: supply.Packages["rapid-mlx"].Recipes["uv"], Supply: supply,
		Target: software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6"},
	}
}

func validSupply() catalog.Document {
	python := catalog.Recipe{
		Method: "python-environment", RecipeRevision: "cpython-uv/v1",
		Source:        catalog.Source{Kind: "python-runtime", Implementation: "cpython"},
		VersionScheme: "pep440", Selection: catalog.Selection{Policy: "range", Constraint: ">=3.12,<3.13"},
	}
	mlx := catalog.Recipe{
		Method: "python-environment", RecipeRevision: "mlx-uv/v1",
		Source:        catalog.Source{Kind: "python-index", Index: "pypi", Distribution: "mlx"},
		VersionScheme: "pep440", Selection: catalog.Selection{Policy: "range", Constraint: ">=1,<2"},
		Dependencies: []catalog.Dependency{{Package: "cpython", Constraint: ">=3.12,<3.13"}},
	}
	rapid := catalog.Recipe{
		Method: "python-environment", RecipeRevision: "rapid-mlx-uv/v1",
		Source:        catalog.Source{Kind: "python-index", Index: "pypi", Distribution: "rapid-mlx"},
		VersionScheme: "pep440", Selection: catalog.Selection{Policy: "range", Constraint: ">=0.1,<0.2"},
		Dependencies: []catalog.Dependency{
			{Package: "cpython", Constraint: ">=3.12,<3.13"},
			{Package: "mlx", Constraint: ">=1.2,<1.3"},
		},
	}
	document := catalog.Document{
		Schema: "temper-software-supply/v1", Sequence: 1, PublishedAt: "2026-08-24T00:00:00Z",
		Methods:  map[string]catalog.Method{"python-environment": {Description: "isolated Python environment"}},
		Adapters: map[string]catalog.Adapter{"uv": {Method: "python-environment", Protocol: catalog.AdapterProtocolV1, EffectModel: "isolated"}},
		TargetBindings: []catalog.TargetBinding{{
			Method: "python-environment", Target: software.Target{OS: "darwin", Arch: "arm64"}, Adapter: "uv",
		}},
		Packages: map[string]catalog.Package{
			"cpython":   {Description: "runtime", Recipes: map[string]catalog.Recipe{"uv": python}},
			"mlx":       {Description: "framework", Recipes: map[string]catalog.Recipe{"uv": mlx}},
			"rapid-mlx": {Description: "server", Recipes: map[string]catalog.Recipe{"uv": rapid}},
		},
	}
	if err := document.Validate(); err != nil {
		panic(fmt.Sprintf("invalid uv test catalog: %v", err))
	}
	return document
}

const pythonMetadataFixture = `{
  "cpython-3.12.13-darwin-aarch64-none": {
    "name": "cpython", "arch": {"family": "aarch64", "variant": null}, "os": "darwin", "libc": "none",
    "major": 3, "minor": 12, "patch": 13, "prerelease": "", "variant": null, "build": "20260801",
    "url": "https://github.com/astral-sh/python-build-standalone/releases/download/20260801/cpython-3.12.13.tar.gz",
    "sha256": "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
  },
  "cpython-3.12.14-darwin-aarch64-none": {
    "name": "cpython", "arch": {"family": "aarch64", "variant": null}, "os": "darwin", "libc": "none",
    "major": 3, "minor": 12, "patch": 14, "prerelease": "", "variant": null, "build": "20260814",
    "url": "https://github.com/astral-sh/python-build-standalone/releases/download/20260814/cpython-3.12.14.tar.gz",
    "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  },
  "cpython-3.13.7-darwin-aarch64-none": {
    "name": "cpython", "arch": {"family": "aarch64", "variant": null}, "os": "darwin", "libc": "none",
    "major": 3, "minor": 13, "patch": 7, "prerelease": "", "variant": null, "build": "20260814",
    "url": "https://github.com/astral-sh/python-build-standalone/releases/download/20260814/cpython-3.13.7.tar.gz",
    "sha256": "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
  },
  "cpython-3.12.14-linux-aarch64-none": {
    "name": "cpython", "arch": {"family": "aarch64", "variant": null}, "os": "linux", "libc": "none",
    "major": 3, "minor": 12, "patch": 14, "prerelease": "", "variant": null, "build": "20260814",
    "url": "https://github.com/astral-sh/python-build-standalone/releases/download/20260814/cpython-linux.tar.gz",
    "sha256": "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
  }
}`

const pylockFixture = `lock-version = "1.0"
created-by = "uv"
requires-python = ">=3.12.14"

[[packages]]
name = "mlx"
version = "1.2.3"
index = "https://pypi.org/simple"
wheels = [{ url = "https://files.pythonhosted.org/packages/mlx-1.2.3-cp312-cp312-macosx_15_0_arm64.whl", size = 222, hashes = { sha256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" } }]

[[packages]]
name = "rapid-mlx"
version = "0.1.5"
index = "https://pypi.org/simple"
wheels = [{ url = "https://files.pythonhosted.org/packages/rapid_mlx-0.1.5-py3-none-any.whl", size = 111, hashes = { sha256 = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc" } }]

[[packages]]
name = "typing-extensions"
version = "4.15.0"
index = "https://pypi.org/simple"
wheels = [{ url = "https://files.pythonhosted.org/packages/typing_extensions-4.15.0-py3-none-any.whl", size = 333, hashes = { sha256 = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee" } }]
`

const pylockWithoutMLX = `lock-version = "1.0"
created-by = "uv"
requires-python = ">=3.12.14"

[[packages]]
name = "rapid-mlx"
version = "0.1.5"
index = "https://pypi.org/simple"
wheels = [{ url = "https://files.pythonhosted.org/packages/rapid_mlx-0.1.5-py3-none-any.whl", size = 111, hashes = { sha256 = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc" } }]
`
