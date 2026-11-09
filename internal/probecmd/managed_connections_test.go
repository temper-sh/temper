package probecmd

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// Disposable socket helper: no launchd, model, or inference. The parent owns
// this lifetime and tests real lsof endpoints and SIGSTOP/SIGCONT behavior.
func TestManagedSocketProcess(t *testing.T) {
	peer := os.Getenv("TEMPER_TEST_SOCKET_PEER")
	if peer == "" {
		return
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	outgoing, err := net.Dial("tcp4", peer)
	if err != nil {
		t.Fatal(err)
	}
	defer outgoing.Close()
	fmt.Println(listener.Addr())
	go func() {
		incoming, err := listener.Accept()
		if err == nil {
			defer incoming.Close()
			fmt.Fprintln(incoming, "accepted")
			_, _ = bufio.NewReader(incoming).ReadByte()
		}
	}()
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		fmt.Println("alive")
	}
}

func TestManagedQuiesceHoldsTheRouterWithIdleClientConnections(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS kernel/lsof observation")
	}
	peer, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestManagedSocketProcess$")
	cmd.Env = append(os.Environ(), "TEMPER_TEST_SOCKET_PEER="+peer.Addr().String())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		t.Fatal("helper failed before listening")
	}
	listen := scanner.Text()
	rows, err := readRows()
	if err != nil {
		t.Fatal(err)
	}
	identity := ManagedIdentity{}
	for _, row := range rows {
		if row.pid == cmd.Process.Pid {
			identity.ProcessIdentity = ProcessIdentity{ID: "router", PID: row.pid, PGID: row.pgid, Started: row.started}
		}
	}
	identity.Path, err = processExecutable(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	identity.Arguments, err = processArguments(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	release, err := QuiesceManaged(identity, listen)
	if err != nil {
		t.Fatal("outgoing keepalive prevented quiesce", err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	incoming, err := net.Dial("tcp4", listen)
	if err != nil {
		t.Fatal(err)
	}
	defer incoming.Close()
	if line, err := bufio.NewReader(incoming).ReadString('\n'); err != nil || line != "accepted\n" {
		t.Fatal(line, err)
	}
	// The client is this test process: the refusal names the app to close.
	// An idle client connection is not work: the router's own shutdown closes
	// it. The hold still freezes the router until release.
	release, err = QuiesceManaged(identity, listen)
	if err != nil {
		t.Fatal("idle client refused quiesce", err)
	}
	if _, err := fmt.Fprintln(stdin, "ping"); err != nil {
		t.Fatal(err)
	}
	answered := make(chan bool, 1)
	go func() { answered <- scanner.Scan() && scanner.Text() == "alive" }()
	select {
	case <-answered:
		t.Fatal("held router kept running")
	case <-time.After(300 * time.Millisecond):
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	select {
	case ok := <-answered:
		if !ok {
			t.Fatal("released router did not resume")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("release left router suspended")
	}
}
