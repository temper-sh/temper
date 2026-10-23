package probecmd

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ManagedCommand is an exact allowed child invocation. Each demand load is a
// fresh lifetime; it does not inherit a previous experiment's PID identity.
type ManagedCommand struct {
	ID, Path  string
	Arguments []string
	// The exact owned launcher is a retryable exec transition, never a process
	// that observation or recovery may adopt as the final engine.
	LauncherPath      string
	LauncherArguments []string
}

var errManagedExec = errors.New("owned managed launcher is still in transition; retry after exec")

func managedLaunching(row processRow, command ManagedCommand) bool {
	if command.LauncherPath == "" || row.executable != command.LauncherPath {
		return false
	}
	// Executable and argv are separate kernel reads. Exec can occur between
	// them; only the frozen destination's complete argv is an allowed crossover.
	return (strings.HasPrefix(row.arguments, command.LauncherPath+"\x00") && managedArguments(row.arguments, command.LauncherArguments)) ||
		(strings.HasPrefix(row.arguments, command.Path+"\x00") && managedArguments(row.arguments, command.Arguments))
}

type ManagedObservation struct {
	Processes  []ProcessIdentity
	Identities []ManagedIdentity
	Listening  bool
	Suspended  bool
}
type ManagedIdentity struct {
	ProcessIdentity
	Path      string `json:"path"`
	Arguments string `json:"arguments"`
}

func managedArguments(actual string, expected []string) bool {
	args := strings.Split(actual, "\x00")
	if len(args) != len(expected)+1 {
		return false
	}
	for i, want := range expected {
		got := args[i+1]
		if want == "${PORT}" {
			n, err := strconv.Atoi(got)
			if err != nil || n < 1024 || n > 65535 {
				return false
			}
		} else if got != want {
			return false
		}
	}
	return true
}

// ObserveManaged verifies current ancestry, kernel executable and argv. It has
// no remembered engine lifetime: an absent engine is ordinary idle availability.
func ObserveManaged(pid int, router string, args []string, listen string, commands []ManagedCommand) (ManagedObservation, error) {
	reader := managedInspector{readRows, processExecutable, processArguments, readListeners}
	return reader.observe(pid, router, args, listen, commands)
}

// A process can exit between the process table and kernel identity reads.
// Re-read the complete boundary after ESRCH; never turn a partial snapshot into
// an idle observation or retry an ownership/permission failure as disappearance.
type managedInspector struct {
	rows       func() ([]processRow, error)
	executable func(int) (string, error)
	arguments  func(int) (string, error)
	listeners  func([]processRow, int, string) (bool, error)
}

func (r managedInspector) observe(pid int, router string, args []string, listen string, commands []ManagedCommand) (ManagedObservation, error) {
	var observed ManagedObservation
	var err error
	deadline := time.Now().Add(2 * time.Second)
	disappearances := 0
	for {
		observed, err = r.snapshot(pid, router, args, listen, commands)
		if errors.Is(err, errManagedExec) && time.Now().Before(deadline) {
			time.Sleep(25 * time.Millisecond)
			continue
		}
		if errors.Is(err, syscall.ESRCH) {
			disappearances++
			if disappearances < 3 && time.Now().Before(deadline) {
				time.Sleep(25 * time.Millisecond)
				continue
			}
		}
		break
	}
	return observed, err
}

func (r managedInspector) snapshot(pid int, router string, args []string, listen string, commands []ManagedCommand) (ManagedObservation, error) {
	rows, err := r.rows()
	if err != nil {
		return ManagedObservation{}, err
	}
	rootFound := false
	for _, r := range rows {
		if r.pid == pid {
			rootFound = true
			if r.pgid != pid || r.exited() {
				return ManagedObservation{}, errors.New("router group is not a live owned leader")
			}
		}
	}
	if !rootFound {
		return ManagedObservation{}, errors.New("router PID is absent")
	}
	descendants, groups := processBoundary(rows, pid, map[int]processRow{})
	var owned []processRow
	for i, row := range rows {
		if !descendants[row.pid] && !groups[row.pgid] {
			continue
		}
		if !descendants[row.pid] {
			return ManagedObservation{}, errors.New("unrelated process in managed group")
		}
		if row.exited() {
			return ManagedObservation{}, errors.New("exiting managed child; retry after it is reaped")
		}
		row.executable, err = r.executable(row.pid)
		if err != nil {
			return ManagedObservation{}, fmt.Errorf("read managed process %d executable: %w", row.pid, err)
		}
		row.arguments, err = r.arguments(row.pid)
		if err != nil {
			return ManagedObservation{}, fmt.Errorf("read managed process %d arguments: %w", row.pid, err)
		}
		rows[i] = row
		owned = append(owned, row)
	}
	var result ManagedObservation
	for _, row := range owned {
		role := ""
		if row.pid == pid {
			result.Suspended = strings.Contains(row.state, "T")
			if row.executable != router || !exactArguments(row.arguments, args) {
				return result, errors.New("launchd router differs from its exact selected command")
			}
			role = "router"
		} else {
			for _, c := range commands {
				if c.ID == "router" {
					continue // Only the selected launchd leader may be the router.
				}
				if managedLaunching(row, c) {
					return ManagedObservation{}, errManagedExec
				}
				if row.executable == c.Path && managedArguments(row.arguments, c.Arguments) {
					role = c.ID
					break
				}
			}
			if role == "" && routerInspection(row, rows, pid, Invocation{Path: router}, map[int]processRow{}) {
				role = "inspection"
			}
			// A shell launch is accepted only while executing the selected absolute
			// command. Arbitrary descendants never become signaling authority.
			if role == "" && isLaunchShell(row.executable) {
				return result, errors.New("managed launch shell is still in transition; retry after exec")
			}
			if role == "" {
				return result, fmt.Errorf("unrecognized managed child %d (%s)", row.pid, row.executable)
			}
		}
		result.Processes = append(result.Processes, ProcessIdentity{ID: role, PID: row.pid, PGID: row.pgid, Started: row.started})
		result.Identities = append(result.Identities, ManagedIdentity{ProcessIdentity: ProcessIdentity{ID: role, PID: row.pid, PGID: row.pgid, Started: row.started}, Path: row.executable, Arguments: row.arguments})
	}
	result.Listening, err = r.listeners(owned, pid, listen)
	return result, err
}

// QuiesceManaged temporarily freezes only the verified router while checking
// its accepted TCP connections. A connection is conservatively treated as work,
// including idle keepalive. Children remain running. The caller must always
// release the hold; no engine or unrelated group is signaled here.
func QuiesceManaged(captured ManagedIdentity, listen string) (func() error, error) {
	listener, err := netip.ParseAddrPort(listen)
	if err != nil || !listener.Addr().IsLoopback() || listener.Port() == 0 {
		return nil, errors.New("invalid managed listener for idle check")
	}
	pid := captured.PID
	remaining, err := ReapManaged([]ManagedIdentity{captured}, false)
	if err != nil {
		return nil, err
	}
	if !remaining {
		return nil, errors.New("router exited before idle check")
	}
	if err := syscall.Kill(pid, syscall.SIGSTOP); err != nil {
		return nil, err
	}
	release := func() error {
		return ResumeManaged(captured)
	}
	// Signal delivery is not the stopped-state observation. Wait for the hold
	// before inspecting sockets, otherwise accept could race the final read.
	held := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := readRows()
		if err != nil {
			_ = release()
			return nil, err
		}
		for _, row := range rows {
			if row.pid == pid && row.started == captured.Started && row.pgid == captured.PGID && strings.Contains(row.state, "T") {
				held = true
			}
		}
		if held {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !held {
		_ = release()
		return nil, errors.New("router hold was not observed; transition refused")
	}
	raw, err := systemRead("/usr/sbin/lsof", "-nP", "-a", "-p", strconv.Itoa(pid), "-iTCP", "-sTCP:ESTABLISHED", "-Fpn")
	if err != nil {
		var exit interface{ ExitCode() int }
		if !errors.As(err, &exit) || exit.ExitCode() != 1 || len(raw) != 0 {
			_ = release()
			return nil, fmt.Errorf("cannot prove router idle: %w", err)
		}
	}
	accepted, err := managedAcceptedConnections(raw, pid, listener)
	if err != nil {
		_ = release()
		return nil, fmt.Errorf("cannot prove router idle: %w", err)
	}
	if accepted {
		_ = release()
		return nil, errors.New("router has accepted connections; close idle clients and retry after active work finishes")
	}
	return release, nil
}

// lsof reports local->remote endpoints for both accepted clients and outgoing
// engine connections. Only a local endpoint on the router listener is accepted
// client work. Idle outgoing keepalives must not prevent an idle shutdown.
func managedAcceptedConnections(raw []byte, pid int, listener netip.AddrPort) (bool, error) {
	if len(raw) == 0 {
		return false, nil
	}
	process, pending, sockets, accepted := false, false, 0, false
	invalid := errors.New("unexpected established-socket observation")
	for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		if len(line) < 2 {
			return false, invalid
		}
		switch line[0] {
		case 'p':
			if process || line[1:] != strconv.Itoa(pid) {
				return false, invalid
			}
			process = true
		case 'f':
			if _, err := strconv.ParseUint(line[1:], 10, 32); err != nil || !process || pending {
				return false, invalid
			}
			pending = true
		case 'n':
			local, remote, ok := strings.Cut(line[1:], "->")
			from, err := netip.ParseAddrPort(local)
			_, peerErr := netip.ParseAddrPort(remote)
			if !pending || !ok || err != nil || peerErr != nil || from.Port() == 0 || from.Addr().Unmap() != listener.Addr().Unmap() {
				return false, invalid
			}
			accepted = accepted || from.Port() == listener.Port()
			sockets++
			pending = false
		default:
			return false, invalid
		}
	}
	if !process || pending || sockets == 0 {
		return false, invalid
	}
	return accepted, nil
}

func ManagedListenerClosed(listen string) bool { return listenerClosed(listen) }

// ManagedIdentityAllowed validates persisted recovery authority against the
// exact selected commands, including arguments. A matching executable alone
// never permits a signal.
func ManagedIdentityAllowed(identity ManagedIdentity, commands []ManagedCommand) bool {
	if identity.PID <= 1 || identity.PGID <= 1 || identity.Started == "" {
		return false
	}
	for _, c := range commands {
		if c.ID == identity.ID && c.Path == identity.Path && managedArguments(identity.Arguments, c.Arguments) {
			return true
		}
	}
	return identity.ID == "inspection" && inspectionCommand(processRow{executable: identity.Path, arguments: identity.Arguments})
}

// CaptureManagedLaunch records the process that will become the engine after
// exec. The launcher has no remaining process or background loop after exec.
func CaptureManagedLaunch(id, path string, args []string) (ManagedIdentity, error) {
	if err := syscall.Setpgid(0, 0); err != nil {
		return ManagedIdentity{}, err
	}
	rows, err := readRows()
	if err != nil {
		return ManagedIdentity{}, err
	}
	for _, r := range rows {
		if r.pid == os.Getpid() {
			return ManagedIdentity{ProcessIdentity: ProcessIdentity{ID: id, PID: r.pid, PGID: r.pgid, Started: r.started}, Path: path, Arguments: strings.Join(append([]string{path}, args...), "\x00")}, nil
		}
	}
	return ManagedIdentity{}, errors.New("could not capture engine launch identity")
}

// ObserveManagedLoads follows a durably recorded engine process group even
// after the router exits. PID reuse proves the old lifetime ended. Unknown
// group members refuse recovery; they never acquire signaling authority.
func ObserveManagedLoads(roots []ManagedIdentity, commands []ManagedCommand) ([]ManagedIdentity, error) {
	deadline := time.Now().Add(2 * time.Second)
	for {
		rows, err := readRows()
		if err != nil {
			return nil, err
		}
		observed, err := observeManagedLoads(rows, roots, commands, processExecutable, processArguments)
		if !errors.Is(err, errManagedExec) || !time.Now().Before(deadline) {
			return observed, err
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func observeManagedLoads(rows []processRow, roots []ManagedIdentity, commands []ManagedCommand, executable, arguments func(int) (string, error)) ([]ManagedIdentity, error) {
	var result []ManagedIdentity
	seen := map[int]bool{}
	for _, root := range roots {
		reused := false
		for _, r := range rows {
			if r.pid == root.PID && r.started != root.Started {
				reused = true
			}
		}
		if reused {
			continue
		}
		descendants, groups := processBoundary(rows, root.PID, map[int]processRow{})
		groups[root.PGID] = true
		for _, r := range rows {
			if seen[r.pid] || (!descendants[r.pid] && !groups[r.pgid]) {
				continue
			}
			if r.exited() {
				return nil, errors.New("managed engine group is exiting; retry after it is reaped")
			}
			path, err := executable(r.pid)
			if err != nil {
				return nil, err
			}
			args, err := arguments(r.pid)
			if err != nil {
				return nil, err
			}
			role := ""
			for _, c := range commands {
				if r.pid == root.PID && r.pgid == root.PGID && c.ID == root.ID && c.Path == root.Path && managedArguments(root.Arguments, c.Arguments) &&
					managedLaunching(processRow{executable: path, arguments: args}, c) {
					return nil, errManagedExec
				}
				if (c.ID == root.ID || strings.HasPrefix(c.ID, root.ID+"/")) && path == c.Path && managedArguments(args, c.Arguments) {
					role = c.ID
					break
				}
			}
			if role == "" || (r.pid == root.PID && (r.pgid != root.PGID || path != root.Path || args != root.Arguments)) {
				return nil, errors.New("unrecognized process in recorded engine group; refusing recovery")
			}
			seen[r.pid] = true
			result = append(result, ManagedIdentity{ProcessIdentity: ProcessIdentity{ID: role, PID: r.pid, PGID: r.pgid, Started: r.started}, Path: path, Arguments: args})
		}
	}
	return result, nil
}

// ReapManaged acts only on a captured owned identity with matching kernel start
// time, group, executable and complete argv. A saved PID alone never authorizes
// a signal; no process is adopted by scanning for an executable name.
func ReapManaged(identities []ManagedIdentity, signal bool) (bool, error) {
	rows, err := readRows()
	if err != nil {
		return false, err
	}
	remaining := false
	for _, identity := range identities {
		for _, row := range rows {
			if row.pid != identity.PID {
				continue
			}
			if row.started != identity.Started || row.pgid != identity.PGID {
				continue // The captured lifetime ended; never signal the new one.
			}
			remaining = true
			if row.exited() {
				continue
			}
			path, err := processExecutable(row.pid)
			if err != nil {
				return false, err
			}
			args, err := processArguments(row.pid)
			if err != nil {
				return false, err
			}
			if path != identity.Path || args != identity.Arguments {
				return false, errors.New("owned shutdown identity changed")
			}
			if signal {
				if err := syscall.Kill(row.pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
					return false, err
				}
			}
		}
	}
	return remaining, nil
}

func ResumeManaged(identity ManagedIdentity) error {
	rows, err := readRows()
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.pid != identity.PID {
			continue
		}
		if row.started != identity.Started || row.pgid != identity.PGID {
			return nil // The captured lifetime ended.
		}
		path, err := processExecutable(row.pid)
		if err != nil {
			return err
		}
		args, err := processArguments(row.pid)
		if err != nil {
			return err
		}
		if path != identity.Path || args != identity.Arguments {
			return errors.New("suspended router command changed")
		}
		if strings.Contains(row.state, "T") {
			return syscall.Kill(row.pid, syscall.SIGCONT)
		}
		return nil
	}
	return nil
}
