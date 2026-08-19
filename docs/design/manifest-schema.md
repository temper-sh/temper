# `manifest.yaml` — C2 direction sketch (parked for the M4 design pass)

Parked 2026-08-17 after owner review of the mode-first sketch. This is a
**direction, not a schema**: the manifest `apply` consumes at M1 stays
legacy-shaped (`models.yaml`), the wizard writes the base at M3, and the
`modes:` extension gets its real data-modeling pass at M4 — starting from
this sketch instead of from memory.

## Settled directions

- **Entries say what a thing is; modes say what is live.** Identity and
  mode-invariant tuning on the entry; residency, placement and per-mode
  tuning in the mode binding. Composition is visible at the composer
  (same inversion as the lock's profiles-as-catalogue).
- **Resolution lives in the lock.** `repo`/sources here are intent;
  revisions and file hashes are `manifest.lock.yaml` rows.
- **One home per primitive.** `window`/`max_tokens` live once on the model
  entry; engine KV settings and harness derivations (Pi compaction,
  legacy FINDINGS #25) are computed from them, never stored beside them.
  Legacy stores the window twice (`launch.max_kv_size`,
  `pi.contextWindow`) and warns on drift; this removes the drift class.
- **Roles are the join surface.** Tools and harnesses name roles
  (`rerank`, `coder`); each mode binds them or is visibly invalid —
  never a silent substitution.
- **`off` is a mode** — start/stop are mode transitions, not special
  cases.
- **Tools carry sources** (owner, 2026-08-17): tool entries reference
  real repositories — GitHub or elsewhere, not exclusively ours. Their
  pins become lock rows when the lock grows its tool section (M2).
- **Patches carry sources** (owner, same day): HF or GitHub, replacing
  the legacy `patches/*/FETCH` indirection.
- **Applicability is catalog knowledge, not manifest structure** (owner,
  2026-08-19): whether a dependency is worth having can depend on the
  machine's resource constraints — the live case is Pi extensions
  (`compaction-guard`, `context-trim`) that matter beside a 16k window
  and are noise beside a frontier one. The condition lives on the catalog
  profile (M2 envelope) and the wizard clips offers by it; the manifest
  never encodes conditions, it records what was chosen for *this*
  machine.

## Deliberately unsettled

- **Per-mode `tuning:` shape** — the owner is not sold on the block as
  sketched; it evolves at the M4 pass together with the modes schema.
- **Source spelling for patches and tools** — scheme-prefixed strings vs
  structured fields, decided when the lock learns to resolve them (M2).
  Treat the sketch's spelling as placeholder.

## The sketch

Drawn from the live legacy entries so it can be compared line-by-line
with today's `models.yaml`.

```yaml
schema: temper-manifest/v1

defaults:
  ttl: 1800
  gpu_memory_utilization: 0.85

# ---- entries: what a thing IS --------------------------------------------
# Identity + tuning that is true in every mode. Resolution (revision, file
# hashes) lives in manifest.lock.yaml — sources here are intent, not pins.

models:
  - id: qwen3.8-27b-mlx
    display_name: "Qwen3.8 27B (local, native MTP)"
    engine: mlx-vlm
    runtime: mlx-vlm-0.6.13-mlx-0.32.0-apc-mtp-q8fix1
    repo: mlx-community/Qwen3.8-27B-4bit
    draft: {repo: mlx-community/Qwen3.8-27B-MTP-4bit, kind: mtp, block_size: 3}
    patches:
      - name: mlx-vlm-0.6.13-qwen35-apc-mtp-q8
        source: <hf or github ref>    # spelling TBD; replaces patches/*/FETCH
    window: 16384          # ONE home for the window: engine max_kv_size AND
    max_tokens: 4096       # Pi contextWindow/compaction all derive from these
    launch:                # typed engine tuning, as today — minus
      kv_bits: 8           # max_kv_size/max_tokens, which are now derived
      apc_exact_cache_entries: 1
      # ...
    safety: {physical_stop_gib: 22}   # ...
    pi: {reasoning: false, input: [text]}   # minus contextWindow/maxTokens

  - id: rerank-qwen3-0.6b
    engine: llama-server
    kind: rerank
    repo: "Voodisss/Qwen3-Reranker-0.6B-GGUF-llama_cpp:Q8_0"
    flags: --ctx-size 4096 --parallel 1 --batch-size 256 --ubatch-size 256
    # no -ngl, no group, no ttl — placement is a layout fact, not an
    # identity fact; it lives in the mode bindings below

tools:
  - id: project-search
    source: <github ref>   # tools have sources too — not exclusively ours;
                           # pin → lock when the tool section lands (M2)
    needs: [rerank]        # a role, not an entry id

harnesses:
  - id: pi
    derives: [compaction]  # reserve = max_tokens + window/8
                           # keep = (window − reserve)/2
                           # re-materialized on every apply / mode switch

# ---- modes: what is LIVE -------------------------------------------------
# The whole layout readable in one place: members by role, residency,
# per-mode tuning. Roles are the join — harnesses and tools speak roles,
# the mode decides the binding.

modes:
  coding:
    default: true
    models:
      coder:
        use: qwen3.8-27b-mlx
        residency: {group: pinned, preload: true, ttl: 7200}
      rerank:
        use: rerank-qwen3-0.6b
        residency: {group: heavy, ttl: 120}
        tuning: {ngl: 0}   # CPU — the coder owns the GPU (FINDINGS #16);
                           # tuning shape unsettled, evolves at M4
    tools: [project-search]
    harnesses: [pi]        # compaction derives from coder's 16k window

  research:
    models:
      rerank:
        use: rerank-qwen3-0.6b
        residency: {group: utilities, ttl: 1800}
        tuning: {ngl: 99}  # GPU placement — the coder is unloaded here
    tools: [project-search]
    harnesses: [pi]        # no local chat member → compaction re-derives
                           # frontier-sized, not inherited from coding

  off: {}                  # a mode like any other: the empty layout
```
