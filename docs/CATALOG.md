# Update and inspect the preset catalog

The catalog contains the presets offered by Temper.
[Compare the choices](../catalog/README.md), then use `temper init` or
`temper configure` to select them.

Setup uses a verified local catalog when one is available. Otherwise it reads
the signed stable catalog. A damaged local catalog is reported rather than
silently replaced.

## Get catalog updates

```sh
temper catalog update --root "$HOME/.temper"
```

Temper downloads and verifies the catalog before making it available.
This updates the choices you can browse; your saved presets, installed models,
and running layout stay unchanged.

To inspect the stored catalog snapshots:

```sh
temper catalog inspect --root "$HOME/.temper" --json
```

You can return to a retained snapshot without a network connection:

```sh
temper catalog rollback --root "$HOME/.temper" --snapshot SHA256
```

Replace `SHA256` with the snapshot identity from inspection. Rolling back the
catalog also leaves your saved configuration unchanged.

## Compile a preset for a script or experiment

An **execution lock** is a file recording the exact model files, engine, and
settings needed to run one preset. Use it when a script or experiment needs
fixed inputs independent of your everyday layouts.

```sh
temper catalog compile --root "$HOME/.temper" \
  --preset qwen3.8-27b-q4xl-mtp --target darwin/arm64 \
  --out execution.lock.json
temper execution inspect --lock execution.lock.json
```

Compilation can use the stored catalog and recorded software offline. It does
not download models or start a service. Repeating the same compilation leaves
the output unchanged; use a new output path for different inputs.
`--dry-run` previews without writing.

After reviewing the requirements, follow the
[execution command reference](contracts/execution-runtime.md) for installation,
running, and cleanup.

## Choose software versions explicitly

| Option | Behavior |
|---|---|
| `--software recorded` | Uses the catalog's recorded releases; the default. |
| `--software latest` | Looks up and verifies newer releases. |
| `--software tested` | Uses a recorded tested version where one is available. |

These choices apply when compiling a new preset. They do not update saved
locks, and a failed lookup never silently switches to another option.
A tested release has evidence under particular conditions, not a guarantee
for every model, setting, or Mac.

## Reference

- [Preset authoring and execution locks](contracts/execution-lock.md)
- [Signed updates, verification, and rollback](contracts/catalog-distribution.md)
- [Catalog publication for maintainers](catalog/README.md)

Current Temper accepts catalog v3 and execution-lock v3. Configurations from
alpha.10 or earlier need new selections in a fresh root. Historical experiments
keep their original inputs and compatible Temper version.
