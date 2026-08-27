# `temper update` — move existing lock pins

Status: executable native lock-update contract, 2026-08-20.

`update` re-resolves existing layout pins from authoritative upstream metadata
and commits changed lock rows once. It does not download model weights,
materialize artifacts, render configuration, or touch the live service.

## Invocation

```text
temper update [layout-id]
  [--manifest manifest.yaml]
  [--lock manifest.lock.yaml]
  [--dry-run]
```

One layout ID is the normal operation. With no ID, every manifest layout is
re-resolved in stable ID order and output includes an `update-all` warning:
the single lock commit is atomic, but the operator is accepting several
independent upstream moves and acceptance obligations together. More than one
positional ID is a usage refusal.

Both manifest and lock must already exist and parse strictly. Every target
must have a lock row whose repository, selected model file, and selected patch
still agree with the manifest. A missing row is resolved by `temper resolve`;
selection drift is fixed by the human who owns the manifest, never interpreted
as permission for `update` to change repositories or files. Non-target rows,
including orphans, retain their values.

## Resolution boundary

For every target, the Hugging Face metadata read resolves the repository's
current `main` commit and the selected file's authoritative LFS SHA-256 without
downloading model weights. A selected file without authoritative SHA-256
metadata is a refusal.

When a layout selects a patch, `update` also reads the patch source at the
exact commit already pinned in the manifest, applies the named built-in
transform, and hashes the final small artifact. This recalculates the complete
row through the same resolution path as `temper resolve`; it does not move the
manifest's patch source.

The new row receives today's civil date only when its artifact identity
changes. If revision, model hash, and patch hashes are all unchanged, the old
row—including its original `resolved` date—is retained and the command is
second-run-clean.

## Commit and concurrency

All proposed rows are accumulated in memory and the complete candidate lock is
validated before any write. `--dry-run` performs upstream reads and candidate
validation but creates no file or temporary file.

A non-dry run stages the complete candidate beside the lock, syncs it, verifies
that the original lock bytes have not changed, and commits once with an atomic
rename. Any target or upstream failure aborts the whole operation. A concurrent
writer causes a refusal and rerun, never a lost update.

Changing a row selects a new content-addressed artifact set. Existing artifact
sets are not removed and the new one is not fetched implicitly; the explicit
follow-up is `temper fetch <layout-id>`, then `temper apply` when the operator
chooses to activate it.

## Output

Success begins with:

```text
RESULT update changed|unchanged|would-change targets=<count> changed=<count>
```

Bare `update` then prints:

```text
WARNING update-all targets=<count> detail="re-resolved independent layout pins together"
```

One stable line follows for every target:

```text
LOCK <layout-id> changed|unchanged old-revision=<commit> new-revision=<commit> old-artifact-set=<digest> new-artifact-set=<digest>
```

For each changed layout, the final output is its role-specific, ready-to-paste
acceptance gate. Each `GATE` line names the claim and the next `COMMAND` line is
the complete command:

```text
GATE <layout-id> plain-completion
COMMAND <curl and jq command>
GATE <layout-id> streaming-tool-call
COMMAND <curl, SSE filtering, and jq command>
```

A coder receives both the plain completion and streamed parsed-tool-call
checks. The streaming check requires a `tool_calls` delta; a non-streaming
response cannot cover this regression. A reranker receives one ordering and
score-magnitude check: the relevant document must rank first and the maximum
score must exceed `0.001`, catching GGUFs that return HTTP 200 with degenerate
scores. Commands address the selected layout ID at the local OpenAI-compatible
endpoint.

The commands are printed and never executed, including during a non-dry run.
Exit `2` is usage refusal, `1` is input/upstream/filesystem failure, and `0`
means the result line is valid. Tested-catalog membership warnings join this
surface once the signed software catalog exists; v1 does not invent a local
verified/unverified state.
