# Curated presets for guided setup

[`guided-setup.json`](guided-setup.json) is the macOS ARM64 authoring
catalog, using `temper-catalog/v3`. A preset fixes model weights, engine,
templates and inference settings. Your named layouts compose selected presets;
the catalog does not decide their membership, startup loading or default.

Recommended contains exactly these five presets, in this S-tier order:

| Preset ID | Useful role and tradeoff |
|---|---|
| `qwen3.8-27b-q4xl-splash` | Coding with Splash/DFlash2; platform and cache-conversion requirements apply |
| `muse-glimmer-30b-q4xl-llama` | Technical drafts, planning, orchestration and a second review; verify operational claims |
| `gemma-4-26b-a4b-qat-ud-q4-k-xl-llama` | Creative drafting and chat with shorter waits; check facts and narrative continuity |
| `gemma-4-31b-qat-ud-q4-k-xl-llama` | Creative drafting and revision with longer waits; no established human preference advantage over 26B |
| `qwen3.8-27b-q4xl-mtp` | Qwen's llama.cpp baseline for coding and document work; strict formats still need review |

All includes those five plus `gemma-4-e2b-q4km-off`,
`gemma-4-e4b-q4km-off`, and `qwen3.5-4b-q4km-off`. A smaller preset can be your
main model or a helper. Recommendation membership is an editorial choice;
compatibility and local speed observations can support an All entry without
making it Recommended. Intended roles do not claim completed-work evidence
for every application.

```sh
./build/temper init --catalog catalog/guided-setup.json --dry-run
```

Nothing starts selected. Recommended and All share one selection. Presets have
independent context/template settings and reusable material. The next screen
composes arbitrary layouts; Local and Utility are removable empty starters.
[The setup guide](../docs/contracts/init.md) covers editing, preparation and
managed activation. Save/prepare does not activate a layout.

Current presets default to **medium thinking**, and supporting clients can
override it per request. A different default needs a test establishing why it
is preferable; historical runs with thinking off do not alone justify that
exception. Wizard cards omit thinking settings. Existing saved locks and the
measurements below keep their exact original settings. The smaller presets'
IDs retain their historical `-off` suffix so saved references remain valid;
the request defaults, not those IDs, control thinking.

The chooser places available **S (>16–32 GB)** choices before **XS (up to
16 GB)**, with incompatible choices afterwards. Tiers are estimates, not machine
qualification. Edit `preset_order` to change order within a bucket. Model and
weight labels belong to artifacts, engine labels to engines, and memory tiers
and descriptions to presets. Editorial changes preserve execution identity and
saved selections.

The Splash preset uses Splash 1.1.0, the exact Unsloth target, Frog template and
matching DFlash2 draft. It requires Apple M3 or newer and macOS 26.4 or later.
Preparation derives its tokenizer without loading weights; first start builds
an additional runtime weight cache. Its int8 KV, medium reasoning, 24 GiB memory
cap and authored 118,000-token window remain explicit settings, not a new 1.1.0
capacity measurement. The prior [integration check](../docs/PLAN.md#splash-110-integration)
used 1.1.0/v257 at 32k with short chat/tool requests; it does not qualify v260
or the larger authored window.

Gemma 31B's weights and launch configuration come from the retained
[`gemma31-ud-mtp0-final` composition](../../v3/labs/workstreams/writing-and-orchestration/results/qat-m5-2026-09-28.json):
Unsloth QAT UD-Q4_K_XL, llama.cpp b11205, plain decoding, Q8 KV, two checkpoints
and a 40,960-token window. That measurement used thinking off; the current
medium default is a different execution configuration and inherits no exact
context or performance witness from it.

For scripted choices:

```sh
./build/temper init --catalog catalog/guided-setup.json \
  --preset qwen3.8-27b-q4xl-mtp --preset gemma-4-e2b-q4km-off \
  --context qwen3.8-27b-q4xl-mtp=40960 --dry-run --json
```

Signed sequence 3 publishes this catalog for alpha.11. Historical signed
snapshots retain their issued schemas and 32k configuration for pinned clients.

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
| [Muse Glimmer 30B](../../v3/results/models/muse-glimmer-30b/README.md) | Technical explanation, structured drafts, critical review and a completed conflict-recovery workflow | Migration SQL, atomicity and rollout guarantees needed material corrections; Q5 and high reasoning did not repair them |
| [Gemma 4 26B-A4B](../../v3/results/models/gemma-4-26b-a4b/README.md) | Creative scenes, dialogue revision and general explanation with short waits | Length and factual constraints still need review; strict operational guarantees were unsound |
| [Gemma 4 31B](../../v3/results/models/gemma-4-31b/README.md) | Creative drafting, revision and explanation | Longer waits; continuity edits and independent checking of operational advice remain necessary |

The [writing and orchestration comparison](../../v3/labs/workstreams/writing-and-orchestration/README.md)
measured Glimmer's Q4 XL target with medium reasoning and plain decoding, plus
Unsloth's Gemma 26B QAT UD-Q4_K_XL with thinking off and plain decoding. Both
use llama.cpp b11205, F16 KV and two bounded context checkpoints. Glimmer's
draft variants did not establish a useful local speed gain. Gemma's MTP
candidates were faster but did not clear the writing screen; plain decoding
also retains a deadline-coherence failure in the final scene. The QAT
conversion comparison found modest speed gains and mixed writing outcomes,
not a demonstrated literary-quality improvement. Gemma 31B is another recommended S preset with
[its own assessment](../../v3/results/models/gemma-4-31b/README.md),
longer waits and no demonstrated human preference advantage. These are text-only observations on M5/32 GiB,
not vision, multilingual or autonomous-operation qualifications.

The small-model observations used text-only Q4_K_M, embedded templates,
thinking off, Q8 K/V, 256-token batches and a 16,384-token window on Apple M5 /
32 GiB. They include simulated tools and AI assessment; general chat quality,
human time saved and low-memory machine fit were not established. A helper layout can
expose the same Qwen3.5 preset to a harness; its standalone successes
do not prove that offloading improves a complete job.

The candidate retains those core sampling and cache settings, with explicit
Temper seed and launch controls. The
[24 September allocation](../../v3/labs/workstreams/model-runtime-optimization/method/catalog-refresh-2026-09-24.md)
checked the four then-authored compositions on b11149/v257. Its five focused
specimens preserve important limits: all four changed a supplied plural while
proofreading; both Gemmas mishandled absent evidence; E2B left conflict recovery
unfinished. These observations extend the linked cards without replacing their
broader historical assessments; they do not qualify all task behavior on the
newly recorded b11157. The historical small-model profiles used a 0.50
`gpu_memory_utilization` admission assumption. New preset selection does not
reserve that profile-wide fraction: layouts budget their actual active set.
Neither policy establishes 8/16 GiB fit.

## Templates

Templates are explicit per-model choices. Changing one retains the same model
weights and creates a distinct execution identity.

| Choice | Offered for | Default |
|---|---|---|
| Embedded template | All catalog models | Gemma E2B/E4B, Gemma 26B/31B and Qwen3.5 |
| Meta official template | Muse Glimmer 30B | Glimmer |
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

Both Qwen models and Gemma 26B declare a **262,144-token native ceiling** for the
combined input-plus-output window. Gemma E2B/E4B and Glimmer declare **131,072**. Official
[Qwen3.8](https://huggingface.co/Qwen/Qwen3.8-27B#best-practices),
[Qwen3.5](https://huggingface.co/Qwen/Qwen3.5-4B#processing-ultra-long-texts),
[Gemma E2B](https://huggingface.co/google/gemma-4-E2B-it) and
[Gemma E4B](https://huggingface.co/google/gemma-4-E4B-it) cards were checked on
23 September 2026. The Qwen roughly 1M-token extensions need RoPE scaling;
those are separate configurations.

The [Glimmer](https://huggingface.co/meta-models/Muse-Glimmer-30B) and
[Gemma 26B](https://huggingface.co/google/gemma-4-26B-A4B-it) ceilings were checked
on 27 September. A native ceiling does not establish fit or acceptable latency.

Selecting a preset accepts its authored window. `c` in the editor or
`--context PRESET=TOKENS` changes it explicitly. Review reports unknown fit
unless an exact machine finding applies. This authoring candidate has no
canonical findings yet because the retained shadow evidence has no published
HTTP(S) destination. Its windows are 118,000 for Qwen–Splash, 40,960 for
Qwen–llama and Gemma 31B, 57,344 for Glimmer, 98,304 for Gemma 26B, and 16,384
for the smaller models. These are settings, not automatic machine capacity
recommendations. Historical measurements below keep their producing engines,
windows and output reserves; shorter tasks do not prove a larger context fits.

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
| Muse Glimmer 30B, Q4 XL / medium / F16 / no draft | b11205 / v260 | 57,344 | 49,152 | 4,096 reserved | 582.26 / 7.36 s |
| Gemma 4 26B-A4B, Unsloth QAT UD-Q4_K_XL / off / F16 / no draft | b11205 / v260 | 98,304 | 90,112 | 4,096 reserved | 554.25 / 4.96 s |

The [initial writing comparison](../../v3/labs/workstreams/writing-and-orchestration/results/m5-2026-09-27.json)
retains Glimmer and the Google Gemma findings; the
[QAT comparison](../../v3/labs/workstreams/writing-and-orchestration/results/qat-m5-2026-09-28.json)
owns the Unsloth Gemma result. Peak engine RSS was 16.07 GiB for Glimmer and
16.53 GiB for this Gemma 26B layout. These are the largest completed
retrieval/continuation points in their respective studies under a ten-minute
request limit, with a reserved rather than fully generated 4,096-token answer. Glimmer's
replies were bare JSON; Gemma's correct values were enclosed in a JSON fence.
Neither result qualifies arbitrary long-document reasoning. Smaller passed
points retain their exact materials in the linked records; the older Google
Gemma points are not measurements of the Unsloth target.

To preview both writing choices before composing a layout:

```sh
./build/temper init --catalog catalog/guided-setup.json \
  --preset muse-glimmer-30b-q4xl-llama \
  --preset gemma-4-26b-a4b-qat-ud-q4-k-xl-llama \
  --context muse-glimmer-30b-q4xl-llama=57344 \
  --context gemma-4-26b-a4b-qat-ud-q4-k-xl-llama=98304 --dry-run
```

The listed filled-context points had no observed native swap growth.
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

The writing comparison checked and froze
[llama.cpp b11205](https://github.com/ggml-org/llama.cpp/releases/tag/b11205) and
[llama-swap v260](https://github.com/mostlygeek/llama-swap/releases/tag/v260) on
**27 September 2026**, including exact release archive identities. Glimmer and
Gemma 26B use b11205; earlier GGUF choices retain their b11157 engine record.
The shared recorded router advances to v260. The Qwen3.8 40,960-token result
still binds b11157/v257, and small-model task/context observations retain b11149.
`--software latest` resolves newer releases explicitly; recorded software keeps
offline compilation reproducible without broadening old measurements.

`--software tested` selects the following prior observed versions. Evidence is
scoped to the cited conditions; it is not a claim that every current template,
context or launch setting was tested together.

| Component / model | Minimum tested version | Retained evidence |
|---|---|---|
| llama.cpp / Gemma E2B | b10655 | [Exact capability configuration](../../v3/labs/workstreams/small-model-capability-portfolio/results/gemma-4-e2b.json), `cal07-gemma4-e2b-off:c9531ba173ac` |
| llama.cpp / Gemma E4B | b10621 | [Exact capability configuration](../../v3/labs/workstreams/small-model-capability-portfolio/results/gemma-4-e4b.json), `cal07-gemma4-e4b-q4km-off-b10621-ctx16k:b81efd11cde9` |
| llama.cpp / Qwen3.5 | b10621 | [Exact capability configuration](../../v3/labs/workstreams/small-model-capability-portfolio/results/qwen3.5-4b.json), `cal07-qwen35-4b-q4km-off-b10621-ctx16k:465b8c16fdb6` |
| llama.cpp / Qwen3.8 | b10964 | [22 September native integration check](../docs/PLAN.md#current-state), frozen `qwen-machine-study@2` lock |
| llama.cpp / Glimmer and Gemma 26B | b11205 | [Writing comparison](../../v3/labs/workstreams/writing-and-orchestration/README.md): exact draft/configuration comparisons, role work and context |
| llama-swap | v260 | Same writing comparison: chat and native-completion routes, template/tokenizer passthrough, streaming and owned shutdown |

The b10621 observations used the Homebrew build; fallback resolves the official
archive for that upstream release, not the original Homebrew binary. All listed
fallback archives remain resolvable. Minimum **required** versions remain unknown;
the observed tested versions are not inferred compatibility floors. The separate
native allocations check only the configurations identified in their records;
optional Sharp compositions still need their own applicable checks. Source
currency checks are distinct from the separately authorized native comparisons.

## Edit a description

The local assessment pages above are available in this shadow workspace. The
catalog's optional `assessment_url` requires HTTP(S); these V3 cards do not yet
have a published destination. Leave that field absent until a real public
assessment URL exists rather than substituting an upstream model card or a
nonexistent publication.

Each preset owns its manually authored `description`. Recommended entries
require nonblank copy; All-only entries may omit it. Different engines using
the same weights can explain different tradeoffs. Edit the preset directly:

```sh
./build/temper catalog describe --catalog catalog/guided-setup.json \
  --preset qwen3.5-4b-q4km-off \
  --description 'Useful for my extraction tasks; I review its factual claims.' \
  --dry-run
```

Omit `--dry-run` to save the edit. `--description-file FILE` reads your wording
from a file; `--assessment-url URL` adds or replaces the optional link.
`--if-empty` offers a suggestion without replacing an existing description.
Descriptions do not change execution identity or measured context facts.
