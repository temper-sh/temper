package probecmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Supervision binds a foreground probe to the identities its caller selected.
// The status is observation, never authority to signal a PID read from disk.
type Supervision struct {
	StatusPath   string
	Root         string
	Installation string
	Generation   string
	Listen       string
}

// Validate checks the supervision boundary without reserving or writing a file.
func (s Supervision) Validate() error {
	if runtime.GOOS != "darwin" {
		return errors.New("supervised probe observation currently requires macOS")
	}
	if err := validateListen(s.Listen); err != nil {
		return err
	}
	if !filepath.IsAbs(s.StatusPath) {
		return errors.New("probe status path must be absolute")
	}
	if _, err := os.Lstat(s.StatusPath); !errors.Is(err, os.ErrNotExist) {
		return errors.New("probe status path must be new")
	}
	info, err := os.Lstat(filepath.Dir(s.StatusPath))
	if err != nil || !info.IsDir() {
		return errors.New("probe status parent must be a real directory")
	}
	return nil
}

type ProcessIdentity struct {
	ID      string `json:"id"`
	PID     int    `json:"pid"`
	PGID    int    `json:"pgid"`
	Started string `json:"ps_lstart"`
}

type probeStatus struct {
	Schema            string            `json:"schema"`
	State             string            `json:"state"`
	TemperPID         int               `json:"temper_pid"`
	Root              string            `json:"root"`
	Installation      string            `json:"installation"`
	Generation        string            `json:"generation"`
	Listen            string            `json:"listen"`
	ProcessGroup      int               `json:"process_group_id"`
	Roles             []ProcessIdentity `json:"roles"`
	ListenersVerified bool              `json:"listeners_verified"`
	Updated           string            `json:"updated_at"`
	SafeToCleanup     bool              `json:"safe_to_cleanup"`
	Error             string            `json:"error,omitempty"`
}

type processRow struct {
	pid, ppid, pgid     int
	started, executable string
	state               string
}

func (p processRow) exited() bool {
	return strings.HasPrefix(p.state, "Z") || strings.Contains(p.state, "E")
}

var errCommandUnavailable = errors.New("owned process command temporarily unavailable")

func parseRows(raw string) ([]processRow, error) {
	var rows []processRow
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 10 {
			return nil, errors.New("incomplete process observation")
		}
		pid, e1 := strconv.Atoi(fields[0])
		ppid, e2 := strconv.Atoi(fields[1])
		pgid, e3 := strconv.Atoi(fields[2])
		if e1 != nil || e2 != nil || e3 != nil {
			return nil, errors.New("invalid process identity")
		}
		// Skip the fixed columns without normalizing spaces inside an executable path.
		executable := strings.TrimLeft(line, " \t")
		for range 9 {
			separator := strings.IndexAny(executable, " \t")
			if separator < 0 {
				return nil, errors.New("incomplete process observation")
			}
			executable = strings.TrimLeft(executable[separator:], " \t")
		}
		rows = append(rows, processRow{pid, ppid, pgid, strings.Join(fields[3:8], " "), executable, fields[8]})
	}
	return rows, nil
}

func systemRead(path string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LC_ALL=C"}
	return cmd.Output()
}

func readRows() ([]processRow, error) {
	raw, err := systemRead("/bin/ps", "-axww", "-o", "pid=,ppid=,pgid=,lstart=,stat=,comm=")
	if err != nil {
		return nil, fmt.Errorf("read process identities: %w", err)
	}
	return parseRows(string(raw))
}

func readMembers(group int, invocation Invocation, known map[int]processRow) ([]processRow, []ProcessIdentity, error) {
	var err error
	invocation.Path, err = filepath.EvalSymlinks(invocation.Path)
	if err != nil {
		return nil, nil, err
	}
	if invocation.EnginePath != "" {
		invocation.EnginePath, err = filepath.EvalSymlinks(invocation.EnginePath)
		if err != nil {
			return nil, nil, err
		}
	}
	for attempt := 0; ; attempt++ {
		rows, err := readRows()
		if err != nil {
			return nil, nil, err
		}
		descendants, groups := processBoundary(rows, group, known)
		for i, row := range rows {
			if row.exited() || (!descendants[row.pid] && !groups[row.pgid]) {
				continue
			}
			rows[i].executable, err = processExecutable(row.pid)
			if err != nil {
				err = fmt.Errorf("%w: PID %d: %v", errCommandUnavailable, row.pid, err)
				break
			}
		}
		var owned []processRow
		var roles []ProcessIdentity
		if err == nil {
			owned, roles, err = members(rows, group, invocation, known)
		}
		if !errors.Is(err, errCommandUnavailable) || attempt == 2 {
			return owned, roles, err
		}
		// ps reads the process table and argv separately. A child can exit
		// between those reads. Refresh the observation before any signal;
		// an unavailable command never establishes ownership by itself.
		time.Sleep(10 * time.Millisecond)
	}
}

// processBoundary includes the router's descendants and every previously bound
// group. A router may create a child group; group membership alone never adopts
// an unrelated process. Known children remain observable after reparenting.
func processBoundary(rows []processRow, group int, known map[int]processRow) (map[int]bool, map[int]bool) {
	descendants, groups := map[int]bool{group: true}, map[int]bool{group: true}
	for pid, prior := range known {
		descendants[pid], groups[prior.pgid] = true, true
	}
	for changed := true; changed; {
		changed = false
		for _, row := range rows {
			if descendants[row.ppid] && !descendants[row.pid] {
				descendants[row.pid] = true
				changed = true
			}
		}
	}
	for _, row := range rows {
		if descendants[row.pid] && row.pgid == row.pid {
			groups[row.pgid] = true
		}
	}
	return descendants, groups
}

// members verifies all owned groups before signaling. Previously observed
// role identities and group memberships never silently rebind.
func members(rows []processRow, group int, invocation Invocation, known map[int]processRow) ([]processRow, []ProcessIdentity, error) {
	var found []processRow
	roles := []ProcessIdentity{}
	roleIDs := map[string]bool{}
	descendants, groups := processBoundary(rows, group, known)
	for _, row := range rows {
		if !descendants[row.pid] && !groups[row.pgid] {
			continue
		}
		prior, tracked := known[row.pid]
		if !descendants[row.pid] || !groups[row.pgid] || (row.pid == group && row.pgid != group) || (tracked && prior.pgid != row.pgid) {
			return nil, nil, errors.New("unrelated group member or changed owned process group")
		}
		if row.exited() {
			// macOS replaces an exited process's command with <defunct> until
			// its parent reaps it. Retain the observed identity, but do not
			// publish a live role or treat the group as gone yet.
			if !tracked || prior.started != row.started {
				return nil, nil, errors.New("unverified exited process in owned group")
			}
			row.executable = prior.executable
			found = append(found, row)
			continue
		}
		if tracked {
			// A launch shell may exec the expected engine without changing PID or
			// start time. Once it is an engine, its identity cannot rebind.
			expectedExec := isLaunchShell(prior.executable) && row.executable == invocation.EnginePath
			if !prior.exited() && prior.started == row.started && row.executable == "("+filepath.Base(prior.executable)+")" {
				return nil, nil, fmt.Errorf("%w: PID %d", errCommandUnavailable, row.pid)
			}
			if prior.exited() || prior.started != row.started || (prior.executable != row.executable && !expectedExec) {
				return nil, nil, fmt.Errorf("owned process %d identity changed: start %q -> %q, executable %q -> %q, state %q -> %q", row.pid, prior.started, row.started, prior.executable, row.executable, prior.state, row.state)
			}
		}
		role := ""
		switch {
		case row.pid == group && row.executable == invocation.Path:
			role = "router"
		case invocation.EnginePath != "" && row.executable == invocation.EnginePath:
			role = "engine"
		case isLaunchShell(row.executable):
		default:
			return nil, nil, fmt.Errorf("unexpected process %d in owned group", row.pid)
		}
		if role != "" {
			if roleIDs[role] {
				return nil, nil, fmt.Errorf("multiple %s processes in owned group", role)
			}
			for _, prior := range known {
				if prior.executable == row.executable && prior.pid != row.pid {
					return nil, nil, fmt.Errorf("%s process restarted", role)
				}
			}
			roleIDs[role] = true
			roles = append(roles, ProcessIdentity{role, row.pid, row.pgid, row.started})
		}
		found = append(found, row)
	}
	for _, row := range found {
		known[row.pid] = row
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].ID < roles[j].ID })
	return found, roles, nil
}

func isLaunchShell(path string) bool {
	return path == "/bin/sh" || path == "/bin/bash" || path == "sh" || path == "bash"
}

func containsRole(roles []ProcessIdentity, id string) bool {
	for _, role := range roles {
		if role.ID == id {
			return true
		}
	}
	return false
}

func validateListenerOutput(raw string, router int, listen string) (bool, error) {
	pid := 0
	planned := 0
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "p") {
			pid, _ = strconv.Atoi(line[1:])
		}
		if !strings.HasPrefix(line, "n") {
			continue
		}
		endpoint := line[1:]
		if !strings.HasPrefix(endpoint, "127.0.0.1:") && !strings.HasPrefix(endpoint, "[::1]:") {
			return false, fmt.Errorf("owned process exposed non-loopback listener %q", endpoint)
		}
		if endpoint == listen {
			if pid != router {
				return false, errors.New("planned listener is not owned by router")
			}
			planned++
		}
	}
	if planned > 1 {
		return false, errors.New("multiple owners of planned listener")
	}
	return planned == 1, nil
}

func readListeners(rows []processRow, router int, listen string) (bool, error) {
	if len(rows) == 0 {
		return false, nil
	}
	pids := make([]string, 0, len(rows))
	for _, row := range rows {
		pids = append(pids, strconv.Itoa(row.pid))
	}
	raw, err := systemRead("/usr/sbin/lsof", "-nP", "-a", "-p", strings.Join(pids, ","), "-iTCP", "-sTCP:LISTEN", "-Fpn")
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || len(raw) != 0 {
			return false, fmt.Errorf("read owned listeners: %w", err)
		}
	}
	return validateListenerOutput(string(raw), router, listen)
}

func listenerClosed(listen string) bool {
	connection, err := net.DialTimeout("tcp4", listen, 250*time.Millisecond)
	if err == nil {
		connection.Close()
		return false
	}
	return errors.Is(err, syscall.ECONNREFUSED)
}

func writeStatus(path string, status probeStatus) error {
	status.Updated = time.Now().UTC().Format(time.RFC3339Nano)
	data, err := json.Marshal(status)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".temper-probe-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(append(data, '\n')); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func runSupervised(ctx context.Context, invocation Invocation, stdout, stderr io.Writer) error {
	s := invocation.Supervision
	if err := s.Validate(); err != nil {
		return err
	}
	if !listenerClosed(s.Listen) {
		return errors.New("probe listener is occupied or cannot be inspected")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	reserved, err := os.OpenFile(s.StatusPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := reserved.Close(); err != nil {
		return err
	}
	status := probeStatus{Schema: "temper-probe-status/v1", State: "starting", TemperPID: os.Getpid(), Root: s.Root, Installation: s.Installation, Generation: s.Generation, Listen: s.Listen, Roles: []ProcessIdentity{}}
	if err := writeStatus(s.StatusPath, status); err != nil {
		return err
	}
	command := exec.Command(invocation.Path, invocation.Arguments...)
	command.Env = invocation.Environment
	command.Stdout = stdout
	command.Stderr = stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = 2 * time.Second
	if err := command.Start(); err != nil {
		status.State = "stopped"
		status.SafeToCleanup = true
		status.Error = err.Error()
		return errors.Join(err, writeStatus(s.StatusPath, status))
	}
	group := command.Process.Pid
	status.ProcessGroup = group
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	known := map[int]processRow{}
	var failure error
	reaped := false
	observe := func() error {
		owned, roles, err := readMembers(group, invocation, known)
		if err != nil {
			return err
		}
		hasRouter := false
		for _, role := range roles {
			if role.ID == "router" {
				hasRouter = true
			}
		}
		if !hasRouter {
			return errors.New("probe router disappeared")
		}
		for _, prior := range status.Roles {
			if prior.ID == "engine" && !containsRole(roles, "engine") {
				return errors.New("probe engine disappeared")
			}
		}
		ready, err := readListeners(owned, group, s.Listen)
		if err != nil {
			return err
		}
		if status.ListenersVerified && !ready {
			return errors.New("probe listener disappeared")
		}
		status.Roles = roles
		status.ListenersVerified = ready
		if ready {
			status.State = "running"
		}
		return writeStatus(s.StatusPath, status)
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
running:
	for {
		if failure = observe(); failure != nil {
			break
		}
		select {
		case <-ctx.Done():
			break running
		case err := <-done:
			reaped = true
			failure = fmt.Errorf("probe router exited unexpectedly: %v", err)
			break running
		case <-ticker.C:
		}
	}
	// Cancellation is expected. Identity loss or a missing final proof is not.
	shutdownErr := shutdownGroup(group, invocation, known, 30*time.Second)
	if shutdownErr == nil && !listenerClosed(s.Listen) {
		shutdownErr = errors.New("planned listener remained after shutdown")
	}
	if !reaped {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			shutdownErr = errors.Join(shutdownErr, errors.New("probe child was not reaped"))
		}
	}
	status.State = "stopped"
	status.ListenersVerified = false
	status.SafeToCleanup = shutdownErr == nil
	if failure != nil || shutdownErr != nil {
		status.Error = errors.Join(failure, shutdownErr).Error()
	}
	return errors.Join(failure, shutdownErr, writeStatus(s.StatusPath, status))
}

func shutdownGroup(group int, invocation Invocation, known map[int]processRow, grace time.Duration) error {
	verified := func() (present bool, liveGroups []int, err error) {
		owned, _, err := readMembers(group, invocation, known)
		if err != nil {
			return false, nil, err
		}
		_, groups := processBoundary(nil, group, known)
		for id := range groups {
			seen, live := false, false
			for _, row := range owned {
				if row.pgid == id {
					seen, live = true, live || !row.exited()
				}
			}
			if !seen && !errors.Is(syscall.Kill(-id, 0), syscall.ESRCH) {
				return false, nil, errors.New("group exists without observable members")
			}
			if live {
				liveGroups = append(liveGroups, id)
			}
		}
		// Stop children first so the router can reap them before it exits.
		sort.Slice(liveGroups, func(i, j int) bool {
			if liveGroups[i] == group || liveGroups[j] == group {
				return liveGroups[j] == group
			}
			return liveGroups[i] < liveGroups[j]
		})
		return len(owned) != 0, liveGroups, nil
	}
	for _, stage := range []struct {
		signal syscall.Signal
		wait   time.Duration
	}{{syscall.SIGTERM, grace}, {syscall.SIGKILL, 5 * time.Second}} {
		present, liveGroups, err := verified()
		if err != nil || !present {
			return err
		}
		for _, id := range liveGroups {
			_, current, err := verified()
			if err != nil {
				return err
			}
			live := false
			for _, candidate := range current {
				live = live || candidate == id
			}
			if !live {
				continue
			}
			if signalErr := syscall.Kill(-id, stage.signal); signalErr != nil && !errors.Is(signalErr, syscall.ESRCH) {
				// The last live member may have exited since observation. macOS
				// refuses signals to an all-zombie group; still wait for reaping.
				_, remaining, err := verified()
				stillLive := false
				for _, candidate := range remaining {
					stillLive = stillLive || candidate == id
				}
				if err != nil || stillLive {
					return errors.Join(signalErr, err)
				}
			}
		}
		deadline := time.Now().Add(stage.wait)
		for time.Now().Before(deadline) {
			present, _, err := verified()
			if err != nil || !present {
				return err
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	return errors.New("owned process group remained after KILL")
}
