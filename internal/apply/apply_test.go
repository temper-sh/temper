package apply_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	applyverb "github.com/temper-sh/temper/internal/apply"
	"github.com/temper-sh/temper/internal/testfixture"
)

func TestApplyDryRunFirstRunAndSecondRun(t *testing.T) {
	workspace := t.TempDir()
	manifestPath, lockPath := writeInputs(t, workspace)
	root := filepath.Join(workspace, "temper-root")
	receiptPath := materializeCoder(t, root, manifestPath, lockPath)
	options := applyverb.Options{
		ManifestPath: manifestPath,
		LockPath:     lockPath,
		Root:         root,
		Mode:         "local",
	}
	receiptBefore, err := os.Stat(receiptPath)
	if err != nil {
		t.Fatal(err)
	}

	dryOptions := options
	dryOptions.DryRun = true
	dry, err := applyverb.Run(context.Background(), dryOptions)
	if err != nil {
		t.Fatal(err)
	}
	if !dry.Changed {
		t.Fatal("fresh dry run reported unchanged")
	}
	if _, err := os.Lstat(filepath.Join(root, "rendered")); !os.IsNotExist(err) {
		t.Fatalf("dry run created rendered output: %v", err)
	}
	receiptAfter, err := os.Stat(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !receiptBefore.ModTime().Equal(receiptAfter.ModTime()) {
		t.Fatal("dry run rewrote the artifact receipt")
	}

	first, err := applyverb.Run(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Changed {
		t.Fatal("first apply reported unchanged")
	}
	currentPath := filepath.Join(root, "rendered", "current")
	target, err := os.Readlink(currentPath)
	if err != nil {
		t.Fatal(err)
	}
	if target != filepath.Join("generations", first.Generation) {
		t.Fatalf("current target = %q, want generation %s", target, first.Generation)
	}
	generationPath := filepath.Join(root, "rendered", target)
	generationBefore, err := os.Stat(generationPath)
	if err != nil {
		t.Fatal(err)
	}
	pointerBefore, err := os.Lstat(currentPath)
	if err != nil {
		t.Fatal(err)
	}

	second, err := applyverb.Run(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if second.Changed {
		t.Fatal("second identical apply reported changed")
	}
	generationAfter, _ := os.Stat(generationPath)
	pointerAfter, _ := os.Lstat(currentPath)
	if !generationBefore.ModTime().Equal(generationAfter.ModTime()) || !pointerBefore.ModTime().Equal(pointerAfter.ModTime()) {
		t.Fatal("second apply rewrote the generation or commit pointer")
	}
	entries, err := os.ReadDir(filepath.Join(root, "rendered", "generations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("second apply left %d generation entries, want one", len(entries))
	}
}

func TestApplyModeSwitchCommitsAnotherCompleteGeneration(t *testing.T) {
	workspace := t.TempDir()
	manifestPath, lockPath := writeInputs(t, workspace)
	root := filepath.Join(workspace, "temper-root")
	materializeCoder(t, root, manifestPath, lockPath)
	local, err := applyverb.Run(context.Background(), applyverb.Options{
		ManifestPath: manifestPath, LockPath: lockPath, Root: root, Mode: "local",
	})
	if err != nil {
		t.Fatal(err)
	}
	off, err := applyverb.Run(context.Background(), applyverb.Options{
		ManifestPath: manifestPath, LockPath: lockPath, Root: root, Mode: "off",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !off.Changed || off.Generation == local.Generation {
		t.Fatalf("off result = %#v after local generation %s", off, local.Generation)
	}
	config, err := os.ReadFile(filepath.Join(root, "rendered", "current", "llama-swap", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), "models: {}") {
		t.Fatalf("off generation is not an empty world:\n%s", config)
	}
}

func TestApplyRefusesAChangedImmutableGeneration(t *testing.T) {
	workspace := t.TempDir()
	manifestPath, lockPath := writeInputs(t, workspace)
	root := filepath.Join(workspace, "temper-root")
	materializeCoder(t, root, manifestPath, lockPath)
	options := applyverb.Options{ManifestPath: manifestPath, LockPath: lockPath, Root: root, Mode: "local"}
	first, err := applyverb.Run(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "rendered", "generations", first.Generation, "llama-swap", "config.yaml")
	if err := os.WriteFile(configPath, []byte("changed behind Temper's back\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = applyverb.Run(context.Background(), options)
	if err == nil || !strings.Contains(err.Error(), "does not match its content digest") {
		t.Fatalf("second apply error = %v, want immutable-generation refusal", err)
	}
}

func TestApplyRefusesMissingArtifactSetWithoutWriting(t *testing.T) {
	workspace := t.TempDir()
	manifestPath, lockPath := writeInputs(t, workspace)
	root := filepath.Join(workspace, "temper-root")
	_, err := applyverb.Run(context.Background(), applyverb.Options{
		ManifestPath: manifestPath,
		LockPath:     lockPath,
		Root:         root,
		Mode:         "local",
		DryRun:       true,
	})
	if err == nil || !strings.Contains(err.Error(), "run temper fetch coder") {
		t.Fatalf("error = %v, want actionable fetch refusal", err)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("refused dry run touched root: %v", err)
	}
}

func TestApplyRefusesMalformedArtifactSetWithoutWriting(t *testing.T) {
	workspace := t.TempDir()
	manifestPath, lockPath := writeInputs(t, workspace)
	root := filepath.Join(workspace, "temper-root")
	receiptPath := materializeCoder(t, root, manifestPath, lockPath)
	modelPath := filepath.Join(filepath.Dir(receiptPath), "model", "coder.gguf")
	if err := os.WriteFile(modelPath, []byte("wrong size"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := applyverb.Run(context.Background(), applyverb.Options{
		ManifestPath: manifestPath,
		LockPath:     lockPath,
		Root:         root,
		Mode:         "local",
	})
	if err == nil || !strings.Contains(err.Error(), "verify layout \"coder\" artifact set") {
		t.Fatalf("error = %v, want malformed-set refusal", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "rendered")); !os.IsNotExist(err) {
		t.Fatalf("refused apply created rendered output: %v", err)
	}
}

func writeInputs(t *testing.T, directory string) (string, string) {
	t.Helper()
	manifestPath := filepath.Join(directory, "manifest.yaml")
	lockPath := filepath.Join(directory, "manifest.lock.yaml")
	if err := os.WriteFile(manifestPath, []byte(applyManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte(applyLock), 0o644); err != nil {
		t.Fatal(err)
	}
	return manifestPath, lockPath
}

func materializeCoder(t *testing.T, root, manifestPath, lockPath string) string {
	t.Helper()
	set := testfixture.MaterializeLayout(t, root, manifestPath, lockPath, "coder", map[string][]byte{
		"model/coder.gguf": []byte("weights"),
	})
	return filepath.Join(set.Path(), "receipt.json")
}

const applyManifest = `schema: temper-manifest/v1
defaults: {ttl: 1800, gpu_memory_utilization: 0.85}
layouts:
  coder:
    display_name: Coder
    model: {repo: org/Coder, file: coder.gguf}
    engine: llama-server
    role: coder
    window: 8192
    max_tokens: 2048
    kv: q8
    thinking: off
    llama: {parallel: 1, flash_attention: on, batch: 512, ubatch: 512}
modes:
  local:
    foreground: local
    harnesses: [pi]
    members:
      resident: [{layout: coder, ttl: 7200, preferred: true}]
  off:
    foreground: none
`

const applyLock = `schema: temper-lock/v1
entries:
  coder:
    repo: org/Coder
    revision: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    files:
      - {name: coder.gguf, sha256: 9a129038d9a00aed0cf6a7ea059ca50a813449061ab87848cf1a13eafdf33b2c}
    resolved: 2026-08-19
`
