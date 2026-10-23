# Kapitsa native runtime

`kapitsa/v1` serves text chat from an explicitly selected MLX safetensors
snapshot and complete DFlash2 draft. It uses a pinned native release through
the existing release installer, exact artifact receipts, router readiness and
owned-process supervision. It requires Apple M1 or newer and macOS 14 or later;
measurements on M5 do not establish performance or memory fit on other chips.

The typed profile owns `slots`, `max_memory_bytes`, `prefill_chunk_tokens` and
`ane`. Context and request defaults retain their existing preset homes. Byte
allowances render as decimal GB without rounding through a float. Disk caching
is disabled. DFlash block/tree decisions remain engine-owned. No model switch,
auto-update, implicit download, template override or external draft discovery is
enabled. The adapter supplies explicit target/draft paths, a loopback listener,
`KAPITSA_NO_AUTO_UPDATE=1`, `KAPITSA_NO_PROMPT=1`, offline HF settings and
`NI_ANE=0|1`. Unsupported penalties, min-p and oversized seeds are refused.
Thinking and sampling are router request defaults; explicit client values retain
the router's existing override behavior. `reasoning_effort` is the thinking
override (`none` disables it). An omitted output cap supplies the context size
as `max_tokens`, overriding Kapitsa's implicit 16,384-token cap; the engine stops
at its context boundary. Buffered and streamed tool calls use
the engine's chat API without a second parser.

Keep the Temper root on APFS when model storage is exFAT. Existing artifact sets
form an owned directory of links to only the declared, verified model and draft
files; the source cache and weights stay unchanged. AppleDouble files cannot be
declared as model material, and unexpected files in the engine's view fail
verification before startup. Missing or redirected cache files require normal
preparation/verification, never an implicit engine download.

The writable home is `ROOT/runtime/kapitsa/<identity>`, derived from preset,
artifact-set and software identities. It is created immediately before serving,
with symlinked parents refused. Context changes retain local calibration;
different software or model material gets a separate home. Retries preserve it.
`execution paths --json` reports its path and SHA-256 identities of current
`kflash/*.json` calibration files. Inspection is read-only, including before
first startup. Fresh installations calibrate locally: research calibration is
not copied into a user's root or silently treated as fixed execution input.

Memory guidance uses applicable workload/counter observations and otherwise
marks unsupported context, workspace and ANE-service accounting as unknown.
The engine allowance is a ceiling, never a minimum RAM requirement. Process
footprint is not the Metal allocation counter, so it does not justify a wired
limit refusal or sysctl recommendation. Historical evidence stays unchanged
when software, weights, placement or profile settings change.

The selected catalog candidate requires Temper 0.4.0. Its pinned native release,
model revisions and measured profile come from the
[Qwen configuration reference](https://temper-sh.github.io/models/qwen3.8-27b/layouts/kapitsa-mlx4-reference.html).
