package catalog_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/software"
)

func contextCatalog(t *testing.T) (catalog.Document, machine.Facts) {
	t.Helper()
	d := document()
	l := d.Layouts["qwen-32k"]
	l.ContextLimitTokens = 262144
	d.Layouts["qwen-32k"] = l
	facts := machine.Facts{Schema: machine.FactsSchemaV1,
		Target: software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "26.6"},
		Chip:   "Apple M5", HardwareModel: "Mac17,3", OSBuild: "25G76", PhysicalMemoryBytes: 32 << 30,
		MetalDeviceMemoryMiB: 32768 * 81 / 100, MetalDeviceMemorySource: machine.MetalDeviceSourcePredicted,
		WiredLimitMiB: 24576, WiredLimitSource: budget.WiredSourceLive}
	for _, window := range []int{65536, 32768} {
		execution, err := catalog.ContextExecutionSHA256(d, "qwen-32k", "template", window)
		if err != nil {
			t.Fatal(err)
		}
		l.ContextFindings = append(l.ContextFindings, catalog.ContextFinding{WindowTokens: window, MaxOutputTokens: 4096,
			ExecutionSHA256:        execution,
			Machine:                catalog.ContextMachine{Target: facts.Target, Chip: facts.Chip, HardwareModel: facts.HardwareModel, PhysicalMemoryBytes: facts.PhysicalMemoryBytes, MinimumWiredLimitMiB: 24000},
			EngineMemoryLimitBytes: 24 << 30, SwapGrowthLimitBytes: 512 << 20, Evidence: "https://example.test/context-fixture", LatencyNote: "Initial answer took 20 minutes; above the interactive budget."})
	}
	d.Layouts["qwen-32k"] = l
	return d, facts
}

func TestAutomaticContextChoosesLargestApplicableTestNotNativeLimitOrLatencyCap(t *testing.T) {
	d, facts := contextCatalog(t)
	before, _ := json.Marshal(d)
	s := catalog.Selection{Schema: catalog.SelectionSchema, Profile: "local-qwen"}
	resolved, err := catalog.ResolveMachineContexts(d, s, facts)
	if err != nil {
		t.Fatal(err)
	}
	if got := resolved.ContextWindows["qwen-32k"]; got != 65536 {
		t.Fatalf("automatic context = %d", got)
	}
	if s.ContextWindows != nil {
		t.Fatal("resolution changed the supplied selection")
	}
	locked, err := catalog.Compile(d, resolved, software.Target{OS: "darwin", Arch: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := catalog.MarshalLock(locked)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := catalog.ParseLock(raw)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := restored.Projections()
	if err != nil {
		t.Fatal(err)
	}
	if got := projection.Manifest.Layouts["qwen-32k"].Window; got != 65536 {
		t.Fatalf("runtime window = %d", got)
	}
	after, _ := json.Marshal(d)
	if string(before) != string(after) {
		t.Fatal("context selection mutated catalog evidence")
	}
}

func TestContextEvidenceDoesNotTransferAcrossMachinesOrExecutionChanges(t *testing.T) {
	for _, name := range []string{"chip", "larger RAM", "hardware model", "OS", "wired allowance", "template choice", "template bytes", "weights", "engine", "router", "output allowance", "kv", "mtp", "parallel", "cache", "batch"} {
		t.Run(name, func(t *testing.T) {
			d, facts := contextCatalog(t)
			l := d.Layouts["qwen-32k"]
			s := catalog.Selection{Schema: catalog.SelectionSchema, Profile: "local-qwen"}
			switch name {
			case "chip":
				facts.Chip = "Apple M4"
			case "larger RAM":
				facts.PhysicalMemoryBytes = 64 << 30
				facts.MetalDeviceMemoryMiB = 65536 * 81 / 100
			case "hardware model":
				facts.HardwareModel = "MacOther,1"
			case "OS":
				facts.Target.DistributionVersion = "27.0"
			case "wired allowance":
				facts.WiredLimitMiB = 23000
			case "template choice":
				s.Templates = map[string]string{"qwen-32k": ""}
			case "template bytes":
				p := d.Patches["template"]
				p.Files[0].SHA256 = strings.Repeat("f", 64)
				d.Patches["template"] = p
			case "weights":
				a := d.Artifacts[l.Artifact]
				a.Files[0].SHA256 = strings.Repeat("f", 64)
				d.Artifacts[l.Artifact] = a
			case "engine":
				e := d.Engines[l.Engine]
				e.Supply.Release.Version = "b10999"
				d.Engines[l.Engine] = e
			case "router":
				d.Runtime.Router.Release.Version = "v999"
			case "output allowance":
				l.RequestDefaults.MaxOutputTokens = 8192
			case "kv":
				l.EngineConfig.KVCache = "q4"
			case "mtp":
				l.Speculation = catalog.Speculation{Method: "none", Source: "none"}
			case "parallel":
				l.EngineConfig.Parallel = 2
			case "cache":
				l.EngineConfig.PromptCacheRAMMiB = 2048
			case "batch":
				l.EngineConfig.BatchTokens = 1024
			}
			d.Layouts["qwen-32k"] = l
			if _, err := catalog.ResolveMachineContexts(d, s, facts); err == nil || !strings.Contains(err.Error(), "tested context is unknown") {
				t.Fatalf("changed condition inherited a default: %v", err)
			}
			// Lack of a tested claim must not lock out an explicit experiment.
			s.ContextWindows = map[string]int{"qwen-32k": 98304}
			chosen, err := catalog.ResolveMachineContexts(d, s, facts)
			if err != nil || chosen.ContextWindows["qwen-32k"] != 98304 {
				t.Fatalf("explicit choice refused: %v", err)
			}
		})
	}
}

func TestMissingContextEvidenceStaysUnknownAndDescriptionEditsPreserveEvidence(t *testing.T) {
	d, facts := contextCatalog(t)
	l := d.Layouts["qwen-32k"]
	l.ContextFindings = nil
	d.Layouts["qwen-32k"] = l
	if _, err := catalog.ResolveMachineContexts(d, catalog.Selection{Schema: catalog.SelectionSchema, Profile: "local-qwen"}, facts); err == nil {
		t.Fatal("no findings silently used the authored window or native maximum")
	}
	d, facts = contextCatalog(t)
	baseline := compile(t, d)
	url := "https://example.test/my-assessment"
	edited, err := catalog.Describe(d, "qwen-q4", "My preferred document model; check quotations.", &url, false)
	if err != nil {
		t.Fatal(err)
	}
	next := compile(t, edited)
	if next.Digests.Profile != baseline.Digests.Profile || next.SourceSnapshotSHA256 == baseline.SourceSnapshotSHA256 {
		t.Fatal("editorial change altered execution identity or failed to alter catalog identity")
	}
	if d.Artifacts["qwen-q4"].Description != "" {
		t.Fatal("description edit mutated its input")
	}
	if !reflect.DeepEqual(d.Layouts, edited.Layouts) {
		t.Fatal("description edit changed context evidence")
	}
	suggestion := "Automatically refreshed wording"
	preserved, err := catalog.Describe(edited, "qwen-q4", suggestion, nil, true)
	if err != nil || preserved.Artifacts["qwen-q4"].Description != edited.Artifacts["qwen-q4"].Description {
		t.Fatal("suggestion replaced custom copy")
	}
	s, err := catalog.ResolveMachineContexts(preserved, catalog.Selection{Schema: catalog.SelectionSchema, Profile: "local-qwen"}, facts)
	if err != nil || s.ContextWindows["qwen-32k"] != 65536 {
		t.Fatalf("description invalidated context finding: %v", err)
	}
}

func TestContextFindingValidationRejectsMalformedEvidenceAndBudgets(t *testing.T) {
	for _, name := range []string{"identity", "output", "ceiling", "RAM", "wired", "engine memory", "swap", "evidence", "escape"} {
		t.Run(name, func(t *testing.T) {
			d, _ := contextCatalog(t)
			l := d.Layouts["qwen-32k"]
			f := &l.ContextFindings[0]
			switch name {
			case "identity":
				f.ExecutionSHA256 = "bad"
			case "output":
				f.MaxOutputTokens = f.WindowTokens
			case "ceiling":
				f.WindowTokens = 262145
			case "RAM":
				f.Machine.PhysicalMemoryBytes = 0
			case "wired":
				f.Machine.MinimumWiredLimitMiB = 999999
			case "engine memory":
				f.EngineMemoryLimitBytes = 64 << 30
			case "swap":
				f.SwapGrowthLimitBytes = -1
			case "evidence":
				f.Evidence = "file:///private/raw.json"
			case "escape":
				f.LatencyNote = "\x1b[31m"
			}
			d.Layouts["qwen-32k"] = l
			if err := d.Validate(); err == nil {
				t.Fatal("invalid context finding accepted")
			}
		})
	}
}
