# `manifest.yaml` (`temper-manifest/v2`)

The low-level manifest declares a renderable composition. The current product
workflow stores [presets and user layouts](../contracts/layouts.md); execution
commands derive manifests internally. Explicit manifest users retain ownership
of their file and resolve model/template pins into `manifest.lock.yaml`.

Manifest v1 is rejected. Its role, model.file, top-level KV, preferred-member and
llama speculation aliases are removed. Historical design and examples remain
in Git at `f59c281:docs/design/manifest-schema.md`.

## Layouts and modes

A manifest **layout** declares model material, engine, technical interface,
modalities and inference settings. A **mode** explicitly selects members,
placement, tool services and harnesses. These are low-level renderer terms;
they do not define catalog recommendations or user-layout storage.

```yaml
schema: temper-manifest/v2
defaults: {ttl: 1800, gpu_memory_utilization: 0.85}
patches: {}
layouts:
  assistant:
    display_name: Assistant
    model: {repo: example/model, format: gguf, files: [model.gguf]}
    engine: llama-server
    interface: chat-completions
    modalities: [text]
    window: 32768
    max_tokens: 4096
    thinking: off
    speculation: {method: none}
    llama: {kv: q8, parallel: 1, flash_attention: on, batch: 512, ubatch: 512}
tools: {}
modes:
  work:
    foreground: assistant
    tools: []
    harnesses: []
    members:
      resident: [{layout: assistant, ngl: 99}]
      on_demand: []
  off: {foreground: none}
```

Each model supplies `repo`, explicit `format` and sorted, distinct relative
`files`. GGUF selects one complete file. MLX/safetensors snapshots declare the
files required by their typed engine contract. Template patches have independent
immutable sources; a layout references a selected patch with `chat_template`.
External speculation drafts have their own model record and lock identity.

Interfaces are `chat-completions` and `reranking`; modalities are explicit.
A chat layout needs a positive output allowance below its context window and
explicit thinking behavior. A reranker cannot carry chat-only settings.
Speculation explicitly selects `none`, `mtp`, `dflash` or `dflash2`, subject to
the engine and draft constraints in the [execution contract](../contracts/execution-lock.md).

Exactly one engine tuning block must match `engine`: `llama`, `splash`,
`rapid_mlx`, `mlx_vlm` or `vllm_metal`. Unknown fields, unsupported combinations,
invalid budgets and unsafe paths refuse before effects. Engine support in a
parser is not a capability or machine-fit claim.

## Composition

A local `foreground` names a resident chat-completions member directly.
`external_foreground: true` leaves the default with the client while exposing
explicit helper members; it cannot name a local foreground. `foreground: none`
is an empty mode. Optional `services` maps a tool-facing service, currently
`rerank`, to an included member implementing that interface.

Members choose `ttl`, llama-only `ngl`, and optional resident `preload`.
Availability, foreground/default selection and startup loading are distinct.
Repeated membership and on-demand preload refuse. Tools and harnesses are
explicit choices; a tool's required services must be bound. Memory checks are
separate from structural schema validation.

## Rendering

`apply` validates locked, materialized artifacts before producing an immutable
render generation. Its pure renderer owns llama-swap, engine argv and selected
harness configuration. Pi compaction derives from the foreground's window and
output allowance. With external foreground, provider-owned Pi defaults remain
unchanged. Existing unrelated harness settings are preserved.

Unknown engine/harness support is a refusal, not a partial render. The
[apply contract](../contracts/apply.md), [check contract](../contracts/check.md)
and [renderer tests](../../internal/render/) own publication, admission and
observable output behavior.
