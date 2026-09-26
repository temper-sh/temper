package setupcmd

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/setupui"
)

func TestUnknownContextRequiresExplicitChoiceWithoutCreatingRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent")
	c := fixtureCommand(t)
	c.Catalog = func(context.Context, string, string) (catalog.Document, string, error) {
		return guidedCatalog(t), fixtureDigest, nil
	}
	code, _, diagnostics := runSetup(c, "--root", root, "--profile", compactProfile, "--dry-run")
	if code == 0 || !strings.Contains(diagnostics, "tested context is unknown") {
		t.Fatalf("invented default: %d %s", code, diagnostics)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("failed automatic choice wrote root")
	}
	code, output, diagnostics := runSetup(c, "--root", root, "--profile", compactProfile, "--context", compactLayout+"=65536", "--dry-run", "--json")
	if code != 0 {
		t.Fatal(diagnostics)
	}
	var got report
	if err := json.Unmarshal([]byte(output), &got); err != nil {
		t.Fatal(err)
	}
	if got.Plan.Modes[0].Selection.ContextWindows[compactLayout] != 65536 || got.Plan.Modes[0].Contexts[compactLayout].Status != "unknown" {
		t.Fatal("explicit context was replaced or falsely marked tested")
	}
}

func TestWizardRequestsMissingContextAndReviewsTheCorrectionBeforeSave(t *testing.T) {
	root := filepath.Join(t.TempDir(), "setup")
	c := fixtureCommand(t)
	c.Catalog = func(context.Context, string, string) (catalog.Document, string, error) {
		return guidedCatalog(t), fixtureDigest, nil
	}
	c.UI = func(ctx context.Context, _ io.Reader, _ io.Writer, input setupui.Input, preview func(context.Context, setupui.Choices) (setupui.Review, error)) (setupui.Decision, error) {
		for _, p := range input.Profiles {
			if p.ID == utilityProfile && !p.Contexts[0].ManualRequired {
				t.Fatal("wizard offered automatic context without any catalog findings")
			}
		}
		choices := setupui.Choices{Profiles: []setupui.Choice{{Mode: "utility", Profile: utilityProfile}}, Software: "recorded"}
		review, err := preview(ctx, choices)
		if err != nil || review.ContextRequired == nil || review.ContextRequired.Profile != utilityProfile || review.ContextRequired.Layout != compactLayout || review.Token != "" {
			t.Fatalf("missing context did not become an input request: %+v, %v", review, err)
		}
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Fatal("input request created the setup root")
		}
		choices.Profiles[0].ContextWindows = map[string]int{compactLayout: 16384}
		review, err = preview(ctx, choices)
		if err != nil || review.ContextRequired != nil || review.Token == "" || !review.CanPrepare {
			t.Fatalf("explicit context did not produce a usable review: %+v, %v", review, err)
		}
		return setupui.Decision{Choices: choices, Action: "save", ReviewToken: review.Token}, nil
	}
	if code, _, diagnostics := runSetup(c, "--root", root); code != 0 {
		t.Fatal(diagnostics)
	}
	saved, err := setup.Load(root)
	locks := saved.Locks
	if err != nil || len(locks) != 1 || locks[0].Selection.ContextWindows[compactLayout] != 16384 {
		t.Fatalf("saved context differs from the corrected review: %+v, %v", locks, err)
	}
}

func TestAutomaticContextChecksResolvedSoftwareAndTemplate(t *testing.T) {
	for _, change := range []string{"release", "template"} {
		t.Run(change, func(t *testing.T) {
			c := fixtureCommand(t)
			c.Resolve = func(ctx context.Context, d catalog.Document, s catalog.Selection, choice string) (catalog.Document, error) {
				if change == "release" {
					l := d.Layouts[compactLayout]
					e := d.Engines[l.Engine]
					r := *e.Supply.Release
					r.Version = "b12000"
					e.Supply.Release = &r
					d.Engines[l.Engine] = e
				}
				return d, nil
			}
			if change == "template" {
				d := testedCompactCatalog(t, guidedCatalog(t))
				p := d.Patches[largePatch]
				p.CompatibleArtifacts = []string{d.Layouts[compactLayout].Artifact}
				d.Patches["alternative"] = p
				c.Catalog = func(context.Context, string, string) (catalog.Document, string, error) { return d, fixtureDigest, nil }
			}
			args := []string{"--root", filepath.Join(t.TempDir(), "absent"), "--profile", compactProfile, "--software", "latest", "--dry-run"}
			if change == "template" {
				args = append(args, "--template", compactLayout+"=alternative")
			}
			if code, _, diag := runSetup(c, args...); code == 0 || !strings.Contains(diag, "tested context is unknown") {
				t.Fatalf("changed %s inherited evidence: %d %s", change, code, diag)
			}
			args = append(args, "--context", compactLayout+"=32768")
			if code, out, diag := runSetup(c, args...); code != 0 || !strings.Contains(out, "Context fit: unknown") {
				t.Fatalf("explicit %s override: %d %s %s", change, code, out, diag)
			}
		})
	}
}

func TestDescriptionAndContextEvidenceSurviveReviewSaveAndResume(t *testing.T) {
	c := fixtureCommand(t)
	root := filepath.Join(t.TempDir(), "setup")
	d := testedCompactCatalog(t, guidedCatalog(t))
	link := "https://example.test/owner-assessment"
	d, err := catalog.Describe(d, d.Layouts[compactLayout].Artifact, "My main chat model; I review its factual claims.", &link, false)
	if err != nil {
		t.Fatal(err)
	}
	c.Catalog = func(context.Context, string, string) (catalog.Document, string, error) { return d, fixtureDigest, nil }
	input, err := options(d, eightGiBFacts(), 40*GiB, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range input.Profiles {
		if p.ID == compactProfile || p.ID == utilityProfile {
			if p.Description != "My main chat model; I review its factual claims." || p.AssessmentURL != link || !strings.Contains(p.Details, "Model files") {
				t.Fatalf("copy and technical facts not separated: %+v", p)
			}
		}
	}
	if code, out, diag := runSetup(c, "--root", root, "--profile", compactProfile); code != 0 || !strings.Contains(out, "My main chat model") || !strings.Contains(out, "Synthetic slow-response observation") {
		t.Fatalf("review: %d %s %s", code, out, diag)
	}
	// Resume must use the saved text, number and evidence even if the catalog moves.
	c.Catalog = func(context.Context, string, string) (catalog.Document, string, error) {
		t.Fatal("resume consulted new catalog")
		return catalog.Document{}, "", nil
	}
	if code, out, diag := runSetup(c, "--root", root, "--resume", "--dry-run"); code != 0 || !strings.Contains(out, "context 16384 tokens") || !strings.Contains(out, "My main chat model") {
		t.Fatalf("resume: %d %s %s", code, out, diag)
	}
}

func TestScriptContextIsReviewedSavedForBothModesAndResumedExactly(t *testing.T) {
	root := filepath.Join(t.TempDir(), "setup")
	c := fixtureCommand(t)
	args := []string{"--root", root, "--profile", compactProfile, "--profile", utilityProfile, "--context", compactLayout + "=65536"}
	code, output, diagnostics := runSetup(c, append(args, "--dry-run")...)
	if code != 0 || !strings.Contains(output, "context 65536 tokens (input + output)") {
		t.Fatalf("preview omitted context: %d %s %s", code, output, diagnostics)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("context preview wrote configuration")
	}
	code, _, diagnostics = runSetup(c, args...)
	if code != 0 {
		t.Fatal(diagnostics)
	}
	saved, err := setup.Load(root)
	locks := saved.Locks
	if err != nil {
		t.Fatal(err)
	}
	if len(locks) != 2 {
		t.Fatal("missing selected mode")
	}
	for _, locked := range locks {
		if locked.Selection.ContextWindows[compactLayout] != 65536 || locked.Records.Layouts[compactLayout].ContextWindowTokens != 65536 {
			t.Fatal("context lost in saved choices or exact lock")
		}
	}
	c.Catalog = func(context.Context, string, string) (catalog.Document, string, error) {
		t.Fatal("resume read changing catalog defaults")
		return catalog.Document{}, "", nil
	}
	c.Resolve = func(context.Context, catalog.Document, catalog.Selection, string) (catalog.Document, error) {
		t.Fatal("resume resolved software again")
		return catalog.Document{}, nil
	}
	code, output, diagnostics = runSetup(c, "--root", root, "--resume", "--dry-run")
	if code != 0 || strings.Count(output, "context 65536 tokens") != 2 {
		t.Fatalf("resume lost exact windows: %d %s %s", code, output, diagnostics)
	}
}

func TestContextPreviewTokenSavesTheReviewedWindow(t *testing.T) {
	root := filepath.Join(t.TempDir(), "setup")
	c := fixtureCommand(t)
	c.UI = func(ctx context.Context, _ io.Reader, _ io.Writer, _ setupui.Input, preview func(context.Context, setupui.Choices) (setupui.Review, error)) (setupui.Decision, error) {
		choices := setupui.Choices{Profiles: []setupui.Choice{{Mode: "local", Profile: compactProfile, ContextWindows: map[string]int{compactLayout: 65536}}}, Software: "recorded"}
		accepted, err := preview(ctx, choices)
		if err != nil {
			t.Fatal(err)
		}
		choices.Profiles[0].ContextWindows[compactLayout] = 262144
		other, err := preview(ctx, choices)
		if err != nil {
			t.Fatal(err)
		}
		if accepted.Token == other.Token {
			t.Fatal("different context choices shared a preview token")
		}
		return setupui.Decision{Action: "save", ReviewToken: accepted.Token}, nil
	}
	code, _, diagnostics := runSetup(c, "--root", root)
	if code != 0 {
		t.Fatal(diagnostics)
	}
	saved, err := setup.Load(root)
	locks := saved.Locks
	if err != nil {
		t.Fatal(err)
	}
	if locks[0].Records.Layouts[compactLayout].ContextWindowTokens != 65536 {
		t.Fatal("save used a different window than the accepted review")
	}
}
