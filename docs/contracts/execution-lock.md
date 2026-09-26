# Catalog and execution-lock preparation

Temper compiles a verified active catalog or explicit local catalog and selection into a
self-contained execution lock. The [direct execution runtime](execution-runtime.md)
consumes that lock for installation, rendering and serving. Compatibility exports
remain available for issued clients. Version selection is a preparation
step; installation consumes exact inputs and never silently updates them.

```sh
temper catalog compile --catalog catalog.json --selection selection.json \
  --target darwin/arm64 --software recorded|latest|tested \
  --out execution.lock.json [--dry-run] [--json]
temper execution inspect --lock execution.lock.json
temper execution prepare --lock execution.lock.json \
  --root /explicit/temper-root --installation candidate [--dry-run]
```

Use `--root ROOT` instead of `--catalog FILE` to compile from the verified active
publication. Exactly one source is required. The [catalog guide](../CATALOG.md)
describes download, inspection and explicit selection. Published compilation
binds the lock's source identity to the exact signed snapshot bytes, including
when latest/tested resolution changes the software inputs. The existing local
authoring path retains its canonical document identity and issued-lock behavior.

Choose one software value. `recorded` is the default and uses retained exact
inputs without network access. `latest` discovers the newest downloadable
llama.cpp numbered build (published upstream as a nightly), and the upstream
stable releases for llama-swap and Splash. `tested` explicitly selects the recorded minimum
tested version; it refuses unknown or conflicting tested boundaries.
All choices must satisfy required versions. A failed lookup never triggers an
automatic fallback. A newer resolved lock needs a new output path.

Latest/tested discovery supports the current GitHub release archives for
llama.cpp, llama-swap and Splash on macOS ARM64. It resolves the release tag to a commit,
selects the target archive, verifies its upstream SHA-256 and size, and computes
the bounded archive inventory without writing files. llama.cpp discovery scans
at most 100 recent releases, skipping drafts and builds missing the target
archive. Once selected, an archive's integrity failure refuses rather than
trying an older build. Only official llama.cpp `b<number>` builds may carry
GitHub's prerelease flag; other sources require stable releases.

Numbered `b...` and `v...` tags are compared numerically within their own family.
Semantic `MAJOR.MINOR.PATCH` and `vMAJOR.MINOR.PATCH` tags use semantic version ordering. Ordering between
families is never inferred. Unknown tag formats or missing integrity metadata
refuse. See upstream's [versioning explanation](https://github.com/ggml-org/ggml/discussions/1579)
and [release API](https://docs.github.com/en/rest/releases/releases).

The output parent must already exist. Compilation preserves the selection and
refuses to replace a different lock. Export verifies existing derived files,
writes only absent ones, and refuses changed files, symlinks and unrelated
directory contents. Repeating the same resolution writes nothing. Each file
publishes atomically without replacement; a partial export can be rerun to fill
missing files. Consumers proceed only after success. Dry runs write nothing,
install nothing and launch no processes; moving resolution still reads network
metadata and archive bytes.

## Owned records and scope

`temper-catalog/v2` has Artifact, Patch, Engine, Layout and Profile records.
Optional presentation metadata keeps the chooser's Model / Weights / Engine
labels distinct: Artifact owns `model_name` and `weights_name`; Engine owns
`display_name`. A Layout's `memory_tier` is an estimated machine-capacity group
(`XS`, `S`, `M`, `L`, `XL`, or `XXL`), not model file size or an admission rule.
Missing labels fall back to existing record names; missing tiers stay
unspecified. The optional catalog-level `layout_order` lists layout IDs in
editorial order. IDs must exist and appear at most once; unlisted choices follow
in stable profile-ID order within their tier. Setup groups available tiers from
largest to smallest before applying that order. These fields change catalog
source identity, but neither execution identity nor context evidence identity.

The `splash/v1` engine variant uses `kv_cache` (`int8` or `bf16`),
`max_memory_bytes`, `reasoning_effort` and `request_timeout_seconds`; llama.cpp
fields cannot appear in that variant. A Splash Layout selects one target GGUF
and `speculation: {method: dflash2, source: artifact, max_draft_tokens: 0,
draft_artifact: ID}`. The draft Artifact pins its own repository, revision,
`config.json` and `model.safetensors`. Splash's release-owned architecture mapping
validates that this is the proper DFlash2 sidecar. It is included automatically
with the Layout, without another user choice. The engine owns draft block size.

The selected closure includes target, draft, template and software independently.
Changing draft bytes invalidates the composition and artifact-set identity without
changing target identity. Draftless issued locks retain their original digests.
Compatibility exports represent the sidecar as `layout.draft` and `entry.draft`;
Splash preparation is performed by `execution prepare`.

Software supplies contain `package`, `target`, a GitHub `source` and optionally
a retained exact `release`. The source names its repository, asset template and
archive root. `{version}` expands to the tag and `{number}` to its numeric part.
The resolved release records version, commit and archive identity. Omit it to
require latest/tested resolution before compilation. Installer selections,
adapter IDs and unit maps are generated at export, never authored in supplies.

A Layout may declare `engine_versions.minimum_required` with `required_source`
and `minimum_tested` with `tested_evidence`. These apply to that Layout and its
engine target. Router facts use `runtime.router.versions`. Required is a hard
support floor; tested is optional evidence under the cited conditions. Testing
a later version does not make it the required floor. Unknown boundaries remain
absent, and a tested release below a newly required floor cannot be selected as
a fallback. Version metadata does not change execution identity.

`temper-selection/v2` contains `schema`, `profile`, and optional `templates`
and `context_windows` maps.
The `templates` map keys selected layout IDs to a compatible patch ID; an empty
value explicitly selects the model's embedded template. An omitted map or key
inherits that layout's catalog default (`patches[0]`, or embedded when there is
no patch). `temper catalog select` freezes every selected layout's current
choice into the new user-owned selection; repeat `--template layout=patch` or
`--template layout=builtin` to override individual choices. The compiler accepts
older selections with omitted choices without rewriting them. Unknown or
unselected layout keys and missing or incompatible patches are refused before
publication or runtime effects. Each profile binding uses its layout as
identity. The compiler rejects empty tools/integrations placeholders and
separate binding IDs in v2 rather than maintaining unused extension slots.

`context_windows` maps selected layout IDs to total input-plus-output token
windows. Low-level selection with an omitted value uses the layout's authored
`context_window_tokens`. Optional `context_limit_tokens` separately records the
model/configuration ceiling; without it, the authored window remains the limit
for compatibility. An explicit choice must exceed
`request_defaults.max_output_tokens` and cannot exceed that limit.
`catalog select --context LAYOUT=TOKENS` and guided setup freeze each chosen number.
Compilation records it in the selected
layout and renders that exact window. Context changes alter execution identity
while reusing the same model bytes. Old selections and frozen execution locks
retain their original windows. A context limit states capacity, not measured
memory fit or task quality; extended RoPE scaling requires its own configuration.

Layouts may carry reviewed `context_findings`. Each gives `window_tokens`,
`max_output_tokens`, a tested `execution_sha256`, a `machine` selector
(`target`, `chip`, exact `physical_memory_bytes`, optional `hardware_model`,
and `minimum_wired_limit_mib`), `engine_memory_limit_bytes`,
`swap_growth_limit_bytes`, an `evidence` URL and optional `latency_note`.
The execution digest binds the actual window, output allowance, model bytes,
template, engine, router and all launch/request controls. `catalog compile
--json` reports `context_execution_digests` per layout for these exact inputs;
this identifies a candidate and does not certify that a test ran. Changed inputs leave a finding
inapplicable rather than silently updating its evidence identity. Findings and
the ceiling are catalog metadata, excluded from execution identity.

Guided setup resolves software before selecting the largest matching tested
window. Its automatic choice requires applicable evidence; otherwise it reports
unknown and requests an explicit window. Explicit values remain possible up to
the model ceiling and disclose whether that exact point has matching evidence.
There is no interpolation from a larger machine or between tested points.
Latency notes do not reduce the capacity default. This first automatic path
covers single-layout profiles; multi-layout composition needs its own fit
evidence and explicit windows. Preview performs no model run or weight download.

Artifacts may carry `description` and an optional `assessment_url`. Workshop
edits these through `temper catalog describe`; one artifact's description is
shared by its local-main and utility choices. Descriptions may contain the
owner's personal assessment and need no Results record. They are display
metadata, excluded from execution identity, and software/evidence refreshes
preserve them. `catalog describe --if-empty` supplies a suggestion only when no
description exists; replacement is an explicit edit.

A profile with omitted `foreground` has one local default route. A profile with
`foreground: external` has no default route and at least one available helper
binding. The derived v2 manifest uses `external_foreground: true` with no
`foreground` layout to tell a selected harness that its provider-owned model
does the foreground work. A v2 `foreground: external` remains an ordinary local
layout reference when a layout has that ID. Temper still
renders the explicitly selected local helpers by layout ID, without a generic
local router group. Pi's existing default model and compaction settings remain
provider-owned in this mode. The empty `none` mode
remains a separate manifest state. A compact chat model can be a local default
when that profile selects it; artifact size is not a role classifier.
With no resident local model, the resident wall-model check reports
`not-applicable`; it does not establish that loading an on-demand helper will
fit alongside a harness-owned foreground. Current catalog locks also do not
select or install a Pi integration; Pi settings preservation applies when an
explicit integration supplies Pi's base configuration to the renderer.

The lock retains only selected layouts and their material/engine records. It
keeps the source snapshot hash and one profile execution digest; intermediate
record/material/engine/layout hash maps are not serialized. Consumed model,
selected template, engine or settings changes alter execution identity. Template
overrides retain the original catalog snapshot identity and do not duplicate
model weights. License, display
name, source-discovery instructions and evidence metadata do not. Export also
identifies the exact lock bytes by SHA-256. Local machine paths never enter the
portable lock.

The executable slice accepts one complete GGUF per artifact, at most one
external template, text chat through `llama-server/v2`, complete release
archives for llama.cpp and llama-swap, and none or embedded-MTP speculation.
The [Qwen specimen](../../catalog/qwen38-m5-refresh.json) and
[selection](../../catalog/qwen38-m5-refresh.selection.json) exercise this path.
Their retained versions are reproducible inputs, not asserted minimum tested
boundaries. Availability and measured capability remain Results assessments.

## Issued Field Kit inputs

Already-issued v1 catalogs, selections and execution locks remain readable and
compilable with their original identities and four export files. V1 does not
accept moving software resolution. Its legacy fields exist only for this real
consumer; new authoring uses v2. Frozen Field Kit packages and their producing
runtime are unchanged. Direct execution-lock consumption and removal of the
compatibility bridge belong to the separately recorded Field Kit second wave.

## Export contract

`--json` emits `temper-execution-inputs/v1` with:

- `profile`, `execution_digest`, and exact input `lock_sha256`;
- selected `layouts`, for the caller's existing per-layout fetch sequence;
- `changed` and `dry_run` booleans;
- `inputs`, mapping the four filenames below to `path` and byte `sha256`.

`manifest.yaml`, `manifest.lock.yaml`, `software.lock.yaml` and
`request-defaults.json` are derived compatibility inputs, not new authoring
surfaces. Field Kit supplies the shipped execution lock and receives these
inputs from Temper; it does not implement the compiler in Python. Field Kit
continues to own consent, orchestration, protocol execution and cleanup.

The v2 software projection records execution-lock provenance, without claiming
to be an experiment. It omits the older software lock's resolution date rather
than copying the catalog's authoring date into an observation. Installation
receipts retain their actual observation time. The software projection declares
`target_mode: compatible` and portable
`darwin/arm64`. Existing software locks that omit `target_mode` retain their
exact-host matching behavior. Compatible locks do not insert the consuming
Mac's observed OS version into lock identity. The current compatibility mode
is limited to macOS on ARM64; actual hardware fit and observed OS/build remain
machine evidence. Older Temper binaries reject the new field before effects.

`llama-server/v2` emits the refreshed `--load-mode` API and explicit cache,
reasoning-preservation, context-shift, fitting, thread and sampling controls.
Existing manifest layouts without those optional controls retain their argv.
Sampling values are server defaults that explicit API requests may override.
For layouts using these refreshed controls, the declared output budget also
becomes the `--predict` server default. Explicit API output budgets take
precedence. `request-defaults.json` retains the same intended budget for callers.

The optional `engine_config.controls.checkpoint_min_step` maps to
`--checkpoint-min-step`. Omission retains the exact engine's default; zero
explicitly removes the minimum spacing. Negative values are rejected before
runtime effects. Changing this control changes layout and profile execution
identity without changing model or template material identity.

The typed `engine_config.kv_cache` accepts `q4`, `q8` and `f16`, rendered
for both K and V as `q4_0`, `q8_0` and `f16` respectively. Q4 is available
for explicitly selected experiments; parser/rendering support does not
establish model correctness or machine fit.
