# `temper-catalog` release signing contract

Status: approved for retained software-catalog release tooling, 2026-08-24.

`temper-catalog` is a release-only command, deliberately separate from the
end-user `temper` binary. It validates and signs exact software catalog or
channel bytes with the production trust identity, and verifies detached
publications without needing a private key.

## Signing

```text
op read "$TEMPER_CATALOG_KEY_REF" | go run ./cmd/temper-catalog sign \
  --kind catalog \
  --artifact docs/catalog/snapshots/<sha256>/catalog.yaml \
  --output docs/catalog/snapshots/<sha256>/catalog.signature.yaml \
  [--replace] \
  [--dry-run]

op read "$TEMPER_CATALOG_KEY_REF" | go run ./cmd/temper-catalog sign \
  --kind channel \
  --channel stable \
  --artifact docs/catalog/channels/stable/channel.yaml \
  --output docs/catalog/channels/stable/channel.signature.yaml \
  [--replace] \
  [--dry-run]
```

The command accepts exactly one canonical standard-padded base64 Ed25519 seed
on stdin, with an optional final LF or CRLF. It has no key flag, key-file
option, credential discovery, or 1Password-specific code. `op read` is one
supported way to provide the input without placing it in argv or the tree.

Before any file effect, the command strictly validates the artifact. Catalogs
must satisfy the schema and every adapter capability compiled into the release
tool. Channels must satisfy the channel schema and the explicit `--channel`
identity. The derived signature must verify with the production public trust
root, proving that the supplied seed is the corresponding release key.

An absent output is staged beside its destination, synced, and atomically
placed. An identical output is unchanged. A different output is refused unless
`--replace` is explicit; replacement is also staged and atomic. The original
destination is rechecked immediately before commit, and a concurrent change is
refused. `--dry-run` performs the key, artifact, signature, and output checks
but creates no file or temporary stage.

Success emits exactly one line:

```text
RESULT catalog-sign <created|replaced|would-create|would-replace|unchanged> kind=<catalog|channel> key=<id>
```

## Verification

```text
go run ./cmd/temper-catalog verify \
  --kind catalog \
  --artifact docs/catalog/snapshots/<sha256>/catalog.yaml \
  --signature docs/catalog/snapshots/<sha256>/catalog.signature.yaml

go run ./cmd/temper-catalog verify \
  --kind channel \
  --channel stable \
  --artifact docs/catalog/channels/stable/channel.yaml \
  --signature docs/catalog/channels/stable/channel.signature.yaml
```

Verification checks the exact bytes, strict detached-signature envelope,
production trust key, document schema, expected channel identity, and compiled
catalog capabilities. Success emits:

```text
RESULT catalog-verify valid kind=<catalog|channel> key=<id>
```

Exit `0` means the result line is valid. Exit `1` is a key-input, artifact,
signature, filesystem, capability, or commit failure. Exit `2` is invalid
command usage. Failures emit no result line and never echo seed bytes.
