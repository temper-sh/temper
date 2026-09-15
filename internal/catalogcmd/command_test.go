package catalogcmd_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	return []string{"catalog", "compile", "--catalog", "../../catalog/qwen38-m5-refresh.json", "--selection", "../../catalog/qwen38-m5-refresh.selection.json", "--target", "darwin/arm64", "--out", out}
}

func TestDryRunHasNoFilesystemEffects(t *testing.T) {
	root := t.TempDir()
	lock := filepath.Join(root, "execution.lock.json")
	invoke(t, append(compileArgs(lock), "--dry-run")...)
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("dry compile wrote files: %v, %v", entries, err)
	}
	invoke(t, compileArgs(lock)...)
	destination := filepath.Join(root, "inputs")
	invoke(t, "execution", "export", "--lock", lock, "--out", destination, "--dry-run")
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("dry export created destination: %v", err)
	}
}

func TestExportReplaysAndRepairsOnlyAbsentExactInputs(t *testing.T) {
	root := t.TempDir()
	lock := filepath.Join(root, "execution.lock.json")
	invoke(t, compileArgs(lock)...)
	first, err := os.Stat(lock)
	if err != nil {
		t.Fatal(err)
	}
	if output := invoke(t, compileArgs(lock)...); !bytes.Contains([]byte(output), []byte("unchanged")) {
		t.Fatal(output)
	}
	again, _ := os.Stat(lock)
	if !os.SameFile(first, again) || !first.ModTime().Equal(again.ModTime()) {
		t.Fatal("second compile rewrote lock")
	}
	destination := filepath.Join(root, "inputs")
	args := []string{"execution", "export", "--lock", lock, "--out", destination}
	invoke(t, args...)
	if output := invoke(t, args...); !bytes.Contains([]byte(output), []byte("unchanged")) {
		t.Fatal(output)
	}
	kept := filepath.Join(destination, "manifest.lock.yaml")
	before, _ := os.Stat(kept)
	if err := os.Remove(filepath.Join(destination, "request-defaults.json")); err != nil {
		t.Fatal(err)
	}
	invoke(t, args...)
	after, _ := os.Stat(kept)
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("repair rewrote an existing exact input")
	}
	if err := os.WriteFile(kept, []byte("user edit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := catalogcmd.Run(context.Background(), args, &out, &stderr); code == 0 {
		t.Fatal("overwrote changed derived input")
	}
	data, _ := os.ReadFile(kept)
	if string(data) != "user edit\n" {
		t.Fatal("changed input was replaced")
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

func TestJSONExportBindsExactLockAndEveryDerivedInput(t *testing.T) {
	root := t.TempDir()
	lock := filepath.Join(root, "execution.lock.json")
	invoke(t, compileArgs(lock)...)
	destination := filepath.Join(root, "inputs")
	args := []string{"execution", "export", "--lock", lock, "--out", destination, "--json"}
	for _, wantChanged := range []bool{true, false} {
		var result struct {
			Schema  string `json:"schema"`
			LockSHA string `json:"lock_sha256"`
			Changed bool   `json:"changed"`
			DryRun  bool   `json:"dry_run"`
			Inputs  map[string]struct {
				Path string `json:"path"`
				SHA  string `json:"sha256"`
			} `json:"inputs"`
		}
		if err := json.Unmarshal([]byte(invoke(t, args...)), &result); err != nil {
			t.Fatal(err)
		}
		if result.Schema != "temper-execution-inputs/v1" || result.Changed != wantChanged || result.DryRun || len(result.Inputs) != 4 {
			t.Fatalf("invalid disposition: %+v", result)
		}
		data, err := os.ReadFile(lock)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if result.LockSHA != hex.EncodeToString(sum[:]) {
			t.Fatal("wrong lock identity")
		}
		for name, identity := range result.Inputs {
			if identity.Path != filepath.Join(destination, name) {
				t.Fatal("unbound input path")
			}
			data, err := os.ReadFile(identity.Path)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(data)
			if identity.SHA != hex.EncodeToString(sum[:]) {
				t.Fatalf("wrong input identity: %s", name)
			}
		}
	}
}
