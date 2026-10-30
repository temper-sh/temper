package presetcmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/presetcmd"
	"github.com/temper-sh/temper/internal/setup"
	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/testfixture"
	"github.com/temper-sh/temper/internal/userpreset"
)

const mtp = "qwen3.8-27b-q4xl-mtp"

func command(t *testing.T) (presetcmd.Command, string) {
	t.Helper()
	d := testfixture.MixedEngineCatalog(t)
	root := filepath.Join(t.TempDir(), "root")
	return presetcmd.Command{
		Home:    func() (string, error) { return t.TempDir(), nil },
		Catalog: func(string) (catalog.View, error) { return catalog.View{Document: d}, nil },
		Facts: func(context.Context) (machine.Facts, error) {
			return machine.Facts{Schema: machine.FactsSchemaV1, Target: software.Target{OS: "darwin", Arch: "arm64", Distribution: "macos", DistributionVersion: "26.6"},
				HardwareModel: "MacTest,1", Chip: "Apple M5", OSBuild: "25G1", PhysicalMemoryBytes: 32 << 30,
				MetalDeviceMemoryMiB: 21 << 10, MetalDeviceMemorySource: machine.MetalDeviceSourceLive, WiredLimitMiB: 21 << 10, WiredLimitSource: budget.WiredSourceMetal}, nil
		},
	}, root
}

func run(t *testing.T, c presetcmd.Command, root string, args ...string) (string, string, int) {
	t.Helper()
	var out, diagnostics bytes.Buffer
	code := c.Run(context.Background(), append(args, "--root", root), &out, &diagnostics)
	return out.String(), diagnostics.String(), code
}

func TestForkThenEditThenCheckReportsTheChangesAndTheFit(t *testing.T) {
	c, root := command(t)
	if out, diagnostics, code := run(t, c, root, "fork", mtp, "qwen-cool", "--name", "Qwen, cooler"); code != 0 || !strings.Contains(out, "follows catalog updates") {
		t.Fatalf("fork: %d %s %s", code, out, diagnostics)
	}
	raw, err := os.ReadFile(userpreset.Path(root, "qwen-cool"))
	if err != nil || !strings.Contains(string(raw), `"origin": "`+mtp+`"`) {
		t.Fatalf("fork file: %s %v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(root, "temper-root.json")); err != nil {
		t.Fatal("the root has no marker after the first write")
	}
	edited := strings.Replace(string(raw), `"name": "Qwen, cooler"`, `"name": "Qwen, cooler", "changes": {"request_defaults": {"sampling": {"temperature": 0.6}}, "engine_config": {"batch_tokens": 1024}}`, 1)
	if err := os.WriteFile(userpreset.Path(root, "qwen-cool"), []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	out, diagnostics, code := run(t, c, root, "check", "qwen-cool")
	for _, want := range []string{"qwen-cool: fork of " + mtp, "request_defaults.sampling.temperature 1 → 0.6", "engine_config.batch_tokens", "This Mac: runtime fit unknown", "KV/context cache: unknown", "not tested by the catalog"} {
		if code != 0 || !strings.Contains(out, want) {
			t.Fatalf("check is missing %q: %d %s %s", want, code, out, diagnostics)
		}
	}
	out, _, code = run(t, c, root, "check", "--json")
	var report struct {
		Presets []struct {
			ID      string
			Valid   bool
			Fits    *bool
			Changes []catalog.Change
		}
	}
	if code != 0 || json.Unmarshal([]byte(out), &report) != nil || len(report.Presets) != 1 || !report.Presets[0].Valid || report.Presets[0].Fits != nil || len(report.Presets[0].Changes) != 2 {
		t.Fatalf("check --json: %s", out)
	}
	// An invalid file fails the check and names the reason; others still pass.
	if err := os.WriteFile(userpreset.Path(root, "broken"), []byte(`{"schema": "temper-preset/v1", "origin": "`+mtp+`", "changes": {"engine": "splash"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _, code = run(t, c, root, "check")
	if code != 1 || !strings.Contains(out, "broken: invalid: changes.engine is not a setting") || !strings.Contains(out, "qwen-cool: fork") {
		t.Fatalf("check of an invalid file: %d %s", code, out)
	}
}

func TestCreationRefusesIDsThatAreTakenOrInvalid(t *testing.T) {
	c, root := command(t)
	if _, _, code := run(t, c, root, "fork", mtp, "mine"); code != 0 {
		t.Fatal("first fork failed")
	}
	for name, test := range map[string]struct {
		args []string
		want string
	}{
		"existing file":   {[]string{"fork", mtp, "mine"}, "already exists"},
		"catalog ID":      {[]string{"fork", mtp, "gemma-4-e2b-q4km-off"}, "the catalog has a preset"},
		"invalid ID":      {[]string{"copy", mtp, "Mine"}, "is not a preset ID"},
		"fork of yours":   {[]string{"fork", "mine", "mine-two"}, "forks start from catalog presets"},
		"unknown origin":  {[]string{"fork", "qwen3.8-27b-q4xl", "mine-two"}, "did you mean " + mtp},
		"too few":         {[]string{"fork", mtp}, ""},
		"unknown setting": {[]string{"copy", "nothing-here", "mine-two"}, "is not in the catalog"},
	} {
		t.Run(name, func(t *testing.T) {
			out, diagnostics, code := run(t, c, root, test.args...)
			if code == 0 || !strings.Contains(diagnostics, test.want) {
				t.Fatalf("got %d %s %s, want %q", code, out, diagnostics, test.want)
			}
		})
	}
}

func TestCopyFreezesAForkAndKeepsItsRecords(t *testing.T) {
	c, root := command(t)
	if err := os.MkdirAll(userpreset.Dir(root), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userpreset.Path(root, "qwen-cool"), []byte(`{"schema": "temper-preset/v1", "origin": "`+mtp+`", "changes": {"request_defaults": {"sampling": {"temperature": 0.6}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, diagnostics, code := run(t, c, root, "copy", "qwen-cool", "qwen-frozen"); code != 0 || !strings.Contains(out, "profile for 32 GiB Macs") {
		t.Fatalf("copy: %d %s %s", code, out, diagnostics)
	}
	raw, err := os.ReadFile(userpreset.Path(root, "qwen-frozen"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := catalog.ParseUserPreset("qwen-frozen", raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Fork() || *u.Preset.RequestDefaults.Sampling.Temperature != 0.6 || len(u.Preset.Profiles) != 1 || u.Artifacts["qwen3.8-27b-q4xl"].Files == nil || u.Templates["froggeric-qwen38"].Files == nil {
		t.Fatalf("copy lost a value or a record: %s", raw)
	}
	// The copy holds the catalog's records unchanged, so nothing shadows a
	// different catalog record, and it keeps the fork's name.
	if out, _, code := run(t, c, root, "check", "qwen-frozen"); code != 0 || !strings.Contains(out, "qwen-frozen: copy") || strings.Contains(out, "Warning") || strings.Contains(out, "(yours) (yours)") {
		t.Fatalf("copy does not check cleanly: %s", out)
	}
}

func TestFieldsListTheSettingsOfTheOriginsEngineWithValues(t *testing.T) {
	c, root := command(t)
	out, _, code := run(t, c, root, "fields", mtp, "--json")
	var report struct {
		Kind   string `json:"engine_config_kind"`
		Fields []struct {
			Path  string
			Value any
		}
	}
	if code != 0 || json.Unmarshal([]byte(out), &report) != nil || report.Kind != "llama-server/v1" {
		t.Fatalf("fields: %s", out)
	}
	values := map[string]any{}
	for _, f := range report.Fields {
		values[f.Path] = f.Value
	}
	if values["engine_config.kv_cache"] == nil || values["request_defaults.sampling.temperature"] == nil || len(values) != len(catalog.Fields("llama-server/v1")) {
		t.Fatalf("fields lack values: %v", values)
	}
	if out, _, code := run(t, c, root, "fields", "qwen3.8-27b-q4xl-splash"); code != 0 || !strings.Contains(out, "engine_config.max_memory_bytes") || strings.Contains(out, "engine_config.batch_tokens") {
		t.Fatalf("fields of another engine: %s", out)
	}
}

func TestPlanReviewsTheCompiledPresetAndSavesNothing(t *testing.T) {
	c, root := command(t)
	if _, diagnostics, code := run(t, c, root, "fork", mtp, "qwen-128k"); code != 0 {
		t.Fatalf("fork: %s", diagnostics)
	}
	raw, err := os.ReadFile(userpreset.Path(root, "qwen-128k"))
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(raw), `"origin"`, `"changes": {"context_window_tokens": 131072}, "origin"`, 1)
	if err := os.WriteFile(userpreset.Path(root, "qwen-128k"), []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	var reviewed setup.Configuration
	c.Plan = func(_ context.Context, planRoot string, next setup.Configuration) (setup.Plan, error) {
		if planRoot != root {
			t.Fatalf("planned root %s, want %s", planRoot, root)
		}
		reviewed = next
		return setup.Plan{Root: planRoot, Configuration: &next, DownloadBytes: 42, CanPrepare: true,
			Presets: []setup.PresetPlan{{Preset: "qwen-128k", Lock: next.Presets["qwen-128k"].Lock, ModelBytes: 40}}}, nil
	}
	out, diagnostics, code := run(t, c, root, "plan", "qwen-128k", "--json")
	var document struct {
		Schema, Preset string
		Plan           struct {
			CanPrepare    bool  `json:"can_prepare"`
			DownloadBytes int64 `json:"download_bytes"`
			Presets       map[string]struct {
				ModelBytes int64 `json:"model_bytes"`
			}
		}
	}
	if code != 0 || json.Unmarshal([]byte(out), &document) != nil {
		t.Fatalf("plan --json: %d %s %s", code, out, diagnostics)
	}
	if document.Schema != "temper-preset-plan/v1" || document.Preset != "qwen-128k" || !document.Plan.CanPrepare ||
		document.Plan.DownloadBytes != 42 || document.Plan.Presets["qwen-128k"].ModelBytes != 40 {
		t.Fatalf("plan --json: %s", out)
	}
	if len(reviewed.Presets) != 1 || reviewed.Presets["qwen-128k"].Lock.Records.Presets["qwen-128k"].Window() != 131072 {
		t.Fatalf("the plan did not review the compiled fork: %+v", reviewed.Presets)
	}
	if len(reviewed.Layouts) != 0 {
		t.Fatalf("the plan reviewed layouts: %+v", reviewed.Layouts)
	}
	out, _, code = run(t, c, root, "plan", "qwen-128k")
	if code != 0 || strings.Contains(out, "Configuration: ") || !strings.Contains(out, "Plan only: nothing was saved or downloaded") {
		t.Fatalf("plan: %s", out)
	}
	if _, err := os.Stat(filepath.Join(root, "configuration.json")); err == nil {
		t.Fatal("plan saved a configuration")
	}
	if _, diagnostics, code := run(t, c, root, "plan", "missing"); code == 0 || !strings.Contains(diagnostics, "there is no") {
		t.Fatalf("plan of a missing preset: %d %s", code, diagnostics)
	}
}

func TestCheckPreservesUnavailableOriginReason(t *testing.T) {
	c, root := command(t)
	if _, d, code := run(t, c, root, "fork", mtp, "mine"); code != 0 {
		t.Fatal(d)
	}
	view, _ := c.Catalog(root)
	delete(view.Document.Presets, mtp)
	view.Unavailable = []catalog.Unavailable{{Kind: "preset", ID: mtp, Reason: "engine requires a newer Temper"}}
	c.Catalog = func(string) (catalog.View, error) { return view, nil }
	out, diagnostics, code := run(t, c, root, "check", "mine")
	if code == 0 || !strings.Contains(out+diagnostics, "engine requires a newer Temper") {
		t.Fatal("check lost the catalog refusal", code, out, diagnostics)
	}
}
