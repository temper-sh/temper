package catalog_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/software"
)

func TestVersionPolicySeparatesRequiredFromTestedAndScopesItToLayout(t *testing.T) {
	d := document()
	l := d.Layouts["qwen-32k"]
	l.EngineVersions = &catalog.Versions{MinimumRequired: "b10900", RequiredSource: "upstream first containing release", MinimumTested: "b11000", TestedEvidence: "fixture study conditions"}
	d.Layouts["qwen-32k"] = l
	// The selected release is above required but below tested. Untested is allowed.
	locked := compile(t, d)
	if locked.Digests.Profile != compile(t, document()).Digests.Profile {
		t.Fatal("evidence metadata changed execution identity")
	}
	l.EngineVersions.MinimumRequired = "b10937"
	if _, err := catalog.Compile(d, catalog.Selection{Schema: catalog.SelectionSchema, Profile: "local-qwen"}, software.Target{OS: "darwin", Arch: "arm64"}); err == nil {
		t.Fatal("accepted engine below a selected layout's required floor")
	}
	// An unselected layout cannot impose its floor on the chosen profile.
	d.Layouts["future-model"] = l
	l.EngineVersions = nil
	d.Layouts["qwen-32k"] = l
	compile(t, d)
}

func TestTestedFallbackIsExplicitAndMustSatisfyCurrentRequirements(t *testing.T) {
	d := document()
	s := catalog.Selection{Schema: catalog.SelectionSchema, Profile: "local-qwen"}
	if err := catalog.ValidateTestedSoftware(d, s); err == nil || !strings.Contains(err.Error(), "llama-swap") {
		t.Fatalf("missing router tested boundary = %v", err)
	}
	if _, err := catalog.ResolveSoftware(context.Background(), d, s, "tested", nil); err == nil || !strings.Contains(err.Error(), "no tested version") {
		t.Fatalf("unknown tested boundary = %v", err)
	}
	d.Runtime.Router.Versions = &catalog.Versions{MinimumTested: "b10936", TestedEvidence: "router fixture smoke"}
	if err := catalog.ValidateTestedSoftware(d, s); err == nil || !strings.Contains(err.Error(), "llama-cpp") {
		t.Fatalf("missing layout tested boundary = %v", err)
	}
	l := d.Layouts["qwen-32k"]
	l.EngineVersions = &catalog.Versions{MinimumRequired: "b10900", RequiredSource: "upstream feature", MinimumTested: "b10936", TestedEvidence: "study fixture"}
	d.Layouts["qwen-32k"] = l
	if err := catalog.ValidateTestedSoftware(d, s); err != nil {
		t.Fatalf("complete retained tested boundary: %v", err)
	}
	resolved, err := catalog.ResolveSoftware(context.Background(), d, s, "tested", nil)
	if err != nil {
		t.Fatal(err)
	} // Exact retained inputs need no network.
	compile(t, resolved)
	l.EngineVersions.MinimumRequired = "b10937"
	if err := catalog.ValidateTestedSoftware(d, s); err == nil || !strings.Contains(err.Error(), "minimum required") {
		t.Fatalf("tested below required = %v", err)
	}
	if _, err := catalog.ResolveSoftware(context.Background(), d, s, "tested", nil); err == nil || !strings.Contains(err.Error(), "minimum required") {
		t.Fatalf("incompatible fallback = %v", err)
	}
}

func TestTestedSoftwareRequiresCommonEvidenceForSelectedEngineLayouts(t *testing.T) {
	d := document()
	d.Runtime.Router.Versions = &catalog.Versions{MinimumTested: "b10936", TestedEvidence: "router fixture smoke"}
	first := d.Layouts["qwen-32k"]
	first.EngineVersions = &catalog.Versions{MinimumRequired: "b10900", RequiredSource: "upstream feature", MinimumTested: "b10936", TestedEvidence: "first layout study"}
	d.Layouts["qwen-32k"] = first
	second := first
	second.DisplayName = "Second selected layout"
	second.EngineVersions = &catalog.Versions{MinimumRequired: "b10910", RequiredSource: "another upstream feature", MinimumTested: "b10936", TestedEvidence: "second layout study"}
	d.Layouts["qwen-second"] = second
	p := d.Profiles["local-qwen"]
	p.Bindings = append(p.Bindings, catalog.Binding{Layout: "qwen-second", Route: "available", Residency: "on-demand", IdleTTLSeconds: 600})
	d.Profiles["local-qwen"] = p
	s := catalog.Selection{Schema: catalog.SelectionSchema, Profile: "local-qwen"}
	if err := catalog.ValidateTestedSoftware(d, s); err != nil {
		t.Fatalf("common tested engine release refused: %v", err)
	}
	if _, err := catalog.ResolveSoftware(context.Background(), d, s, "tested", nil); err != nil {
		t.Fatalf("resolver disagreed on retained common tested release: %v", err)
	}
	second.EngineVersions.MinimumTested = "b10937"
	d.Layouts["qwen-second"] = second
	if err := catalog.ValidateTestedSoftware(d, s); err == nil || !strings.Contains(err.Error(), "different tested versions") {
		t.Fatalf("conflicting engine evidence = %v", err)
	}
	if _, err := catalog.ResolveSoftware(context.Background(), d, s, "tested", nil); err == nil || !strings.Contains(err.Error(), "different tested versions") {
		t.Fatalf("resolver accepted conflicting engine evidence: %v", err)
	}
	second.EngineVersions.MinimumTested = "b10936"
	second.EngineVersions.MinimumRequired = "b10937"
	d.Layouts["qwen-second"] = second
	if err := catalog.ValidateTestedSoftware(d, s); err == nil || !strings.Contains(err.Error(), "minimum required") {
		t.Fatalf("common tested engine release below one layout floor = %v", err)
	}
}

func TestV2LockOmitsInstallerAuthoringAndRedundantIdentities(t *testing.T) {
	l := compile(t, document())
	raw, err := catalog.MarshalLock(l)
	if err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{`"tools"`, `"integrations"`, `"id":`, `"units":`, `"recipe_revision"`, `"experiment"`, `"materials"`} {
		if strings.Contains(string(raw), unwanted) {
			t.Errorf("v2 lock contains %s", unwanted)
		}
	}
	var decoded map[string]json.RawMessage
	_ = json.Unmarshal(raw, &decoded)
	var digests map[string]string
	_ = json.Unmarshal(decoded["digests"], &digests)
	if len(digests) != 1 || digests["profile"] == "" {
		t.Fatalf("redundant hashes: %v", digests)
	}
	p, err := l.Projections()
	if err != nil {
		t.Fatal(err)
	}
	if p.Software.Provenance.Experiment != nil || p.Software.Provenance.Execution == nil {
		t.Fatal("catalog installation fabricated experiment provenance")
	}
	if p.Software.Resolved != "" {
		t.Fatal("catalog date was presented as a resolution observation")
	}
}
