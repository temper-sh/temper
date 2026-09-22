# Choose a configuration from the catalog

Temper's catalog lets you inspect an installable configuration and save an exact
execution lock before downloading model files. These commands require Temper
**0.1.0-alpha.10 or later** on macOS ARM64.

The first profile is `qwen3.8-27b-q4xl-local`: Qwen3.8 27B in the
UD-Q4_K_XL build, the Frog v22.5 template, and a 32,768-token context window.
Its model file is approximately 17.6 GB. The configuration comes from local
Apple M5 / 32 GiB work; independent Field Kit observations are still pending.

## Download and inspect

```sh
mkdir -p "$HOME/.local/share"
temper catalog update --root "$HOME/.local/share/temper"
temper catalog inspect --root "$HOME/.local/share/temper"
temper catalog inspect --root "$HOME/.local/share/temper" \
  --profile qwen3.8-27b-q4xl-local --json
```

The update downloads signed catalog metadata. Inspection uses the verified local
copy and lists available profiles and retained snapshots. These commands do not
download model files or serving software.

## Select and lock

Run these commands in the directory where you want to keep your configuration:

```sh
temper catalog select --root "$HOME/.local/share/temper" \
  --profile qwen3.8-27b-q4xl-local --out selection.json
temper catalog compile --root "$HOME/.local/share/temper" \
  --selection selection.json --target darwin/arm64 --out execution.lock.json
temper execution inspect --lock execution.lock.json
```

The Selection records your profile choice. The Execution Lock records the exact
model, template, software files and settings that will be used. The default
`--software recorded` uses catalog-recorded versions and works offline after the
catalog update. Inspect the lock's downloads and machine requirements before
continuing to [installation and execution](contracts/execution-runtime.md).

Selection and compilation support `--dry-run`. Identical reruns leave files
unchanged. To adopt a different choice or configuration, use a new output path;
Temper refuses to overwrite a different existing Selection or Execution Lock.

## Choose newer software explicitly

```sh
temper catalog compile --root "$HOME/.local/share/temper" \
  --selection selection.json --target darwin/arm64 --software latest \
  --out execution.latest.lock.json
```

`latest` resolves upstream stable engine and router releases, including archive
reads to verify their contents. The resulting lock retains exact versions and
checksums. This does not install or activate the new software.

`--software tested` is an explicit fallback to recorded minimum tested versions
when the catalog supplies that evidence. It refuses an unknown tested boundary;
the first profile does not declare one. A minimum required version is a separate
compatibility floor that every choice must satisfy. See the
[version-selection contract](contracts/execution-lock.md#owned-records-and-scope).

## Update or roll back the catalog

Run `catalog update` again when you want new entries. Your Selection, existing
locks and installed software stay unchanged. Compile to a new lock path when you
decide to adopt updated inputs.

`catalog inspect` lists retained snapshot digests. To deliberately return to one:

```text
temper catalog rollback --root /absolute/temper-root --snapshot FULL_SHA256
```

Replace the root and digest with your actual values. Rollback works offline and
supports `--dry-run`. Temper retains the highest accepted publication sequence,
so a stale network response cannot silently move the catalog backward afterward.
A subsequent explicit `catalog update` can restore the latest publication.

The [distribution contract](contracts/catalog-distribution.md) documents exact
verification and recovery behavior. Existing Execution Locks remain usable even
when the catalog cannot be reached.
