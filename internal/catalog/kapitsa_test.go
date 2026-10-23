package catalog_test

import (
	"os"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/render"
	"github.com/temper-sh/temper/internal/testfixture"
)

func TestKapitsaCatalogCompilesAndGatesOlderClients(t *testing.T) {
	d := testfixture.KapitsaCatalog(t)
	id := "qwen3.8-27b-mlx4-kapitsa"
	l, err := catalog.CompilePreset(d, id, 32, 0, d.Runtime.Router.Target)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := catalog.MarshalLock(l)
	if err != nil {
		t.Fatal(err)
	}
	l, err = catalog.ParseLock(raw)
	if err != nil {
		t.Fatal(err)
	}
	p, err := l.Projections()
	if err != nil {
		t.Fatal(err)
	}
	b, err := render.Build(render.Inputs{Root: "/temper", Mode: id, Manifest: p.Manifest, Lock: p.Artifacts, StartPort: 10001})
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for _, f := range b.Artifacts {
		text += string(f.Data)
	}
	for _, want := range []string{"kapitsa serve", "25.769803776", "98304", "1024", "\"reasoning_effort?\": \"none\"", "KAPITSA_HOME", "NI_ANE"} {
		if !strings.Contains(text, want) {
			t.Fatal("missing", want)
		}
	}
	if strings.Contains(text, "frog") || strings.Contains(text, "allow-switch") {
		t.Fatal("implicit template or model switching")
	}
	changed, err := catalog.ConfigureExecution(l, id, catalog.ExecutionSettings{ContextWindowTokens: 8192, MaxMemoryBytes: 20 << 30})
	if err != nil {
		t.Fatal(err)
	}
	if catalog.SameExecution(l, changed) || l.Records.Presets[id].Config().Kapitsa.MaxMemoryBytes != 24<<30 {
		t.Fatal("configuration mutated source or lost identity")
	}
	raw, err = os.ReadFile("../testfixture/testdata/kapitsa-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	view, err := catalog.Read(raw, "0.3.2")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Document.Presets) != 0 || len(view.Unavailable) == 0 {
		t.Fatal("old client offered unsupported backend")
	}
	view, err = catalog.Read(raw, "0.4.0")
	if err != nil || len(view.Document.Presets) != 1 {
		t.Fatal(view.Unavailable, err)
	}
}
