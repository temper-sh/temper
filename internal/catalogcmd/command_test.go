package catalogcmd_test

import (
	"bytes"
	"context"

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
	return []string{"catalog", "compile", "--catalog", "../../catalog/guided-setup.json", "--preset", "qwen3.8-27b-q4xl-mtp", "--target", "darwin/arm64", "--out", out}
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
