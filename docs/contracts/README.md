# Command reference

Start with [Set up and use your local models](init.md) for a complete walkthrough.
These pages explain individual commands and their exact behavior.

## Everyday use

| Task | Command or guide |
|---|---|
| Choose models and prepare a layout | [`temper init` and `temper configure`](init.md) |
| Compose layouts from saved presets | [`temper layout`](layouts.md#standalone-layout-editor) |
| Start, inspect or stop a saved layout | [`temper layout activate`, `status`, `stop`](layouts.md) |
| Connect Pi, PiG or Hermes to the active layout | [Harness integrations](harnesses.md) |
| Understand estimated and unknown memory use | [Memory estimates](memory-estimates.md) |
| Get the newest presets and software for your choices | [`temper update`](init.md#change-your-setup) |
| Update or roll back the stored catalog | [Catalog updates](../CATALOG.md) |
| Manage presets and layouts in scripts | [Scripted configuration](layouts.md#script-preset-and-layout-changes) |

## Custom configurations and experiments

| Task | Reference |
|---|---|
| Install, check or remove locked software | [`software`](software-install.md) |
| Create and edit your own presets | [`temper preset`](user-presets.md) |
| Define and compile a preset | [Catalog records and execution locks](execution-lock.md) |
| Prepare, serve and remove an execution lock | [Execution commands](execution-runtime.md) |

## Releases and catalog maintenance

- [Build, package and distribute Temper](release.md)
- [Distribute catalog updates](catalog-distribution.md)
- [Publish and verify a catalog](catalog-signing.md)

[Back to Temper](../../README.md)
