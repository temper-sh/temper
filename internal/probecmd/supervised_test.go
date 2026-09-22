package probecmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	router := processRow{100, 1, 100, "Tue Sep 22 12:00:00 2026", "/owned/router", "S"}
	engine := processRow{101, 100, 100, router.started, "/owned/engine", "S"}
	known := map[int]processRow{}
	_, roles, err := members([]processRow{router, engine, {200, 1, 200, router.started, "/other/engine", "S"}}, 100, inv, known)
	if err != nil || len(roles) != 2 || roles[0].ID != "engine" {
		t.Fatal(roles, err)
	}
	for _, other := range []processRow{{101, 100, 100, "new start", "/owned/engine", "S"}, {102, 100, 100, router.started, "/owned/engine", "S"}, {102, 100, 100, router.started, "/other/engine", "S"}} {
		if _, _, err := members([]processRow{router, other}, 100, inv, known); err == nil {
			t.Fatal("adopted changed process", other)
		}
	}
}

func TestLaunchShellMayExecOnlyTheExpectedEngine(t *testing.T) {
	inv := Invocation{Path: "/owned/router", EnginePath: "/owned/engine"}
	shell := processRow{101, 100, 100, "Tue Sep 22 12:00:00 2026", "/bin/sh", "S"}
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

func TestEngineMayStartItsOwnGroupButCannotMoveAfterObservation(t *testing.T) {
	inv := Invocation{Path: "/owned/router", EnginePath: "/owned/engine"}
	router := processRow{100, 1, 100, "Tue Sep 22 12:00:00 2026", inv.Path, "S"}
	engine := processRow{101, 100, 101, router.started, inv.EnginePath, "S"}
	known := map[int]processRow{}
	if _, roles, err := members([]processRow{router, engine}, 100, inv, known); err != nil || len(roles) != 2 || roles[0].PGID != engine.pid {
		t.Fatal("refused the router's engine group", roles, err)
	}
	engine.ppid = 1
	if owned, _, err := members([]processRow{engine}, 100, inv, known); err != nil || len(owned) != 1 {
		t.Fatal("forgot the verified engine after its router exited", owned, err)
	}
	engine.pgid = 102
	if _, _, err := members([]processRow{engine}, 100, inv, known); err == nil {
		t.Fatal("accepted a changed process group")
	}
	engine.pgid = 101
	stranger := processRow{102, 1, 101, router.started, inv.EnginePath, "S"}
	if _, _, err := members([]processRow{engine, stranger}, 100, inv, known); err == nil {
		t.Fatal("adopted an unrelated member of the engine group")
	}
}

func TestExitedProcessRemainsOwnedUntilReaped(t *testing.T) {
	inv := Invocation{Path: "/owned/router", EnginePath: "/owned/engine"}
	router := processRow{100, 1, 100, "Tue Sep 22 12:00:00 2026", inv.Path, "S"}
	engine := processRow{101, 100, 100, router.started, inv.EnginePath, "S"}
	known := map[int]processRow{100: router, 101: engine}
	exited := engine
	exited.executable, exited.state = "<defunct>", "Z"
	owned, roles, err := members([]processRow{router, exited}, 100, inv, known)
	if err != nil || len(owned) != 2 || len(roles) != 1 || roles[0].ID != "router" {
		t.Fatal(owned, roles, err)
	}
	if known[101].executable != inv.EnginePath || !known[101].exited() {
		t.Fatal("lost the exited engine's observed identity")
	}
	if _, _, err := members([]processRow{router, engine}, 100, inv, known); err == nil {
		t.Fatal("accepted a live process after observing its exit")
	}
	owned, _, err = members([]processRow{router}, 100, inv, known)
	if err != nil || len(owned) != 1 {
		t.Fatal("reaped child remained in the group", owned, err)
	}
}

func TestExitStateDoesNotAdoptUnknownReusedOrMovedProcesses(t *testing.T) {
	inv := Invocation{Path: "/owned/router", EnginePath: "/owned/engine"}
	engine := processRow{101, 100, 100, "Tue Sep 22 12:00:00 2026", inv.EnginePath, "S"}
	for _, child := range []processRow{
		{102, 100, 100, engine.started, "<defunct>", "Z"},
		{101, 100, 100, "new start", "<defunct>", "Z"},
		{101, 1, 101, engine.started, "<defunct>", "Z"},
		{101, 100, 100, engine.started, "<defunct>", "S"},
	} {
		if _, _, err := members([]processRow{child}, 100, inv, map[int]processRow{101: engine}); err == nil {
			t.Fatal("accepted unverified process", child)
		}
	}
}

func TestUnavailableCommandRequiresFreshObservationBeforeSignaling(t *testing.T) {
	inv := Invocation{Path: "/owned/router"}
	router := processRow{100, 1, 100, "Tue Sep 22 12:00:00 2026", inv.Path, "S"}
	known := map[int]processRow{100: router}
	unavailable := router
	unavailable.executable = "(router)"
	owned, roles, err := members([]processRow{unavailable}, 100, inv, known)
	if !errors.Is(err, errCommandUnavailable) || len(owned) != 0 || len(roles) != 0 || known[100] != router {
		t.Fatal("unavailable command granted ownership", owned, roles, known, err)
	}
	unavailable.started = "new start"
	if _, _, err := members([]processRow{unavailable}, 100, inv, known); err == nil || errors.Is(err, errCommandUnavailable) {
		t.Fatal("treated a reused PID as a transient observation", err)
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

func TestShutdownWaitsForAnOwnedExitedProcessToBeReaped(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS process identity observation")
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(binary, "-test.run=^TestProbeProcessHelper$")
	child.Env = append(os.Environ(), "TEMPER_TEST_PROBE=engine")
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	}()
	rows, err := readRows()
	if err != nil {
		t.Fatal(err)
	}
	known := map[int]processRow{}
	if _, _, err := members(rows, child.Process.Pid, Invocation{Path: binary}, known); err != nil || len(known) != 1 {
		t.Fatal("did not observe the owned process", known, err)
	}
	if err := child.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	// Leave this child unreaped so the supervisor must observe macOS's
	// <defunct> command rather than depending on a scheduling coincidence.
	exited := false
	deadline := time.Now().Add(5 * time.Second)
	for !exited && time.Now().Before(deadline) {
		rows, err := readRows()
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row.pid == child.Process.Pid && row.exited() {
				exited = true
			}
		}
		if !exited {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !exited {
		t.Fatal("child did not exit")
	}
	done := make(chan error, 1)
	go func() { done <- shutdownGroup(child.Process.Pid, Invocation{Path: binary}, known, 5*time.Second) }()
	select {
	case err := <-done:
		t.Fatal("shutdown finished before the child was reaped", err)
	case <-time.After(200 * time.Millisecond):
	}
	_ = child.Wait()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not observe reaping")
	}
}

func TestProcessAndListenerParsersPreserveExactBoundaries(t *testing.T) {
	rows, err := parseRows(" 100 1 100 Tue Sep 22 12:00:00 2026 S /path with  spaces/router\n")
	if err != nil || len(rows) != 1 || rows[0].executable != "/path with  spaces/router" {
		t.Fatal(rows, err)
	}
	rows, err = parseRows(" 101 100 100 Tue Sep 22 12:00:00 2026 Z <defunct>\n")
	if err != nil || len(rows) != 1 || !rows[0].exited() {
		t.Fatal(rows, err)
	}
	rows, err = parseRows(" 101 100 100 Tue Sep 22 12:00:00 2026 ?E (engine)\n")
	if err != nil || len(rows) != 1 || !rows[0].exited() {
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
	for _, separateGroup := range []bool{false, true} {
		t.Run(fmt.Sprintf("separate-engine-group=%t", separateGroup), func(t *testing.T) {
			testSupervisedForeground(t, separateGroup)
		})
	}
}

func testSupervisedForeground(t *testing.T, separateGroup bool) {
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
	var processOutput bytes.Buffer
	inv := Invocation{Path: router, EnginePath: engine, Arguments: []string{"-test.run=^TestProbeProcessHelper$"},
		Environment: append(os.Environ(), "TEMPER_TEST_PROBE=router", "TEMPER_TEST_LISTEN="+listen, "TEMPER_TEST_ENGINE="+engine,
			fmt.Sprintf("TEMPER_TEST_ENGINE_GROUP=%t", separateGroup)),
		Supervision: &Supervision{StatusPath: statusPath, Root: parent, Installation: "fixture", Generation: strings.Repeat("b", 64), Listen: listen}}
	go func() { done <- runSupervised(ctx, inv, &processOutput, &processOutput) }()
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
			t.Fatalf("%v; helper output: %s", err, processOutput.String())
		}
	case <-time.After(45 * time.Second):
		t.Fatal("supervisor did not stop")
	}
	if observed.State != "running" || !observed.ListenersVerified || len(observed.Roles) != 2 {
		t.Fatalf("no owned live boundary: %+v; helper output: %s", observed, processOutput.String())
	}
	if separateGroup && observed.Roles[0].PGID != observed.Roles[0].PID {
		t.Fatal("engine did not start in its own group", observed.Roles)
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
	for _, role := range observed.Roles {
		if err := syscall.Kill(-role.PGID, 0); err != syscall.ESRCH {
			t.Fatalf("%s group remains: %v", role.ID, err)
		}
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
	// Real llama-swap launches by basename and gives the engine its own group.
	// argv[0] is deliberately insufficient to establish executable identity.
	child.Args[0] = filepath.Base(child.Path)
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: os.Getenv("TEMPER_TEST_ENGINE_GROUP") == "true"}
	child.Env = append(os.Environ(), "TEMPER_TEST_PROBE=engine")
	if err := child.Start(); err != nil {
		os.Exit(3)
	}
	<-ctx.Done()
	_ = child.Wait()
}
