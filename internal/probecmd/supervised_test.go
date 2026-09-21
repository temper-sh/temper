package probecmd

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestMembershipDoesNotAdoptAnotherExecutableOrReusedIdentity(t *testing.T) {
	inv := Invocation{Path: "/owned/router", EnginePath: "/owned/engine"}
	router := processRow{100, 1, 100, "Tue Sep 22 12:00:00 2026", "/owned/router"}
	engine := processRow{101, 100, 100, router.started, "/owned/engine"}
	known := map[int]processRow{}
	_, roles, err := members([]processRow{router, engine, {200, 1, 200, router.started, "/other/engine"}}, 100, inv, known)
	if err != nil || len(roles) != 2 || roles[0].ID != "engine" {
		t.Fatal(roles, err)
	}
	for _, other := range []processRow{{101, 100, 100, "new start", "/owned/engine"}, {102, 100, 100, router.started, "/owned/engine"}, {102, 100, 100, router.started, "/other/engine"}} {
		if _, _, err := members([]processRow{router, other}, 100, inv, known); err == nil {
			t.Fatal("adopted changed process", other)
		}
	}
}

func TestLaunchShellMayExecOnlyTheExpectedEngine(t *testing.T) {
	inv := Invocation{Path: "/owned/router", EnginePath: "/owned/engine"}
	shell := processRow{101, 100, 100, "Tue Sep 22 12:00:00 2026", "/bin/sh"}
	known := map[int]processRow{shell.pid: shell}
	engine := shell
	engine.executable = inv.EnginePath
	if _, roles, err := members([]processRow{engine}, 100, inv, known); err != nil || roles[0].ID != "engine" {
		t.Fatal(roles, err)
	}
	if _, _, err := members([]processRow{shell}, 100, inv, known); err == nil {
		t.Fatal("engine silently became a shell")
	}
}

func TestMovedOrEscapedChildPreventsGroupOnlyShutdownProof(t *testing.T) {
	inv := Invocation{Path: "/owned/router", EnginePath: "/owned/engine"}
	router := processRow{100, 1, 100, "Tue Sep 22 12:00:00 2026", inv.Path}
	engine := processRow{101, 100, 101, router.started, inv.EnginePath}
	if _, _, err := members([]processRow{router, engine}, 100, inv, map[int]processRow{}); err == nil {
		t.Fatal("accepted descendant in another group")
	}
	engine.ppid = 1
	if _, _, err := members([]processRow{engine}, 100, inv, map[int]processRow{101: engine}); err == nil {
		t.Fatal("forgot a previously observed orphan outside its group")
	}
}

func TestShutdownEscalatesForAnOwnedProcessIgnoringTerm(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS process identity observation")
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(t.TempDir(), "ready")
	child := exec.Command(binary, "-test.run=^TestProbeProcessHelper$")
	child.Env = append(os.Environ(), "TEMPER_TEST_PROBE=stubborn", "TEMPER_TEST_READY="+ready)
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(ready); err != nil {
		t.Fatal("helper did not initialize")
	}
	if err := shutdownGroup(child.Process.Pid, Invocation{Path: binary}, map[int]processRow{}, 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	<-done
	if child.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Fatal("did not escalate to KILL")
	}
}

func TestProcessAndListenerParsersPreserveExactBoundaries(t *testing.T) {
	rows, err := parseRows(" 100 1 100 Tue Sep 22 12:00:00 2026 /path with  spaces/router\n")
	if err != nil || len(rows) != 1 || rows[0].executable != "/path with  spaces/router" {
		t.Fatal(rows, err)
	}
	if ready, err := validateListenerOutput("p100\nn127.0.0.1:18080\np101\nn127.0.0.1:18081\n", 100, "127.0.0.1:18080"); err != nil || !ready {
		t.Fatal(ready, err)
	}
	for _, raw := range []string{"p101\nn127.0.0.1:18080\n", "p100\nn*:18080\n", "p100\nn0.0.0.0:18080\n"} {
		if _, err := validateListenerOutput(raw, 100, "127.0.0.1:18080"); err == nil {
			t.Fatal("accepted listener", raw)
		}
	}
}

func TestSupervisedForegroundStopsOwnedRouterAndEngine(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS listener observation")
	}
	parent := filepath.Join(t.TempDir(), "owned  processes")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	router, engine := filepath.Join(parent, "router"), filepath.Join(parent, "engine")
	for _, path := range []string{router, engine} {
		if err := os.WriteFile(path, raw, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listen := listener.Addr().String()
	listener.Close()
	statusPath := filepath.Join(parent, "status.json")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	inv := Invocation{Path: router, EnginePath: engine, Arguments: []string{"-test.run=^TestProbeProcessHelper$"},
		Environment: append(os.Environ(), "TEMPER_TEST_PROBE=router", "TEMPER_TEST_LISTEN="+listen, "TEMPER_TEST_ENGINE="+engine),
		Supervision: &Supervision{StatusPath: statusPath, Root: parent, Installation: "fixture", Generation: strings.Repeat("b", 64), Listen: listen}}
	go func() { done <- runSupervised(ctx, inv, io.Discard, io.Discard) }()
	var observed probeStatus
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		raw, _ := os.ReadFile(statusPath)
		_ = json.Unmarshal(raw, &observed)
		if observed.State == "running" && len(observed.Roles) == 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(45 * time.Second):
		t.Fatal("supervisor did not stop")
	}
	if observed.State != "running" || !observed.ListenersVerified || len(observed.Roles) != 2 {
		t.Fatalf("no owned live boundary: %+v", observed)
	}
	raw, err = os.ReadFile(statusPath)
	if err != nil {
		t.Fatal(err)
	}
	var stopped probeStatus
	if err := json.Unmarshal(raw, &stopped); err != nil {
		t.Fatal(err)
	}
	if stopped.State != "stopped" || !stopped.SafeToCleanup || !listenerClosed(listen) {
		t.Fatalf("shutdown unproven: %+v", stopped)
	}
	if err := syscall.Kill(-stopped.ProcessGroup, 0); err != syscall.ESRCH {
		t.Fatalf("group remains: %v", err)
	}
	// A stale status path cannot be reused by another invocation.
	if err := runSupervised(context.Background(), inv, io.Discard, io.Discard); err == nil {
		t.Fatal("reused status path")
	}
}

func TestProbeProcessHelper(t *testing.T) {
	role := os.Getenv("TEMPER_TEST_PROBE")
	if role == "" {
		return
	}
	if role == "stubborn" {
		signal.Ignore(syscall.SIGTERM)
		if err := os.WriteFile(os.Getenv("TEMPER_TEST_READY"), []byte("ready"), 0o600); err != nil {
			os.Exit(4)
		}
		for {
			time.Sleep(time.Second)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()
	if role == "engine" {
		<-ctx.Done()
		return
	}
	listener, err := net.Listen("tcp4", os.Getenv("TEMPER_TEST_LISTEN"))
	if err != nil {
		os.Exit(2)
	}
	defer listener.Close()
	child := exec.Command(os.Getenv("TEMPER_TEST_ENGINE"), "-test.run=^TestProbeProcessHelper$")
	child.Env = append(os.Environ(), "TEMPER_TEST_PROBE=engine")
	if err := child.Start(); err != nil {
		os.Exit(3)
	}
	<-ctx.Done()
	_ = child.Wait()
}
