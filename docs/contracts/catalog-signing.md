# `temper-catalog` publication contract

Status: current catalog publication tooling for Temper 0.1.0, 2026-10-11.

`temper-catalog` is a release-only command, separate from the end-user
`temper` binary. `publish` turns one authored catalog source into a signed
publication in one step. `verify-publication` checks a publication tree
without a key. The [distribution contract](catalog-distribution.md) owns the
channel format and what clients do with it.

## Publication tree

The tree is the `catalog/v1/` folder that the Results site serves at
`https://temper-sh.github.io/catalog/v1/`:

```text
channels/stable/channel.yaml
channels/stable/channel.signature.yaml
snapshots/<catalog-sha256>/catalog.json
snapshots/<catalog-sha256>/catalog.signature.yaml
```

Authored sources live next to it, one per platform, for example
`catalog/sources/apple-silicon.json`. A snapshot is the exact bytes of its
source; it is never rewritten.

## Publish

```text
op read "$TEMPER_CATALOG_KEY_REF" | go run ./cmd/temper-catalog publish \
  --source ../temper-sh.github.io/catalog/sources/apple-silicon.json \
  --root ../temper-sh.github.io/catalog/v1 \
  [--dry-run]
```

One `--source` per run. The command:

1. reads the source with the strict publisher rules (unknown keys refused,
   complete presentation) and takes the platform from its `platform` field;
2. checks `previous date <= date <= today (UTC)`, where the previous date is
   the date of the catalog the channel names for this platform now; the
   command never rewrites `date`;
3. reads and verifies the current channel, if the tree has one;
4. stops with `unchanged` when the channel already names this source's
   SHA-256 for its platform;
5. otherwise builds the next channel: sequence + 1 (1 for a new tree), this
   platform's entry replaced, every other platform's entry kept;
6. signs the catalog and the channel with the seed from stdin, writes them
   to a stage, and runs the full publication check on the staged result;
7. without `--dry-run`, commits: the snapshot first, the channel pair last.
   `--dry-run` writes nothing into the tree.

The seed is one canonical standard-padded base64 Ed25519 seed on stdin, with
an optional final LF or CRLF. The command has no key flag, key file or
credential discovery. The derived key must match the production trust root.

Success prints one line that says whether the platform's catalog was
published, would be published (dry run) or was already published, with its
sequence, preset count and SHA-256.

## Verify a publication

```text
go run ./cmd/temper-catalog verify-publication --root ../temper-sh.github.io/catalog/v1 \
  [--source ../temper-sh.github.io/catalog/sources/apple-silicon.json]
```

This checks the channel signature and schema; for every platform entry, the
snapshot folder, its signature and its digest; and, for this Temper's
platform, the full catalog with the client reader. `--source` also requires
the published snapshot to be the source's exact bytes. It reads only local
files and prints one line with the verified sequence, preset count and
digest.

## Availability

```text
go run ./cmd/temper-catalog availability --catalog FILE [--temper-version V] [--json]
```

Reports which presets a Temper version can use from one catalog and why the
others are unavailable.

Exit `0` means the result line is valid. Exit `1` is a key-input, source,
signature, filesystem, capability or commit failure. Exit `2` is invalid
usage. Failures print no result line and never echo seed bytes.
