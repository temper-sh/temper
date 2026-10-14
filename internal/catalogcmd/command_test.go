package catalogcmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/temper-sh/temper/internal/catalog"
	"strings"

	"os"
	"path/filepath"
	"testing"

	"github.com/temper-sh/temper/internal/catalogcmd"
)

func invoke(t *testing.T, args ...string) string {
	t.Helper()
	var out, err bytes.Buffer
	if code := catalogcmd.Run(context.Background(), args, &out, &err); code != 0 {
		t.Fatalf("command exited %d: %s", code, err.String())
	}
	return out.String()
}

func compileArgs(out string) []string {
	return []string{"catalog", "compile", "--catalog", "../testfixture/testdata/catalog.json", "--preset", "qwen3.8-27b-q4xl-mtp", "--target", "darwin/arm64", "--out", out}
}

func TestDryRunHasNoFilesystemEffects(t *testing.T) {
	root := t.TempDir()
	lock := filepath.Join(root, "execution.lock.json")
	invoke(t, append(compileArgs(lock), "--dry-run")...)
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("dry compile wrote files: %v, %v", entries, err)
	}

}

func TestRefusesOutputSymlinkAndCancelledWrite(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "user-file")
	lock := filepath.Join(root, "lock")
	if err := os.WriteFile(target, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, lock); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := catalogcmd.Run(context.Background(), compileArgs(lock), &out, &stderr); code == 0 {
		t.Fatal("accepted output symlink")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "preserve" {
		t.Fatal("changed symlink target")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := catalogcmd.Run(ctx, compileArgs(filepath.Join(root, "cancelled")), &out, &stderr); code == 0 {
		t.Fatal("accepted cancelled write")
	}
	if _, err := os.Lstat(filepath.Join(root, "cancelled")); !os.IsNotExist(err) {
		t.Fatal("cancelled command wrote output")
	}
}

func TestCompileSelectsTheProfileAMacOfThatMemoryUses(t *testing.T) {
	raw, err := os.ReadFile("../testfixture/testdata/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var tree map[string]any
	if err := json.Unmarshal(raw, &tree); err != nil {
		t.Fatal(err)
	}
	preset := tree["presets"].(map[string]any)["qwen3.8-27b-q4xl-mtp"].(map[string]any)
	small := preset["profiles"].([]any)[0].(map[string]any)
	large := map[string]any{}
	for k, v := range small {
		large[k] = v
	}
	large["min_memory_gib"], large["context_window_tokens"] = 64, 131072
	preset["profiles"] = []any{small, large}
	raw, err = json.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "catalog.json")
	if err := os.WriteFile(source, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args   []string
		window int
		label  string
	}{{nil, 65536, "profile for 32 GiB Macs"}, {[]string{"--memory-gib", "48"}, 65536, "profile for 32 GiB Macs"}, {[]string{"--memory-gib", "64"}, 131072, "profile for 64 GiB Macs"}} {
		out := filepath.Join(dir, fmt.Sprintf("lock-%d.json", len(tc.args)+tc.window))
		args := append([]string{"catalog", "compile", "--catalog", source, "--preset", "qwen3.8-27b-q4xl-mtp", "--target", "darwin/arm64", "--out", out}, tc.args...)
		if result := invoke(t, args...); !strings.Contains(result, tc.label) {
			t.Fatalf("compile result %q lacks %s", result, tc.label)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		lock, err := catalog.ParseLock(data)
		if err != nil {
			t.Fatal(err)
		}
		if got := lock.Records.Presets["qwen3.8-27b-q4xl-mtp"].Window(); got != tc.window {
			t.Fatalf("%v selected window %d, want %d", tc.args, got, tc.window)
		}
	}
}
