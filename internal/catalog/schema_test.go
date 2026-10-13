package catalog_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
)

func TestCurrentContractsRejectRetiredSchemasAndFields(t *testing.T) {
	source, err := json.Marshal(document())
	if err != nil {
		t.Fatal(err)
	}
	for _, schema := range []string{"temper-catalog/v2", "temper-catalog/v3", "temper-software-supply/v1"} {
		raw := bytes.Replace(source, []byte(catalog.Schema), []byte(schema), 1)
		if _, err := catalog.Parse(raw); err == nil {
			t.Fatalf("accepted %s", schema)
		}
	}
	for _, field := range []string{"layouts", "profiles", "layout_order"} {
		raw := bytes.Replace(source, []byte(`"date":`), []byte(`"`+field+`": {}, "date":`), 1)
		if _, err := catalog.Parse(raw); err == nil {
			t.Fatalf("accepted retired field %s", field)
		}
	}
	locked := compile(t, document())
	raw, err := catalog.MarshalLock(locked)
	if err != nil {
		t.Fatal(err)
	}
	for _, schema := range []string{"temper-execution-lock/v2", "temper-execution-lock/v3"} {
		if _, err := catalog.ParseLock(bytes.Replace(raw, []byte(catalog.LockSchema), []byte(schema), 1)); err == nil {
			t.Fatalf("accepted %s", schema)
		}
	}
	for _, field := range []string{"selection", "digests"} {
		changed := bytes.Replace(raw, []byte(`"preset":`), []byte(`"`+field+`": {}, "preset":`), 1)
		if _, err := catalog.ParseLock(changed); err == nil {
			t.Fatalf("accepted retired lock field %s", field)
		}
	}
	duplicate := bytes.Replace(source, []byte(`"date":`), []byte(`"schema":"`+catalog.Schema+`","date":`), 1)
	if _, err := catalog.Parse(duplicate); err == nil {
		t.Fatal("accepted duplicate key")
	}
	for _, unwanted := range []string{`"layouts"`, `"selection"`, `"digests"`, `"execution_digest"`} {
		if strings.Contains(string(raw), unwanted) {
			t.Fatalf("native lock contains %s", unwanted)
		}
	}
}

func TestCompiledTemplateIsIndependentOfLaterCatalogDefaults(t *testing.T) {
	d := document()
	original := compile(t, d)
	alternative := d.Templates["template"]
	alternative.Files = []catalog.File{{Path: "alternative.jinja", Bytes: 14, SHA256: strings.Repeat("f", 64)}}
	d.Templates["alternative"] = alternative
	p := d.Presets["qwen-32k"]
	p.Template = "alternative"
	d.Presets["qwen-32k"] = p
	next := compile(t, d)
	if catalog.SameExecution(next, original) {
		t.Fatal("template change did not change identity")
	}
	if original.Records.Presets["qwen-32k"].Template != "template" {
		t.Fatal("catalog edit changed frozen template")
	}
	if err := original.Validate(); err != nil {
		t.Fatal(err)
	}
	p.Template = "missing"
	d.Presets["qwen-32k"] = p
	if _, err := catalog.CompilePreset(d, "qwen-32k", 0, 0, original.Target); err == nil {
		t.Fatal("unknown template accepted")
	}
}
