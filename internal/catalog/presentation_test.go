package catalog_test

import "testing"

func TestPresentationChangesPreserveExecutionIdentity(t *testing.T) {
	d := document()
	before := compile(t, d)
	a := d.Artifacts["qwen-q4"]
	a.ModelName, a.WeightsName = "Qwen", "Unsloth Q4 GGUF"
	d.Artifacts["qwen-q4"] = a
	e := d.Engines["llama-b10936"]
	e.DisplayName = "llama.cpp"
	d.Engines["llama-b10936"] = e
	l := d.Presets["qwen-32k"]
	l.MemoryTier = "S"
	d.Presets["qwen-32k"] = l
	d.PresetOrder = []string{"qwen-32k"}

	after := compile(t, d)
	if before.ExecutionDigest != after.ExecutionDigest {
		t.Fatal("editorial changes altered the selected execution identity")
	}
	if before.SourceSnapshotSHA256 == after.SourceSnapshotSHA256 {
		t.Fatal("catalog source identity did not change")
	}
	if len(after.Records.PresetOrder) != 0 {
		t.Fatal("execution lock retained unrelated catalog navigation")
	}
}

func TestCatalogRefusesBrokenNavigation(t *testing.T) {
	for _, tc := range []struct {
		name, tier string
		order      []string
	}{
		{name: "unknown layout", order: []string{"missing"}},
		{name: "repeated layout", order: []string{"qwen-32k", "qwen-32k"}},
		{name: "unknown tier", tier: "small"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := document()
			d.PresetOrder = tc.order
			l := d.Presets["qwen-32k"]
			l.MemoryTier = tc.tier
			d.Presets["qwen-32k"] = l
			if err := d.Validate(); err == nil {
				t.Fatal("invalid catalog navigation was accepted")
			}
		})
	}
}
