# Command reference

Start with [Set up and use your local models](init.md) for a complete walkthrough.
These pages explain individual commands and their exact behavior.

## Everyday use

| Task | Command or guide |
|---|---|
| Choose models and prepare a layout | [`temper init` and `temper configure`](init.md) |
| Start, inspect or stop a saved layout | [`temper layout activate`, `status`, `stop`](layouts.md) |
| Update the available presets | [Catalog updates](../CATALOG.md) |
| Manage presets and layouts in scripts | [Scripted configuration](layouts.md#script-preset-and-layout-changes) |

## Custom configurations and experiments

The [explicit configuration workflow](../EXPLICIT-WORKFLOW.md) connects these
commands into a download, inspection and rendering procedure.

| Task | Reference |
|---|---|
| Pin model files and templates | [`resolve`](resolve.md) and [`update`](update.md) |
| Download model files | [`fetch`](fetch.md) |
| Check files and memory estimates | [`check`](check.md) |
| Render a configuration | [`apply`](apply.md) |
| Install, check or remove locked software | [`software`](software-install.md) |
| Define and compile a preset | [Catalog records and execution locks](execution-lock.md) |
| Prepare and run an execution lock | [Execution commands](execution-runtime.md) |
| Start an experiment server | [`probe serve`](probe-serve.md) |
| Count tokens with the selected engine | [`probe tokenize`](probe-tokenize.md) |
| Provide host operations to Field Kit | [Field Kit interface](field-kit.md) |

## Releases and catalog maintenance

- [Build, package and distribute Temper](release.md)
- [Distribute catalog updates](catalog-distribution.md)
- [Sign and verify a catalog](catalog-signing.md)

[Back to Temper](../../README.md)
