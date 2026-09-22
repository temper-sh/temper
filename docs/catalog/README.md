# Catalog publication

GitHub Pages serves this directory from `master:/docs` at
`https://temper-sh.github.io/temper/catalog/`. The current entry is
[`channels/stable/channel.yaml`](channels/stable/channel.yaml).

The first maintained publication contains the Qwen3.8 27B profile authored in
[`catalog/qwen38-m5-refresh.json`](../../catalog/qwen38-m5-refresh.json).
The snapshot is an exact published copy of that source. Its retained software
versions reproduce the reviewed configuration; they do not assert minimum
required or minimum tested boundaries.

```text
channels/stable/channel.yaml
channels/stable/channel.signature.yaml
snapshots/<catalog-sha256>/catalog.json
snapshots/<catalog-sha256>/catalog.signature.yaml
```

Snapshots are immutable. Keep previously published bytes available for existing
clients and deliberate rollback. The older `catalog.yaml` software snapshot is
retained alongside current-format snapshots; it is not maintained in parallel.

## Publish a catalog update

Review the authored catalog and its evidence references. A data update using
existing Temper capabilities needs no new binary release. New schema or engine
capabilities require a compatible client before their catalog entries ship.

1. Copy reviewed source bytes to `snapshots/<sha256>/catalog.json`, using their
   exact SHA-256 for the directory name. Never replace an existing digest.
2. Sign the snapshot with the [signing command](../contracts/catalog-signing.md).
3. Update the stable channel to the new digest, immutable HTTPS directory and a
   sequence greater than the current one. Sign its exact final bytes.
4. Run `go run ./cmd/temper-catalog verify-publication --root docs/catalog`.
5. Commit and push the reviewed publication files together. Pages deploys
   `/docs` from `master`. Verify the served publication through
   `temper catalog update --root /absolute/disposable-root` before announcing it.

Private signing material never belongs in this tree or a Pages artifact.
The signing command accepts the existing Ed25519 seed only through stdin.

See [using the catalog](../CATALOG.md) for consumer commands and
[the distribution contract](../contracts/catalog-distribution.md) for verification,
offline use, rollback and failure behavior.
