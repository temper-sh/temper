# Signed catalog distribution

Temper publishes the maintained `temper-catalog/v1` independently of its
binary. The catalog sources live in the Results site repository; Temper owns
their format and the publishing tool ([publication contract](catalog-signing.md)).
One `stable` channel serves every platform.

## Consumer commands

```text
temper catalog update --root ROOT [--dry-run] [--json]
temper catalog inspect --root ROOT [--preset ID] [--json]
temper catalog compile --root ROOT --preset ID --target darwin/arm64 \
  --out NEW_LOCK [--software stable|latest] [--memory-gib N] [--dry-run] [--json]
temper catalog rollback --root ROOT --snapshot SHA256 [--dry-run] [--json]
```

`temper update` and `temper catalog update` are the only commands that
download a newer catalog. The first real `temper init` or `configure` stores
the verified catalog when the root has none. There is no background check.

`update` reads the channel with a 30-second overall deadline and verifies its
signature. When the channel names the active catalog for this platform, it
downloads nothing and reports `changed: false`; a higher sequence is still
stored as the new floor. Otherwise it downloads the snapshot and verifies its
signature, digest and platform before changing local state. Failures leave
the previous active catalog usable.

Inspection, stable compilation and rollback use verified local snapshots
without network access. Rollback selects an exact retained snapshot. The store
keeps the newest verified channel as the floor even after rollback: an
ordinary update refuses an older sequence, or a different digest at the same
sequence.

These commands never install software, change an installation, start
processes or rewrite an existing Execution Lock.

## Channel

The public entry is
`https://temper-sh.github.io/catalog/v1/channels/stable/channel.yaml`, with
`channel.signature.yaml` beside it. The catalog schema major is part of the
path: `catalog/vN/` only ever names `temper-catalog/vN`.

```yaml
schema: temper-catalog-channel/v1
channel: stable
sequence: 7
catalogs:
  apple-silicon:
    schema: temper-catalog/v1
    sha256: <catalog-sha256>
    locator: https://temper-sh.github.io/catalog/v1/snapshots/<catalog-sha256>/
```

- One sequence for the whole channel. Publishing any platform adds one.
- A client parses only its own platform's entry, strictly. Other entries are
  skipped unparsed, so a later field there never breaks an older client.
  Its own entry missing means "no catalog for this platform".
- An entry naming a newer catalog schema means "this catalog needs a newer
  Temper"; the local catalog stays active.
- The catalog file carries the same `platform`; the client checks it.
- Snapshots are content-addressed folders shared by every platform:
  `snapshots/<sha256>/catalog.json` and `catalog.signature.yaml`.

Both files are signed by the Ed25519 trust root compiled into the binary
(detached `temper-signature/v1` envelopes over the exact bytes).

## Local store

```text
ROOT/catalog/state.json                  active and latest snapshot digests
ROOT/catalog/channel/                    newest verified channel (the floor)
ROOT/catalog/snapshots/<sha256>/         catalog, its channel, both signatures
```

The store is durable user data, like `configuration.json`; the rest of the
root is cache.
