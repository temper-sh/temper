# `temper resolve` — missing lock rows

Status: executable M1 contract, 2026-08-20.

`resolve` adds pins for manifest layouts absent from `manifest.lock.yaml`. It
never moves, repairs, or re-resolves an existing row; changing a pin belongs to
the separate executable `update [id]` contract.

## Invocation

```text
temper resolve
  [--manifest manifest.yaml]
  [--lock manifest.lock.yaml]
  [--dry-run]
```

The lock may be absent on the first run. A present lock is parsed strictly and
every row shared with the manifest is checked for repo, selected-file, and
selected-patch drift before any upstream read.

## Resolution boundary

For each missing layout, the Hugging Face metadata read resolves the repo's
current `main` to a 40-character commit and obtains the selected file's LFS
SHA-256 without downloading model weights. A file without SHA-256 metadata is
a refusal in v1; pinning must not hash a multi-GB download just to discover its
identity.

Patch source is explicit and pinned because a local transform is defined
against exact source bytes:

```text
hf://owner/repo@<40-character-commit>/<path>[?transform=<id>]
```

Patch resolution downloads only that small source, applies the named built-in
transform when present, and records the final artifact SHA-256 under each
layout that consumes it. v1 implements `qwen38-prefix-stability-v1`; an
unknown scheme or transform refuses loudly.

The resolved civil date comes from the resolver's clock. Upstream and clock
reads are injected boundaries in tests; the suite is hermetic and offline.

## Commit and concurrency

All missing rows are accumulated in memory, the complete candidate lock is
validated and staged beside the destination, and one atomic rename commits
it. Immediately before the rename, `resolve` verifies that the original lock
bytes—or original absence—have not changed. A concurrent writer therefore
causes a refusal and rerun, never a lost update.

Existing row values remain identical. The lock is mechanically owned and may
be deterministically reserialized when rows are added. A failure before the
rename leaves the lock unchanged. `--dry-run` performs reads and validation
but writes no lock or temporary file.

## Output

Success begins with:

```text
RESULT resolve changed|unchanged|would-change entries=<count>
```

One `LOCK <layout-id> revision=<commit>` line follows for each proposed or
committed row. Exit `2` is usage refusal, `1` is input/upstream/filesystem
failure, and `0` means the result line is valid.

`resolve` does not download model weights, render configuration, materialize
an artifact set, select a model, or touch the live service.
