package baseline_test

import (
	"testing"

	"github.com/temper-sh/temper/internal/fieldkit/baseline"
)

func TestBuiltinCatalogRetainsHistoryAndActivatesMultiStageV3(t *testing.T) {
	snapshot, err := baseline.LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Document.Revision != 3 || len(snapshot.Entries) != 3 {
		t.Fatalf("catalog revision/entries = %d/%d, want 3/3", snapshot.Document.Revision, len(snapshot.Entries))
	}
	legacy, err := baseline.Find(snapshot, "qwen38-dynamic-q4xl@1")
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Reference.Availability != "retired" || legacy.Package.Mechanics.Runner == nil || legacy.Package.Mechanics.Runner.Path == "" {
		t.Fatalf("legacy package = %#v", legacy.Reference)
	}
	owned, err := baseline.Find(snapshot, "qwen38-dynamic-q4xl@2")
	if err != nil {
		t.Fatal(err)
	}
	if owned.Reference.Availability != "retired" || owned.Package.Mechanics.Runner != nil || owned.Package.Mechanics.Orchestration != "" {
		t.Fatalf("Temper-owned predecessor = %#v", owned.Reference)
	}
	active, err := baseline.Find(snapshot, "qwen38-dynamic-q4xl@3")
	if err != nil {
		t.Fatal(err)
	}
	if active.Reference.Availability != "active" || active.Package.Mechanics.Runner != nil || active.Package.Mechanics.Orchestration != baseline.OrchestrationTemperMultiStageV1 {
		t.Fatalf("active package = %#v", active.Reference)
	}
	if active.Package.Mechanics.Protocol == nil || active.Package.Mechanics.Protocol.ID != "qwen38-dynamic-short" || active.Package.Mechanics.Protocol.Revision != 1 {
		t.Fatalf("active protocol = %#v", active.Package.Mechanics.Protocol)
	}
	if _, err := active.Material(active.Package.Mechanics.Manifest.Path); err != nil {
		t.Fatal(err)
	}
}
