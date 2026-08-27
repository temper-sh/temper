package session_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/fieldkit/catalog"
	"github.com/temper-sh/temper/internal/fieldkit/session"
)

func TestSessionRecordsConsentAttemptsAdaptationAndReportIdentity(t *testing.T) {
	promoted := packageFixture("bounded-adaptive", 3)
	document, err := session.New(
		"session-001", snapshotFixture(), entryFixture(promoted),
		"machine.yaml", strings.Repeat("1", 64), strings.Repeat("2", 64),
		"2026-08-25T20:00:00Z",
	)
	if err != nil {
		t.Fatal(err)
	}
	document, err = document.StartAttempt(promoted, "2026-08-25T20:01:00Z", strings.Repeat("3", 64), map[string]string{
		"context-kib": "32768", "kv": "q8",
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err = document.Observe(promoted, 1, session.Observation{
		At: "2026-08-25T20:02:00Z", Kind: "task-result", Value: "pass",
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err = document.Decide(promoted, 1, session.Decision{
		At: "2026-08-25T20:03:00Z", Action: "adapt", Reason: "advance to the next promoted context value",
	})
	if err != nil {
		t.Fatal(err)
	}
	document, err = document.AddNote(promoted, "deviation", "2026-08-25T20:04:00Z", "background load was observed")
	if err != nil {
		t.Fatal(err)
	}
	document, err = document.Finish(promoted, "REPORT.md", strings.Repeat("4", 64))
	if err != nil {
		t.Fatal(err)
	}
	data, err := session.Marshal(document, promoted)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := session.Parse(data, promoted)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.State != "complete" || len(parsed.Attempts) != 1 || len(parsed.Deviations) != 1 || parsed.Report.Name != "REPORT.md" {
		t.Fatalf("session = %#v", parsed)
	}
}

func TestSessionRefusesOutOfBoundsAndFixedAdaptation(t *testing.T) {
	adaptive := packageFixture("bounded-adaptive", 2)
	document, err := session.New(
		"session-002", snapshotFixture(), entryFixture(adaptive),
		"machine.yaml", strings.Repeat("1", 64), strings.Repeat("2", 64),
		"2026-08-25T20:00:00Z",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := document.StartAttempt(adaptive, "2026-08-25T20:01:00Z", strings.Repeat("3", 64), map[string]string{
		"context-kib": "99999", "kv": "q8",
	}); err == nil || !strings.Contains(err.Error(), "outside promoted integer bounds") {
		t.Fatalf("out-of-bounds error = %v", err)
	}

	fixed := packageFixture("fixed", 1)
	fixed.Parameters = []catalog.Parameter{{ID: "arm", Kind: "fixed", Fixed: "candidate", Required: true}}
	document, err = session.New(
		"session-003", snapshotFixture(), entryFixture(fixed),
		"machine.yaml", strings.Repeat("1", 64), strings.Repeat("2", 64),
		"2026-08-25T20:00:00Z",
	)
	if err != nil {
		t.Fatal(err)
	}
	document, err = document.StartAttempt(fixed, "2026-08-25T20:01:00Z", strings.Repeat("3", 64), map[string]string{"arm": "candidate"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := document.Decide(fixed, 1, session.Decision{
		At: "2026-08-25T20:02:00Z", Action: "adapt", Reason: "not allowed",
	}); err == nil || !strings.Contains(err.Error(), "cannot adapt") {
		t.Fatalf("fixed adaptation error = %v", err)
	}
}

func TestStoreCommitsOnceAndRefusesAStaleSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions", "one.json")
	if err := os.Mkdir(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	first, err := session.ReadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Commit(context.Background(), []byte("first\n")); err != nil {
		t.Fatal(err)
	}
	stale := first
	if err := stale.Commit(context.Background(), []byte("second\n")); err == nil || !strings.Contains(err.Error(), "changed concurrently") {
		t.Fatalf("stale commit error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "first\n" {
		t.Fatalf("stored data = %q", data)
	}
}

func packageFixture(kind string, attempts int64) catalog.Package {
	hash := strings.Repeat("a", 64)
	return catalog.Package{
		Schema: catalog.PackageSchemaV1, ID: "context-envelope", Revision: 1,
		Origin: catalog.Origin{
			ExperimentID: "context-envelope", ExperimentRevision: 1, ExperimentSHA256: hash,
			PromotionID: "context-envelope-field-kit", PromotionRevision: 1, PromotionSHA256: hash,
		},
		Kind: kind, Title: "Context envelope", Question: "Which promoted context value remains stable?", Decision: "Whether the exact profile advances.",
		Applicability: catalog.Predicate{
			OS: "darwin", Arch: "arm64", Distribution: "macos",
			MinPhysicalMemoryMiB: 32768, MinWiredLimitMiB: 20000,
		},
		Relevance: []catalog.Signal{},
		Cost: catalog.Cost{
			FixedRuntimeMinutes: 5, SetupMinutesMin: 0, SetupMinutesMax: 1,
			MemoryPressure: "high", ServiceDisruption: "isolated-service-only", PaidProvider: "none", IdleRequired: true,
		},
		Consent: catalog.Consent{
			Choices: []string{"run-context-envelope"}, Reads: []string{"machine-facts"}, Writes: []string{"isolated-report"},
			NetworkDestinations: []string{}, LocalOutput: "local-only", Cleanup: "restore-isolated-root", RenewedConsent: []string{"any bound increase"},
		},
		Parameters: []catalog.Parameter{
			{ID: "context-kib", Kind: "integer", Minimum: 16384, Maximum: 65536, Required: true},
			{ID: "kv", Kind: "enum", Values: []string{"q4", "q8"}, Required: true},
		},
		Bounds:    catalog.Bounds{MaximumAttempts: attempts, MaximumRuntimeMinutes: 30},
		StopRules: []catalog.StopRule{{ID: "memory-pressure", Observation: "memory pressure exceeds promoted ceiling", Action: "stop"}},
		Mechanics: catalog.Mechanics{
			TemperProtocol: "temper-field-kit-binding/v1",
			Plan:           catalog.FileIdentity{Path: "plan.yaml", SHA256: hash}, ExternalInputs: []catalog.FileIdentity{},
			Resume: "resume-from-last-complete-attempt", Interruption: "stop-isolated-processes-and-preserve-session",
		},
		Report: catalog.Report{
			Schema: "field-kit-experiment-report/v1", RequiredConditions: []string{"machine", "temper-binding"},
			Sensitivity: "review-before-sharing", Submission: "explicit-export-only",
		},
		Prompt:       catalog.FileIdentity{Path: "prompt.md", SHA256: hash},
		Invalidation: []string{"any promoted material changes"},
	}
}

func snapshotFixture() catalog.Snapshot {
	return catalog.Snapshot{
		Document: catalog.Document{Schema: catalog.CatalogSchemaV1, Revision: 1, PromotedAt: "2026-08-25T20:00:00Z"},
		SHA256:   strings.Repeat("b", 64),
	}
}

func entryFixture(promoted catalog.Package) catalog.Entry {
	return catalog.Entry{
		Reference: catalog.Reference{
			ID: promoted.ID, Revision: promoted.Revision,
			Availability: "active", AvailabilityReason: "fixture is active",
			PackagePath: "experiments/context-envelope@1/package.json", PackageSHA256: strings.Repeat("c", 64),
		},
		Package: promoted,
	}
}
