# Catalog and execution locks

Temper compiles one explicitly selected `temper-catalog/v3` preset into a
self-contained `temper-execution-lock/v3`. Installation, rendering and serving
consume that exact lock through the [execution runtime](execution-runtime.md).
User compositions live in [`temper-configuration/v1`](layouts.md).

```sh
temper catalog compile --catalog catalog/guided-setup.json \
  --preset qwen3.8-27b-q4xl-mtp --target darwin/arm64 \
  --software recorded --out execution.lock.json [--dry-run] [--json]
temper execution inspect --lock execution.lock.json
temper execution configure --lock execution.lock.json \
  --preset qwen3.8-27b-q4xl-mtp --context 32768 --max-output 4096 \
  --out execution.32k.lock.json [--dry-run]
temper execution prepare --lock execution.32k.lock.json \
  --root /explicit/temper-root --installation candidate [--dry-run]
```

Exactly one catalog source is required: `--catalog FILE` for local authoring or
`--root ROOT` for a verified active publication. Published compilation retains
the exact signed snapshot digest even when software resolution changes the
selected material. Local authoring uses the canonical source document digest.

The output parent must exist. Compilation and configuration publish atomically,
leave identical files untouched, and refuse different content or symlinks.
Changing settings requires a new output path. Dry runs create no files.

## Owned records and identity

The catalog owns Artifact, Patch, Engine and Preset records:

- Artifacts pin model or draft repository, revision, format and exact files.
  `model_name` and `weights_name` provide separate display labels.
- Patches pin template files and name compatible artifacts.
- Engines declare the typed runtime adapter, supported interfaces/modalities,
  software supply and display label.
- Presets select those records and own request defaults, engine settings,
  speculation, context limits/findings, memory tier and editorial copy.
  Recommended presets require an authored description. `preset_order` controls
  presentation; it never selects a preset for the user.

A lock contains `schema`, `source_snapshot_sha256`, `preset`, portable `target`,
selected `records`, and one `execution_digest`. It retains only that preset,
its target/draft/template, engine, router and auxiliary Python environments.
No Selection, profile, catalog layout or intermediate digest map is serialized.
Validation recompiles the retained records and refuses altered identity,
unselected records and noncanonical content. Runtime consumption is offline
and needs no source catalog, clock or machine-specific path.

Execution identity includes consumed material, exact software, request defaults
and launch settings. Editorial copy, discovery instructions, license and
required/tested evidence metadata do not change it. Model and software material
can therefore be reused across settings changes. Context evidence keeps its
independently versioned hash definition; retiring a storage format does not
rewrite existing measurements.

## Software resolution

`--software recorded` uses retained exact inputs offline. `latest` resolves the
newest downloadable official llama.cpp numbered build and stable GitHub releases
for llama-swap and Splash. `tested` selects a recorded minimum tested version
when evidence exists. Every choice must satisfy required versions. A failed
lookup or integrity check never falls back silently.

GitHub discovery resolves a tag to a commit, verifies the archive's upstream
SHA-256 and size, and computes a bounded inventory without writing files.
llama.cpp discovery scans at most 100 recent releases; drafts and builds without
the target archive are skipped. Integrity failure after selecting an archive
refuses the operation. Numbered and semantic tags are ordered only within their
own families. Recorded Python environments carry exact interpreter, wheel and
source dependency closures; they do not resolve from the installed environment.

Supplies own source/release facts or an exact Python environment. Installer
selections and unit maps are derived internally. Software locks record execution
provenance and compatible `darwin/arm64` targeting; they do not invent an
experiment or use the catalog date as an installation observation.

## Settings and evidence

The authored `context_window_tokens` counts input plus output. An explicit window
must exceed `request_defaults.max_output_tokens` and remain within
`context_limit_tokens`, or the authored window if no separate ceiling is known.
Setup accepts the authored default and reports whether exact applicable evidence
exists. It never infers fit from a larger machine or a neighboring tested point.

`execution configure` changes only context, output allowance, and optional
Splash `max_memory_bytes`. It preserves software, weights, templates, sampling,
speculation and source snapshot identity. A nonzero memory override is refused
for other engines. Setup's `--context PRESET=TOKENS` and
`--template PRESET=PATCH|builtin` produce customized exact presets.

A `context_finding` binds window, output allowance and `execution_sha256` to an
exact target, chip, RAM amount, optional hardware model and minimum wired-memory
budget. It records engine-memory/swap bounds, evidence URL and optional latency
note. `catalog compile --json` reports `context_execution_digests` for candidate
inputs; this identifies a configuration and does not claim a test occurred.
Changed software, material or execution settings invalidate the match.

Preset `description` and optional `assessment_url` are edited with
`catalog describe --preset ID`. `--if-empty` preserves existing authored copy.

## Engine contracts

llama.cpp accepts complete GGUF material, text chat and at most one external
template. Speculation may be disabled, embedded MTP, or an exact GGUF draft for
MTP/DFlash. External DFlash uses 1–15 draft tokens; MTP uses 1–16. Target and draft
material remain independent identities. These controls do not establish that an
arbitrary pairing is correct or faster.

`llama-server/v2` exposes explicit cache, reasoning, context shift, fitting,
thread, load-mode and sampling controls. `checkpoint_min_step` maps to
`--checkpoint-min-step`: omitted uses the engine default, zero removes the
minimum, and negative values refuse. KV `q4`, `q8`, and `f16` map to `q4_0`,
`q8_0`, and `f16` for both K and V. Output allowance supplies `--predict` with
these controls; explicit API request values can override server defaults.

Splash's distinct typed configuration owns KV precision, memory cap, reasoning
and request timeout. It selects a target GGUF and an exact DFlash2 sidecar
(`config.json` plus `model.safetensors`). The release-owned architecture mapping
checks their compatibility. Preparation includes the sidecar automatically.

The [guided catalog](../../catalog/guided-setup.json) and
[experimental Qwen catalog](../../catalog/experiments/qwen-study.json) exercise
these contracts. Results and the cited studies own capability claims.

## Retired formats

Catalog v1/v2, execution-lock v1/v2, Selection files, software-supply catalogs,
and manifest v1 are rejected. There is no importer or compatibility export.
Recompile current presets and recreate saved configurations. Frozen historical
experiments require their pinned older Temper host; their evidence is unchanged.
The current low-level manifest is v2, and internal projections continue to feed
Temper's exact installation and rendering primitives.
