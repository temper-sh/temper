package setupcmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/setupui"
	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/testfixture"
)

const (
	compactProfile = "qwen3.5-4b-local"
	utilityProfile = "qwen3.5-4b-utility"
	compactLayout  = "qwen3.5-4b-q4km-off"
	largePatch     = "froggeric-qwen38-v22.5"
	GiB            = int64(1 << 30)
	fixtureDigest  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func guidedCatalog(t *testing.T) catalog.Document {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "catalog", "guided-setup.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := testfixture.LegacySetupCatalog(raw)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func eightGiBFacts() machine.Facts {
	return machine.Facts{
		Schema:        machine.FactsSchemaV1,
		Target:        software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "15.6"},
		HardwareModel: "Mac14,2", Chip: "Apple M2", OSBuild: "24G90",
		PhysicalMemoryBytes: 8 * GiB, MetalDeviceMemoryMiB: 8192 * 81 / 100,
		MetalDeviceMemorySource: machine.MetalDeviceSourcePredicted,
		WiredLimitMiB:           6500, WiredLimitSource: budget.WiredSourceLive,
	}
}

func fixtureCommand(t *testing.T) Command {
	t.Helper()
	doc := testedCompactCatalog(t, guidedCatalog(t))
	return Command{
		Detect: func(context.Context) (machine.Facts, error) { return eightGiBFacts(), nil },
		Disk:   func(string) (int64, error) { return 40 * GiB, nil },
		Home:   func() (string, error) { return t.TempDir(), nil },
		Catalog: func(context.Context, string, string) (catalog.Document, string, error) {
			return doc, fixtureDigest, nil
		},
		Resolve: func(ctx context.Context, d catalog.Document, s catalog.Selection, choice string) (catalog.Document, error) {
			if choice != "recorded" {
				return catalog.Document{}, errors.New("unexpected moving software request")
			}
			return catalog.ResolveSoftware(ctx, d, s, choice, nil)
		},
		UI: func(context.Context, io.Reader, io.Writer, setupui.Input, func(context.Context, setupui.Choices) (setupui.Review, error)) (setupui.Decision, error) {
			return setupui.Decision{}, errors.New("unexpected UI")
		},
		Dispatch: func(context.Context, []string, io.Writer, io.Writer) int { t.Error("unexpected preparation"); return 1 },
	}
}

// Synthetic evidence for orchestration tests; never added to the real catalog.
func testedCompactCatalog(t *testing.T, doc catalog.Document) catalog.Document {
	t.Helper()
	facts := eightGiBFacts()
	l := doc.Layouts[compactLayout]
	execution, err := catalog.ContextExecutionSHA256(doc, compactLayout, "", 16384)
	if err != nil {
		t.Fatal(err)
	}
	l.ContextFindings = []catalog.ContextFinding{{WindowTokens: 16384, MaxOutputTokens: 4096, ExecutionSHA256: execution,
		Machine:                catalog.ContextMachine{Target: facts.Target, Chip: facts.Chip, PhysicalMemoryBytes: facts.PhysicalMemoryBytes, MinimumWiredLimitMiB: 4096},
		EngineMemoryLimitBytes: 4 * GiB, SwapGrowthLimitBytes: 512 << 20, Evidence: "https://example.test/synthetic-context", LatencyNote: "Synthetic slow-response observation"}}
	doc.Layouts[compactLayout] = l
	return doc
}

func runSetup(c Command, args ...string) (int, string, string) {
	var out, diagnostics bytes.Buffer
	code := c.Run(context.Background(), args, strings.NewReader(""), &out, &diagnostics)
	return code, out.String(), diagnostics.String()
}

func TestRecordedCatalogCompactLocalEightGiBDryRunIsPure(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent-root")
	c := fixtureCommand(t)
	code, output, diagnostics := runSetup(c, "--root", root, "--profile", compactProfile, "--software", "recorded", "--dry-run", "--json")
	if code != 0 {
		t.Fatalf("dry run failed: %s", diagnostics)
	}
	var got report
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatal(err)
	}
	if !got.DryRun || !got.Changed || got.Plan.Root != root || len(got.Plan.Modes) != 1 || got.Plan.Modes[0].Mode != "local" || got.Plan.Modes[0].Profile != compactProfile {
		t.Fatalf("compact plan = %+v", got)
	}
	if !got.Plan.CanPrepare {
		t.Fatalf("8 GiB compact profile refused: %v", got.Plan.Refusals)
	}
	if got.Plan.Modes[0].Selection.ContextWindows[compactLayout] != 16384 || got.Plan.Modes[0].Contexts[compactLayout].Status != "tested" {
		t.Fatal("small-machine preview did not use its applicable tested context")
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry run changed root: %v", err)
	}
}

func TestSharedHFCacheAppearsInSetupPreviewWithoutEffects(t *testing.T) {
	directory := t.TempDir()
	root, cache := filepath.Join(directory, "temper"), filepath.Join(directory, "shared-hub")
	doc := guidedCatalog(t)
	artifact := doc.Artifacts["qwen3.5-4b-q4km"]
	digest := sha256.Sum256([]byte("tiny"))
	artifact.Files[0].Bytes, artifact.Files[0].SHA256 = 4, hex.EncodeToString(digest[:])
	doc.Artifacts["qwen3.5-4b-q4km"] = artifact
	snapshot := filepath.Join(cache, "models--"+strings.ReplaceAll(artifact.Repo, "/", "--"), "snapshots", artifact.Revision, artifact.Files[0].Path)
	if err := os.MkdirAll(filepath.Dir(snapshot), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snapshot, []byte("tiny"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := fixtureCommand(t)
	c.CacheRoot = func() (string, error) { return cache, nil }
	c.Catalog = func(context.Context, string, string) (catalog.Document, string, error) {
		return testedCompactCatalog(t, doc), fixtureDigest, nil
	}
	code, output, diagnostics := runSetup(c, "--root", root, "--profile", compactProfile, "--dry-run", "--json")
	if code != 0 {
		t.Fatalf("preview: %s", diagnostics)
	}
	var got report
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatal(err)
	}
	if got.Plan.HFCache == nil || got.Plan.HFCache.Root != cache || !strings.Contains(got.Plan.WeightSummary(), "No weight download") {
		t.Fatalf("cache omitted from preview: %+v", got.Plan)
	}
	found := false
	for _, file := range got.Plan.Downloads {
		if file.Kind == "model" {
			found = true
			if !file.Cached || file.Action() != "Cached in Hugging Face" {
				t.Fatalf("wrong model status: %+v", file)
			}
		}
	}
	if !found {
		t.Fatal("preview omitted model row")
	}
	for _, path := range []string{root, filepath.Join(cache, ".locks")} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("preview wrote %s: %v", path, err)
		}
	}
}

func TestUtilityProfileHasExternalForegroundWithoutLocalMain(t *testing.T) {
	root := filepath.Join(t.TempDir(), "setup")
	c := fixtureCommand(t)
	code, output, diagnostics := runSetup(c, "--root", root, "--profile", utilityProfile, "--json")
	if code != 0 {
		t.Fatalf("utility dry run failed: %s", diagnostics)
	}
	var got report
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Plan.Modes) != 1 || got.Plan.Modes[0].Mode != "utility" || got.Plan.Modes[0].Profile != utilityProfile {
		t.Fatalf("utility plan = %+v", got.Plan)
	}
	saved, err := setup.Load(root)
	locks := saved.Locks
	if err != nil {
		t.Fatal(err)
	}
	if len(locks) != 1 || locks[0].Records.Profiles[utilityProfile].Foreground != "external" {
		t.Fatalf("saved utility foreground = %+v", locks)
	}
}

func TestUnknownAndIncompatibleChoicesRefuseBeforeEffects(t *testing.T) {
	d := testedCompactCatalog(t, guidedCatalog(t))
	d.Artifacts["unselected"] = d.Artifacts[d.Layouts[compactLayout].Artifact]
	patch := d.Patches[largePatch]
	patch.CompatibleArtifacts = []string{"unselected"}
	d.Patches["incompatible"] = patch
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"unknown profile", []string{"--profile", "unlisted-profile"}, "unknown profile"},
		{"incompatible patch", []string{"--profile", compactProfile, "--template", compactLayout + "=incompatible"}, "compatible"},
		{"unknown layout", []string{"--profile", compactProfile, "--template", "unlisted-layout=builtin"}, "unselected layout"},
		{"unknown context layout", []string{"--profile", compactProfile, "--context", "unknown=65536"}, "unselected layout"},
		{"unselected context layout", []string{"--profile", compactProfile, "--context", "qwen3.8-27b-q4xl-mtp=65536"}, "unselected layout"},
		{"context above maximum", []string{"--profile", compactProfile, "--context", compactLayout + "=262145"}, "context must be between"},
		{"context below output", []string{"--profile", compactProfile, "--context", compactLayout + "=4096"}, "context must be between"},
		{"noninteger context", []string{"--profile", compactProfile, "--context", compactLayout + "=64k"}, "positive integer"},
		{"zero context", []string{"--profile", compactProfile, "--context", compactLayout + "=0"}, "positive integer"},
		{"duplicate context", []string{"--profile", compactProfile, "--context", compactLayout + "=65536", "--context", compactLayout + "=32768"}, "context repeated"},
		{"context during resume", []string{"--resume", "--context", compactLayout + "=65536"}, "--resume uses saved choices"},
		{"context without profile", []string{"--context", compactLayout + "=65536"}, "scripted options require"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "absent-root")
			c := fixtureCommand(t)
			c.Catalog = func(context.Context, string, string) (catalog.Document, string, error) { return d, fixtureDigest, nil }
			args := append([]string{"--root", root}, tc.args...)
			code, _, diagnostics := runSetup(c, args...)
			if code == 0 || !strings.Contains(diagnostics, tc.want) {
				t.Fatalf("code=%d diagnostics=%q", code, diagnostics)
			}
			if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("refused choice changed root: %v", err)
			}
		})
	}
}

func TestCancelLeavesDefaultRootAbsent(t *testing.T) {
	home := t.TempDir()
	c := fixtureCommand(t)
	c.Home = func() (string, error) { return home, nil }
	c.UI = func(_ context.Context, _ io.Reader, _ io.Writer, input setupui.Input, _ func(context.Context, setupui.Choices) (setupui.Review, error)) (setupui.Decision, error) {
		if len(input.Modes) != 2 || !strings.Contains(input.Machine, "Apple M2") {
			t.Fatalf("wizard input = %+v", input)
		}
		return setupui.Decision{Action: "cancel"}, nil
	}
	code, output, diagnostics := runSetup(c)
	if code != 0 || !strings.Contains(output, "cancelled") {
		t.Fatalf("cancel = %d %q %q", code, output, diagnostics)
	}
	if _, err := os.Stat(filepath.Join(home, ".temper")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancel created root: %v", err)
	}
}

func TestDefaultRootIsHomeTemper(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".temper")
	c := fixtureCommand(t)
	c.Home = func() (string, error) { return home, nil }
	c.Disk = func(got string) (int64, error) {
		if got != root {
			t.Fatalf("disk root = %q", got)
		}
		return 40 * GiB, nil
	}
	c.Catalog = func(_ context.Context, got, _ string) (catalog.Document, string, error) {
		if got != root {
			t.Fatalf("catalog root = %q", got)
		}
		return testedCompactCatalog(t, guidedCatalog(t)), fixtureDigest, nil
	}
	code, output, diagnostics := runSetup(c, "--profile", compactProfile, "--dry-run", "--json")
	if code != 0 {
		t.Fatalf("default root failed: %s", diagnostics)
	}
	var got report
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatal(err)
	}
	if got.Plan.Root != root {
		t.Fatalf("default root = %q", got.Plan.Root)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("default dry run created root: %v", err)
	}
}

func TestPreviewTokenPinsAcceptedPlanAcrossMovingResolution(t *testing.T) {
	root := filepath.Join(t.TempDir(), "setup")
	c := fixtureCommand(t)
	want := guidedCatalog(t).Runtime.Router.Release.Version
	var resolutions int
	c.Resolve = func(_ context.Context, d catalog.Document, _ catalog.Selection, choice string) (catalog.Document, error) {
		if choice != "latest" {
			t.Fatalf("choice = %q", choice)
		}
		resolutions++
		release := *d.Runtime.Router.Release
		if resolutions == 2 {
			release.Version = "v999999"
		}
		d.Runtime.Router.Release = &release
		return d, nil
	}
	c.UI = func(ctx context.Context, _ io.Reader, _ io.Writer, _ setupui.Input, preview func(context.Context, setupui.Choices) (setupui.Review, error)) (setupui.Decision, error) {
		choices := setupui.Choices{Profiles: []setupui.Choice{{Mode: "local", Profile: compactProfile, ContextWindows: map[string]int{compactLayout: 16384}}}, Software: "latest"}
		accepted, err := preview(ctx, choices)
		if err != nil {
			t.Fatal(err)
		}
		moving, err := preview(ctx, choices)
		if err != nil {
			t.Fatal(err)
		}
		if accepted.Token == "" || accepted.Token == moving.Token {
			t.Fatalf("moving versions shared token: %q %q", accepted.Token, moving.Token)
		}
		return setupui.Decision{Choices: choices, Action: "save", ReviewToken: accepted.Token}, nil
	}
	code, _, diagnostics := runSetup(c, "--root", root)
	if code != 0 {
		t.Fatalf("accepted preview save failed: %s", diagnostics)
	}
	if resolutions != 2 {
		t.Fatalf("resolution count = %d", resolutions)
	}
	saved, err := setup.Load(root)
	locks := saved.Locks
	if err != nil {
		t.Fatal(err)
	}
	if got := locks[0].Records.Runtime.Router.Release.Version; got != want {
		t.Fatalf("saved moving version %q, want accepted %s", got, want)
	}
}

func TestMissingOrFailedPreviewCannotSave(t *testing.T) {
	for _, tc := range []struct {
		name        string
		failPreview bool
	}{{"missing token", false}, {"preview failure", true}} {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "absent-root")
			c := fixtureCommand(t)
			if tc.failPreview {
				c.Resolve = func(context.Context, catalog.Document, catalog.Selection, string) (catalog.Document, error) {
					return catalog.Document{}, errors.New("upstream unavailable")
				}
			}
			c.UI = func(ctx context.Context, _ io.Reader, _ io.Writer, _ setupui.Input, preview func(context.Context, setupui.Choices) (setupui.Review, error)) (setupui.Decision, error) {
				choices := setupui.Choices{Profiles: []setupui.Choice{{Mode: "local", Profile: compactProfile}}, Software: "recorded"}
				review, err := preview(ctx, choices)
				if tc.failPreview {
					if err == nil {
						t.Fatal("preview unexpectedly succeeded")
					}
				} else if err != nil || review.Token == "" {
					t.Fatalf("preview failed to produce a token: %+v, %v", review, err)
				}
				return setupui.Decision{Choices: choices, Action: "save"}, nil
			}
			code, _, diagnostics := runSetup(c, "--root", root)
			if code == 0 || !strings.Contains(diagnostics, "no matching reviewed") {
				t.Fatalf("code=%d diagnostics=%q", code, diagnostics)
			}
			if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed review created root: %v", err)
			}
		})
	}
}

func TestPrepareNeedsExplicitActionAndUsesSavedReviewedLock(t *testing.T) {
	for _, action := range []string{"save", "prepare"} {
		t.Run(action, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "setup")
			c := fixtureCommand(t)
			var dispatched int
			c.Dispatch = func(_ context.Context, args []string, out, _ io.Writer) int {
				dispatched++
				saved, err := os.ReadFile(filepath.Join(root, setup.ConfigurationDir, "local.execution.lock.json"))
				if err != nil {
					t.Fatalf("configuration not saved before prepare: %v", err)
				}
				lockPath := argument(args, "--lock")
				prepared, err := os.ReadFile(lockPath)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(saved, prepared) {
					t.Fatal("prepared lock differs from accepted saved lock")
				}
				if _, err := io.WriteString(out, `{"generation":"`+strings.Repeat("a", 64)+`"}`); err != nil {
					t.Fatal(err)
				}
				return 0
			}
			c.UI = func(ctx context.Context, _ io.Reader, _ io.Writer, _ setupui.Input, preview func(context.Context, setupui.Choices) (setupui.Review, error)) (setupui.Decision, error) {
				choices := setupui.Choices{Profiles: []setupui.Choice{{Mode: "local", Profile: compactProfile}}, Software: "recorded"}
				review, err := preview(ctx, choices)
				if err != nil {
					t.Fatal(err)
				}
				if !review.CanPrepare {
					t.Fatalf("compact preview refused: %s", review.PrepareReason)
				}
				return setupui.Decision{Choices: choices, Action: action, ReviewToken: review.Token}, nil
			}
			code, _, diagnostics := runSetup(c, "--root", root)
			if code != 0 {
				t.Fatalf("%s failed: %s", action, diagnostics)
			}
			want := 0
			if action == "prepare" {
				want = 1
			}
			if dispatched != want {
				t.Fatalf("dispatches = %d, want %d", dispatched, want)
			}
			if _, err := setup.Load(root); err != nil {
				t.Fatalf("configuration missing: %v", err)
			}
		})
	}
}

func TestResumeUsesSavedExactLockAndPreservesUserEdit(t *testing.T) {
	root := filepath.Join(t.TempDir(), "setup")
	c := fixtureCommand(t)
	code, _, diagnostics := runSetup(c, "--root", root, "--profile", compactProfile)
	if code != 0 {
		t.Fatalf("initial save failed: %s", diagnostics)
	}
	lockPath := filepath.Join(root, setup.ConfigurationDir, "local.execution.lock.json")
	wantLock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	c.Catalog = func(context.Context, string, string) (catalog.Document, string, error) {
		t.Fatal("resume read catalog")
		return catalog.Document{}, "", nil
	}
	c.Resolve = func(context.Context, catalog.Document, catalog.Selection, string) (catalog.Document, error) {
		t.Fatal("resume resolved software")
		return catalog.Document{}, nil
	}
	c.UI = func(context.Context, io.Reader, io.Writer, setupui.Input, func(context.Context, setupui.Choices) (setupui.Review, error)) (setupui.Decision, error) {
		t.Fatal("resume opened wizard")
		return setupui.Decision{}, nil
	}
	var dispatched int
	c.Dispatch = func(_ context.Context, args []string, out, _ io.Writer) int {
		dispatched++
		prepared, err := os.ReadFile(argument(args, "--lock"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(prepared, wantLock) {
			t.Fatal("resume prepared changed lock")
		}
		io.WriteString(out, `{"generation":"`+strings.Repeat("b", 64)+`"}`)
		return 0
	}
	code, _, diagnostics = runSetup(c, "--root", root, "--resume", "--prepare")
	if code != 0 || dispatched != 1 {
		t.Fatalf("resume = code %d, dispatches %d: %s", code, dispatched, diagnostics)
	}
	gotLock, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotLock, wantLock) {
		t.Fatal("resume changed saved lock")
	}

	selectionPath := filepath.Join(root, setup.ConfigurationDir, "local.selection.json")
	userEdit := []byte(`{"schema":"temper-selection/v2","profile":"qwen3.5-4b-utility"}`)
	if err := os.WriteFile(selectionPath, userEdit, 0o600); err != nil {
		t.Fatal(err)
	}
	dispatched = 0
	code, _, diagnostics = runSetup(c, "--root", root, "--resume", "--prepare")
	if code == 0 || dispatched != 0 {
		t.Fatalf("edited resume = code %d dispatches %d: %s", code, dispatched, diagnostics)
	}
	gotEdit, err := os.ReadFile(selectionPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotEdit, userEdit) {
		t.Fatal("resume overwrote user edit")
	}
	c = fixtureCommand(t)
	code, _, diagnostics = runSetup(c, "--root", root, "--profile", compactProfile)
	if code == 0 || !strings.Contains(diagnostics, "preserved user") {
		t.Fatalf("new setup replaced user edit: code %d diagnostics %q", code, diagnostics)
	}
	gotEdit, err = os.ReadFile(selectionPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotEdit, userEdit) {
		t.Fatal("new setup overwrote user edit")
	}
}

func TestLowDiskRefusesPrepareBeforeSaving(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent-root")
	c := fixtureCommand(t)
	c.Disk = func(string) (int64, error) { return 1, nil }
	code, _, diagnostics := runSetup(c, "--root", root, "--profile", compactProfile, "--prepare")
	if code == 0 || !strings.Contains(diagnostics, "disk") {
		t.Fatalf("low-disk prepare = %d %q", code, diagnostics)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("low-disk refusal created root: %v", err)
	}
}

func TestPrepareRechecksDiskAfterPreviewBeforeSaving(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent-root")
	c := fixtureCommand(t)
	free := 40 * GiB
	c.Disk = func(string) (int64, error) {
		return free, nil
	}
	c.UI = func(ctx context.Context, _ io.Reader, _ io.Writer, _ setupui.Input, preview func(context.Context, setupui.Choices) (setupui.Review, error)) (setupui.Decision, error) {
		choices := setupui.Choices{Profiles: []setupui.Choice{{Mode: "local", Profile: compactProfile}}, Software: "recorded"}
		review, err := preview(ctx, choices)
		if err != nil || !review.CanPrepare {
			t.Fatalf("initial preview: %+v, %v", review, err)
		}
		free = 1 // Space disappears after review, before the user prepares.
		return setupui.Decision{Choices: choices, Action: "prepare", ReviewToken: review.Token}, nil
	}
	code, _, diagnostics := runSetup(c, "--root", root)
	if code == 0 || !strings.Contains(diagnostics, "disk") {
		t.Fatalf("code %d, diagnostics %q", code, diagnostics)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed recheck saved root: %v", err)
	}
}

func argument(args []string, name string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == name {
			return args[i+1]
		}
	}
	return ""
}
