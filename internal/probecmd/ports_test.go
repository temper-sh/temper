package probecmd

import (
	"net"
	"strconv"
	"strings"
	"testing"
)

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func TestHeldEnginePortRefusesServingAndNamesThePort(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	held := listener.Addr().(*net.TCPAddr).Port
	err = EnginePortsFree(held-1, 2)
	if err == nil || !strings.Contains(err.Error(), "engine port "+strconv.Itoa(held)+" ") {
		t.Fatalf("held engine port was not reported: %v", err)
	}
	if err := EnginePortsFree(freePort(t), 1); err != nil {
		t.Fatalf("free engine port refused: %v", err)
	}
}
