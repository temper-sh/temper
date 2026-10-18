package catalogcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/userpreset"
)

func TestCompileFindsUserPresetsInTheRootAndTheirFileWins(t *testing.T) {
	root, _ := publishedRoot(t)
	write := func(id, text string) {
		t.Helper()
		if err := os.MkdirAll(userpreset.Dir(root), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(userpreset.Path(root, id), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	compileUser := func(id string, jsonOutput bool) (catalog.Lock, string, string, int) {
		t.Helper()
		lockPath := filepath.Join(t.TempDir(), "lock.json")
		args := []string{"--root", root, "--preset", id, "--target", "darwin/arm64", "--out", lockPath}
		if jsonOutput {
			args = append(args, "--json")
		}
		var out, diagnostics bytes.Buffer
		code := compile(context.Background(), args, &out, &diagnostics, nil, publishedTrust(t))
		if code != 0 {
			return catalog.Lock{}, out.String(), diagnostics.String(), code
		}
		raw, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatal(err)
		}
		l, err := catalog.ParseLock(raw)
		if err != nil {
			t.Fatal(err)
		}
		return l, out.String(), diagnostics.String(), code
	}
	write("qwen-cool", `{"schema": "temper-preset/v1", "origin": "qwen3.8-27b-q4xl-mtp",
	  "changes": {"request_defaults": {"sampling": {"temperature": 0.6}}}}`)
	l, out, _, code := compileUser("qwen-cool", true)
	var report map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &report) != nil || report["user_preset"] != true {
		t.Fatalf("user preset did not compile from the root: %d %s", code, out)
	}
	if l.Preset != "qwen-cool" || l.Records.Presets["qwen-cool"].RequestDefaults.Sampling.Temperature != 0.6 {
		t.Fatal("the lock lost the user preset or its change")
	}
	// A file with a catalog preset's ID wins, with a warning.
	write("gemma-4-e2b-q4km-off", `{"schema": "temper-preset/v1", "origin": "gemma-4-e4b-q4km-off"}`)
	l, _, diagnostics, code := compileUser("gemma-4-e2b-q4km-off", false)
	if code != 0 || l.Records.Presets["gemma-4-e2b-q4km-off"].Artifact != "gemma-4-e4b-q4km" || !strings.Contains(diagnostics, "your file is used") {
		t.Fatalf("the user file did not win with a warning: %d %s", code, diagnostics)
	}
	write("broken", `{"schema": "temper-preset/v1", "origin": "qwen3.8-27b-q4xl-mtp", "changes": {"engine": "splash"}}`)
	if _, _, diagnostics, code := compileUser("broken", false); code == 0 || !strings.Contains(diagnostics, "broken.json") || !strings.Contains(diagnostics, "changes.engine") {
		t.Fatalf("an invalid user preset was not refused with its file and reason: %s", diagnostics)
	}
}
