# Software catalog publication tree

This directory is the source served below
`https://temper-sh.github.io/temper/catalog/` when GitHub Pages is enabled from
the repository's `docs/` directory. Committed publication bytes are public
release artifacts; no private signing material belongs here.

Layout:

```text
channels/<channel>/channel.yaml
channels/<channel>/channel.signature.yaml
snapshots/<catalog-sha256>/catalog.yaml
snapshots/<catalog-sha256>/catalog.signature.yaml
```

Snapshot directories are immutable. Their directory name is the SHA-256 of the
exact `catalog.yaml` bytes. A channel is the only moving publication: release
review writes a higher monotonic sequence and exact snapshot locator, validates
the complete channel-to-snapshot join, then signs the final channel bytes.
Existing snapshot bytes are never rewritten or removed while a Temper lock,
receipt, Field Kit packet, or supported binary may reference them.

The retained release workflow is [`temper-catalog`](../contracts/catalog-signing.md).
It reads the Ed25519 seed only from stdin, validates the exact document and
compiled release capabilities, then atomically creates or explicitly replaces
the detached signature. The same command verifies both sides of the published
channel-to-snapshot pair without private key access.

The sequence-1 snapshot is also Temper's embedded bootstrap. The hermetic test
suite requires the embedded and published catalog/signature bytes to remain
identical, verifies both signatures with the compiled production trust root,
and verifies the stable channel's digest, sequence, and locator join. Future
catalog snapshots are independent release data and do not replace the embedded
bootstrap until a later binary release deliberately chooses a new fallback.
