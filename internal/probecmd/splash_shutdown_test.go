package probecmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// These processes are copies of this test executable. They load no model and
// never contact launchd, a live service or the network beyond a temporary loopback
// listener. A file asks the fake frontend to replace its first native child.
func TestSupervisedSplashRestartFailsRunAndVerifiesRealProcessCleanup(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("kernel process ownership is implemented on macOS")
	}
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
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
	router, frontend, engine := filepath.Join(parent, "router"), filepath.Join(parent, "python3.13"), filepath.Join(parent, "splash")
	for _, path := range []string{router, frontend, engine} {
		if err := os.WriteFile(path, raw, 0755); err != nil {
			t.Fatal(err)
		}
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listen := listener.Addr().String()
	listener.Close()
	statusPath, replace := filepath.Join(parent, "status.json"), filepath.Join(parent, "replace")
	helper := []string{"-test.run=^TestSplashRecoveryProcessHelper$", "--"}
	initial := append(slices.Clone(helper), "serve-native", "/prepared", "32768", "25769803776", "0")
	relaunch := append(slices.Clone(initial), "--relaunch-context", "32768")
	inv := Invocation{Path: router, Arguments: append(slices.Clone(helper), "router"), EnginePort: freePort(t),
		FrontendPath: frontend, FrontendArguments: append(slices.Clone(helper), "frontend"),
		EnginePath: engine, EngineArguments: initial, EngineRelaunchArguments: relaunch,
		Environment: append(os.Environ(), "TEMPER_SPLASH_RECOVERY=router", "TEMPER_SPLASH_LISTEN="+listen,
			"TEMPER_SPLASH_FRONTEND="+frontend, "TEMPER_SPLASH_ENGINE="+engine, "TEMPER_SPLASH_REPLACE="+replace, "GORACE=atexit_sleep_ms=0"),
		Supervision: &Supervision{StatusPath: statusPath, Root: parent, Installation: "fixture", Generation: strings.Repeat("c", 64), Listen: listen}}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	var output bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- runSupervised(ctx, inv, &output, &output) }()
	finished := false
	t.Cleanup(func() {
		cancel()
		if !finished {
			select {
			case <-done:
			case <-time.After(35 * time.Second):
				t.Error("helper supervisor did not terminate")
			}
		}
	})
	var ready probeStatus
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	waiting := time.NewTimer(10 * time.Second)
	defer waiting.Stop()
readyLoop:
	for {
		select {
		case err := <-done:
			finished = true
			t.Fatalf("helper stopped before readiness: %v; %s", err, &output)
		case <-waiting.C:
			t.Fatal("helper did not establish its three owned roles")
		case <-ticker.C:
			raw, _ := os.ReadFile(statusPath)
			_ = json.Unmarshal(raw, &ready)
			if ready.State == "running" && len(ready.Roles) == 3 && ready.ListenersVerified {
				break readyLoop
			}
		}
	}
	if err := os.WriteFile(replace, []byte("replace"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		finished = true
		if !errors.Is(err, errEngineRestarted) {
			t.Fatalf("run did not reject the native restart: %v; %s", err, &output)
		}
	case <-ctx.Done():
		t.Fatal("restart did not end the experiment")
	}
	raw, err = os.ReadFile(statusPath)
	if err != nil {
		t.Fatal(err)
	}
	var stopped probeStatus
	if err := json.Unmarshal(raw, &stopped); err != nil {
		t.Fatal(err)
	}
	if stopped.State != "stopped" || !stopped.SafeToCleanup || len(stopped.Processes) != 4 || !strings.Contains(stopped.Error, "engine process restarted") || !listenerClosed(listen) {
		t.Fatalf("failed run lacks a complete cleanup proof: %+v; %s", stopped, &output)
	}
	for _, process := range stopped.Processes {
		if err := syscall.Kill(-process.PGID, 0); !errors.Is(err, syscall.ESRCH) {
			t.Fatalf("owned group %d remains: %v", process.PGID, err)
		}
	}
}

func TestSplashRecoveryProcessHelper(t *testing.T) {
	role := os.Getenv("TEMPER_SPLASH_RECOVERY")
	if role == "" {
		return
	}
	signals, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()
	// A failed test cannot leave these model-free children running indefinitely.
	ctx, cancel := context.WithTimeout(signals, 20*time.Second)
	defer cancel()
	if role == "engine" {
		if slices.Contains(os.Args, "--relaunch-context") {
			<-ctx.Done()
			return
		}
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := os.Stat(os.Getenv("TEMPER_SPLASH_REPLACE")); err == nil {
					return
				}
			}
		}
	}
	helper := []string{"-test.run=^TestSplashRecoveryProcessHelper$", "--"}
	path, childRole := os.Getenv("TEMPER_SPLASH_ENGINE"), "engine"
	args := append(slices.Clone(helper), "serve-native", "/prepared", "32768", "25769803776", "0")
	if role == "router" {
		listener, err := net.Listen("tcp4", os.Getenv("TEMPER_SPLASH_LISTEN"))
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		path, childRole, args = os.Getenv("TEMPER_SPLASH_FRONTEND"), "frontend", append(slices.Clone(helper), "frontend")
	}
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			args = append(args, "--relaunch-context", "32768")
		}
		child := exec.Command(path, args...)
		// Like Splash, the native child inherits its frontend's group.
		if role == "router" {
			child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		}
		child.Env = append(os.Environ(), "TEMPER_SPLASH_RECOVERY="+childRole)
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- child.Wait() }()
		select {
		case <-ctx.Done():
			_ = child.Process.Signal(syscall.SIGTERM)
			<-done
			return
		case <-done:
			if role == "router" || attempt > 0 || ctx.Err() != nil {
				<-ctx.Done()
				return
			}
		}
	}
}
