package uv

import (
	"strings"
	"testing"
)

func TestBoundedBufferRetainsOnlyItsLimitAndReportsOverflow(t *testing.T) {
	buffer := newBoundedBuffer(4)
	written, err := buffer.Write([]byte("abcdef"))
	if err != nil || written != 6 || string(buffer.Bytes()) != "abcd" || !buffer.overflow {
		t.Fatalf("buffer = written %d data %q overflow %t error %v", written, buffer.Bytes(), buffer.overflow, err)
	}
	if _, err := buffer.Write([]byte("later")); err != nil || string(buffer.Bytes()) != "abcd" {
		t.Fatalf("second write = data %q error %v", buffer.Bytes(), err)
	}
}

func TestUVEnvironmentDropsEveryProviderOverridePrefix(t *testing.T) {
	environment := uvEnvironment([]string{
		"PATH=/bin", "UV_INDEX=private", "PIP_INDEX_URL=private", "PYTHONHOME=private",
		"VIRTUAL_ENV=private", "CONDA_PREFIX=private", "FORCE_COLOR=1",
	})
	joined := "\n" + strings.Join(environment, "\n") + "\n"
	for _, forbidden := range []string{"UV_INDEX=", "PIP_INDEX_URL=", "PYTHONHOME=", "VIRTUAL_ENV=", "CONDA_PREFIX=", "FORCE_COLOR="} {
		if strings.Contains(joined, "\n"+forbidden) {
			t.Fatalf("environment retains %q: %v", forbidden, environment)
		}
	}
	if !strings.Contains(joined, "\nPATH=/bin\n") || !strings.Contains(joined, "\nUV_NO_CONFIG=1\n") {
		t.Fatalf("environment = %v", environment)
	}
}
