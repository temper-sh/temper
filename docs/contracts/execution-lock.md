# Catalog and execution locks

Temper compiles one explicitly selected `temper-catalog/v1` preset into a
self-contained `temper-execution-lock/v1`. Installation, rendering and serving
consume that exact lock through the [execution runtime](execution-runtime.md).
User compositions live in [`temper-configuration/v1`](layouts.md).

```sh
temper catalog compile --root ~/.temper \
  --preset qwen3.8-27b-q4xl-mtp --target darwin/arm64 \
  --software stable --out execution.lock.json [--memory-gib N] [--dry-run] [--json]
temper execution inspect --lock execution.lock.json
temper execution configure --lock execution.lock.json \
  --preset qwen3.8-27b-q4xl-mtp --context 32768 --max-output 4096 \
  --out execution.32k.lock.json [--compute cpu|gpu] [--dry-run]
temper execution prepare --lock execution.32k.lock.json \
  --root /explicit/temper-root --installation candidate [--dry-run]
```

Exactly one catalog source is required: `--root ROOT` for the verified catalog
stored in a root, or `--catalog FILE` for a local catalog file, used as is with
no signature check. `--memory-gib N` selects the profile for a Mac with N GiB
of memory (default: the smallest profile).

The output parent must exist. Compilation and configuration publish atomically,
leave identical files untouched, and refuse different content or symlinks.
Changing settings requires a new output path. Dry runs create no files.

## The catalog

`temper-catalog/v1` preserves existing field meanings and accepted records.
Incompatible field or reader changes get `temper-catalog/v2` under a new URL.
Optional output limits extend the accepted v1 records: existing positive
limits keep their meaning. Earlier readers mark uncapped chat presets
unavailable, while retaining their saved presets and limits.

Top level: `schema`, `platform` (`apple-silicon`), `date` (`YYYY-MM-DD`, when
the catalog content last changed; the publisher refuses a date before the
previous catalog's or in the future), `runtime.router`, `artifacts`,
`templates`, `engines`, `presets` and `presentation`.

- **Artifacts** pin model files: `sources` (each `hub`, `repo`, 40-character
  `revision`; Hugging Face is required, other hubs are download-only
  mirrors), `format`, exact `files` (path, bytes, SHA-256) and `license`.
  `presentation` holds `model_name` and `weights_name`.
- **Templates** pin chat-template files the same way, may name a fixed
  `transform`, and list `compatible_artifacts`. A preset names one, or
  `builtin` for the template inside the model.
- **Engines** declare the typed `adapter`, `interfaces`, `modalities` and a
  software `supply`: the GitHub `source` and the tested `release`, whose
  download URL must be that source's GitHub release URL.
- **Presets** select an artifact, optional draft and projector, a template
  and an engine, and own `request_defaults`, `speculation`,
  `context_limit_tokens`, `compute` with `compute_options`, and `profiles`.
  Each profile is complete (`min_memory_gib`, `context_window_tokens`,
  `engine_config`); a Mac gets the profile with the highest `min_memory_gib`
  it meets, else the smallest.
- **Version policy:** the router and engines may set `minimum_temper` and
  `exclude_versions`; presets may set `exclude_engine_versions`. A range uses
  `<`, `<=`, `>`, `>=` or `=` and a version; a comma joins at most one lower
  and one upper bound. The rules that apply to a preset are the union of its
  router's, engine's and its own. The tested release must be allowed; `latest`
  picks the newest allowed release.
- **Presentation** lives on the record it describes. A preset's holds
  `display_name`, `description`, optional `assessment_url`, `group`, `section`
  and `memory_tier`. The top-level `presentation` holds only the `groups`
  (tabs with their sections and descriptions), `fallback_group` and
  `preset_order`. Ordering never selects a preset for the user.

Record IDs are permanent. A saved preset remembers the catalog preset it came
from, and `temper update` rebuilds it from that ID, so an ID never changes
meaning; a changed record gets a new ID.

### Reader rules

The publisher (`temper-catalog publish`, `catalog describe`) is strict: unknown
keys are refused and presentation must be complete. Clients read tolerantly,
so a newer catalog never breaks an older Temper as a whole:

- Containers (the top level, `runtime`, `presentation`) ignore unknown keys.
- Records are strict. A record this Temper cannot read (an unknown field or
  value, adapter or transform, a model with no Hugging Face source, or a
  `minimum_temper` above this version) is unavailable with a reason, and so is
  every preset that uses it. Other presets stay available.
- Presentation falls back instead of hiding: an invalid value is dropped,
  and an unknown group places the preset in the fallback group.
- A problem every preset shares (schema, platform, date, the router) refuses
  the catalog; the stored catalog stays active.

## Locks and identity

A lock contains `schema`, `preset`, portable `target` and the selected
`records`: only that preset with its selected profile, its
target/draft/projector/template, engine and router. No configuration, catalog
layout or fingerprint is serialized. Validation is structural: schema,
target, valid records, resolved software and exactly the preset's closure. It
never recompiles, so a later Temper reads an earlier lock unchanged; a lock
whose engine or router needs a newer Temper is refused with that reason.
Runtime consumption is offline and needs no source catalog, clock or
machine-specific path.

A lock compiled from one of your [own presets](user-presets.md) may hold a
template whose only source is `{"hub": "local"}`: a file in the root's
`presets.d`. Its `files` record the file's prepared bytes and digest when the
lock was compiled. Preparation reads the file from that root, needs no network
for it, and refuses a file with other content. A catalog cannot have a local
template; the publisher and every client refuse one.

Temper compares two locks by what runs: consumed material, the template
transform, exact software, request defaults and launch settings. One running
Temper computes the comparison on both sides; it is never stored or printed.
Presentation, sources, version policy, license, record IDs and choice policy
do not change it. Model and software material can therefore be reused across
settings changes. Whoever keeps evidence about a lock hashes the lock file it
owns.

## Software resolution

`--software stable` uses the catalog's tested releases offline. `latest`
resolves the newest downloadable official llama.cpp numbered build and stable
GitHub releases for llama-swap and Splash, skipping excluded versions. For
`catalog compile` a failed lookup or integrity check is an error; guided setup
and `temper update` keep that item's tested release instead and print why.

GitHub discovery resolves a tag to a commit, verifies the archive's upstream
SHA-256 and size, and computes a bounded inventory without writing files.
llama.cpp discovery scans at most 100 recent releases; drafts and builds without
the target archive are skipped. Integrity failure after selecting an archive
refuses the operation. Numbered and semantic tags are ordered only within their
own families. Python-based engines carry exact interpreter, wheel and source
dependency closures; they do not resolve from the installed environment.

Installer selections and unit maps are derived internally. Software locks
record execution provenance and compatible `darwin/arm64` targeting; they do
not invent an experiment or use the catalog date as an installation
observation.

## Settings and evidence

Each preset requires `compute` (`cpu` or `gpu`) and a nonempty, unique
`compute_options` list containing that selection. The authored selection is the
default; an execution lock retains the user's choice. Options are curated per
preset for practical use and may be narrower than the adapter's capabilities.
llama.cpp can offer both devices; Splash, RapidMLX and vLLM Metal currently
allow only GPU. The CrispEmbed adapter exposes CPU reranking. Unsupported
choices are rejected. These fields remain in v1 without inferred defaults or
compatibility readers.

For llama.cpp, `engine_config.gpu_layers` retains the positive offload count
used in GPU mode. CPU mode renders zero GPU layers and explicitly disables
device, KV and operation offloading, including the external draft's device.
Switching back restores the authored GPU count. CPU-only presets may set that
count to zero. Compute selection changes what runs; allowed choices and
inactive GPU tuning do not.

`execution configure --compute cpu|gpu` selects among the retained choices,
alongside the required context and optional output default. Omitting it preserves the
current device. Weights and installed software remain reusable.

The authored `context_window_tokens` counts input plus output. A window must
be positive and remain within
`context_limit_tokens`, or the authored window if no separate ceiling is known.
Setup accepts the authored default. It never infers fit from a larger machine.

`request_defaults.max_output_tokens` is optional. Omission or zero means no
separate output cap: generation may use the context remaining after the rendered
input, including reasoning tokens, and stops normally when the model finishes.
A positive value supplies an overridable request default. It does not reserve
that space in advance or constrain context edits; the engine's context boundary
still applies. Negative values are invalid. A client's explicit request allowance
takes precedence over the default, within the engine's request/context rules.

`execution configure` takes the full desired context/output settings.
Omitting `--max-output`, or passing `--max-output 0`, clears the old output
default in the new lock. Explicit experimental allowances remain ordinary
positive values. Changing a default does not change the conditions or claims
of historical measurements.

The adapters implement omission as follows:

- llama.cpp uses `--predict -1` and disables context shifting, so reaching the
  context boundary stops generation without discarding history. An explicit
  output default retains the configured context-shift behavior.
- Splash sets its native default to the configured window; its frontend clamps
  omitted request allowances to the space remaining after tokenizing the input.
- vLLM clears only `max_new_tokens` from the model's generation configuration
  and uses its native remaining-context calculation. Explicit defaults are
  passed through the router's defaults filter, preserving request overrides.
- Rapid MLX's current adapter requires an explicit positive default because
  its CLI cannot express remaining-context generation. Omitting the native flag
  would impose the engine's finite default; a window-sized explicit default can
  instead reject valid prompts. No shipped catalog preset uses this adapter.

Reranking presets carry no generation defaults: output allowance is absent or
zero and reasoning/sampling are absent. They need no `--max-output` argument.
CrispEmbed reads its fixed sequence limit from the selected GGUF. Its preset
window must equal the authored model limit; context overrides are refused and
the picker disables that editor.

`execution configure` changes only context, output allowance, optional compute
selection and Splash `max_memory_bytes`. It preserves software, weights, templates, sampling
and speculation. A nonzero memory override is refused
for other engines. Setup's `--context PRESET=TOKENS` and
`--compute PRESET=cpu|gpu` produce customized exact presets and are saved as
the preset's choices.

Preset `description` and optional `assessment_url` are edited with
`catalog describe --preset ID`. `--if-empty` preserves existing authored copy.

## Engine contracts

llama.cpp accepts complete GGUF material, text reranking, or chat with text
and optional image input. Chat permits at most one external template.
Image input requires `projector_artifact`, a separate exact GGUF Artifact.
It joins the selected lock closure, manifest/lock projection, verified atomic
fetch and memory/download totals. The renderer supplies its installed path to
`--mmproj`; CPU placement also disables projector offloading. Text-only presets
continue to use `--no-mmproj`. Changing projector bytes invalidates the artifact
set and execution while preserving reusable target weights and software.

A template may name a fixed `transform` owned by `internal/patch`.
Its repository, revision and path identify the source; `files` records the
transformed output bytes and digest. Compilation carries the transform into
the pinned manifest source, and fetch transforms before verifying and
publishing the material. `translategemma-text-input-v1` adapts plain text and
language-code kwargs to the publisher's structured input, preserving its
translation prompt. The independently pinned source is shared by all three
TranslateGemma sizes.

Speculation may be disabled, embedded MTP, or an exact GGUF draft for
MTP/DFlash. External DFlash uses 1–15 draft tokens; MTP uses 1–16. Target and draft
material remain independent identities. These controls do not establish that an
arbitrary pairing is correct or faster.

`llama-server/v1` exposes explicit cache, reasoning, context shift, fitting,
thread, load-mode and sampling controls. `checkpoint_min_step` maps to
`--checkpoint-min-step`: omitted uses the engine default, zero removes the
minimum, and negative values refuse. The sampling `seed` is optional: set, it
maps to `--seed` for repeatable runs; omitted, the engine picks a random seed,
so asking again gives a new answer. KV `q4`, `q8`, and `f16` map to `q4_0`,
`q8_0`, and `f16` for both K and V. The optional output default supplies
`--predict` independently of that controls block; explicit API request values
can override server defaults.

`crispembed/v1` serves a complete GGUF text reranker with explicit positive
`threads`, CPU placement and offline model loading. It installs the pinned
upstream release archive through the existing release adapter, launches
`crispembed-server` on loopback, checks `/health/ready`, and exposes `/rerank`
and `/v1/rerank`. It has no generation, KV, speculation or chat-template
controls. The selected converter must preserve the model's full scoring head;
being a GGUF file alone does not establish compatibility.

Splash's distinct typed configuration owns KV precision, memory cap, reasoning
and request timeout. It selects a target GGUF and an exact DFlash2 sidecar
(`config.json` plus `model.safetensors`). The release-owned architecture mapping
checks their compatibility. Preparation includes the sidecar automatically.
`splash/v1` preserves the Splash 1.1 frontend and native argument contract for
saved executions. `splash/v2` uses Splash 1.3.2's single source-assembly root and
package frontend, with native metadata-only target/draft validation. Its SSD
prefix cache is disabled explicitly; ANE prefill and FP32 recurrent state use
the selected release's defaults. Omitted output limits use remaining context;
an explicit limit becomes a router request default that clients can override.

The frozen test catalogs in `internal/testfixture/testdata/` exercise these
contracts. Results and the cited studies own capability claims.

## First public schema

Temper 0.1.0 starts the catalog, execution lock, manifest and engine adapters at
v1. Pre-release v2/v3 documents, Selection files and software-supply catalogs
are rejected. There are no schema aliases, importers or compatibility exports.
Use a fresh root and recompile current presets. Frozen historical experiments
retain their original inputs and pinned host; their evidence is unchanged.
