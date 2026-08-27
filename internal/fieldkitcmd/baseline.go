package fieldkitcmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/temper-sh/temper/internal/fieldkit/baseline"
	"github.com/temper-sh/temper/internal/fieldkit/baselinerun"
	"github.com/temper-sh/temper/internal/fieldkit/catalog"
	"github.com/temper-sh/temper/internal/fieldkitprotocol"
	"github.com/temper-sh/temper/internal/machine"
)

type executor interface {
	Run(context.Context, string, []string, io.Writer, io.Writer) error
	RunProtocol(context.Context, baselinerun.ProtocolInvocation, io.Writer, io.Writer) error
}

type processExecutor struct{}

func (processExecutor) Run(ctx context.Context, path string, arguments []string, stdout, stderr io.Writer) error {
	command := exec.CommandContext(ctx, path, arguments...)
	command.Stdout = stdout
	command.Stderr = stderr
	command.Env = os.Environ()
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	command.WaitDelay = 35 * time.Second
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	return nil
}

func (processExecutor) RunProtocol(ctx context.Context, invocation baselinerun.ProtocolInvocation, stdout, stderr io.Writer) error {
	runner, err := fieldkitprotocol.NewRunner()
	if err != nil {
		return err
	}
	return runner.Run(ctx, fieldkitprotocol.Options{
		ID: invocation.ID, Revision: invocation.Revision, Schema: invocation.Schema,
		TemperPath: invocation.TemperPath, Root: invocation.Root, SoftwareLock: invocation.SoftwareLock,
		Generation: invocation.Generation, Installation: invocation.Installation, Model: invocation.Model,
		Listen: invocation.Listen, Report: invocation.Report, LogDirectory: invocation.LogDirectory,
	}, stdout, stderr)
}

func runBaseline(ctx context.Context, arguments []string, stdout, stderr io.Writer, execute executor, detect FactsDetector) int {
	return runBaselineWithInput(ctx, arguments, strings.NewReader(""), stdout, stderr, execute, detect)
}

func runBaselineWithInput(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer, execute executor, detect FactsDetector) int {
	if len(arguments) == 0 {
		baselineUsage(stderr)
		return 2
	}
	switch arguments[0] {
	case "verify":
		return runBaselineVerify(arguments[1:], stdout, stderr)
	case "inspect":
		return runBaselineInspect(ctx, arguments[1:], stdout, stderr, detect)
	case "explain":
		return runBaselineExplain(ctx, arguments[1:], stdout, stderr, detect)
	case "start":
		return runBaselineStart(ctx, arguments[1:], stdout, stderr, detect)
	case "status":
		return runBaselineStatus(arguments[1:], stdout, stderr)
	case "run-next":
		return runBaselineNext(ctx, arguments[1:], stdout, stderr, execute)
	case "run":
		return runBaselineRun(ctx, arguments[1:], input, stdout, stderr, execute, detect)
	case "finish":
		return runBaselineFinish(ctx, arguments[1:], stdout, stderr)
	case "help", "--help", "-h":
		baselineUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "field-kit baseline: unknown command %q\n\n", arguments[0])
		baselineUsage(stderr)
		return 2
	}
}

func runBaselineVerify(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("field-kit baseline verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	catalogPath := flags.String("catalog", "", "override the embedded baseline catalog")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		return 2
	}
	snapshot, err := loadBaselineCatalog(*catalogPath)
	if err != nil {
		fmt.Fprintf(stderr, "field-kit baseline verify: %v\n", err)
		return 1
	}
	for _, entry := range snapshot.Entries {
		if entry.Reference.Availability != "active" {
			continue
		}
		protocol := entry.Package.Mechanics.Protocol
		if protocol == nil || !fieldkitprotocol.Supports(protocol.ID, protocol.Revision, protocol.Schema) {
			fmt.Fprintf(stderr, "field-kit baseline verify: active baseline %s@%d names a protocol this Temper release does not support\n", entry.Package.ID, entry.Package.Revision)
			return 1
		}
	}
	fmt.Fprintf(stdout, "FIELD-KIT BASELINES ok revision=%d sha256=%s baselines=%d\n", snapshot.Document.Revision, snapshot.SHA256, len(snapshot.Entries))
	return 0
}

func runBaselineInspect(ctx context.Context, arguments []string, stdout, stderr io.Writer, detect FactsDetector) int {
	flags := flag.NewFlagSet("field-kit baseline inspect", flag.ContinueOnError)
	flags.SetOutput(stderr)
	catalogPath := flags.String("catalog", "", "override the embedded baseline catalog")
	factsPath := flags.String("facts", "", "canonical Temper machine facts override")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		return 2
	}
	snapshot, facts, _, err := loadBaselineAndFacts(ctx, *catalogPath, *factsPath, detect)
	if err != nil {
		fmt.Fprintf(stderr, "field-kit baseline inspect: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "FIELD-KIT BASELINE MACHINE catalog=%s baselines=%d\n", snapshot.SHA256, len(snapshot.Entries))
	for _, entry := range snapshot.Entries {
		if entry.Reference.Availability != "active" {
			fmt.Fprintf(stdout, "BASELINE %s %s@%d reason=%q\n", entry.Reference.Availability, entry.Package.ID, entry.Package.Revision, entry.Reference.AvailabilityReason)
			continue
		}
		applicability := catalog.EvaluatePredicate(entry.Package.Applicability, facts)
		status := "inapplicable"
		if applicability.Applicable {
			status = "applicable"
		}
		fmt.Fprintf(stdout, "BASELINE %s %s@%d reason=%q\n", status, entry.Package.ID, entry.Package.Revision, strings.Join(applicability.Reasons, "; "))
		if applicability.Applicable {
			for _, signal := range entry.Package.Relevance {
				if catalog.EvaluatePredicate(signal.When, facts).Applicable {
					fmt.Fprintf(stdout, "RELEVANCE %s %s reason=%q\n", entry.Package.ID, signal.ID, signal.Reason)
				}
			}
		}
	}
	return 0
}

func runBaselineExplain(ctx context.Context, arguments []string, stdout, stderr io.Writer, detect FactsDetector) int {
	if len(arguments) == 0 || strings.HasPrefix(arguments[0], "-") {
		fmt.Fprintln(stderr, "field-kit baseline explain: exact baseline id@revision is required")
		return 2
	}
	selector := arguments[0]
	flags := flag.NewFlagSet("field-kit baseline explain", flag.ContinueOnError)
	flags.SetOutput(stderr)
	catalogPath := flags.String("catalog", "", "override the embedded baseline catalog")
	factsPath := flags.String("facts", "", "canonical Temper machine facts override")
	if err := flags.Parse(arguments[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		return 2
	}
	snapshot, facts, _, err := loadBaselineAndFacts(ctx, *catalogPath, *factsPath, detect)
	if err != nil {
		fmt.Fprintf(stderr, "field-kit baseline explain: %v\n", err)
		return 1
	}
	entry, err := baseline.Find(snapshot, selector)
	if err != nil || entry.Reference.Availability != "active" {
		if err == nil {
			err = fmt.Errorf("baseline is %s: %s", entry.Reference.Availability, entry.Reference.AvailabilityReason)
		}
		fmt.Fprintf(stderr, "field-kit baseline explain: %v\n", err)
		return 1
	}
	if applicability := catalog.EvaluatePredicate(entry.Package.Applicability, facts); !applicability.Applicable {
		fmt.Fprintf(stderr, "field-kit baseline explain: baseline is not applicable: %s\n", strings.Join(applicability.Reasons, "; "))
		return 1
	}
	if _, err := stdout.Write(baselineDisclosure(entry.Package, facts)); err != nil {
		fmt.Fprintf(stderr, "field-kit baseline explain: write: %v\n", err)
		return 1
	}
	return 0
}

func baselineDisclosure(item baseline.Package, facts catalog.MachineFacts) []byte {
	var output bytes.Buffer
	fmt.Fprintf(&output, "FIELD KIT BASELINE %s@%d\n", item.ID, item.Revision)
	fmt.Fprintf(&output, "Title: %s\nWhat it does: %s\nEvidence boundary: %s\n", item.Title, item.Summary, item.EvidenceScope)
	fmt.Fprintf(&output, "Machine: %s; memory=%d MiB; wired-limit=%d MiB\n", facts.Chip, facts.PhysicalMemoryBytes/(1024*1024), facts.WiredLimitMiB)
	fmt.Fprintf(&output, "Exact profile: model=%s@%s file=%s sha256=%s; engine=%s@%s; router=%s@%s; template=%s\n",
		item.Profile.ModelRepository, item.Profile.ModelRevision, item.Profile.ModelFile, item.Profile.ModelSHA256,
		item.Profile.EngineVersion, item.Profile.EngineRevision, item.Profile.RouterVersion, item.Profile.RouterRevision, item.Profile.TemplateSHA256)
	fmt.Fprintf(&output, "Runtime: context=%d; max-output=%d; slots=%d; kv=%s; flash=%s; batch=%d/%d; reasoning=%s; speculation=%s\n",
		item.Profile.ContextTokens, item.Profile.MaximumOutputTokens, item.Profile.ParallelSlots, item.Profile.KV, item.Profile.FlashAttention, item.Profile.BatchTokens, item.Profile.MicrobatchTokens, item.Profile.Reasoning, item.Profile.Speculation)
	fmt.Fprintf(&output, "Time: protocol %d min; setup %d-%d min\n", item.Cost.FixedRuntimeMinutes, item.Cost.SetupMinutesMin, item.Cost.SetupMinutesMax)
	fmt.Fprintf(&output, "Resources: network<=%d bytes; temporary-disk<=%d bytes; retained-disk<=%d bytes; memory=%s; idle=%t\n",
		item.Cost.NetworkBytesMax, item.Cost.TemporaryDiskBytes, item.Cost.RetainedDiskBytes, item.Cost.MemoryPressure, item.Cost.IdleRequired)
	fmt.Fprintf(&output, "Effects: service=%s; paid-provider=%s\n", item.Cost.ServiceDisruption, item.Cost.PaidProvider)
	fmt.Fprintf(&output, "Reads: %s\nWrites: %s\nNetwork destinations: %s\n", joined(item.Consent.Reads), joined(item.Consent.Writes), joined(item.Consent.NetworkDestinations))
	fmt.Fprintf(&output, "Output: %s; submission=%s\nCleanup: %s\n", item.Consent.LocalOutput, item.Report.Submission, item.Consent.Cleanup)
	fmt.Fprintf(&output, "Renewed consent: %s\n", joined(item.Consent.RenewedConsent))
	fmt.Fprintln(&output, "Outcome choice: keep or restore. This choice is recorded before effects.")
	fmt.Fprintln(&output, "No download, installation, service start, baseline run, cleanup, or upload occurs from this explanation.")
	return output.Bytes()
}

func runBaselineStart(ctx context.Context, arguments []string, stdout, stderr io.Writer, detect FactsDetector) int {
	return runBaselineStartWithDisclosure(ctx, arguments, nil, stdout, stderr, detect)
}

func runBaselineStartWithDisclosure(ctx context.Context, arguments []string, disclosed []byte, stdout, stderr io.Writer, detect FactsDetector) int {
	flags := flag.NewFlagSet("field-kit baseline start", flag.ContinueOnError)
	flags.SetOutput(stderr)
	catalogPath := flags.String("catalog", "", "override the embedded baseline catalog")
	factsPath := flags.String("facts", "", "canonical Temper machine facts override")
	selector := flags.String("baseline", "", "exact baseline id@revision")
	sessionPath := flags.String("session", "", "new session path outside the dedicated root")
	id := flags.String("id", "", "stable local session id")
	disclosurePath := flags.String("disclosure", "", "exact disclosure shown to the user")
	temperPath := flags.String("temper", "", "Temper executable override (defaults to this binary)")
	rootFlag := flags.String("root", "", "new dedicated Temper root")
	outcome := flags.String("outcome", "", "keep or restore")
	at := flags.String("at", "", "canonical UTC RFC3339 consent time")
	consent := flags.String("consent", "", "must be exactly yes")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *consent != "yes" {
		fmt.Fprintln(stderr, "field-kit baseline start: explicit --consent yes is required; no root or session was created")
		return 1
	}
	if flags.NArg() != 0 || *selector == "" || (disclosed == nil && *disclosurePath == "") || *rootFlag == "" || *outcome == "" {
		fmt.Fprintln(stderr, "field-kit baseline start: --baseline, --disclosure, --root, and --outcome are required")
		return 2
	}
	if *at == "" {
		*at = time.Now().UTC().Format(time.RFC3339Nano)
	}
	consentAt, err := time.Parse(time.RFC3339Nano, *at)
	if err != nil || consentAt.Location() != time.UTC || consentAt.Format(time.RFC3339Nano) != *at {
		fmt.Fprintln(stderr, "field-kit baseline start: --at must be canonical UTC RFC3339")
		return 1
	}
	snapshot, facts, factsData, err := loadBaselineAndFacts(ctx, *catalogPath, *factsPath, detect)
	if err != nil {
		fmt.Fprintf(stderr, "field-kit baseline start: %v\n", err)
		return 1
	}
	entry, err := baseline.Find(snapshot, *selector)
	if err != nil || entry.Reference.Availability != "active" {
		if err == nil {
			err = fmt.Errorf("baseline is %s: %s", entry.Reference.Availability, entry.Reference.AvailabilityReason)
		}
		fmt.Fprintf(stderr, "field-kit baseline start: %v\n", err)
		return 1
	}
	if applicable := catalog.EvaluatePredicate(entry.Package.Applicability, facts); !applicable.Applicable {
		fmt.Fprintf(stderr, "field-kit baseline start: baseline is not applicable: %s\n", strings.Join(applicable.Reasons, "; "))
		return 1
	}
	disclosure := append([]byte(nil), disclosed...)
	if disclosed == nil {
		disclosure, err = readRegularFile(*disclosurePath)
	}
	if err != nil || !bytes.Equal(disclosure, baselineDisclosure(entry.Package, facts)) {
		fmt.Fprintln(stderr, "field-kit baseline start: disclosure bytes differ from the exact applicable baseline disclosure")
		return 1
	}
	if *temperPath == "" {
		*temperPath, err = os.Executable()
		if err != nil {
			fmt.Fprintf(stderr, "field-kit baseline start: locate executing Temper binary: %v\n", err)
			return 1
		}
	}
	temperAbsolute, err := filepath.Abs(*temperPath)
	if err != nil || filepath.Clean(temperAbsolute) != temperAbsolute {
		fmt.Fprintln(stderr, "field-kit baseline start: --temper must resolve to a clean path")
		return 1
	}
	temperData, err := readExecutable(temperAbsolute)
	if err != nil {
		fmt.Fprintf(stderr, "field-kit baseline start: Temper executable: %v\n", err)
		return 1
	}
	root, err := filepath.Abs(*rootFlag)
	if err != nil || filepath.Clean(root) != root || root == string(filepath.Separator) {
		fmt.Fprintln(stderr, "field-kit baseline start: --root must resolve to a clean dedicated path other than filesystem root")
		return 1
	}
	if *sessionPath == "" {
		*sessionPath = root + ".session.json"
	}
	if *id == "" {
		stamp := strings.ToLower(strings.ReplaceAll(*at, ":", ""))
		*id = entry.Package.ID + "-" + stamp
	}
	sessionAbsolute, err := filepath.Abs(*sessionPath)
	if err != nil || within(root, sessionAbsolute) {
		fmt.Fprintln(stderr, "field-kit baseline start: session must be outside the dedicated root")
		return 1
	}
	if _, err := os.Lstat(root); !errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintln(stderr, "field-kit baseline start: dedicated root must not already exist")
		return 1
	}
	if err := realDirectory(filepath.Dir(root)); err != nil {
		fmt.Fprintf(stderr, "field-kit baseline start: root parent: %v\n", err)
		return 1
	}
	if err := realDirectory(filepath.Dir(sessionAbsolute)); err != nil {
		fmt.Fprintf(stderr, "field-kit baseline start: session parent: %v\n", err)
		return 1
	}
	store, err := baselinerun.ReadStore(sessionAbsolute)
	if err != nil || store.Exists {
		fmt.Fprintln(stderr, "field-kit baseline start: session already exists or cannot be read")
		return 1
	}
	softwareLock, err := baselinerun.CompileSoftwareLock(entry.Package, facts)
	if err != nil {
		fmt.Fprintf(stderr, "field-kit baseline start: compile software lock: %v\n", err)
		return 1
	}
	document, marker, err := baselinerun.New(*id, snapshot, entry,
		baselinerun.FileIdentity{Path: filepath.Join(root, "field-kit", "machine.yaml"), SHA256: baselinerun.Digest(factsData)},
		baselinerun.FileIdentity{Path: temperAbsolute, SHA256: baselinerun.Digest(temperData)},
		root, *outcome, baselinerun.Digest(disclosure), baselinerun.Digest(softwareLock), *at)
	if err != nil {
		fmt.Fprintf(stderr, "field-kit baseline start: %v\n", err)
		return 1
	}
	markerData, _ := baselinerun.MarshalMarker(marker)
	sessionData, _ := baselinerun.Marshal(document, entry.Package)
	if err := createBaselineRoot(root, markerData, softwareLock, factsData, entry); err != nil {
		fmt.Fprintf(stderr, "field-kit baseline start: create dedicated root: %v\n", err)
		return 1
	}
	if err := store.Commit(ctx, sessionData); err != nil {
		_ = os.RemoveAll(root)
		fmt.Fprintf(stderr, "field-kit baseline start: commit session: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "FIELD-KIT BASELINE consented id=%s baseline=%s@%d outcome=%s session=%s root=%s next=%s\n", document.ID, entry.Package.ID, entry.Package.Revision, document.Outcome, sessionAbsolute, root, document.Stages[0].ID)
	return 0
}

func runBaselineStatus(arguments []string, stdout, stderr io.Writer) int {
	snapshot, entry, _, document, _, exit := loadBaselineSession(arguments, "status", stderr)
	_ = snapshot
	if exit != 0 {
		return exit
	}
	fmt.Fprintf(stdout, "FIELD-KIT BASELINE SESSION id=%s state=%s baseline=%s@%d outcome=%s generation=%s\n", document.ID, document.State, entry.Package.ID, entry.Package.Revision, document.Outcome, valueOr(document.Generation, "none"))
	for _, stage := range document.Stages {
		fmt.Fprintf(stdout, "STAGE %s %s operation=%s\n", stage.ID, stage.State, stage.Operation)
	}
	return 0
}

func runBaselineNext(ctx context.Context, arguments []string, stdout, stderr io.Writer, execute executor) int {
	_, entry, store, document, _, exit := loadBaselineSession(arguments, "run-next", stderr)
	if exit != 0 {
		return exit
	}
	if document.State == "complete" {
		fmt.Fprintln(stderr, "field-kit baseline run-next: session is complete")
		return 1
	}
	_, _, exit = advanceBaselineStage(ctx, "run-next", entry, store, document, stdout, stderr, execute)
	return exit
}

func runBaselineRun(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer, execute executor, detect FactsDetector) int {
	if len(arguments) > 0 && !strings.HasPrefix(arguments[0], "-") {
		return runBaselineGuided(ctx, arguments, input, stdout, stderr, execute, detect)
	}
	return runBaselineAll(ctx, arguments, stdout, stderr, execute)
}

func runBaselineGuided(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer, execute executor, detect FactsDetector) int {
	selector := arguments[0]
	flags := flag.NewFlagSet("field-kit baseline run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	catalogPath := flags.String("catalog", "", "override the embedded baseline catalog")
	factsPath := flags.String("facts", "", "canonical Temper machine facts override")
	temperPath := flags.String("temper", "", "Temper executable override (defaults to this binary)")
	rootFlag := flags.String("root", "", "new dedicated Temper root")
	outcome := flags.String("outcome", "", "keep or restore; prompted when omitted")
	sessionPath := flags.String("session", "", "session path outside the dedicated root")
	id := flags.String("id", "", "stable local session id")
	reportPath := flags.String("report", "", "final report path outside the dedicated root")
	at := flags.String("at", "", "canonical UTC RFC3339 consent time")
	if err := flags.Parse(arguments[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *rootFlag == "" {
		fmt.Fprintln(stderr, "field-kit baseline run: guided mode requires ID@REV and --root")
		return 2
	}
	root, err := filepath.Abs(*rootFlag)
	if err != nil || filepath.Clean(root) != root || root == string(filepath.Separator) {
		fmt.Fprintln(stderr, "field-kit baseline run: --root must resolve to a clean dedicated path other than filesystem root")
		return 1
	}
	if *sessionPath == "" {
		*sessionPath = root + ".session.json"
	}
	sessionAbsolute, err := filepath.Abs(*sessionPath)
	if err != nil || within(root, sessionAbsolute) {
		fmt.Fprintln(stderr, "field-kit baseline run: session must be outside the dedicated root")
		return 1
	}

	reader := bufio.NewReader(input)
	store, err := baselinerun.ReadStore(sessionAbsolute)
	if err != nil {
		fmt.Fprintf(stderr, "field-kit baseline run: session: %v\n", err)
		return 1
	}
	if store.Exists {
		_, _, _, document, _, exit := loadBaselineSessionValues(*catalogPath, selector, sessionAbsolute, "run", stderr)
		if exit != 0 {
			return exit
		}
		if document.Workspace.Root != root {
			fmt.Fprintf(stderr, "field-kit baseline run: --root %s differs from session root %s\n", root, document.Workspace.Root)
			return 1
		}
		if *outcome != "" && *outcome != document.Outcome {
			fmt.Fprintf(stderr, "field-kit baseline run: --outcome %s differs from session outcome %s\n", *outcome, document.Outcome)
			return 1
		}
		*outcome = document.Outcome
		fmt.Fprintf(stdout, "FIELD-KIT BASELINE resuming session=%s root=%s\n", sessionAbsolute, root)
		return runGuidedBaselineSession(ctx, selector, sessionAbsolute, *reportPath, *catalogPath, *outcome, reader, stdout, stderr, execute)
	}
	snapshot, err := loadBaselineCatalog(*catalogPath)
	if err != nil {
		fmt.Fprintf(stderr, "field-kit baseline run: %v\n", err)
		return 1
	}
	entry, err := baseline.Find(snapshot, selector)
	if err != nil {
		fmt.Fprintf(stderr, "field-kit baseline run: %v\n", err)
		return 1
	}
	if entry.Package.Mechanics.Orchestration != baseline.OrchestrationTemperMultiStageV1 {
		fmt.Fprintln(stderr, "field-kit baseline run: this immutable package authorizes only run-next")
		return 1
	}

	var disclosure bytes.Buffer
	explainArguments := []string{selector}
	if *factsPath != "" {
		explainArguments = append(explainArguments, "--facts", *factsPath)
	}
	if *catalogPath != "" {
		explainArguments = append(explainArguments, "--catalog", *catalogPath)
	}
	if exit := runBaselineExplain(ctx, explainArguments, &disclosure, stderr, detect); exit != 0 {
		return exit
	}
	_, _ = stdout.Write(disclosure.Bytes())

	if *outcome == "" {
		fmt.Fprint(stderr, "Field Kit outcome [keep/restore]: ")
		answer, err := readGuidedAnswer(reader)
		if err != nil {
			fmt.Fprintf(stderr, "\nfield-kit baseline run: read outcome: %v\n", err)
			return 1
		}
		*outcome = answer
	}
	if *outcome != "keep" && *outcome != "restore" {
		fmt.Fprintln(stderr, "field-kit baseline run: outcome must be keep or restore; no root or session was created")
		return 1
	}
	fmt.Fprintf(stderr, "Type yes to consent to %s with outcome %s: ", selector, *outcome)
	answer, err := readGuidedAnswer(reader)
	if err != nil {
		fmt.Fprintf(stderr, "\nfield-kit baseline run: read consent: %v\n", err)
		return 1
	}
	if answer != "yes" {
		fmt.Fprintln(stderr, "field-kit baseline run: consent declined; no root or session was created")
		return 1
	}

	startArguments := []string{
		"--baseline", selector, "--root", root, "--outcome", *outcome,
		"--session", sessionAbsolute, "--consent", "yes",
	}
	for _, optional := range []struct {
		name  string
		value string
	}{{"facts", *factsPath}, {"temper", *temperPath}, {"id", *id}, {"at", *at}, {"catalog", *catalogPath}} {
		if optional.value != "" {
			startArguments = append(startArguments, "--"+optional.name, optional.value)
		}
	}
	if exit := runBaselineStartWithDisclosure(ctx, startArguments, disclosure.Bytes(), stdout, stderr, detect); exit != 0 {
		return exit
	}
	return runGuidedBaselineSession(ctx, selector, sessionAbsolute, *reportPath, *catalogPath, *outcome, reader, stdout, stderr, execute)
}

func runGuidedBaselineSession(ctx context.Context, selector, sessionPath, reportPath, catalogPath, outcome string, input *bufio.Reader, stdout, stderr io.Writer, execute executor) int {
	runArguments := []string{"--session", sessionPath, "--baseline", selector}
	if catalogPath != "" {
		runArguments = append(runArguments, "--catalog", catalogPath)
	}
	if exit := runBaselineAll(ctx, runArguments, stdout, stderr, execute); exit != 0 {
		return exit
	}
	finishArguments := []string{"--session", sessionPath, "--baseline", selector}
	if reportPath != "" {
		finishArguments = append(finishArguments, "--report", reportPath)
	}
	if catalogPath != "" {
		finishArguments = append(finishArguments, "--catalog", catalogPath)
	}
	if outcome == "restore" {
		fmt.Fprint(stderr, "Type yes to confirm removal of the dedicated Field Kit root: ")
		answer, err := readGuidedAnswer(input)
		if err != nil {
			fmt.Fprintf(stderr, "\nfield-kit baseline run: read restore confirmation: %v\n", err)
			return 1
		}
		if answer != "yes" {
			fmt.Fprintln(stderr, "field-kit baseline run: restore confirmation declined; the dedicated root was retained")
			return 1
		}
		finishArguments = append(finishArguments, "--confirm-restore", "yes")
	}
	return runBaselineFinish(ctx, finishArguments, stdout, stderr)
}

func readGuidedAnswer(reader *bufio.Reader) (string, error) {
	answer, err := reader.ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && answer != "") {
		return "", err
	}
	return strings.TrimSpace(answer), nil
}

func runBaselineAll(ctx context.Context, arguments []string, stdout, stderr io.Writer, execute executor) int {
	_, entry, store, document, _, exit := loadBaselineSession(arguments, "run", stderr)
	if exit != 0 {
		return exit
	}
	if entry.Package.Mechanics.Orchestration != baseline.OrchestrationTemperMultiStageV1 {
		fmt.Fprintln(stderr, "field-kit baseline run: this immutable package authorizes only run-next")
		return 1
	}
	if document.State == "complete" {
		fmt.Fprintf(stdout, "FIELD-KIT BASELINE already-complete id=%s next=none\n", document.ID)
		return 0
	}
	for {
		if _, _, pending := document.Next(); !pending {
			fmt.Fprintf(stdout, "FIELD-KIT BASELINE stages-complete id=%s next=finish\n", document.ID)
			return 0
		}
		if err := ctx.Err(); err != nil {
			fmt.Fprintf(stderr, "field-kit baseline run: interrupted before next stage: %v\n", err)
			return 1
		}
		store, document, exit = advanceBaselineStage(ctx, "run", entry, store, document, stdout, stderr, execute)
		if exit != 0 {
			return exit
		}
	}
}

func advanceBaselineStage(ctx context.Context, command string, entry baseline.Entry, store baselinerun.Store, document baselinerun.Document, stdout, stderr io.Writer, execute executor) (baselinerun.Store, baselinerun.Document, int) {
	if err := verifySessionInputs(document, entry); err != nil {
		fmt.Fprintf(stderr, "field-kit baseline %s: %v\n", command, err)
		return store, document, 1
	}
	invocation, err := baselinerun.Plan(document, entry)
	if err != nil {
		fmt.Fprintf(stderr, "field-kit baseline %s: %v\n", command, err)
		return store, document, 1
	}
	if err := ensureRealDirectories(document.Workspace.Root, filepath.Dir(invocation.OutputPath)); err != nil {
		fmt.Fprintf(stderr, "field-kit baseline %s: evidence directory: %v\n", command, err)
		return store, document, 1
	}
	var output bytes.Buffer
	if invocation.NoProcess {
		fmt.Fprintf(&output, "RESULT baseline-outcome kept root=%s\n", document.Workspace.Root)
		_, _ = stdout.Write(output.Bytes())
	} else if invocation.Protocol != nil {
		fmt.Fprintf(stdout, "FIELD-KIT BASELINE running stage=%s program=temper\n", invocation.StageID)
		if err := execute.RunProtocol(ctx, *invocation.Protocol, io.MultiWriter(stdout, &output), stderr); err != nil {
			_ = writeAtomic(invocation.OutputPath+".failed", output.Bytes(), 0o600)
			fmt.Fprintf(stderr, "field-kit baseline %s: stage %s failed without advancing the session: %v\n", command, invocation.StageID, err)
			return store, document, 1
		}
	} else {
		fmt.Fprintf(stdout, "FIELD-KIT BASELINE running stage=%s program=%s\n", invocation.StageID, filepath.Base(invocation.Path))
		if err := execute.Run(ctx, invocation.Path, invocation.Arguments, io.MultiWriter(stdout, &output), stderr); err != nil {
			_ = writeAtomic(invocation.OutputPath+".failed", output.Bytes(), 0o600)
			fmt.Fprintf(stderr, "field-kit baseline %s: stage %s failed without advancing the session: %v\n", command, invocation.StageID, err)
			return store, document, 1
		}
	}
	if err := validateStageOutput(document, entry, invocation, output.Bytes()); err != nil {
		_ = writeAtomic(invocation.OutputPath+".failed", output.Bytes(), 0o600)
		fmt.Fprintf(stderr, "field-kit baseline %s: stage %s output refusal: %v\n", command, invocation.StageID, err)
		return store, document, 1
	}
	if err := writeAtomic(invocation.OutputPath, output.Bytes(), 0o600); err != nil {
		fmt.Fprintf(stderr, "field-kit baseline %s: retain stage evidence: %v\n", command, err)
		return store, document, 1
	}
	evidence := baselinerun.FileIdentity{Path: invocation.OutputPath, SHA256: baselinerun.Digest(output.Bytes())}
	generation := ""
	var binding, protocol *baselinerun.FileIdentity
	_, stage, _ := document.Next()
	switch stage.Operation {
	case "config-apply":
		generation, _ = baselinerun.ParseApplyGeneration(output.Bytes())
	case "material-bind":
		binding = &evidence
	case "live-protocol":
		path := filepath.Join(document.Workspace.Root, "field-kit", "protocol-report.json")
		data, _ := readRegularFile(path)
		item := baselinerun.FileIdentity{Path: path, SHA256: baselinerun.Digest(data)}
		protocol = &item
		evidence = item
	}
	next, err := document.CompleteStage(entry.Package, invocation.StageID, time.Now().UTC().Format(time.RFC3339Nano), evidence, generation, binding, protocol)
	if err != nil {
		fmt.Fprintf(stderr, "field-kit baseline %s: record stage: %v\n", command, err)
		return store, document, 1
	}
	data, _ := baselinerun.Marshal(next, entry.Package)
	if err := store.Commit(context.Background(), data); err != nil {
		fmt.Fprintf(stderr, "field-kit baseline %s: commit stage: %v\n", command, err)
		return store, document, 1
	}
	_, upcoming, pending := next.Next()
	if pending {
		fmt.Fprintf(stdout, "FIELD-KIT BASELINE completed stage=%s next=%s\n", invocation.StageID, upcoming.ID)
	} else {
		fmt.Fprintf(stdout, "FIELD-KIT BASELINE completed stage=%s next=finish\n", invocation.StageID)
	}
	return baselinerun.Store{Path: store.Path, Data: data, Exists: true}, next, 0
}

func runBaselineFinish(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("field-kit baseline finish", flag.ContinueOnError)
	flags.SetOutput(stderr)
	catalogPath := flags.String("catalog", "", "override the embedded baseline catalog")
	selector := flags.String("baseline", "", "exact baseline id@revision")
	sessionPath := flags.String("session", "", "baseline session")
	reportPath := flags.String("report", "", "final report path outside the dedicated root")
	confirmRestore := flags.String("confirm-restore", "", "must be yes for a restore outcome")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *sessionPath == "" {
		fmt.Fprintln(stderr, "field-kit baseline finish: --session is required")
		return 2
	}
	_, entry, store, document, sessionAbsolute, exit := loadBaselineSessionValues(*catalogPath, *selector, *sessionPath, "finish", stderr)
	if exit != 0 {
		return exit
	}
	if _, _, pending := document.Next(); pending {
		fmt.Fprintln(stderr, "field-kit baseline finish: baseline stages remain; use run or run-next")
		return 1
	}
	if *reportPath == "" {
		*reportPath = document.Workspace.Root + ".report.md"
	}
	reportAbsolute, err := filepath.Abs(*reportPath)
	if err != nil || within(document.Workspace.Root, reportAbsolute) || within(document.Workspace.Root, sessionAbsolute) {
		fmt.Fprintln(stderr, "field-kit baseline finish: report and session must remain outside the dedicated root")
		return 1
	}
	if document.Outcome == "restore" {
		if *confirmRestore != "yes" {
			fmt.Fprintln(stderr, "field-kit baseline finish: restore outcome requires --confirm-restore yes; the dedicated root was retained")
			return 1
		}
	}
	report := renderBaselineReport(document, entry.Package)
	if err := writeNewOrSame(reportAbsolute, report, 0o600); err != nil {
		fmt.Fprintf(stderr, "field-kit baseline finish: write report: %v\n", err)
		return 1
	}
	if document.Outcome == "restore" {
		if _, err := os.Lstat(document.Workspace.Root); err == nil {
			if err := baselinerun.VerifyMarker(document.Workspace.Root, document.Workspace.MarkerSHA256); err != nil {
				fmt.Fprintf(stderr, "field-kit baseline finish: restore refusal: %v\n", err)
				return 1
			}
			if err := os.RemoveAll(document.Workspace.Root); err != nil {
				fmt.Fprintf(stderr, "field-kit baseline finish: restore dedicated root: %v\n", err)
				return 1
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintf(stderr, "field-kit baseline finish: inspect dedicated root: %v\n", err)
			return 1
		}
	}
	next, err := document.Finish(entry.Package, baselinerun.FileIdentity{Path: reportAbsolute, SHA256: baselinerun.Digest(report)})
	if err != nil {
		fmt.Fprintf(stderr, "field-kit baseline finish: %v\n", err)
		return 1
	}
	data, _ := baselinerun.Marshal(next, entry.Package)
	if err := store.Commit(ctx, data); err != nil {
		fmt.Fprintf(stderr, "field-kit baseline finish: commit session: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "FIELD-KIT BASELINE complete id=%s outcome=%s report=%s sha256=%s\n", next.ID, next.Outcome, reportAbsolute, next.Report.SHA256)
	return 0
}

func loadBaselineSession(arguments []string, command string, stderr io.Writer) (baseline.Snapshot, baseline.Entry, baselinerun.Store, baselinerun.Document, string, int) {
	flags := flag.NewFlagSet("field-kit baseline "+command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	catalogPath := flags.String("catalog", "", "override the embedded baseline catalog")
	selector := flags.String("baseline", "", "exact baseline id@revision")
	sessionPath := flags.String("session", "", "baseline session path")
	if err := flags.Parse(arguments); err != nil {
		return baseline.Snapshot{}, baseline.Entry{}, baselinerun.Store{}, baselinerun.Document{}, "", 2
	}
	if flags.NArg() != 0 || *sessionPath == "" {
		fmt.Fprintf(stderr, "field-kit baseline %s: --session is required\n", command)
		return baseline.Snapshot{}, baseline.Entry{}, baselinerun.Store{}, baselinerun.Document{}, "", 2
	}
	return loadBaselineSessionValues(*catalogPath, *selector, *sessionPath, command, stderr)
}

func loadBaselineSessionValues(catalogPath, selector, sessionPath, command string, stderr io.Writer) (baseline.Snapshot, baseline.Entry, baselinerun.Store, baselinerun.Document, string, int) {
	sessionAbsolute, err := filepath.Abs(sessionPath)
	if err != nil {
		fmt.Fprintf(stderr, "field-kit baseline %s: session path: %v\n", command, err)
		return baseline.Snapshot{}, baseline.Entry{}, baselinerun.Store{}, baselinerun.Document{}, "", 1
	}
	store, err := baselinerun.ReadStore(sessionAbsolute)
	if err != nil || !store.Exists {
		fmt.Fprintf(stderr, "field-kit baseline %s: session does not exist or cannot be read\n", command)
		return baseline.Snapshot{}, baseline.Entry{}, store, baselinerun.Document{}, "", 1
	}
	identity, err := baselinerun.Identify(store.Data)
	if err != nil {
		fmt.Fprintf(stderr, "field-kit baseline %s: session identity refusal: %v\n", command, err)
		return baseline.Snapshot{}, baseline.Entry{}, store, baselinerun.Document{}, "", 1
	}
	identifiedSelector := fmt.Sprintf("%s@%d", identity.ID, identity.Revision)
	if selector != "" && selector != identifiedSelector {
		fmt.Fprintf(stderr, "field-kit baseline %s: --baseline %s differs from session baseline %s\n", command, selector, identifiedSelector)
		return baseline.Snapshot{}, baseline.Entry{}, store, baselinerun.Document{}, "", 1
	}
	snapshot, err := loadBaselineCatalog(catalogPath)
	if err != nil {
		fmt.Fprintf(stderr, "field-kit baseline %s: %v\n", command, err)
		return baseline.Snapshot{}, baseline.Entry{}, baselinerun.Store{}, baselinerun.Document{}, "", 1
	}
	entry, err := baseline.Find(snapshot, identifiedSelector)
	if err != nil {
		fmt.Fprintf(stderr, "field-kit baseline %s: %v\n", command, err)
		return snapshot, baseline.Entry{}, baselinerun.Store{}, baselinerun.Document{}, "", 1
	}
	document, err := baselinerun.Parse(store.Data, entry.Package)
	if err != nil || document.Baseline.PackageSHA256 != entry.Reference.PackageSHA256 {
		fmt.Fprintf(stderr, "field-kit baseline %s: session differs from immutable package: %v\n", command, err)
		return snapshot, entry, store, baselinerun.Document{}, "", 1
	}
	return snapshot, entry, store, document, sessionAbsolute, 0
}

func loadBaselineCatalog(catalogPath string) (baseline.Snapshot, error) {
	if catalogPath == "" {
		return baseline.LoadBuiltin()
	}
	return baseline.Load(catalogPath)
}

func loadBaselineAndFacts(ctx context.Context, catalogPath, factsPath string, detect FactsDetector) (baseline.Snapshot, catalog.MachineFacts, []byte, error) {
	snapshot, err := loadBaselineCatalog(catalogPath)
	if err != nil {
		return baseline.Snapshot{}, catalog.MachineFacts{}, nil, err
	}
	var data []byte
	if factsPath != "" {
		data, err = readRegularFile(factsPath)
		if err != nil {
			return baseline.Snapshot{}, catalog.MachineFacts{}, nil, err
		}
		if _, err := machine.ParseFacts(data); err != nil {
			return baseline.Snapshot{}, catalog.MachineFacts{}, nil, err
		}
	} else {
		if detect == nil {
			return baseline.Snapshot{}, catalog.MachineFacts{}, nil, errors.New("machine facts detector is unavailable; provide --facts")
		}
		detected, err := detect(ctx)
		if err != nil {
			return baseline.Snapshot{}, catalog.MachineFacts{}, nil, fmt.Errorf("detect machine facts: %w", err)
		}
		data, err = machine.MarshalFacts(detected)
		if err != nil {
			return baseline.Snapshot{}, catalog.MachineFacts{}, nil, fmt.Errorf("encode machine facts: %w", err)
		}
	}
	facts, err := catalog.ParseMachineFacts(data)
	return snapshot, facts, data, err
}

func validateStageOutput(document baselinerun.Document, entry baseline.Entry, invocation baselinerun.Invocation, output []byte) error {
	_, stage, _ := document.Next()
	switch stage.Operation {
	case "software-install":
		if !bytes.Contains(output, []byte("RESULT software-install ")) {
			return errors.New("missing Temper software-install result")
		}
	case "model-fetch":
		if !bytes.Contains(output, []byte("RESULT fetch ")) {
			return errors.New("missing Temper fetch result")
		}
	case "config-apply":
		_, err := baselinerun.ParseApplyGeneration(output)
		return err
	case "software-check":
		if !bytes.Contains(output, []byte("RESULT software-check exact ")) {
			return errors.New("software check was not exact")
		}
	case "artifact-check":
		if !bytes.Contains(output, []byte("RESULT check ok ")) {
			return errors.New("artifact check was not ok")
		}
	case "material-bind":
		if !bytes.HasPrefix(output, []byte("schema: temper-field-kit-binding/v1\n")) {
			return errors.New("binding output has the wrong schema")
		}
	case "live-protocol":
		data, err := readRegularFile(filepath.Join(document.Workspace.Root, "field-kit", "protocol-report.json"))
		if err != nil {
			return err
		}
		var report struct {
			Schema string `json:"schema"`
			Status string `json:"status"`
		}
		if invocation.Protocol == nil {
			return errors.New("live protocol invocation is absent")
		}
		if err := json.Unmarshal(data, &report); err != nil || report.Schema != invocation.Protocol.Schema || report.Status != "pass" {
			return errors.New("live protocol report is not a pass")
		}
	case "outcome":
		if document.Outcome == "keep" && !bytes.Contains(output, []byte("RESULT baseline-outcome kept ")) {
			return errors.New("keep outcome result is missing")
		}
		if document.Outcome == "restore" && !bytes.Contains(output, []byte("RESULT software-remove ")) {
			return errors.New("restore outcome software removal result is missing")
		}
	}
	_ = entry
	_ = invocation
	return nil
}

func verifySessionInputs(document baselinerun.Document, entry baseline.Entry) error {
	temper, err := readExecutable(document.Temper.Path)
	if err != nil || baselinerun.Digest(temper) != document.Temper.SHA256 {
		return errors.New("Temper executable bytes differ from the consented session")
	}
	if err := baselinerun.VerifyMarker(document.Workspace.Root, document.Workspace.MarkerSHA256); err != nil {
		return err
	}
	lock, err := readRegularFile(document.Workspace.SoftwareLock.Path)
	if err != nil || baselinerun.Digest(lock) != document.Workspace.SoftwareLock.SHA256 {
		return errors.New("compiled software lock differs from the consented session")
	}
	machineData, err := readRegularFile(document.Machine.Path)
	if err != nil || baselinerun.Digest(machineData) != document.Machine.SHA256 {
		return errors.New("machine facts differ from the consented session")
	}
	packageData, err := readRegularFile(filepath.Join(document.Workspace.PackageRoot, "package.json"))
	if err != nil || baselinerun.Digest(packageData) != entry.Reference.PackageSHA256 {
		return errors.New("baseline package differs from the consented session")
	}
	for name, expected := range entry.Files {
		data, err := readRegularFile(filepath.Join(document.Workspace.PackageRoot, filepath.FromSlash(name)))
		if err != nil || !bytes.Equal(data, expected) {
			return fmt.Errorf("baseline package material %q differs from the consented session", name)
		}
	}
	return nil
}

func createBaselineRoot(root string, marker, softwareLock, facts []byte, entry baseline.Entry) (returnErr error) {
	if err := os.Mkdir(root, 0o700); err != nil {
		return err
	}
	committed := false
	defer func() {
		if returnErr != nil && !committed {
			_ = os.RemoveAll(root)
		}
	}()
	fieldKit := filepath.Join(root, "field-kit")
	if err := os.Mkdir(fieldKit, 0o700); err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(root, ".temper-field-kit-owner.json"), marker, 0o600); err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(fieldKit, "software.lock.yaml"), softwareLock, 0o600); err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(fieldKit, "machine.yaml"), facts, 0o600); err != nil {
		return err
	}
	packageRoot := filepath.Join(fieldKit, "package")
	if err := os.Mkdir(packageRoot, 0o700); err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(packageRoot, "package.json"), entry.PackageData, 0o600); err != nil {
		return err
	}
	names := make([]string, 0, len(entry.Files))
	for name := range entry.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		destination := filepath.Join(packageRoot, filepath.FromSlash(name))
		parent := filepath.Dir(destination)
		if parent != packageRoot {
			if err := ensureRealDirectories(packageRoot, parent); err != nil {
				return err
			}
		}
		if err := writeAtomic(destination, entry.Files[name], 0o600); err != nil {
			return err
		}
	}
	committed = true
	return nil
}

func writeAtomic(path string, data []byte, mode fs.FileMode) error {
	directory := filepath.Dir(path)
	stage, err := os.CreateTemp(directory, ".field-kit-stage-*")
	if err != nil {
		return err
	}
	stagePath := stage.Name()
	defer os.Remove(stagePath)
	if err := stage.Chmod(mode); err != nil {
		stage.Close()
		return err
	}
	if _, err := stage.Write(data); err != nil {
		stage.Close()
		return err
	}
	if err := stage.Sync(); err != nil {
		stage.Close()
		return err
	}
	if err := stage.Close(); err != nil {
		return err
	}
	if err := os.Rename(stagePath, path); err != nil {
		return err
	}
	dir, err := os.Open(directory)
	if err == nil {
		err = dir.Sync()
		dir.Close()
	}
	return err
}

func writeNewOrSame(path string, data []byte, mode fs.FileMode) error {
	existing, err := readRegularFile(path)
	if err == nil {
		if bytes.Equal(existing, data) {
			return nil
		}
		return errors.New("destination already exists with different bytes")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return writeAtomic(path, data, mode)
}

func ensureRealDirectories(root, target string) error {
	if !within(root, target) {
		return errors.New("evidence directory escapes the dedicated root")
	}
	current := root
	relative, _ := filepath.Rel(root, target)
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			if err := os.Mkdir(current, 0o700); err != nil {
				return err
			}
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
			return errors.New("evidence path contains a file or symlink")
		}
	}
	return nil
}

func readRegularFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&fs.ModeSymlink != 0 {
		return nil, errors.New("expected a regular file without symlink indirection")
	}
	return os.ReadFile(path)
}

func readExecutable(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&fs.ModeSymlink != 0 || info.Mode().Perm()&0o111 == 0 {
		return nil, errors.New("expected an executable regular file without symlink indirection")
	}
	return os.ReadFile(path)
}

func realDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
		return errors.New("expected a real directory")
	}
	return nil
}

func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func renderBaselineReport(document baselinerun.Document, promoted baseline.Package) []byte {
	var output bytes.Buffer
	fmt.Fprintf(&output, "# Field Kit baseline report — %s\n\n", promoted.Title)
	fmt.Fprintf(&output, "- Session: `%s`\n- Baseline: `%s@%d`\n- State: complete\n- Outcome: `%s`\n", document.ID, promoted.ID, promoted.Revision, document.Outcome)
	fmt.Fprintf(&output, "- Catalog SHA-256: `%s`\n- Package SHA-256: `%s`\n- Source SHA-256: `%s`\n", document.Catalog.SHA256, document.Baseline.PackageSHA256, document.Baseline.SourceSHA256)
	fmt.Fprintf(&output, "- Temper SHA-256: `%s`\n- Machine facts SHA-256: `%s`\n- Rendered generation: `%s`\n", document.Temper.SHA256, document.Machine.SHA256, document.Generation)
	if document.Binding != nil {
		fmt.Fprintf(&output, "- Material binding SHA-256: `%s`\n", document.Binding.SHA256)
	}
	if document.Protocol != nil {
		fmt.Fprintf(&output, "- Protocol report SHA-256: `%s`\n", document.Protocol.SHA256)
	}
	fmt.Fprint(&output, "\n## Completed stages\n\n")
	for _, stage := range document.Stages {
		fmt.Fprintf(&output, "- `%s` — %s at %s; evidence `%s`\n", stage.ID, stage.State, stage.CompletedAt, stage.Evidence.SHA256)
	}
	fmt.Fprintln(&output, "\nGenerated content is not retained. This local report is not uploaded automatically and does not promote a product recommendation.")
	return output.Bytes()
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func joined(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}

func baselineUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage:")
	fmt.Fprintln(writer, "  field-kit baseline verify [--catalog PATH]")
	fmt.Fprintln(writer, "  field-kit baseline inspect [--facts PATH] [--catalog PATH]")
	fmt.Fprintln(writer, "  field-kit baseline explain ID@REV [--facts PATH] [--catalog PATH]")
	fmt.Fprintln(writer, "  field-kit baseline run ID@REV --root NEW_PATH [--outcome keep|restore] [--session PATH] [--report PATH] [--facts PATH] [--temper PATH] [--catalog PATH]")
	fmt.Fprintln(writer, "  field-kit baseline start --baseline ID@REV --root NEW_PATH --disclosure PATH --outcome keep|restore --consent yes [--session PATH] [--id ID] [--facts PATH] [--temper PATH] [--at UTC_RFC3339] [--catalog PATH]")
	fmt.Fprintln(writer, "  field-kit baseline status --session PATH [--baseline ID@REV] [--catalog PATH]")
	fmt.Fprintln(writer, "  field-kit baseline run --session PATH [--baseline ID@REV] [--catalog PATH]")
	fmt.Fprintln(writer, "  field-kit baseline run-next --session PATH [--baseline ID@REV] [--catalog PATH]")
	fmt.Fprintln(writer, "  field-kit baseline finish --session PATH [--baseline ID@REV] [--report PATH] [--confirm-restore yes] [--catalog PATH]")
}
