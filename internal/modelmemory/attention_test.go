package modelmemory_test

import (
	"math"
	"testing"

	"github.com/temper-sh/temper/internal/modelmemory"
)

func geometry() modelmemory.Attention {
	return modelmemory.Attention{Kind: "full-attention/v1", Architecture: "qwen3", Layers: 32, KVHeads: 8, KeyLength: 128, ValueLength: 128}
}

func TestGQAStorageIncludesBothCachesAndQuantizationScales(t *testing.T) {
	for _, tc := range []struct {
		format string
		mib    int64
	}{{"f16", 512}, {"q8", 272}, {"q4", 144}} {
		for _, slots := range []int{1, 2, 4} {
			got, err := modelmemory.LlamaKV(geometry(), 4096, slots, tc.format)
			if err != nil || got != tc.mib<<20 {
				t.Fatalf("%s, %d slots: %d bytes, %v; want %d MiB for the shared 4K context", tc.format, slots, got, err, tc.mib)
			}
			double, err := modelmemory.LlamaKV(geometry(), 8192, slots, tc.format)
			if err != nil || double != 2*got {
				t.Fatal("doubling context did not double the cache", double, err)
			}
		}
	}
	g := geometry()
	g.ValueLength = 64
	got, err := modelmemory.LlamaKV(g, 4096, 1, "q8")
	if err != nil || got != 204<<20 {
		t.Fatal("asymmetric K/V widths were conflated", got, err)
	}
}

func TestContextPaddingAndInvalidArithmetic(t *testing.T) {
	got, err := modelmemory.LlamaTokens(4097, 2)
	if err != nil || got != 4608 {
		t.Fatal("split cache padding", got, err)
	}
	for _, tc := range []struct{ context, parallel int }{{0, 1}, {100, 0}, {1, 1024}, {math.MaxInt, 1}} {
		if _, err := modelmemory.LlamaTokens(tc.context, tc.parallel); err == nil {
			t.Fatal("invalid allocation accepted", tc)
		}
	}
	g := geometry()
	g.KeyLength, g.KVHeads = 31, 1
	if _, err := modelmemory.LlamaKV(g, 4096, 1, "q4"); err == nil {
		t.Fatal("unaligned quantized row accepted")
	}
	g = geometry()
	g.Layers = math.MaxInt64
	if _, err := modelmemory.LlamaKV(g, 4096, 1, "f16"); err == nil {
		t.Fatal("overflow accepted")
	}
}
