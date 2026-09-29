# Signed catalog distribution

Temper publishes the maintained `temper-catalog/v3` independently of its binary.
The catalog remains authored in this repository. One `stable` channel supplies
reviewed installable records; model assessments remain in Results.

## Consumer commands

```text
temper catalog update --root ROOT [--dry-run] [--json]
temper catalog inspect --root ROOT [--preset ID] [--json]
temper catalog compile --root ROOT --preset ID --target darwin/arm64 \
  --out NEW_LOCK [--software recorded|latest|tested] [--dry-run] [--json]
temper catalog rollback --root ROOT --snapshot SHA256 [--dry-run] [--json]
```

`update` makes four bounded HTTPS reads with a 30-second overall deadline. It
verifies both signatures, the channel identity and catalog digest, and the
catalog's supported schema and capabilities before changing local state.
Failures leave the previous active catalog usable. No background update exists.

Inspection, recorded compilation and rollback use verified local
snapshots without network access. Inspection lists retained snapshot identities
and available presets; `--preset` displays the exact composition. A different
existing Execution Lock is never overwritten; choose a new path.
Compilation accepts exactly one of `--root` (verified active publication) and
the existing `--catalog` (explicit local authoring input).

Rollback deliberately selects an exact retained, verified snapshot. The store
remembers the highest accepted channel sequence even after rollback. An ordinary
network update refuses an older sequence or a different digest at the same
sequence. Updating after rollback may explicitly restore the highest accepted
snapshot or accept a newer one.

These commands never install software, change an installation, start processes,
or rewrite an existing Execution Lock. Latest and tested software
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

The channel uses `temper-catalog-channel/v1` and names `temper-catalog/v3`, a
positive sequence, the exact catalog SHA-256 and its immutable HTTPS directory.
Sequence belongs to the signed publication, not the authored model records.
Each changed publication uses a new sequence and catalog digest; an identical
publication is a clean rerun. The existing Ed25519 trust root signs exact bytes.
Private signing material enters the release tool only through stdin.

Older catalog and software-publication schemas are rejected. Published historical
bytes remain immutable for their pinned older clients; they are not accepted
rollback targets for the current binary. The checked-in stable pointer names
signed v3 sequence 3 for alpha.11. Deploy and verify that publication before
announcing the new client.
