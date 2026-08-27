# `temper software catalog update` contract

Status: approved software-catalog update implementation, 2026-08-24.

## Invocation

```text
temper software catalog update \
  --root /absolute/temper/data/root \
  [--channel stable] \
  [--dry-run]
```

`--root` is required and is never inferred from the live service, a user home,
or the legacy stack. `--channel` defaults to `stable` and must be a lowercase
stable ID. The v1 production channel root is compiled into the binary as:

```text
https://temper-sh.github.io/temper/catalog/channels/
```

There is no command-line source override, credential discovery, mirror
fallback, cache, retry, background updater, or daemon. The explicit command is
the consent boundary for its four small HTTPS reads, and the complete operation
has a 30-second deadline.

## Read and commit behavior

For channel `stable`, the production source reads, in order:

1. `stable/channel.yaml`;
2. `stable/channel.signature.yaml`;
3. the signed catalog locator's `catalog.yaml`;
4. that locator's `catalog.signature.yaml`.

Each read uses the bounded HTTPS source contract. Temper verifies the channel
signature, channel-to-catalog digest/schema/sequence join, catalog signature,
strict catalog schema, and every declared compiled adapter capability before
examining or changing the active catalog store.

The candidate is then compared with the authenticated active snapshot.
Rollback and same-sequence equivocation are refusals. A changed publication is
staged as an immutable snapshot and becomes active through one atomic pointer
commit. An identical second run is clean. `--dry-run` completes the same reads
and validation but creates no root, directory, snapshot, temporary file, or
active pointer.

This command changes only the active software catalog. It never rewrites a
software lock, resolves or installs software, changes a receipt, starts a
service, or touches the live legacy stack.

## Output and exits

Success emits exactly one stable result line:

```text
RESULT software-catalog-update <changed|would-change|unchanged> channel=<id> sequence=<uint> sha256=<digest> channel-key=<id> catalog-key=<id>
```

- `changed` means a new authenticated snapshot was activated.
- `would-change` means the same activation would occur without `--dry-run`.
- `unchanged` means the authenticated candidate is already active; it is also
  used for an unchanged dry run.

Exit `0` means the result line is valid. Exit `1` is a read, authentication,
capability, rollback/equivocation, active-state, deadline, or commit failure.
Exit `2` is invalid command usage. Failures emit no result line.
