package preset_test

import (
	"os"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/preset"
)

func TestExactSoftwareClosuresShareOnlyIdenticalMaterial(t *testing.T) {
	raw, err := os.ReadFile("../../catalog/guided-setup.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	installations := map[string]map[string]string{}
	for _, id := range []string{"qwen3.8-27b-q4xl-mtp", "qwen3.8-27b-q4xl-splash", "gemma-4-31b-qat-ud-q4-k-xl-llama", "muse-glimmer-30b-q4xl-llama"} {
		lock, err := catalog.CompilePreset(d, id, "", 0, d.Runtime.Router.Target)
		if err != nil {
			t.Fatal(err)
		}
		sets, err := preset.Software(lock)
		if err != nil {
			t.Fatal(err)
		}
		installations[id] = map[string]string{}
		for _, set := range sets {
			installations[id][set.Package] = set.ID
		}
	}
	q := installations["qwen3.8-27b-q4xl-mtp"]
	s := installations["qwen3.8-27b-q4xl-splash"]
	g := installations["gemma-4-31b-qat-ud-q4-k-xl-llama"]
	m := installations["muse-glimmer-30b-q4xl-llama"]
	if q["llama-swap"] != s["llama-swap"] || q["llama-swap"] != g["llama-swap"] {
		t.Fatal("shared router was installed per preset")
	}
	if q["llama-cpp"] == g["llama-cpp"] {
		t.Fatal("different llama.cpp versions collapsed")
	}
	if g["llama-cpp"] != m["llama-cpp"] {
		t.Fatal("identical engine not reused")
	}
}
