package hfcache

import (
	"reflect"
	"strings"
	"testing"
)

func TestSharedWeightRemovalUsesAnExplicitFileAndCacheWithoutAShell(t *testing.T) {
	entry := Entry{Repo: "owner/model", Revision: strings.Repeat("a", 40), Name: "weights/model.gguf", SHA256: strings.Repeat("b", 64)}
	path, args, err := removeCommand("/cache with spaces", entry, func(s string) (string, error) {
		if s != "hf" {
			t.Fatal(s)
		}
		return "/bin/hf", nil
	})
	if err != nil || path != "/bin/hf" || !reflect.DeepEqual(args, []string{"cache", "rm", "hf://models/owner/model/weights/model.gguf", "--cache-dir", "/cache with spaces", "--yes"}) {
		t.Fatal(path, args, err)
	}
	for _, pair := range [][2]string{{"relative", "owner/model"}, {"/cache", "--all"}, {"/cache", "owner/model; rm -rf /"}, {"/cache", "owner/a/b"}} {
		bad := entry
		bad.Repo = pair[1]
		if _, _, err := removeCommand(pair[0], bad, func(string) (string, error) { t.Fatal("looked up a client for invalid deletion"); return "", nil }); err == nil {
			t.Fatal(pair)
		}
	}
	for _, name := range []string{"../escape", "/absolute", "."} {
		bad := entry
		bad.Name = name
		if _, _, err := removeCommand("/cache", bad, func(string) (string, error) { t.Fatal("invalid file reached client lookup"); return "", nil }); err == nil {
			t.Fatal(name)
		}
	}
}
