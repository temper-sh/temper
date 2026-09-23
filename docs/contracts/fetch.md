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

When the set is absent, `fetch` first looks for its model hashes in complete,
receipted sets under the same root. It hard-links reusable model files into a
sibling staging directory and verifies the linked bytes against the selected
SHA-256. Missing model files and the selected template are downloaded and
verified. Models missing from Temper are first looked up in the shared HF cache;
cache misses are downloaded there by the official `hf` client. Fetch applies
named patch transforms, syncs the complete tree, writes
a deterministic receipt, then publishes the directory with one atomic rename.
A mismatch or interrupted download removes the installation stage and publishes
no artifact set. HF cache files and resumable partials remain owned by HF.
Concurrent identical fetches converge on the same set and verify the winner.

Template and layout changes retain separate set identities, paths and receipts
while sharing already installed model bytes. Existing v1 receipts need no
migration. The filesystem must support hard links between the sets; a failed
link is reported rather than silently consuming another model-sized copy.
Removing one set leaves another set's links intact. Shared model files remain
immutable: Temper never edits them in place. Discovery never credits private
stages, patch files or invalid receipts; malformed material claiming a requested
model hash is refused before reuse. Unrelated damaged sets do not block a new
selection.

## Shared Hugging Face cache

Model downloads use the standard Hub cache. The location follows
`HF_HUB_CACHE`, then its legacy alias `HUGGINGFACE_HUB_CACHE`, then
`HF_HOME/hub`, then `XDG_CACHE_HOME/huggingface/hub`, finally
`~/.cache/huggingface/hub`. Temper reads exact revision snapshots, including
relative blob links, or an existing same-repository blob named by the locked
SHA-256. Cache presence is a candidate for reuse; installation still verifies
the staged bytes against the lock.

On a miss, Temper invokes:

```text
hf download --revision COMMIT --cache-dir CACHE --quiet -- REPO FILE
```

An installed `hf` on PATH is used as-is. When absent, `uv tool run --no-config
--from huggingface-hub hf ...` provisions the official client in uv's isolated
tool cache. The downloader is support software; its version does not change the
locked model identity, which Temper verifies independently. No exact HF version
is imposed, and a failing installed client is reported without silently
switching tools. If neither executable is available, the error asks the user to
install uv and provides `uv tool install huggingface-hub`. System-installer
adapters remain unwired. New HF/Python support-tool downloads and HF cache
overhead are additional to the model/runtime size allowance.

HF owns authentication, transport, cache locking, partial downloads and snapshot
publication. Temper inherits HF configuration and disables HF telemetry and
automatic update checks. It does not parse progress output, write HF cache
metadata, force downloads, change branch refs, or prune shared cache files.
Cancellation stops the downloader process group, including a uv-spawned Python
process. A failed preparation preserves HF's resumable state and completed
cache files for a later retry.

Installation uses a hard link to the resolved cache file on the same filesystem,
or a verified copy when linking reports a different filesystem. Other link
failures are reported. The prepared model is a regular file beneath the Temper
root: removing the HF cache does not invalidate it, and removing a Temper
installation does not remove the shared cache. Templates, transformed patches
and receipts stay in the Temper artifact set.

## Existing installations and preview

When the set already exists, `fetch` verifies its receipt identity and
canonical bytes, exact regular-file shape, and recorded sizes without
routinely re-hashing multi-GB weights. `apply` calls this same verifier before
it will render any selected layout. On-demand byte verification belongs to
`check --verify`; an existing malformed or incomplete immutable set is a
refusal, never an in-place repair.

`--dry-run` checks inputs and local presence only. It performs no network read,
creates no root, stage or cache, does not invoke hf/uv, and reports whether the
set would be prepared. Guided setup additionally inspects shared cache presence
and disk usage in its [review](init.md).

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
