# Guided setup catalog candidate

[`guided-setup.json`](guided-setup.json) is an unpublished authoring catalog for
macOS ARM64. It offers four local main models and a compact utility profile for
a harness-owned foreground model. Descriptions draw on the existing
[capability-work suite](../../v3/workshop/suites/capability-work/README.md) and
its [assessed portfolio](../../v3/labs/workstreams/small-model-capability-portfolio/README.md).
They describe observed tasks and limitations; they are editable starting points
for the owner's assessment.

| Profile | User choice | Route |
|---|---|---|
| `gemma-4-e2b-local` | Small local main, Q4_K_M | One `default` route, resident |
| `gemma-4-e4b-local` | Small local main, Q4_K_M | One `default` route, resident |
| `qwen3.5-4b-local` | Compact local main, Q4_K_M | One `default` route, resident |
| `qwen3.5-4b-utility` | Local text utility beside an external foreground | One `available` route, loaded on demand |
| `qwen3.8-27b-q4xl-local` | Larger local main | Existing `default` route, resident |

```sh
./build/temper init --catalog catalog/guided-setup.json --dry-run
```

Users explicitly choose a profile before compilation or installation. A local
compile checks the authoring contract and produces an exact execution lock; it
does not test behavior, download weights, install software or publish this
candidate. See the [execution-lock contract](../docs/contracts/execution-lock.md).

The chooser shows **S (>16–32 GB)** before **XS (up to 16 GB)** when both have
available choices. These placements are estimates, separate from machine checks.
Use Space to select models to install and Enter or `d` to choose the default
local model. Other selected configurations remain installed alternatives.
Each has independent template and context choices.

To change the shared display order, reorder layout IDs in `layout_order` in
[`guided-setup.json`](guided-setup.json). The order applies within each tier;
unlisted choices follow in stable profile-ID order. Model and weight labels live
on their artifact, engine labels on the engine, and memory tiers on layouts.
Editing these fields does not alter execution settings or saved user defaults.
The S order is Qwen–Splash first and Qwen–llama last. The Splash choice installs
[Splash 1.1.0](https://github.com/incoai/splash/releases/tag/1.1.0), the shared
Unsloth UD-Q4_K_XL target, and its matching DFlash2 draft. Frog remains the selected
template. Splash requires Apple M3 or newer and macOS 26.4 or later; incompatible
machines show it among unavailable choices.

`qwen3.8-27b-splash-local` is the profile and `qwen3.8-27b-q4xl-splash` its layout.
The draft is included automatically, pinned at its own revision. Preparing the
selection derives a local tokenizer without loading the model. First start also
builds Splash's weight cache, requiring additional disk space shown in review.
The catalog uses int8 KV, medium reasoning, and a 24 GiB engine memory ceiling.
Context remains an explicit choice without a new 1.1.0 capacity finding.
Historical Splash coding evidence retains its original engine identity.
A [native integration check](../docs/PLAN.md#splash-110-integration) passed for
1.1.0 / llama-swap v257 at 32k advertised context with short chat and tool requests;
these versions are available through the explicit tested-software choice.

For scripted installation choices:

```sh
./build/temper init --catalog catalog/guided-setup.json \
  --profile qwen3.8-27b-q4xl-local --profile gemma-4-e2b-local \
  --default-profile qwen3.8-27b-q4xl-local \
  --context qwen3.8-27b-q4xl-mtp=40960 \
  --context gemma-4-e2b-q4km-off=16384 --dry-run
```

The published stable catalog and [`qwen38-m5-refresh.json`](qwen38-m5-refresh.json)
retain their original 32k configuration. That specimen reproduces the old
setup and protects issued execution identities; this candidate supplies the
new choices. Saved user choices are never silently expanded.

## Assessed uses and boundaries

The [job-based choice guide](../../v3/results/guides/choose-a-local-model.md)
explains the differences among these candidates. In particular, Qwen3.5 is not
the lowest-memory small option in the historical measurements: both Gemma
configurations had lower observed server RSS. That comparison remains attached
to the tested M5/32 GiB compositions, not inferred minimum RAM. The current
filled-context allocation observed lower Qwen3.5 RSS than either Gemma;
configuration and workload matter more than a model-level memory ranking.

| Model | Useful prior observations | Consequential limits |
|---|---|---|
| [Gemma E2B](../../v3/results/models/gemma-4-e2b/README.md) | Short extraction and tool tasks, with lower observed memory use | Writing accuracy varied; conflict recovery could stop after reading |
| [Gemma E4B](../../v3/results/models/gemma-4-e4b/README.md) | Short sourced drafts, extraction and tool workflows | Longer edits retained false claims after feedback |
| [Qwen3.5 4B](../../v3/results/models/qwen3.5-4b/README.md) | Short extraction, classification and tool workflows | Editing changed supplied facts and retained superseded instructions |
| [Qwen3.8 27B](../../v3/results/models/qwen3.8-27b/README.md) | Source-based document work and verified tool workflows | Strict output formats and Swedish prose need review |

The small-model observations used text-only Q4_K_M, embedded templates,
thinking off, Q8 K/V, 256-token batches and a 16,384-token window on Apple M5 /
32 GiB. They include simulated tools and AI assessment; general chat quality,
human time saved and low-memory machine fit were not established. The utility
profile exposes the same Qwen3.5 model to a harness; its standalone successes
do not prove that offloading improves a complete job.

The candidate retains those core sampling and cache settings, with explicit
Temper seed and launch controls. The
[24 September allocation](../../v3/labs/workstreams/model-runtime-optimization/method/catalog-refresh-2026-09-24.md)
checked the four then-authored compositions on b11149/v257. Its five focused
specimens preserve important limits: all four changed a supplied plural while
proofreading; both Gemmas mishandled absent evidence; E2B left conflict recovery
unfinished. These observations extend the linked cards without replacing their
broader historical assessments; they do not qualify all task behavior on the
newly recorded b11157. Small-model profiles use a 0.50
`gpu_memory_utilization` preparation policy: this is an admission assumption,
not an enforced llama.cpp memory limit or evidence of 8/16 GiB fit.

## Templates

Templates are explicit per-model choices. Changing one retains the same model
weights and creates a distinct execution identity.

| Choice | Offered for | Default |
|---|---|---|
| Embedded template | All four models | Gemma E2B/E4B and Qwen3.5 |
| [Frog v22.5](https://huggingface.co/froggeric/Qwen-Fixed-Chat-Templates/blob/855bffc49448e299789730ff92c9b8d834d6cc14/README.md) | Both Qwen models | Qwen3.8 |
| [Sharp v22.5.0](https://huggingface.co/peculiar-ragdoll/Qwen-Sharp-Chat-Templates/blob/85461fc118aaf25e7319c7ecf2481f944aac3a32/README.md) | Both Qwen models | Optional |

Sharp builds on Frog and adds an instruction favoring concise output, together
with template fixes. Choosing it changes prompt behavior and style. Its
upstream claims do not extend the retained capability results to this new
composition. The same Sharp v22.5.0 bytes passed a narrow three-question
Qwen3.8 screen under b10936 with thinking off; that historical result does not
cover today's medium-thinking/MTP configuration. See `tuning_screens[8]` in
the [retained Qwen result](../../v3/labs/workstreams/qwen-baseline-refresh/results/qwen3.8-27b.json).
Select `sharp-qwen-v22.5.0` in the Template tab, or pass
`--template qwen3.8-27b-q4xl-mtp=sharp-qwen-v22.5.0` to `temper init`.
Gemma has only its embedded choice, so its Template tab is omitted.

## Context

Both Qwen models declare a **262,144-token native ceiling** for the combined
input-plus-output window. Gemma E2B/E4B declare **131,072**. Official
[Qwen3.8](https://huggingface.co/Qwen/Qwen3.8-27B#best-practices),
[Qwen3.5](https://huggingface.co/Qwen/Qwen3.5-4B#processing-ultra-long-texts),
[Gemma E2B](https://huggingface.co/google/gemma-4-E2B-it) and
[Gemma E4B](https://huggingface.co/google/gemma-4-E4B-it) cards were checked on
23 September 2026. The Qwen roughly 1M-token extensions need RoPE scaling;
those are separate configurations.

The Context screen uses the largest applicable reviewed machine finding when
available. This candidate has no canonical findings yet: setup requests an explicit
window before continuing and shows its fit as unknown. `--context LAYOUT=TOKENS`
supplies a window up to the ceiling. The authored windows are 40,960 for Qwen3.8
and 16,384 for the small models, available to low-level compilation;
they are not automatic machine recommendations. Short capability tasks at 16k
do not establish a machine's context capacity. The retained Qwen3.8 102,400-token
observation reserves 512 output tokens; it does not establish that window with
this candidate's 4,096-token allowance.

The [small-model observations](../../v3/labs/workstreams/model-runtime-optimization/results/catalog-defaults-m5.json)
and [Qwen3.8 capacity result](../../v3/labs/workstreams/model-runtime-optimization/results/qwen27-capacity-m5.json)
retain successful lookup/continuation points for M5 / 32 GiB / Mac17,3. Engine
versions are explicit: the small-model rows have not been rerun on b11157.

| Composition | llama.cpp / router | Window | Initial input | Output allowance | Initial / follow-up service |
|---|---|---:|---:|---:|---:|
| Qwen3.5 4B | b11149 / v257 | 16,384 | 11,776 | 4,096 | 17.34 / 17.02 s |
| Gemma E2B | b11149 / v257 | 16,384 | 11,776 | 4,096 | 12.09 / 1.16 s |
| Gemma E4B | b11149 / v257 | 16,384 | 11,776 | 4,096 | 18.88 / 2.00 s |
| Qwen3.8 27B | b11157 / v257 | 40,960 | 36,352 | 4,096 | 506.28 / 21.08 s |

All remained inside the frozen resource bounds with no observed swap growth.
Gemma's strict JSON controls failed on Markdown fences; a separate prospective
semantic arm established its lookup points. Qwen3.5's follow-up reprocessed the
whole input, while the others reused a prefix; context fit alone does not
establish useful conversation latency. Times exclude native token construction
and do not represent storage-cold startup or a fixed cross-model benchmark.

The Qwen row reports the fresh confirmation; discovery also passed, taking
487.83 / 21.00 seconds. Confirmation continuation used 36,442 input tokens,
reusing 36,348 and processing 94 new tokens. A separate forced resource test
consumed 36,570 input plus the full 4,096 output tokens, ending at the length
limit in 535.64 seconds. That test establishes output-budget resource behavior,
not answer quality. Confirmation peak RSS was 20.829 GiB, with no swap growth,
memory-pressure, thermal or CPU-limit event and verified shutdown. The slow
initial request remains a practical cost. Larger stopped points do not establish
a numeric machine ceiling, and this result does not establish low-memory fit.

Larger short-answer work did succeed: 65,536 lookup and continuation passed
twice, and the 98,304 lookup passed. Their later continuation/full-output
requests, and a 49,152 full-output request, triggered the global macOS pressure
warning below the RSS limit and without swap growth. The controller stopped
them; no OOM or model crash was observed. The cause remains unresolved because
system-wide memory attribution was not collected. The authored 40,960 point
therefore remains a conservative tested choice, not a prohibition on larger
explicit windows.

A separate [checkpoint comparison](../../v3/labs/workstreams/model-runtime-optimization/results/qwen35-checkpoint-reuse-m5.json)
changed only Qwen3.5's checkpoint count from zero to one. Forward-continuation
time fell by 89% in both order blocks, from 17–18 seconds to about 1.9 seconds,
with all sixteen answers correct. The canonical count remains zero pending
representative multistep and rewind review; the table above retains the actual
default observations.

Reviewed candidate fields, including the dedicated context-execution digests,
are retained with these results. Canonical findings still require a real public
HTTP(S) evidence destination, which the V3 shadow results do not yet have.
These points therefore do not silently enable automatic setup defaults or
establish maximum context, real 8/16 GiB fit or combined-helper residency.

## Currency and tested software

Rechecked on **24 September 2026**. All four selected GGUF files still match their
assessed revisions; upstream metadata confirms the recorded sizes and hashes.
Frog v22.5 is unchanged. Sharp v22.5.0 is recorded from its canonical
repository. Exact revisions, sizes and hashes live in the catalog.

The candidate records [llama.cpp b11157](https://github.com/ggml-org/llama.cpp/releases/tag/b11157)
and [llama-swap v257](https://github.com/mostlygeek/llama-swap/releases/tag/v257).
Temper's existing resolver verified both release archives. `--software latest`
still resolves the newest releases explicitly; these recorded versions provide
reproducible offline compilation. The Qwen3.8 40,960-token result binds these
exact releases; the linked small-model/task observations retain b11149.

`--software tested` selects the following prior observed versions. Evidence is
scoped to the cited conditions; it is not a claim that every current template,
context or launch setting was tested together.

| Component / model | Minimum tested version | Retained evidence |
|---|---|---|
| llama.cpp / Gemma E2B | b10655 | [Exact capability configuration](../../v3/labs/workstreams/small-model-capability-portfolio/results/gemma-4-e2b.json), `cal07-gemma4-e2b-off:c9531ba173ac` |
| llama.cpp / Gemma E4B | b10621 | [Exact capability configuration](../../v3/labs/workstreams/small-model-capability-portfolio/results/gemma-4-e4b.json), `cal07-gemma4-e4b-q4km-off-b10621-ctx16k:b81efd11cde9` |
| llama.cpp / Qwen3.5 | b10621 | [Exact capability configuration](../../v3/labs/workstreams/small-model-capability-portfolio/results/qwen3.5-4b.json), `cal07-qwen35-4b-q4km-off-b10621-ctx16k:465b8c16fdb6` |
| llama.cpp / Qwen3.8 | b10964 | [22 September native integration check](../docs/PLAN.md#current-state), frozen `qwen-machine-study@2` lock |
| llama-swap | v255 | Same bounded native integration check: routing, one request and owned shutdown |

The b10621 observations used the Homebrew build; fallback resolves the official
archive for that upstream release, not the original Homebrew binary. All listed
fallback archives remain resolvable. Minimum **required** versions remain unknown;
the observed tested versions are not inferred compatibility floors. The separate
native allocations check only the configurations identified in their records;
optional Sharp compositions still need their own applicable checks. Source
currency checking itself performed no inference or model-weight download.

## Edit a description

The local assessment pages above are available in this shadow workspace. The
catalog's optional `assessment_url` requires HTTP(S); these V3 cards do not yet
have a published destination. Leave that field absent until a real public
assessment URL exists rather than substituting an upstream model card or a
nonexistent publication.

Each model's `description` is editable catalog text shared by its main and
utility profiles. Use Workshop's
[description editor](../../v3/workshop/README.md#edit-a-model-description), or:

```sh
./build/temper catalog describe --catalog catalog/guided-setup.json \
  --artifact qwen3.5-4b-q4km \
  --description 'Useful for my extraction tasks; I review its factual claims.' \
  --dry-run
```

Omit `--dry-run` to save the edit. `--description-file FILE` reads your wording
from a file; `--assessment-url URL` adds or replaces the optional link.
`--if-empty` offers a suggestion without replacing an existing description.
Descriptions do not change execution identity or measured context facts.
