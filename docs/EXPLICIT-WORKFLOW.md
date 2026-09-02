# Run an explicit Temper configuration

This guide is for collaborators who already have a reviewed `manifest.yaml`,
its `manifest.lock.yaml`, and any required `software.lock.yaml`. It is not a
model-selection guide: Temper will not infer a model, engine, mode, tool, or
live installation for you.

Use an isolated Temper root. Do not point these commands at a live service or
consumer configuration unless that cutover has been reviewed separately.

## Resolve and inspect the model configuration

Fill only missing model and template lock rows:

```sh
temper resolve \
  --manifest /path/to/manifest.yaml \
  --lock /path/to/manifest.lock.yaml
```

To preview an explicit upstream pin move, name one layout:

```sh
temper update qwen3.8-27b-gguf-24k \
  --manifest /path/to/manifest.yaml \
  --lock /path/to/manifest.lock.yaml \
  --dry-run
```

Remove `--dry-run` only when you intend to commit that lock change. `update`
does not fetch weights, render configuration, or touch a service; it prints the
follow-up commands required by the changed pin.

Fetch each layout needed by the selected mode, one explicit ID at a time:

```sh
temper fetch qwen3.8-27b-gguf-24k \
  --manifest /path/to/manifest.yaml \
  --lock /path/to/manifest.lock.yaml \
  --root /path/to/isolated/temper-root
```

A fetch can download multi-gigabyte weights. Temper verifies every member of
the selected artifact set before publishing that set beneath the isolated
root; it does not provide a fetch-all operation.

Audit the selected mode and its predicted resident-memory fit:

```sh
temper check \
  --manifest /path/to/manifest.yaml \
  --lock /path/to/manifest.lock.yaml \
  --mode local \
  --root /path/to/isolated/temper-root
```

Add `--verify` when you want a full SHA-256 read of every selected artifact.
Without it, the check uses the admitted immutable artifact-set records.

Preview the rendered generation:

```sh
temper apply \
  --manifest /path/to/manifest.yaml \
  --lock /path/to/manifest.lock.yaml \
  --mode local \
  --root /path/to/isolated/temper-root \
  --dry-run
```

The dry run still requires the selected artifact sets to be present and valid;
Temper will not preview configuration backed by missing model material. Remove
`--dry-run` to publish one immutable rendered generation beneath the explicit
root. `apply` does not activate that generation in a live llama-swap or harness
configuration.

## Install an exact software closure

An already-resolved software lock can describe a shared base or an isolated
experiment environment. Preview the complete installation first:

```sh
temper software install \
  --lock /path/to/software.lock.yaml \
  --installation field-kit-base \
  --root /path/to/isolated/temper-root \
  --dry-run
```

Remove `--dry-run` to install only the named, locked closure. Temper's compiled
isolated methods support reviewed upstream release archives and uv environments
made from an exact managed CPython plus a hash-required wheel set. Temper does
not discover ambient Python packages, indexes, or caches as dependencies.

Check the installed provider, receipt, requirements, and shared claims:

```sh
temper software check \
  --lock /path/to/software.lock.yaml \
  --installation field-kit-base \
  --root /path/to/isolated/temper-root
```

Preview provenance-guided removal:

```sh
temper software remove \
  --lock /path/to/software.lock.yaml \
  --installation field-kit-base \
  --root /path/to/isolated/temper-root \
  --dry-run
```

Remove `--dry-run` only when you intend to release that named installation.
Temper preserves pre-existing files and shared packages still claimed by
another installation.

## Start a bounded probe

`temper probe serve` is intended for a Field Kit stage rather than direct
interactive use. It admits only a rendered generation whose required
executables resolve inside matching software receipts, then starts a loopback
foreground process group. Review the exact interface and shutdown behavior in
the [probe contract](contracts/probe-serve.md).

This isolated probe is not Temper's future production `start`, `stop`, or
`status` lifecycle. It never selects a question, model, or experiment and does
not take over the live service.
