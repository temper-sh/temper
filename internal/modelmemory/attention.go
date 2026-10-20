// Package modelmemory derives cache geometry from model metadata and estimates
// the storage of supported attention caches. It does not measure runtime memory.
package modelmemory

import (
	"errors"
	"fmt"
	"math"
)

// Attention describes a uniform, full-attention decoder. It is attached to the
// exact artifact whose metadata establishes these dimensions. Hybrid, sliding
// window, recurrent and compressed/latent attention need different estimators.
type Attention struct {
	Kind         string `json:"kind" yaml:"kind"`
	Architecture string `json:"architecture" yaml:"architecture"`
	Layers       int64  `json:"layers" yaml:"layers"`
	KVHeads      int64  `json:"kv_heads" yaml:"kv_heads"`
	KeyLength    int64  `json:"key_length" yaml:"key_length"`
	ValueLength  int64  `json:"value_length" yaml:"value_length"`
}

func (a Attention) Validate() error {
	if a.Kind != "full-attention/v1" || a.Architecture == "" {
		return errors.New("context_memory requires full-attention/v1 and an architecture")
	}
	if a.Layers <= 0 || a.KVHeads <= 0 || a.KeyLength <= 0 || a.ValueLength <= 0 {
		return errors.New("context_memory dimensions must be positive")
	}
	if a.KeyLength > math.MaxInt64-a.ValueLength {
		return errors.New("context_memory dimensions overflow")
	}
	if _, err := Product(a.Layers, a.KVHeads, a.KeyLength+a.ValueLength); err != nil {
		return errors.New("context_memory dimensions overflow")
	}
	return nil
}

func Product(values ...int64) (int64, error) {
	n := int64(1)
	for _, v := range values {
		if v < 0 || v != 0 && n > math.MaxInt64/v {
			return 0, errors.New("memory estimate overflows")
		}
		n *= v
	}
	return n, nil
}

// LlamaTokens models the padded total -c allocation used by Temper's adapter.
// Parallel slots share that total; -c is not multiplied by --parallel. Padding
// each split to 256 also covers non-unified caches (the larger allocation).
func LlamaTokens(context, parallel int) (int64, error) {
	if context <= 0 || parallel <= 0 {
		return 0, errors.New("context and parallel slots must be positive")
	}
	pad := func(n int64) (int64, error) {
		if n > math.MaxInt64-255 {
			return 0, errors.New("context allocation overflows")
		}
		return (n + 255) / 256 * 256, nil
	}
	total, err := pad(int64(context))
	if err != nil {
		return 0, err
	}
	perSlot, err := pad(total / int64(parallel))
	if err != nil || perSlot == 0 {
		return 0, errors.New("context allocation is too small for parallel slots")
	}
	total, err = Product(perSlot, int64(parallel))
	return total, err
}

// LlamaKV counts K and V payloads, including GGML block scale bytes. Tensor
// headers, allocator padding, graphs and backend work buffers are not included.
func LlamaKV(a Attention, context, parallel int, dtype string) (int64, error) {
	if err := a.Validate(); err != nil {
		return 0, err
	}
	tokens, err := LlamaTokens(context, parallel)
	if err != nil {
		return 0, err
	}
	block, bytes := int64(1), int64(2)
	switch dtype {
	case "f16":
	case "q8":
		block, bytes = 32, 34
	case "q4":
		block, bytes = 32, 18
	default:
		return 0, fmt.Errorf("unmodeled llama KV cache format %q", dtype)
	}
	var row int64
	for _, length := range []int64{a.KeyLength, a.ValueLength} {
		elements, err := Product(a.KVHeads, length)
		if err != nil {
			return 0, err
		}
		if elements%block != 0 {
			return 0, errors.New("KV dimensions are not aligned to the cache quantization block")
		}
		n, err := Product(elements/block, bytes)
		if err != nil || row > math.MaxInt64-n {
			return 0, errors.New("KV row size overflows")
		}
		row += n
	}
	return Product(row, a.Layers, tokens)
}
