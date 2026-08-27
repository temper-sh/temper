package baselinerun_test

import (
	"testing"

	"github.com/temper-sh/temper/internal/fieldkit/baselinerun"
)

func TestIdentifyReadsOnlyAValidSessionSelector(t *testing.T) {
	data := []byte(`{"schema":"temper-field-kit-baseline-session/v1","baseline":{"id":"qwen38-dynamic-q4xl","revision":3,"package_sha256":"ignored","source_sha256":"ignored"},"untrusted":"ignored"}`)
	identity, err := baselinerun.Identify(data)
	if err != nil {
		t.Fatal(err)
	}
	if identity.ID != "qwen38-dynamic-q4xl" || identity.Revision != 3 {
		t.Fatalf("identity = %#v", identity)
	}
}

func TestIdentifyRefusesMalformedSessionSelectors(t *testing.T) {
	tests := [][]byte{
		[]byte(`{`),
		[]byte(`{"schema":"wrong","baseline":{"id":"qwen38-dynamic-q4xl","revision":3}}`),
		[]byte(`{"schema":"temper-field-kit-baseline-session/v1","baseline":{"id":"QWEN","revision":3}}`),
		[]byte(`{"schema":"temper-field-kit-baseline-session/v1","baseline":{"id":"qwen38-dynamic-q4xl","revision":0}}`),
	}
	for _, data := range tests {
		if _, err := baselinerun.Identify(data); err == nil {
			t.Fatalf("Identify(%q) succeeded", data)
		}
	}
}
