# Local catalog and execution-lock preparation

Temper can compile an explicit local catalog and user selection into a
self-contained execution lock, then export inputs for its existing install,
fetch, render, check, bind and probe primitives. This additive development path
does not publish a catalog, choose a profile for the user, or start a service.

```sh
temper catalog compile --catalog catalog.json --selection selection.json \
  --target darwin/arm64 --out execution.lock.json [--dry-run] [--json]
temper execution export --lock execution.lock.json --out inputs \
  [--dry-run] [--json]
```

The output parent must already exist. Compilation preserves the selection and
refuses to replace a different lock. Export verifies existing derived files,
writes only absent ones, and refuses changed files, symlinks and unrelated
directory contents. A repeated successful invocation writes nothing. Each file
publishes atomically without replacement; a partial export can be rerun to fill
missing files. Consumers proceed only after the command succeeds. Dry runs
perform no writes, downloads, installations or process launches.

## Owned records and scope

`temper-catalog/v1` has Artifact, Patch, Engine, Layout and Profile records.
The initial executable slice accepts one complete GGUF per artifact, at most
one external chat template, text chat through `llama-server/v2`, exact release
archives for llama.cpp and llama-swap, and none or embedded-MTP speculation.
An incomplete interpreter/dependency closure, unsupported adapter, unknown
selected reference or incompatible patch is rejected before producing a lock.
Other engines, draft-model artifacts and catalog publication remain separate
extensions; local compilation is not a signature or qualification claim.

The lock retains only selected layouts and their material/engine records.
Hashes distinguish record metadata, loader material, engine closure, layout
execution and profile execution. A license-only edit changes record identity;
model or template bytes change material and dependent execution identities.
Display names do not invalidate execution evidence. Paths on the consuming
machine are derived after compilation and never enter the portable lock.

The authorable [Qwen refresh specimen](../../catalog/qwen38-m5-refresh.json)
and [explicit example selection](../../catalog/qwen38-m5-refresh.selection.json)
exercise the actual compiler. Availability and measured capability belong to
Results, not this file or the catalog compiler.

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

The software projection declares `target_mode: compatible` and portable
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
