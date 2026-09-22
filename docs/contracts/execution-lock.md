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
inputs without network access. `latest` discovers the upstream stable release
for each selected software source. `tested` explicitly selects the recorded
minimum tested version; it refuses unknown or conflicting tested boundaries.
All choices must satisfy required versions. A failed lookup never triggers an
automatic fallback. A newer resolved lock needs a new output path.

Latest/tested discovery supports the current GitHub release archives for
llama.cpp and llama-swap on macOS ARM64. It resolves the release tag to a commit,
selects the target archive, verifies its upstream SHA-256 and size, and computes
the bounded archive inventory without writing files. Numbered `b...` and `v...`
tags are compared numerically within their own family. Unknown tag formats or
missing integrity metadata refuse. See the upstream
[release API](https://docs.github.com/en/rest/releases/releases#get-the-latest-release).

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

`temper-selection/v2` contains only `schema` and `profile`. Each profile binding
uses its layout as identity. The compiler rejects empty tools/integrations
placeholders and separate binding IDs in v2 rather than maintaining unused
extension slots.

The lock retains only selected layouts and their material/engine records. It
keeps the source snapshot hash and one profile execution digest; intermediate
record/material/engine/layout hash maps are not serialized. Consumed model,
template, engine or settings changes alter execution identity. License, display
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
