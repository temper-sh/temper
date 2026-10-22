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

Temper runs on macOS on Apple Silicon. Individual
presets may require a newer chip or macOS version; setup shows those requirements.

Temper downloads models with the Hugging Face command-line tool `hf`, which you
install yourself, for example with `brew install hf`. Temper installs the
engines and the router it needs; it does not install `hf` or your chat app.

We advise installing the [Pi](https://github.com/earendil-works/pi) coding
agent before you set up Temper, so you can try your models as soon as the
layout starts. Pi's own installer is
`curl -fsSL https://pi.dev/install.sh | sh`. Select Pi on the wizard's
**Harnesses** screen to synchronize the active layout's models automatically.
PiG and Hermes are supported too; see [Harness integrations](docs/contracts/harnesses.md).

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

In the wizard, choose your presets. The **Default** layout includes them all,
with models loading only when requested. Choose any harness integrations,
then review the downloads and memory
estimates, then choose **Install and start**. Model downloads can be many
gigabytes. When the install finishes, Temper starts the layout and prints its
address and model IDs. To start it again later, for example after restarting
your Mac:

```sh
temper layout activate default
```

In a selected Pi integration, open `/model` to refresh the picker; restart PiG
if its picker has not refreshed. For Hermes, start a new session and choose
`temper` in its model picker. Connect other chat or coding apps using the OpenAI-compatible API
base URL `http://127.0.0.1:8080/v1` and an included preset's ID as the model name.
You can optionally mark a preset as **Default** in the layout to use the model
name `default`.

## Presets and layouts

A **preset** combines model weights chosen for the best quality, the engine
that best fits the model and hardware, and tuned settings.

A **layout** groups presets for a particular way of working. You might have one
for coding, another for writing, or one with small local helpers to use alongside
a cloud assistant. You can switch layouts without reinstalling models.
Start with **Default**, then edit it or add another layout, such as **Helper**
for small models.

Use [`temper layout`](docs/contracts/layouts.md#standalone-layout-editor) to
choose which of your selected presets belong to each layout, what loads at
startup, and when idle models unload. Saving a layout keeps it ready for your
next explicit activation.

Use [`temper preset`](docs/contracts/user-presets.md) to fork a catalog preset
or make an independent copy, then edit its settings, weights or template.
Saving keeps your authoring separate from the exact locks used by layouts.

## How we choose presets

The **General** and **Specialist** tabs hold our curated recommendations for
general work and specific tasks. We combine trusted upstream and practitioner evidence with
our own experiments and checks of compatibility, memory use, context, and
response time. Answer quality and usefulness matter more than raw generation
speed.

Our [research and guides](https://github.com/temper-sh/temper-sh.github.io)
explain the model assessments and experiments behind these choices.

Each recommendation explains its intended use and main tradeoffs. Specialist
choices are grouped by task, with guidance on their role in a pipeline. The
**Other** tab holds additional options outside those recommendations, including
smaller models. Each preset appears in one tab. Estimates and untested machine
fit remain visible; fit on 8/16 GB Macs has not yet been verified.

Every choice shows its model license. Click the underlined license to read
its conditions; the same link appears in the final review.

The **Compute** control offers **CPU** or **GPU** where both are practical choices.
The selected device appears before Context on the same settings line. Fixed GPU
presets omit the device label; CPU-only presets show **CPU only**.
The choice applies in every layout using that preset and appears in the memory
review. CPU models still use RAM.

## Everyday use

```sh
temper update          # Get the newest presets and software for your choices
temper configure       # Change presets and layouts
temper layout status   # Inspect the running layout
temper layout stop     # Stop serving and unload its models
```

Saved choices stay unchanged until you run `temper update` or edit them.
`temper update` downloads the newest catalog and shows, for each saved preset,
what changes (old → new); your context and compute choices stay. It then
installs the new files and restarts the running layout. Add `--dry-run` to only
look, or `--software` to change only the router and engine releases.

By default Temper uses the software releases the catalog tested. To follow the
newest upstream releases instead, run `temper configure --software latest`;
when a newer release is not usable, Temper keeps the tested one and says why.

Your configuration lives in `~/.temper` by default. Preparing a layout installs
its files; activation starts the service. Model weights stay in the shared
Hugging Face cache; Temper keeps references to those files and reuses completed
downloads. The router writes its log to `~/.temper/managed/logs/router.log`;
each start begins a new log.

Model serving is accessible only from your Mac. Temper has no telemetry or
background updater.

Upgrade with `brew upgrade temper-sh/temper/temper`. A running layout keeps
working and moves to the new release when it next starts. When upgrading
from 0.1.0, run `temper layout stop` first.

## Uninstall and disk space

To remove Temper completely:

```sh
temper layout stop
brew uninstall temper-sh/temper/temper
rm -rf ~/.temper
```

`~/.temper` holds two things you chose: `configuration.json` (your presets and
layouts) and `catalog/` (the verified catalogs). Everything else there is
cache that Temper can rebuild: installed engines and the router (about 30–300
MB per release), a prepared model copy for Splash presets (about 16–20 GB per
version; the review shows the estimate), model links and rendered settings.
`temper cleanup` removes old releases and caches that no saved preset and no
running layout uses (`--dry-run` shows them first). Deleting `~/.temper` with
no layout running and running `temper init` again is also safe.

Model files stay in the Hugging Face cache (usually `~/.cache/huggingface/hub`),
because other tools can use them too. List them with `hf cache ls` and remove
one with `hf cache rm model/OWNER/NAME`; each model can take many gigabytes.

A Temper folder cannot be moved or copied: a running layout is tied to its
path. To start over somewhere else, stop the layout and run `temper init --root
PATH`; then pass `--root PATH` to every command.

## Compatibility

Temper keeps these working across its releases: the published catalog files;
your `~/.temper` folder (`configuration.json`, the stored catalog and
`temper-root.json`, which marks the folder's format), since 0.2; and your own
preset files in `presets.d` (`temper-preset/v1`), since 0.3. Command output,
file formats inside the cache, and tool commands such as `catalog compile` may
change in any release; scripts that use them should pin an exact Temper
version.

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
- [Preset choices and their limitations](https://temper-sh.github.io/catalog/)
- [Your own presets](docs/contracts/user-presets.md)
- [Command reference](docs/contracts/)
- [Contributing and building from source](docs/CODE.md)

## License

[0BSD](LICENSE).
