package probecmd

import (
	"net"
	"os"
	"runtime"
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
	t.Parallel()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	held := listener.Addr().(*net.TCPAddr).Port
	err = EnginePortsFree(held, 1)
	if err == nil || !strings.Contains(err.Error(), "engine port "+strconv.Itoa(held)+" ") {
		t.Fatalf("held engine port was not reported: %v", err)
	}
	if runtime.GOOS == "darwin" && !strings.Contains(err.Error(), "process "+strconv.Itoa(os.Getpid())) {
		t.Fatalf("held engine port did not identify this process: %v", err)
	}
}

func TestEnginePortRangeRefusesTheFirstOccupiedPort(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		occupied map[string]bool
		want     string
	}{
		{name: "all free"},
		{name: "first held", occupied: map[string]bool{"127.0.0.1:12000": true}, want: "engine port 12000 "},
		{name: "last held", occupied: map[string]bool{"127.0.0.1:12001": true}, want: "engine port 12001 "},
		{name: "both held", occupied: map[string]bool{"127.0.0.1:12000": true, "127.0.0.1:12001": true}, want: "engine port 12000 "},
		{name: "outside range", occupied: map[string]bool{"127.0.0.1:11999": true, "127.0.0.1:12002": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			closed := func(address string) bool { return !tc.occupied[address] }
			holder := func(int) string { return "fixture (process 42)" }
			err := enginePortsFree(12000, 2, closed, holder)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "fixture (process 42)") {
				t.Fatalf("wanted %q with process identity, got %v", tc.want, err)
			}
		})
	}
}
