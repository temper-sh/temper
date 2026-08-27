package fieldkitprotocol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	protocolLimit = 20 * time.Minute
	startupLimit  = 5 * time.Minute
	swapStopMiB   = 512.0
)

var swapPattern = regexp.MustCompile(`used = ([0-9.]+)M`)

type Options struct {
	ID           string
	Revision     int
	Schema       string
	TemperPath   string
	Root         string
	SoftwareLock string
	Generation   string
	Installation string
	Model        string
	Listen       string
	Report       string
	LogDirectory string
}

type Runner struct {
	probe     Probe
	resources ResourceReader
	client    Client
	now       func() time.Time
}

type Probe interface {
	DryRun(context.Context, ProbeOptions) error
	Start(context.Context, ProbeOptions, io.Writer, io.Writer) (RunningProbe, error)
}

type RunningProbe interface {
	BaseURL() string
	Exited() (bool, error)
	Stop() error
}

type ProbeOptions struct {
	TemperPath   string
	Root         string
	Installation string
	SoftwareLock string
	Generation   string
	Listen       string
}

type ResourceReader interface {
	SwapMiB(context.Context) (float64, error)
	Thermal(context.Context) (string, error)
}

func NewRunner() (Runner, error) {
	client, err := NewClient(&http.Client{})
	if err != nil {
		return Runner{}, err
	}
	return Runner{probe: ProcessProbe{}, resources: SystemResources{}, client: client, now: time.Now}, nil
}

func NewRunnerWith(probe Probe, resources ResourceReader, httpClient *http.Client, now func() time.Time) (Runner, error) {
	if probe == nil || resources == nil || now == nil {
		return Runner{}, errors.New("Field Kit protocol runner dependencies are required")
	}
	client, err := NewClient(httpClient)
	if err != nil {
		return Runner{}, err
	}
	return Runner{probe: probe, resources: resources, client: client, now: now}, nil
}

func (r Runner) Run(ctx context.Context, options Options, stdout, _ io.Writer) error {
	if !Supports(options.ID, options.Revision, options.Schema) {
		return fmt.Errorf("unsupported Temper Field Kit protocol %s@%d (%s)", options.ID, options.Revision, options.Schema)
	}
	if options.TemperPath == "" || options.Root == "" || options.SoftwareLock == "" || options.Generation == "" || options.Installation == "" || options.Model == "" || options.Listen == "" || options.Report == "" || options.LogDirectory == "" {
		return errors.New("Temper Field Kit protocol invocation is incomplete")
	}
	runCtx, cancel := context.WithTimeout(ctx, protocolLimit)
	defer cancel()
	swapStart, err := r.resources.SwapMiB(runCtx)
	if err != nil {
		return fmt.Errorf("read initial swap: %w", err)
	}
	report := Report{
		Schema: options.Schema, Status: "running", Model: options.Model, Generation: options.Generation,
		StartedAt: r.now().UTC().Format(time.RFC3339Nano), SwapStartMiB: swapStart,
		Checks: map[string]any{}, Resources: []ResourceSnapshot{},
	}
	if err := writeReport(options.Report, report); err != nil {
		return fmt.Errorf("write initial protocol report: %w", err)
	}
	if err := ensureDirectory(options.LogDirectory); err != nil {
		return fmt.Errorf("create protocol log directory: %w", err)
	}
	stdoutPath := filepath.Join(options.LogDirectory, "probe.stdout")
	stderrPath := filepath.Join(options.LogDirectory, "probe.stderr")
	probeOptions := ProbeOptions{
		TemperPath: options.TemperPath, Root: options.Root, Installation: options.Installation,
		SoftwareLock: options.SoftwareLock, Generation: options.Generation, Listen: options.Listen,
	}
	if err := r.probe.DryRun(runCtx, probeOptions); err != nil {
		report.Status = "fail"
		report.Error = "probe admission: " + err.Error()
		return finishReport(options.Report, stdoutPath, stderrPath, &report, r.now, stdout, errors.New(report.Error))
	}
	probeStdout, err := openNewLog(stdoutPath)
	if err != nil {
		return err
	}
	probeStderr, err := openNewLog(stderrPath)
	if err != nil {
		_ = probeStdout.Close()
		return err
	}
	logsClosed := false
	closeLogs := func() {
		if logsClosed {
			return
		}
		_ = probeStdout.Sync()
		_ = probeStderr.Sync()
		_ = probeStdout.Close()
		_ = probeStderr.Close()
		logsClosed = true
	}
	defer closeLogs()
	running, err := r.probe.Start(runCtx, probeOptions, probeStdout, probeStderr)
	if err != nil {
		closeLogs()
		report.Status = "fail"
		report.Error = "start isolated router: " + err.Error()
		return finishReport(options.Report, stdoutPath, stderrPath, &report, r.now, stdout, errors.New(report.Error))
	}
	stopped := false
	defer func() {
		if !stopped {
			_ = running.Stop()
		}
	}()
	finish := func(runErr error) error {
		stopErr := running.Stop()
		stopped = true
		closeLogs()
		if stopErr != nil && runErr == nil {
			runErr = fmt.Errorf("stop isolated router: %w", stopErr)
			report.Status = "fail"
			report.Error = runErr.Error()
		}
		return finishReport(options.Report, stdoutPath, stderrPath, &report, r.now, stdout, runErr)
	}
	if err := waitReady(runCtx, r.client, running); err != nil {
		report.Status = "fail"
		report.Error = err.Error()
		return finish(err)
	}
	snapshot := func(ctx context.Context) (ResourceSnapshot, error) {
		return r.resourceSnapshot(ctx, swapStart)
	}
	checks, resources, err := r.client.Exercise(runCtx, running.BaseURL(), options.Model, snapshot)
	if err != nil {
		report.Status = "fail"
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			report.Status = "interrupted"
		}
		report.Error = err.Error()
	} else {
		report.Status = "pass"
		report.Checks = checks
		report.Resources = resources
	}
	return finish(err)
}

type Report struct {
	Schema       string             `json:"schema"`
	Status       string             `json:"status"`
	Model        string             `json:"model"`
	Generation   string             `json:"generation"`
	StartedAt    string             `json:"started_at"`
	FinishedAt   string             `json:"finished_at,omitempty"`
	SwapStartMiB float64            `json:"swap_start_mib"`
	Checks       map[string]any     `json:"checks"`
	Resources    []ResourceSnapshot `json:"resources"`
	Error        string             `json:"error,omitempty"`
	ProbeStdout  *FileEvidence      `json:"probe_stdout,omitempty"`
	ProbeStderr  *FileEvidence      `json:"probe_stderr,omitempty"`
}

type FileEvidence struct {
	Name   string `json:"name"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

func (r Runner) resourceSnapshot(ctx context.Context, swapStart float64) (ResourceSnapshot, error) {
	swap, err := r.resources.SwapMiB(ctx)
	if err != nil {
		return ResourceSnapshot{}, fmt.Errorf("read swap: %w", err)
	}
	thermal, err := r.resources.Thermal(ctx)
	if err != nil {
		return ResourceSnapshot{}, fmt.Errorf("read thermal status: %w", err)
	}
	lower := strings.ToLower(thermal)
	if strings.Contains(lower, "thermal warning level has been recorded") && !strings.Contains(lower, "no thermal warning") {
		return ResourceSnapshot{}, errors.New("thermal warning was recorded")
	}
	if strings.Contains(lower, "performance warning level has been recorded") && !strings.Contains(lower, "no performance warning") {
		return ResourceSnapshot{}, errors.New("performance warning was recorded")
	}
	growth := swap - swapStart
	if growth >= swapStopMiB {
		return ResourceSnapshot{}, fmt.Errorf("swap grew %.1f MiB; 512 MiB stop reached", growth)
	}
	return ResourceSnapshot{
		SwapMiB: roundThree(swap), SwapGrowthMiB: roundThree(growth), ThermalSHA256: digest([]byte(thermal)),
		ChildPeakMemory: map[string]any{"enforced": false, "reason": "portable router-root process telemetry is not exposed"},
	}, nil
}

type SystemResources struct{}

func (SystemResources) SwapMiB(ctx context.Context) (float64, error) {
	output, err := controlledCommand(ctx, "/usr/sbin/sysctl", "-n", "vm.swapusage")
	if err != nil {
		return 0, err
	}
	match := swapPattern.FindStringSubmatch(string(output))
	if len(match) != 2 {
		return 0, errors.New("could not parse vm.swapusage")
	}
	return strconv.ParseFloat(match[1], 64)
}

func (SystemResources) Thermal(ctx context.Context) (string, error) {
	output, err := controlledCommand(ctx, "/usr/bin/pmset", "-g", "therm")
	return strings.TrimSpace(string(output)), err
}

func controlledCommand(ctx context.Context, path string, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, path, arguments...)
	command.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin"}
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return output, nil
}

func waitReady(ctx context.Context, client Client, running RunningProbe) error {
	startup, cancel := context.WithTimeout(ctx, startupLimit)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		exited, err := running.Exited()
		if exited {
			if err == nil {
				err = errors.New("process exited")
			}
			return fmt.Errorf("isolated router exited during startup: %w", err)
		}
		request, err := http.NewRequestWithContext(startup, http.MethodGet, strings.TrimRight(running.BaseURL(), "/")+"/health", nil)
		if err == nil {
			response, requestErr := client.http.Do(request)
			if requestErr == nil {
				data, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
				response.Body.Close()
				value := strings.TrimSpace(string(data))
				if response.StatusCode >= 200 && response.StatusCode < 300 && (value == "OK" || strings.HasPrefix(value, "{")) {
					return nil
				}
			}
		}
		select {
		case <-startup.Done():
			return errors.New("isolated router did not become healthy within 300 seconds")
		case <-ticker.C:
		}
	}
}

func finishReport(path, stdoutPath, stderrPath string, report *Report, now func() time.Time, stdout io.Writer, returnErr error) error {
	report.FinishedAt = now().UTC().Format(time.RFC3339Nano)
	for _, item := range []struct {
		path   string
		target **FileEvidence
	}{{stdoutPath, &report.ProbeStdout}, {stderrPath, &report.ProbeStderr}} {
		data, err := readRegular(item.path)
		if err == nil {
			*item.target = &FileEvidence{Name: filepath.Base(item.path), Bytes: len(data), SHA256: digest(data)}
		}
	}
	if err := writeReport(path, *report); err != nil {
		return fmt.Errorf("write final protocol report: %w", err)
	}
	summary, _ := json.Marshal(map[string]any{"schema": report.Schema, "status": report.Status, "report": path})
	fmt.Fprintln(stdout, string(summary))
	return returnErr
}

func writeReport(path string, report Report) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	directory := filepath.Dir(path)
	if err := ensureDirectory(directory); err != nil {
		return err
	}
	stage, err := os.CreateTemp(directory, ".temper-field-kit-report-*")
	if err != nil {
		return err
	}
	stagePath := stage.Name()
	defer os.Remove(stagePath)
	if err := stage.Chmod(0o600); err != nil {
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
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func ensureDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return os.MkdirAll(path, 0o700)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
		return errors.New("expected a real directory")
	}
	return nil
}

func openNewLog(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o600)
}

func readRegular(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&fs.ModeSymlink != 0 {
		return nil, errors.New("expected a regular file without symlink indirection")
	}
	return os.ReadFile(path)
}

func roundThree(value float64) float64 {
	result, _ := strconv.ParseFloat(fmt.Sprintf("%.3f", value), 64)
	return result
}

type ProcessProbe struct{}

func (ProcessProbe) DryRun(ctx context.Context, options ProbeOptions) error {
	var stderr bytes.Buffer
	command := exec.CommandContext(ctx, options.TemperPath, append(probeArguments(options), "--dry-run")...)
	command.Stdout = io.Discard
	command.Stderr = &stderr
	command.Env = os.Environ()
	if err := command.Run(); err != nil {
		detail := stderr.Bytes()
		if len(detail) > 500 {
			detail = detail[:500]
		}
		return fmt.Errorf("%w: stderr_bytes=%d stderr_prefix_sha256=%s", err, stderr.Len(), digest(detail))
	}
	return nil
}

func (ProcessProbe) Start(_ context.Context, options ProbeOptions, stdout, stderr io.Writer) (RunningProbe, error) {
	command := exec.Command(options.TemperPath, probeArguments(options)...)
	command.Stdout = stdout
	command.Stderr = stderr
	command.Env = os.Environ()
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return nil, err
	}
	running := &processProbe{command: command, base: "http://" + options.Listen, done: make(chan error, 1)}
	go func() { running.done <- command.Wait() }()
	return running, nil
}

func probeArguments(options ProbeOptions) []string {
	return []string{
		"probe", "serve", "--root", options.Root, "--installation", options.Installation,
		"--software-lock", options.SoftwareLock, "--generation", options.Generation, "--listen", options.Listen,
	}
}

type processProbe struct {
	command *exec.Cmd
	base    string
	done    chan error
	mu      sync.Mutex
	exited  bool
	err     error
}

func (p *processProbe) BaseURL() string { return p.base }

func (p *processProbe) Exited() (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.exited {
		return true, p.err
	}
	select {
	case p.err = <-p.done:
		p.exited = true
		return true, p.err
	default:
		return false, nil
	}
}

func (p *processProbe) Stop() error {
	p.mu.Lock()
	if p.exited {
		err := p.err
		p.mu.Unlock()
		return err
	}
	process := p.command.Process
	p.mu.Unlock()
	if process == nil {
		return nil
	}
	if err := syscall.Kill(-process.Pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	select {
	case err := <-p.done:
		p.mu.Lock()
		p.exited, p.err = true, err
		p.mu.Unlock()
		return nil
	case <-timer.C:
		if err := syscall.Kill(-process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
		err := <-p.done
		p.mu.Lock()
		p.exited, p.err = true, err
		p.mu.Unlock()
		return nil
	}
}
