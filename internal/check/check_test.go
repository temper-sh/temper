package check_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/budget"
	checkverb "github.com/temper-sh/temper/internal/check"
	"github.com/temper-sh/temper/internal/testfixture"
)

func TestRunReportsACleanReceiptAudit(t *testing.T) {
	workspace := t.TempDir()
	manifestPath, lockPath := writeCheckInputs(t, workspace, fullCheckLock)
	root := filepath.Join(workspace, "root")
	materializeCheckLayouts(t, root, manifestPath, lockPath)

	result, err := checkverb.Run(context.Background(), checkverb.Options{
		ManifestPath: manifestPath,
		LockPath:     lockPath,
		Root:         root,
		Mode:         "local",
		Machine:      checkMachine,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK() || result.Verification != checkverb.VerificationReceipt || len(result.Findings) != 0 {
		t.Fatalf("result = %#v", result)
	}
	if len(result.Layouts) != 2 || result.Layouts[0].ID != "coder" || !result.Layouts[0].OK || result.Layouts[1].ID != "reranker" || !result.Layouts[1].OK {
		t.Fatalf("layout results = %#v", result.Layouts)
	}
	if result.Budget.Status != budget.StatusFits || result.Budget.Holder != "coder" || result.Budget.AllocationMiB != 22560 || result.Budget.HolderMinimumMiB != 1 || result.Budget.RequiredMiB != 23584 || result.Budget.SpareMiB != 992 {
		t.Fatalf("budget = %#v", result.Budget)
	}
}

func TestRunAccumulatesLockAndArtifactFindingsWithoutWriting(t *testing.T) {
	workspace := t.TempDir()
	manifestPath, lockPath := writeCheckInputs(t, workspace, partialCheckLock)
	root := filepath.Join(workspace, "absent-root")

	result, err := checkverb.Run(context.Background(), checkverb.Options{
		ManifestPath: manifestPath,
		LockPath:     lockPath,
		Root:         root,
		Mode:         "local",
		Machine:      checkMachine,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []checkverb.Finding{
		{Code: checkverb.CodeArtifactNotMaterialized, Layout: "coder"},
		{Code: checkverb.CodeLockEntryOrphan, Layout: "orphan"},
		{Code: checkverb.CodeLockEntryMissing, Layout: "reranker"},
	}
	if result.OK() || len(result.Findings) != len(want) {
		t.Fatalf("result = %#v", result)
	}
	for index := range want {
		if result.Findings[index].Code != want[index].Code || result.Findings[index].Layout != want[index].Layout {
			t.Fatalf("finding %d = %#v, want %#v", index, result.Findings[index], want[index])
		}
	}
	if result.Budget.Status != budget.StatusUnavailable {
		t.Fatalf("budget = %#v, want unavailable", result.Budget)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("check touched absent root: %v", err)
	}
}

func TestRunAddsABudgetFindingWhenTheResidentEnvelopeExceedsTheWall(t *testing.T) {
	workspace := t.TempDir()
	manifestPath, lockPath := writeCheckInputs(t, workspace, fullCheckLock)
	root := filepath.Join(workspace, "root")
	materializeCheckLayouts(t, root, manifestPath, lockPath)

	result, err := checkverb.Run(context.Background(), checkverb.Options{
		ManifestPath: manifestPath,
		LockPath:     lockPath,
		Root:         root,
		Mode:         "local",
		Machine: budget.Machine{
			PhysicalMiB: 2048, DeviceMiB: 1658, WiredLimitMiB: 1500,
			WiredSource: budget.WiredSourceLive,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OK() || result.Budget.Status != budget.StatusExceeded || result.Budget.RequiredMiB != 2433 || len(result.Findings) != 1 {
		t.Fatalf("result = %#v", result)
	}
	finding := result.Findings[0]
	if finding.Code != checkverb.CodeBudgetExceeded || finding.Layout != "coder" || finding.Detail != "predicted resident requirement 2433 MiB exceeds wired limit 1500 MiB; gpu_memory_utilization 0.287 or lower would fit" {
		t.Fatalf("finding = %#v", finding)
	}
}

func TestRunMarksACPUOnlyForegroundBudgetNotApplicable(t *testing.T) {
	workspace := t.TempDir()
	manifestPath, lockPath := writeCheckInputs(t, workspace, fullCheckLock)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "{layout: coder, preferred: true}", "{layout: coder, preferred: true, ngl: 0}", 1))
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(workspace, "root")
	materializeCheckLayouts(t, root, manifestPath, lockPath)

	result, err := checkverb.Run(context.Background(), checkverb.Options{
		ManifestPath: manifestPath, LockPath: lockPath, Root: root, Mode: "local", Machine: checkMachine,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK() || result.Budget.Status != budget.StatusNotApplicable {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunVerifyDetectsSameSizeContentDrift(t *testing.T) {
	workspace := t.TempDir()
	manifestPath, lockPath := writeCheckInputs(t, workspace, fullCheckLock)
	root := filepath.Join(workspace, "root")
	sets := materializeCheckLayouts(t, root, manifestPath, lockPath)
	modelPath := filepath.Join(sets.coder, "model", "coder.gguf")
	if err := os.WriteFile(modelPath, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}

	routine, err := checkverb.Run(context.Background(), checkverb.Options{
		ManifestPath: manifestPath, LockPath: lockPath, Root: root, Mode: "local",
		Machine: checkMachine,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !routine.OK() {
		t.Fatalf("routine receipt audit = %#v, want clean", routine)
	}

	verified, err := checkverb.Run(context.Background(), checkverb.Options{
		ManifestPath: manifestPath, LockPath: lockPath, Root: root, Mode: "local", Verify: true,
		Machine: checkMachine,
	})
	if err != nil {
		t.Fatal(err)
	}
	if verified.OK() || verified.Verification != checkverb.VerificationSHA256 || len(verified.Findings) != 1 {
		t.Fatalf("verified result = %#v", verified)
	}
	finding := verified.Findings[0]
	if finding.Code != checkverb.CodeArtifactHashMismatch || finding.Layout != "coder" {
		t.Fatalf("finding = %#v", finding)
	}
}

func TestRunClassifiesMalformedArtifactReceipt(t *testing.T) {
	workspace := t.TempDir()
	manifestPath, lockPath := writeCheckInputs(t, workspace, fullCheckLock)
	root := filepath.Join(workspace, "root")
	sets := materializeCheckLayouts(t, root, manifestPath, lockPath)
	receiptPath := filepath.Join(sets.coder, "receipt.json")
	receipt, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(receiptPath, append(receipt, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := checkverb.Run(context.Background(), checkverb.Options{
		ManifestPath: manifestPath, LockPath: lockPath, Root: root, Mode: "local",
		Machine: checkMachine,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OK() || len(result.Findings) != 1 || result.Findings[0].Code != checkverb.CodeArtifactInvalid || result.Findings[0].Layout != "coder" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunReportsLockSelectionDriftWithoutGuessingArtifactIdentity(t *testing.T) {
	workspace := t.TempDir()
	manifestPath, lockPath := writeCheckInputs(t, workspace, driftCheckLock)
	root := filepath.Join(workspace, "root")
	testfixture.MaterializeLayout(t, root, manifestPath, lockPath, "reranker", map[string][]byte{
		"model/reranker.gguf": []byte("ranking"),
	})

	result, err := checkverb.Run(context.Background(), checkverb.Options{
		ManifestPath: manifestPath, LockPath: lockPath, Root: root, Mode: "local",
		Machine: checkMachine,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OK() || len(result.Findings) != 1 || result.Findings[0].Code != checkverb.CodeLockSelectionDrift || result.Findings[0].Layout != "coder" {
		t.Fatalf("result = %#v", result)
	}
	if result.Layouts[0].ID != "coder" || result.Layouts[0].ArtifactSet != "" || result.Layouts[0].OK {
		t.Fatalf("drifting layout result = %#v", result.Layouts[0])
	}
}

var checkMachine = budget.Machine{
	PhysicalMiB: 32768, DeviceMiB: 26542, WiredLimitMiB: 24576,
	WiredSource: budget.WiredSourceLive,
}

type checkSets struct {
	coder    string
	reranker string
}

func materializeCheckLayouts(t *testing.T, root, manifestPath, lockPath string) checkSets {
	t.Helper()
	coder := testfixture.MaterializeLayout(t, root, manifestPath, lockPath, "coder", map[string][]byte{
		"model/coder.gguf": []byte("weights"),
	})
	reranker := testfixture.MaterializeLayout(t, root, manifestPath, lockPath, "reranker", map[string][]byte{
		"model/reranker.gguf": []byte("ranking"),
	})
	return checkSets{coder: coder.Path(), reranker: reranker.Path()}
}

func writeCheckInputs(t *testing.T, directory, lock string) (string, string) {
	t.Helper()
	manifestPath := filepath.Join(directory, "manifest.yaml")
	lockPath := filepath.Join(directory, "manifest.lock.yaml")
	if err := os.WriteFile(manifestPath, []byte(checkManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	return manifestPath, lockPath
}

const checkManifest = `schema: temper-manifest/v1
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
  reranker:
    display_name: Reranker
    model: {repo: org/Reranker, file: reranker.gguf}
    engine: llama-server
    role: rerank
    window: 4096
    llama: {parallel: 1, flash_attention: auto, batch: 256, ubatch: 256}
modes:
  local:
    foreground: local
    members:
      resident: [{layout: coder, preferred: true}]
      on_demand: [{layout: reranker}]
`

const fullCheckLock = `schema: temper-lock/v1
entries:
  coder:
    repo: org/Coder
    revision: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    files:
      - {name: coder.gguf, sha256: 9a129038d9a00aed0cf6a7ea059ca50a813449061ab87848cf1a13eafdf33b2c}
    resolved: 2026-08-20
  reranker:
    repo: org/Reranker
    revision: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
    files:
      - {name: reranker.gguf, sha256: ee6fb3167b02cd70a7d02c4cdfc50ae5bfa6e63f7779eb20218ddf3e74138bec}
    resolved: 2026-08-20
`

const partialCheckLock = `schema: temper-lock/v1
entries:
  coder:
    repo: org/Coder
    revision: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    files:
      - {name: coder.gguf, sha256: 9a129038d9a00aed0cf6a7ea059ca50a813449061ab87848cf1a13eafdf33b2c}
    resolved: 2026-08-20
  orphan:
    repo: org/Orphan
    revision: cccccccccccccccccccccccccccccccccccccccc
    files:
      - {name: orphan.gguf, sha256: 88f6811ab5d8fc6d3177f9b7609ae0fcebfda187e5046b62d38bb539e88b74d7}
    resolved: 2026-08-20
`

const driftCheckLock = `schema: temper-lock/v1
entries:
  coder:
    repo: org/Other
    revision: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    files:
      - {name: coder.gguf, sha256: 9a129038d9a00aed0cf6a7ea059ca50a813449061ab87848cf1a13eafdf33b2c}
    resolved: 2026-08-20
  reranker:
    repo: org/Reranker
    revision: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
    files:
      - {name: reranker.gguf, sha256: ee6fb3167b02cd70a7d02c4cdfc50ae5bfa6e63f7779eb20218ddf3e74138bec}
    resolved: 2026-08-20
`
