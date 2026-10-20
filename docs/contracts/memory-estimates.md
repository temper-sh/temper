# Memory accounting

Setup, layout planning and `preset check` share one assessment of the saved
execution. An applicable catalog measurement supplies the accounted memory.
Otherwise Temper estimates it and explains why the measurement is unavailable
or inapplicable. Neither an observed peak nor an incomplete estimate establishes
worst-case runtime fit.

## Catalog measurements

Each profile can carry one reviewed `memory` observation:

- `observed_on` (YYYY-MM-DD), public HTTPS assessment `source`, `workload` and
  `limitations`;
- `counter`: `engine-rss`, `engine-physical-footprint`,
  `engine-native-allocation` or `system-wired-growth`;
- `loaded_bytes` (nullable) and positive `peak_bytes` for that same counter;
- `machine`: chip, physical memory bytes, Metal budget in MiB and observed OS;
- `execution`: engine version and supply hashes, target/draft/projector file
  identities, compute placement, context, engine configuration and speculation.

The counter must support the accounting claim. For example, an engine footprint
that omits mapped weights cannot represent total runtime use. The research
record retains other counters, exact requests and sampling details; the catalog
carries the reviewed observation used for planning. Do not sum overlapping
counters or combine loaded and peak readings from different executions.

`Lock.MemoryExecution()` describes an execution; it does not prove that it ran.
Backfill from the tested lock and verify its identity against the selected
preset. Changing engine binaries/version, weights, draft, projector, placement,
context, batch/cache settings or speculation invalidates applicability.
The original measurement stays readable. Names and download locations do not
change applicability. Request sampling is described by the workload; OS is
retained as provenance. Chip, physical memory and Metal budget must match the
observed machine.

When applicable, `memory_estimate.basis` and `status` are `measured`,
`measurement` contains the observation, and `memory_bytes` uses its peak without
adding estimates or overlapping envelopes. Loaded memory remains visible but
is not the admission value. The OS allowance is added once. Actual populated
input, output and concurrency must remain visible in the workload and limits;
allocation-only smoke tests do not establish full-window memory use.

When absent or inapplicable, `basis` is `estimated`, `status` is `incomplete`
and `fallback_reason` explains a rejected observation. The estimate below
supplies `memory_bytes`. Readers predating this profile field cannot consume
it; publish sources carrying measurements only with compatible Temper support.

## Estimation fallback

The report separates model-file bytes (a weight-storage proxy), estimated KV
payload, configured prompt-cache reserve and engine allocation envelopes.
Without an applicable measurement, `memory_bytes` is the accounted estimate:
the larger of weights plus estimated KV and the engine envelope, plus any
separate host prompt-cache reserve. An envelope already includes weights/cache;
it is never added to them. The OS allowance is added
once at machine/layout admission. File size is not resident memory, and none
of these fields is a measurement of loaded or peak memory.

Optional artifact metadata supplies cache geometry before weights are installed:

```json
"context_memory": {
  "kind": "full-attention/v1",
  "architecture": "qwen3",
  "layers": 32,
  "kv_heads": 8,
  "key_length": 128,
  "value_length": 128
}
```

This describes uniform full attention only. Geometry belongs to the exact
artifact, survives compilation into locks and follows artifact replacement.
It is descriptive metadata, not a change in executable identity. Catalog authors
must derive it from that artifact's metadata. `preset weights` attempts a bounded
GGUF-header read for supported Llama, Qwen2, Qwen3 and Qwen3-MoE architectures;
missing or unsupported metadata leaves an explicit unknown and does not prevent
using the weights. It never downloads weights to estimate memory.

The llama-server estimator counts separate K and V widths, layers and padded
context allocation. F16 uses 2 bytes per element; Q8_0 uses 34 bytes per 32
elements, Q4_0 18 per 32. Temper passes context as total `-c`, shared among
parallel slots; the estimator rounds each split to 256 tokens, conservatively
covering non-unified allocation. It does not multiply the whole context by
parallelism. A configured prompt-cache RAM cap is reserved separately, not
reported as currently allocated memory.

Unknowns name their reason: missing geometry, hybrid/recurrent or sliding-window
state, speculative decoding, vision work buffers, and engine behavior without a
supported estimator. Python allocation envelopes remain visible; their caches
are not inferred from a llama formula or added again to the envelope. Splash's
`max_memory_bytes` is shown separately as `engine_limit_bytes`: it is an upper
allocation limit, not a required allocation. It does not increase the estimate
or cause refusal merely because it equals the Metal budget. Graphs, allocator
overhead and loaded/peak use remain unknown in the fallback. Consequently
`preset check` can refuse an estimate exceeding the budget but does not emit
`fits: true` from these inputs.

Layout `all_fit` is the scheduling decision based on the selected measurements
or estimates, not proof of simultaneous residency. `memory_incomplete` exposes
unknown allocations and unmeasured workloads. Startup admission uses the same
selection. Unknown components alone do not prevent installation or activation.

Arithmetic references:
[llama context allocation](https://github.com/ggml-org/llama.cpp/blob/b11396/src/llama-context.cpp),
[KV tensors](https://github.com/ggml-org/llama.cpp/blob/b11396/src/llama-kv-cache.cpp),
and [GGML block layouts](https://github.com/ggml-org/llama.cpp/blob/b11396/ggml/src/ggml-common.h).
The header reader follows the [GGUF specification](https://github.com/ggml-org/ggml/blob/master/docs/gguf.md).
These support estimates, not guarantees across engine releases.
