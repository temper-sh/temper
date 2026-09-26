package setupcmd

import (
	"context"
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
	"github.com/temper-sh/temper/internal/setupui"
)

func TestPreviewRefreshesEffectiveMetalBudgetAfterManualChange(t *testing.T) {
	c := fixtureCommand(t)
	facts := thirtyTwoGiBFacts()
	facts.MetalDeviceMemoryMiB, facts.WiredLimitMiB = 18432, 18432
	facts.MetalDeviceMemorySource, facts.WiredLimitSource = machine.MetalDeviceSourceLive, budget.WiredSourceMetal
	c.Detect = func(context.Context) (machine.Facts, error) { return facts, nil }
	root := filepath.Join(t.TempDir(), "absent")
	c.UI = func(ctx context.Context, _ io.Reader, _ io.Writer, _ setupui.Input, preview func(context.Context, setupui.Choices) (setupui.Review, error)) (setupui.Decision, error) {
		choices := setupui.Choices{Software: "recorded", Profiles: []setupui.Choice{{Mode: "local", Profile: "qwen3.8-27b-q4xl-local", ContextWindows: map[string]int{"qwen3.8-27b-q4xl-mtp": 32768}}}}
		before, err := preview(ctx, choices)
		if err != nil || !before.Sections[0].Warning {
			t.Fatalf("missing initial warning: %+v, %v", before, err)
		}
		override := int64(24576)
		facts.WiredLimitOverrideMiB = &override
		configured, err := preview(ctx, choices)
		if err != nil || !configured.Sections[0].Warning {
			t.Fatalf("override alone cleared warning: %+v, %v", configured, err)
		}
		facts.MetalDeviceMemoryMiB, facts.WiredLimitMiB = 24576, 24576
		after, err := preview(ctx, choices)
		if err != nil || !after.CanPrepare {
			t.Fatalf("refreshed budget not usable: %+v, %v", after, err)
		}
		for _, section := range after.Sections {
			if section.Warning {
				t.Fatalf("retry retained stale warning: %+v", section)
			}
		}
		return setupui.Decision{Action: "save", ReviewToken: after.Token}, nil
	}
	code, output, diagnostics := runSetup(c, "--root", root, "--dry-run")
	if code != 0 {
		t.Fatal(diagnostics)
	}
	if strings.Contains(output, "Memory budget is tight") {
		t.Fatalf("saved preview retained stale warning: %s", output)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("dry run changed root: %v", err)
	}
}

func TestPreviewReportsFailedMetalRefreshWithoutReusingOldFacts(t *testing.T) {
	c := fixtureCommand(t)
	queryErr := errors.New("Metal unavailable")
	var failRead bool
	c.Detect = func(context.Context) (machine.Facts, error) {
		if failRead {
			return machine.Facts{}, queryErr
		}
		return eightGiBFacts(), nil
	}
	c.UI = func(ctx context.Context, _ io.Reader, _ io.Writer, _ setupui.Input, preview func(context.Context, setupui.Choices) (setupui.Review, error)) (setupui.Decision, error) {
		failRead = true
		_, err := preview(ctx, setupui.Choices{Software: "recorded", Profiles: []setupui.Choice{{Mode: "local", Profile: compactProfile}}})
		if !errors.Is(err, queryErr) {
			t.Fatalf("refresh reused old facts: %v", err)
		}
		return setupui.Decision{Action: "cancel"}, nil
	}
	if code, _, diagnostics := runSetup(c, "--root", filepath.Join(t.TempDir(), "absent"), "--dry-run"); code != 0 {
		t.Fatal(diagnostics)
	}
}

func thirtyTwoGiBFacts() machine.Facts {
	facts := eightGiBFacts()
	facts.PhysicalMemoryBytes = 32 * GiB
	facts.MetalDeviceMemoryMiB = 32768 * 81 / 100
	facts.WiredLimitMiB = 24576
	return facts
}

func TestMemoryAdviceReachesModelChoiceReviewAndScriptedPreview(t *testing.T) {
	c := fixtureCommand(t)
	c.Detect = func(context.Context) (machine.Facts, error) { return thirtyTwoGiBFacts(), nil }
	root := filepath.Join(t.TempDir(), "absent")
	c.UI = func(ctx context.Context, _ io.Reader, _ io.Writer, input setupui.Input, preview func(context.Context, setupui.Choices) (setupui.Review, error)) (setupui.Decision, error) {
		var advice *setupui.Section
		for _, profile := range input.Profiles {
			if profile.ID == "qwen3.8-27b-q4xl-local" {
				advice = profile.Advice
				if profile.DisabledReason != "" {
					t.Fatalf("soft warning disabled the profile: %s", profile.DisabledReason)
				}
			}
		}
		if advice == nil || !advice.Warning || !strings.Contains(strings.Join(advice.Lines, "\n"), "sudo sysctl iogpu.wired_limit_mb=26624") {
			t.Fatalf("model choice omitted actionable advice: %+v", advice)
		}
		review, err := preview(ctx, setupui.Choices{Software: "recorded", Profiles: []setupui.Choice{{
			Mode: "local", Profile: "qwen3.8-27b-q4xl-local", ContextWindows: map[string]int{"qwen3.8-27b-q4xl-mtp": 32768},
		}}})
		if err != nil {
			t.Fatal(err)
		}
		if !review.CanPrepare || !review.Sections[0].Warning || !strings.Contains(strings.Join(review.Sections[0].Lines, "\n"), "sudo sysctl iogpu.wired_limit_mb=26624") {
			t.Fatalf("review lost memory advice or changed preparation permission: %+v", review)
		}
		return setupui.Decision{Action: "cancel"}, nil
	}
	if code, _, diagnostics := runSetup(c, "--root", root, "--dry-run"); code != 0 {
		t.Fatal(diagnostics)
	}
	code, output, diagnostics := runSetup(c, "--root", root, "--profile", "qwen3.8-27b-q4xl-local", "--context", "qwen3.8-27b-q4xl-mtp=32768", "--dry-run", "--json")
	if code != 0 {
		t.Fatal(diagnostics)
	}
	var got report
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatal(err)
	}
	if got.Plan.Modes[0].WiredMemory == nil || got.Plan.Modes[0].WiredMemory.SuggestedLimitMiB != 26624 {
		t.Fatal("JSON preview omitted wired-memory advice")
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("advisory preview wrote configuration: %v", err)
	}
}

func TestBlockedPreparationAndModeChoiceExposeManualMemoryRemedy(t *testing.T) {
	d := guidedCatalog(t)
	d.Profiles = map[string]catalog.Profile{"qwen3.8-27b-q4xl-local": d.Profiles["qwen3.8-27b-q4xl-local"]}
	facts := thirtyTwoGiBFacts()
	facts.WiredLimitMiB, facts.WiredLimitSource = 32768*65/100, budget.WiredSourcePredicted
	input, err := options(d, facts, 100*GiB, true)
	if err != nil {
		t.Fatal(err)
	}
	if input.Modes[0].DisabledReason == "" || input.Modes[0].Advice == nil {
		t.Fatal("blocked mode hides the only profile's manual remedy")
	}
	c := fixtureCommand(t)
	c.Catalog = func(context.Context, string, string) (catalog.Document, string, error) { return d, fixtureDigest, nil }
	c.Detect = func(context.Context) (machine.Facts, error) { return facts, nil }
	root := filepath.Join(t.TempDir(), "absent")
	code, _, diagnostics := runSetup(c, "--root", root, "--profile", "qwen3.8-27b-q4xl-local", "--context", "qwen3.8-27b-q4xl-mtp=32768", "--prepare")
	if code == 0 || !strings.Contains(diagnostics, "sudo sysctl iogpu.wired_limit_mb=26624") {
		t.Fatalf("blocked preparation failed without its remedy: code=%d, %s", code, diagnostics)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("blocked preparation changed root: %v", err)
	}
}
