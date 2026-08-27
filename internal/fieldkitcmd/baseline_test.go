package fieldkitcmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/fieldkit/baselinerun"
)

type baselineExecutorFake struct {
	calls   int
	errorAt int
}

func (f *baselineExecutorFake) next() error {
	f.calls++
	if f.errorAt == f.calls {
		return fmt.Errorf("injected stage failure %d", f.calls)
	}
	return nil
}

func (f *baselineExecutorFake) RunProtocol(_ context.Context, invocation baselinerun.ProtocolInvocation, stdout, _ io.Writer) error {
	if err := f.next(); err != nil {
		return err
	}
	if err := os.WriteFile(invocation.Report, []byte("{\n  \"schema\": \"field-kit-qwen38-dynamic-protocol/v1\",\n  \"status\": \"pass\"\n}\n"), 0o600); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "{\"report\":%q,\"status\":\"pass\"}\n", invocation.Report)
	return nil
}

func (f *baselineExecutorFake) Run(_ context.Context, path string, arguments []string, stdout, _ io.Writer) error {
	if err := f.next(); err != nil {
		return err
	}
	command := strings.Join(arguments, " ")
	switch {
	case strings.Contains(command, "software install"):
		fmt.Fprintln(stdout, "RESULT software-install changed installation=field-kit-qwen38-dynamic packages=2 units=2 effects=2 claims=0")
	case strings.HasPrefix(command, "fetch "):
		fmt.Fprintln(stdout, "RESULT fetch changed layout=qwen38-dynamic-q4xl artifact-set="+strings.Repeat("a", 64))
	case strings.HasPrefix(command, "apply "):
		fmt.Fprintln(stdout, "RESULT apply changed mode=baseline generation="+strings.Repeat("b", 64))
	case strings.Contains(command, "software check"):
		fmt.Fprintln(stdout, "RESULT software-check exact installation=field-kit-qwen38-dynamic packages=2 units=2 requirements=0 problems=0 receipt="+strings.Repeat("c", 64))
	case strings.Contains(command, "software remove"):
		fmt.Fprintln(stdout, "RESULT software-remove changed installation=field-kit-qwen38-dynamic packages=2 units=2 effects=2 claims=0")
	case strings.HasPrefix(command, "check "):
		fmt.Fprintln(stdout, "RESULT check ok mode=baseline verification=full layouts=1 problems=0")
	case strings.Contains(command, "field-kit bind"):
		fmt.Fprint(stdout, "schema: temper-field-kit-binding/v1\nfixture: true\n")
	default:
		return fmt.Errorf("unexpected fake invocation: %s %s", path, command)
	}
	return nil
}

func TestBaselineGuidedRunShowsConsentAndCompletesFromOneCommand(t *testing.T) {
	workspace := t.TempDir()
	factsPath := filepath.Join(workspace, "machine.yaml")
	facts := `schema: temper-machine-facts/v1
target:
  os: darwin
  arch: arm64
  distribution: macos
  distribution_version: "26.0"
hardware_model: Mac17,3
chip: Apple M5
os_build: 25A1
physical_memory_bytes: 34359738368
metal_device_memory_mib: 26542
metal_device_memory_source: predicted-metal-81-percent
wired_limit_mib: 24576
wired_limit_source: live-sysctl
`
	if err := os.WriteFile(factsPath, []byte(facts), 0o600); err != nil {
		t.Fatal(err)
	}
	temperPath := filepath.Join(workspace, "temper")
	if err := os.WriteFile(temperPath, []byte("fixture temper"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(workspace, "guided-root")
	arguments := []string{
		"run", "qwen38-dynamic-q4xl@3", "--root", root,
		"--facts", factsPath, "--temper", temperPath, "--at", "2026-08-27T12:00:00Z",
	}
	fake := &baselineExecutorFake{}
	var stdout, stderr bytes.Buffer
	exit := runBaselineWithInput(context.Background(), arguments, strings.NewReader("keep\nyes\n"), &stdout, &stderr, fake, nil)
	if exit != 0 || !strings.Contains(stdout.String(), "FIELD KIT BASELINE qwen38-dynamic-q4xl@3") || !strings.Contains(stdout.String(), "FIELD-KIT BASELINE complete") {
		t.Fatalf("guided exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "outcome [keep/restore]") || !strings.Contains(stderr.String(), "Type yes to consent") {
		t.Fatalf("guided consent prompt = %q", stderr.String())
	}
	if fake.calls != 7 {
		t.Fatalf("guided process calls = %d, want 7", fake.calls)
	}
	for _, path := range []string{root + ".session.json", root + ".report.md"} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("guided output %s: %v", path, err)
		}
	}

	stdout.Reset()
	stderr.Reset()
	exit = runBaselineWithInput(context.Background(), arguments, strings.NewReader(""), &stdout, &stderr, fake, nil)
	if exit != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "resuming session=") || !strings.Contains(stdout.String(), "already-complete") {
		t.Fatalf("guided rerun exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
	if fake.calls != 7 {
		t.Fatalf("guided rerun repeated process calls: %d", fake.calls)
	}
}

func TestBaselineGuidedRunDeclinesBeforeRootOrSessionEffects(t *testing.T) {
	workspace := t.TempDir()
	factsPath := filepath.Join(workspace, "machine.yaml")
	facts := `schema: temper-machine-facts/v1
target:
  os: darwin
  arch: arm64
  distribution: macos
  distribution_version: "26.0"
hardware_model: Mac17,3
chip: Apple M5
os_build: 25A1
physical_memory_bytes: 34359738368
metal_device_memory_mib: 26542
metal_device_memory_source: predicted-metal-81-percent
wired_limit_mib: 24576
wired_limit_source: live-sysctl
`
	if err := os.WriteFile(factsPath, []byte(facts), 0o600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(workspace, "declined-root")
	fake := &baselineExecutorFake{}
	var stdout, stderr bytes.Buffer
	exit := runBaselineWithInput(context.Background(), []string{
		"run", "qwen38-dynamic-q4xl@3", "--root", root, "--outcome", "keep", "--facts", factsPath,
	}, strings.NewReader("no\n"), &stdout, &stderr, fake, nil)
	if exit != 1 || fake.calls != 0 || !strings.Contains(stderr.String(), "consent declined") {
		t.Fatalf("declined exit=%d calls=%d stdout=%q stderr=%q", exit, fake.calls, stdout.String(), stderr.String())
	}
	for _, path := range []string{root, root + ".session.json", root + ".report.md"} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("declined guided run created %s: %v", path, err)
		}
	}
}

func TestBaselineKeepWorkflowIsConsentedResumableAndReportable(t *testing.T) {
	workspace := t.TempDir()
	factsPath := filepath.Join(workspace, "machine.yaml")
	facts := `schema: temper-machine-facts/v1
target:
  os: darwin
  arch: arm64
  distribution: macos
  distribution_version: "26.0"
hardware_model: Mac17,3
chip: Apple M5
os_build: 25A1
physical_memory_bytes: 34359738368
metal_device_memory_mib: 26542
metal_device_memory_source: predicted-metal-81-percent
wired_limit_mib: 24576
wired_limit_source: live-sysctl
`
	if err := os.WriteFile(factsPath, []byte(facts), 0o600); err != nil {
		t.Fatal(err)
	}
	temperPath := filepath.Join(workspace, "temper")
	if err := os.WriteFile(temperPath, []byte("fixture temper"), 0o755); err != nil {
		t.Fatal(err)
	}
	disclosurePath := filepath.Join(workspace, "disclosure.txt")
	var disclosure, stderr bytes.Buffer
	exit := runBaseline(context.Background(), []string{
		"explain", "qwen38-dynamic-q4xl@3", "--facts", factsPath,
	}, &disclosure, &stderr, &baselineExecutorFake{}, nil)
	if exit != 0 || stderr.Len() != 0 {
		t.Fatalf("explain exit=%d stderr=%q", exit, stderr.String())
	}
	if err := os.WriteFile(disclosurePath, disclosure.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(workspace, "dedicated-root")
	session := root + ".session.json"
	var stdout bytes.Buffer
	exit = runBaseline(context.Background(), []string{
		"start", "--facts", factsPath, "--baseline", "qwen38-dynamic-q4xl@3",
		"--disclosure", disclosurePath,
		"--temper", temperPath, "--root", root, "--outcome", "keep",
		"--at", "2026-08-27T12:00:00Z", "--consent", "yes",
	}, &stdout, &stderr, &baselineExecutorFake{}, nil)
	if exit != 0 || stderr.Len() != 0 {
		t.Fatalf("start exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "id=qwen38-dynamic-q4xl-2026-08-27t120000z") || !strings.Contains(stdout.String(), "session="+session) {
		t.Fatalf("start did not report generated identity and path: %q", stdout.String())
	}
	if _, err := os.Lstat(filepath.Join(root, ".temper-field-kit-owner.json")); err != nil {
		t.Fatalf("start did not create owned root: %v", err)
	}

	fake := &baselineExecutorFake{}
	promptPath := filepath.Join(root, "field-kit", "package", "PROMPT.md")
	prompt, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(promptPath, append(append([]byte(nil), prompt...), 'x'), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runBaseline(context.Background(), []string{"status", "--session", session}, &stdout, &stderr, fake, nil); exit != 0 || !strings.Contains(stdout.String(), "baseline=qwen38-dynamic-q4xl@3") {
		t.Fatalf("status exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runBaseline(context.Background(), []string{"status", "--baseline", "qwen38-dynamic-q4xl@2", "--session", session}, &stdout, &stderr, fake, nil); exit != 1 || !strings.Contains(stderr.String(), "differs from session baseline") {
		t.Fatalf("selector mismatch exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runBaseline(context.Background(), []string{
		"run-next", "--session", session,
	}, &stdout, &stderr, fake, nil); exit != 1 || fake.calls != 0 || !strings.Contains(stderr.String(), "package material") {
		t.Fatalf("tamper exit=%d calls=%d stdout=%q stderr=%q", exit, fake.calls, stdout.String(), stderr.String())
	}
	if err := os.WriteFile(promptPath, prompt, 0o600); err != nil {
		t.Fatal(err)
	}
	failing := &baselineExecutorFake{errorAt: 2}
	stdout.Reset()
	stderr.Reset()
	if exit = runBaseline(context.Background(), []string{"run", "--session", session}, &stdout, &stderr, failing, nil); exit != 1 || failing.calls != 2 || !strings.Contains(stderr.String(), "failed without advancing") {
		t.Fatalf("failed run exit=%d calls=%d stdout=%q stderr=%q", exit, failing.calls, stdout.String(), stderr.String())
	}
	fake = &baselineExecutorFake{}
	stdout.Reset()
	stderr.Reset()
	if exit = runBaseline(context.Background(), []string{"run", "--session", session}, &stdout, &stderr, fake, nil); exit != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "stages-complete") {
		t.Fatalf("resumed run exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
	if fake.calls != 6 {
		t.Fatalf("resumed process calls = %d, want 6 (one stage was committed and keep outcome is internal)", fake.calls)
	}
	report := root + ".report.md"
	stdout.Reset()
	stderr.Reset()
	exit = runBaseline(context.Background(), []string{
		"finish", "--session", session,
	}, &stdout, &stderr, fake, nil)
	if exit != 0 || stderr.Len() != 0 {
		t.Fatalf("finish exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
	data, err := os.ReadFile(report)
	if err != nil || !bytes.Contains(data, []byte("# Field Kit baseline report")) {
		t.Fatalf("report = %q, err=%v", data, err)
	}
	if _, err := os.Lstat(root); err != nil {
		t.Fatalf("keep outcome removed root: %v", err)
	}
}

func TestBaselineStartRefusesBeforeReadsOrRootEffectsWithoutConsent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "must-not-exist")
	var stdout, stderr bytes.Buffer
	exit := runBaseline(context.Background(), []string{"start", "--root", root, "--consent", "no"}, &stdout, &stderr, &baselineExecutorFake{}, nil)
	if exit != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "explicit --consent yes") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("consent refusal created root: %v", err)
	}
}

func TestBaselineRestoreRequiresConfirmationAndDeletesOnlyOwnedRoot(t *testing.T) {
	workspace := t.TempDir()
	factsPath := filepath.Join(workspace, "machine.yaml")
	facts := `schema: temper-machine-facts/v1
target:
  os: darwin
  arch: arm64
  distribution: macos
  distribution_version: "26.0"
hardware_model: Mac17,3
chip: Apple M5
os_build: 25A1
physical_memory_bytes: 34359738368
metal_device_memory_mib: 26542
metal_device_memory_source: predicted-metal-81-percent
wired_limit_mib: 24576
wired_limit_source: live-sysctl
`
	if err := os.WriteFile(factsPath, []byte(facts), 0o600); err != nil {
		t.Fatal(err)
	}
	temperPath := filepath.Join(workspace, "temper")
	if err := os.WriteFile(temperPath, []byte("fixture temper"), 0o755); err != nil {
		t.Fatal(err)
	}
	var disclosure, stderr bytes.Buffer
	if exit := runBaseline(context.Background(), []string{"explain", "qwen38-dynamic-q4xl@3", "--facts", factsPath}, &disclosure, &stderr, &baselineExecutorFake{}, nil); exit != 0 {
		t.Fatalf("explain exit=%d stderr=%q", exit, stderr.String())
	}
	disclosurePath := filepath.Join(workspace, "disclosure.txt")
	if err := os.WriteFile(disclosurePath, disclosure.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(workspace, "owned-root")
	session := filepath.Join(workspace, "session.json")
	var stdout bytes.Buffer
	arguments := []string{
		"start", "--facts", factsPath, "--baseline", "qwen38-dynamic-q4xl@3",
		"--session", session, "--id", "restore-test", "--disclosure", disclosurePath, "--temper", temperPath,
		"--root", root, "--outcome", "restore", "--at", "2026-08-27T12:00:00Z", "--consent", "yes",
	}
	if exit := runBaseline(context.Background(), arguments, &stdout, &stderr, &baselineExecutorFake{}, nil); exit != 0 {
		t.Fatalf("start exit=%d stderr=%q", exit, stderr.String())
	}
	fake := &baselineExecutorFake{}
	for stage := 0; stage < 8; stage++ {
		stdout.Reset()
		stderr.Reset()
		if exit := runBaseline(context.Background(), []string{"run-next", "--baseline", "qwen38-dynamic-q4xl@3", "--session", session}, &stdout, &stderr, fake, nil); exit != 0 {
			t.Fatalf("stage %d exit=%d stderr=%q", stage+1, exit, stderr.String())
		}
	}
	report := filepath.Join(workspace, "report.md")
	finish := []string{"finish", "--baseline", "qwen38-dynamic-q4xl@3", "--session", session, "--report", report}
	stdout.Reset()
	stderr.Reset()
	if exit := runBaseline(context.Background(), finish, &stdout, &stderr, fake, nil); exit != 1 || !strings.Contains(stderr.String(), "--confirm-restore yes") {
		t.Fatalf("unconfirmed finish exit=%d stderr=%q", exit, stderr.String())
	}
	if _, err := os.Lstat(report); !os.IsNotExist(err) {
		t.Fatalf("unconfirmed restore wrote report: %v", err)
	}
	if _, err := os.Lstat(root); err != nil {
		t.Fatalf("unconfirmed restore removed root: %v", err)
	}
	finish = append(finish, "--confirm-restore", "yes")
	stdout.Reset()
	stderr.Reset()
	if exit := runBaseline(context.Background(), finish, &stdout, &stderr, fake, nil); exit != 0 {
		t.Fatalf("confirmed finish exit=%d stderr=%q", exit, stderr.String())
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("confirmed restore retained owned root: %v", err)
	}
	if _, err := os.Lstat(session); err != nil {
		t.Fatalf("restore removed external session: %v", err)
	}
	if _, err := os.Lstat(report); err != nil {
		t.Fatalf("restore removed external report: %v", err)
	}
}
