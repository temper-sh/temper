# `temper apply` — first native contract

Status: executable pre-release contract, revised 2026-08-20.

`apply` turns one reviewed manifest, one exact lock, one fully materialized
selected mode, and optional harness bases into a complete content-addressed
config generation. It does not inspect the running service to decide what the
world should contain.

## Invocation

```text
temper apply --root PATH
  [--manifest manifest.yaml]
  [--lock manifest.lock.yaml]
  [--mode local]
  [--pi-models-base PATH]
  [--pi-settings-base PATH]
  [--dry-run]
```

`--root` is required and is never inferred from the legacy stack. Manifest and
lock paths default to files beside the invocation. Pi base files are optional;
when provided, Temper preserves unowned providers and settings while replacing
the generated local provider, setting the explicitly preferred local model,
and deriving compaction keys.

## Artifact admission

Every resident and on-demand layout in the selected mode must already have its
exact content-addressed artifact set beneath `--root`. `fetch` hashes every
downloaded or transformed byte before atomically publishing that set and its
canonical receipt. Before rendering—even during `--dry-run`—`apply` uses the
same verifier as `fetch` to require:

- receipt identity matching the selected layout and lock-entry digest;
- the exact expected file and directory set, with no symlinks or extras; and
- regular-file sizes matching the receipt created after hash verification.

An absent set is an actionable refusal: `run temper fetch <layout>`. A malformed
set is also a refusal, never a fallback to a repository reference. Routine
apply does not reread multi-GB weights to recompute their hashes;
`check --verify` owns that explicit full-byte audit.

## Pure render boundary

The render is a pure function of:

```text
(manifest bytes, lock bytes, selected mode, absolute root, optional Pi bases)
  -> ordered artifact bytes
```

The first schema revision supports `llama-server`, `foreground: local` and
`foreground: none`, and the Pi harness. Unknown fields and unsupported values
are refusals. A selected layout must have a matching lock row, exact model
file, and every selected patch hash.

The local mode produces:

```text
llama-swap/config.yaml
pi/models.json          # only when Pi is selected
pi/settings.json        # only when Pi is selected
```

`off` produces a llama-swap config with `models: {}` so unload is expressed by
the same total-world mechanism as load.

## Managed state and commit

Artifact bytes determine a SHA-256 generation ID. A non-dry apply stages and
syncs the complete bundle beneath:

```text
<root>/rendered/generations/<generation>/...
```

It then atomically replaces the relative symlink:

```text
<root>/rendered/current -> generations/<generation>
```

Existing generations are immutable and retained. If a directory already
occupying the content-derived ID differs from the expected bytes, `apply`
refuses rather than overwriting it. If `current` already selects a verified
matching generation, the result is `unchanged` and no file is rewritten.

`--dry-run` performs reads, artifact admission, validation, and rendering only.
It reports whether the pointer would change and creates or rewrites no path.

## Output and exits

Success begins with one machine-readable line:

```text
RESULT apply changed|unchanged|would-change mode=<id> generation=<sha256>
```

One `ARTIFACT <relative-path>` line follows per generated artifact. Exit `2`
is CLI usage refusal; exit `1` is an input, rendering, or filesystem refusal;
exit `0` means the reported result is valid.

## Intentional improvement over legacy output

The Bash renderer remains comparison evidence, not a compatibility surface.
For llama-server, native output uses the lock's exact revision and selected
file to construct a local `-m` path and adds `--offline --no-mmproj`. It does
not emit legacy `-hf` references that can follow a moving branch or permit
automatic projector downloads. Typed manifest fields replace raw flag
strings, and tests pin each field-to-config mapping.
`preferred` selects Pi's starting model but never implies a llama-swap
startup preload; `preload` is a separate explicit member choice. The
flash-attention enum preserves `auto` as distinct from `off`. The paired
`llama.spec_type: draft-mtp` and `llama.spec_draft_n_max` fields render exact
embedded-MTP flags; omitted fields render no speculative-decoding behavior.

## Deliberately outside this slice

- resolving or rewriting lock rows (`temper resolve` owns that commit);
- downloading or routinely re-hashing multi-GB artifacts;
- copying configs into consumer-owned homes;
- kicking or querying llama-swap or launchd;
- selecting a model, tool, harness, or mode on the user's behalf.
