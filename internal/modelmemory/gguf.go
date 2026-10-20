package modelmemory

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ReadGGUF reads at most 32 MiB of metadata, never tensor data. Unsupported
// architectures or dimensions are unknowns, not guesses based on model names.
func ReadGGUF(input io.Reader) (*Attention, error) {
	r := &ggufReader{r: &io.LimitedReader{R: input, N: 32 << 20}}
	var magic [4]byte
	if _, err := io.ReadFull(r.r, magic[:]); err != nil {
		return nil, err
	}
	if string(magic[:]) != "GGUF" {
		return nil, errors.New("not a little-endian GGUF")
	}
	version, err := r.uint(4)
	if err != nil || version != 2 && version != 3 {
		return nil, errors.New("unsupported GGUF version")
	}
	if _, err = r.uint(8); err != nil { // tensor count
		return nil, err
	}
	count, err := r.uint(8)
	if err != nil || count > 65536 {
		return nil, errors.New("invalid GGUF metadata count")
	}
	architecture := ""
	numbers := map[string]uint64{}
	seen := map[string]bool{}
	complex := false
	for range count {
		key, err := r.str(1024)
		if err != nil {
			return nil, fmt.Errorf("GGUF metadata key: %w", err)
		}
		if seen[key] {
			return nil, fmt.Errorf("repeated GGUF metadata key %q", key)
		}
		seen[key] = true
		kind, err := r.uint(4)
		if err != nil {
			return nil, err
		}
		switch {
		case key == "general.architecture" && kind == 8:
			architecture, err = r.str(128)
		case kind == 4: // uint32; the GGUF model dimensions use this type.
			numbers[key], err = r.uint(4)
		default:
			err = r.skip(kind, 0)
		}
		if err != nil {
			return nil, fmt.Errorf("GGUF metadata %s: %w", key, err)
		}
		if strings.Contains(key, ".ssm.") || strings.Contains(key, ".attention.sliding_window") ||
			strings.Contains(key, ".attention.head_count") && kind == 9 {
			complex = true
		}
	}
	switch architecture {
	case "llama", "qwen2", "qwen3", "qwen3moe":
	default:
		return nil, fmt.Errorf("cache geometry for GGUF architecture %q is not modeled", architecture)
	}
	if complex {
		return nil, errors.New("hybrid, sliding-window or variable attention geometry is not modeled")
	}
	get := func(key string) int64 { return int64(numbers[architecture+"."+key]) }
	for _, key := range []string{"block_count", "embedding_length", "attention.head_count", "attention.head_count_kv", "attention.key_length", "attention.value_length"} {
		k := architecture + "." + key
		if seen[k] && numbers[k] == 0 {
			return nil, fmt.Errorf("GGUF %s must be a positive scalar uint32", k)
		}
	}
	heads, embedding := get("attention.head_count"), get("embedding_length")
	key, value := get("attention.key_length"), get("attention.value_length")
	if heads <= 0 || embedding <= 0 || embedding%heads != 0 {
		return nil, errors.New("missing or inconsistent GGUF attention dimensions")
	}
	if key == 0 {
		key = embedding / heads
	}
	if value == 0 {
		value = embedding / heads
	}
	kv := get("attention.head_count_kv")
	if kv == 0 && !seen[architecture+".attention.head_count_kv"] {
		kv = heads // GGUF's documented MHA default, not a GQA guess.
	}
	a := &Attention{Kind: "full-attention/v1", Architecture: architecture,
		Layers: get("block_count"), KVHeads: kv, KeyLength: key, ValueLength: value}
	if err := a.Validate(); err != nil {
		return nil, err
	}
	return a, nil
}

type ggufReader struct{ r *io.LimitedReader }

func (r *ggufReader) uint(size int) (uint64, error) {
	var b [8]byte
	if _, err := io.ReadFull(r.r, b[:size]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(b[:]), nil
}

func (r *ggufReader) str(limit uint64) (string, error) {
	n, err := r.uint(8)
	if err != nil {
		return "", err
	}
	if n > limit || n > uint64(r.r.N) {
		return "", errors.New("GGUF string exceeds metadata bound")
	}
	b := make([]byte, n)
	_, err = io.ReadFull(r.r, b)
	return string(b), err
}

func (r *ggufReader) discard(n uint64) error {
	if n > uint64(r.r.N) {
		return errors.New("GGUF metadata exceeds 32 MiB bound")
	}
	_, err := io.CopyN(io.Discard, r.r, int64(n))
	return err
}

func (r *ggufReader) skip(kind uint64, depth int) error {
	switch kind {
	case 0, 1, 7:
		return r.discard(1)
	case 2, 3:
		return r.discard(2)
	case 4, 5, 6:
		return r.discard(4)
	case 10, 11, 12:
		return r.discard(8)
	case 8:
		n, err := r.uint(8)
		if err != nil {
			return err
		}
		return r.discard(n)
	case 9:
		if depth != 0 {
			return errors.New("nested GGUF metadata arrays are unsupported")
		}
		element, err := r.uint(4)
		if err != nil {
			return err
		}
		n, err := r.uint(8)
		if err != nil || n > 1<<20 {
			return errors.New("GGUF array exceeds metadata bound")
		}
		for range n {
			if err := r.skip(element, depth+1); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown GGUF value type %d", kind)
	}
}
