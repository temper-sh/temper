# `temper fetch` — one exact layout artifact set

Status: executable native artifact-fetch contract, 2026-08-20.

`fetch` materializes one explicitly named layout from a validated manifest and
lock. The layout argument is required: a command that may download many
gigabytes never infers scope or fetches every selected model by surprise.

## Invocation

```text
temper fetch <layout-id> --root PATH
  [--manifest manifest.yaml]
  [--lock manifest.lock.yaml]
  [--dry-run]
```

The exact lock row determines the model revision and every expected SHA-256.
Patch source and transform intent come from the manifest. No branch name is
used for a download.

## Atomic artifact set

One layout is one commit unit:

```text
<root>/artifacts/layouts/<layout-id>/<entry-digest>/
  model/<every-selected-file>
  patches/<patch-id>/<output-file>
  receipt.json
```

For v1 GGUF layouts that is one file. V2 directory-backed MLX/safetensors
layouts enumerate a complete sorted snapshot; the engine receives only that
immutable local directory. The entry digest covers repo, revision, every
selected file hash and patch hashes;
the human-only `resolved` date is excluded. The renderer uses paths inside this
immutable set.

When the set is absent, `fetch` downloads every member into a sibling staging
directory, applies named patch transforms, hashes bytes while writing, checks
every result against the lock, syncs the complete tree, writes a deterministic
receipt, then publishes the directory with one atomic rename. A mismatch or
interrupted download removes the stage and publishes nothing. Concurrent
identical fetches converge on the same set and verify the winner.

When the set already exists, `fetch` verifies its receipt identity and
canonical bytes, exact regular-file shape, and recorded sizes without
routinely re-hashing multi-GB weights. `apply` calls this same verifier before
it will render any selected layout. On-demand byte verification belongs to
`check --verify`; an existing malformed or incomplete immutable set is a
refusal, never an in-place repair.

`--dry-run` checks inputs and local presence only. It performs no network read,
creates no root or stage, and reports whether the set would be fetched.

## Output

Success begins with:

```text
RESULT fetch changed|unchanged|would-change layout=<id> artifact-set=<digest>
```

One `FILE <root-relative-path>` line follows per materialized file. Exit `2`
is usage refusal, `1` is input/network/hash/filesystem failure, and `0` means
the result line is valid.

`fetch` does not render or activate configuration, copy consumer files, start
llama-server, or touch llama-swap/launchd. A later `start`/mode slice may call
this same effect after explicit selection; it does not get a second downloader.
