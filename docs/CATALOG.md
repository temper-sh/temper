# Update and inspect the preset catalog

The catalog contains the presets offered by Temper.
[Compare the choices](https://temper-sh.github.io/catalog/), then use
`temper init` or `temper configure` to select them.

Setup uses the verified catalog stored in your root. When the root has none,
the first real setup downloads and stores the signed catalog. A damaged local
catalog is reported rather than silently replaced. Only `temper update` and
`temper catalog update` download a newer one.

## Get catalog updates

```sh
temper catalog update --root "$HOME/.temper"
```

Temper downloads and verifies the catalog before making it available.
This updates the choices you can browse; your saved presets, installed models,
and running layout stay unchanged. To also rebuild your saved presets from it,
use `temper update`.

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

Compilation can use the stored catalog and its tested software offline. It does
not download models or start a service. `--memory-gib N` chooses the
settings the catalog gives a Mac with N GiB of memory; by default the
smallest. Repeating the same compilation leaves
the output unchanged; use a new output path for different inputs.
`--dry-run` previews without writing.

After reviewing the requirements, follow the
[execution command reference](contracts/execution-runtime.md) for installation,
running, and cleanup.

## Choose software versions explicitly

| Option | Behavior |
|---|---|
| `--software stable` | Uses the releases the catalog tested; the default. |
| `--software latest` | Looks up and verifies the newest allowed upstream releases. |

These choices apply when compiling a new preset. They do not update saved
locks. For `catalog compile`, a failed lookup never silently switches to
the stable release; guided setup falls back per item and says so. A tested
release has evidence under particular conditions, not a guarantee for every
model, setting, or Mac.

## Reference

- [Preset authoring and execution locks](contracts/execution-lock.md)
- [Signed updates, verification, and rollback](contracts/catalog-distribution.md)
- [Catalog publication for maintainers](contracts/catalog-signing.md)

Temper 0.1.0 accepts catalog v1 and execution-lock v1. Pre-release configurations
need new selections in a fresh root. Historical experiments
keep their original inputs and compatible Temper version.

To try an edited catalog source, follow the
[catalog source preview](CODE.md#preview-a-catalog-source).
