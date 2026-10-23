package probecmd

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestManagedAcceptedConnections(t *testing.T) {
	for _, tc := range []struct {
		name, raw         string
		accepted, invalid bool
	}{
		{"none", "", false, false},
		{"outgoing", "p123\nf11\nn127.0.0.1:52356->127.0.0.1:12433\n", false, false},
		{"incoming", "p123\nf11\nn127.0.0.1:18090->127.0.0.1:52356\n", true, false},
		{"both", "p123\nf11\nn127.0.0.1:52356->127.0.0.1:12433\nf12\nn127.0.0.1:18090->127.0.0.1:52357\n", true, false},
		{"mapped", "p123\nf11\nn[::ffff:127.0.0.1]:18090->[::ffff:127.0.0.1]:52357\n", true, false},
		{"wrong-pid", "p124\nf11\nn127.0.0.1:52356->127.0.0.1:12433\n", false, true},
		{"unknown-field", "p123\nx11\n", false, true},
		{"missing-socket", "p123\nf11\n", false, true},
		{"missing-fd", "p123\nn127.0.0.1:52356->127.0.0.1:12433\n", false, true},
		{"wrong-local", "p123\nf11\nn192.0.2.1:52356->127.0.0.1:12433\n", false, true},
		{"malformed-peer", "p123\nf11\nn127.0.0.1:52356->unknown\n", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := managedAcceptedConnections([]byte(tc.raw), 123, netip.MustParseAddrPort("127.0.0.1:18090"))
			if got != tc.accepted || (err != nil) != tc.invalid {
				t.Fatal(got, err)
			}
		})
	}
}

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

func TestManagedQuiesceDistinguishesRealIncomingAndOutgoingSockets(t *testing.T) {
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
	if release, err := QuiesceManaged(identity, listen); release != nil || err == nil || !strings.Contains(err.Error(), "accepted connections") {
		t.Fatal("incoming client did not refuse", err)
	}
	if _, err := fmt.Fprintln(stdin, "ping"); err != nil {
		t.Fatal(err)
	}
	if !scanner.Scan() || scanner.Text() != "alive" {
		t.Fatal("refusal left router suspended")
	}
}
