package probecmd

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/temper-sh/temper/internal/datadir"
)

func stoppedFixture(t *testing.T, root string) probeStatus {
	t.Helper()
	if err := datadir.Ensure(root); err != nil {
		t.Fatal(err)
	}
	return probeStatus{Schema: "temper-probe-status/v1", State: "stopped", TemperPID: 72471,
		Root: root, Installation: "field-kit-test", Generation: strings.Repeat("a", 64),
		Listen: "127.0.0.1:62626", ProcessGroup: 72472,
		Roles:   []ProcessIdentity{{"router", 72472, 72472, "Fri Oct 23 18:40:09 2026"}, {"engine", 72486, 72486, "Fri Oct 23 18:40:10 2026"}},
		Updated: "2026-10-23T15:42:04Z", Error: "untracked orphan in router group"}
}

func TestVerifyStoppedPreservesFailedStatusAndChecksHelperGroups(t *testing.T) {
	status := stoppedFixture(t, t.TempDir())
	status.Processes = append(append([]ProcessIdentity{}, status.Roles...), ProcessIdentity{"", 72500, 72500, "helper start"})
	path := filepath.Join(t.TempDir(), "status.json")
	raw, _ := json.Marshal(status)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	missing := func(int) error { return syscall.ESRCH }
	closed := func(string) bool { return true }
	proof, err := verifyStopped(path, missing, closed)
	if err != nil || !proof.SafeToCleanup || proof.StatusSHA256 == "" || !reflect.DeepEqual(proof.ProcessGroups, []int{72472, 72486, 72500}) {
		t.Fatal(proof, err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(raw) {
		t.Fatal("rewrote the failed shutdown evidence")
	}
	// A second invocation is another read and leaves the same original evidence.
	if _, err := verifyStopped(path, missing, closed); err != nil {
		t.Fatal(err)
	}
	for _, live := range []int{72471, 72486, 72500, -72472, -72500} {
		_, err := verifyStopped(path, func(pid int) error {
			if pid == live {
				return nil
			}
			return syscall.ESRCH
		}, closed)
		if err == nil {
			t.Fatalf("accepted existing/reused PID or group %d", live)
		}
	}
	if _, err := verifyStopped(path, func(int) error { return syscall.EPERM }, closed); err == nil {
		t.Fatal("accepted unavailable process observation")
	}
	for _, port := range proof.Listeners {
		if _, err := verifyStopped(path, missing, func(address string) bool { return address != port }); err == nil {
			t.Fatal("accepted occupied/uninspectable listener", port)
		}
	}
}

func TestVerifyStoppedAcceptsLegacyRoleBoundaryButRejectsIncompleteOrChangedStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	status := stoppedFixture(t, t.TempDir())
	write := func(s probeStatus) {
		t.Helper()
		raw, _ := json.Marshal(s)
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	missing := func(int) error { return syscall.ESRCH }
	closed := func(string) bool { return true }
	write(status)
	if _, err := verifyStopped(path, missing, closed); err != nil {
		t.Fatal("refused the issued 0.3.2 status", err)
	}
	for _, change := range []func(*probeStatus){
		func(s *probeStatus) { s.State = "running" },
		func(s *probeStatus) { s.TemperPID = 0 },
		func(s *probeStatus) { s.ProcessGroup = -1 },
		func(s *probeStatus) { s.Root = "relative" },
		func(s *probeStatus) { s.Root = filepath.Join(t.TempDir(), "missing-root") },
		func(s *probeStatus) { s.Roles = nil },
		func(s *probeStatus) { s.Generation = "" },
		func(s *probeStatus) { s.Listen = "0.0.0.0:62626" },
		func(s *probeStatus) { s.ListenersVerified = true },
	} {
		changed := status
		change(&changed)
		write(changed)
		if _, err := verifyStopped(path, missing, closed); err == nil {
			t.Fatal("accepted incomplete stopped status", changed)
		}
	}
	write(status)
	if _, err := verifyStopped(path, missing, func(string) bool { changed := status; changed.Error = "changed"; write(changed); return true }); err == nil {
		t.Fatal("accepted status changed during verification")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyStopped(link, missing, closed); err == nil {
		t.Fatal("accepted an indirect status file")
	}
}

func TestShutdownWaitsForUntrackedOrphanWithoutAdoptingIt(t *testing.T) {
	inv := Invocation{Path: "/owned/router"}
	router := processRow{pid: 72472, ppid: 72471, pgid: 72472, started: "router start", executable: inv.Path, state: "S"}
	orphan := processRow{pid: 74332, ppid: 1, pgid: 72472, started: "helper start", executable: "/usr/sbin/ioreg", state: "R"}
	known := map[int]processRow{router.pid: router}
	_, _, failure := members([]processRow{orphan}, router.pid, inv, known)
	if !errors.Is(failure, errUnverifiedOrphan) || len(known) != 1 {
		t.Fatal("orphan was adopted or not refreshable", known, failure)
	}
	observed := false
	present, groups, err := awaitShutdownObservation(func() (bool, []int, error) {
		if !observed {
			observed = true
			return false, nil, failure
		}
		return false, nil, nil
	}, time.Now().Add(time.Second))
	if err != nil || present || len(groups) != 0 || len(known) != 1 {
		t.Fatal(present, groups, known, err)
	}
	_, _, err = awaitShutdownObservation(func() (bool, []int, error) { return false, nil, failure }, time.Now())
	if !errors.Is(err, errUnverifiedOrphan) {
		t.Fatal("persistent orphan did not refuse", err)
	}
	// A previously bound identity changing is never the transient-orphan case.
	known[orphan.pid] = orphan
	orphan.started = "reused"
	_, _, err = members([]processRow{orphan}, router.pid, inv, known)
	if err == nil || errors.Is(err, errUnverifiedOrphan) {
		t.Fatal("reused identity became retryable", err)
	}
}

func TestVerifyStoppedObservesRealProcessesAndListenersWithoutStoppingThem(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS probe observation")
	}
	status := stoppedFixture(t, t.TempDir())
	supervisor := exec.Command("/usr/bin/true")
	if err := supervisor.Run(); err != nil {
		t.Fatal(err)
	}
	status.TemperPID = supervisor.Process.Pid
	child := exec.Command("/bin/sleep", "60")
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	status.ProcessGroup = child.Process.Pid
	status.Roles = []ProcessIdentity{{"router", child.Process.Pid, child.Process.Pid, "test child"}}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	status.Listen = listener.Addr().String()
	path := filepath.Join(t.TempDir(), "status.json")
	raw, _ := json.Marshal(status)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyStopped(path); err == nil {
		t.Fatal("accepted a live process")
	}
	if err := child.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("verification stopped the child", err)
	}
	_ = child.Process.Kill()
	_ = child.Wait()
	if _, err := VerifyStopped(path); err == nil {
		t.Fatal("accepted a live listener")
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyStopped(path); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(raw) {
		t.Fatal("verification changed the status")
	}
}
