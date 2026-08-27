# Current-posture render acceptance

Accepted locally on 2026-08-19 and rerun after the atomic artifact-set path
landed on 2026-08-20. The original CLI witness below predates mandatory
artifact admission and explicit `--no-mmproj`; those additions are covered by
the hermetic renderer, artifact-set, apply, and CLI tests rather than a second
multi-GB machine run. This remains a renderer acceptance snapshot, not a
catalog recommendation or permission to activate Temper on the live machine.
The legacy stack remains live through the release cutover gate.

## Inputs and witness

The manually maintained fixture is
`internal/render/testdata/current-posture/{manifest.yaml,manifest.lock.yaml}`.
It records the current coder plus CPU reranker posture with exact model and
patched-template pins. The coder revision is the revision used by the
Qwen3.8 engine A/B witness; the reranker revision is the locally resolved
snapshot. Hash verification of the multi-GB files was deliberately not
repeated in this routine render pass.

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
- coder window, parallelism, flash attention, Q8 KV, batch/ubatch, patched
  chat template and non-thinking template argument;
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
| Explicit reranker display name | No reranker name | The native layout owns human-readable identity. |
| Pi `defaultModel` is the manifest's preferred coder | `defaultModel: null` | Make the user's explicit preference deterministic. It still does not preload the model. |

## Not established by this pass

This dated pass did not materialize artifacts, hash the multi-GB weights again,
start llama-server, touch consumer homes, reload llama-swap, or exercise a
model. Resolver/materialization and apply's artifact admission now have
separate hermetic product coverage, but a real weight fetch remains
deliberately outside this renderer witness. The acceptance claim is limited to
deterministic configuration semantics and isolated commit behavior.
