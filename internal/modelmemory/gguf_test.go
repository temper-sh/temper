package modelmemory_test

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/temper-sh/temper/internal/modelmemory"
)

type entry struct {
	key   string
	value any
}

func header(entries []entry) []byte {
	var b bytes.Buffer
	b.WriteString("GGUF")
	put := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	str := func(s string) { put(uint64(len(s))); b.WriteString(s) }
	put(uint32(3))
	put(uint64(1))
	put(uint64(len(entries)))
	for _, e := range entries {
		str(e.key)
		switch v := e.value.(type) {
		case string:
			put(uint32(8))
			str(v)
		case uint32:
			put(uint32(4))
			put(v)
		case []string:
			put(uint32(9))
			put(uint32(8))
			put(uint64(len(v)))
			for _, s := range v {
				str(s)
			}
		case uint64:
			put(uint32(10))
			put(v)
		}
	}
	return b.Bytes()
}

func metadata() []entry {
	return []entry{
		{"general.architecture", "qwen3"},
		{"qwen3.block_count", uint32(32)},
		{"qwen3.embedding_length", uint32(4096)},
		{"qwen3.attention.head_count", uint32(32)},
		{"qwen3.attention.head_count_kv", uint32(8)},
		{"tokenizer.ggml.tokens", []string{"a", "b", "c"}},
	}
}

func TestGGUFGeometryReadsOnlyMetadataAndUsesDocumentedDefaults(t *testing.T) {
	r := bytes.NewReader(append(header(metadata()), []byte("tensor data remains unread")...))
	g, err := modelmemory.ReadGGUF(r)
	if err != nil || *g != geometry() || r.Len() != len("tensor data remains unread") {
		t.Fatalf("wrong geometry or consumed tensor bytes: %+v, %v, remaining %d", g, err, r.Len())
	}
	entries := metadata()
	entries = append(entries, entry{"qwen3.attention.key_length", uint32(64)}, entry{"qwen3.attention.value_length", uint32(256)})
	g, err = modelmemory.ReadGGUF(bytes.NewReader(header(entries)))
	if err != nil || g.KeyLength != 64 || g.ValueLength != 256 {
		t.Fatal("explicit head dimensions lost", g, err)
	}
	entries = append(metadata()[:4], metadata()[5:]...)
	g, err = modelmemory.ReadGGUF(bytes.NewReader(header(entries)))
	if err != nil || g.KVHeads != 32 {
		t.Fatal("missing KV-head count should use documented MHA default", g, err)
	}
}

func TestUnknownMalformedAndHybridGGUFNeverBecomeAnEstimate(t *testing.T) {
	for _, extra := range []entry{
		{"qwen3.attention.sliding_window", uint32(4096)},
		{"qwen3.ssm.state_size", uint32(128)},
		{"qwen3.attention.key_length", uint32(0)},
		{"qwen3.attention.key_length", []string{"128"}},
		{"qwen3.attention.key_length", uint64(128)},
		{"qwen3.block_count", uint32(33)}, // repeated key
	} {
		if _, err := modelmemory.ReadGGUF(bytes.NewReader(header(append(metadata(), extra)))); err == nil {
			t.Fatal("unsupported metadata accepted", extra.key)
		}
	}
	entries := metadata()
	entries[0].value = "qwen35"
	if _, err := modelmemory.ReadGGUF(bytes.NewReader(header(entries))); err == nil {
		t.Fatal("unknown architecture guessed from similar dimensions")
	}
	raw := header(metadata())
	for i := range len(raw) {
		if _, err := modelmemory.ReadGGUF(bytes.NewReader(raw[:i])); err == nil {
			t.Fatalf("truncation at %d was accepted", i)
		}
	}
	binary.LittleEndian.PutUint64(raw[16:], math.MaxUint64)
	if _, err := modelmemory.ReadGGUF(bytes.NewReader(raw)); err == nil {
		t.Fatal("unbounded metadata count accepted")
	}
}
