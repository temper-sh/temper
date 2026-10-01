# Run an explicit Temper configuration

Use this advanced workflow to download, inspect and render a configuration
you maintain yourself. You need a `manifest.yaml`, its `manifest.lock.yaml`,
and a `software.lock.yaml` if you also want to install the serving software.

For everyday setup, follow [Set up and use your local models](contracts/init.md).
The commands below use a separate Temper root so you can inspect the result
before connecting it to an application.

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
Without it, the check uses the records created when the files were fetched.

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

## Install the software listed in a lock

An already-resolved software lock can describe a shared base or an isolated
experiment environment. Preview the complete installation first:

```sh
temper software install \
  --lock /path/to/software.lock.yaml \
  --installation field-kit-base \
  --root /path/to/isolated/temper-root \
  --dry-run
```

Remove `--dry-run` to install the software and dependencies listed in the lock.
Supported installations use release archives or isolated Python environments
with pinned, verified packages. They do not depend on your system's Python
packages.

Check that the installation matches its lock:

```sh
temper software check \
  --lock /path/to/software.lock.yaml \
  --installation field-kit-base \
  --root /path/to/isolated/temper-root
```

Preview removal:

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

`temper probe serve` starts a temporary local server for an experiment. It
checks the rendered configuration and installed software before starting, and
runs in the foreground. Field Kit uses this command to manage bounded tests;
the [probe reference](contracts/probe-serve.md) describes its inputs and shutdown
behavior.

For a saved everyday layout, use
[`temper layout activate`, `status` and `stop`](contracts/init.md#activate-inspect-and-stop).
