# Temper

Temper installs and manages a local AI stack on Apple Silicon Macs. It brings
together carefully chosen software and model presets for people who want to use
local AI without spending their time assembling the stack or following every
new release.

At its core is [llama-swap](https://github.com/mostlygeek/llama-swap), which
connects your apps to the right model, loads models on demand, and unloads them
when idle. Each model is paired with an engine, such as llama.cpp, carefully
chosen for that model and the hardware it will run on. Temper installs and
configures the pieces.

## Get started

Temper is currently alpha software for macOS on Apple Silicon. Individual
presets may require a newer chip or macOS version; setup shows those requirements.

Install with [Homebrew](https://brew.sh/) and open the setup wizard:

```sh
brew tap temper-sh/temper https://github.com/temper-sh/temper
brew trust --formula temper-sh/temper/temper
brew install temper-sh/temper/temper
temper init
```

On Homebrew versions before 7, omit the
[`brew trust`](https://docs.brew.sh/Manpage#trust-options-target-) line.
Homebrew uses a prebuilt package (bottle) when available, otherwise it builds
from source.

In the wizard, choose a preset, include it in the **Local** layout, and mark it
as **Default**. Review the downloads and memory estimates, then choose
**Prepare installation**. Model downloads can be many gigabytes. Once preparation
finishes, start the layout:

```sh
temper layout activate local
```

Connect your preferred chat or coding app using the OpenAI-compatible API
base URL `http://127.0.0.1:8080/v1` and model name `default`.

## Presets and layouts

A **preset** combines model weights chosen for the best quality, the engine
that best fits the model and hardware, and tuned settings.

A **layout** groups presets for a particular way of working. You might have one
for coding, another for writing, or one with small local helpers to use alongside
a cloud assistant. You can switch layouts without reinstalling models.
**Local** and **Utility** are starting points you can adapt, rename, or replace.

## How we choose presets

The **Recommended** list stays small, with choices for different work and
resource needs. We combine trusted upstream and practitioner evidence with
our own experiments and checks of compatibility, memory use, context, and
response time. Answer quality and usefulness matter more than raw generation
speed.

Our [research and guides](https://github.com/temper-sh/temper-sh.github.io)
explain the model assessments and experiments behind these choices.

Each recommendation explains its intended use and main tradeoffs. The **All**
tab includes the recommendations and other catalog options, including smaller
models. Estimates and untested machine fit remain visible; fit on 8/16 GB Macs
has not yet been verified.

## Everyday use

```sh
temper configure       # Change presets and layouts
temper layout status   # Inspect the running layout
temper layout stop     # Stop serving and unload its models
```

Your configuration lives in `~/.temper` by default. Preparing a layout installs
its files; activation starts the service. Saved choices stay unchanged until
you edit them, and shared model downloads are reused.

Model serving is accessible only from your Mac. Temper has no telemetry or
background updater.

Stop active layouts before upgrading with `brew upgrade temper-sh/temper/temper`
or removing the CLI with `brew uninstall temper-sh/temper/temper`. Uninstalling
keeps your configuration and model files.

## Help improve Temper

You can contribute without writing code:

- Run an experiment with [Field Kit](https://github.com/temper-sh/field-kit)
  on a supported Mac and share the report. It records the hardware, model, and
  settings so we can check recommendations on more machines. You review the
  results and choose what to share.
- Contribute corrections, useful workflows, or findings from your own setup
  to the research and guides.

## Learn more

- [Setup, layout management, and recovery](docs/contracts/init.md)
- [Preset choices and their limitations](catalog/README.md)
- [Command reference](docs/contracts/)
- [Contributing and building from source](docs/CODE.md)

## License

[0BSD](LICENSE).
