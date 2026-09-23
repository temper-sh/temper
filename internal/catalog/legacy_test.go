package catalog_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/software"
)

// Captured from the pre-template-choice v2 compiler. An omitted templates map
// must keep both portable execution identity and serialized lock bytes.
func TestExistingV2SelectionKeepsExactQwenLockBytes(t *testing.T) {
	catalogBytes, err := os.ReadFile("../../catalog/qwen38-m5-refresh.json")
	if err != nil {
		t.Fatal(err)
	}
	selectionBytes, err := os.ReadFile("../../catalog/qwen38-m5-refresh.selection.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := catalog.Parse(catalogBytes)
	if err != nil {
		t.Fatal(err)
	}
	s, err := catalog.ParseSelection(selectionBytes)
	if err != nil {
		t.Fatal(err)
	}
	if s.Templates != nil {
		t.Fatal("compatibility fixture unexpectedly contains template choices")
	}
	l, err := catalog.Compile(d, s, software.Target{OS: "darwin", Arch: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	if l.Digests.Profile != "40277487aef35c16a3d078ec58976d778be6389963ba42ffd11ebed5ec155b1e" {
		t.Fatal("existing v2 execution identity changed")
	}
	raw, err := catalog.MarshalLock(l)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(raw)); got != "5a11b208f9b1b52e1a1abb498a3b1411f53f0d9a2db8127f4d50fd8b1d128f4a" {
		t.Fatalf("existing v2 lock bytes changed: %s", got)
	}
}

// This is the issued Qwen study lock, not a newly generated golden snapshot.
// Its four hashes were measured using the pre-cleanup compiler. Field Kit
// checks these exact bytes and compares records/selection after configuration.
func TestIssuedFieldKitV1LockKeepsExecutionIdentityAndExports(t *testing.T) {
	raw, err := os.ReadFile("testdata/field-kit-v1.lock.json")
	if err != nil {
		t.Fatal(err)
	}
	l, err := catalog.ParseLock(raw)
	if err != nil {
		t.Fatal(err)
	}
	if l.Digests.Profile != "b1d0ad76f0bfeb16207f3640382d0eabfb7776185636273505ada2919d3f81cb" {
		t.Fatal("issued execution identity changed")
	}
	p, err := l.Projections()
	if err != nil {
		t.Fatal(err)
	}
	files, err := p.Files()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"manifest.lock.yaml":    "b1696195ab78101f02a84dd3c8ad26e9b49393b740510d1e063efb5c86f034fa",
		"manifest.yaml":         "0ceb57e35b11e1d0912f7cbec9e8b073f3d07d5941434a1f193ce5aa1abf7015",
		"request-defaults.json": "8ed7013ed2e44dbbad057c0c645bc765362d8daec8e2b6546b4042ee5728918c",
		"software.lock.yaml":    "401119db8d48cae8f8e24b85928709108c759d2776f45ba7f065d1c6e863f8af",
	}
	for name, hash := range want {
		if fmt.Sprintf("%x", sha256.Sum256(files[name])) != hash {
			t.Errorf("issued %s changed", name)
		}
	}
	for id, layout := range l.Records.Layouts {
		layout.EngineConfig.Controls.Threads = 6
		l.Records.Layouts[id] = layout
	}
	recompiled, err := catalog.Compile(l.Records, l.Selection, l.Target)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(recompiled.Records, l.Records) || !reflect.DeepEqual(recompiled.Selection, l.Selection) {
		t.Fatal("Field Kit configure comparison changed")
	}
}
