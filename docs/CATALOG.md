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

Current source setup uses **presets** for exact weights/engine/settings and
user-owned **layouts** for their composition. The unpublished
[authoring catalog](../catalog/README.md) has five Recommended presets: Qwen via
Splash, Glimmer 30B, Gemma 26B-A4B, Gemma 31B, and Qwen's llama.cpp baseline.
All also includes the smaller Gemma E2B/E4B and Qwen3.5 choices. Every recommended
preset has manual copy; membership and ordering never select it for the user.

Use `init --catalog catalog/guided-setup.json` for the new editors, or
`configure --preset ID` with that catalog for explicit scripted choices.
`--template PRESET=PATCH|builtin` and `--context PRESET=TOKENS` customize exact
settings. New choices accept their authored window, while review identifies
unknown machine/context fit. Model ceilings do not establish safe capacity.
[Set up, edit and activate layouts](contracts/init.md).

The v3 authoring vocabulary does not rename fields inside issued v1/v2 catalogs,
Selections or Execution Locks. The released workflow above retains its original
profile and layout identities. New configuration stores independent preset locks
and composes them at activation, preserving different engine versions.

## Choose newer software explicitly

```sh
temper catalog compile --root "$HOME/.local/share/temper" \
  --selection selection.json --target darwin/arm64 --software latest \
  --out execution.latest.lock.json
```

`latest` resolves the newest downloadable llama.cpp nightly build and the stable
llama-swap release, including archive reads to verify their contents. The
resulting lock retains exact versions and checksums. This does not install or
activate the new software.

`--software tested` is an explicit fallback to recorded minimum tested versions
when the catalog supplies that evidence. It refuses an unknown tested boundary;
the published first profile does not declare one. The unpublished guided
candidate records observed fallback versions with their evidence boundaries
and, as of 28 September 2026, records llama.cpp b11205 for Glimmer/Gemma 26B,
b11157 for the earlier GGUF choices, Splash 1.1.0 and a shared llama-swap v260
router. Template and software changes still need behavioral checks; prior
capability observations retain their original conditions. A minimum required
version is a separate compatibility floor that every choice must satisfy. See the
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
