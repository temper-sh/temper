# `manifest.yaml` — C2 schema (`temper-manifest/v1`)

Parked as a direction on 2026-08-17, reworked on 2026-08-19 to settle the
layout/mode split, and promoted to the executable v1 contract later that day
by owner decision: native `apply` consumes this schema directly. The legacy
`models.yaml` shape is not a Temper compatibility surface and the Bash
renderer remains only a legacy comparison reference.

The first renderer slice supports llama-server layouts in a local-foreground
mode and the empty `off` mode. A selected engine or harness the renderer does
not implement is a refusal, never a partially generated world. The schema can
grow only through a reviewed revision; unsupported future shapes are not
silently accepted by v1.

## The two nouns

- **Layout** = `(model, engine, tuning)`. A servable configuration. It knows
  nothing about residency, tools or harnesses — only "this model, run this
  way". Layouts are what the wizard's model step offers and what the
  download bill is computed from.
- **Mode** = the world. Which layouts are served and resident, which tools are
  exposed, which harnesses are wired, what the harness settings derive to.
  A mode switch **rebuilds the world**.

Everything below follows from keeping those two apart.

## Settled directions

- **Layouts say what a thing is; modes say what is live.** Identity and
  mode-invariant tuning on the layout; residency and placement in the mode's
  member list. Composition is visible at the composer (same inversion as the
  lock's profiles-as-catalogue).

- **The layout/member line is "what it produces" vs "where it lives"**
  (owner, 2026-08-19). Anything that changes the model's *output* is a
  layout property: weights, engine, patches, window, KV precision, MTP,
  thinking mode. Anything that changes *where it runs and for how long* is a
  member property: placement group, ttl, startup preload, `ngl`, and harness
  preference. Preference and preload are separate choices.

  The test case that fixes the line: the 0.6B reranker runs CPU-only beside a
  resident coder and may use the GPU when none is (legacy FINDINGS #16). That
  is one layout placed two ways — `ngl` is offload, not identity — so it needs
  no second layout and no per-mode `tuning:` block. Conversely `thinking: off`
  *is* identity: it changes the rendered prompt, and flipping it mid-session
  re-renders history so nothing is a token prefix of what came before, costing
  a full re-prefill (legacy FINDINGS #27's template mechanism, deliberately).
  This retires the "per-mode `tuning:` shape" question that was parked here.

- **A mode switch is a full world rebuild with a total plan — there is no
  differ** (owner, 2026-08-19). The plan is a pure function of
  `(manifest, mode, machine)`: it never reads the running world to decide what
  to do, so it cannot be wrong about state it did not observe. Same inputs,
  same plan — second-run-clean applied to mode switching. Reconciliation is
  llama-swap's existing job: writing a config in which a model no longer
  appears *is* the unload instruction. A delta engine here would be a second
  source of truth for residency.

- **Placement groups are named for behaviour, not for the router.**
  `resident:` / `on_demand:` rather than llama-swap's `pinned`/`heavy`, which
  are router trivia in a user's file and would not survive a router change.

- **Residents are a list, always.** One resident coder is a 32 GiB fact, not a
  schema law; on a large machine a mode lists several and they coexist.
  Whether a member set fits is arithmetic against the machine bucket, checked
  at plan time. The schema has no opinion.

- **Resolution lives in the lock.** `model`/sources here are intent;
  revisions and file hashes are `manifest.lock.yaml` rows.

- **One home per primitive.** `window`/`max_tokens` live once on the layout;
  engine KV settings and harness derivations are computed from them, never
  stored beside them. Legacy stores the window twice
  (`launch.max_kv_size`, `pi.contextWindow`) and warns on drift; this removes
  the drift class.

- **Harness settings are derived from the foreground layouts, never stored.**
  Pi's auto-compaction is the witnessed case: frontier-sized defaults zero the
  threshold against a small local window and strand sessions (legacy FINDINGS
  #25). See "Compaction resolution" below for how one global client slot is
  reconciled with several resident layouts.

- **Roles are the join surface.** Tools and harnesses name roles (`rerank`,
  `coder`); each mode binds them or is visibly invalid — never a silent
  substitution. A role-model arrives because a selected tool needs it; the
  user consents to the tool, not to its `ngl` flag.

- **`off` is a mode** — start/stop are mode transitions, not special cases.

- **Tools carry sources** (owner, 2026-08-17): tool entries reference real
  repositories — GitHub or elsewhere, not exclusively ours. Their pins become
  lock rows when the lock grows its tool section (M2).

- **Patches carry sources** (owner, same day): HF or GitHub, replacing the
  legacy `patches/*/FETCH` indirection.

- **Applicability is catalog knowledge, not manifest structure** (owner,
  2026-08-19): whether a dependency is worth having can depend on the
  machine's resource constraints — the live case is Pi extensions
  (`compaction-guard`, `context-trim`) that matter beside a 16k window and are
  noise beside a frontier one. The condition lives on the catalog profile (M2
  envelope) and the wizard clips offers by it; the manifest never encodes
  conditions, it records what was chosen for *this* machine.

- **The catalog is this shape plus metadata** (owner, 2026-08-19). A knowledge
  base of machine profiles stores the same layout structure annotated with
  evidence, measurements, caveats and status; the manifest is the subset a
  user selected, with the prose stripped. This narrows M2's open "how does a
  reviewed packet map into the catalog" question: the unit is the layout, and
  projection is annotation-removal rather than translation. It also means a
  machine profile is diffable against a manifest — "what this box was measured
  with" versus "what this box is running".

- **Recommendation metadata never enters the manifest** (owner, 2026-08-20).
  The catalog may annotate several qualified layouts as recommended for the
  same applicability envelope and attach their evidence-backed performance
  profiles. Projecting any of them into `manifest.yaml` strips recommendation
  prose and measurements; only the layouts the user explicitly selected
  remain, and `preferred` still comes solely from the user's mode choice.

## Compaction resolution — one client slot, several layouts

Each local foreground layout derives its own pair from its own window
(`reserve = window/8`, `keep = (window − reserve)/3` — legacy's current
formulas; the older `reserve = max_tokens + window/8` and `keep = …/2` are
superseded, the `max_tokens` term by the KV budget clamp and the half by a
witnessed post-compaction floor sitting *above* the trigger).

How those reach the client depends on what the client can express:

- **If Pi ships per-model compaction** — upstream
  `earendil-works/pi#8133`, opened 2026-08-14, auto-closed by their
  new-contributor bot and **reopened**, which in that repo is the maintainer
  signal that it passed the quality bar. No label, assignee, milestone or
  reply as of 2026-08-19, so this is worth planning *toward*, not *on*. The
  renderer emits one profile per resident layout keyed by model id, with the
  global pair as the fallback for anything unlisted. No election.
- **Otherwise, the renderer elects** the narrowest resident window, because
  one global setting has to be survivable by the smallest window in the mode.
  Legacy implements exactly this today.
- **A local Pi patch is outside Temper's ownership boundary** (owner,
  2026-08-20). Pi is a user-managed harness, and Temper does not choose its
  Node runtime, package manager, or installation layout. Until upstream offers
  per-model compaction, the renderer uses the global-slot election above;
  Temper does not patch or replace the user's Pi executable.

**The manifest is byte-identical in either supported case.** The compromise is a
rendering concern, a property of the target client's capability, not a fact
about the model — which is why it must not appear in the schema.

## Deferred beyond v1

- Utility modes whose foreground model belongs to a harness. Their renderer
  must preserve provider-owned client settings rather than deriving local
  compaction into a global slot.
- MLX and other engines. Each needs an owned launcher and a typed tuning block;
  selecting one before that implementation exists is a refusal.
- Tool resolution. Tool `source` remains intent until the lock grows its M2
  tool section. Patch resolution is implemented in M1.

## Complete v1 example

Drawn from the live legacy entries so it can be compared line-by-line with
today's `models.yaml`.

```yaml
schema: temper-manifest/v1

defaults:
  ttl: 1800
  gpu_memory_utilization: 0.85

# ---- layouts: (model, engine, tuning) ------------------------------------
# What a servable thing IS. Resolution (revision, file hashes) lives in
# manifest.lock.yaml — sources here are intent, not pins.

patches:
  qwen38-sharp-template:
    source: hf://peculiar-ragdoll/Qwen-Sharp-Chat-Templates@3dc34df52c63dd22ada21f96435e069deaa8d7da/chat_template.jinja?transform=qwen38-prefix-stability-v1
    file: chat_template.jinja

layouts:
  qwen38-llamacpp-24k:
    display_name: "Qwen3.8 27B GGUF 24k"
    model:
      repo: unsloth/Qwen3.8-27B-GGUF
      file: Qwen3.8-27B-Q4_K_M.gguf
    engine: llama-server
    role: coder
    window: 24576
    max_tokens: 4096
    kv: q8
    thinking: off
    chat_template: qwen38-sharp-template
    llama:
      parallel: 1
      flash_attention: on
      batch: 512
      ubatch: 512

  rerank-0.6b:
    display_name: "Qwen3 reranker 0.6B"
    model:
      repo: Voodisss/Qwen3-Reranker-0.6B-GGUF-llama_cpp
      file: Qwen3-Reranker-0.6B-Q8_0.gguf
    engine: llama-server
    role: rerank
    window: 4096
    llama:
      parallel: 1
      flash_attention: auto
      batch: 256
      ubatch: 256

tools:
  project-search:
    source: github://temper-sh/project-search
    needs: [rerank]

# ---- modes: the world ----------------------------------------------------
# Readable in one place: members by placement, tools, harnesses. Everything
# else is derived — compaction from the resident windows and groups from the
# placement; reranker presence comes from the tools that need its role.

modes:
  local:
    foreground: local
    tools: [project-search]
    harnesses: [pi]
    members:
      resident:
        - {layout: qwen38-llamacpp-24k, ttl: 7200, preferred: true}
      on_demand:
        - {layout: rerank-0.6b, ttl: 120, ngl: 0}   # a coder is resident

  off:
    foreground: none
```

## Required fields and invariants

- IDs are lowercase stable names whose components may be separated by `-` or
  `.`, and share their kind's namespace. Layout, patch, tool and mode maps sort
  by ID when order is not semantically owned by a member list.
- `model.repo` is the source intent and `model.file` is the selected artifact;
  the lock snapshots both resolution and hashes. Paths are relative and may
  not contain `..`.
- A v1 patch source is
  `hf://owner/repo@<40-character-commit>/<path>[?transform=<id>]`. The commit
  pins the input bytes; `resolve` hashes the final transformed output. v1's
  built-in transform is `qwen38-prefix-stability-v1`, and unknown transforms
  refuse.
- `window`, `max_tokens`, `kv`, `thinking` and the `llama:` block are layout
  identity because they change output or the serving contract.
  `llama.flash_attention` is the explicit enum `on`, `off` or `auto`; `auto`
  preserves llama.cpp's hardware-dependent default without pretending it is
  disabled. `ttl`, `ngl`, `preferred` and `preload` are member facts because
  they change placement or use.
- `defaults.gpu_memory_utilization` is greater than zero and at most one. It is
  the user's conservative allocation policy for the preferred GPU-resident
  coder in the M1 wall-model prediction; it is not rendered as a llama-server
  flag or claimed as a measured runtime footprint. See
  [`wall-model.md`](wall-model.md). `defaults.ttl` is zero or greater.
- Every mode member references one selected layout and appears in exactly one
  placement list. `preferred: true` is allowed at most once and only on a
  resident coder in a local-foreground mode; it selects Pi's starting model.
  `preload: true` is separate and allowed only on a resident member; it asks
  llama-swap to start that model eagerly. Preference never implies preload.
- Every selected tool exists in `tools`; each role in its `needs` list is
  furnished by a member of that mode. A missing role is a refusal.
- Every selected harness must be implemented by the renderer. v1 implements
  Pi only; it never ignores an unknown harness.
- `foreground: local` requires a resident coder. `foreground: none` permits no
  members, tools or harnesses. Harness-owned foreground is reserved for the
  utility-mode schema revision that can preserve provider-owned settings
  correctly.
- Compaction is derived for the local foreground from the narrowest resident
  coder window: `reserve = window/8`, `keep = (window-reserve)/3`. Neither
  derived value appears in the manifest.

## What `apply` renders

```
temper apply --manifest manifest.yaml --mode local --root <temper-root>

  PLAN — derived from (manifest, mode, machine); nothing read from the running world
    resident        qwen38-llamacpp-24k (preferred)
    on demand       rerank-0.6b, CPU
    pi compaction   reserve 3,072; keep 7,168
    tools           project-search → pi
    artifacts       llama-swap/config.yaml, pi/models.json,
                    pi/settings.json

  → stage an immutable generation
  → atomically move the `rendered/current` pointer (one commit)
```

`apply` stops there: it does not install into consumer homes, download
artifacts, or kick a service. `temper fetch <layout>` materializes one atomic
set under `<temper-root>/artifacts/layouts/<layout>/<entry-digest>/`; later
start/mode verbs may invoke that same effect before activation without
changing this manifest or growing a second downloader.
