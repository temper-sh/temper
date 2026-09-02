# Current-posture render acceptance

Accepted locally on 2026-08-19 and rerun after the atomic artifact-set path
landed on 2026-08-20. The original CLI witness below predates mandatory
artifact admission and explicit `--no-mmproj`; their apply-path coverage is
hermetic. The separate current-command runtime witness below does not replace
that apply coverage. Neither witness is a catalog recommendation or permission
to activate Temper on the live machine. The legacy stack remains live through
the release cutover gate.

The fixture was extended hermetically on 2026-09-02 to reproduce the frozen
Field Kit llama runtime profile: 16 context checkpoints, a disabled RAM prompt
cache, and the supported reasoning-off control. A separate isolated runtime
witness on the same date exercised those flags without using or changing the
live service.

## Inputs and witness

The manually maintained fixture is
`internal/render/testdata/current-posture/{manifest.yaml,manifest.lock.yaml}`.
It records the current coder plus CPU reranker posture with exact model and
patched-template pins. The coder revision is the revision used by the
Qwen3.8 engine A/B witness; the reranker revision is the locally resolved
snapshot. Hash verification of the multi-GB files was deliberately not
repeated in the routine render pass; the separate runtime witness below did
rehash the coder weights.

## Isolated runtime flag witness

On 2026-09-02 the frozen coder command was run directly against the installed
llama.cpp `llama-server` 0.3.0, build 10621 (`c1d0e7a00`), on an unused
loopback port with a temporary home and offline environment. The invocation
used the exact locked local model and template plus explicit
`--host 127.0.0.1`, `--ctx-checkpoints 16`, `--cache-ram 0`, and
`--reasoning off`; it did not route through or reload the live llama-swap.

The process accepted the complete command, loaded in about five seconds, and
reported one slot with `n_ctx_slot = 24576`. Its cache diagnostic disabled the
idle-slot cache because the RAM cache was disabled. `/health` returned
`{"status":"ok"}`, `/props` identified the exact local model path and
text-only modalities, and a deterministic chat request asking for only `OK`
returned exactly `OK` with no reasoning content. The process then shut down
cleanly and released the port.

The template and 17.1 GB model were rehashed and both matched their locked
SHA-256 identities. The flag witness therefore establishes artifact integrity,
command acceptance, startup, basic inference, and shutdown for this installed
engine and cached artifact.

The native CLI rendered the fixture with the existing Pi files as optional
bases into an ignored `.scratch/current-posture/root`. The historical observed
sequence, before artifact admission changed the precondition, was:

```text
dry-run     would-change; root remained absent
first apply changed; generation 7e53b8af7b1ad6de1447cb8c4f47fb072c02cad65b49f5599e77dbe39578c4a6
second run  unchanged; the same sole generation remained selected
```

This generation ID includes the explicit isolated root and the owner's
current unowned Pi settings, so it is a witness for this run rather than a
portable expected constant. Hermetic tests render the same fixture beneath
`/temper` and assert the owned semantics.

## Semantics retained

Comparison against the live legacy-generated files retained:

- llama-swap global timeout, port, TTL and capture-buffer values;
- both model IDs, coder display name, per-model TTLs and pinned/heavy routing;
- coder window, parallelism, 16 context checkpoints, disabled RAM prompt
  cache, flash attention, Q8 KV, batch/ubatch, patched chat template, and
  reasoning disabled through the engine's supported option;
- reranker mode, 4K window, one slot, 256 batch/ubatch and CPU-only `ngl: 0`;
- explicit `--no-mmproj` on every text-only layout;
- no llama-swap startup hook—the current posture does not preload;
- Pi's sole local coder, provider compatibility, 24K context, 4K maximum
  output, non-reasoning/text contract and zero costs;
- Pi compaction at reserve 3,072 and keep-recent 7,168; and
- unowned Pi providers and settings from the supplied base documents.

## Reviewed differences

These differences are intentional and covered by the acceptance unit:

| Native output | Legacy output | Reason |
|---|---|---|
| Exact lock-derived local `-m` path plus `--offline` | Moving `-hf` selector | Prevent branch drift and runtime network resolution. |
| Explicit `--no-mmproj` | Projector discovery left to repo contents | Prevent an undeclared vision projector from arriving or loading. |
| Content-addressed patch path under the explicit Temper root | Absolute path into the legacy checkout | Make generation identity portable and remove the legacy-repo dependency. |
| `-c`, `-b`, `-ub`; explicit `-fa auto` for reranking | Long aliases; flash attention omitted | llama.cpp-equivalent forms; `auto` records the actual default instead of misrepresenting it as `off`. |
| `--ctx-checkpoints 16` and `--cache-ram 0` | Flags absent | Reproduce the frozen Field Kit memory/cache profile instead of inheriting materially different engine defaults. |
| `--reasoning off` | Deprecated `--chat-template-kwargs` thinking override | Use llama.cpp's supported reasoning control rather than a template-parser implementation detail. |
| Explicit reranker display name | No reranker name | The native layout owns human-readable identity. |
| Pi `defaultModel` is the manifest's preferred coder | `defaultModel: null` | Make the user's explicit preference deterministic. It still does not preload the model. |

## Not established by this pass

The renderer pass did not materialize artifacts, touch consumer homes, or
reload llama-swap. The separate direct runtime witness did start and exercise
llama-server, but it did not run through Temper's apply/receipt path or the
generated llama-swap configuration, fetch the model anew, measure
the memory effect of context checkpoints independently, or qualify another
engine. Resolver/materialization and apply's artifact admission have separate
hermetic product coverage. The combined claim is deterministic configuration
semantics plus isolated artifact verification, acceptance, and basic inference
for the frozen llama-server command—not a live cutover or multi-engine quality
claim.
