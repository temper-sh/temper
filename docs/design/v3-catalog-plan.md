# V3 catalog reset plan

Status: **decision draft; no implementation, cutover, reset, or removal is
authorized by this document**

Consolidation note (2026-09-03): Local AI V3's
[`REQUIREMENTS.md`](../../../v3/REQUIREMENTS.md) adopts this study's
Layout/Profile flip, identity boundaries, adapter design, concrete specimens,
and Field Kit deduplication. It starts the executable catalog with five record
kinds and requires Qualification to earn a separate record through real
independent identity, revision, or reuse; Results owns public portfolio
entries. This document remains the detailed research and proof plan, not a
second V3 authority.

Implementation update (2026-09-13): the owner-authorized Qwen refresh produced
the first additive five-record local compiler/export slice, described in
[`execution-lock.md`](../contracts/execution-lock.md). The exact llama.cpp /
llama-swap closure passed isolated install, fetch, apply, replay, binding,
tokenizer and inference checks. This advances the concrete llama specimen;
signed publication, other engine closures and cutover remain outside this
proof. The seven-kind discussion below is retained design history under the
V3 consolidation note, not the implemented schema.

This plan defines the smallest catalog Temper should ship for its first clean
public V3 release. It is deliberately extracted from current Temper code, the
two current Field Kit packages, the Local AI V3 proposal, and current engine
surfaces. It replaces taxonomy-driven planning with two concrete specimens:

- the frozen Qwen3.8 27B + llama.cpp Field Kit configuration; and
- a large-memory Qwen3.8 Flash-Next + Rapid-MLX candidate which is useful but
  not yet admissible as a shipped Temper configuration.

Nothing in the existing tree is to be deleted or rewritten while this plan is
being reviewed. New schema/compiler work, if approved, lands alongside the
current implementation. Cutover and cleanup are a separate final decision.
If accepted, this plan is folded into a small architecture contract at
cutover; it is not intended to become another permanent documentation layer.

## Outcome

V3 should have one compact product graph and three representations of it:

1. the **catalog** says what Temper can offer and what is evidenced;
2. the user-owned **selection** says what the user chose; and
3. the self-contained **execution lock** says exactly what will run.

The graph has seven product record kinds:

1. Artifact
2. Patch
3. Engine
4. Layout
5. Profile
6. Qualification
7. Portfolio entry

`CatalogSnapshot` is a signed container for those records, not an eighth
product concept.

This adopts the proposed noun flip:

- the V3 proposal's **runtime profile** becomes **Layout**: one exact servable
  configuration;
- the V3 proposal's **layout** becomes **Profile**: layouts composed with
  residency and routing; and
- `engine profile` becomes simply **Engine**.

There is no initial `Mode` record. A selected profile is the world Temper
materializes. `off` is an empty desired state or a stop operation, not catalog
content.

## Why this shape is valid

The current names put the more specific concept above the more general one.
Users naturally choose a profile, while a layout describes how one model is
laid out for one engine. Flipping them makes the containment direction read
correctly:

```text
Artifact + Patch + Engine + settings
                    │
                    ▼
                  Layout
                    │
       layouts + bindings + residency + routing
                    │
                    ▼
                  Profile
```

The shape also matches the implemented renderer. Temper already accepts a
semantic launch request, selects exactly one typed engine-tuning variant, and
dispatches to a private command builder. The useful abstraction is real; the
catalog should name its inputs rather than store command-line strings.

The design borrows three ideas from Rapid-MLX's current atomic model catalog:

- immutable identity is separate from mutable presentation;
- recommendation is policy over exact identities rather than a property of
  model bytes; and
- a snapshot has one content identity.

Temper should not copy Rapid-MLX's compatibility envelope, parallel legacy
projections, shadow states, or per-surface rollout machinery. Temper has no
installed V3 base to preserve.

## Minimum record model

| Record | Owns | Does not own |
|---|---|---|
| **Artifact** | Upstream coordinate, immutable loader-consumed files, format, sizes, hashes, tokenizer/config components, and license/distribution facts | Engine settings, machine recommendation, public ranking |
| **Patch** | Independently versioned output-affecting material, immutable bytes or deterministic transform result, compatibility scope, and license facts | A duplicate of the base artifact |
| **Engine** | Engine identity, supported target, complete executable or Python closure, adapter contract, interfaces, modalities, and observable runtime contract | Model recommendation, raw user flags, ambient dependencies |
| **Layout** | One artifact, zero or more patches, one engine, technical interface, modalities, semantic inference policy, and exactly one typed engine configuration | Co-residency, routing, tools, public prose |
| **Profile** | One or more layout bindings, foreground/default route, residency timing, coexistence/resource policy, and optional service/tool bindings | Artifact bytes, repeated engine tuning, evidence prose |
| **Qualification** | One scoped claim bound to an exact subject digest, machine scope or witness, evidence references, caveats, and invalidation conditions | Product lifecycle ceremony, copied raw evidence |
| **Portfolio entry** | Human-facing use, choice reason, tradeoffs, alternatives, applicability summary, and qualification references | Execution identity or install authority |

The first catalog does **not** make these product records:

- workflow, protocol, suite, measurement, or experiment;
- machine bucket;
- promotion or promotion receipt;
- engine profile or runtime profile;
- mode; or
- tool, until the first real supported tool forces a reusable record.

Workflows, protocols, suites, measurements, and experiments stay with Labs or
Field Kit. Machine facts are inputs to a qualification. A reusable machine
scope can become its own record later if several real qualifications make the
duplication costly. Promotion is publication of a reviewed catalog snapshot,
not a stored domain object.

The initial exact llama-swap supply is catalog runtime metadata and is folded
into every affected profile lock. It does not justify a generalized
router/supervisor registry. Promote it to a record kind only when a second real
implementation creates a selection problem.

## One home for each fact

| Fact | Authoritative home |
|---|---|
| Discoverable supported option | Temper catalog |
| Exact source, files, hashes, license | Artifact or Patch record |
| Exact engine binary/interpreter/dependencies | Engine record |
| One-server semantic and engine settings | Layout record |
| Composition, residency, routing, service bindings | Profile record |
| User intent and explicit tool/integration consent | User selection |
| Fully resolved bytes and launch graph | Execution lock |
| What was installed | Installation receipt/state |
| What a process actually loaded | Engine-specific runtime observation |
| Raw experiment method and evidence | Labs or Field Kit |
| Reviewed support claim | Qualification |
| Public explanation and alternatives | Portfolio entry / Results projection |

Generated projections may repeat a fact for transport, but they never become
another writer. The compiler must be able to prove where every repeated value
came from.

## Identity and digest boundaries

One digest cannot answer both “will inference behave the same?” and “is this
the same distributable record?”. The large Rapid specimen exposes the
difference: a repository revision can add or change license/documentation
while every loader-consumed model component remains byte-identical.

V3 therefore uses explicit digest layers:

- **record digest** — all canonical record content, including provenance,
  license, distribution metadata, and runtime components;
- **material digest** — only the ordered immutable files consumed by the
  model loader, plus any selected patch result;
- **engine closure digest** — target, executable/interpreter, every required
  dependency artifact, adapter contract version, and immutable runtime
  controls;
- **layout execution digest** — material digest, engine closure digest,
  interface, modalities, normalized semantic policy, and typed engine
  settings; and
- **profile execution digest** — the ordered layout bindings plus residency,
  routing, and coexistence policy, including the exact router/supervisor
  closure used to enact them.

Claims bind the narrowest digest that proves their scope. Behavioral and fit
claims normally bind a layout or profile execution digest. License and
distribution claims bind the artifact record digest. A license-only change
therefore requires license review and a new record digest, but does not
pretend that model inference changed or automatically require another heavy
model run.

Digest input is canonical JSON emitted from typed data; authoring YAML bytes
are never hashed directly. Sets are sorted, units are named integers, and
fractional tuning uses fixed-point integers rather than floating-point digest
input. Local paths, ports, timestamps, display text, and catalog ordering do
not enter execution digests.

Stable semantic IDs identify a line of product intent. Exact references use
both ID and digest. An explicit update may move an ID to newer content; an
existing execution lock remains pinned.

## Engine adapter boundary

The current adapter pattern should be retained and hardened, not replaced by a
plugin framework:

```text
typed Layout
    │
    ▼
semantic LaunchRequest + one closed engine-config variant
    │
    ▼
engine adapter: validate semantic support and normalize
    │
    ▼
engine command builder: executable + argv + environment
    │
    ▼
safe supervisor serialization / direct process execution
```

Each engine also owns read-side helpers for readiness and observed loaded
identity. Those reads remain separate from the pure command builder. The
process orchestrator owns start, stop, timeout, and cleanup effects.

Rules:

- no raw flags, arbitrary environment maps, or shell fragments in catalog
  records;
- exactly one typed engine configuration per layout;
- semantic differences are refused rather than approximated;
- every common field has one fixed meaning: an engine must enforce it, prove
  that the immutable artifact/engine fixes the same value, or refuse it;
  merely advertising a value to a client is not enforcement;
- every default which could change output, memory, network access, or
  scheduling is made explicit or verified from the running engine;
- an Engine identifies a complete target-specific closure, not a package name;
- readiness does not prove loaded-artifact identity; both are checked; and
- the closed Go switch remains appropriate while the supported engine family
  is small and shipped with Temper.

An adapter version is part of engine closure identity. Changing only the
serialization while preserving the normalized launch plan can use focused
contract evidence; changing semantics produces a new layout execution digest.

### Current engine findings

| Engine | Useful implementation now | Required before support claim |
|---|---|---|
| **llama-server** | Typed builder emits loopback/offline launch, Q8 KV, batching, flash attention, MTP, `--ctx-checkpoints`, `--cache-ram`, and `--reasoning` | Keep real-binary help/parser smoke and loaded-model observation in release gate |
| **Rapid-MLX** | Typed builder covers batching, memory fraction, prefix cache, cache size, KV dtype, PFlash, multimodality, thinking/parser, and speculation | Resolve and lock the full Python closure; disable or explicitly select automatic behavior; verify effective live config and model identity |
| **MLX-VLM** | Typed safe subset covers text/vision, batching, KV quantization, vision cache, and `max_kv_size`; unsupported speculation is refused | Lock full Python closure; keep architectural context separate from KV capacity; add a separately locked drafter before speculation; verify modality and loaded model |
| **vLLM-Metal** | Typed builder covers target-specific vLLM/Metal settings and refuses a known unsafe MTP + prefix-cache combination | Lock the paired plugin/vLLM/MLX closure; add the currently required `--no-async-scheduling` for speculative decode; verify the real parser and observed model identity |

Generic vLLM is a future target-specific Engine, not an alias for vLLM-Metal.
The schema must permit it without putting CUDA/Linux assumptions into Layout,
but no generic-vLLM implementation is part of the first Apple-Silicon slice.

## Concrete specimen: frozen Field Kit configuration

The current package provides exact enough input to force the model. This is a
schema specimen, **not a claim that the 102,400-token profile is qualified**.

```yaml
schema: temper-catalog/v1

runtime:
  router:
    id: llama-swap
    version: v252
    revision: e31a1adee494bb7a578e2a97ec891b3e809899dc
    target: darwin-arm64
    locator: https://github.com/mostlygeek/llama-swap/releases/download/v252/llama-swap_252_darwin_arm64.tar.gz
    bytes: 13009416
    sha256: 2a35ddc40c965bfa758101323c698fdc2f49bc6f6393eae0e09b2940352bc670

artifacts:
  qwen3.8-27b-dynamic-q4xl:
    source: hf://unsloth/Qwen3.8-27B-GGUF
    revision: 4ca720788d1e01f1bff70c033e0d0028fd02e502
    format: gguf
    files:
      - path: Qwen3.8-27B-UD-Q4_K_XL.gguf
        bytes: 17559178144
        sha256: 3f227079003add2511437e5b1e94812e363385225bf6a9b47b0054a72bc8b01e
    license: Apache-2.0

patches:
  froggeric-qwen38-template:
    source: hf://froggeric/Qwen-Fixed-Chat-Templates
    revision: 756cfb69d742355fd310b4ba9d50815a27d9d241
    files:
      - path: chat_template.jinja
        bytes: 27167
        sha256: c47c82b0544752d454f4e427228d9d9d8c3df64c9e446cbd0229362f67948009
    compatible_artifacts: [qwen3.8-27b-dynamic-q4xl]
    license: Apache-2.0

engines:
  llama-server-b10621-darwin-arm64:
    family: llama-server
    target: darwin-arm64
    version: b10621
    revision: c1d0e7a004015f23bc0233470b747b596f29b264
    adapter: llama-server/v1
    supply:
      locator: https://github.com/ggml-org/llama.cpp/releases/download/b10621/llama-b10621-bin-macos-arm64.tar.gz
      bytes: 10954823
      sha256: 429c8270608600188035e5e92f7d78dffb7900904fe7dd7e6a84f48068cd13cf
    interfaces: [chat-completions]
    modalities: [text]

layouts:
  qwen3.8-27b-q4xl-100k-llama:
    artifact: qwen3.8-27b-dynamic-q4xl
    patches: [froggeric-qwen38-template]
    engine: llama-server-b10621-darwin-arm64
    interface: chat-completions
    modalities: [text]
    context_window_tokens: 102400
    request_defaults:
      max_output_tokens: 4096
      reasoning: off
    speculation:
      method: mtp
      source: embedded
      max_draft_tokens: 3
    engine_config:
      kind: llama-server/v1
      parallel: 1
      kv_cache_key: q8_0
      kv_cache_value: q8_0
      flash_attention: on
      batch_tokens: 512
      microbatch_tokens: 512
      context_checkpoints: 16
      prompt_cache_ram_mib: 0

profiles:
  qwen3.8-27b-q4xl-local:
    bindings:
      - id: primary
        layout: qwen3.8-27b-q4xl-100k-llama
        route: default
        residency: resident
        idle_ttl_seconds: 1800
```

The compiler derives all digests; authors do not type them. A Qualification
and public Portfolio entry are added only after the evidence supports their
exact claims. `coding` is a qualification/use, not a model role.

The exact command produced by this specimen must contain:

```text
--ctx-checkpoints 16
--cache-ram 0
--reasoning off
```

Those values are not cosmetic. Current llama.cpp defaults are 32 context
checkpoints and an 8192 MiB RAM cache; zero explicitly disables that cache.

## Concrete specimen: large-memory Rapid candidate

`rapid-mlx/Qwen3.8-Flash-Next-4bit` is the second schema test because it
exposes problems the GGUF specimen cannot:

- it is a directory of config, tokenizer, and 28 weight shards rather than one
  model file;
- measured and current repository revisions have identical loader-consumed
  shards but differ in small metadata/license material;
- its engine is a Python application whose exact identity includes managed
  CPython and every resolved wheel; and
- automatic MTP/PFlash/reasoning behavior can change between engine builds
  even when the user-facing command looks similar.

The measured artifact revision is
`dcf657e4acda2aae72da99cde65b6c491cd96998`; its current upstream head during
this review is `5c91f5a0f8f5ccdbe71900a52d020f284e60d91a`. The measured runtime used
Rapid commit `615a8c5cd17b40db8d49e17d93c96f9094f23221`, identified upstream as a
0.13.4 candidate, while the current public release is 0.13.3.

The candidate may be authored as an Artifact specimen with distinct record
and material digests. It must **fail catalog admission** today because the
available evidence used an unreleased Rapid build and there is not yet a
complete target-specific engine closure tied to that run. A PyPI pin such as
`rapid-mlx==0.13.3` is insufficient: its required MLX, Transformers, serving,
and utility dependencies are version ranges rather than an immutable closure.

The admission error should be direct:

```text
Rapid layout cannot be published: engine closure is incomplete and the
measured engine revision does not match a releasable locked closure
```

Once the closure exists, its typed layout must state all automatic behavior
explicitly. A machine recommendation remains absent until an applicable
larger-memory witness qualifies it. Existing Rapid recommendations are useful
research input, not Temper evidence.

## Selection and execution lock

The selection is small, semantic, and user-owned:

```yaml
schema: temper-selection/v1
profile: qwen3.8-27b-q4xl-local
tools: []
integrations: []
```

Choosing a profile never implies consent to an optional tool or harness
integration. Those remain explicit selections. Temper writes this file once
when absent and later offers advisory diffs only.

Resolution produces a self-contained lock:

```text
temper-selection/v1 + signed temper-catalog/v1 snapshot + target facts
                               │
                               ▼
                    temper-execution-lock/v1
```

The execution lock contains the selected records by ID and digest, expanded
immutable material lists, exact engine/router supply closures, normalized
launch inputs, target constraints, and the resulting profile execution
digest. It needs no catalog lookup to install, verify, render, or run later.
Local installation paths are derived state and never part of portable lock
identity.

## Field Kit deduplication

The current Field Kit has three avoidable compilation layers:

1. its launcher repeats portable-Python constants already present in
   `runtime/python-macos-arm64.json` and synthesizes a Temper software lock;
2. each package repeats software facts in `software.json`, `package.json`, and
   another synthesized software lock; and
3. each package enumerates the same install/fetch/apply/check/bind stages.

The V3 package should instead contain:

```text
package.json                 question, decision, scope, cost, consent,
                             applicability, protocol/controller references
execution.lock.json          exact self-contained Temper graph
protocol/controller content  Field Kit-owned experiment behavior
```

Field Kit's runtime distribution should likewise ship one static exact Python
software lock and ask Temper to install it. The future target schema separates
portable artifact compatibility (`darwin/arm64`, optional minimum OS) from the
observed host OS version, so Field Kit does not synthesize a lock merely to
insert `sw_vers` output.

Generic orchestration is runtime policy: install, fetch, render, verify, bind,
run, and cleanup are derived from the execution lock and package kind. They do
not appear in every package. A release tool computes one package-directory
digest for the catalog index; `package.json` does not manually enumerate and
hash every sibling file.

For a bounded-adaptive question, Field Kit owns the finite parameter lattice
and search policy. Each attempted point compiles to an exact derived Layout
and execution digest. Temper's catalog does not store a parameterized “layout
template” or the experiment workflow.

This is a future cross-repository change. No Field Kit file changes under this
plan without explicit authorization for that step.

## Catalog authorship and publication

There should be one canonical shipped catalog source, in Temper. Workshop may
assemble and review a coherent proposed change, but it must not retain a
second mirrored registry or promotion database.

Start with one human-authored source file. Split it only when actual size or
independent ownership makes a split cheaper than the joins it creates.

The path is:

1. Labs or Field Kit explores an exact candidate directly from a private or
   package lock. Catalog registration is not required to experiment.
2. Workshop assembles the useful evidence references, scoped claims, exact
   candidate records, and public explanation as one change set.
3. One human review decides whether the meaning and effects are acceptable.
4. Accepted canonical source lands in Temper once.
5. CI parses it through typed schemas, resolves references, computes digests,
   validates admission, and emits canonical catalog JSON plus public
   projections.
6. Release signing publishes one immutable snapshot and a small signed channel
   pointer with a monotonic sequence.
7. `temper catalog update` explicitly verifies signature, digest, schema,
   sequence, rollback/equivocation rules, and atomically activates the
   snapshot. There is no updater daemon.
8. A catalog update never rewrites a user's selection or execution lock.
   `temper update` shows an exact old-to-new lock diff and changes it only
   through an explicit user operation.

Git/release history and immutable snapshot digests retain old state. V3 does
not add qualifying/active/suspended lifecycle rows, per-record approval
packets, or embedded promotion receipts.

## Existing Temper disposition

This is a review ledger, not a deletion list.

### Keep and extract

- the pure `internal/render/engine` boundary, typed variants, private builders,
  safe command-word serialization, and refusal behavior;
- artifact pin/fetch/verify and content-addressed set admission;
- canonical machine facts and pure wall-model arithmetic;
- upstream-release and uv installation mechanics, including portable Python;
- staged immutable rendering and one atomic activation point;
- receipt-bound Field Kit bind/serve/tokenize primitives;
- scoped receipts, attributable cleanup, dry-run purity, and clean reruns;
- signed catalog transport, rollback/equivocation checks, and atomic snapshot
  activation; and
- deterministic release packaging and hermetic CI gates.

### Replace behind a new surface

- `temper-manifest/v1` and `/v2` with `temper-selection/v1` plus
  `temper-execution-lock/v1`;
- separate software and qualification catalog models with the one V3 catalog;
- caller-compiled Field Kit software locks with shipped execution locks;
- broad root-wide desired-state machinery with receipts that describe only
  observed installed identity and the minimum safe ownership/lease facts;
- mode-oriented CLI language with profile selection and explicit start/stop;
  and
- generic readiness handling with engine-specific readiness and loaded-model
  observations.

### Do not carry into the clean V3 surface unless evidence creates a need

- six speculative qualification-profile kinds and their large promotion
  envelope;
- `Mode`, including a catalog `off` mode;
- qualifying/active/suspended promotion lifecycle and copied receipts;
- duplicated revision/supersedes fields when Git history and content digests
  already answer the question;
- manually repeated check records, stage lists, primitive lists, report fields,
  and invalidation boilerplate;
- Homebrew supply as a public V3 requirement unless an admitted engine or tool
  actually requires it; and
- currently uncalled bootstrap/policy islands unless the new vertical slice
  gives them one clear owner and caller.

No item in this section is removed before the final cutover gate.

## Ceremony budget

V3 keeps only controls which protect a concrete risk:

- exact hashes, complete closures, and signature verification;
- deterministic typed validation;
- explicit consent before network, storage, paid, private-data, or live-system
  effects;
- scoped evidence for every claim;
- stage, validate, then commit once;
- dry-run purity, clean reruns, attributable cleanup, and rollback; and
- one coherent semantic review per published change set.

It drops ceremony which only proves that paperwork exists:

- separate promotion decisions for the same reviewed change;
- review receipts embedded in consumer records;
- a lifecycle state machine for pre-public candidates;
- repeated evidence because a repository boundary was crossed; and
- schemas which enumerate runtime choreography already fixed by code.

After cutover, the documentation budget is a public README, one architecture
and data-model guide, one contributor/release/roadmap guide, and executable
contracts beside the code they constrain. This decision draft replaces old
planning layers; it does not join them indefinitely.

## Delivery plan and gates

Only Gate 1 and Gate 4 are owner decisions. Gates 2 and 3 are engineering
proof, not separate promotion ceremonies. All work remains additive through
Gate 3.

### Gate 1 — owner contract decision

Approve or revise:

1. the Layout/Profile flip and seven record kinds;
2. the record/material/engine/layout/profile digest boundaries;
3. Temper as the sole canonical shipped catalog source, with Workshop as a
   review workbench rather than a mirror; and
4. the adapter rule that a common semantic value must be enforced or proved,
   never merely advertised; and
5. additive prototyping before any reset or removal.

**Exit evidence:** this document records the accepted choices and unresolved
questions. No code is changed.

### Gate 2 — additive contract proof

Create strict typed schemas for catalog, selection, and execution lock. Encode
the complete llama specimen and the intentionally inadmissible Rapid specimen.
Compile the llama catalog record + selection into one self-contained execution
lock and feed it through the existing fetch/install/render adapter path without
changing the live service.

**Exit evidence:** parser/refusal tests, canonical-byte goldens, reference and
digest tests, and a compiler error proving the incomplete Rapid closure cannot
publish; the exact llama command contains the three Field Kit flags; dry-run
makes no changes; first and second isolated runs are identical; and an
interrupted pre-commit leaves no active generation.

### Gate 3 — additive product proof

Complete the experimental engine supply and prove the reduced Field Kit path:

- Rapid-MLX, MLX-VLM, and vLLM-Metal receive exact target-specific Python
  closures, corrected launch behavior, and engine-specific loaded-identity
  reads;
- in a separately authorized Field Kit change, one parallel V3 fixture uses a
  static execution lock and one parallel runtime path uses a static Python
  lock; and
- the new path is exercised as an isolated release candidate without becoming
  the live default.

Leave every current package and synthesis path intact through this gate. Real
model runs are announced separately because they are heavy and may require a
larger-memory device.

**Exit evidence:** hermetic closure/adapter tests; locked-binary argument
acceptance; authorized loopback start/readiness/loaded-artifact/interface/stop
smokes; observed offline/no-telemetry behavior; no duplicate exact facts or
generic stage list in the V3 Field Kit path; verify/install/resume/cleanup
coverage; signed update and rollback coverage; reproducible release artifacts;
the full Go checks; and no public claim broader than the runtime matrix.

### Gate 4 — explicit cutover and cleanup decision

Present the exact old files/packages/commands proposed for retirement, the
replacement evidence, and rollback point. Reset or delete only after the owner
explicitly approves this gate.

**Exit evidence:** new release path is canonical, old code is unreachable,
the removal diff contains only approved targets, and the final clean-tree
tests pass.

## Verification matrix

| Risk | Required proof |
|---|---|
| Schema accepts ambiguity | Strict unknown-field, tagged-union, reference, unit, and boundary refusals |
| Digest changes for wrong reason | Canonical goldens and paired tests for runtime-byte versus license/prose-only changes |
| Engine flag drift | Current binary `--help`/parser smoke plus exact argv/env tests |
| Ambient Python changes runtime | Complete interpreter + wheel closure, offline installation, receipt comparison |
| Server is ready but wrong model loaded | Engine-specific normalized identity observation checked against lock |
| Automatic engine behavior changes | Explicit typed controls plus observed effective configuration where available |
| Partial mutation | Staging, validation, one commit, interruption injection, and previous-state preservation |
| Repeated run drifts | Second-run-clean and immutable-generation checks |
| Dry run mutates | Before/after filesystem and process observation |
| Catalog rollback/equivocation | Signed channel sequence and immutable snapshot tests |
| Catalog silently changes user intent | Selection and old lock byte equality after catalog update |
| Field Kit duplicates facts | Static-lock fixture and source-of-truth search/assertion |
| Claim outruns evidence | Qualification admission requires exact subject, applicable witness, scoped claim, and evidence reference |

## Research references checked 2026-09-02

- llama.cpp server options: <https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md>
- Rapid-MLX current release and CLI: <https://github.com/raullenchai/Rapid-MLX/releases/latest>, <https://github.com/raullenchai/Rapid-MLX/blob/main/docs/reference/cli.md>
- Rapid-MLX atomic catalog decision: <https://github.com/raullenchai/Rapid-MLX/blob/main/docs/engineering/decisions/2026-08-31-atomic-product-model-catalog.md>
- Rapid-MLX large-model evidence: <https://github.com/raullenchai/Rapid-MLX/blob/main/docs/benchmarks/recent-large-models-m3-ultra.md>
- MLX-VLM: <https://github.com/Blaizzy/mlx-vlm>
- vLLM-Metal configuration: <https://docs.vllm.ai/projects/vllm-metal/en/latest/configuration/>

These sources harden the adapter and identity design. They are not substitutes
for runtime qualification on Temper's exact locked closures.
