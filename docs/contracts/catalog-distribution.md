# Signed catalog distribution

Temper publishes the maintained `temper-catalog/v2` independently of its binary.
The catalog remains authored in this repository. One `stable` channel supplies
reviewed installable records; model assessments remain in Results.

## Consumer commands

```text
temper catalog update --root ROOT [--dry-run] [--json]
temper catalog inspect --root ROOT [--profile ID] [--json]
temper catalog select --root ROOT --profile ID --out SELECTION [--dry-run] [--json]
temper catalog compile --root ROOT --selection SELECTION --target darwin/arm64 \
  --out NEW_LOCK [--software recorded|latest|tested] [--dry-run] [--json]
temper catalog rollback --root ROOT --snapshot SHA256 [--dry-run] [--json]
```

`update` makes four bounded HTTPS reads with a 30-second overall deadline. It
verifies both signatures, the channel identity and catalog digest, and the
catalog's supported schema and capabilities before changing local state.
Failures leave the previous active catalog usable. No background update exists.

Inspection, selection, recorded compilation and rollback use verified local
snapshots without network access. Inspection lists retained snapshot identities
and available profiles; `--profile` displays the selected composition. Selection
writes only the explicitly named profile to the user-owned Selection. A different
existing Selection or Execution Lock is never overwritten; choose a new path.
Compilation accepts exactly one of `--root` (verified active publication) and
the existing `--catalog` (explicit local authoring input).

Rollback deliberately selects an exact retained, verified snapshot. The store
remembers the highest accepted channel sequence even after rollback. An ordinary
network update refuses an older sequence or a different digest at the same
sequence. Updating after rollback may explicitly restore the highest accepted
snapshot or accept a newer one.

These commands never install software, change an installation, start processes,
or rewrite an existing Selection or Execution Lock. Latest and tested software
resolution retain the [execution-lock contract](execution-lock.md)'s explicit
policy and compatibility checks. Unknown required/tested boundaries stay absent.

## Publication

The public entry is
`https://temper-sh.github.io/temper/catalog/channels/stable/channel.yaml`.
GitHub Pages serves the repository's `/docs` directory on `master`.

```text
channels/stable/channel.yaml
channels/stable/channel.signature.yaml
snapshots/<catalog-sha256>/catalog.json
snapshots/<catalog-sha256>/catalog.signature.yaml
```

The channel uses `temper-catalog-channel/v1` and names `temper-catalog/v2`, a
positive sequence, the exact catalog SHA-256 and its immutable HTTPS directory.
Sequence belongs to the signed publication, not the authored model records.
Each changed publication uses a new sequence and catalog digest; an identical
publication is a clean rerun. The existing Ed25519 trust root signs exact bytes.
Private signing material enters the release tool only through stdin.

The earlier software catalog and its issued snapshots retain their parsers and
signatures. Old binaries that cannot understand the current channel
refuse it before changing their store. Existing execution locks remain usable
without any catalog request.

## Local commit and recovery

The current catalog lives under `ROOT/catalog`, separate from the retained
legacy `ROOT/software/catalog` store. Each immutable snapshot retains the
catalog, its signature and the signed channel that authenticated its sequence.
One small state file names the active and highest accepted snapshots.

A mutation stages and verifies the complete snapshot before atomically replacing
that state file. Writers serialize the final comparison and commit with a kernel
file lock and refuse stale observations. Interrupted staging cannot make partial
content active; rerunning verifies or fills the immutable snapshot and completes
the commit. Existing snapshots are retained for explicit rollback.

Dry runs create no directories, lock files, temporary files or state. Identical
reruns preserve existing files and their modification times. Local reads recheck
signatures and identities; symlinks and malformed state are refused.
