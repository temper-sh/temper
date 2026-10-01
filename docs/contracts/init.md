# Set up and use your local models

This guide takes you from choosing a preset to using it in a chat or coding app.
[Install Temper](../../README.md#get-started) first.

## Choose a preset

Open the setup wizard:

```sh
temper init
```

A **preset** combines model weights chosen for quality, an engine suited to the
model and hardware, and tuned settings. Start with **Recommended** and read the
descriptions. **All** also includes smaller models. Nothing is selected for you.

Check the hardware requirements and memory guidance beside your choice.
[Compare the available presets](../../catalog/README.md) if you need help.
Memory figures are estimates unless a matching measurement is shown.

To explore the wizard without saving or downloading anything, use
`temper init --dry-run`.

## Arrange a layout

A **layout** groups presets for a way of working: coding, writing, or local
helpers alongside a cloud assistant. Start with **Local**, or create your own.

For your first setup, open **Local**, include your chosen preset, and mark it
as **Default**. The three choices have different jobs:

| Choice | What happens |
|---|---|
| Included | The model is available to your apps. |
| Load on activation | The model loads when you start the layout, saving a wait on first use. |
| Default | Apps can request `default` instead of the preset's ID. |

A layout can include several models without keeping all of them in memory.
llama-swap loads models when requested and unloads idle ones, after 30 minutes
by default. Models that cannot fit together take turns. The models you choose
to load on activation must fit together.

**Utility** is another empty starting point. You can rename or remove either
starter and reuse the same presets in several layouts.

## Review and prepare

Open **Review** to see the downloads, disk requirements, and estimated memory
use. Model downloads can be many gigabytes; existing compatible downloads are
reused and verified.

Choose **Prepare installation** to save your choices and install the required
files. This does not start a model. **Save configuration** saves the choices
without downloading; **Cancel** leaves them unsaved.

## Activate, inspect and stop

After preparation, start the Local layout:

```sh
temper layout activate local
```

In your chat or coding app, use:

| Setting | Value |
|---|---|
| OpenAI-compatible API base URL | `http://127.0.0.1:8080/v1` |
| Model | `default`, if you selected a default in the layout |

You can also request an included preset by its ID. Temper supplies the local
model service; configure the connection in your preferred app.

```sh
temper layout status
temper layout stop
```

Activation keeps the service running after the command exits. Start it again
after restarting your Mac; it does not start automatically at login.

## Change your setup

```sh
temper configure
```

The same wizard lets you add presets, edit layouts, or change the default.
Review and prepare any missing files, then activate the layout to use the
changes. Saving alone does not change a running service.

For example, after filling the Utility layout:

```sh
temper layout activate utility
```

This switches from the active layout after checking that the current requests
have finished. Close idle client connections if they prevent switching.

Context controls how much conversation or source material a model can use.
Larger settings can need more memory and take longer to answer. Keep the
preset's setting unless you have a reason to change it; its advertised maximum
does not establish fit on your Mac. Context and template edits affect every
layout using that preset.

Clients that support the engine's request controls can change the thinking
level for a request. Current presets default to medium. Pi's thinking menu
requires a configured local provider; Temper's wizard does not configure Pi.

## Recover from a problem

| Problem | What to do |
|---|---|
| Preparation was interrupted | Run `temper configure --resume --prepare`. It reuses your saved choices and completed downloads. Missing files still need network access. |
| A model does not fit | Choose a smaller preset, reduce context, or load fewer models on activation. An unknown estimate is not a measured fit. |
| First Splash startup takes a long time | Its first run builds an additional weight cache. Allow time and disk space for conversion; a short client timeout may expire first. |
| Switching or stopping is refused | Let active requests finish, close apps holding connections, then retry. |
| The service crashed | Retry `temper layout activate local` to recover the saved layout. If Temper cannot verify process ownership, keep the reported details for diagnosis. |

Deleting a layout leaves its models installed. To remove a preset from your
selection, first remove it from layouts that use it.

## Keyboard controls

| Key | Action |
|---|---|
| Arrow keys | Move between rows and controls. |
| Enter or Space | Activate a button or toggle a checkbox. |
| Tab / Shift+Tab | Focus the forward / back action; Enter confirms it. |
| Esc | Go back or cancel the current form. |
| PgUp / PgDown | Scroll long cards and reviews. |

Mouse clicks and trackpad scrolling also work. On the preset screen, `a`
switches Recommended/All, `c` edits context, and `t` cycles compatible templates.

## Configuration and advanced use

Choices are saved in `~/.temper/configuration.json`. Use `--root PATH` on setup
and layout commands to keep another independent configuration, for example:

```sh
temper init --root "$HOME/.temper-writing"
temper layout activate local --root "$HOME/.temper-writing"
```

Each root has one active layout. To use a different local port, stop that
layout first, then activate it with `--listen 127.0.0.1:18080`.

[Script preset and layout changes](layouts.md#script-preset-and-layout-changes)
or read the [configuration and service reference](layouts.md) for exact behavior.
[Catalog updates](../CATALOG.md) do not replace your saved choices.

Configurations from alpha.10 or earlier need a fresh root and new selections.
Keep unfinished Field Kit experiments with the version that created them.
