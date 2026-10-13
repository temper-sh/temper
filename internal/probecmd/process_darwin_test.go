package probecmd

import (
	"encoding/binary"
	"os"
	"strings"
	"testing"
)

func TestKernelArgumentObservationMatchesOwnArguments(t *testing.T) {
	got, err := processArguments(os.Getpid())
	if err != nil || got != strings.Join(os.Args, "\x00") {
		t.Fatalf("kernel argv differs from actual process: %q, %v", got, err)
	}
}

func TestKernelArgumentsExcludeEnvironmentAndPreserveEmptyArguments(t *testing.T) {
	raw := make([]byte, 4)
	binary.NativeEndian.PutUint32(raw, 4)
	raw = append(raw, []byte("/usr/sbin/sysctl\x00\x00sysctl\x00-n\x00\x00kern.hv_vmm_present\x00PRIVATE=value\x00")...)
	got, err := parseProcessArguments(raw)
	if err != nil || got != "sysctl\x00-n\x00\x00kern.hv_vmm_present" {
		t.Fatal(got, err)
	}
	if inspectionCommand(processRow{executable: "/usr/sbin/sysctl", arguments: got}) {
		t.Fatal("flattening argv accepted an unreviewed empty argument")
	}
	for _, invalid := range [][]byte{nil, {1, 0, 0, 0}, append([]byte{2, 0, 0, 0}, []byte("/path\x00arg\x00unterminated")...)} {
		if _, err := parseProcessArguments(invalid); err == nil {
			t.Fatal("accepted incomplete kernel arguments")
		}
	}
}
