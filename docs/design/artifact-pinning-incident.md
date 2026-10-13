# Artifact pinning — a witnessed failure the manifest and lock must prevent

Written 2026-08-20 from a live incident on the legacy stack. This is input for
the `temper-manifest/v1` and `temper-lock/v1` design, not a decision. The short
version: a
model reference that names a *repository* rather than an *artifact* silently
changed the served model, and the engine offers no flag that can pin it back.

## Disposition in Temper (implemented 2026-08-20)

The accepted v1 design closes the serving path that caused this incident:

- the lock identifies the exact revision, selected filename, and SHA-256;
- `fetch <layout>` downloads only that selection, hashes it before publishing,
  and records the resulting regular-file sizes in a canonical immutable-set
  receipt;
- `apply`, including dry-run, refuses every selected layout whose exact set is
  absent or whose receipt, identity, filesystem shape, or recorded sizes fail
  the shared verifier; and
- the renderer emits only the verified local `-m` path with `--offline` and
  `--no-mmproj`; it never emits `-hf`.

Routine apply trusts the receipt that `fetch` published after hashing instead
of rereading every multi-GB file. Explicit `check --verify` owns that full-byte
audit. The incident's proposed expected-size field did not enter the
v1 lock: expected download size belongs to the reviewed catalog profile, while
the receipt records the size actually established during fetch.

## What happened

The legacy manifest served its coder with `-hf unsloth/Qwen3.8-27B-GGUF:Q4_K_M`
— a repo plus a quant label, which is how llama.cpp's convenience path works.
That reference is unpinned: it follows the repository's main branch.

On 2026-08-19 the publisher rewrote the repo. `Qwen3.8-27B-Q4_K_M.gguf`
(15.93 GiB) **no longer exists**; it was replaced by
`Qwen3.8-27B-UD-Q4_K_M.gguf` (15.33 GiB), a different quantization method with
different weights. The same commit range also added `mmproj-*.gguf` vision
projectors and reorganised an MTP drafter into a subdirectory.

Nobody changed the manifest. The next process restart was enough: llama-server
resolved the new file, downloaded ~16 GB, and additionally auto-downloaded a
vision projector into a profile that documents vision as disabled. The stack
had been validated — a real task completed under frozen acceptance criteria —
against an artifact the machine no longer loads.

## Why it could not simply be pinned back

Three attempts, all instructive:

1. **`--hf-file Qwen3.8-27B-Q4_K_M.gguf`** — refuses to resolve, because the
   filename is absent from main. Naming a file is not pinning a revision.
2. **`-m <absolute path to the cached blob>`** — the validated blob is still on
   disk and the revision is still fetchable by SHA, so this *should* work. It
   does not: **`-hf` silently overrides `-m`.** Verified by
   `GET /props` on the running server reporting the UD path while the command
   line contained both flags. No warning, no error — the wrong model loads and
   everything looks healthy.
3. **`--no-mmproj`** — this one works, and is the only mitigation that took
   effect.

llama.cpp b10360 exposes `-hf <user>/<model>[:quant]` and
`-hff/--hf-file FILE`. **There is no revision argument.** The convenience path
cannot express an immutable artifact.

## What this means for the design

**A repo reference is intent; it must never reach a rendered command.** The
schema note already says "Resolution lives in the lock: `model`/sources here
are intent; revisions and file hashes are `manifest.lock.yaml` rows." This
incident is the proof of why that boundary has to be enforced at *render* time,
not merely recorded. Concretely:

- **The renderer emits a resolved local path, never a repo reference.** For a
  locked llama-server layout it emits `-m <path>` and **must not** emit `-hf`
  at all. Emitting both is not belt-and-braces; it is a silent wrong answer,
  because the repo reference wins.
- **The lock row for an artifact needs revision, filename, size and SHA-256.**
  Revision alone is insufficient — the incident shows publishers rename and
  replace files within a repo, so the row must identify the file, and the hash
  is what makes the identity checkable rather than assumed. The legacy
  manifest already does exactly this for mlx-vlm entries through their `files:`
  block; the llama-server path never got the same treatment, and that asymmetry
  is the whole bug.
- **Resolution is a separate, explicit step from serving.** Fetch by revision,
  verify the hash, then serve the verified local path. `temper apply` should
  refuse to render a layout whose lock row is missing or whose on-disk artifact
  fails its hash, rather than falling back to a repo reference.
- **A repo reference can drag in undeclared components.** `-hf` auto-downloaded
  a vision projector. Whatever the manifest does not name must not arrive. If a
  layout declares text-only, the render should carry the flag that refuses
  extras (`--no-mmproj` here), and the resolver should not fetch them.
- **`temper check` gains a drift class**: *resolved artifact ≠ locked artifact*.
  This is cheap — compare the served model's reported path and hash against the
  lock. On this incident, `check` would have caught it at the first restart
  instead of a human noticing a vision warning in a log.

## Cheap detection worth stealing

llama-server answers `GET /props` with `model_path`. That single field is what
proved the pin had failed. Any engine adapter should expose "what did you
actually load", and `status` should surface it. A served model that disagrees
with the lock is a business refusal, not a warning.

## Reproduction facts, for whoever implements this

- Repo: `unsloth/Qwen3.8-27B-GGUF`.
- Validated revision `f1bfb127c64f7072bdd2cad55f258b9c8b2910fe`, file
  `Qwen3.8-27B-Q4_K_M.gguf`, 15.93 GiB. Still fetchable by revision
  (`hf download ... --revision f1bfb127…`) even though absent from main.
- Drifted-to revision `27af057ecb382ddfea5d12837360a8980560e3ed`, file
  `Qwen3.8-27B-UD-Q4_K_M.gguf`, 15.33 GiB, plus `mmproj-BF16.gguf` 0.87 GiB.
- `hf cache prune` treats the validated snapshot as an orphaned revision and
  will delete it; the cost is a re-download, not permanent loss.

## What is *not* claimed here

That the new artifact is worse. Unsloth's Dynamic 3.0 may well be an
improvement, and re-qualifying it is a separate, legitimate question. The
failure is not "we got a bad model" — it is that **the served model changed
identity without a decision, and the tooling could neither prevent nor report
it.** A pinning design that only protects against bad artifacts misses the
point; it must protect against *unrequested* ones.
