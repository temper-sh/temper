package setupcmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/setup"
)

func TestCatalogOrderGroupsAvailableTiersBeforeEditorialOrder(t *testing.T) {
	doc := guidedCatalog(t)
	// Keep this ordering fixture independent of future catalog additions.
	for id := range doc.Profiles {
		if !slices.Contains([]string{"qwen3.8-27b-q4xl-local", "qwen3.8-27b-splash-local", "gemma-4-e4b-local", "gemma-4-e2b-local", compactProfile}, id) {
			delete(doc.Profiles, id)
		}
	}
	const baseline = "qwen3.8-27b-q4xl-mtp"
	doc.Layouts["preferred"] = doc.Layouts[baseline]
	doc.Profiles["z-preferred"] = catalog.Profile{GPUMemoryUtilization: .85, Bindings: []catalog.Binding{{Layout: "preferred", Route: "default", Residency: "resident", IdleTTLSeconds: 1800}}}
	// XS appears first in the editorial list, but available S still leads.
	doc.LayoutOrder = []string{"gemma-4-e4b-q4km-off", "preferred", baseline, "gemma-4-e2b-q4km-off", compactLayout}
	input, err := options(doc, thirtyTwoGiBFacts(), 100*GiB, true)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, p := range input.Profiles {
		if p.Mode == "local" {
			ids = append(ids, p.ID)
		}
	}
	want := []string{"z-preferred", "qwen3.8-27b-q4xl-local", "gemma-4-e4b-local", "gemma-4-e2b-local", compactProfile, "qwen3.8-27b-splash-local"}
	if !slices.Equal(ids, want) {
		t.Fatalf("model order = %v, want %v", ids, want)
	}
	if p := input.Profiles[0]; p.Name != "Model: Qwen3.8 27B" || !strings.Contains(p.Components, "Weights: Unsloth UD-Q4_K_XL") || !strings.Contains(p.Components, "Engine: llama.cpp") {
		t.Fatalf("model/weights/engine are not distinct: %+v", p)
	}
	small, err := options(doc, eightGiBFacts(), 100*GiB, true)
	if err != nil {
		t.Fatal(err)
	}
	if small.Profiles[0].MemoryTier != "XS" || small.Profiles[0].DisabledReason != "" || small.Profiles[len(small.Profiles)-1].DisabledReason == "" {
		t.Fatal("an unavailable larger group displaced available models")
	}
}

func TestMultipleInstalledModelsPreserveDefaultThroughPrepareAndOfflineResume(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	c := fixtureCommand(t)
	c.Detect = func(context.Context) (machine.Facts, error) { return thirtyTwoGiBFacts(), nil }
	const alternate = "gemma-4-e2b-local"
	const alternateLayout = "gemma-4-e2b-q4km-off"
	args := []string{"--root", root, "--profile", alternate, "--profile", compactProfile, "--default-profile", compactProfile, "--context", alternateLayout + "=16384", "--context", compactLayout + "=16384", "--prepare", "--json"}
	var preparedProfiles, installationIDs []string
	c.Dispatch = func(_ context.Context, args []string, out, _ io.Writer) int {
		if !slices.Equal(args[:2], []string{"execution", "prepare"}) {
			t.Fatalf("setup tried to start a model: %v", args)
		}
		lockPath := args[slices.Index(args, "--lock")+1]
		data, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatal(err)
		}
		locked, err := catalog.ParseLock(data)
		if err != nil {
			t.Fatal(err)
		}
		preparedProfiles = append(preparedProfiles, locked.Selection.Profile)
		installationIDs = append(installationIDs, args[slices.Index(args, "--installation")+1])
		fmt.Fprintf(out, `{"generation":%q}`, strings.Repeat("a", 64))
		return 0
	}
	code, output, diagnostics := runSetup(c, args...)
	if code != 0 {
		t.Fatal(diagnostics)
	}
	var report report
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatal(err)
	}
	if report.Plan.DefaultProfile != compactProfile || len(report.Plan.Modes) != 2 || !report.Prepared[0].Default || report.Prepared[1].Default {
		t.Fatalf("default/alternatives not disclosed: %+v", report)
	}
	if !slices.Equal(preparedProfiles, []string{compactProfile, alternate}) || !slices.Equal(installationIDs, []string{"setup-local", "setup-local." + alternate}) {
		t.Fatalf("preparation mixed configuration identities: %v %v", preparedProfiles, installationIDs)
	}
	saved, err := setup.Load(root)
	if err != nil || saved.DefaultProfile != compactProfile || len(saved.Locks) != 2 {
		t.Fatalf("saved setup: %+v %v", saved, err)
	}
	before, err := os.ReadFile(filepath.Join(root, setup.ConfigurationDir, "local.execution.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	c.Catalog = func(context.Context, string, string) (catalog.Document, string, error) {
		t.Fatal("resume read a moving catalog")
		return catalog.Document{}, "", errors.New("offline")
	}
	code, _, diagnostics = runSetup(c, "--root", root, "--resume", "--prepare")
	if code != 0 {
		t.Fatal(diagnostics)
	}
	after, err := os.ReadFile(filepath.Join(root, setup.ConfigurationDir, "local.execution.lock.json"))
	if err != nil || string(before) != string(after) {
		t.Fatal("resume rewrote the exact default lock")
	}
	if !slices.Equal(preparedProfiles, []string{compactProfile, alternate, compactProfile, alternate}) {
		t.Fatalf("resume lost an installed choice: %v", preparedProfiles)
	}
}

func TestAmbiguousOrUnselectedDefaultRefusedWithoutSaving(t *testing.T) {
	for _, tc := range []struct {
		name           string
		defaultProfile string
	}{
		{name: "no default"},
		{name: "unselected default", defaultProfile: "qwen3.8-27b-q4xl-local"},
		{name: "utility is not a local default", defaultProfile: utilityProfile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "absent")
			c := fixtureCommand(t)
			args := []string{"--root", root, "--profile", compactProfile, "--profile", "gemma-4-e2b-local", "--profile", utilityProfile, "--context", "gemma-4-e2b-q4km-off=16384"}
			if tc.defaultProfile != "" {
				args = append(args, "--default-profile", tc.defaultProfile)
			}
			code, _, diagnostics := runSetup(c, args...)
			if code == 0 || !strings.Contains(diagnostics, "default profile") {
				t.Fatalf("bad default accepted or wrong refusal: %d %s", code, diagnostics)
			}
			if _, err := os.Lstat(root); !os.IsNotExist(err) {
				t.Fatalf("refusal wrote setup state: %v", err)
			}
		})
	}
}

func TestSplashLeadsSOnCompatibleMachineAndIsUnavailableBelowPlatformFloor(t *testing.T) {
	doc := guidedCatalog(t)
	facts := thirtyTwoGiBFacts()
	facts.Chip = "Apple M5"
	facts.Target.DistributionVersion = "26.4"
	input, err := options(doc, facts, 100*GiB, true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"qwen3.8-27b-splash-local", "muse-glimmer-30b-local", "gemma-4-26b-a4b-local", "gemma-4-31b-local", "qwen3.8-27b-q4xl-local"}
	if len(input.Profiles) < len(want) {
		t.Fatalf("missing S choices: %+v", input.Profiles)
	}
	for i, id := range want {
		if input.Profiles[i].ID != id || input.Profiles[i].MemoryTier != "S" || input.Profiles[i].DisabledReason != "" {
			t.Fatalf("S choice %d: %+v, want %s available", i, input.Profiles[i], id)
		}
	}
	if !strings.Contains(input.Profiles[0].Components, "Splash 1.1.0 / DFlash2") {
		t.Fatal(input.Profiles[0].Components)
	}
	facts.Chip = "Apple M2"
	input, err = options(doc, facts, 100*GiB, true)
	if err != nil {
		t.Fatal(err)
	}
	if input.Profiles[0].ID == "qwen3.8-27b-splash-local" {
		t.Fatal("incompatible Splash led the available list")
	}
}
