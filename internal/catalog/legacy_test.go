package catalog_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
)

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
